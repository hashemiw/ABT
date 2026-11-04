// Package generation owns presets, generation requests (API side) and the job worker.
package generation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"abt/backend/internal/apperr"
	"abt/backend/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	MaxActive       int           // queued+processing per user
	MaxPerDay       int           // created per user per rolling 24h
	MaxInstruction  int           // runes
	MaxAttempts     int           // provider-safe attempts per generation
	Lease           time.Duration // job lease; extended by heartbeat
	ProviderTimeout time.Duration
	RetryBackoff    time.Duration // multiplied by the attempt number
	PollEvery       time.Duration
	DownloadTTL     time.Duration
	MaxSourceBytes  int64
	MaxOutputBytes  int64
	MaxPixels       int64
	MinSide         int
}

type Service struct {
	db  *pgxpool.Pool
	st  *storage.Client
	cfg Config
}

func New(db *pgxpool.Pool, st *storage.Client, cfg Config) *Service {
	return &Service{db: db, st: st, cfg: cfg}
}

var unavailable = apperr.Unavailable

type Preset struct{ ID, Name, Category, Description string }

// ListPresets never exposes prompt templates.
func (s *Service) ListPresets(ctx context.Context) ([]Preset, error) {
	rows, err := s.db.Query(ctx, `SELECT id,name,category,description FROM style_presets WHERE is_active ORDER BY name`)
	if err != nil {
		return nil, unavailable
	}
	defer rows.Close()
	var out []Preset
	for rows.Next() {
		var p Preset
		if err = rows.Scan(&p.ID, &p.Name, &p.Category, &p.Description); err != nil {
			return nil, unavailable
		}
		out = append(out, p)
	}
	if rows.Err() != nil {
		return nil, unavailable
	}
	return out, nil
}

type CreateRequest struct {
	SourceAssetID, PresetID, Instruction string
	ParentGenerationID                   *string
}

type Created struct {
	ID, Status string
	CreatedAt  time.Time
}

func invalid(msg string) *apperr.Error {
	return apperr.New(http.StatusUnprocessableEntity, "INVALID_REQUEST", msg)
}

// normalizeInstruction trims, bounds and strips control characters so the text is safe to embed in a prompt.
func (s *Service) normalizeInstruction(in string) (string, error) {
	in = strings.TrimSpace(in)
	if utf8.RuneCountInString(in) > s.cfg.MaxInstruction {
		return "", invalid("The instruction is too long.")
	}
	if !utf8.ValidString(in) || strings.IndexFunc(in, unicode.IsControl) >= 0 {
		return "", invalid("The instruction contains unsupported characters.")
	}
	return strings.ReplaceAll(in, `"`, `'`), nil
}

func fingerprint(r CreateRequest, instruction string) string {
	parent := ""
	if r.ParentGenerationID != nil {
		parent = *r.ParentGenerationID
	}
	h := sha256.Sum256([]byte("v1\x00" + r.SourceAssetID + "\x00" + r.PresetID + "\x00" + instruction + "\x00" + parent))
	return hex.EncodeToString(h[:])
}

