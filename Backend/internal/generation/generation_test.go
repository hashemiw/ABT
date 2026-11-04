package generation

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abt/backend/internal/apperr"
	"abt/backend/internal/provider"
	"abt/backend/internal/storage"
	"abt/backend/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real PostgreSQL + S3-compatible storage (opt in with TEST_DATABASE_URL and TEST_STORAGE_*).
// No test here can reach a real AI vendor: only provider.Mock and local fakes are used.

type env struct {
	t   *testing.T
	db  *pgxpool.Pool
	st  *storage.Client
	cfg Config
	svc *Service
}

func testCfg() Config {
	return Config{MaxActive: 5, MaxPerDay: 50, MaxInstruction: 60, MaxAttempts: 3, Lease: 600 * time.Millisecond, ProviderTimeout: 2 * time.Second,
		RetryBackoff: 10 * time.Millisecond, PollEvery: 20 * time.Millisecond, DownloadTTL: time.Minute,
		MaxSourceBytes: 4 << 20, MaxOutputBytes: 8 << 20, MaxPixels: 4_000_000, MinSide: 64}
}

func newEnv(t *testing.T, tune func(*Config), storageEndpoint func(string) string) *env {
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
	schema := fmt.Sprintf("abt_gen_%d", time.Now().UnixNano())
	q := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(context.Background(), "DROP SCHEMA "+q+" CASCADE"); admin.Close() })
	pc, _ := pgxpool.ParseConfig(dsn)
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pc.MaxConns = 20
	db, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = migrations.Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	if storageEndpoint != nil {
		ep = storageEndpoint(ep)
	}
	st := storage.New(storage.Config{Endpoint: ep, PublicEndpoint: ep, Bucket: "abt-test", Region: "us-east-1",
		AccessKey: os.Getenv("TEST_STORAGE_ACCESS_KEY"), SecretKey: os.Getenv("TEST_STORAGE_SECRET_KEY")})
	if err = st.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := testCfg()
	if tune != nil {
		tune(&cfg)
	}
	return &env{t: t, db: db, st: st, cfg: cfg, svc: New(db, st, cfg)}
}

