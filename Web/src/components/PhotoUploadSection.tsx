import React, { useRef, useState } from 'react';
import { Camera, Upload, CheckCircle2, AlertTriangle, Trash2, RefreshCw, Sparkles } from 'lucide-react';
import { api, SessionExpiredError } from '../api/client';
import { MediaAsset } from '../types';

export type UploadPhase =
  | { type: 'idle' }
  | { type: 'preparing' }
  | { type: 'uploading' }
  | { type: 'validating' }
  | { type: 'ready'; assetId: string; downloadUrl?: string }
  | { type: 'deleting' }
  | { type: 'failed'; message: string; retryable: boolean; sessionExpired?: boolean };

interface PhotoUploadSectionProps {
  phase: UploadPhase;
  previewUrl: string | null;
  onPhaseChange: (phase: UploadPhase) => void;
  onPreviewChange: (url: string | null) => void;
  onAssetReady: (asset: MediaAsset | null) => void;
  /** Session rejected: parent must drop session state. A new session is created only on explicit user action. */
  onSessionExpired: () => void;
  onNewSession: () => void;
}

// Built-in curated sample portraits for instant testing
const SAMPLE_PORTRAITS = [
  {
    name: 'Classic Portrait',
    desc: 'Editorial portrait in natural lighting',
  },
  {
    name: 'Casual Street',
    desc: 'Full-profile relaxed streetwear pose',
  },
  {
    name: 'Business Style',
    desc: 'Modern professional studio shot',
  }
];

// Fallback generator if remote image fetching is blocked by CORS
function createSamplePortraitBlob(sampleName: string): Promise<Blob> {
  return new Promise((resolve) => {
    const canvas = document.createElement('canvas');
    canvas.width = 720;
    canvas.height = 960;
    const ctx = canvas.getContext('2d')!;

    // Background studio gradient
    const grad = ctx.createLinearGradient(0, 0, 0, 960);
    grad.addColorStop(0, '#1e293b');
    grad.addColorStop(1, '#0f172a');
    ctx.fillStyle = grad;
    ctx.fillRect(0, 0, 720, 960);

    // Subtle rim lighting
    const rim = ctx.createRadialGradient(360, 380, 50, 360, 480, 400);
    rim.addColorStop(0, 'rgba(99, 102, 241, 0.25)');
    rim.addColorStop(1, 'rgba(15, 23, 42, 0)');
    ctx.fillStyle = rim;
    ctx.fillRect(0, 0, 720, 960);

    // Shoulders & Body
    ctx.fillStyle = '#334155';
    ctx.beginPath();
    ctx.ellipse(360, 800, 260, 200, 0, 0, Math.PI * 2);
    ctx.fill();

    // Neck
    ctx.fillStyle = '#e2b397';
    ctx.fillRect(320, 480, 80, 100);

    // Head
    ctx.fillStyle = '#f3c9af';
    ctx.beginPath();
    ctx.ellipse(360, 380, 110, 140, 0, 0, Math.PI * 2);
    ctx.fill();

    // Hair
    ctx.fillStyle = '#1e1b18';
    ctx.beginPath();
    ctx.ellipse(360, 290, 120, 80, 0, 0, Math.PI * 2);
    ctx.fill();

    // Eyes
    ctx.fillStyle = '#262626';
    ctx.beginPath();
    ctx.ellipse(315, 375, 10, 6, 0, 0, Math.PI * 2);
    ctx.ellipse(405, 375, 10, 6, 0, 0, Math.PI * 2);
    ctx.fill();

    // Eyebrows
    ctx.strokeStyle = '#262626';
    ctx.lineWidth = 4;
    ctx.beginPath();
    ctx.arc(315, 360, 16, Math.PI * 1.1, Math.PI * 1.9);
    ctx.stroke();
    ctx.beginPath();
    ctx.arc(405, 360, 16, Math.PI * 1.1, Math.PI * 1.9);
    ctx.stroke();

    // Nose
    ctx.strokeStyle = '#d49b7d';
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.moveTo(360, 370);
    ctx.lineTo(355, 410);
    ctx.lineTo(365, 410);
    ctx.stroke();

    // Smile
    ctx.strokeStyle = '#b91c1c';
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.arc(360, 435, 20, 0.1 * Math.PI, 0.9 * Math.PI);
    ctx.stroke();

    // Clothing collar
    ctx.fillStyle = '#4f46e5';
    ctx.beginPath();
    ctx.moveTo(310, 560);
    ctx.lineTo(360, 640);
    ctx.lineTo(410, 560);
    ctx.closePath();
    ctx.fill();

    // Label
    ctx.fillStyle = '#94a3b8';
    ctx.font = '14px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText(`Sample Portrait: ${sampleName}`, 360, 920);

    canvas.toBlob((blob) => {
      resolve(blob || new Blob([], { type: 'image/jpeg' }));
    }, 'image/jpeg', 0.92);
  });
}

