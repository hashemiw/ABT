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
	AI                         AI
	Gen                        Gen
}

// AI selects the image provider. Real (and therefore potentially paid) calls need three explicit opt-ins.
type AI struct {
	Provider               string // mock | gemini
	EnableReal, AllowPaid  bool
	GeminiKey, GeminiModel string
	MockMode               string
	RealCallCap            int
	GeminiImageSize        string
	GeminiMaxInputSide     int
}

type Gen struct {
	Timeout, Lease                              time.Duration
	MaxActive, MaxPerDay, MaxChars, MaxAttempts int
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
	if err := loadAI(get, &c); err != nil {
		return c, err
	}
	return c, nil
}

func loadAI(get func(string) string, c *Config) error {
	a := &c.AI
	a.Provider = get("AI_PROVIDER")
	if a.Provider == "" {
		a.Provider = "mock"
	}
	for _, f := range []struct {
		name string
		to   *bool
	}{{"ENABLE_REAL_AI_CALLS", &a.EnableReal}, {"ALLOW_PAID_AI_CALLS", &a.AllowPaid}} {
		if v := get(f.name); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s must be true or false", f.name)
			}
			*f.to = b
		}
	}
	a.GeminiKey, a.GeminiModel, a.MockMode = get("GEMINI_API_KEY"), get("GEMINI_IMAGE_MODEL"), get("MOCK_PROVIDER_MODE")
	switch a.Provider {
	case "mock":
		// A mock deployment must not look like it could spend money.
		if a.EnableReal || a.AllowPaid {
			return fmt.Errorf("AI_PROVIDER=mock requires ENABLE_REAL_AI_CALLS=false and ALLOW_PAID_AI_CALLS=false")
		}
		switch a.MockMode {
		case "", "success", "timeout", "failure", "invalid_output", "identical", "busy":
		default:
			return fmt.Errorf("MOCK_PROVIDER_MODE is not a known mode")
		}
	case "gemini":
		if a.MockMode != "" {
			return fmt.Errorf("MOCK_PROVIDER_MODE is only valid with AI_PROVIDER=mock")
		}
		// Free-tier image editing is not assumed: real calls are treated as paid and need both opt-ins.
		if !a.EnableReal || !a.AllowPaid {
			return fmt.Errorf("AI_PROVIDER=gemini requires ENABLE_REAL_AI_CALLS=true and ALLOW_PAID_AI_CALLS=true")
		}
		if a.GeminiKey == "" || a.GeminiModel == "" {
			return fmt.Errorf("AI_PROVIDER=gemini requires GEMINI_API_KEY and GEMINI_IMAGE_MODEL")
		}
		if c.WorkerConcurrency != 1 {
			return fmt.Errorf("AI_PROVIDER=gemini requires WORKER_CONCURRENCY=1")
		}
		a.RealCallCap, a.GeminiImageSize, a.GeminiMaxInputSide = 2, "1K", 1024
		if v := get("REAL_AI_MAX_CALLS"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1000 {
				return fmt.Errorf("REAL_AI_MAX_CALLS must be 1..1000")
			}
			a.RealCallCap = n
		}
		if v := get("GEMINI_IMAGE_SIZE"); v != "" {
			switch v {
			case "off": // omit the field entirely
				a.GeminiImageSize = ""
			case "512", "1K", "2K", "4K":
				a.GeminiImageSize = v
			default:
				return fmt.Errorf("GEMINI_IMAGE_SIZE must be off, 512, 1K, 2K or 4K")
			}
		}
		if v := get("GEMINI_MAX_INPUT_SIDE"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 256 || n > 4096 {
				return fmt.Errorf("GEMINI_MAX_INPUT_SIDE must be 256..4096")
			}
			a.GeminiMaxInputSide = n
		}
	default:
		return fmt.Errorf("AI_PROVIDER must be mock or gemini")
	}
	g := &c.Gen
	g.Timeout, g.Lease, g.MaxActive, g.MaxPerDay, g.MaxChars, g.MaxAttempts = 90*time.Second, 60*time.Second, 2, 30, 200, 3
	for _, d := range []struct {
		name string
		to   *time.Duration
		max  time.Duration
	}{{"GENERATION_TIMEOUT", &g.Timeout, 5 * time.Minute}, {"JOB_LEASE", &g.Lease, 10 * time.Minute}} {
		if v := get(d.name); v != "" {
			x, err := time.ParseDuration(v)
			if err != nil || x < time.Second || x > d.max {
				return fmt.Errorf("%s must be a duration in [1s, %s]", d.name, d.max)
			}
			*d.to = x
		}
	}
	for _, n := range []struct {
		name string
		to   *int
		max  int
	}{{"MAX_ACTIVE_GENERATIONS", &g.MaxActive, 20}, {"MAX_GENERATIONS_PER_DAY", &g.MaxPerDay, 1000}, {"MAX_INSTRUCTION_CHARS", &g.MaxChars, 1000}, {"MAX_GENERATION_ATTEMPTS", &g.MaxAttempts, 5}} {
		if v := get(n.name); v != "" {
			x, err := strconv.Atoi(v)
			if err != nil || x < 1 || x > n.max {
				return fmt.Errorf("%s must be 1..%d", n.name, n.max)
			}
			*n.to = x
		}
	}
	return nil
}
