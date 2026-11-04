package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testJPEG(t *testing.T, c color.Color) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			im.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, im, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestMockDeterministicAndDifferent(t *testing.T) {
	src := testJPEG(t, color.RGBA{200, 100, 50, 255})
	m := NewMock("")
	a, err := m.Edit(context.Background(), Input{Image: src, MIME: "image/jpeg", Prompt: "p1"})
	if err != nil || !a.Mock || a.Provider != "mock" {
		t.Fatalf("%v %+v", err, a)
	}
	b, _ := m.Edit(context.Background(), Input{Image: src, MIME: "image/jpeg", Prompt: "p1"})
	c, _ := m.Edit(context.Background(), Input{Image: src, MIME: "image/jpeg", Prompt: "p2"})
	if !bytes.Equal(a.Image, b.Image) {
		t.Fatal("same input must give same output")
	}
	if bytes.Equal(a.Image, c.Image) || bytes.Equal(a.Image, src) {
		t.Fatal("output must differ from source and from other prompts")
	}
	if _, err := ValidateOutput(a.Image, src, 1<<20, 1<<24); err != nil {
		t.Fatal(err)
	}
}

func TestMockFailureModes(t *testing.T) {
	src := testJPEG(t, color.White)
	for mode, kind := range map[string]Kind{ModeFailure: Permanent, ModeBusy: NotExecuted} {
		_, err := NewMock(mode).Edit(context.Background(), Input{Image: src, MIME: "image/jpeg"})
		if AsError(err).Kind != kind {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := NewMock(ModeTimeout).Edit(ctx, Input{Image: src}); AsError(err).Code != "PROVIDER_TIMEOUT" {
		t.Fatal(err)
	}
	bad, _ := NewMock(ModeBadOut).Edit(context.Background(), Input{Image: src})
	if _, err := ValidateOutput(bad.Image, src, 1<<20, 1<<24); err == nil {
		t.Fatal("invalid output accepted")
	}
	same, _ := NewMock(ModeIdentical).Edit(context.Background(), Input{Image: src, MIME: "image/jpeg"})
	if _, err := ValidateOutput(same.Image, src, 1<<20, 1<<24); err == nil || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("identical output must fail: %v", err)
	}
}

func TestValidateOutputPNGAndLimits(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var b bytes.Buffer
	png.Encode(&b, im)
	if m, err := ValidateOutput(b.Bytes(), []byte("x"), 1<<20, 1000); err != nil || m != "image/png" {
		t.Fatal(m, err)
	}
	if _, err := ValidateOutput(b.Bytes(), []byte("x"), 1<<20, 50); err == nil {
		t.Fatal("pixel cap ignored")
	}
	if _, err := ValidateOutput(b.Bytes(), []byte("x"), 10, 1000); err == nil {
		t.Fatal("size cap ignored")
	}
}

func gjson(parts string) []byte {
	return []byte(`{"candidates":[{"content":{"parts":` + parts + `},"finishReason":"STOP"}],"responseId":"r1","usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1290,"totalTokenCount":1300}}`)
}

func TestParseGeminiFixtures(t *testing.T) {
	img := base64.StdEncoding.EncodeToString([]byte("IMG"))
	ok, err := parseGemini(gjson(`[{"text":"here"},{"inlineData":{"mimeType":"image/png","data":"`+img+`"}}]`), "m", time.Second)
	if err != nil || string(ok.Image) != "IMG" || ok.MIME != "image/png" || ok.Usage["total_tokens"] != 1300 || ok.RemoteID != "r1" || ok.Mock {
		t.Fatalf("%v %+v", err, ok)
	}
	cases := map[string]struct{ body, code string }{
		"text only":      {string(gjson(`[{"text":"I cannot"}]`)), "PROVIDER_NO_IMAGE"},
		"no candidates":  {`{"candidates":[]}`, "PROVIDER_NO_IMAGE"},
		"prompt blocked": {`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`, "PROVIDER_REFUSED"},
		"image safety":   {`{"candidates":[{"finishReason":"IMAGE_SAFETY"}]}`, "PROVIDER_REFUSED"},
		"no image":       {`{"candidates":[{"finishReason":"NO_IMAGE"}]}`, "PROVIDER_NO_IMAGE"},
		"bad base64":     {string(gjson(`[{"inlineData":{"mimeType":"image/png","data":"!!!"}}]`)), "PROVIDER_INVALID_OUTPUT"},
		"not json":       {`<html>`, "PROVIDER_FAILURE"},
	}
	for name, c := range cases {
		_, err := parseGemini([]byte(c.body), "m", 0)
		if e := AsError(err); e.Code != c.code || e.Kind != Permanent {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestGeminiRequestShapeAgainstLocalServer(t *testing.T) {
	src := testJPEG(t, color.RGBA{1, 2, 3, 255})
	var gotKey, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath = r.Header.Get("x-goog-api-key"), r.URL.Path
		if r.URL.RawQuery != "" {
			t.Error("key must not be in the URL")
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.Write(gjson(`[{"inlineData":{"mimeType":"image/jpeg","data":"` + base64.StdEncoding.EncodeToString(testJPEG(t, color.Black)) + `"}}]`))
	}))
	defer srv.Close()
	out, err := newGemini("test-key", "model-x", srv.URL).Edit(context.Background(), Input{Image: src, MIME: "image/jpeg", Prompt: "EDIT THIS"})
	if err != nil || out.Provider != "gemini" || out.Model != "model-x" {
		t.Fatalf("%v %+v", err, out)
	}
	if gotKey != "test-key" || gotPath != "/v1beta/models/model-x:generateContent" {
		t.Fatalf("%q %q", gotKey, gotPath)
	}
	parts := gotBody["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "EDIT THIS" {
		t.Fatal("prompt not sent")
	}
	inl := parts[1].(map[string]any)["inlineData"].(map[string]any)
	dec, _ := base64.StdEncoding.DecodeString(inl["data"].(string))
	if inl["mimeType"] != "image/jpeg" || !bytes.Equal(dec, src) {
		t.Fatal("source bytes/MIME not transferred exactly")
	}
	if m := gotBody["generationConfig"].(map[string]any)["responseModalities"].([]any); len(m) != 2 {
		t.Fatal("responseModalities")
	}
}

func TestGeminiHTTPClassification(t *testing.T) {
	for status, kind := range map[int]Kind{429: NotExecuted, 503: NotExecuted, 500: Uncertain, 504: Uncertain, 400: Permanent, 403: Permanent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := newGemini("k", "m", srv.URL).Edit(context.Background(), Input{Image: []byte("x"), MIME: "image/jpeg"})
		srv.Close()
		if AsError(err).Kind != kind {
			t.Errorf("status %d -> %v", status, err)
		}
	}
}

func TestGeminiTimeoutIsUncertainAndLeaksNoKey(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // lets the server notice the client hanging up
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := newGemini("SECRET-KEY", "m", srv.URL).Edit(ctx, Input{Image: []byte("x"), MIME: "image/jpeg"})
	e := AsError(err)
	if e.Kind != Uncertain || e.Code != "PROVIDER_TIMEOUT" || strings.Contains(err.Error(), "SECRET-KEY") {
		t.Fatalf("%v", err)
	}
}
