package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// limiter is a fixed-window counter per key.
// shortcut: in-process only, so limits are per API replica; move to PostgreSQL/edge limiting before scaling out or external beta.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]int
	reset  time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string]int{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now := time.Now(); now.After(l.reset) {
		l.hits, l.reset = map[string]int{}, now.Add(l.window)
	}
	l.hits[key]++
	return l.hits[key] <= l.max
}

// clientIP uses the socket peer; forwarded headers are client-controlled and not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
