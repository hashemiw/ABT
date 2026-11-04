package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"abt/backend/internal/generation"
	"abt/backend/internal/provider"
)

func (e *env) callIdem(method, path, token, key string, body any) (int, map[string]any) {
	e.t.Helper()
	return e.callWith(method, path, token, map[string]string{"Idempotency-Key": key}, body)
}

// readyAsset uploads a JPEG through the real media API and completes it.
func (e *env) readyAsset(u user) string {
	id := e.upload(u, jpegBytes(200, 200), "image/jpeg")
	if code, b := e.call("POST", "/v1/media/"+id+"/complete", u.token, "{}"); code != 200 {
		e.t.Fatalf("complete: %d %v", code, b)
	}
	return id
}

// Web -> Go API -> worker -> mock provider -> storage -> result. No real AI vendor is reachable from this test.
func TestGenerationEndToEndWithMock(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u, other := e.newUser(), e.newUser()

	if code, _ := e.call("GET", "/v1/styles/presets", "", nil); code != 401 {
		t.Fatalf("presets without auth: %d", code)
	}
	code, p := e.call("GET", "/v1/styles/presets", u.token, nil)
	if code != 200 || len(p["data"].([]any)) != 5 {
		t.Fatalf("%d %v", code, p)
	}
	for _, it := range p["data"].([]any) {
		for k := range it.(map[string]any) {
			if k == "prompt_template" || k == "template_version" {
				t.Fatal("prompt template leaked")
			}
		}
	}

	src := e.readyAsset(u)
	body := map[string]any{"source_asset_id": src, "preset_id": "old_money", "instruction": "dark navy"}
	code, g := e.callIdem("POST", "/v1/generations", u.token, "key-1", body)
	if code != 202 || g["status"] != "queued" || g["status_url"] != "/v1/generations/"+g["id"].(string) {
		t.Fatalf("%d %v", code, g)
	}
	id := g["id"].(string)
	if c2, g2 := e.callIdem("POST", "/v1/generations", u.token, "key-1", body); c2 != 202 || g2["id"] != id {
		t.Fatalf("idempotent repeat: %d %v", c2, g2)
	}
	if c3, b3 := e.callIdem("POST", "/v1/generations", u.token, "key-1", map[string]any{"source_asset_id": src, "preset_id": "minimalist"}); c3 != 409 || errCode(b3) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict: %d %v", c3, b3)
	}
	if c4, b4 := e.call("POST", "/v1/generations", u.token, body); c4 != 422 || errCode(b4) != "INVALID_REQUEST" {
		t.Fatalf("missing key: %d %v", c4, b4)
	}
	if c5, _ := e.callIdem("POST", "/v1/generations", other.token, "key-1", body); c5 != 404 {
		t.Fatalf("another user's source must be 404, got %d", c5)
	}
	if c6, _ := e.call("GET", "/v1/generations/"+id, other.token, nil); c6 != 404 {
		t.Fatalf("cross-user get: %d", c6)
	}

	_, st := e.call("GET", "/v1/generations/"+id, u.token, nil)
	if st["status"] != "queued" || st["result"] != nil {
		t.Fatalf("%v", st)
	}
	w := generation.NewWorker(e.db, e.st, provider.NewMock(""), e.gcfg, "http-test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if did, err := w.RunOnce(context.Background()); !did || err != nil {
		t.Fatal(did, err)
	}
	code, st = e.call("GET", "/v1/generations/"+id, u.token, nil)
	res, _ := st["result"].(map[string]any)
	if code != 200 || st["status"] != "completed" || res == nil || res["is_mock"] != true || res["provider"] != "mock" {
		t.Fatalf("%d %v", code, st)
	}
	resp, err := http.Get(res["download_url"].(string))
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("result download: %v %v", err, resp)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_, srcMeta := e.call("GET", "/v1/media/"+src, u.token, nil)
	sr, _ := http.Get(srcMeta["download_url"].(string))
	in, _ := io.ReadAll(sr.Body)
	sr.Body.Close()
	if bytes.Equal(in, out) || len(out) < 100 {
		t.Fatal("result must differ from the stored source")
	}
	// The result asset is a normal owned asset: owner can read it, others cannot, and deleting it hides the result.
	rid := res["asset_id"].(string)
	if c, _ := e.call("GET", "/v1/media/"+rid, other.token, nil); c != 404 {
		t.Fatalf("other user reads result asset: %d", c)
	}
	if c, _ := e.call("DELETE", "/v1/media/"+rid, u.token, nil); c != 202 {
		t.Fatalf("delete result: %d", c)
	}
	if _, st = e.call("GET", "/v1/generations/"+id, u.token, nil); st["result"] != nil {
		t.Fatal("deleted result still served")
	}
	// Cancel on a finished generation returns its actual state.
	if c, cb := e.call("POST", "/v1/generations/"+id+"/cancel", u.token, "{}"); c != 200 || cb["status"] != "completed" {
		t.Fatalf("%d %v", c, cb)
	}
	_ = strings.TrimSpace
}

func TestGenerationFailureIsReportedSafely(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u := e.newUser()
	src := e.readyAsset(u)
	_, g := e.callIdem("POST", "/v1/generations", u.token, "k", map[string]any{"source_asset_id": src, "preset_id": "streetwear"})
	w := generation.NewWorker(e.db, e.st, provider.NewMock(provider.ModeFailure), e.gcfg, "w", slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.RunOnce(context.Background())
	_, st := e.call("GET", "/v1/generations/"+g["id"].(string), u.token, nil)
	er, _ := st["error"].(map[string]any)
	if st["status"] != "failed" || st["result"] != nil || er == nil || er["code"] != "PROVIDER_FAILURE" || er["message"] == "" {
		t.Fatalf("%v", st)
	}
	if strings.Contains(er["message"].(string), "mock") {
		t.Fatal("internal detail leaked")
	}
}
