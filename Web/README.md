# ABT Web

React 19 + Vite + Tailwind client for the ABT Go backend. It talks only to the Go API; there is no Node backend and no AI key in the browser.
UI originated from a Gemini-generated prototype; its Express server, in-memory persistence, offline session fallback and canvas "mock demo" were removed.

## Status (M1)

| Feature | State |
|---|---|
| Anonymous session (`POST /v1/sessions`) | real |
| Upload intent -> direct PUT to storage -> complete -> show stored photo | real |
| Delete (`DELETE /v1/media/{id}`) | real |
| Generation, presets, history, feedback | **not implemented in Go yet (M2)**. Generate button is disabled; the preset list is a static preview (`src/presets.ts`). `GenerationViewer`/`HistoryDrawer` and the API wrappers in `src/api/client.ts` are kept for M2 but not mounted. |

If the backend is unreachable the UI says so; nothing falls back to fake success.

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
npm run test:e2e    # REAL end-to-end: needs `docker compose up` in Backend/ (Go + PostgreSQL + storage)
```

The e2e suite runs the client from Node against the live API and storage with synthetic fixtures (`e2e/fixtures/`): JPEG and PNG intent -> PUT -> complete (`{}`) -> stored copy -> ownership isolation -> delete -> 404, plus failure cases. It fails (does not skip) when the stack is down and deletes the assets it creates. It runs outside a browser, so real browser CORS is only covered by an OPTIONS preflight assertion plus manual checks. Set `ABT_API_URL` to target another API.

## Known limits

- Generation, presets, history and feedback do not exist in Go yet (M2). The Generate button is disabled with an explanation; styles shown are a static preview (`src/presets.ts`). `GenerationViewer`/`HistoryDrawer` and API wrappers are kept for M2 but not mounted.
- Session token lives in `localStorage` (anonymous session); revisit when accounts exist.
- No browser-automation (Playwright) test yet; UI flows were verified manually in a browser.
