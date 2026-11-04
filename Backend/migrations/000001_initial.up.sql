CREATE TABLE users (
 id UUID PRIMARY KEY,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 deleted_at TIMESTAMPTZ
);
CREATE TABLE sessions (
 id UUID PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id),
 credential_hash TEXT NOT NULL UNIQUE CHECK (credential_hash <> ''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at),
 revoked_at TIMESTAMPTZ
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE TABLE media_assets (
 id UUID PRIMARY KEY,
 owner_id UUID NOT NULL REFERENCES users(id),
 storage_key TEXT NOT NULL UNIQUE CHECK (storage_key <> ''),
 kind TEXT NOT NULL CHECK (kind IN ('original','result')),
 status TEXT NOT NULL CHECK (status IN ('pending_upload','validating','ready','rejected','deletion_pending','deleted')),
 mime_type TEXT,
 size_bytes BIGINT CHECK (size_bytes > 0),
 width INTEGER CHECK (width > 0),
 height INTEGER CHECK (height > 0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 deleted_at TIMESTAMPTZ,
 UNIQUE(id,owner_id),
 CHECK ((status = 'deleted') = (deleted_at IS NOT NULL))
);
CREATE INDEX media_assets_owner_created_idx ON media_assets(owner_id,created_at DESC);
CREATE TABLE style_presets (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 category TEXT NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 prompt_template TEXT NOT NULL,
 template_version INTEGER NOT NULL CHECK (template_version > 0),
 is_active BOOLEAN NOT NULL DEFAULT true,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE generations (
 id UUID PRIMARY KEY,
 owner_id UUID NOT NULL REFERENCES users(id),
 source_asset_id UUID NOT NULL,
 parent_generation_id UUID,
 preset_id TEXT REFERENCES style_presets(id),
 instruction TEXT,
 instruction_snapshot TEXT NOT NULL,
 prompt_version INTEGER NOT NULL CHECK (prompt_version > 0),
 status TEXT NOT NULL CHECK (status IN ('queued','processing','completed','failed','canceled')),
 output_asset_id UUID,
 provider TEXT,
 model TEXT,
 idempotency_key TEXT NOT NULL CHECK (idempotency_key <> ''),
 request_fingerprint TEXT NOT NULL CHECK (request_fingerprint <> ''),
 error_code TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 started_at TIMESTAMPTZ,
 completed_at TIMESTAMPTZ,
 UNIQUE(id,owner_id),
 UNIQUE(owner_id,idempotency_key),
 FOREIGN KEY(source_asset_id,owner_id) REFERENCES media_assets(id,owner_id),
 FOREIGN KEY(output_asset_id,owner_id) REFERENCES media_assets(id,owner_id),
 FOREIGN KEY(parent_generation_id,owner_id) REFERENCES generations(id,owner_id),
 CHECK (parent_generation_id IS DISTINCT FROM id),
 CHECK (status <> 'completed' OR output_asset_id IS NOT NULL),
 CHECK (status <> 'failed' OR (error_code IS NOT NULL AND error_code <> ''))
);
CREATE INDEX generations_owner_created_idx ON generations(owner_id,created_at DESC,id DESC);
CREATE INDEX generations_parent_idx ON generations(parent_generation_id) WHERE parent_generation_id IS NOT NULL;
CREATE TABLE generation_jobs (
 generation_id UUID PRIMARY KEY REFERENCES generations(id),
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_owner TEXT,
 lease_expires_at TIMESTAMPTZ,
 claim_token UUID,
 attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
 last_error_code TEXT,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK ((lease_owner IS NULL AND lease_expires_at IS NULL AND claim_token IS NULL)
     OR (lease_owner IS NOT NULL AND lease_owner <> '' AND lease_expires_at IS NOT NULL AND claim_token IS NOT NULL))
);
CREATE INDEX generation_jobs_available_idx ON generation_jobs(available_at) WHERE lease_owner IS NULL;
CREATE INDEX generation_jobs_lease_idx ON generation_jobs(lease_expires_at) WHERE lease_expires_at IS NOT NULL;
CREATE TABLE feedback (
 id UUID PRIMARY KEY,
 owner_id UUID NOT NULL REFERENCES users(id),
 generation_id UUID NOT NULL,
 rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
 issue_category TEXT,
 comment TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(generation_id,owner_id) REFERENCES generations(id,owner_id),
 UNIQUE(owner_id,generation_id)
);
