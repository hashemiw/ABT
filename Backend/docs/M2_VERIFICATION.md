# M2 verification — generation slice (mock provider)

Date: 2026-11-04. Stack: Docker Compose PostgreSQL 17 + SeaweedFS + Go API + worker (`AI_PROVIDER=mock`).

| Check | Result |
|---|---|
| `gofmt -l .` / `go vet ./...` | clean |
| `go test -race -count=1 ./...` with `TEST_DATABASE_URL`, `TEST_STORAGE_*` | all packages `ok` (config, database, generation, httpapi, media, provider, storage, migrations); exit 0 |
| Generation integration tests (real PostgreSQL + storage, mock provider) | ownership, invalid/deleted/not-ready source, preset validation, instruction limits, per-user limits, idempotency (same/changed/cross-user), 12 concurrent identical requests = 1 generation + 1 job, generation/job atomic rollback, claim exclusivity, lease expiry reclaim with new token and attempt, stale worker complete/fail fenced out, reclaim after possible provider call does not call again, crash-before-call recovery, 4 workers × 6 jobs = 6 calls, cancel queued and in-flight, source deleted before run, result-storage failure not retried, failure modes (failure/timeout/invalid output/identical/busy with bounded retries) |
| HTTP end-to-end test | presets (no templates, auth required) → create → idempotent repeat/409 → worker → result download differs from source → cross-user 404 → delete result → cancel returns real state; failure reported safely |
| Provider tests (offline) | mock determinism; Gemini fixtures (image, text-only, blocked, safety, no candidates, bad base64, non-JSON); request carries exact bytes/MIME/prompt/key header, key not in URL; HTTP 429/503 = not executed, 5xx = uncertain, 4xx = permanent; timeout uncertain and leaks no key |
| Config matrix | mock/gemini flag combinations, missing key/model, unknown provider/mock mode |
| No real AI call | no test constructs a real vendor URL; the Gemini adapter is exercised only against `httptest` servers and fixtures |

## Not verified

- Any real Gemini request: not run (needs explicit approval, see PR). Image quality, identity preservation and clothing change are **unverified**; the mock only tints and stamps a banner.
- Orphan-object sweeper, multi-replica rate limiting, retention of idempotency records, load behaviour.