export const PhotoUploadSection: React.FC<PhotoUploadSectionProps> = ({
  phase,
  previewUrl,
  onPhaseChange,
  onPreviewChange,
  onAssetReady,
  onSessionExpired,
  onNewSession
}) => {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [lastBlob, setLastBlob] = useState<{ blob: Blob; filename: string } | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const inFlight = useRef(false); // blocks double clicks before React re-renders

  const isBusy = ['preparing', 'uploading', 'validating', 'deleting'].includes(phase.type);

  // Failures are shown, never converted to success. An expired session stops the operation; nothing is replayed.
  const fail = (err: any) => {
    if (err instanceof SessionExpiredError) {
      onPreviewChange(null);
      onAssetReady(null);
      onSessionExpired();
      onPhaseChange({ type: 'failed', message: err.message, retryable: false, sessionExpired: true });
      return;
    }
    onPhaseChange({ type: 'failed', message: err?.message || 'We could not process that photo.', retryable: true });
  };

  // upload-intent -> direct PUT to storage -> complete -> GET stored copy.
  const handlePhotoSelected = async (fileOrBlob: Blob, filename: string) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setNotice(null);
    let assetId: string | null = null;
    try {
      setLastBlob({ blob: fileOrBlob, filename });
      onPhaseChange({ type: 'preparing' });
      const localUrl = URL.createObjectURL(fileOrBlob);
      onPreviewChange(localUrl);

      if (fileOrBlob.size > 10 * 1024 * 1024) {
        onPhaseChange({ type: 'failed', message: 'This photo exceeds the 10MB limit. Please choose a smaller photo.', retryable: false });
        return;
      }
      const contentType = fileOrBlob.type;
      if (contentType !== 'image/jpeg' && contentType !== 'image/png') {
        onPhaseChange({ type: 'failed', message: 'Only JPEG or PNG photos are supported.', retryable: false });
        return;
      }

      onPhaseChange({ type: 'uploading' });
      const intent = await api.createUploadIntent(filename, contentType, fileOrBlob.size);
      assetId = intent.assetId;
      await api.uploadBytes(intent.upload.url, intent.upload.method, intent.upload.headers, fileOrBlob);
      onPhaseChange({ type: 'validating' });
      await api.completeMedia(assetId);
      const asset = await api.getMedia(assetId);
      // Show the stored copy, not the local file, so the preview proves the round trip.
      URL.revokeObjectURL(localUrl);
      onPreviewChange(asset.download_url);
      onPhaseChange({ type: 'ready', assetId: asset.id, downloadUrl: asset.download_url || undefined });
      onAssetReady(asset);
    } catch (err: any) {
      // Best-effort cleanup of the half-finished asset; the user still sees the original error.
      if (assetId && !(err instanceof SessionExpiredError)) api.deleteMedia(assetId).catch(() => {});
      fail(err);
    } finally {
      inFlight.current = false;
    }
  };

  const handleFileInput = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    handlePhotoSelected(file, file.name);
  };

  // Samples are generated locally (synthetic, non-personal); nothing is fetched from third parties.
  const handleSelectSample = async (sampleName: string) => {
    const blob = await createSamplePortraitBlob(sampleName);
    await handlePhotoSelected(blob, `${sampleName.toLowerCase().replace(/\s+/g, '_')}.jpg`);
  };

  const handleDelete = async () => {
    if (phase.type !== 'ready' || inFlight.current) return;
    inFlight.current = true;
    const ready = phase;
    setNotice(null);
    try {
      onPhaseChange({ type: 'deleting' });
      await api.deleteMedia(ready.assetId);
      onPreviewChange(null);
      setLastBlob(null);
      onAssetReady(null);
      onPhaseChange({ type: 'idle' });
    } catch (err: any) {
      if (err instanceof SessionExpiredError) return fail(err);
      onPhaseChange(ready); // still stored: keep it visible and let the user retry
      setNotice(`Delete failed: ${err?.message || 'unknown error'}. Click Delete Photo to try again.`);
    } finally {
      inFlight.current = false;
    }
  };

  const handleRetry = () => {
    if (lastBlob) {
      handlePhotoSelected(lastBlob.blob, lastBlob.filename);
    }
  };

  return (
    <div className="bg-slate-900/70 border border-slate-800 rounded-2xl p-6 shadow-xl backdrop-blur-sm">
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-lg font-bold text-white flex items-center gap-2">
            <Camera className="w-5 h-5 text-indigo-400" />
            1. Your Photo
          </h2>
          <p className="text-xs text-slate-400">
            Upload a portrait photo to transform with your chosen style.
          </p>
        </div>
        {phase.type === 'ready' && (
          <span className="inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">
            <CheckCircle2 className="w-3.5 h-3.5" /> Photo Ready
          </span>
        )}
      </div>

      {/* Hidden File Input */}
      <input
        type="file"
        ref={fileInputRef}
        onChange={handleFileInput}
        accept="image/jpeg,image/png"
        className="hidden"
      />

      {/* Photo Preview Area */}
      <div className="relative group rounded-xl overflow-hidden bg-slate-950 border-2 border-dashed border-slate-800 hover:border-slate-700 transition-all flex flex-col items-center justify-center min-h-[300px]">
        {previewUrl ? (
          <div className="relative w-full h-[320px] bg-slate-950 flex items-center justify-center overflow-hidden">
            <img
              src={previewUrl}
              alt="Selected photo preview"
              className="max-h-full max-w-full object-contain rounded-lg shadow-md"
            />
            {/* Overlay if busy */}
            {isBusy && (
              <div className="absolute inset-0 bg-slate-950/80 backdrop-blur-sm flex flex-col items-center justify-center gap-3">
                <div className="w-10 h-10 border-3 border-indigo-500 border-t-transparent rounded-full animate-spin" />
                <span className="text-sm font-medium text-slate-200">
                  {phase.type === 'preparing' && 'Preparing your photo…'}
                  {phase.type === 'uploading' && 'Processing photo…'}
                  {phase.type === 'validating' && 'Validating framing…'}
                  {phase.type === 'deleting' && 'Deleting…'}
                </span>
              </div>
            )}
          </div>
        ) : (
          <div className="p-8 text-center flex flex-col items-center">
            <div className="w-16 h-16 rounded-2xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400 mb-4 group-hover:scale-105 transition-transform">
              <Upload className="w-8 h-8" />
            </div>
            <h3 className="font-semibold text-slate-200 text-sm mb-1">
              Choose a photo of yourself
            </h3>
            <p className="text-xs text-slate-400 max-w-xs mb-5">
              Select a JPEG or PNG photo. Clear portraits with visible clothing work best.
            </p>
            <button
              onClick={() => fileInputRef.current?.click()}
              disabled={isBusy}
              className="px-5 py-2.5 rounded-xl bg-indigo-600 hover:bg-indigo-500 text-white font-medium text-sm transition-all shadow-lg shadow-indigo-600/25 flex items-center gap-2 cursor-pointer"
            >
              <Upload className="w-4 h-4" />
              Choose From Device
            </button>
          </div>
        )}
      </div>

      {/* Status Feedback Area */}
      <div className="mt-4">
        {phase.type === 'ready' && (
          <div className="bg-emerald-950/40 border border-emerald-800/40 rounded-xl p-3 flex items-center justify-between text-xs">
            <div className="flex items-center gap-2.5 text-emerald-300">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <div>
                <span className="font-semibold">Photo ready for transformation</span>
                <p className="text-slate-400 text-[11px]">
                  {phase.downloadUrl?.startsWith('blob:')
                    ? 'Photo loaded & framed for transformation.'
                    : 'Normalized & secured in backend storage.'}
                </p>
              </div>
            </div>
            <span className="text-[11px] text-emerald-400 font-mono bg-emerald-950/60 px-2 py-0.5 rounded border border-emerald-800/60">
              Ready
            </span>
          </div>
        )}

        {notice && <p role="alert" className="text-xs text-red-300 mb-2">{notice}</p>}
        {phase.type === 'failed' && (
          <div className="bg-red-950/40 border border-red-800/40 rounded-xl p-3 flex items-center justify-between text-xs">
            <div className="flex items-center gap-2.5 text-red-300">
              <AlertTriangle className="w-4 h-4 text-red-400 shrink-0" />
              <span>{phase.message}</span>
            </div>
            {phase.sessionExpired && (
              <button
                onClick={onNewSession}
                className="px-2.5 py-1 rounded bg-red-800/40 hover:bg-red-800/60 text-red-200 font-medium text-xs cursor-pointer"
              >
                Start New Session
              </button>
            )}
            {phase.retryable && (
              <button
                onClick={handleRetry}
                className="px-2.5 py-1 rounded bg-red-800/40 hover:bg-red-800/60 text-red-200 font-medium text-xs flex items-center gap-1 transition-colors cursor-pointer"
              >
                <RefreshCw className="w-3 h-3" /> Try Again
              </button>
            )}
          </div>
        )}
      </div>

      {/* Actions & Sample Selector */}
      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 pt-3 border-t border-slate-800/80">
        <div className="flex items-center gap-2">
          {previewUrl && (
            <button
              onClick={() => fileInputRef.current?.click()}
              disabled={isBusy}
              className="px-3 py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs font-medium text-slate-300 transition-colors border border-slate-700 cursor-pointer"
            >
              Choose Different Photo
            </button>
          )}

          {phase.type === 'ready' && (
            <button
              onClick={handleDelete}
              disabled={isBusy}
              className="px-3 py-1.5 rounded-lg bg-red-950/40 hover:bg-red-900/50 text-red-400 hover:text-red-300 text-xs font-medium transition-colors border border-red-900/50 flex items-center gap-1.5 cursor-pointer"
            >
              <Trash2 className="w-3.5 h-3.5" />
              Delete Photo
            </button>
          )}
        </div>

        {/* Quick Sample Selector */}
        {!previewUrl && (
          <div className="flex items-center gap-1.5 text-xs text-slate-400">
            <span className="text-[11px] font-medium text-slate-500 mr-1 flex items-center gap-1">
              <Sparkles className="w-3 h-3 text-amber-400" /> Or try sample:
            </span>
            {SAMPLE_PORTRAITS.map(sample => (
              <button
                key={sample.name}
                onClick={() => handleSelectSample(sample.name)}
                disabled={isBusy}
                className="px-2.5 py-1 rounded bg-slate-800/80 hover:bg-slate-800 text-slate-300 hover:text-white border border-slate-700/60 text-[11px] transition-colors cursor-pointer"
              >
                {sample.name}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
