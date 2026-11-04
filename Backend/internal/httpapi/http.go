package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type Pinger interface{ Ping(context.Context) error }

func Handler(db Pinger, timeout time.Duration, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var b [16]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		status := http.StatusOK
		var body any = map[string]string{"status": "ok"}
		code, message := "", ""
		switch {
		case r.URL.Path != "/health/live" && r.URL.Path != "/health/ready":
			status = 404
			code = "RESOURCE_NOT_FOUND"
			message = "Resource not found."
		case r.Method != http.MethodGet:
			status = 405
			code = "INVALID_REQUEST"
			message = "Method not allowed."
			w.Header().Set("Allow", "GET")
		case r.URL.Path == "/health/ready":
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			if err := db.Ping(ctx); err != nil {
				status = 503
				code = "SERVICE_UNAVAILABLE"
				message = "Service temporarily unavailable."
			}
		}
		if code != "" {
			body = map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": id}}
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
		log.Info("http request", "request_id", id, "status", status, "latency", time.Since(start))
	})
}

// Serve drains active requests on cancellation and force-closes after the deadline.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, timeout time.Duration) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := server.Shutdown(stop)
		if err != nil {
			_ = server.Close()
		}
		<-done
		return err
	}
}
