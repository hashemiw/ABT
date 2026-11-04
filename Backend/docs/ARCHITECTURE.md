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

# M1 addendum — identity and media

## Decisions made (routine, reversible)

- **Storage client:** a ~250-line SigV4 S3 client (`internal/storage`) instead of an SDK: six operations, zero new dependencies. Presigning is checked against AWS's published test vector. Path-style addressing, so it works with any S3-compatible store.
- **Local store:** SeaweedFS pinned by digest. MinIO's public images (Docker Hub, quay.io) were no longer pullable in this environment. Only the S3 wire protocol is relied upon.
- **Two keys per asset:** the client can only write `uploads/{owner}/{asset}` (signed PUT bound to exact `Content-Length` and `Content-Type`). Completion validates it, writes a re-encoded JPEG to `media/{owner}/{asset}.jpg` (`media_assets.storage_key`), and deletes the raw object *before* publishing `ready`. A client that replays its upload URL later cannot alter a validated image. No schema change was needed.
- **Normalization:** content-sniffed JPEG/PNG only, pixel cap (16 MP), minimum side 128 px, full decode behind a concurrency semaphore, re-encoded as JPEG q92 (strips EXIF/GPS; PNG alpha flattened onto white). HEIC has no stdlib decoder, so the iOS client converts before upload. EXIF orientation is **not** applied server-side; the client bakes it into pixels (documented `shortcut:` in code).
- **Completion concurrency:** `Complete` holds the asset row `FOR UPDATE` across validation, so concurrent/repeated calls serialize and later callers see the settled result (tested with 5 concurrent calls ×20 runs).
- **Deletion:** `DELETE` flips `deletion_pending` (immediately invisible: GET/complete return 404). The worker sweep (`FOR UPDATE SKIP LOCKED`, one asset per transaction) removes both objects and leaves a `deleted` tombstone so M2 history foreign keys stay valid. Repeats return the current state (`deletion_pending`, later `deleted`) with 202 and do no new work. Abandoned uploads (pending > `ABANDONED_UPLOAD_AFTER`) enter the same path. Previously issued download URLs die when the object is removed (≤ one sweep interval) or at expiry (5 min).
- **Sessions:** 256-bit random token, SHA-256 hash stored, 30-day TTL (configurable), `users`+`sessions` created in one transaction, IDs from `gen_random_uuid()`. Expired sessions are purged a week after expiry by the worker.
- **Abuse controls (M1 scope):** per-IP session creation limit (in-process fixed window, `shortcut:` noted in code), per-user cap on unfinished uploads (advisory-lock-serialized), body-size limits, strict JSON. Per-user authenticated request limits and global spend limits arrive with generation (M2/M3).
- **Additive error codes:** `RATE_LIMITED`, `UPLOAD_LIMIT_REACHED` (documented in OpenAPI).
- **HTTP layer:** stdlib `ServeMux` method routing wrapped to keep the JSON error envelope for 404/405; services return `apperr` values that handlers map to safe responses; logs record route templates, never raw paths, tokens or signed URLs.

## Decision briefs (not blocking M1; recommendations are the defaults now in code)

1. **Image and metadata retention.** *Why:* privacy policy, storage cost, history UX. *Options:* (a) keep until user deletes; (b) auto-expire originals/results after N days (e.g. 30) while keeping text metadata; (c) expire everything. *Recommendation:* (b) with N=30 for internal testing, shorter once external. *Blocks:* final M4 history/deletion UX and any external testing; M1 only has abandoned-upload cleanup (1 h) and user-initiated deletion.
2. **Account recovery.** Anonymous sessions cannot be refreshed or recovered after reinstall/expiry (new user, old uploads unreachable). *Options:* accept for the internal MVP (current), or add Sign in with Apple linking. *Recommendation:* accept until after M3. *Blocks:* external beta.
3. **HEIC server-side support.** *Options:* client converts (current), or add a cgo/libheif decoder. *Recommendation:* keep client conversion; avoids cgo and attack surface.
4. **Abuse protection before external users.** In-process IP limits are not sufficient beyond a single replica; anonymous tokens are cheap to mint. *Options:* PostgreSQL-backed counters, edge/WAF limits, App Attest. *Recommendation:* decide before external beta; M2 needs per-user generation caps regardless.
5. **Pixel/size limits.** Defaults (10 MiB, 16 MP, ≥128 px) are guesses pending the Gemini input-limit review in M3.
