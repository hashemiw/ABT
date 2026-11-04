package generation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"abt/backend/internal/media"
	"abt/backend/internal/provider"
	"abt/backend/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker executes generation jobs.
//
// Protocol: a job is claimed with FOR UPDATE SKIP LOCKED and gets a lease, a fresh claim_token and attempt_count+1.
// Every later write (provider-start mark, heartbeat, completion, failure, retry) is fenced by
// (claim_token AND lease_expires_at > now()), so a stale worker can never overwrite a newer claim.
// The provider is called with no transaction or lock open.
//
// Cost policy (uncertain outcomes): provider_call_started_at is written before the call. If a job is reclaimed
// while that mark is set, the earlier call may have run (and billed): the job fails with PROVIDER_OUTCOME_UNKNOWN
// and is NOT retried. Timeouts and transport errors after sending are also uncertain and never auto-retried.
// Only failures where the vendor provably did not execute (e.g. HTTP 429/503) or that happen before the call
// (source read) are retried, up to MaxAttempts, with linear backoff. A failure to store an already-produced result
// is not retried either, because retrying would pay for a second generation.
type Worker struct {
	db   *pgxpool.Pool
	st   *storage.Client
	prov provider.ImageProvider
	cfg  Config
	id   string
	log  *slog.Logger
}

func NewWorker(db *pgxpool.Pool, st *storage.Client, prov provider.ImageProvider, cfg Config, id string, log *slog.Logger) *Worker {
	return &Worker{db: db, st: st, prov: prov, cfg: cfg, id: id, log: log}
}

func (w *Worker) Run(ctx context.Context, concurrency int) {
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				did, err := w.RunOnce(ctx)
				if err != nil {
					w.log.Error("generation worker error", "error", err)
				}
				if !did || err != nil {
					select {
					case <-ctx.Done():
					case <-time.After(w.cfg.PollEvery):
					}
				}
			}
		}()
	}
	wg.Wait()
}

type claim struct {
	genID, owner, sourceID, prompt, token string
	attempt                               int
	providerStarted                       bool
}

// RunOnce claims and processes at most one job. It reports whether a job was claimed.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	c, err := w.claim(ctx)
	if err != nil || c == nil {
		return false, err
	}
	w.process(ctx, c)
	return true, nil
}

