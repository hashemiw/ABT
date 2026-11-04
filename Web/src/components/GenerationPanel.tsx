import React, { useState } from 'react';
import { AlertTriangle, Loader2, Sparkles, XCircle } from 'lucide-react';
import { Generation } from '../types';

interface Props {
  generation: Generation | null;
  beforeUrl: string | null;
  presetName: string;
  /** Polling stopped (time limit or repeated errors) while the job is still unfinished. */
  pollingStopped: string | null;
  sessionExpired: boolean;
  onCancel: () => void;
  onCheckAgain: () => void;
  onRetry: () => void;
  onNewSession: () => void;
}

const stateText: Record<string, string> = {
  queued: 'Queued — waiting for a worker to pick this up.',
  processing: 'Processing — the image service is working on your photo.',
};

export const GenerationPanel: React.FC<Props> = ({ generation: g, beforeUrl, presetName, pollingStopped, sessionExpired, onCancel, onCheckAgain, onRetry, onNewSession }) => {
  const [pos, setPos] = useState(50);
  if (!g) return null;
  const active = g.status === 'queued' || g.status === 'processing';
  const mock = g.result?.is_mock;

  return (
    <section className="bg-slate-900/80 border border-slate-800 rounded-2xl p-6 mt-8" aria-live="polite">
      <div className="flex flex-wrap items-center gap-3 mb-4">
        <h2 className="text-xl font-bold text-white flex items-center gap-2"><Sparkles className="w-5 h-5 text-indigo-400" />3. Result</h2>
        <span className="text-xs px-2.5 py-0.5 rounded-full border border-slate-700 text-slate-300">{g.status.toUpperCase()}</span>
        {g.status === 'completed' && (
          mock
            ? <span className="text-xs px-2.5 py-0.5 rounded-full bg-amber-500/15 text-amber-300 border border-amber-500/30">MOCK PROVIDER — pipeline test, not a real AI edit</span>
            : <span className="text-xs px-2.5 py-0.5 rounded-full bg-emerald-500/10 text-emerald-300 border border-emerald-500/20">Real provider: {g.result?.provider} {g.result?.model}</span>
        )}
        <span className="text-xs text-slate-400">Style: {presetName}</span>
      </div>

      {active && (
        <div className="flex flex-wrap items-center gap-3 text-sm text-slate-300">
          <Loader2 className="w-4 h-4 animate-spin" />
          <span>{pollingStopped ?? stateText[g.status]}</span>
          {pollingStopped ? (
            <button onClick={onCheckAgain} className="px-3 py-1 rounded bg-slate-800 border border-slate-700 text-xs cursor-pointer">Check again</button>
          ) : null}
          <button onClick={onCancel} className="px-3 py-1 rounded bg-slate-800 border border-slate-700 text-xs cursor-pointer">Cancel</button>
        </div>
      )}

      {g.status === 'failed' && (
        <div role="alert" className="flex flex-wrap items-center gap-3 text-sm text-red-300">
          <AlertTriangle className="w-4 h-4" />
          <span>{g.error?.message ?? "We couldn't generate your image."} <span className="text-slate-500">({g.error?.code})</span></span>
          <button onClick={onRetry} className="px-3 py-1 rounded bg-red-900/40 border border-red-800/50 text-xs cursor-pointer">Try again</button>
        </div>
      )}

      {g.status === 'canceled' && (
        <div className="flex items-center gap-3 text-sm text-slate-300">
          <XCircle className="w-4 h-4" /> Canceled. Cancellation cannot guarantee the image service stopped working.
          <button onClick={onRetry} className="px-3 py-1 rounded bg-slate-800 border border-slate-700 text-xs cursor-pointer">Generate again</button>
        </div>
      )}

      {sessionExpired && (
        <p role="alert" className="text-sm text-red-300 mt-3">
          Your session expired, so this result can no longer be tracked. <button onClick={onNewSession} className="underline cursor-pointer">Start new session</button>
        </p>
      )}

      {g.status === 'completed' && g.result && (
        <div>
          <div className="relative max-w-xl mx-auto select-none">
            {beforeUrl && <img src={beforeUrl} alt="Before (your stored photo)" className="w-full rounded-lg" />}
            <div className="absolute inset-0 overflow-hidden" style={{ clipPath: `inset(0 0 0 ${pos}%)` }}>
              <img src={g.result.download_url} alt="After (generated result)" className="w-full h-full object-cover rounded-lg" />
            </div>
            <div className="absolute top-0 bottom-0 w-0.5 bg-white pointer-events-none" style={{ left: `${pos}%` }} />
            <span className="absolute left-2 top-2 text-[11px] px-2 py-0.5 rounded bg-black/60">Before</span>
            <span className="absolute right-2 top-2 text-[11px] px-2 py-0.5 rounded bg-black/60">After{mock ? ' (mock)' : ''}</span>
          </div>
          <input type="range" min={0} max={100} value={pos} onChange={e => setPos(Number(e.target.value))} aria-label="Before/after position" className="w-full max-w-xl mx-auto block mt-3" />
          {mock && <p className="text-xs text-amber-300/90 text-center mt-2">The mock provider only tints the photo and adds a striped banner. It does not change clothing.</p>}
          <div className="text-center mt-3">
            <button onClick={onRetry} className="px-3 py-1 rounded bg-slate-800 border border-slate-700 text-xs cursor-pointer">Generate again</button>
          </div>
        </div>
      )}
    </section>
  );
};
