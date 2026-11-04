package config

import "testing"

func TestLoad(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://abt:local@localhost:5432/abt"}
	c, err := Load(func(k string) string { return base[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.WorkerConcurrency != 1 || c.DBMaxConns != 5 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	for _, tc := range []struct{ k, v string }{{"DATABASE_URL", ""}, {"DATABASE_URL", "https://example.com/db"}, {"HTTP_ADDR", "bad"}, {"HTTP_ADDR", ":99999"}, {"DB_TIMEOUT", "0s"}, {"DB_TIMEOUT", "oops"}, {"SHUTDOWN_TIMEOUT", "2m"}, {"DB_MAX_CONNS", "0"}, {"WORKER_CONCURRENCY", "-1"}, {"AI_PROVIDER", "gemini"}, {"ENABLE_REAL_AI_CALLS", "true"}, {"ALLOW_PAID_AI_CALLS", "true"}, {"ALLOW_PAID_AI_CALLS", "typo"}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			_, err := Load(func(k string) string {
				if k == tc.k {
					return tc.v
				}
				return base[k]
			})
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	_, err = Load(func(k string) string {
		switch k {
		case "DB_MAX_CONNS":
			return "10"
		case "WORKER_CONCURRENCY":
			return "2"
		case "DB_TIMEOUT":
			return "1s"
		case "SHUTDOWN_TIMEOUT":
			return "5s"
		case "AI_PROVIDER":
			return "mock"
		case "ENABLE_REAL_AI_CALLS", "ALLOW_PAID_AI_CALLS":
			return "false"
		}
		return base[k]
	})
	if err != nil {
		t.Fatal(err)
	}
}