func (w *Worker) claim(ctx context.Context) (*claim, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	c := &claim{}
	err = tx.QueryRow(ctx, `SELECT j.generation_id::text, j.provider_call_started_at IS NOT NULL
		FROM generation_jobs j JOIN generations g ON g.id=j.generation_id
		WHERE g.status IN ('queued','processing') AND j.available_at <= now() AND (j.lease_owner IS NULL OR j.lease_expires_at < now())
		ORDER BY j.available_at, j.generation_id LIMIT 1 FOR UPDATE OF j SKIP LOCKED`).Scan(&c.genID, &c.providerStarted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The generation row is updated first and conditionally: a concurrent cancel wins cleanly.
	err = tx.QueryRow(ctx, `UPDATE generations SET status='processing', started_at=COALESCE(started_at, now())
		WHERE id=$1 AND status IN ('queued','processing') RETURNING owner_id::text, source_asset_id::text, instruction_snapshot`, c.genID).Scan(&c.owner, &c.sourceID, &c.prompt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `UPDATE generation_jobs SET lease_owner=$2, lease_expires_at=now()+make_interval(secs => $3::float8), claim_token=gen_random_uuid(),
		attempt_count=attempt_count+1, updated_at=now() WHERE generation_id=$1 RETURNING claim_token::text, attempt_count`, c.genID, w.id, w.cfg.Lease.Seconds()).Scan(&c.token, &c.attempt)
	if err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}

// fenced is the WHERE clause every claimed-job write shares.
const fenced = `generation_id=$1 AND claim_token=$2::uuid AND lease_expires_at > now()`

var (
	errLost   = errors.New("claim lost")
	errBudget = errors.New("provider call budget exhausted")
)

// startCall atomically (a) re-checks the lease, (b) enforces the call budget under a global advisory lock,
// (c) inserts the ledger row and (d) sets provider_call_started_at. Nothing is called if any step fails.
func (w *Worker) startCall(ctx context.Context, c *claim) (int64, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	if w.cfg.RealCallCap > 0 {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('provider-budget:'||$1,0))`, w.prov.Name()); err != nil {
			return 0, err
		}
		var used int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM provider_calls WHERE provider=$1`, w.prov.Name()).Scan(&used); err != nil {
			return 0, err
		}
		if used >= w.cfg.RealCallCap {
			return 0, errBudget
		}
	}
	n, err := tx.Exec(ctx, `UPDATE generation_jobs SET provider_call_started_at=now(), updated_at=now() WHERE `+fenced, c.genID, c.token)
	if err != nil || n.RowsAffected() != 1 {
		return 0, errLost
	}
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO provider_calls(generation_id, provider, model) VALUES ($1,$2,$3) RETURNING id`, c.genID, w.prov.Name(), w.cfg.ProviderModel).Scan(&id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// endCall records the outcome; if it never runs (crash) the row stays 'unknown' and still counts.
func (w *Worker) endCall(ctx context.Context, id int64, outcome, code string, latency time.Duration, usage map[string]int) {
	u, _ := json.Marshal(usage)
	_, _ = w.db.Exec(context.WithoutCancel(ctx), `UPDATE provider_calls SET outcome=$2, error_code=NULLIF($3,''), latency_ms=$4, usage=$5 WHERE id=$1`, id, outcome, code, latency.Milliseconds(), u)
}

func (w *Worker) process(ctx context.Context, c *claim) {
	log := w.log.With("generation", c.genID, "attempt", c.attempt)
	if c.providerStarted {
		log.Warn("reclaimed after a possibly-executed provider call; not retrying")
		w.finish(ctx, c, "PROVIDER_OUTCOME_UNKNOWN", false)
		return
	}
	if c.attempt > w.cfg.MaxAttempts {
		w.finish(ctx, c, "PROVIDER_UNAVAILABLE", false)
		return
	}
	var status, key string
	var mime *string
	err := w.db.QueryRow(ctx, `SELECT status, storage_key, mime_type FROM media_assets WHERE id=$1 AND owner_id=$2`, c.sourceID, c.owner).Scan(&status, &key, &mime)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "ready") {
		w.finish(ctx, c, "SOURCE_UNAVAILABLE", false)
		return
	}
	if err != nil {
		w.finish(ctx, c, "STORAGE_FAILURE", true)
		return
	}
	src, err := w.st.Get(ctx, key, w.cfg.MaxSourceBytes)
	if errors.Is(err, storage.ErrNotFound) {
		w.finish(ctx, c, "SOURCE_UNAVAILABLE", false)
		return
	}
	if err != nil {
		w.finish(ctx, c, "STORAGE_FAILURE", true)
		return
	}
	in := provider.Input{Image: src, MIME: "image/jpeg", Prompt: c.prompt, RequestID: c.genID}
	if mime != nil {
		in.MIME = *mime
	}
	// Record the intent to call BEFORE calling (fenced, one transaction) and enforce the persistent call budget.
	callID, berr := w.startCall(ctx, c)
	if errors.Is(berr, errBudget) {
		log.Warn("real-call budget exhausted; provider not called")
		w.finish(ctx, c, "AI_BUDGET_EXHAUSTED", false)
		return
	}
	if berr != nil {
		log.Warn("claim lost or ledger unavailable before provider call; provider not called")
		return
	}
	if w.cfg.MaxProviderSide > 0 {
		if small, derr := provider.DownscaleJPEG(src, w.cfg.MaxProviderSide); derr == nil {
			in.Image, in.MIME = small, "image/jpeg"
		}
	}
	pctx, cancel := context.WithTimeout(ctx, w.cfg.ProviderTimeout)
	var lost bool
	var mu sync.Mutex
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		w.heartbeat(pctx, c, func() { mu.Lock(); lost = true; mu.Unlock(); cancel() })
	}()
	start := time.Now()
	out, perr := w.prov.Edit(pctx, in)
	cancel()
	<-hbDone
	mu.Lock()
	wasLost := lost
	mu.Unlock()
	if ctx.Err() != nil || wasLost {
		// Shutdown, cancellation or lost lease: do not finalize. A reclaim will see provider_call_started_at and fail safely.
		log.Warn("provider call interrupted; leaving job for lease expiry/cancel", "lost", wasLost)
		return
	}
	if perr != nil {
		pe := provider.AsError(perr)
		w.endCall(ctx, callID, "failed", pe.Code+": "+pe.Msg, time.Since(start), nil)
		log.Warn("provider call failed", "code", pe.Code, "kind", int(pe.Kind), "latency_ms", time.Since(start).Milliseconds(), "detail", pe.Msg)
		w.finish(ctx, c, pe.Code, pe.Kind == provider.NotExecuted)
		return
	}
	w.endCall(ctx, callID, "succeeded", "", time.Since(start), out.Usage)
	if _, verr := provider.ValidateOutput(out.Image, src, w.cfg.MaxOutputBytes, w.cfg.MaxPixels); verr != nil {
		w.finish(ctx, c, provider.AsError(verr).Code, false)
		return
	}
	jpg, wd, ht, nerr := media.Normalize(out.Image, w.cfg.MaxPixels, w.cfg.MinSide)
	if nerr != nil {
		w.finish(ctx, c, "PROVIDER_INVALID_OUTPUT", false)
		return
	}
	// Key includes the claim token: a stale worker can only ever delete its own object.
	rkey := "results/" + c.owner + "/" + c.genID + "-" + c.token + ".jpg"
	if err := w.st.Put(ctx, rkey, "image/jpeg", jpg); err != nil {
		w.finish(ctx, c, "STORAGE_FAILURE", false)
		return
	}
	latency := out.Latency
	if latency == 0 {
		latency = time.Since(start)
	}
	if err := w.complete(ctx, c, rkey, out, jpg, wd, ht, latency); err != nil {
		// shortcut: a crash between Put and commit leaves an unreferenced object (no List API yet); add a prefix sweeper.
		if derr := w.st.Delete(context.WithoutCancel(ctx), rkey); derr != nil {
			log.Warn("orphan result object not removed", "error", derr)
		}
		if errors.Is(err, errLost) {
			log.Warn("claim lost before completion; result discarded")
			return
		}
		log.Error("completion failed", "error", err)
		w.finish(ctx, c, "STORAGE_FAILURE", false)
		return
	}
	log.Info("generation completed", "provider", out.Provider, "mock", out.Mock, "latency_ms", latency.Milliseconds())
}

func (w *Worker) heartbeat(ctx context.Context, c *claim, onLost func()) {
	t := time.NewTicker(w.cfg.Lease / 3)
	defer t.Stop()
	lastOK := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := w.db.Exec(ctx, `UPDATE generation_jobs j SET lease_expires_at=now()+make_interval(secs => $3::float8), updated_at=now()
				FROM generations g WHERE j.generation_id=$1 AND j.claim_token=$2::uuid AND j.lease_expires_at > now() AND g.id=j.generation_id AND g.status='processing'`,
				c.genID, c.token, w.cfg.Lease.Seconds())
			switch {
			case err == nil && n.RowsAffected() == 1:
				lastOK = time.Now()
			case err == nil: // canceled or fenced out
				onLost()
				return
			case time.Since(lastOK) > w.cfg.Lease: // cannot prove we still hold the lease
				onLost()
				return
			}
		}
	}
}

func (w *Worker) complete(ctx context.Context, c *claim, rkey string, out *provider.Output, jpg []byte, wd, ht int, latency time.Duration) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var one int
	err = tx.QueryRow(ctx, `SELECT 1 FROM generation_jobs j JOIN generations g ON g.id=j.generation_id
		WHERE j.generation_id=$1 AND j.claim_token=$2::uuid AND j.lease_expires_at > now() AND g.status='processing' FOR UPDATE OF j, g`, c.genID, c.token).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return errLost
	}
	if err != nil {
		return err
	}
	var assetID string
	if err = tx.QueryRow(ctx, `INSERT INTO media_assets(id,owner_id,storage_key,kind,status,mime_type,size_bytes,width,height)
		VALUES (gen_random_uuid(),$1,$2,'result','ready','image/jpeg',$3,$4,$5) RETURNING id::text`, c.owner, rkey, len(jpg), wd, ht).Scan(&assetID); err != nil {
		return err
	}
	usage, _ := json.Marshal(out.Usage) // counts only; never request content
	if _, err = tx.Exec(ctx, `UPDATE generations SET status='completed', output_asset_id=$2, provider=$3, model=$4, completed_at=now(), provider_latency_ms=$5, usage=$6 WHERE id=$1`,
		c.genID, assetID, out.Provider, out.Model, latency.Milliseconds(), usage); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE generation_jobs SET lease_owner=NULL, lease_expires_at=NULL, claim_token=NULL, last_error_code=NULL, updated_at=now() WHERE generation_id=$1`, c.genID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// finish records a failure fenced by the claim. retryable failures go back to the queue while attempts remain.
