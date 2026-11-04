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
  tags?: string[];
  accent_color?: string;
}

export interface Generation {
  id: string;
  preset_id?: string;
  presetId?: string;
  status: 'queued' | 'processing' | 'completed' | 'failed' | 'canceled';
  created_at: string;
  started_at?: string | null;
  completed_at?: string | null;
  result?: {
    asset_id: string;
    download_url: string;
    provider?: string;
    model?: string;
  } | null;
  error?: {
    code: string;
    message: string;
  } | null;
}

export interface GenerationHistoryItem {
  id: string;
  status: 'queued' | 'processing' | 'completed' | 'failed' | 'canceled';
  preset_id: string;
  created_at: string;
  result_asset_id: string | null;
  download_url: string | null;
}
