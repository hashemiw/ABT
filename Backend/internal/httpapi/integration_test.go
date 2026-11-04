package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"abt/backend/internal/config"
	"abt/backend/internal/httpapi"
	"abt/backend/internal/identity"
	"abt/backend/internal/media"
	"abt/backend/internal/storage"
	"abt/backend/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real PostgreSQL + S3-compatible storage. Opt in with TEST_DATABASE_URL and TEST_STORAGE_ENDPOINT/_ACCESS_KEY/_SECRET_KEY.
type env struct {
	t     *testing.T
	srv   *httptest.Server
	db    *pgxpool.Pool
	st    *storage.Client
	media *media.Service
}

func newEnv(t *testing.T, tune func(*config.Media), sessionsPerMinute int) *env {
	t.Helper()
	dsn, ep := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_STORAGE_ENDPOINT")
	if dsn == "" || ep == "" {
		t.Skip("set TEST_DATABASE_URL and TEST_STORAGE_* to run integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("abt_it_%d", time.Now().UnixNano())
	q := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(context.Background(), "DROP SCHEMA "+q+" CASCADE"); admin.Close() })
	pc, _ := pgxpool.ParseConfig(dsn)
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = migrations.Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := storage.New(storage.Config{Endpoint: ep, PublicEndpoint: ep, Bucket: "abt-test", Region: "us-east-1",
		AccessKey: os.Getenv("TEST_STORAGE_ACCESS_KEY"), SecretKey: os.Getenv("TEST_STORAGE_SECRET_KEY")})
	if err = st.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	mc := config.Media{MaxUploadBytes: 2 << 20, MaxPixels: 4_000_000, MinSide: 64, UploadURLTTL: time.Minute, DownloadURLTTL: time.Minute,
		MaxPendingUploads: 50, AbandonedUploadAfter: time.Hour, MaxConcurrentDecoders: 2}
	if tune != nil {
		tune(&mc)
	}
	ms := media.New(db, st, mc)
	h := httpapi.Handler(httpapi.Deps{DB: db, DBTimeout: time.Second, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Identity: identity.New(db, time.Hour), Media: ms, SessionsPerMinute: sessionsPerMinute})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t, srv, db, st, ms}
}

type user struct{ id, token string }

func (e *env) call(method, path, token string, body any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *env) newUser() user {
	e.t.Helper()
	code, b := e.call("POST", "/v1/sessions", "", "{}")
	if code != 201 {
		e.t.Fatalf("session: %d %v", code, b)
	}
	return user{b["user"].(map[string]any)["id"].(string), b["session"].(map[string]any)["token"].(string)}
}

func errCode(b map[string]any) string {
	if m, ok := b["error"].(map[string]any); ok {
		return m["code"].(string)
	}
	return ""
}

func jpegBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func pngBytes(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0 // fully transparent
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// withExif inserts an APP1/Exif segment after SOI to prove metadata is stripped.
func withExif(j []byte) []byte {
	seg := append([]byte{0xFF, 0xE1, 0, 12}, []byte("Exif\x00\x00SECRET")...)
	return append(append(append([]byte{}, j[:2]...), seg...), j[2:]...)
}

// upload runs the intent and PUT steps and returns the asset ID.
func (e *env) upload(u user, data []byte, declared string) string {
	e.t.Helper()
	code, b := e.call("POST", "/v1/media/upload-intents", u.token, map[string]any{"filename": "p", "content_type": declared, "size_bytes": len(data)})
	if code != 201 {
		e.t.Fatalf("intent: %d %v", code, b)
	}
	up := b["upload"].(map[string]any)
	req, _ := http.NewRequest(up["method"].(string), up["url"].(string), bytes.NewReader(data))
	for k, v := range up["headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		e.t.Fatalf("PUT: %v %v", err, resp)
	}
	resp.Body.Close()
	return b["asset_id"].(string)
}

func TestSessionsAndAuth(t *testing.T) {
	e := newEnv(t, nil, 1000)
	a := e.newUser()
	var hash string
	if err := e.db.QueryRow(context.Background(), `SELECT credential_hash FROM sessions WHERE user_id=$1`, a.id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == a.token || hash != identity.Hash(a.token) || len(a.token) < 40 {
		t.Fatal("token must be stored only as a hash and be high entropy")
	}
	for name, tok := range map[string]string{"missing": "", "garbage": "nope"} {
		if c, b := e.call("GET", "/v1/media/6ea536d8-cc40-43f7-b2fd-b893c0255a7a", tok, nil); c != 401 || errCode(b) != "UNAUTHORIZED" {
			t.Fatalf("%s: %d %v", name, c, b)
		}
	}
	e.db.Exec(context.Background(), `UPDATE sessions SET expires_at=now()-interval '1s', created_at=now()-interval '1h' WHERE user_id=$1`, a.id)
	if c, _ := e.call("POST", "/v1/media/upload-intents", a.token, map[string]any{}); c != 401 {
		t.Fatalf("expired session: %d", c)
	}
	b := e.newUser()
	e.db.Exec(context.Background(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1`, b.id)
	if c, _ := e.call("POST", "/v1/media/upload-intents", b.token, map[string]any{}); c != 401 {
		t.Fatalf("revoked session: %d", c)
	}
	if c, b := e.call("GET", "/nope", "", nil); c != 404 || errCode(b) != "RESOURCE_NOT_FOUND" {
		t.Fatal(c, b)
	}
	if c, b := e.call("GET", "/v1/sessions", "", nil); c != 405 || errCode(b) != "INVALID_REQUEST" {
		t.Fatal(c, b)
	}
}

func TestSessionRateLimit(t *testing.T) {
	e := newEnv(t, nil, 3)
	for i := 0; i < 3; i++ {
		e.newUser()
	}
	if c, b := e.call("POST", "/v1/sessions", "", "{}"); c != 429 || errCode(b) != "RATE_LIMITED" {
		t.Fatal(c, b)
	}
}

func TestUploadLifecycle(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u := e.newUser()
	data := withExif(jpegBytes(300, 200))
	id := e.upload(u, data, "image/jpeg")

	var key string
	e.db.QueryRow(context.Background(), `SELECT storage_key FROM media_assets WHERE id=$1`, id).Scan(&key)
	if !strings.Contains(key, u.id) || strings.Contains(key, "portrait") {
		t.Fatalf("key not server generated: %s", key)
	}
	c, b := e.call("GET", "/v1/media/"+id, u.token, nil)
	if c != 200 || b["status"] != "pending_upload" || b["download_url"] != nil {
		t.Fatalf("pending GET: %d %v", c, b)
	}
	// Repeated completion is safe, including concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, b := e.call("POST", "/v1/media/"+id+"/complete", u.token, "{}")
			if c != 200 || b["status"] != "ready" || b["width"].(float64) != 300 || b["height"].(float64) != 200 || b["content_type"] != "image/jpeg" {
				t.Errorf("complete: %d %v", c, b)
			}
		}()
	}
	wg.Wait()
	c, b = e.call("POST", "/v1/media/"+id+"/complete", u.token, "{}")
	if c != 200 || b["status"] != "ready" {
		t.Fatal(c, b)
	}
	c, b = e.call("GET", "/v1/media/"+id, u.token, nil)
	if c != 200 || b["kind"] != "original" || b["status"] != "ready" {
		t.Fatalf("ready GET: %d %v", c, b)
	}
	if _, leaked := b["storage_key"]; leaked || strings.Contains(fmt.Sprint(b), "media/") && !strings.Contains(b["download_url"].(string), "X-Amz-Signature") {
		t.Fatal("storage key leaked")
	}
	resp, err := http.Get(b["download_url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	img, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil || img.Bounds().Dx() != 300 || bytes.Contains(body, []byte("SECRET")) || bytes.Contains(body, []byte("Exif")) {
		t.Fatalf("download not a clean normalized JPEG: %v", err)
	}
	// The client-writable object must be gone.
	if _, err := e.st.Head(context.Background(), "uploads/"+u.id+"/"+id); err == nil {
		t.Fatal("raw upload object still exists")
	}
}

func TestPNGAlphaFlattened(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u := e.newUser()
	id := e.upload(u, pngBytes(100, 100), "image/png")
	if c, b := e.call("POST", "/v1/media/"+id+"/complete", u.token, nil); c != 200 || b["content_type"] != "image/jpeg" {
		t.Fatal(c, b)
	}
	_, b := e.call("GET", "/v1/media/"+id, u.token, nil)
	resp, _ := http.Get(b["download_url"].(string))
	img, err := jpeg.Decode(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if r, g, bl, _ := img.At(50, 50).RGBA(); r>>8 < 250 || g>>8 < 250 || bl>>8 < 250 {
		t.Fatal("transparent PNG should flatten to white")
	}
}

func TestInvalidMedia(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u := e.newUser()
	if c, b := e.call("POST", "/v1/media/00000000-0000-4000-8000-000000000000/complete", u.token, nil); c != 404 {
		t.Fatal(c, b)
	}
	// Not uploaded yet.
	code, b := e.call("POST", "/v1/media/upload-intents", u.token, map[string]any{"filename": "p.jpg", "content_type": "image/jpeg", "size_bytes": 10})
	id := b["asset_id"].(string)
	if code != 201 {
		t.Fatal(code)
	}
	if c, b := e.call("POST", "/v1/media/"+id+"/complete", u.token, nil); c != 409 || errCode(b) != "MEDIA_NOT_READY" {
		t.Fatal(c, b)
	}
	cases := map[string][]byte{
		"not an image":    []byte("this is plainly not an image, whatever the declared type says"),
		"too many pixels": jpegBytes(2100, 2100),
		"too small":       jpegBytes(20, 20),
		"truncated jpeg":  jpegBytes(300, 300)[:400],
	}
	for name, data := range cases {
		id := e.upload(u, data, "image/jpeg")
		for i := 0; i < 2; i++ { // rejection is stable on repeat
			if c, b := e.call("POST", "/v1/media/"+id+"/complete", u.token, nil); c != 422 || errCode(b) != "MEDIA_INVALID" {
				t.Fatalf("%s: %d %v", name, c, b)
			}
		}
		if _, b := e.call("GET", "/v1/media/"+id, u.token, nil); b["status"] != "rejected" || b["download_url"] != nil {
			t.Fatalf("%s: %v", name, b)
		}
		if _, err := e.st.Head(context.Background(), "uploads/"+u.id+"/"+id); err == nil {
			t.Fatalf("%s: rejected upload not removed", name)
		}
	}
}

func TestIntentValidationAndSignedConstraints(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u := e.newUser()
	for name, tc := range map[string]struct {
		body any
		want int
	}{
		"gif":       {map[string]any{"filename": "a", "content_type": "image/gif", "size_bytes": 5}, 415},
		"zero":      {map[string]any{"filename": "a", "content_type": "image/png", "size_bytes": 0}, 422},
		"huge":      {map[string]any{"filename": "a", "content_type": "image/png", "size_bytes": 3 << 20}, 413},
		"no name":   {map[string]any{"filename": " ", "content_type": "image/png", "size_bytes": 5}, 422},
		"unknown":   {map[string]any{"filename": "a", "content_type": "image/png", "size_bytes": 5, "storage_key": "x"}, 400},
		"malformed": {"{nope", 400},
	} {
		if c, b := e.call("POST", "/v1/media/upload-intents", u.token, tc.body); c != tc.want {
			t.Fatalf("%s: %d %v", name, c, b)
		}
	}
	// A signed URL binds the declared size: sending a different body is refused by storage.
	_, b := e.call("POST", "/v1/media/upload-intents", u.token, map[string]any{"filename": "a", "content_type": "image/jpeg", "size_bytes": 100})
	up := b["upload"].(map[string]any)
	req, _ := http.NewRequest("PUT", up["url"].(string), bytes.NewReader(make([]byte, 5000)))
	req.Header.Set("Content-Type", "image/jpeg")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode < 400 {
		t.Fatalf("size-mismatched PUT accepted: %v", resp)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	e := newEnv(t, nil, 1000)
	a, b := e.newUser(), e.newUser()
	id := e.upload(a, jpegBytes(200, 200), "image/jpeg")
	e.call("POST", "/v1/media/"+id+"/complete", a.token, nil)
	for _, r := range [][2]string{{"GET", ""}, {"POST", "/complete"}, {"DELETE", ""}} {
		if c, body := e.call(r[0], "/v1/media/"+id+r[1], b.token, nil); c != 404 || errCode(body) != "RESOURCE_NOT_FOUND" {
			t.Fatalf("%v: %d %v", r, c, body)
		}
	}
	if _, body := e.call("GET", "/v1/media/"+id, a.token, nil); body["status"] != "ready" {
		t.Fatal("owner lost access after foreign attempts")
	}
}

func TestDeletionLifecycle(t *testing.T) {
	e := newEnv(t, nil, 1000)
	u := e.newUser()
	id := e.upload(u, jpegBytes(200, 200), "image/jpeg")
	e.call("POST", "/v1/media/"+id+"/complete", u.token, nil)
	_, g := e.call("GET", "/v1/media/"+id, u.token, nil)
	signed := g["download_url"].(string)

	for i := 0; i < 2; i++ {
		if c, b := e.call("DELETE", "/v1/media/"+id, u.token, nil); c != 202 || b["status"] != "deletion_pending" {
			t.Fatal(c, b)
		}
	}
	if c, _ := e.call("GET", "/v1/media/"+id, u.token, nil); c != 404 {
		t.Fatal("pending-deletion asset must be inaccessible")
	}
	if c, _ := e.call("POST", "/v1/media/"+id+"/complete", u.token, nil); c != 404 {
		t.Fatal("pending-deletion asset must not be completable")
	}
	n, err := e.media.Sweep(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	if n, _ := e.media.Sweep(context.Background()); n != 0 {
		t.Fatal("second sweep repeated destructive work")
	}
	if resp, _ := http.Get(signed); resp.StatusCode == 200 {
		t.Fatal("object still downloadable after deletion")
	}
	if c, b := e.call("DELETE", "/v1/media/"+id, u.token, nil); c != 202 || b["status"] != "deleted" {
		t.Fatal(c, b)
	}
	var status string
	var deletedAt *time.Time
	e.db.QueryRow(context.Background(), `SELECT status, deleted_at FROM media_assets WHERE id=$1`, id).Scan(&status, &deletedAt)
	if status != "deleted" || deletedAt == nil {
		t.Fatalf("tombstone missing: %s", status)
	}
}

func TestAbandonedUploadsAndPendingCap(t *testing.T) {
	e := newEnv(t, func(m *config.Media) { m.MaxPendingUploads = 2; m.AbandonedUploadAfter = time.Millisecond }, 1000)
	u := e.newUser()
	id := e.upload(u, jpegBytes(100, 100), "image/jpeg")
	body := map[string]any{"filename": "a", "content_type": "image/jpeg", "size_bytes": 5}
	if c, _ := e.call("POST", "/v1/media/upload-intents", u.token, body); c != 201 {
		t.Fatal(c)
	}
	if c, b := e.call("POST", "/v1/media/upload-intents", u.token, body); c != 429 || errCode(b) != "UPLOAD_LIMIT_REACHED" {
		t.Fatal(c, b)
	}
	time.Sleep(20 * time.Millisecond)
	if n, err := e.media.Sweep(context.Background()); err != nil || n != 2 {
		t.Fatalf("sweep %d %v", n, err)
	}
	if _, err := e.st.Head(context.Background(), "uploads/"+u.id+"/"+id); err == nil {
		t.Fatal("abandoned upload object not removed")
	}
	if c, _ := e.call("POST", "/v1/media/upload-intents", u.token, body); c != 201 {
		t.Fatal("cap should free up after cleanup")
	}
}

func TestSignedURLsExpire(t *testing.T) {
	e := newEnv(t, func(m *config.Media) { m.UploadURLTTL = time.Second; m.DownloadURLTTL = time.Second }, 1000)
	u := e.newUser()
	id := e.upload(u, jpegBytes(100, 100), "image/jpeg")
	e.call("POST", "/v1/media/"+id+"/complete", u.token, nil)
	_, g := e.call("GET", "/v1/media/"+id, u.token, nil)
	_, b := e.call("POST", "/v1/media/upload-intents", u.token, map[string]any{"filename": "a", "content_type": "image/jpeg", "size_bytes": 5})
	if resp, _ := http.Get(g["download_url"].(string)); resp.StatusCode != 200 {
		t.Fatal("fresh URL should work")
	}
	time.Sleep(2500 * time.Millisecond)
	if resp, _ := http.Get(g["download_url"].(string)); resp.StatusCode == 200 {
		t.Fatal("download URL did not expire")
	}
	req, _ := http.NewRequest("PUT", b["upload"].(map[string]any)["url"].(string), strings.NewReader("12345"))
	req.Header.Set("Content-Type", "image/jpeg")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode == 200 {
		t.Fatal("upload URL did not expire")
	}
}
