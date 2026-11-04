// END-TO-END against the real local stack (Go API + PostgreSQL + storage + worker). The worker runs the MOCK provider:
// this proves the pipeline (API -> job -> worker -> storage -> result), NOT that any clothing was edited by AI.
import { readFileSync } from 'node:fs';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { APIClient } from '../src/api/client';

const API = process.env.ABT_API_URL ?? 'http://127.0.0.1:8080';
const mem = () => { const m = new Map<string, string>(); return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) }; };
const client = new APIClient(API, mem());
const jpg = () => new Blob([readFileSync(new URL('./fixtures/gradient.jpg', import.meta.url))], { type: 'image/jpeg' });
const created: string[] = [];

async function readyAsset() {
  const blob = jpg();
  const intent = await client.createUploadIntent('g.jpg', 'image/jpeg', blob.size);
  created.push(intent.assetId);
  await client.uploadBytes(intent.upload.url, intent.upload.method, intent.upload.headers, blob);
  await client.completeMedia(intent.assetId);
  return intent.assetId;
}

async function waitDone(id: string, ms = 30_000) {
  const end = Date.now() + ms;
  for (;;) {
    const g = await client.getGeneration(id);
    if (!['queued', 'processing'].includes(g.status)) return g;
    if (Date.now() > end) throw new Error(`still ${g.status} after ${ms}ms: is the worker running?`);
    await new Promise(r => setTimeout(r, 500));
  }
}

beforeAll(async () => {
  expect(await client.checkBackendHealth(), 'Go API not healthy; run docker compose up in Backend/').toBe(true);
  await client.ensureSession();
});
afterAll(async () => { for (const id of created) await client.deleteMedia(id).catch(() => {}); });

describe('generation M2 (mock provider)', () => {
  it('presets -> create -> idempotent repeat -> worker -> result differs from source -> delete result', async () => {
    const presets = await client.getPresets();
    expect(presets.map(p => p.id)).toContain('old_money');
    expect(JSON.stringify(presets)).not.toMatch(/prompt/i);

    const src = await readyAsset();
    const key = crypto.randomUUID();
    const a = await client.createGeneration(src, 'old_money', 'navy palette', key);
    const b = await client.createGeneration(src, 'old_money', 'navy palette', key);
    expect(b.id).toBe(a.id);
    await expect(client.createGeneration(src, 'streetwear', '', key)).rejects.toMatchObject({ status: 409, code: 'IDEMPOTENCY_CONFLICT' });

    const g = await waitDone(a.id);
    expect(g.status).toBe('completed');
    expect(g.result?.is_mock).toBe(true);
    const after = new Uint8Array(await (await fetch(g.result!.download_url)).arrayBuffer());
    const srcAsset = await client.getMedia(src);
    const before = new Uint8Array(await (await fetch(srcAsset.download_url!)).arrayBuffer());
    expect(after.length).toBeGreaterThan(100);
    expect(Buffer.from(after).equals(Buffer.from(before))).toBe(false);
    created.push(g.result!.asset_id);

    await client.deleteMedia(g.result!.asset_id);
    expect((await client.getGeneration(a.id)).result).toBeNull();
  });

  it('another session cannot read the generation', async () => {
    const src = await readyAsset();
    const g = await client.createGeneration(src, 'minimalist', '', crypto.randomUUID());
    const other = new APIClient(API, mem());
    await other.ensureSession();
    await expect(other.getGeneration(g.id)).rejects.toMatchObject({ status: 404 });
    await client.cancelGeneration(g.id);
  });

  it('unknown preset and unusable source are rejected', async () => {
    const src = await readyAsset();
    await expect(client.createGeneration(src, 'nope', '', crypto.randomUUID())).rejects.toMatchObject({ code: 'PRESET_NOT_FOUND' });
    await expect(client.createGeneration(crypto.randomUUID(), 'old_money', '', crypto.randomUUID())).rejects.toMatchObject({ status: 404 });
  });
});
