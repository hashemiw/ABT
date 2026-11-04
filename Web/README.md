# ABT Web

React 19 + Vite + Tailwind client for the ABT Go backend. It talks only to the Go API; there is no Node backend and no AI key in the browser.
UI originated from a Gemini-generated prototype; its Express server, in-memory persistence, offline session fallback and canvas "mock demo" were removed.

## Status

| Feature | State |
|---|---|
| Anonymous session, upload intent -> direct PUT -> complete -> show stored copy -> delete | real (M1) |
| Styles | fetched from `GET /v1/styles/presets` (authenticated) |
| Generate | `POST /v1/generations` with an `Idempotency-Key`; a retry of the *same* request reuses its key, a changed request gets a new one |
| Progress | polling with backoff, stops on a terminal state, after 3 min, or after 5 consecutive errors, then offers "Check again" / Cancel. No progress percentage is shown. |
| Result | Before = stored source, After = stored result, with a slider. A **mock** result is labelled "MOCK PROVIDER" |
| Not built | history list, refinement, feedback |

**Mock vs real:** the default worker uses the offline mock provider. It tints the photo and adds a striped banner to prove the pipeline; it does **not** edit clothing. Real Gemini output only appears if the Go worker is explicitly configured for it (see `Backend/README.md`); the web app never sees a provider key.

## Run locally

```bash
cd Backend && docker compose up -d --build   # API :8080, storage :9000, Postgres, worker
cd ../Web && npm install && npm run dev      # http://127.0.0.1:3000
```

Use `http://127.0.0.1:3000` (not a LAN address) so signed storage URLs on `127.0.0.1:9000` stay reachable.

## Networking and proxy (development only)

- **The Vite proxy is a dev convenience.** `vite.config.ts` forwards `/v1` and `/health` to `ABT_API_URL` (default `http://127.0.0.1:8080`) so the browser sees one origin and the Go API needs no CORS. `vite preview`/`vite build` output has **no proxy**.
- **Production needs its own routing**: serve the built `dist/` and the API behind one reverse proxy/origin (`/v1`, `/health` -> Go), or add CORS to Go. Not done here.
- The browser PUTs the photo straight to the signed storage URL (cross-origin by design). Local SeaweedFS answers the CORS preflight; **a production bucket needs a CORS rule** for the web origin (methods `PUT, GET`, header `Content-Type`). `STORAGE_PUBLIC_ENDPOINT` must be browser-reachable.
- Photos: JPEG or PNG, at most 10 MB, at least 128 px per side (server rule). Stored copy is a normalized JPEG.

## Errors and session behaviour

- API down, or something other than `{"status":"ok"}` on `/health/live` (e.g. a foreign server answering "hello from server"): shown as unreachable; nothing is uploaded; no offline mode.
- Upload/complete failure: error shown with **Try Again** (user-initiated, one attempt per click, no automatic retry). The half-finished asset is deleted best-effort.
- HTTP 401 / expired session: the token is discarded, the operation stops and the user is told that photos from the previous session are inaccessible. A new anonymous session is created **only** when the user clicks *Start New Session* (or the header reset button); the failed operation is never replayed under the new identity.
- Double clicks on upload/delete are ignored while one is in flight. A failed delete keeps the photo visible with a message.
- Tokens and signed URLs are never logged or placed in error messages.

## Tests

```bash
npm run lint        # tsc --noEmit (src, e2e)
npm test            # MOCKED unit tests (stubbed fetch): health parsing, 401 handling, "{}" body, error messages
npm run test:e2e    # REAL end-to-end: needs `docker compose up` in Backend/ (Go + PostgreSQL + storage + mock worker)
```

The e2e suite runs the client from Node against the live API and storage with synthetic fixtures (`e2e/fixtures/`): JPEG and PNG intent -> PUT -> complete (`{}`) -> stored copy -> ownership isolation -> delete -> 404, plus failure cases. It fails (does not skip) when the stack is down and deletes the assets it creates. It runs outside a browser, so real browser CORS is only covered by an OPTIONS preflight assertion plus manual checks. Set `ABT_API_URL` to target another API.

## Known limits

- If no worker is running, a generation stays queued; the UI says so after polling stops. It never shows a fake result.
- Session token lives in `localStorage` (anonymous session); revisit when accounts exist.
- No browser-automation (Playwright) test yet; UI flows were verified manually in a browser.