func (e *env) user() string {
	var id string
	if err := e.db.QueryRow(context.Background(), `INSERT INTO users(id) VALUES (gen_random_uuid()) RETURNING id::text`).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func testImage(c color.RGBA) []byte {
	im := image.NewRGBA(image.Rect(0, 0, 160, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 160; x++ {
			im.Set(x, y, color.RGBA{c.R + uint8(x), c.G + uint8(y), c.B, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, im, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

// source inserts a ready original (as media.Complete would leave it) and its stored object.
func (e *env) source(owner string, status string) (string, []byte) {
	var id string
	if err := e.db.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	key := "media/" + owner + "/" + id + ".jpg"
	data := testImage(color.RGBA{30, 60, 90, 255})
	if status == "ready" {
		if err := e.st.Put(context.Background(), key, "image/jpeg", data); err != nil {
			e.t.Fatal(err)
		}
	}
	_, err := e.db.Exec(context.Background(), `INSERT INTO media_assets(id,owner_id,storage_key,kind,status,mime_type,size_bytes,width,height,deleted_at)
		VALUES ($1,$2,$3,'original',$4,'image/jpeg',$5,160,160,CASE WHEN $4='deleted' THEN now() END)`, id, owner, key, status, len(data))
	if err != nil {
		e.t.Fatal(err)
	}
	return id, data
}

func (e *env) create(owner, src, key string) *Created {
	e.t.Helper()
	c, err := e.svc.Create(context.Background(), owner, key, CreateRequest{SourceAssetID: src, PresetID: "old_money", Instruction: "navy palette"})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

type recorder struct {
	inner provider.ImageProvider
	calls atomic.Int32
	mu    sync.Mutex
	last  provider.Input
}

func (r *recorder) Name() string { return r.inner.Name() }
func (r *recorder) Edit(ctx context.Context, in provider.Input) (*provider.Output, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.last = in
	r.mu.Unlock()
	return r.inner.Edit(ctx, in)
}

func (e *env) worker(p provider.ImageProvider, id string) *Worker {
	return NewWorker(e.db, e.st, p, e.cfg, id, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func (e *env) status(id string) (status, code string) {
	var c *string
	if err := e.db.QueryRow(context.Background(), `SELECT status, error_code FROM generations WHERE id=$1`, id).Scan(&status, &c); err != nil {
		e.t.Fatal(err)
	}
	if c != nil {
		code = *c
	}
	return
}

func (e *env) attempts(id string) (n int) {
	e.db.QueryRow(context.Background(), `SELECT attempt_count FROM generation_jobs WHERE generation_id=$1`, id).Scan(&n)
	return
}

func code(err error) string { return apperr.From(err).Code }

// ---- API-side behaviour ----

func TestCreateValidationOwnershipAndLimits(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxActive = 2 }, nil)
	ctx := context.Background()
	alice, bob := e.user(), e.user()
	src, _ := e.source(alice, "ready")
	pending, _ := e.source(alice, "pending_upload")
	gone, _ := e.source(alice, "deletion_pending")
	req := CreateRequest{SourceAssetID: src, PresetID: "old_money"}

	for name, tc := range map[string]struct {
		owner, key string
		r          CreateRequest
		want       string
	}{
		"other user's source": {bob, "k1", req, "RESOURCE_NOT_FOUND"},
		"deleted source":      {alice, "k2", CreateRequest{SourceAssetID: gone, PresetID: "old_money"}, "RESOURCE_NOT_FOUND"},
		"not ready source":    {alice, "k3", CreateRequest{SourceAssetID: pending, PresetID: "old_money"}, "MEDIA_NOT_READY"},
		"unknown preset":      {alice, "k4", CreateRequest{SourceAssetID: src, PresetID: "nope"}, "PRESET_NOT_FOUND"},
		"long instruction":    {alice, "k5", CreateRequest{SourceAssetID: src, PresetID: "old_money", Instruction: strings.Repeat("x", 61)}, "INVALID_REQUEST"},
		"control chars":       {alice, "k6", CreateRequest{SourceAssetID: src, PresetID: "old_money", Instruction: "a\x00b"}, "INVALID_REQUEST"},
		"refinement":          {alice, "k7", CreateRequest{SourceAssetID: src, PresetID: "old_money", ParentGenerationID: &src}, "INVALID_REQUEST"},
		"empty key":           {alice, "", req, "INVALID_REQUEST"},
		"key with space":      {alice, "a b", req, "INVALID_REQUEST"},
	} {
		if _, err := e.svc.Create(ctx, tc.owner, tc.key, tc.r); code(err) != tc.want {
			t.Errorf("%s: got %v want %s", name, err, tc.want)
		}
	}
	if _, err := e.svc.Create(ctx, alice, "ok1", req); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(ctx, alice, "ok2", req); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(ctx, alice, "ok3", req); code(err) != "GENERATION_LIMIT_REACHED" {
		t.Fatalf("active cap not enforced: %v", err)
	}
	// Bob has his own budget and cannot read Alice's generation.
	var id string
	e.db.QueryRow(ctx, `SELECT id::text FROM generations LIMIT 1`).Scan(&id)
	if _, err := e.svc.Get(ctx, bob, id); code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatalf("cross-user read: %v", err)
	}
	if _, err := e.svc.Cancel(ctx, bob, id); code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatalf("cross-user cancel: %v", err)
	}
}

func TestIdempotency(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	u := e.user()
	src, _ := e.source(u, "ready")
	a := e.create(u, src, "same-key")
	b, err := e.svc.Create(ctx, u, "same-key", CreateRequest{SourceAssetID: src, PresetID: "old_money", Instruction: "navy palette"})
	if err != nil || b.ID != a.ID {
		t.Fatalf("repeat must return the original: %v %v", b, err)
	}
	if _, err = e.svc.Create(ctx, u, "same-key", CreateRequest{SourceAssetID: src, PresetID: "streetwear"}); code(err) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("changed input with same key: %v", err)
	}
	// Keys are per user: another user may reuse it.
	v := e.user()
	vsrc, _ := e.source(v, "ready")
	if c := e.create(v, vsrc, "same-key"); c.ID == a.ID {
		t.Fatal("keys must be scoped per user")
	}
}

func TestConcurrentIdenticalRequestsMakeOneGenerationAndOneJob(t *testing.T) {
	e := newEnv(t, nil, nil)
	u := e.user()
	src, _ := e.source(u, "ready")
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := e.svc.Create(context.Background(), u, "race-key", CreateRequest{SourceAssetID: src, PresetID: "old_money"})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- c.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	var g, j int
	e.db.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM generations), (SELECT count(*) FROM generation_jobs)`).Scan(&g, &j)
	if len(seen) != 1 || g != 1 || j != 1 {
		t.Fatalf("distinct=%d generations=%d jobs=%d", len(seen), g, j)
	}
}

func TestGenerationAndJobAreAtomic(t *testing.T) {
	e := newEnv(t, nil, nil)
	u := e.user()
	src, _ := e.source(u, "ready")
	// A pre-existing job row for the next id is impossible to forge, so force the second INSERT to fail instead:
	// dropping the jobs table makes the transaction fail after the generation INSERT.
	if _, err := e.db.Exec(context.Background(), `ALTER TABLE generation_jobs RENAME TO generation_jobs_x`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(context.Background(), u, "atomic", CreateRequest{SourceAssetID: src, PresetID: "old_money"}); err == nil {
		t.Fatal("expected failure")
	}
	var n int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM generations`).Scan(&n)
	if n != 0 {
		t.Fatalf("generation survived a failed job insert: %d", n)
	}
}

func TestPresetsHidePromptTemplates(t *testing.T) {
	e := newEnv(t, nil, nil)
	ps, err := e.svc.ListPresets(context.Background())
	if err != nil || len(ps) != 5 {
		t.Fatalf("%v %d", err, len(ps))
	}
	e.db.Exec(context.Background(), `UPDATE style_presets SET is_active=false WHERE id='streetwear'`)
	ps, _ = e.svc.ListPresets(context.Background())
	if len(ps) != 4 {
		t.Fatal("inactive preset listed")
	}
	u := e.user()
	src, _ := e.source(u, "ready")
	if _, err := e.svc.Create(context.Background(), u, "k", CreateRequest{SourceAssetID: src, PresetID: "streetwear"}); code(err) != "PRESET_NOT_FOUND" {
		t.Fatalf("inactive preset selectable: %v", err)
	}
}

// ---- Worker behaviour (mock provider only) ----

func TestWorkerSuccessStoresSeparateResultAndPassesCorrectInput(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	u := e.user()
	src, srcBytes := e.source(u, "ready")
	g := e.create(u, src, "k")
	rec := &recorder{inner: provider.NewMock("")}
	if did, err := e.worker(rec, "w1").RunOnce(ctx); !did || err != nil {
		t.Fatal(did, err)
	}
	if rec.calls.Load() != 1 || !bytes.Equal(rec.last.Image, srcBytes) || rec.last.MIME != "image/jpeg" {
		t.Fatal("provider did not receive the exact stored source bytes and MIME")
	}
	if !strings.Contains(rec.last.Prompt, "tailored cashmere") && !strings.Contains(rec.last.Prompt, "old money") || !strings.Contains(rec.last.Prompt, "navy palette") ||
		!strings.Contains(rec.last.Prompt, "identity") {
		t.Fatalf("prompt missing preset/instruction/preservation goals: %q", rec.last.Prompt)
	}
	v, err := e.svc.Get(ctx, u, g.ID)
	if err != nil || v.Status != "completed" || v.Result == nil || !v.Result.Mock || v.Result.Provider != "mock" {
		t.Fatalf("%v %+v", err, v)
	}
	var srcKey, outKey string
	e.db.QueryRow(ctx, `SELECT storage_key FROM media_assets WHERE id=$1`, src).Scan(&srcKey)
	e.db.QueryRow(ctx, `SELECT storage_key FROM media_assets WHERE id=$1`, v.Result.AssetID).Scan(&outKey)
	if srcKey == outKey {
		t.Fatal("result must not overwrite the source")
	}
	out, err := e.st.Get(ctx, outKey, 8<<20)
	if err != nil || bytes.Equal(out, srcBytes) {
		t.Fatalf("result missing or identical to source: %v", err)
	}
	still, _ := e.st.Get(ctx, srcKey, 8<<20)
	if !bytes.Equal(still, srcBytes) {
		t.Fatal("source object changed")
	}
	var lease *string
	e.db.QueryRow(ctx, `SELECT lease_owner FROM generation_jobs WHERE generation_id=$1`, g.ID).Scan(&lease)
	if lease != nil {
		t.Fatal("lease not released")
	}
	// Result ownership: another user cannot see it through the generation.
	if _, err := e.svc.Get(ctx, e.user(), g.ID); code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatal("cross-user result access")
	}
	// Deleting the result asset hides the result but keeps the record.
	e.db.Exec(ctx, `UPDATE media_assets SET status='deletion_pending' WHERE id=$1`, v.Result.AssetID)
	v, _ = e.svc.Get(ctx, u, g.ID)
	if v.Result != nil {
		t.Fatal("deleted result still served")
	}
}

func TestWorkerFailureModes(t *testing.T) {
	cases := []struct {
		mode, code string
		calls      int32
		attempts   int
	}{
		{provider.ModeFailure, "PROVIDER_FAILURE", 1, 1},
		{provider.ModeTimeout, "PROVIDER_TIMEOUT", 1, 1}, // uncertain: never retried
		{provider.ModeBadOut, "PROVIDER_INVALID_OUTPUT", 1, 1},
		{provider.ModeIdentical, "PROVIDER_INVALID_OUTPUT", 1, 1}, // original must not become the result
		{provider.ModeBusy, "PROVIDER_UNAVAILABLE", 3, 3},         // definitely-not-executed: bounded retries
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			e := newEnv(t, func(cfg *Config) { cfg.ProviderTimeout = 150 * time.Millisecond }, nil)
			u := e.user()
			src, _ := e.source(u, "ready")
			g := e.create(u, src, "k")
			rec := &recorder{inner: provider.NewMock(c.mode)}
			w := e.worker(rec, "w")
			for i := 0; i < 6; i++ {
				w.RunOnce(context.Background())
				time.Sleep(40 * time.Millisecond) // let backoff elapse
			}
			st, code := e.status(g.ID)
			if st != "failed" || code != c.code || rec.calls.Load() != c.calls || e.attempts(g.ID) != c.attempts {
				t.Fatalf("status=%s code=%s calls=%d attempts=%d", st, code, rec.calls.Load(), e.attempts(g.ID))
			}
			var outputs int
			e.db.QueryRow(context.Background(), `SELECT count(*) FROM media_assets WHERE kind='result'`).Scan(&outputs)
			if outputs != 0 {
				t.Fatal("failed generation produced a result asset")
			}
			v, _ := e.svc.Get(context.Background(), u, g.ID)
			if v.Result != nil {
				t.Fatal("failed generation exposes a result")
			}
		})
	}
}

