// MOCKED tests: fetch is stubbed. These prove client behaviour, not that Go/storage work (see e2e/).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIClient, SessionExpiredError } from './client';

const memStore = () => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
};
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const sessionBody = { user: { id: 'u1' }, session: { token: 'tok-secret', expires_at: new Date(Date.now() + 3600_000).toISOString() } };

afterEach(() => vi.unstubAllGlobals());

async function loggedIn(fetchMock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('fetch', fetchMock);
  const c = new APIClient('', memStore());
  fetchMock.mockResolvedValueOnce(json(sessionBody, 201));
  await c.ensureSession();
  fetchMock.mockClear();
  return c;
}

describe('health', () => {
  it('rejects a foreign server answering "hello from server"', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('hello from server', { status: 200 })));
    expect(await new APIClient('', memStore()).checkBackendHealth()).toBe(false);
  });
  it('rejects JSON without status ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ status: 'offline' })));
    expect(await new APIClient('', memStore()).checkBackendHealth()).toBe(false);
  });
  it('is false when the API is unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed')));
    expect(await new APIClient('', memStore()).checkBackendHealth()).toBe(false);
  });
  it('is true for {"status":"ok"}', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ status: 'ok' })));
    expect(await new APIClient('', memStore()).checkBackendHealth()).toBe(true);
  });
});

describe('session', () => {
  it('does not invent an offline session when creation fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed')));
    const c = new APIClient('', memStore());
    await expect(c.ensureSession()).rejects.toThrow(/Cannot reach the ABT API/);
    expect(c.getSession()).toBeNull();
  });
  it('401 drops the token, throws SessionExpiredError and does NOT retry or create a session', async () => {
    const f = vi.fn();
    const c = await loggedIn(f);
    f.mockResolvedValue(json({ error: { code: 'UNAUTHORIZED', message: 'x', request_id: 'r' } }, 401));
    await expect(c.deleteMedia('a1')).rejects.toBeInstanceOf(SessionExpiredError);
    expect(f).toHaveBeenCalledTimes(1); // no POST /v1/sessions, no replay of the DELETE
    expect(c.getSession()).toBeNull();
  });
  it('without a session no request is sent', async () => {
    const f = vi.fn();
    vi.stubGlobal('fetch', f);
    await expect(new APIClient('', memStore()).getMedia('a1')).rejects.toBeInstanceOf(SessionExpiredError);
    expect(f).not.toHaveBeenCalled();
  });
  it('never puts the token in an error message', async () => {
    const f = vi.fn();
    const c = await loggedIn(f);
    f.mockResolvedValue(json({ error: { code: 'X', message: 'boom', request_id: 'r' } }, 500));
    const err = await c.getMedia('a1').catch(e => e);
    expect(String(err.message)).not.toContain('tok-secret');
  });
});

describe('media', () => {
  it('complete sends exactly {} (Go rejects unknown fields)', async () => {
    const f = vi.fn();
    const c = await loggedIn(f);
    f.mockResolvedValue(json({ id: 'a1', status: 'ready', width: 1, height: 1 }));
    await c.completeMedia('a1');
    expect(f.mock.calls[0][0]).toBe('/v1/media/a1/complete');
    expect(f.mock.calls[0][1].body).toBe('{}');
  });
  it('a failed storage PUT is an error, not success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 403 })));
    await expect(new APIClient('', memStore()).uploadBytes('http://s/x', 'PUT', {}, new Blob(['x']))).rejects.toThrow(/status 403/);
  });
  it('a blocked/unreachable storage PUT names CORS/endpoint without leaking the signed URL', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    const err = await new APIClient('', memStore()).uploadBytes('http://s/x?X-Amz-Signature=SECRET', 'PUT', {}, new Blob(['x'])).catch(e => e);
    expect(err.message).toMatch(/CORS/);
    expect(err.message).not.toContain('SECRET');
  });
});
