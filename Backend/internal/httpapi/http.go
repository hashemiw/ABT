package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"abt/backend/internal/apperr"
	"abt/backend/internal/identity"
	"abt/backend/internal/media"
)

type Pinger interface{ Ping(context.Context) error }

type Deps struct {
	DB                Pinger
	DBTimeout         time.Duration
	Log               *slog.Logger
	Identity          *identity.Service // nil disables /v1 routes (health-only tests)
	Media             *media.Service
	SessionsPerMinute int
}

type ctxKey struct{}

// userID returns the authenticated user for the current request.
func userID(r *http.Request) string { s, _ := r.Context().Value(ctxKey{}).(string); return s }

type server struct {
	Deps
	sessionLimit *limiter
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func Handler(d Deps) http.Handler {
	s := &server{Deps: d, sessionLimit: newLimiter(d.SessionsPerMinute, time.Minute)}
	mux := http.NewServeMux()
	route := func(pattern string, auth bool, h handlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if auth {
				uid, err := s.Identity.Authenticate(r.Context(), bearer(r))
				if err != nil {
					s.fail(w, r, err)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, uid))
			}
			if err := h(w, r); err != nil {
				s.fail(w, r, err)
			}
		})
	}
	route("GET /health/live", false, func(w http.ResponseWriter, r *http.Request) error {
		return writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	route("GET /health/ready", false, func(w http.ResponseWriter, r *http.Request) error {
		ctx, cancel := context.WithTimeout(r.Context(), s.DBTimeout)
		defer cancel()
		if s.DB.Ping(ctx) != nil {
			return apperr.Unavailable
		}
		return writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	if d.Identity != nil {
		route("POST /v1/sessions", false, s.createSession)
		route("POST /v1/media/upload-intents", true, s.createUploadIntent)
		route("POST /v1/media/{id}/complete", true, s.completeMedia)
		route("GET /v1/media/{id}", true, s.getMedia)
		route("DELETE /v1/media/{id}", true, s.deleteMedia)
	}
	return s.wrap(mux)
}

// wrap adds request IDs, security headers, logging and JSON bodies for router-level 404/405.
func (s *server) wrap(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var b [16]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		rec := &statusWriter{ResponseWriter: w, status: 200}
		r = r.WithContext(context.WithValue(r.Context(), reqKey{}, id))
		if h, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(rec, r) // ServeHTTP (not h) populates path values and r.Pattern
		} else { // the mux's own 404/405 responses are plain text; replace them
			probe := httptest.NewRecorder()
			h.ServeHTTP(probe, r)
			if allow := probe.Header().Get("Allow"); allow != "" {
				rec.Header().Set("Allow", allow)
				s.fail(rec, r, apperr.New(405, "INVALID_REQUEST", "Method not allowed."))
			} else {
				s.fail(rec, r, apperr.NotFound)
			}
		}
		// r.Pattern is a route template, so identifiers and query strings stay out of logs.
		s.Log.Info("http request", "request_id", id, "method", r.Method, "route", r.Pattern, "status", rec.status, "latency", time.Since(start))
	})
}

type reqKey struct{}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }

func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := apperr.From(err)
	id, _ := r.Context().Value(reqKey{}).(string)
	if e.Status >= 500 && e != apperr.Unavailable {
		s.Log.Error("request failed", "request_id", id, "code", e.Code, "error", err)
	}
	if e.Status == 429 {
		w.Header().Set("Retry-After", "60")
	}
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": e.Code, "message": e.Message, "request_id": id}})
}

func writeJSON(w http.ResponseWriter, status int, body any) error {
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// decode reads a small, strict JSON object. An empty body is treated as {}.
func decode(r *http.Request, dst any) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 64<<10))
	if err != nil {
		return apperr.New(400, "INVALID_REQUEST", "Request body is too large or unreadable.")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil || dec.More() {
		return apperr.New(400, "INVALID_REQUEST", "Malformed JSON request.")
	}
	return nil
}

// Serve drains active requests on cancellation and force-closes after the deadline.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, timeout time.Duration) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
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
