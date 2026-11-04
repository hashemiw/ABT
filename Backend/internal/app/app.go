package app

import (
	"abt/backend/internal/config"
	"abt/backend/internal/database"
	"abt/backend/internal/httpapi"
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
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
	if role == "worker" {
		log.Info("worker foundation ready; job processing disabled", "concurrency", c.WorkerConcurrency)
		<-ctx.Done()
	} else {
		listener, err := net.Listen("tcp", c.HTTPAddr)
		if err != nil {
			log.Error("HTTP listener failed")
			return 1
		}
		log.Info("API ready", "address", listener.Addr().String())
		if err := httpapi.Serve(ctx, listener, httpapi.Handler(db, c.DBTimeout, log), c.ShutdownTimeout); err != nil {
			log.Error("HTTP server stopped with error")
			return 1
		}
	}
	log.Info("shutdown complete")
	return 0
}
