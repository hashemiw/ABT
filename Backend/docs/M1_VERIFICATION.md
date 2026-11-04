# M1 verification report — identity and media

Date: 2026-11-04. Baseline: b530fc8 (M0). Result: M1 backend and initial iOS client implemented and verified as stated below. Nothing committed or pushed.

## Scope delivered

Backend: anonymous sessions (SHA-256 hashed credentials), bearer authentication, ownership enforcement (foreign/missing/pending-deletion assets are indistinguishable 404s), private S3-compatible storage with a self-contained SigV4 client, upload intents (signed PUT bound to exact size and type), synchronous completion with content-based validation and JPEG normalization (metadata stripped), authorized metadata and short-lived download URLs, idempotent deletion with tombstones, worker cleanup (deletions, abandoned uploads, expired sessions), per-IP session rate limit, per-user unfinished-upload cap, OpenAPI 0.2.0.

iOS: XcodeGen SwiftUI app (iOS 17+, no dependencies): Keychain session store, `APIClient` actor, PhotosPicker, ImageIO preparation (HEIC/PNG/JPEG → upright JPEG ≤2048 px), upload state machine with loading, retry and error states, stored-copy confirmation, delete.

Not in M1: presets, generations, jobs, providers, feedback endpoints; no AI call of any kind was made.

## Checks run (host: macOS, Go 1.27.1, Xcode 27.2, Docker 29.8.1)

|Check|Result|
|---|---|
|M0 baseline: gofmt, go vet, go test, real-PostgreSQL tests, `/health/ready` on the pre-existing stack|Passed before any change|
|`gofmt -l cmd internal migrations`|No files listed|
|`go vet ./...`|Passed|
|`go build -o bin/ ./cmd/...`|Passed|
|`go test -race -count=1 ./...` with `TEST_DATABASE_URL` and `TEST_STORAGE_*` against Compose PostgreSQL 17 and SeaweedFS|All passed (config, database, httpapi integration ×10, media, storage, migrations)|
|`TestUploadLifecycle` repeated ×20 (concurrent completions)|Passed; an earlier flaky race was found and fixed (see below)|
|Storage SigV4 presign vs AWS's published test vector|Matches|
|`docker compose up --build -d`, final code|PostgreSQL, storage healthy; migrate exited 0 and created the bucket; API healthy; worker running|
|curl smoke against the rebuilt stack: session → intent → signed PUT → complete → unauthenticated GET|200 PUT, `ready` 600×600 JPEG, 401|
|`xcodebuild test` (iPhone 18 Pro simulator, iOS 27.0, ad-hoc signing)|8 tests in 3 suites passed: API client (session creation, bearer, 401 re-session retry, error envelope, signed upload without bearer, date decoding), Keychain round trip, ImagePreparer orientation/downscale/rejection|
|Simulator end to end (backend in Compose, loopback networking)|Picked a HEIC from the simulator library → converted → session + intent → direct upload → validated → stored copy displayed via signed URL → Delete → API 202, worker sweep, DB tombstone `deleted`. An initial run hit an upload failure (below); the app showed the error state with *Try again*, which then succeeded|

### Backend integration tests cover
Missing/garbage/expired/revoked tokens → 401; hash-only credential storage; session rate limit (429); server-generated keys; EXIF payload stripped; PNG alpha flattened; raw upload object removed after completion; non-image, truncated, oversized-pixel and undersized images → 422 and stable on repeat; premature completion → 409; intent validation (415/413/422/400); signed URL rejects a different body size; cross-user GET/complete/DELETE → 404 with owner unaffected; deletion (immediate 404, idempotent repeat, sweep once only, object gone, tombstone); abandoned-upload cleanup and pending cap (429); signed upload/download URLs expire.

## Issues found and resolved during verification

1. Concurrent `Complete` calls could return 503/409 while another call was mid-publish → serialized with the asset row lock; verified 20 repeated runs.
2. Local SeaweedFS `-volume.max=8` was exhausted by the first test bucket, so the app's first real upload failed with a storage error. Raised to 64 in compose. This was local configuration, not application logic.
3. Unsigned simulator test builds cannot use the Keychain (-34018); tests run with ad-hoc signing. The README command reflects this.
4. MinIO images could not be pulled from Docker Hub/quay.io; SeaweedFS (digest-pinned) is the local store. The client uses only the S3 wire protocol.

## Not verified / limits

- No physical device testing. Simulator run used a wallpaper image, not a portrait; no accessibility audit, VoiceOver or Dynamic Type pass.
- Real S3/cloud storage not exercised; behavior of signed `Content-Length` enforcement is verified against SeaweedFS only.
- OpenAPI conformance is by hand plus the integration assertions; no automated schema validator runs.
- HEIC is converted on the client; the server accepts JPEG/PNG only. EXIF orientation is not applied server-side.
- Rate limiting is in-process per API replica; global/per-user request limits are deferred to M2/M3.
- A storage outage during deletion stops that sweep at the failing row until storage recovers (documented in code).
- Bash sandboxing blocked `simctl`/Docker in this environment; those commands ran with sandbox disabled, as in M0.
- Clearing local data: `docker compose down -v` destroys database and storage volumes.

## Files changed

Backend: `internal/{apperr,storage,identity,media}` (new), `internal/httpapi/{http,limiter,v1,integration_test}.go`, `internal/app/app.go`, `internal/config/*`, `cmd/migrate/main.go`, `compose.yaml`, `.env.example`, `api/openapi.yaml`, `README.md`, `docs/ARCHITECTURE.md` (M1 addendum + decision briefs), this report. iOS: `iOS/` (new). Root `README.md`, `.gitignore`. No migration was added; the M0 schema sufficed.
