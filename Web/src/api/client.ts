import { UserSession, UploadIntent, MediaAsset, Generation, GenerationHistoryItem } from '../types';

const SESSION_STORAGE_KEY = 'abt_session_v2';

class APIClient {
  private session: UserSession | null = null;
  private sessionPromise: Promise<UserSession> | null = null;
  private isBackendOnline: boolean | null = null;

  constructor() {
    this.loadSession();
  }

  private loadSession(): void {
    try {
      const saved = localStorage.getItem(SESSION_STORAGE_KEY);
      if (saved) {
        const parsed = JSON.parse(saved) as UserSession;
        if (new Date(parsed.expiresAt).getTime() > Date.now() + 60000) {
          this.session = parsed;
        } else {
          localStorage.removeItem(SESSION_STORAGE_KEY);
        }
      }
    } catch {
      localStorage.removeItem(SESSION_STORAGE_KEY);
    }
  }

  public getSession(): UserSession | null {
    return this.session;
  }

  public clearSession(): void {
    this.session = null;
    localStorage.removeItem(SESSION_STORAGE_KEY);
  }

  public async checkBackendHealth(): Promise<boolean> {
    try {
      const controller = new AbortController();
      const id = setTimeout(() => controller.abort(), 2000);
      const res = await fetch('/health/live', { signal: controller.signal });
      clearTimeout(id);
      if (!res.ok) {
        this.isBackendOnline = false;
        return false;
      }
      const data = await res.json().catch(() => null);
      const isOnline = !!data && data.status === 'ok' && data.backend_connected !== false;
      this.isBackendOnline = isOnline;
      return isOnline;
    } catch {
      this.isBackendOnline = false;
      return false;
    }
  }

  public getBackendStatus(): boolean | null {
    return this.isBackendOnline;
  }

  public async ensureSession(): Promise<UserSession> {
    if (this.session && new Date(this.session.expiresAt).getTime() > Date.now() + 60000) {
      return this.session;
    }
    if (this.sessionPromise) {
      return this.sessionPromise;
    }

    this.sessionPromise = (async () => {
      try {
        const res = await fetch('/v1/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({})
        });

        if (!res.ok) {
          const err = await res.json().catch(() => ({}));
          throw new Error(err.error?.message || `Failed to create session: ${res.status}`);
        }
        this.isBackendOnline = true;

        const data = await res.json();
        const newSession: UserSession = {
          userId: data.user.id,
          token: data.session.token,
          expiresAt: data.session.expires_at
        };

        this.session = newSession;
        this.isBackendOnline = true;
        localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(newSession));
        return newSession;
      } catch (err) {
        this.isBackendOnline = false;
        throw err;
      } finally {
        this.sessionPromise = null;
      }
    })();

    return this.sessionPromise;
  }

  private async authorizedFetch<T>(url: string, options: RequestInit = {}): Promise<T> {
    const session = await this.ensureSession();
    const headers = new Headers(options.headers || {});
    headers.set('Authorization', `Bearer ${session.token}`);
    headers.set('Accept', 'application/json');

    let response: Response;
    try {
      response = await fetch(url, { ...options, headers });
    } catch (err: any) {
      this.isBackendOnline = false;
      throw new Error(`Cannot reach the ABT API (${url}): ${err.message}. Is the Go backend running?`);
    }

    // Handle session expired / revoked (401)
    if (response.status === 401) {
      this.clearSession();
      const freshSession = await this.ensureSession();
      headers.set('Authorization', `Bearer ${freshSession.token}`);
      response = await fetch(url, { ...options, headers });
    }

    if (!response.ok) {
      const errData = await response.json().catch(() => ({}));
      const message = errData.error?.message || `Request failed with code ${response.status}`;
      throw new Error(message);
    }

    return response.json();
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
