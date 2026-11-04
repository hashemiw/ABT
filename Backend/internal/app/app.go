package app

import (
	"abt/backend/internal/config"
	"abt/backend/internal/database"
	"abt/backend/internal/httpapi"
	"abt/backend/internal/identity"
	"abt/backend/internal/media"
	"abt/backend/internal/storage"
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func Main(role string) int {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", role)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := config.Load(os.Getenv)
	if err != nil {
		log.Error("configuration rejected", "error", err)
		return 1
	}
	db, err := database.Open(ctx, c)
	if err != nil {
		log.Error("startup failed", "error", err)
		return 1
	}
	defer db.Close()
	st := storage.New(storage.Config(c.Storage))
	ids := identity.New(db, c.SessionTTL)
	mediaSvc := media.New(db, st, c.Media)
	if role == "worker" {
		log.Info("worker ready; running media cleanup only", "concurrency", c.WorkerConcurrency)
		cleanupLoop(ctx, log, c.MediaSweepInterval, ids, mediaSvc)
	} else {
		listener, err := net.Listen("tcp", c.HTTPAddr)
		if err != nil {
			log.Error("HTTP listener failed")
			return 1
		}
		log.Info("API ready", "address", listener.Addr().String())
		h := httpapi.Handler(httpapi.Deps{DB: db, DBTimeout: c.DBTimeout, Log: log, Identity: ids, Media: mediaSvc, SessionsPerMinute: c.SessionsPerMinute})
		if err := httpapi.Serve(ctx, listener, h, c.ShutdownTimeout); err != nil {
			log.Error("HTTP server stopped with error")
			return 1
		}
	}
	log.Info("shutdown complete")
	return 0
}

func cleanupLoop(ctx context.Context, log *slog.Logger, every time.Duration, ids *identity.Service, m *media.Service) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := m.Sweep(ctx); err != nil {
				log.Error("media sweep failed", "error", err)
			} else if n > 0 {
				log.Info("media sweep", "deleted", n)
			}
			if err := ids.PurgeExpired(ctx); err != nil {
				log.Error("session purge failed", "error", err)
			}
		}
	}
}
