package migrations

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// Opt-in real PostgreSQL test. Each run owns an isolated schema, never public.
func TestPostgresMigrations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run real PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("abt_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Concurrent runners must serialize and record one version.
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- Up(ctx, pool) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 2 {
		t.Fatalf("versions=%d error=%v", count, err)
	}
	for _, table := range []string{"users", "sessions", "media_assets", "style_presets", "generations", "generation_jobs", "feedback"} {
		if _, err = pool.Exec(ctx, "SELECT 1 FROM "+table+" LIMIT 0"); err != nil {
			t.Fatal(err)
		}
	}
	const u1 = "00000000-0000-0000-0000-000000000001"
	const u2 = "00000000-0000-0000-0000-000000000002"
	const a1 = "10000000-0000-0000-0000-000000000001"
	const a2 = "10000000-0000-0000-0000-000000000002"
	const g1 = "20000000-0000-0000-0000-000000000001"
	must := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	must("INSERT INTO users(id) VALUES($1),($2)", u1, u2)
	must("INSERT INTO media_assets(id,owner_id,storage_key,kind,status) VALUES($1,$2,'one','original','ready'),($3,$4,'two','original','ready')", a1, u1, a2, u2)
	must("INSERT INTO generations(id,owner_id,source_asset_id,instruction_snapshot,prompt_version,status,idempotency_key,request_fingerprint) VALUES($1,$2,$3,'snapshot',1,'queued','key','hash')", g1, u1, a1)
	must("INSERT INTO generation_jobs(generation_id) VALUES($1)", g1)
	for _, tc := range []struct {
		name, sql, code string
		args            []any
	}{
		{"owner", "UPDATE generations SET source_asset_id=$1 WHERE id=$2", "23503", []any{a2, g1}},
		{"output owner", "UPDATE generations SET output_asset_id=$1 WHERE id=$2", "23503", []any{a2, g1}},
		{"completed output", "UPDATE generations SET status='completed' WHERE id=$1", "23514", []any{g1}},
		{"failed error", "UPDATE generations SET status='failed' WHERE id=$1", "23514", []any{g1}},
		{"self parent", "UPDATE generations SET parent_generation_id=id WHERE id=$1", "23514", []any{g1}},
		{"duplicate key", "INSERT INTO generations SELECT '20000000-0000-0000-0000-000000000002'::uuid,owner_id,source_asset_id,parent_generation_id,preset_id,instruction,instruction_snapshot,prompt_version,status,output_asset_id,provider,model,idempotency_key,request_fingerprint,error_code,created_at,started_at,completed_at FROM generations WHERE id=$1", "23505", []any{g1}},
		{"duplicate job", "INSERT INTO generation_jobs(generation_id) VALUES($1)", "23505", []any{g1}},
		{"incomplete lease", "UPDATE generation_jobs SET lease_owner='worker' WHERE generation_id=$1", "23514", []any{g1}},
		{"attempt count", "UPDATE generation_jobs SET attempt_count=-1 WHERE generation_id=$1", "23514", []any{g1}},
		{"media dimensions", "UPDATE media_assets SET width=0 WHERE id=$1", "23514", []any{a1}},
		{"referenced deletion", "DELETE FROM media_assets WHERE id=$1", "23503", []any{a1}},
		{"feedback owner", "INSERT INTO feedback(id,owner_id,generation_id,rating) VALUES('30000000-0000-0000-0000-000000000001',$1,$2,4)", "23503", []any{u2, g1}},
		{"rating", "INSERT INTO feedback(id,owner_id,generation_id,rating) VALUES('30000000-0000-0000-0000-000000000001',$1,$2,6)", "23514", []any{u1, g1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tc.sql, tc.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("want SQLSTATE %s, got %v", tc.code, err)
			}
		})
	}
	must("UPDATE media_assets SET status='deletion_pending' WHERE id=$1", a1)
	must("UPDATE media_assets SET status='deleted',deleted_at=now() WHERE id=$1", a1)
	must("UPDATE generation_jobs SET lease_owner='worker',lease_expires_at=now()+interval '1 minute',claim_token='40000000-0000-0000-0000-000000000001',attempt_count=1 WHERE generation_id=$1", g1)
	must("UPDATE schema_migrations SET checksum='tampered'")
	if err := Up(ctx, pool); err == nil {
		t.Fatal("checksum tampering accepted")
	}
	// Down SQL is destructive and used only inside this disposable schema.
	down, err := files.ReadFile("000001_initial.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	must(string(down))
	must("DELETE FROM schema_migrations")
	if err := Up(ctx, pool); err != nil {
		t.Fatalf("reapply: %v", err)
	}
}
