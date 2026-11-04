package config

import "testing"

func TestLoad(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://abt:local@localhost:5432/abt", "STORAGE_ENDPOINT": "http://127.0.0.1:9000", "STORAGE_BUCKET": "abt", "STORAGE_ACCESS_KEY": "k", "STORAGE_SECRET_KEY": "s"}
	c, err := Load(func(k string) string { return base[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.WorkerConcurrency != 1 || c.DBMaxConns != 5 || c.Storage.PublicEndpoint != "http://127.0.0.1:9000" || c.Media.MaxUploadBytes != 10<<20 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	for _, tc := range []struct{ k, v string }{{"DATABASE_URL", ""}, {"DATABASE_URL", "https://example.com/db"}, {"HTTP_ADDR", "bad"}, {"HTTP_ADDR", ":99999"}, {"DB_TIMEOUT", "0s"}, {"DB_TIMEOUT", "oops"}, {"SHUTDOWN_TIMEOUT", "2m"}, {"DB_MAX_CONNS", "0"}, {"WORKER_CONCURRENCY", "-1"}, {"AI_PROVIDER", "gemini"}, {"ENABLE_REAL_AI_CALLS", "true"}, {"ALLOW_PAID_AI_CALLS", "true"}, {"ALLOW_PAID_AI_CALLS", "typo"}, {"STORAGE_ENDPOINT", ""}, {"STORAGE_ENDPOINT", "http://h/path"}, {"STORAGE_BUCKET", ""}, {"STORAGE_SECRET_KEY", ""}, {"MAX_UPLOAD_BYTES", "0"}, {"SESSION_TTL", "1s"}, {"SESSIONS_PER_MINUTE", "x"}} {
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

func TestAISafetyMatrix(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://abt:local@localhost:5432/abt", "STORAGE_ENDPOINT": "http://127.0.0.1:9000", "STORAGE_BUCKET": "abt", "STORAGE_ACCESS_KEY": "k", "STORAGE_SECRET_KEY": "s"}
	with := func(kv map[string]string) (Config, error) {
		return Load(func(k string) string {
			if v, ok := kv[k]; ok {
				return v
			}
			return base[k]
		})
	}
	if c, err := with(nil); err != nil || c.AI.Provider != "mock" || c.AI.EnableReal || c.AI.AllowPaid {
		t.Fatalf("defaults must be mock and off: %v %+v", err, c.AI)
	}
	bad := map[string]map[string]string{
		"mock + enable":          {"ENABLE_REAL_AI_CALLS": "true"},
		"mock + paid":            {"ALLOW_PAID_AI_CALLS": "true"},
		"gemini without flags":   {"AI_PROVIDER": "gemini", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m"},
		"gemini enable only":     {"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m"},
		"gemini paid only":       {"AI_PROVIDER": "gemini", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m"},
		"gemini no key":          {"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_IMAGE_MODEL": "m"},
		"gemini no model":        {"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k"},
		"gemini with mock mode":  {"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m", "MOCK_PROVIDER_MODE": "failure"},
		"unknown provider":       {"AI_PROVIDER": "openai"},
		"unknown mock mode":      {"MOCK_PROVIDER_MODE": "chaos"},
		"bad generation timeout": {"GENERATION_TIMEOUT": "1h"},
		"zero attempts":          {"MAX_GENERATION_ATTEMPTS": "0"},
	}
	for name, kv := range bad {
		if _, err := with(kv); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	bad["gemini concurrency 2"] = map[string]string{"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m", "WORKER_CONCURRENCY": "2"}
	bad["bad cap"] = map[string]string{"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m", "REAL_AI_MAX_CALLS": "0"}
	bad["bad size"] = map[string]string{"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m", "GEMINI_IMAGE_SIZE": "8K"}
	for name, kv := range bad {
		if _, err := with(kv); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	ok := map[string]string{"AI_PROVIDER": "gemini", "ENABLE_REAL_AI_CALLS": "true", "ALLOW_PAID_AI_CALLS": "true", "GEMINI_API_KEY": "k", "GEMINI_IMAGE_MODEL": "m"}
	if c, err := with(ok); err != nil || c.AI.Provider != "gemini" {
		t.Fatalf("explicit opt-in must load: %v", err)
	} else if c.AI.RealCallCap != 2 || c.AI.GeminiImageSize != "1K" || c.AI.GeminiMaxInputSide != 1024 {
		t.Fatalf("conservative defaults expected: %+v", c.AI)
	}
}
