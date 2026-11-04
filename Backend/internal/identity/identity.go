// Package identity issues and validates anonymous session credentials.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"abt/backend/internal/apperr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db  *pgxpool.Pool
	ttl time.Duration
}

func New(db *pgxpool.Pool, ttl time.Duration) *Service { return &Service{db: db, ttl: ttl} }

// Hash is the stored form of a bearer token. Tokens are 256-bit random, so a fast
// unsalted hash is sufficient; the raw token is never persisted.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type Issued struct {
	UserID    string
	Token     string
	ExpiresAt time.Time
}

func (s *Service) Issue(ctx context.Context) (Issued, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Issued{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	out := Issued{Token: token}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return out, apperr.Unavailable
	}
	defer tx.Rollback(context.Background())
	if err = tx.QueryRow(ctx, `INSERT INTO users(id) VALUES (gen_random_uuid()) RETURNING id::text`).Scan(&out.UserID); err != nil {
		return out, apperr.Unavailable
	}
	if err = tx.QueryRow(ctx, `INSERT INTO sessions(id,user_id,credential_hash,expires_at) VALUES (gen_random_uuid(),$1,$2,now()+$3::interval) RETURNING expires_at`,
		out.UserID, Hash(token), s.ttl.String()).Scan(&out.ExpiresAt); err != nil {
		return out, apperr.Unavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return out, apperr.Unavailable
	}
	out.ExpiresAt = out.ExpiresAt.UTC()
	return out, nil
}

// Authenticate resolves a bearer token to its user ID.
func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > 256 {
		return "", apperr.Unauthorized
	}
	var uid string
	err := s.db.QueryRow(ctx, `SELECT s.user_id::text FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.credential_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND u.deleted_at IS NULL`, Hash(token)).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperr.Unauthorized
	}
	if err != nil {
		return "", apperr.Unavailable
	}
	return uid, nil
}

// PurgeExpired removes sessions that expired more than a week ago.
func (s *Service) PurgeExpired(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - interval '7 days'`)
	return err
}
