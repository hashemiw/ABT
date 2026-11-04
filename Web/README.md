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

`npm run lint` type-checks, `npm run build` builds. Use `http://127.0.0.1:3000` (not a LAN address) so signed storage URLs on `127.0.0.1:9000` stay reachable.

## Networking

- `/v1` and `/health` are proxied by Vite to `ABT_API_URL` (default `http://127.0.0.1:8080`), so the API needs no CORS. A production deployment must serve web and API from one origin (reverse proxy) or add CORS to Go.
- The browser PUTs the file straight to the signed storage URL (cross-origin). Local SeaweedFS already answers the CORS preflight; **a production bucket needs a CORS rule** allowing the web origin, methods `PUT, GET`, and header `Content-Type`.
- `STORAGE_PUBLIC_ENDPOINT` must be reachable from the browser.
- Photos must be JPEG or PNG (the API rejects other types).

## Known limits

- Session token is kept in `localStorage` (anonymous, 24h-class session); acceptable for M1, revisit with accounts.
- Port 8080 may be taken by an unrelated local process; the health check requires `{"status":"ok"}`, so a foreign server is reported as unreachable.
