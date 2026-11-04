# AI Beauty Tool backend — M1

Go modular monolith with independent API and worker processes. M0 provided configuration, PostgreSQL pooling, explicit migrations, health endpoints, JSON logs and graceful shutdown. **M1 adds** anonymous sessions (hashed credentials), private S3-compatible storage, upload intents with signed URLs, server-side image validation/normalization, authorized metadata/download access, and an idempotent deletion lifecycle. The worker only runs media/session cleanup. No generation, job processing, image providers or external AI calls exist. See [M1 verification](docs/M1_VERIFICATION.md).

## Local environment

Requires Go 1.26+ and Docker with Compose. From `Backend/`:

```sh
cp .env.example .env
docker compose up --build -d
docker compose ps -a
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
docker compose logs api worker migrate
```

Compose starts PostgreSQL and a local SeaweedFS S3 store (digest-pinned image; MinIO's public images were no longer pullable), waits for its health check, runs the one-shot migration command, then starts API and worker. The migration container also creates the private bucket (`STORAGE_ENSURE_BUCKET=true`, local only) and exits 0. Database data lives in a named volume. Local credentials are development-only and ports bind to loopback. `docker compose down` preserves data; adding `-v` destroys local data.

## Run Go processes on the host

```sh
docker compose up -d db
set -a
. ./.env
set +a
go run ./cmd/migrate
go run ./cmd/api
# In another terminal, load the same environment then:
go run ./cmd/worker
```

Go programs read the environment, not `.env` files. Compose uses `.env` for interpolation but deliberately uses the internal `db:5432` URL. Never commit real secrets. GEMINI variables are reserved and unused; there is no configured image model or provider credential requirement.

## Configuration

|Variable|Default / requirement|
|---|---|
|DATABASE_URL|Required PostgreSQL URL with host and database; never logged|
|HTTP_ADDR|`:8080`, numeric port 1–65535|
|DB_MAX_CONNS|5, range 1–100 per process|
|DB_TIMEOUT|3s, positive and at most 1m; connection and health probe timeout|
|SHUTDOWN_TIMEOUT|10s, positive and at most 1m; request drain deadline|
|WORKER_CONCURRENCY|1, range 1–32; reserved for later job processing|
|STORAGE_ENDPOINT / STORAGE_BUCKET / STORAGE_ACCESS_KEY / STORAGE_SECRET_KEY|Required. Server-side S3 origin and credentials (never logged)|
|STORAGE_PUBLIC_ENDPOINT|Defaults to STORAGE_ENDPOINT. Origin embedded in signed URLs; must be reachable by the client. Simulator: `http://127.0.0.1:9000`; real device: this Mac's LAN address|
|STORAGE_REGION|us-east-1|
|STORAGE_ENSURE_BUCKET|`true` makes cmd/migrate create the bucket (local only)|
|SESSION_TTL|720h, 1m..2160h|
|SESSIONS_PER_MINUTE|10 per client IP (in-process)|
|MAX_UPLOAD_BYTES|10485760|
|MAX_IMAGE_PIXELS|16000000 (decode bomb guard; minimum side 128)|
|UPLOAD_URL_TTL / DOWNLOAD_URL_TTL|10m / 5m|
|MAX_PENDING_UPLOADS|10 unfinished uploads per user|
|ABANDONED_UPLOAD_AFTER|1h, then cleaned up|
|MEDIA_SWEEP_INTERVAL|30s worker cleanup cadence|
|MAX_CONCURRENT_DECODERS|2 concurrent image decodes per API process|
|AI_PROVIDER|mock only until M3|
|ENABLE_REAL_AI_CALLS|false; true is rejected in M0|
|ALLOW_PAID_AI_CALLS|false; true is rejected in M0|
|GEMINI_API_KEY / GEMINI_IMAGE_MODEL|Unused placeholders for later integration|
|POSTGRES_PORT / API_PORT / STORAGE_PORT|Compose-only host ports, 5432 / 8080 / 9000|

Startup checks database connectivity with a deadline and fails on invalid configuration. Readiness pings PostgreSQL per request (503 on failure); liveness is independent (200). Neither checks storage or AI. Migrations are an explicit deployment prerequisite, not a readiness side effect. API logs include request ID, status and latency; dependency failures use safe messages. SIGINT/SIGTERM drains HTTP requests, force-closes after the deadline, and closes the pool. Container stop grace must exceed the configured shutdown timeout.

## Verification

```sh
gofmt -l cmd internal migrations
go vet ./...
go test ./...
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/migrate ./cmd/migrate
TEST_DATABASE_URL='postgres://abt:abt_local_only@127.0.0.1:5432/abt?sslmode=disable' \
TEST_STORAGE_ENDPOINT=http://127.0.0.1:9000 TEST_STORAGE_ACCESS_KEY=abtlocal TEST_STORAGE_SECRET_KEY=abtlocal_secret_only \
  go test -count=1 -race ./...
```

Ordinary tests skip PostgreSQL/storage integration when the TEST_* variables are absent (start `docker compose up -d db storage` first; the tests create the `abt-test` bucket and isolated schemas). Integration uses a unique temporary schema, checks concurrent/repeated migrations, constraints, checksum rejection, rollback and reapplication, and cleans up afterward. The test database role must be allowed to create/drop schemas; use a local test database. Tests never call AI APIs.

## Migrations and boundaries

`cmd/migrate` embeds versioned SQL from `migrations/`, serializes concurrent runs with a transaction advisory lock, and validates applied checksums. All pending versions and their ledger entries commit atomically. Never edit an applied migration; add a new numbered migration. The down file is a manual destructive rollback for disposable environments; no automatic downgrade command exists.

`internal/config` validates environment; `internal/database` owns pooling; `internal/httpapi` owns HTTP and shutdown; `internal/app` wires processes. Domain packages: `identity` (sessions), `media` (uploads, validation, deletion), `storage` (minimal SigV4 S3 client, no SDK), `apperr` (client-safe errors). `api/openapi.yaml` covers the implemented surface (health, sessions, media). See [architecture review](docs/ARCHITECTURE.md) for schema deviations, future provider boundaries and remaining decisions, and [verification report](docs/M0_VERIFICATION.md) for observed results. Original specifications remain in `../Docs/`.
