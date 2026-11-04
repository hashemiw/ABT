package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr, DatabaseURL      string
	DBMaxConns                 int32
	DBTimeout, ShutdownTimeout time.Duration
	WorkerConcurrency          int
	Storage                    Storage
	SessionTTL                 time.Duration
	SessionsPerMinute          int // per client IP
	MediaSweepInterval         time.Duration
	Media                      Media
}

// Storage describes the private S3-compatible bucket. Endpoint is used by the
// server; PublicEndpoint is the host baked into signed URLs handed to clients.
type Storage struct {
	Endpoint, PublicEndpoint, Bucket, Region, AccessKey, SecretKey string
}

type Media struct {
	MaxUploadBytes        int64
	MaxPixels             int64
	MinSide               int
	UploadURLTTL          time.Duration
	DownloadURLTTL        time.Duration
	MaxPendingUploads     int
	AbandonedUploadAfter  time.Duration
	MaxConcurrentDecoders int
}

// Load accepts an injected environment lookup so tests never depend on host secrets.
func Load(get func(string) string) (Config, error) {
	c := Config{HTTPAddr: ":8080", DBMaxConns: 5, DBTimeout: 3 * time.Second, ShutdownTimeout: 10 * time.Second, WorkerConcurrency: 1,
		SessionTTL: 30 * 24 * time.Hour, SessionsPerMinute: 10, MediaSweepInterval: 30 * time.Second,
		Storage: Storage{Region: "us-east-1"},
		Media: Media{MaxUploadBytes: 10 << 20, MaxPixels: 16_000_000, MinSide: 128, UploadURLTTL: 10 * time.Minute,
			DownloadURLTTL: 5 * time.Minute, MaxPendingUploads: 10, AbandonedUploadAfter: time.Hour, MaxConcurrentDecoders: 2}}
	if v := get("HTTP_ADDR"); v != "" {
		c.HTTPAddr = v
	}
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return c, fmt.Errorf("HTTP_ADDR must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return c, fmt.Errorf("HTTP_ADDR port must be 1..65535")
	}
	c.DatabaseURL = get("DATABASE_URL")
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path == "" || u.Path == "/" {
		return c, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with host and database")
	}
	for _, item := range []struct {
		name   string
		target *time.Duration
	}{{"DB_TIMEOUT", &c.DBTimeout}, {"SHUTDOWN_TIMEOUT", &c.ShutdownTimeout}, {"UPLOAD_URL_TTL", &c.Media.UploadURLTTL}, {"DOWNLOAD_URL_TTL", &c.Media.DownloadURLTTL},
		{"ABANDONED_UPLOAD_AFTER", &c.Media.AbandonedUploadAfter}, {"MEDIA_SWEEP_INTERVAL", &c.MediaSweepInterval}} {
		if v := get(item.name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 || d > time.Minute {
				return c, fmt.Errorf("%s must be a duration in (0, 1m]", item.name)
			}
			*item.target = d
		}
	}
	for _, item := range []struct {
		name string
		max  int
	}{{"DB_MAX_CONNS", 100}, {"WORKER_CONCURRENCY", 32}} {
		v := get(item.name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > item.max {
			return c, fmt.Errorf("%s must be 1..%d", item.name, item.max)
		}
		if item.name == "DB_MAX_CONNS" {
			c.DBMaxConns = int32(n)
		} else {
			c.WorkerConcurrency = n
		}
	}
	if v := get("SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > 90*24*time.Hour {
			return c, fmt.Errorf("SESSION_TTL must be a duration in [1m, 2160h]")
		}
		c.SessionTTL = d
	}
	for _, item := range []struct {
		name   string
		target *int
		max    int
	}{{"SESSIONS_PER_MINUTE", &c.SessionsPerMinute, 1000}, {"MAX_PENDING_UPLOADS", &c.Media.MaxPendingUploads, 100}, {"MAX_CONCURRENT_DECODERS", &c.Media.MaxConcurrentDecoders, 16}} {
		if v := get(item.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > item.max {
				return c, fmt.Errorf("%s must be 1..%d", item.name, item.max)
			}
			*item.target = n
		}
	}
	for _, item := range []struct {
		name   string
		target *int64
		max    int64
	}{{"MAX_UPLOAD_BYTES", &c.Media.MaxUploadBytes, 50 << 20}, {"MAX_IMAGE_PIXELS", &c.Media.MaxPixels, 64_000_000}} {
		if v := get(item.name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 || n > item.max {
				return c, fmt.Errorf("%s must be 1..%d", item.name, item.max)
			}
			*item.target = n
		}
	}
	s := &c.Storage
	s.Endpoint, s.PublicEndpoint, s.Bucket = get("STORAGE_ENDPOINT"), get("STORAGE_PUBLIC_ENDPOINT"), get("STORAGE_BUCKET")
	s.AccessKey, s.SecretKey = get("STORAGE_ACCESS_KEY"), get("STORAGE_SECRET_KEY")
	if v := get("STORAGE_REGION"); v != "" {
		s.Region = v
	}
	if s.PublicEndpoint == "" {
		s.PublicEndpoint = s.Endpoint
	}
	for _, e := range []string{s.Endpoint, s.PublicEndpoint} {
		u, err := url.Parse(e)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return c, fmt.Errorf("STORAGE_ENDPOINT and STORAGE_PUBLIC_ENDPOINT must be http(s) origins")
		}
	}
	if s.Bucket == "" || s.AccessKey == "" || s.SecretKey == "" {
		return c, fmt.Errorf("STORAGE_BUCKET, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY are required")
	}
	if v := get("AI_PROVIDER"); v != "" && v != "mock" {
		return c, fmt.Errorf("AI_PROVIDER must be mock in M0")
	}
	for _, name := range []string{"ENABLE_REAL_AI_CALLS", "ALLOW_PAID_AI_CALLS"} {
		if v := get(name); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil || b {
				return c, fmt.Errorf("%s must be false in M0", name)
			}
		}
	}
	return c, nil
}
