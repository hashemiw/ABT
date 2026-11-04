export interface UserSession {
  userId: string;
  token: string;
  expiresAt: string;
}

export interface UploadIntent {
  assetId: string;
  upload: {
    url: string;
    method: string;
    headers: Record<string, string>;
    expires_at: string;
  };
}

export interface MediaAsset {
  id: string;
  kind: 'original' | 'result';
  status: 'pending_upload' | 'validating' | 'ready' | 'rejected' | 'deletion_pending' | 'deleted';
  width: number | null;
  height: number | null;
  content_type: string | null;
  download_url: string | null;
  download_url_expires_at: string | null;
}

export interface StylePreset {
  id: string;
  name: string;
  category: string;
  description: string;
}

export type GenerationStatus = 'queued' | 'processing' | 'completed' | 'failed' | 'canceled';

export interface Generation {
  id: string;
  status: GenerationStatus;
  preset_id: string;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
  result: {
    asset_id: string;
    download_url: string;
    download_url_expires_at: string;
    provider: string;
    model: string;
    is_mock: boolean;
  } | null;
  error: { code: string; message: string } | null;
}
