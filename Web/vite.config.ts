import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// The browser only talks to this origin; /v1 and /health are proxied to the Go API, so the API needs no CORS.
// Signed storage URLs are cross-origin by design: the storage service must allow this origin (see README).
const API_URL = process.env.ABT_API_URL ?? 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: '127.0.0.1',
    port: 3000,
    proxy: { '/v1': API_URL, '/health': API_URL },
  },
});
