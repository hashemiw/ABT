package main

import (
	"abt/backend/internal/config"
	"abt/backend/internal/database"
	"abt/backend/internal/storage"
	"abt/backend/migrations"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, time.Minute)
	defer cancel()
	c, err := config.Load(os.Getenv)
	if err != nil {
		log.Error("configuration rejected", "error", err)
		return 1
	}
	db, err := database.Open(ctx, c)
	if err != nil {
		log.Error("database unavailable")
		return 1
	}
	defer db.Close()
	if err = migrations.Up(ctx, db); err != nil {
		log.Error("migration failed; inspect schema and migration checksums")
		return 1
	}
	log.Info("migrations applied")
	if os.Getenv("STORAGE_ENSURE_BUCKET") == "true" {
		if err = storage.New(storage.Config(c.Storage)).EnsureBucket(ctx); err != nil {
			log.Error("storage bucket setup failed", "error", err)
			return 1
		}
		log.Info("storage bucket ready")
	}
	return 0
}