func (w *Worker) finish(ctx context.Context, c *claim, code string, retryable bool) {
	ctx = context.WithoutCancel(ctx)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(context.Background())
	if retryable && c.attempt < w.cfg.MaxAttempts {
		n, err := tx.Exec(ctx, `UPDATE generation_jobs SET lease_owner=NULL, lease_expires_at=NULL, claim_token=NULL, provider_call_started_at=NULL, last_error_code=$3,
			available_at=now()+make_interval(secs => $4::float8), updated_at=now() WHERE `+fenced, c.genID, c.token, code, (w.cfg.RetryBackoff * time.Duration(c.attempt)).Seconds())
		if err != nil || n.RowsAffected() != 1 {
			return
		}
		if _, err = tx.Exec(ctx, `UPDATE generations SET status='queued' WHERE id=$1 AND status='processing'`, c.genID); err != nil {
			return
		}
		_ = tx.Commit(ctx)
		return
	}
	n, err := tx.Exec(ctx, `UPDATE generation_jobs SET lease_owner=NULL, lease_expires_at=NULL, claim_token=NULL, last_error_code=$3, updated_at=now() WHERE `+fenced, c.genID, c.token, code)
	if err != nil || n.RowsAffected() != 1 {
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE generations SET status='failed', error_code=$2, completed_at=now() WHERE id=$1 AND status='processing'`, c.genID, code); err != nil {
		return
	}
	_ = tx.Commit(ctx)
}