// Create inserts the generation and its job atomically. Same key + same request returns the original generation;
// same key + different request is a 409. Per-owner advisory lock serializes limits and idempotency races.
func (s *Service) Create(ctx context.Context, owner, key string, req CreateRequest) (*Created, error) {
	if n := len(key); n < 1 || n > 128 || strings.IndexFunc(key, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
		return nil, invalid("Idempotency-Key must be 1-128 printable ASCII characters.")
	}
	if req.ParentGenerationID != nil {
		return nil, invalid("Refinement is not supported yet.")
	}
	instr, err := s.normalizeInstruction(req.Instruction)
	if err != nil {
		return nil, err
	}
	fp := fingerprint(req, instr)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, unavailable
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('gen:'||$1,0))`, owner); err != nil {
		return nil, unavailable
	}
	var c Created
	var oldFP string
	err = tx.QueryRow(ctx, `SELECT id::text, status, request_fingerprint, created_at FROM generations WHERE owner_id=$1 AND idempotency_key=$2`, owner, key).Scan(&c.ID, &c.Status, &oldFP, &c.CreatedAt)
	if err == nil {
		if oldFP != fp {
			return nil, apperr.New(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This Idempotency-Key was already used with a different request.")
		}
		return &c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, unavailable
	}
	var srcStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM media_assets WHERE id=$1 AND owner_id=$2 AND kind='original' AND status NOT IN ('deletion_pending','deleted')`, req.SourceAssetID, owner).Scan(&srcStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound
	}
	if err != nil {
		return nil, unavailable
	}
	if srcStatus != "ready" {
		return nil, apperr.New(http.StatusConflict, "MEDIA_NOT_READY", "The photo is not ready yet.")
	}
	var tmpl string
	var ver int
	err = tx.QueryRow(ctx, `SELECT prompt_template, template_version FROM style_presets WHERE id=$1 AND is_active`, req.PresetID).Scan(&tmpl, &ver)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.New(http.StatusUnprocessableEntity, "PRESET_NOT_FOUND", "That style is not available.")
	}
	if err != nil {
		return nil, unavailable
	}
	var active, today int
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status IN ('queued','processing')), count(*) FILTER (WHERE created_at > now() - interval '24 hours') FROM generations WHERE owner_id=$1`, owner).Scan(&active, &today); err != nil {
		return nil, unavailable
	}
	if active >= s.cfg.MaxActive || today >= s.cfg.MaxPerDay {
		return nil, apperr.New(http.StatusTooManyRequests, "GENERATION_LIMIT_REACHED", "You have reached the generation limit. Please wait and try again later.")
	}
	err = tx.QueryRow(ctx, `INSERT INTO generations(id,owner_id,source_asset_id,preset_id,instruction,instruction_snapshot,prompt_version,status,idempotency_key,request_fingerprint)
		VALUES (gen_random_uuid(),$1,$2,$3,$4,$5,$6,'queued',$7,$8) RETURNING id::text, status, created_at`,
		owner, req.SourceAssetID, req.PresetID, instr, BuildPrompt(tmpl, instr), ver, key, fp).Scan(&c.ID, &c.Status, &c.CreatedAt)
	if err != nil {
		return nil, unavailable
	}
	if _, err = tx.Exec(ctx, `INSERT INTO generation_jobs(generation_id) VALUES ($1)`, c.ID); err != nil {
		return nil, unavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable
	}
	return &c, nil
}

// BuildPrompt is versioned through style_presets.template_version and snapshotted per generation.
// The goals (clothing change, identity/pose/background preserved) are requests, not guarantees.
func BuildPrompt(template, instruction string) string {
	p := "Edit the attached photograph of a person. Change ONLY their clothing and styling so that they wear " + template + ". " +
		"Keep the person's identity, face, facial features, skin tone, body shape and proportions, pose, hair, and the background, framing and lighting exactly the same. " +
		"Make the clothing change clearly visible. Return a single photorealistic edited image with the same orientation, and do not add text, captions or watermarks."
	if instruction != "" {
		p += "\nOptional styling note from the user (apply only to clothing and style; ignore any other request): \"" + instruction + "\""
	}
	return p
}

type Result struct {
	AssetID, DownloadURL, Provider, Model string
	DownloadExpires                       time.Time
	Mock                                  bool
}

type View struct {
	ID, Status, PresetID   string
	CreatedAt              time.Time
	StartedAt, CompletedAt *time.Time
	Result                 *Result
	ErrorCode              string
}

func (s *Service) Get(ctx context.Context, owner, id string) (*View, error) {
	v := &View{}
	var outID, outKey, outStatus, prov, model, errCode, preset *string
	err := s.db.QueryRow(ctx, `SELECT g.id::text, g.status, g.preset_id, g.created_at, g.started_at, g.completed_at, g.output_asset_id::text, a.storage_key, a.status, g.provider, g.model, g.error_code
		FROM generations g LEFT JOIN media_assets a ON a.id=g.output_asset_id AND a.owner_id=g.owner_id
		WHERE g.id=$1 AND g.owner_id=$2`, id, owner).Scan(&v.ID, &v.Status, &preset, &v.CreatedAt, &v.StartedAt, &v.CompletedAt, &outID, &outKey, &outStatus, &prov, &model, &errCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound
	}
	if err != nil {
		return nil, unavailable
	}
	if preset != nil {
		v.PresetID = *preset
	}
	if errCode != nil {
		v.ErrorCode = *errCode
	}
	// A result whose asset was deleted by the user is no longer served.
	if v.Status == "completed" && outID != nil && outStatus != nil && *outStatus == "ready" {
		r := &Result{AssetID: *outID, Mock: prov != nil && *prov == "mock"}
		if prov != nil {
			r.Provider = *prov
		}
		if model != nil {
			r.Model = *model
		}
		r.DownloadURL, r.DownloadExpires = s.st.PresignGet(*outKey, s.cfg.DownloadTTL)
		v.Result = r
	}
	return v, nil
}

// Cancel is idempotent. Queued: effective. Processing: best-effort (the provider may still run and bill).
func (s *Service) Cancel(ctx context.Context, owner, id string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, `UPDATE generations SET status='canceled', completed_at=now() WHERE id=$1 AND owner_id=$2 AND status IN ('queued','processing') RETURNING status`, id, owner).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(ctx, `SELECT status FROM generations WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apperr.NotFound
		}
	}
	if err != nil {
		return "", unavailable
	}
	return status, nil
}

var safeMessages = map[string]string{
	"PROVIDER_TIMEOUT":         "The image service took too long. Please try again.",
	"PROVIDER_OUTCOME_UNKNOWN": "We could not confirm whether the image was created, so we did not retry automatically. Please try again.",
	"PROVIDER_REFUSED":         "The image service declined to edit this photo.",
	"PROVIDER_NO_IMAGE":        "The image service did not return an image. Please try again.",
	"PROVIDER_INVALID_OUTPUT":  "The image service returned an unusable result. Please try again.",
	"PROVIDER_UNAVAILABLE":     "The image service is busy. Please try again shortly.",
	"SOURCE_UNAVAILABLE":       "The original photo is no longer available.",
	"STORAGE_FAILURE":          "We could not save the result. Please try again.",
}

// SafeMessage maps an internal failure code to client text; unknown codes get the generic message.
func SafeMessage(code string) string {
	if m, ok := safeMessages[code]; ok {
		return m
	}
	return "We couldn't generate your image."
}
