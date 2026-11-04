package database

import (
	"abt/backend/internal/config"
	"context"
	"os"
	"testing"
	"time"
)

func TestRejectInvalidDriverConfig(t *testing.T) {
	_, err := Open(context.Background(), config.Config{DatabaseURL: "postgres://user:secret@localhost/db?connect_timeout=not-an-int", DBTimeout: time.Second, DBMaxConns: 1})
	if err == nil || err.Error() != "invalid database connection configuration" {
		t.Fatalf("unsafe/unexpected error: %v", err)
	}
}
func TestPostgresPool(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL pool tests")
	}
	c := config.Config{DatabaseURL: dsn, DBMaxConns: 2, DBTimeout: time.Second}
	p, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Config().MaxConns != 2 {
		t.Fatal("pool limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(ctx, c); err == nil {
		t.Fatal("canceled startup succeeded")
	}
	if err := p.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
