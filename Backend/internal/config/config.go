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
}

// Load accepts an injected environment lookup so tests never depend on host secrets.
func Load(get func(string) string) (Config, error) {
	c := Config{HTTPAddr: ":8080", DBMaxConns: 5, DBTimeout: 3 * time.Second, ShutdownTimeout: 10 * time.Second, WorkerConcurrency: 1}
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
	}{{"DB_TIMEOUT", &c.DBTimeout}, {"SHUTDOWN_TIMEOUT", &c.ShutdownTimeout}} {
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
