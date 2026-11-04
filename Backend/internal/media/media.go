// Package media owns upload intents, validation, authorized access and the deletion lifecycle.
package media

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"abt/backend/internal/apperr"
	"abt/backend/internal/config"
	"abt/backend/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db  *pgxpool.Pool
	st  *storage.Client
	cfg config.Media
	sem chan struct{} // bounds concurrent image decodes
}

func New(db *pgxpool.Pool, st *storage.Client, cfg config.Media) *Service {
	return &Service{db: db, st: st, cfg: cfg, sem: make(chan struct{}, cfg.MaxConcurrentDecoders)}
}

// Keys are server-generated. The client may only write rawKey; the validated
// JPEG lives at storage_key, which no signed upload URL ever targets.
func rawKey(owner, id string) string   { return "uploads/" + owner + "/" + id }
func finalKey(owner, id string) string { return "media/" + owner + "/" + id + ".jpg" }

var unavailable = apperr.Unavailable

type UploadIntent struct {
	AssetID   string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

func (s *Service) CreateUploadIntent(ctx context.Context, owner, filename, contentType string, size int64) (*UploadIntent, error) {
	if n := len(strings.TrimSpace(filename)); n == 0 || len(filename) > 255 {
		return nil, apperr.New(http.StatusUnprocessableEntity, "INVALID_REQUEST", "A filename of 1-255 characters is required.")
	}
	if contentType != "image/jpeg" && contentType != "image/png" {
		return nil, apperr.New(http.StatusUnsupportedMediaType, "MEDIA_INVALID", "Only JPEG and PNG images are supported.")
	}
	if size <= 0 {
		return nil, apperr.New(http.StatusUnprocessableEntity, "INVALID_REQUEST", "size_bytes must be positive.")
	}
	if size > s.cfg.MaxUploadBytes {
		return nil, apperr.New(http.StatusRequestEntityTooLarge, "MEDIA_INVALID", "This image is too large to upload.")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, unavailable
	}
	defer tx.Rollback(context.Background())
	// Serialize per owner so the pending-upload cap cannot be raced past.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, owner); err != nil {
		return nil, unavailable
	}
	var pending int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM media_assets WHERE owner_id=$1 AND status IN ('pending_upload','validating')`, owner).Scan(&pending); err != nil {
		return nil, unavailable
	}
	if pending >= s.cfg.MaxPendingUploads {
		return nil, apperr.New(http.StatusTooManyRequests, "UPLOAD_LIMIT_REACHED", "Too many unfinished uploads. Finish or wait before starting another.")
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		return nil, unavailable
	}
	if _, err = tx.Exec(ctx, `INSERT INTO media_assets(id,owner_id,storage_key,kind,status) VALUES ($1,$2,$3,'original','pending_upload')`, id, owner, finalKey(owner, id)); err != nil {
		return nil, unavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable
	}
	url, headers, exp := s.st.PresignPut(rawKey(owner, id), contentType, size, s.cfg.UploadURLTTL)
	return &UploadIntent{AssetID: id, URL: url, Headers: headers, ExpiresAt: exp}, nil
}

type Asset struct {
	ID, Kind, Status string
	ContentType      *string
	Width, Height    *int
	DownloadURL      string
	DownloadExpires  time.Time
}

const assetCols = `id::text, kind, status, mime_type, width, height`

func scan(row pgx.Row) (*Asset, error) {
	a := &Asset{}
	err := row.Scan(&a.ID, &a.Kind, &a.Status, &a.ContentType, &a.Width, &a.Height)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound
	}
	if err != nil {
		return nil, unavailable
	}
	return a, nil
}

// load returns an owned asset. Other users' assets and pending-deletion or deleted
// assets are indistinguishable from nonexistent ones.
func (s *Service) load(ctx context.Context, owner, id string) (*Asset, error) {
	a, err := scan(s.db.QueryRow(ctx, `SELECT `+assetCols+` FROM media_assets WHERE id=$1 AND owner_id=$2 AND status NOT IN ('deletion_pending','deleted')`, id, owner))
	return a, err
}

func (s *Service) Get(ctx context.Context, owner, id string) (*Asset, error) {
	a, err := s.load(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if a.Status == "ready" {
		var key string
		if err = s.db.QueryRow(ctx, `SELECT storage_key FROM media_assets WHERE id=$1`, id).Scan(&key); err != nil {
			return nil, unavailable
		}
		a.DownloadURL, a.DownloadExpires = s.st.PresignGet(key, s.cfg.DownloadURLTTL)
	}
	return a, nil
}

// Complete validates the uploaded object and makes the asset ready. It is safe to repeat and to race:
// the asset row lock serializes concurrent completions, so later callers observe the settled result.
// The lock is held across storage I/O (bounded by request timeouts); acceptable for one short-lived row.
func (s *Service) Complete(ctx context.Context, owner, id string) (*Asset, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, unavailable
	}
	defer tx.Rollback(context.Background())
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM media_assets WHERE id=$1 AND owner_id=$2 AND status NOT IN ('deletion_pending','deleted') FOR UPDATE`, id, owner).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound
	}
	if err != nil {
		return nil, unavailable
	}
	switch status {
	case "ready":
		return scan(tx.QueryRow(ctx, `SELECT `+assetCols+` FROM media_assets WHERE id=$1`, id))
	case "rejected":
		return nil, invalid("This image was rejected. Please choose a different photo.")
	}
	size, err := s.st.Head(ctx, rawKey(owner, id))
	if errors.Is(err, storage.ErrNotFound) {
		return nil, apperr.New(http.StatusConflict, "MEDIA_NOT_READY", "Please finish uploading your image.")
	}
	if err != nil {
		return nil, unavailable
	}
	var out []byte
	var w, h int
	var reason *apperr.Error
	if size > s.cfg.MaxUploadBytes {
		reason = apperr.New(http.StatusRequestEntityTooLarge, "MEDIA_INVALID", "This image is too large.")
	} else {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-ctx.Done():
			return nil, unavailable
		}
		raw, err := s.st.Get(ctx, rawKey(owner, id), s.cfg.MaxUploadBytes)
		if err != nil {
			return nil, unavailable
		}
		out, w, h, reason = Normalize(raw, s.cfg.MaxPixels, s.cfg.MinSide)
	}
	if reason != nil {
		if err := s.st.Delete(ctx, rawKey(owner, id)); err != nil {
			return nil, unavailable
		}
		if _, err := tx.Exec(ctx, `UPDATE media_assets SET status='rejected' WHERE id=$1`, id); err != nil {
			return nil, unavailable
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, unavailable
		}
		return nil, reason
	}
	// Order matters: validated object first, then the client-writable raw object is removed, then publish.
	if err = s.st.Put(ctx, finalKey(owner, id), "image/jpeg", out); err != nil {
		return nil, unavailable
	}
	if err = s.st.Delete(ctx, rawKey(owner, id)); err != nil {
		return nil, unavailable
	}
	a, err := scan(tx.QueryRow(ctx, `UPDATE media_assets SET status='ready', mime_type='image/jpeg', size_bytes=$2, width=$3, height=$4
		WHERE id=$1 RETURNING `+assetCols, id, len(out), w, h))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable
	}
	return a, nil
}

