package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(c context.Context) error { return f(c) }
func TestHealth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name, path, method string
		fail               bool
		want               int
	}{{"live", "/health/live", "GET", true, 200}, {"ready", "/health/ready", "GET", false, 200}, {"unready", "/health/ready", "GET", true, 503}, {"method", "/health/live", "POST", false, 405}, {"missing", "/unknown", "GET", false, 404}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := Handler(pingFunc(func(ctx context.Context) error {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Error("missing deadline")
				}
				if tc.fail {
					return errors.New("secret database error")
				}
				return nil
			}), time.Second, log)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status %d", w.Code)
			}
			if tc.path == "/health/live" && calls != 0 {
				t.Fatal("liveness contacted DB")
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("leaked error")
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
			}
			if tc.want == 503 && !strings.Contains(w.Body.String(), "SERVICE_UNAVAILABLE") {
				t.Fatal(w.Body.String())
			}
		})
	}
}
func TestReadinessTimeout(t *testing.T) {
	h := Handler(pingFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }), 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.Write([]byte("finished")) }), time.Second)
	}()
	response := make(chan error, 1)
	go func() {
		c := http.Client{Timeout: 2 * time.Second}
		r, err := c.Get("http://" + listener.Addr().String())
		if err == nil {
			defer r.Body.Close()
			b, e := io.ReadAll(r.Body)
			err = e
			if string(b) != "finished" {
				err = errors.New("response interrupted")
			}
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestShutdownDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	exited := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(exited) }), 20*time.Millisecond)
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		c := http.Client{Timeout: time.Second}
		r, err := c.Get("http://" + listener.Addr().String())
		if err == nil {
			r.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want deadline exceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown blocked")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("handler context not canceled")
	}
	<-clientDone
}
