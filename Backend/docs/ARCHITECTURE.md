# M0 architecture review and plan

Reviewed all five Markdown specifications in ../Docs; originals remain unchanged. No PDFs were supplied. Precedence: kickoff, API contract, database schema, architecture, PRD, foundation.

## Decisions and conflicts

- Use Backend as the Go module root, standard net/http and slog, pgxpool for PostgreSQL. No ORM/sqlc until actual business queries require it. API and worker are independent processes. Explicit migration command; never migrate during application startup.
- API specifies deletion_pending but schema omits it: include it. Keep media tombstones and restrictive foreign keys to preserve history. Future handlers must deny reads and new operations for pending/deleted assets and hide affected history. Physical purge/retention is unresolved; no deletion operations in M0.
- Schema says application ownership checks are required. Add composite foreign keys on (resource_id, owner_id) as defense in depth; authorization and readiness still require transactional service checks later.
- Add nullable UUID claim_token to jobs because schema requires fresh fencing identity but supplies no column. Lease owner, expiry, and token must all be present or all absent. Job lifecycle derives from generations.status; do not add a second status. Future claims must lock rows, check generation state, increment attempt_count, replace token, and fence every completion/heartbeat by token and unexpired lease. Recovery and transition logic is deliberately M2 work.
- Generation/job atomic creation is a future service transaction; PK guarantees at most one job, not existence of a job for every generation. No speculative triggers or business processing in M0.
- Add style_presets.description for API response. Keep nullable preset_id as persistence spec requires; future API must require an active preset. API result_asset_id maps to database output_asset_id.
- Keep attempt_count only on jobs, per database precedence. Preserve instruction_snapshot and prompt_version. Unique owner/idempotency_key plus request_fingerprint supports future conflict detection. M0 never purges generation metadata or idempotency keys; retention duration must be decided before external testing.
- All private owners and required relationships are NOT NULL. Completed generations require output; failed generations require an error code. Feedback owner is constrained in SQL; completed-only feedback, supported issue categories, and text limits need future service validation.
- No real AI adapters or requests in M0. AI_PROVIDER must be mock; both enable flags must remain false. Gemini model/key names are reserved configuration, with no model default. Later milestones must consult current official Gemini documentation and verify project access and cost explicitly. Free-tier availability is unverified. OpenAI and planning agents remain deferred.
- Object storage is not an M0 dependency: no upload/read business flow exists yet. Compose needs PostgreSQL, migration task, API, and idle worker only.

## Implementation sequence

1. Configuration, logging, connection pool, process lifecycle.
2. Explicit transactional, versioned migrations with advisory locking and checksum validation.
3. Health routes and M0 OpenAPI contract.
4. Compose, Dockerfile, environment example and local instructions.
5. Unit tests, real PostgreSQL migration/constraint tests, full build/runtime verification.

## Remaining decisions

Before dependent milestones: image and metadata retention including idempotency expiry; account recovery; instruction/comment limits and issue-category enum; image formats and dimensions; budgets, retry limits, provider timeout and uncertain-outcome reconciliation; verified Gemini model/project access and pricing. No external contract for these is silently invented in M0. State transitions and crash recovery must be tested when the job processor exists.