func TestFencingStaleWorkerCannotOverwrite(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	u := e.user()
	src, _ := e.source(u, "ready")
	g := e.create(u, src, "k")
	a, err := e.worker(provider.NewMock(""), "A").claim(ctx)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if b, _ := e.worker(provider.NewMock(""), "B").claim(ctx); b != nil {
		t.Fatal("a leased job was claimed twice")
	}
	e.db.Exec(ctx, `UPDATE generation_jobs SET lease_expires_at=now()-interval '1 second' WHERE generation_id=$1`, g.ID) // A "crashed"
	b, err := e.worker(provider.NewMock(""), "B").claim(ctx)
	if err != nil || b == nil || b.token == a.token || b.attempt != 2 {
		t.Fatalf("expired lease not reclaimed with a new token: %+v %v", b, err)
	}
	wa := e.worker(provider.NewMock(""), "A")
	out := &provider.Output{Image: testImage(color.RGBA{1, 1, 1, 255}), Provider: "mock", Model: "m"}
	if err := wa.complete(ctx, a, "results/x/old.jpg", out, out.Image, 160, 160, time.Second); err != errLost {
		t.Fatalf("stale completion must be fenced out: %v", err)
	}
	wa.finish(ctx, a, "PROVIDER_FAILURE", false) // stale failure must be a no-op
	if st, _ := e.status(g.ID); st != "processing" {
		t.Fatalf("stale worker changed state to %s", st)
	}
	if err := e.worker(provider.NewMock(""), "B").complete(ctx, b, "results/x/new.jpg", out, out.Image, 160, 160, time.Second); err != nil {
		t.Fatalf("current owner must complete: %v", err)
	}
	if st, _ := e.status(g.ID); st != "completed" {
		t.Fatal(st)
	}
}

