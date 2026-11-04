import React, { useRef, useState } from 'react';
import { Camera, Upload, CheckCircle2, AlertTriangle, Trash2, RefreshCw, Sparkles } from 'lucide-react';
import { api } from '../api/client';
import { MediaAsset } from '../types';

export type UploadPhase =
  | { type: 'idle' }
  | { type: 'preparing' }
  | { type: 'uploading' }
  | { type: 'validating' }
  | { type: 'ready'; assetId: string; downloadUrl?: string }
  | { type: 'deleting' }
  | { type: 'failed'; message: string; retryable: boolean };

interface PhotoUploadSectionProps {
  phase: UploadPhase;
  previewUrl: string | null;
  onPhaseChange: (phase: UploadPhase) => void;
  onPreviewChange: (url: string | null) => void;
  onAssetReady: (asset: MediaAsset | null) => void;
}

// Built-in curated sample portraits for instant testing
const SAMPLE_PORTRAITS = [
  {
    name: 'Classic Portrait',
    desc: 'Editorial portrait in natural lighting',
    url: 'https://images.unsplash.com/photo-1534528741775-53994a69daeb?auto=format&fit=crop&w=800&q=80'
  },
  {
    name: 'Casual Street',
    desc: 'Full-profile relaxed streetwear pose',
    url: 'https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?auto=format&fit=crop&w=800&q=80'
  },
  {
    name: 'Business Style',
    desc: 'Modern professional studio shot',
    url: 'https://images.unsplash.com/photo-1573496359142-b8d87734a5a2?auto=format&fit=crop&w=800&q=80'
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
  onAssetReady
}) => {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [lastBlob, setLastBlob] = useState<{ blob: Blob; filename: string } | null>(null);

  const isBusy = ['preparing', 'uploading', 'validating', 'deleting'].includes(phase.type);

  // Process raw photo data:
  // 1. Prepare / validate preview
  // Prepare preview, then: upload-intent -> direct PUT to storage -> complete -> GET stored copy.
  const handlePhotoSelected = async (fileOrBlob: Blob, filename: string) => {
    try {
      setLastBlob({ blob: fileOrBlob, filename });
      onPhaseChange({ type: 'preparing' });

      // Local preview URL
      const localUrl = URL.createObjectURL(fileOrBlob);
      onPreviewChange(localUrl);

      // Validate size (max 10MB)
      if (fileOrBlob.size > 10 * 1024 * 1024) {
        onPhaseChange({
          type: 'failed',
          message: 'This photo exceeds the 10MB limit. Please choose a smaller photo.',
          retryable: false
        });
        return;
      }

      onPhaseChange({ type: 'uploading' });

      const contentType = fileOrBlob.type;
      if (contentType !== 'image/jpeg' && contentType !== 'image/png') {
        onPhaseChange({ type: 'failed', message: 'Only JPEG or PNG photos are supported.', retryable: false });
        return;
      }
      const intent = await api.createUploadIntent(filename, contentType, fileOrBlob.size);
      await api.uploadBytes(intent.upload.url, intent.upload.method, intent.upload.headers, fileOrBlob);
      onPhaseChange({ type: 'validating' });
      await api.completeMedia(intent.assetId);
      const asset = await api.getMedia(intent.assetId);
      // Show the stored copy, not the local file, so the preview proves the round trip.
      URL.revokeObjectURL(localUrl);
      onPreviewChange(asset.download_url);
      onPhaseChange({ type: 'ready', assetId: asset.id, downloadUrl: asset.download_url || undefined });
      onAssetReady(asset);
    } catch (err: any) {
      console.error('Photo processing failed:', err);
      onPhaseChange({
        type: 'failed',
        message: err.message || 'We could not process that photo. Please try a different image.',
        retryable: true
      });
    }
  };

  const handleFileInput = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    handlePhotoSelected(file, file.name);
  };

  const handleSelectSample = async (sampleUrl: string, sampleName: string) => {
    try {
      onPhaseChange({ type: 'preparing' });
      const resp = await fetch(sampleUrl, { mode: 'cors' });
      if (!resp.ok) throw new Error('Fetch failed');
      const blob = await resp.blob();
      await handlePhotoSelected(blob, `${sampleName.toLowerCase().replace(/\s+/g, '_')}.jpg`);
    } catch {
      // Fallback: create self-contained canvas sample portrait to avoid CORS issues
      try {
        const fallbackBlob = await createSamplePortraitBlob(sampleName);
        await handlePhotoSelected(fallbackBlob, `${sampleName.toLowerCase().replace(/\s+/g, '_')}.jpg`);
      } catch (err: any) {
        onPhaseChange({
          type: 'failed',
          message: 'Could not load sample. Please upload a photo from your device.',
          retryable: true
        });
      }
    }
  };

  const handleDelete = async () => {
    if (phase.type !== 'ready') return;
    try {
      const assetId = phase.assetId;
      onPhaseChange({ type: 'deleting' });
      await api.deleteMedia(assetId);
      // Reset
      onPreviewChange(null);
      setLastBlob(null);
      onAssetReady(null);
      onPhaseChange({ type: 'idle' });
    } catch (err: any) {
      onPhaseChange({ type: 'ready', assetId: phase.assetId, downloadUrl: phase.downloadUrl });
      alert(err.message || 'Failed to delete photo.');
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

        {phase.type === 'failed' && (
          <div className="bg-red-950/40 border border-red-800/40 rounded-xl p-3 flex items-center justify-between text-xs">
            <div className="flex items-center gap-2.5 text-red-300">
              <AlertTriangle className="w-4 h-4 text-red-400 shrink-0" />
              <span>{phase.message}</span>
            </div>
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
                onClick={() => handleSelectSample(sample.url, sample.name)}
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
