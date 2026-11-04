# AI Beauty Tool backend — M0

Go modular monolith with independent API and worker processes. M0 implements configuration, PostgreSQL pooling, explicit migrations, health endpoints, JSON logs, graceful shutdown, and tests. The worker is intentionally idle. No authentication, uploads, job execution, image adapters or external AI calls exist.

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

Compose starts PostgreSQL, waits for its health check, runs the one-shot migration command, then starts API and worker. A successful migration container exits with code 0. Database data lives in a named volume. Local credentials are development-only and ports bind to loopback. `docker compose down` preserves data; adding `-v` destroys local data.

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
|AI_PROVIDER|mock only in M0|
|ENABLE_REAL_AI_CALLS|false; true is rejected in M0|
|ALLOW_PAID_AI_CALLS|false; true is rejected in M0|
|GEMINI_API_KEY / GEMINI_IMAGE_MODEL|Unused placeholders for later integration|
|POSTGRES_PORT / API_PORT|Compose-only host ports, 5432 / 8080|

Startup checks database connectivity with a deadline and fails on invalid configuration. Readiness pings PostgreSQL per request (503 on failure); liveness is independent (200). Neither checks storage or AI. Migrations are an explicit deployment prerequisite, not a readiness side effect. API logs include request ID, status and latency; dependency failures use safe messages. SIGINT/SIGTERM drains HTTP requests, force-closes after the deadline, and closes the pool. Container stop grace must exceed the configured shutdown timeout.

## Verification

```sh
gofmt -l cmd internal migrations
go vet ./...
go test ./...
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
go build -o bin/migrate ./cmd/migrate
TEST_DATABASE_URL='postgres://abt:abt_local_only@127.0.0.1:5432/abt?sslmode=disable' go test -count=1 -v ./migrations
```

Ordinary tests skip PostgreSQL integration when TEST_DATABASE_URL is absent. Integration uses a unique temporary schema, checks concurrent/repeated migrations, constraints, checksum rejection, rollback and reapplication, and cleans up afterward. The test database role must be allowed to create/drop schemas; use a local test database. Tests never call AI APIs.

## Migrations and boundaries

`cmd/migrate` embeds versioned SQL from `migrations/`, serializes concurrent runs with a transaction advisory lock, and validates applied checksums. All pending versions and their ledger entries commit atomically. Never edit an applied migration; add a new numbered migration. The down file is a manual destructive rollback for disposable environments; no automatic downgrade command exists.

`internal/config` validates environment; `internal/database` owns pooling; `internal/httpapi` owns HTTP and shutdown; `internal/app` wires processes. Domain packages will be added only when needed. `api/openapi.yaml` covers the implemented M0 surface only. See [architecture review](docs/ARCHITECTURE.md) for schema deviations, future provider boundaries and remaining decisions, and [verification report](docs/M0_VERIFICATION.md) for observed results. Original specifications remain in `../Docs/`.