func TestReclaimAfterPossibleProviderCallDoesNotCallAgain(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	u := e.user()
	src, _ := e.source(u, "ready")
	g := e.create(u, src, "k")
	a, _ := e.worker(provider.NewMock(""), "A").claim(ctx)
	// A recorded the provider call and then died.
	e.db.Exec(ctx, `UPDATE generation_jobs SET provider_call_started_at=now(), lease_expires_at=now()-interval '1 second' WHERE generation_id=$1 AND claim_token=$2::uuid`, g.ID, a.token)
	rec := &recorder{inner: provider.NewMock("")}
	if did, err := e.worker(rec, "B").RunOnce(ctx); !did || err != nil {
		t.Fatal(did, err)
	}
	st, code := e.status(g.ID)
	if st != "failed" || code != "PROVIDER_OUTCOME_UNKNOWN" || rec.calls.Load() != 0 {
		t.Fatalf("status=%s code=%s calls=%d", st, code, rec.calls.Load())
	}
}

func TestCrashBeforeProviderCallIsRecovered(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	u := e.user()
	src, _ := e.source(u, "ready")
	g := e.create(u, src, "k")
	e.worker(provider.NewMock(""), "A").claim(ctx)
	e.db.Exec(ctx, `UPDATE generation_jobs SET lease_expires_at=now()-interval '1 second' WHERE generation_id=$1`, g.ID)
	rec := &recorder{inner: provider.NewMock("")}
	e.worker(rec, "B").RunOnce(ctx)
	if st, _ := e.status(g.ID); st != "completed" || rec.calls.Load() != 1 {
		t.Fatalf("job not recovered: %s calls=%d", st, rec.calls.Load())
	}
}

