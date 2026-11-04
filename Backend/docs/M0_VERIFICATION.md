# M0 verification report

Date: 2026-11-04 (Asia/Tehran). Result: M0 implemented and verified. M1 has not started.

## Implemented

- Go module under Backend; local Git repository initialized without a commit.
- Independent cmd/api and cmd/worker, plus explicit cmd/migrate.
- Environment validation, bounded PostgreSQL connection pool and startup/probe timeouts.
- net/http health endpoints, consistent JSON error envelope, request IDs, slog JSON logs.
- SIGINT/SIGTERM shutdown, graceful request draining and forced close after deadline.
- Seven domain tables, foreign keys, ownership constraints, checks, idempotency uniqueness and access-pattern indexes.
- Versioned up/down SQL, transactional migration application, advisory locking, checksum validation, rejection of unknown applied versions.
- Docker Compose PostgreSQL, migration task, API, idle worker; non-root application containers.
- Unit and opt-in PostgreSQL tests; .env.example; README; M0 OpenAPI 3.1; architecture review and implementation plan.
- Five existing Markdown specifications reviewed and preserved. No PDF conversion was necessary.

## Commands executed and results

Commands below ran from Backend unless stated otherwise. Host Go commands used GOCACHE=/tmp/abt-go-cache and GOMODCACHE=/tmp/abt-go-mod to keep caches in writable temporary storage.

|Command / check|Observed result|
|---|---|
|`go version`|Host Go 1.27.1 darwin/arm64; module targets Go 1.26; container builds with Go 1.26|
|`go mod tidy`|Passed; pgx and transitive dependencies resolved, go.sum generated|
|`gofmt -w cmd internal migrations`, then `gofmt -l cmd internal migrations`|Formatted; final check returned no filenames|
|`go vet ./...`|Passed from Backend, including after final test additions|
|`go test ./...`|Passed; ordinary run skips opt-in database tests|
|`TEST_DATABASE_URL='postgres://abt:abt_local_only@localhost:5432/abt?sslmode=disable' go test -count=1 -v ./...`|Passed all unit and real PostgreSQL integration tests|
|`go build -o bin/ ./cmd/...`|Passed; API, worker, migration binaries built|
|`docker compose up --build -d`|Passed; all three application images built and services started|
|`docker compose ps -a`|API and PostgreSQL healthy, worker running, migration exited 0|
|`docker compose exec -T db pg_isready -U abt -d abt`|Accepting connections|
|`docker compose exec -T db psql -U abt -d abt -c '\dt' -c 'SELECT version, applied_at FROM schema_migrations'`|Seven domain tables plus ledger; 000001_initial.up.sql applied|
|`curl --noproxy '*' --fail --max-time 5 -i http://127.0.0.1:8080/health/live` and `/health/ready`|Both 200, application/json, status ok, request ID and no-store headers|
|`docker compose stop db`, then live/ready probes|Liveness 200; readiness 503 with SERVICE_UNAVAILABLE and no database error disclosure|
|`docker compose start db`, then readiness probe|Database accepting connections; readiness recovered to 200|
|`docker compose stop api worker` and application logs|Both logged shutdown complete; containers stopped cleanly|
|`docker compose start api worker`|Migration reran safely and exited 0; API and worker restarted|
|`docker compose logs --no-color api worker migrate` and database log tail|No unexpected application errors; expected outage and shutdown events observed|
|`git init` from workspace root|Initialized local repository; no files staged or committed|

## Test evidence

Configuration tests cover defaults, valid overrides, invalid URLs/ports/durations/pool/concurrency limits, real provider rejection, enabled AI flags, and malformed booleans. Database tests exercise driver configuration error sanitization, pool limits, real connectivity, and canceled startup.

Health tests cover liveness without database calls, readiness success/failure/deadline, 404/405 responses, and request IDs. Shutdown tests cover finishing an active response and force-close/cancel when the deadline expires.

Real PostgreSQL migration tests use a unique temporary schema and remove it afterward. Concurrent migration runners serialize; repeated application creates one ledger entry. Tests inspect all seven tables and reject cross-owner source/output/feedback references, missing completed output, failed state without error, self-parenting, duplicate idempotency keys/jobs, partial leases, negative attempt counts, invalid dimensions, invalid ratings, and deletion of referenced media. Valid deletion tombstones and complete leases are accepted. Checksum tampering is rejected; down SQL and clean reapplication succeed.

Job claiming, state transitions, fencing execution, crash recovery and provider billing semantics are not implemented or claimed tested in M0. The schema supplies their prerequisites.

## Environment issues resolved

Initial sandbox runs could not resolve/download modules, bind a localhost test socket, access Docker's socket, or initialize .git. Re-running the necessary commands with approved execution access succeeded. Docker was initially stopped; standard macOS open commands failed, but `docker desktop start` succeeded. One go vet invocation from the workspace root failed because the module lives in Backend; the corrected invocation passed.

An initial curl to localhost:8080 reached another existing server and returned plain text. Explicit 127.0.0.1 probes reached ABT and verified the JSON contract. README and environment examples now use explicit IPv4 for host access.

No required M0 verification remains unexecuted. No Gemini/project availability tests or paid/provider integration tests were run because real AI is explicitly outside M0.

## Limitations and remaining decisions

See ARCHITECTURE.md for documented specification conflicts and resolutions. Media deletion_pending, a job claim_token, preset description, and SQL ownership defenses are intentional additions. Restrictive references preserve tombstones; future service code must enforce authorization, asset readiness, history hiding, atomic generation/job creation, completed-only feedback, transitions and lease fencing.

Retention durations, idempotency expiration, media constraints, supported feedback codes/text limits, account recovery, provider budgets and retry policy remain decisions for later milestones. Gemini model availability and project pricing remain unverified; no model is hardcoded. Real-call and paid-call flags cannot be enabled in M0. No provider adapter exists, including mock generation; mock here is the enforced safe configuration for the foundation only.

Local development credentials in Compose and .env.example are disposable examples. No real secret or .env file was created, staged or committed. No image or AI network requests were made. Application runtime only contacts PostgreSQL; build-time dependency/image downloads occurred.

Services are left running locally with PostgreSQL data persisted. Stop with `docker compose down` from Backend. Do not add `-v` unless intentionally deleting local database data.
