-- Persistent ledger of every provider call attempt. Rows are inserted BEFORE the call, so uncertain/crashed calls count.
-- The real-call cap is enforced against count(*) of provider='gemini'; a restart cannot reset it.
CREATE TABLE provider_calls (
 id BIGSERIAL PRIMARY KEY,
 generation_id UUID NOT NULL REFERENCES generations(id),
 provider TEXT NOT NULL CHECK (provider <> ''),
 model TEXT NOT NULL,
 started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 outcome TEXT NOT NULL DEFAULT 'unknown' CHECK (outcome IN ('unknown','succeeded','failed')),
 error_code TEXT,
 latency_ms INTEGER CHECK (latency_ms >= 0),
 usage JSONB
);
CREATE INDEX provider_calls_provider_idx ON provider_calls(provider);