func TestConcurrentWorkersProcessEachJobOnce(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxActive = 10 }, nil)
	u := e.user()
	src, _ := e.source(u, "ready")
	for i := 0; i < 6; i++ {
		e.create(u, src, fmt.Sprintf("k%d", i))
	}
	rec := &recorder{inner: provider.NewMock("")}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := e.worker(rec, fmt.Sprintf("w%d", i))
			for n := 0; n < 4; n++ {
				w.RunOnce(context.Background())
			}
		}(i)
	}
	wg.Wait()
	var done int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM generations WHERE status='completed'`).Scan(&done)
	if done != 6 || rec.calls.Load() != 6 {
		t.Fatalf("completed=%d provider calls=%d (want 6/6)", done, rec.calls.Load())
	}
}

func TestCancelQueuedAndProcessing(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	u := e.user()
	src, _ := e.source(u, "ready")
	q := e.create(u, src, "q")
	if st, err := e.svc.Cancel(ctx, u, q.ID); err != nil || st != "canceled" {
		t.Fatal(st, err)
	}
	if st, _ := e.svc.Cancel(ctx, u, q.ID); st != "canceled" { // repeat is safe
		t.Fatal(st)
	}
	rec := &recorder{inner: provider.NewMock("")}
	if did, _ := e.worker(rec, "w").RunOnce(ctx); did || rec.calls.Load() != 0 {
		t.Fatal("canceled queued job was executed")
	}
	// Cancel while the provider call is in flight: result must be discarded.
	p := e.create(u, src, "p")
	slow := &recorder{inner: &provider.Mock{Mode: provider.ModeSuccess, Delay: 800 * time.Millisecond}}
	done := make(chan struct{})
	go func() { e.worker(slow, "w").RunOnce(ctx); close(done) }()
	time.Sleep(150 * time.Millisecond)
	if st, _ := e.svc.Cancel(ctx, u, p.ID); st != "canceled" {
		t.Fatal(st)
	}
	<-done
	var results int
	e.db.QueryRow(ctx, `SELECT count(*) FROM media_assets WHERE kind='result'`).Scan(&results)
	if st, _ := e.status(p.ID); st != "canceled" || results != 0 {
		t.Fatalf("canceled generation finished anyway: %s results=%d", st, results)
	}
}

func TestSourceDeletedBeforeRun(t *testing.T) {
	e := newEnv(t, nil, nil)
	u := e.user()
	src, _ := e.source(u, "ready")
	g := e.create(u, src, "k")
	e.db.Exec(context.Background(), `UPDATE media_assets SET status='deletion_pending' WHERE id=$1`, src)
	rec := &recorder{inner: provider.NewMock("")}
	e.worker(rec, "w").RunOnce(context.Background())
	if st, code := e.status(g.ID); st != "failed" || code != "SOURCE_UNAVAILABLE" || rec.calls.Load() != 0 {
		t.Fatalf("%s %s calls=%d", st, code, rec.calls.Load())
	}
}

// A storage that rejects result writes: the provider result is lost, and it must not be retried (second charge).
func TestResultStorageFailureIsNotRetried(t *testing.T) {
	var proxy *httptest.Server
	e := newEnv(t, nil, func(real string) string {
		target, _ := url.Parse(real)
		rp := httputil.NewSingleHostReverseProxy(target) // keeps the incoming Host, so SigV4 still verifies
		proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/results/") {
				http.Error(w, "boom", 500)
				return
			}
			rp.ServeHTTP(w, r)
		}))
		return proxy.URL
	})
	defer proxy.Close()
	u := e.user()
	src, _ := e.source(u, "ready")
	g := e.create(u, src, "k")
	rec := &recorder{inner: provider.NewMock("")}
	w := e.worker(rec, "w")
	for i := 0; i < 3; i++ {
		w.RunOnce(context.Background())
		time.Sleep(30 * time.Millisecond)
	}
	if st, code := e.status(g.ID); st != "failed" || code != "STORAGE_FAILURE" || rec.calls.Load() != 1 {
		t.Fatalf("%s %s calls=%d", st, code, rec.calls.Load())
	}
}