// Delete marks an owned asset for deletion. Repeats return the current state without new work.
func (s *Service) Delete(ctx context.Context, owner, id string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, `UPDATE media_assets SET status='deletion_pending' WHERE id=$1 AND owner_id=$2 AND status NOT IN ('deletion_pending','deleted') RETURNING status`, id, owner).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(ctx, `SELECT status FROM media_assets WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apperr.NotFound
		}
	}
	if err != nil {
		return "", unavailable
	}
	return status, nil
}

// Sweep expires abandoned uploads and physically removes objects of deletion_pending assets,
// leaving a 'deleted' tombstone row (history references stay valid).
// shortcut: a permanently failing storage delete stops the sweep at that row; add per-row backoff when it matters.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	if _, err := s.db.Exec(ctx, `UPDATE media_assets SET status='deletion_pending' WHERE status IN ('pending_upload','validating') AND created_at < now() - $1::interval`, s.cfg.AbandonedUploadAfter.String()); err != nil {
		return 0, err
	}
	n := 0
	for ; n < 50; n++ {
		done, err := s.sweepOne(ctx)
		if err != nil {
			return n, err
		}
		if done {
			break
		}
	}
	return n, nil
}

func (s *Service) sweepOne(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	var id, owner, key string
	err = tx.QueryRow(ctx, `SELECT id::text, owner_id::text, storage_key FROM media_assets WHERE status='deletion_pending' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &owner, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, k := range []string{rawKey(owner, id), key} {
		if err = s.st.Delete(ctx, k); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE media_assets SET status='deleted', deleted_at=now() WHERE id=$1`, id); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
