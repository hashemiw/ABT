import { UserSession, UploadIntent, MediaAsset, Generation, GenerationHistoryItem } from '../types';

const SESSION_STORAGE_KEY = 'abt_session_v2';

type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/** The token was rejected (or is missing/expired). The caller must stop and ask the user; nothing is retried. */
export class SessionExpiredError extends Error {
  constructor() {
    super('Your session has expired. Photos from the previous session are no longer accessible.');
    this.name = 'SessionExpiredError';
  }
}

export class ApiError extends Error {
  constructor(public status: number, public code: string | undefined, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

export class APIClient {
  private session: UserSession | null = null;
  private sessionPromise: Promise<UserSession> | null = null;
  private isBackendOnline: boolean | null = null;

  // base and store are injectable so tests can target a real Go server from Node.
  constructor(private base = '', private store: Store | null = typeof window === 'undefined' ? null : window.localStorage) {
    this.loadSession();
  }

  private loadSession(): void {
    try {
      const saved = this.store?.getItem(SESSION_STORAGE_KEY);
      if (saved) {
        const parsed = JSON.parse(saved) as UserSession;
        if (new Date(parsed.expiresAt).getTime() > Date.now() + 60000) this.session = parsed;
        else this.store?.removeItem(SESSION_STORAGE_KEY);
      }
    } catch {
      this.store?.removeItem(SESSION_STORAGE_KEY);
    }
  }

  public getSession(): UserSession | null {
    return this.session;
  }

  /** Drop the token. Assets owned by it become unreachable from this client. */
  public clearSession(): void {
    this.session = null;
    try { this.store?.removeItem(SESSION_STORAGE_KEY); } catch { /* storage unavailable */ }
  }

  /** True only for a real ABT API: HTTP 200 and exactly {"status":"ok"}. Anything else (text, other server) is offline. */
  public async checkBackendHealth(): Promise<boolean> {
    const controller = new AbortController();
    const id = setTimeout(() => controller.abort(), 2000);
    try {
      const res = await fetch(`${this.base}/health/live`, { signal: controller.signal });
      const data = res.ok ? await res.json().catch(() => null) : null;
      this.isBackendOnline = !!data && data.status === 'ok';
    } catch {
      this.isBackendOnline = false;
    } finally {
      clearTimeout(id);
    }
    return this.isBackendOnline;
  }

  public getBackendStatus(): boolean | null {
    return this.isBackendOnline;
  }

  /** Returns the current session or creates one. Only called at startup or on explicit user action, never mid-operation. */
  public async ensureSession(): Promise<UserSession> {
    if (this.session && new Date(this.session.expiresAt).getTime() > Date.now() + 60000) return this.session;
    this.clearSession();
    this.sessionPromise ??= this.createSession().finally(() => { this.sessionPromise = null; });
    return this.sessionPromise;
  }

  private async createSession(): Promise<UserSession> {
    const res = await this.request('/v1/sessions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
    const data = await res.json();
    const session: UserSession = { userId: data.user.id, token: data.session.token, expiresAt: data.session.expires_at };
    this.session = session;
    try { this.store?.setItem(SESSION_STORAGE_KEY, JSON.stringify(session)); } catch { /* storage unavailable */ }
    return session;
  }

  private async request(path: string, init: RequestInit): Promise<Response> {
    let res: Response;
    try {
      res = await fetch(this.base + path, init);
    } catch (err: any) {
      this.isBackendOnline = false;
      throw new Error(`Cannot reach the ABT API: ${err.message}. Is the Go backend running?`);
    }
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new ApiError(res.status, body.error?.code, body.error?.message || `Request failed (${res.status})`);
    }
    return res;
  }

  private async authorizedFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
    const session = this.session;
    if (!session || new Date(session.expiresAt).getTime() <= Date.now()) {
      this.clearSession();
      throw new SessionExpiredError();
    }
    const headers = new Headers(options.headers || {});
    headers.set('Authorization', `Bearer ${session.token}`);
    headers.set('Accept', 'application/json');
    try {
      return await (await this.request(path, { ...options, headers })).json();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        this.clearSession();
        throw new SessionExpiredError();
      }
      throw err;
    }
  }

  // Upload lifecycle
  public async createUploadIntent(filename: string, contentType: string, sizeBytes: number): Promise<UploadIntent> {
    const data = await this.authorizedFetch<{ asset_id: string; upload: UploadIntent['upload'] }>('/v1/media/upload-intents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filename, content_type: contentType, size_bytes: sizeBytes })
    });
    return {
      assetId: data.asset_id,
      upload: data.upload
    };
  }

  public async uploadBytes(uploadUrl: string, method: string, headers: Record<string, string>, data: Blob | ArrayBuffer): Promise<void> {
    let res: Response;
    try {
      res = await fetch(uploadUrl, { method, headers: { ...headers }, body: data });
    } catch (err: any) {
      // A network-level failure here is usually storage CORS or an unreachable STORAGE_PUBLIC_ENDPOINT.
      throw new Error(`Direct upload to storage was blocked or unreachable (${err.message}). Check storage CORS and STORAGE_PUBLIC_ENDPOINT.`);
    }
    if (!res.ok) {
      throw new Error(`Upload failed with status ${res.status}`);
    }
  }

  public async completeMedia(assetId: string): Promise<{ id: string; status: string; width: number; height: number }> {
    return this.authorizedFetch(`/v1/media/${assetId}/complete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}'
    });
  }

  public async getMedia(assetId: string): Promise<MediaAsset> {
    return this.authorizedFetch<MediaAsset>(`/v1/media/${assetId}`);
  }

  public async deleteMedia(assetId: string): Promise<{ id: string; status: string }> {
    return this.authorizedFetch(`/v1/media/${assetId}`, {
      method: 'DELETE'
    });
  }

  // Generations
  public async createGeneration(sourceAssetId: string, presetId: string, instruction?: string, parentGenerationId?: string | null): Promise<{ id: string; status: string; status_url: string }> {
    const idempotencyKey = crypto.randomUUID();
    return this.authorizedFetch('/v1/generations', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Idempotency-Key': idempotencyKey
      },
      body: JSON.stringify({
        source_asset_id: sourceAssetId,
        preset_id: presetId,
        instruction: instruction || '',
        parent_generation_id: parentGenerationId || null
      })
    });
  }

  public async getGeneration(generationId: string): Promise<Generation> {
    return this.authorizedFetch<Generation>(`/v1/generations/${generationId}`);
  }

  public async listGenerations(): Promise<GenerationHistoryItem[]> {
    const data = await this.authorizedFetch<{ data: GenerationHistoryItem[] }>('/v1/generations');
    return data.data;
  }

  public async cancelGeneration(generationId: string): Promise<{ id: string; status: string }> {
    return this.authorizedFetch(`/v1/generations/${generationId}/cancel`, {
      method: 'POST'
    });
  }

  public async submitFeedback(generationId: string, rating: number, comment?: string): Promise<void> {
    await this.authorizedFetch('/v1/feedback', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        generation_id: generationId,
        rating,
        comment
      })
    });
  }
}

export const api = new APIClient();
