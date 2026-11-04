// END-TO-END (no mocks): real Go API + PostgreSQL + S3-compatible storage started via `docker compose up` in Backend/.
// Run: ABT_API_URL=http://127.0.0.1:8080 npm run test:e2e
import { readFileSync } from 'node:fs';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { APIClient, ApiError, SessionExpiredError } from '../src/api/client';

const API = process.env.ABT_API_URL ?? 'http://127.0.0.1:8080';
const ORIGIN = 'http://127.0.0.1:3000'; // the Vite dev origin
const mem = () => { const m = new Map<string, string>(); return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) }; };
const fixture = (name: string, type: string) => new Blob([readFileSync(new URL(`./fixtures/${name}`, import.meta.url))], { type });

// Record the exact body sent to /complete without altering behaviour.
const completeBodies: unknown[] = [];
const realFetch = globalThis.fetch;
globalThis.fetch = (async (input: any, init?: any) => {
  if (String(input).endsWith('/complete')) completeBodies.push(init?.body);
  return realFetch(input, init);
}) as typeof fetch;

const client = new APIClient(API, mem());
const created: string[] = [];
let other: APIClient;

beforeAll(async () => {
  // Fail loudly (not skip) if the stack is down: a green run must mean the real path was exercised.
  expect(await client.checkBackendHealth(), `Go API not healthy at ${API}; run docker compose up in Backend/`).toBe(true);
  await client.ensureSession();
});

afterAll(async () => {
  for (const id of created) await client.deleteMedia(id).catch(() => {}); // idempotent; worker removes objects
});

for (const [file, type] of [['gradient.jpg', 'image/jpeg'], ['gradient.png', 'image/png']] as const) {
  describe(`upload lifecycle (${type})`, () => {
    it('intent -> PUT -> complete {} -> stored copy -> delete -> inaccessible', async () => {
      const blob = fixture(file, type);
      const intent = await client.createUploadIntent(file, type, blob.size);
      created.push(intent.assetId);
      expect(intent.upload.method).toBe('PUT');

      // Browser-style cross-origin preflight against the signed storage URL.
      const pre = await realFetch(intent.upload.url, { method: 'OPTIONS', headers: { Origin: ORIGIN, 'Access-Control-Request-Method': 'PUT', 'Access-Control-Request-Headers': 'content-type' } });
      expect(pre.headers.get('access-control-allow-origin')).toBe(ORIGIN);

      await client.uploadBytes(intent.upload.url, intent.upload.method, intent.upload.headers, blob);
      const done = await client.completeMedia(intent.assetId);
      expect(done.status).toBe('ready');
      expect(completeBodies.at(-1)).toBe('{}');

      const asset = await client.getMedia(intent.assetId);
      expect(asset.status).toBe('ready');
      const img = await realFetch(asset.download_url!);
      expect(img.status).toBe(200);
      expect(img.headers.get('content-type')).toBe('image/jpeg'); // server normalizes to JPEG
      expect((await img.arrayBuffer()).byteLength).toBeGreaterThan(100);

      // Another anonymous session must not see it.
      other = new APIClient(API, mem());
      await other.ensureSession();
      await expect(other.getMedia(intent.assetId)).rejects.toMatchObject({ status: 404 });

      expect((await client.deleteMedia(intent.assetId)).status).toBe('deletion_pending');
      await expect(client.getMedia(intent.assetId)).rejects.toMatchObject({ status: 404 });
      // Repeated delete is idempotent on the server.
      expect((await client.deleteMedia(intent.assetId)).status).toMatch(/deletion_pending|deleted/);
    });
  });
}

describe('failure paths against the real stack', () => {
  it('complete before the object is uploaded fails (409), not success', async () => {
    const intent = await client.createUploadIntent('x.jpg', 'image/jpeg', 100);
    created.push(intent.assetId);
    await expect(client.completeMedia(intent.assetId)).rejects.toMatchObject({ status: 409 });
  });
  it('storage rejects a body that does not match the declared size', async () => {
    const intent = await client.createUploadIntent('x.jpg', 'image/jpeg', 100);
    created.push(intent.assetId);
    await expect(client.uploadBytes(intent.upload.url, 'PUT', intent.upload.headers, new Blob([new Uint8Array(5)], { type: 'image/jpeg' }))).rejects.toThrow();
  });
  it('non-image bytes are rejected on complete (422)', async () => {
    const bytes = new Blob([new Uint8Array(200)], { type: 'image/jpeg' });
    const intent = await client.createUploadIntent('x.jpg', 'image/jpeg', bytes.size);
    created.push(intent.assetId);
    await client.uploadBytes(intent.upload.url, 'PUT', intent.upload.headers, bytes);
    await expect(client.completeMedia(intent.assetId)).rejects.toMatchObject({ status: 422 });
  });
  it('a token the server rejects yields SessionExpiredError and the client drops it', async () => {
    const c = new APIClient(API, mem());
    await c.ensureSession();
    (c as any).session = { ...c.getSession()!, token: 'invalid-token' };
    await expect(c.getMedia(created[0])).rejects.toBeInstanceOf(SessionExpiredError);
    expect(c.getSession()).toBeNull();
  });
  it('unreachable API: health is false and operations error', async () => {
    const dead = new APIClient('http://127.0.0.1:1', mem());
    expect(await dead.checkBackendHealth()).toBe(false);
    await expect(dead.ensureSession()).rejects.toThrow(/Cannot reach/);
  });
  it('ApiError carries the server status', () => expect(new ApiError(400, 'X', 'm').status).toBe(400));
});
