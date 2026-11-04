import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Navbar } from './components/Navbar';
import { PhotoUploadSection, UploadPhase } from './components/PhotoUploadSection';
import { StylePresetSelector } from './components/StylePresetSelector';
import { GenerationPanel } from './components/GenerationPanel';
import { SpecsModal } from './components/SpecsModal';
import { api, SessionExpiredError } from './api/client';
import { UserSession, StylePreset, MediaAsset, Generation } from './types';
import { Sparkles, Server, ServerOff } from 'lucide-react';

// Polling is bounded: stop on a terminal state, after POLL_LIMIT_MS, or after repeated errors.
const POLL_LIMIT_MS = 3 * 60 * 1000;
const POLL_MAX_ERRORS = 5;

export function App() {
  const [session, setSession] = useState<UserSession | null>(null);
  const [presets, setPresets] = useState<StylePreset[]>([]);
  const [presetsError, setPresetsError] = useState<string | null>(null);
  const [selectedPresetId, setSelectedPresetId] = useState<string>('');
  const [instruction, setInstruction] = useState<string>('');
  const [isBackendOnline, setIsBackendOnline] = useState<boolean | null>(null);
  const [initError, setInitError] = useState<string | null>(null);
  const [isSpecsOpen, setIsSpecsOpen] = useState<boolean>(false);

  const [phase, setPhase] = useState<UploadPhase>({ type: 'idle' });
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [readyAsset, setReadyAsset] = useState<MediaAsset | null>(null);

  const [generation, setGeneration] = useState<Generation | null>(null);
  const [beforeUrl, setBeforeUrl] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [pollingStopped, setPollingStopped] = useState<string | null>(null);
  const [pollNonce, setPollNonce] = useState(0); // bumping it restarts a stopped poll on user request
  const [sessionExpired, setSessionExpired] = useState(false);

  // One idempotency key per distinct request; kept after a network failure so a retry is the same request.
  const pending = useRef<{ sig: string; key: string } | null>(null);
  const inFlight = useRef(false);

  const handleSessionExpired = useCallback(() => {
    setSession(null);
    setReadyAsset(null);
    setSessionExpired(true);
    setPollingStopped(null);
  }, []);

  const loadPresets = useCallback(async () => {
    try {
      const ps = await api.getPresets();
      setPresets(ps);
      setPresetsError(null);
      setSelectedPresetId(prev => prev || ps[0]?.id || '');
    } catch (err: any) {
      if (err instanceof SessionExpiredError) return handleSessionExpired();
      setPresetsError(err.message);
    }
  }, [handleSessionExpired]);

  useEffect(() => {
    (async () => {
      const online = await api.checkBackendHealth();
      setIsBackendOnline(online);
      if (!online) return;
      try {
        setSession(await api.ensureSession());
        await loadPresets();
      } catch (err: any) {
        setInitError(err.message);
      }
    })();
  }, [loadPresets]);

  // Bounded polling of the active generation.
  const genId = generation?.id;
  const active = generation?.status === 'queued' || generation?.status === 'processing';
  useEffect(() => {
    if (!genId || !active) return;
    let stopped = false;
    let errors = 0;
    const startedAt = Date.now();
    let timer: ReturnType<typeof setTimeout>;
    const tick = (delay: number) => {
      timer = setTimeout(async () => {
        if (stopped) return;
        if (Date.now() - startedAt > POLL_LIMIT_MS) {
          setPollingStopped('Still not finished after 3 minutes. The job may be waiting for a worker. You can check again or cancel.');
          return;
        }
        try {
          const g = await api.getGeneration(genId);
          if (stopped) return;
          errors = 0;
          setGeneration(g);
          if (g.status === 'queued' || g.status === 'processing') tick(Math.min(delay * 1.3, 4000));
        } catch (err: any) {
          if (stopped) return;
          if (err instanceof SessionExpiredError) return handleSessionExpired();
          if (++errors >= POLL_MAX_ERRORS) {
            setPollingStopped(`Lost contact with the server (${err.message}). The job may still be running.`);
            return;
          }
          tick(Math.min(delay * 2, 8000));
        }
      }, delay);
    };
    setPollingStopped(null);
    tick(1000);
    return () => { stopped = true; clearTimeout(timer); };
  }, [genId, active, pollNonce, handleSessionExpired]);

  // The stored source URL expires; fetch a fresh one when a result arrives so "Before" is the stored photo.
  useEffect(() => {
    if (generation?.status !== 'completed' || !readyAsset) return;
    api.getMedia(readyAsset.id).then(a => setBeforeUrl(a.download_url)).catch(() => setBeforeUrl(previewUrl));
  }, [generation?.status, generation?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleGenerate = async () => {
    if (!readyAsset || !selectedPresetId || inFlight.current) return;
    inFlight.current = true;
    setSubmitting(true);
    setSubmitError(null);
    const sig = `${readyAsset.id}|${selectedPresetId}|${instruction.trim()}`;
    if (pending.current?.sig !== sig) pending.current = { sig, key: crypto.randomUUID() };
    try {
      const res = await api.createGeneration(readyAsset.id, selectedPresetId, instruction.trim(), pending.current.key);
      pending.current = null; // accepted: the next click is a new generation
      setBeforeUrl(null);
      setGeneration(await api.getGeneration(res.id));
    } catch (err: any) {
      if (err instanceof SessionExpiredError) handleSessionExpired();
      else setSubmitError(err.message); // key kept: clicking again repeats the SAME request
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  };

  const handleCancel = async () => {
    if (!generation) return;
    try {
      await api.cancelGeneration(generation.id);
      setGeneration(await api.getGeneration(generation.id));
    } catch (err: any) {
      if (err instanceof SessionExpiredError) handleSessionExpired();
      else setSubmitError(err.message);
    }
  };

  const handleResetSession = async () => {
    api.clearSession();
    setPhase({ type: 'idle' });
    setPreviewUrl(null);
    setReadyAsset(null);
    setGeneration(null);
    setBeforeUrl(null);
    setSessionExpired(false);
    setInitError(null);
    pending.current = null;
    try {
      setSession(await api.ensureSession());
      await loadPresets();
    } catch (err: any) {
      setSession(null);
      setInitError(err.message);
    }
  };

  const selectedPreset = presets.find(p => p.id === selectedPresetId);
  const busyGenerating = submitting || active;
  let hint: string | null = null;
  if (!session) hint = 'No active session.';
  else if (phase.type !== 'ready' || !readyAsset) hint = 'Upload a photo first.';
  else if (!selectedPresetId) hint = presetsError ? 'Styles could not be loaded.' : 'Loading styles…';
  else if (busyGenerating) hint = 'A generation is already in progress.';

  return (
    <div className="min-h-screen flex flex-col bg-slate-950 text-slate-100 selection:bg-indigo-500/30">
      <Navbar session={session} isLive={isBackendOnline} onOpenSpecs={() => setIsSpecsOpen(true)} onResetSession={handleResetSession} />

      <div className={`border-b px-4 py-2 text-xs ${isBackendOnline !== false ? 'bg-emerald-950/40 border-emerald-900/50 text-emerald-300' : 'bg-amber-950/40 border-amber-900/50 text-amber-300'}`}>
        <div className="max-w-6xl mx-auto w-full flex items-center gap-2">
          {isBackendOnline === null ? (
            <span>Checking the Go backend…</span>
          ) : isBackendOnline ? (
            <>
              <Server className="w-3.5 h-3.5 text-emerald-400" />
              <span><strong>Go backend connected.</strong>{initError ? ` Session failed: ${initError}` : session ? ' Sessions, uploads, styles and generation run against the real API.' : ' No active session: use the reset button in the header to start a new one.'}</span>
            </>
          ) : (
            <>
              <ServerOff className="w-3.5 h-3.5 text-amber-400 shrink-0" />
              <span><strong>Go backend unreachable.</strong> Nothing will be uploaded. Start it with <code>docker compose up</code> in <code>Backend/</code>, then reload.</span>
            </>
          )}
        </div>
      </div>

      <main className="flex-1 max-w-6xl w-full mx-auto px-4 py-8">
        <div className="text-center max-w-2xl mx-auto mb-10">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-indigo-500/10 border border-indigo-500/20 text-indigo-400 text-xs font-semibold mb-4">
            <Sparkles className="w-3.5 h-3.5" />
            AI Beauty Tool (ABT) • Fashion Transformation
          </div>
          <h1 className="text-3xl sm:text-4xl font-extrabold text-white tracking-tight mb-3">Visualize Fashion Styles On Your Photo</h1>
          <p className="text-slate-400 text-sm leading-relaxed">
            Upload your portrait, pick a style, and the backend edits the clothing in your photo. The goal is to keep your face and pose; results vary and are not guaranteed.
          </p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 items-stretch">
          <PhotoUploadSection
            phase={phase}
            previewUrl={previewUrl}
            onPhaseChange={setPhase}
            onPreviewChange={setPreviewUrl}
            onAssetReady={setReadyAsset}
            onSessionExpired={handleSessionExpired}
            onNewSession={handleResetSession}
          />
          <StylePresetSelector
            presets={presets}
            selectedPresetId={selectedPresetId}
            instruction={instruction}
            presetsError={presetsError}
            canGenerate={hint === null}
            generateHint={submitError ? `Could not start: ${submitError} Click again to retry the same request.` : hint}
            isSubmitting={submitting}
            onSelectPreset={setSelectedPresetId}
            onInstructionChange={setInstruction}
            onGenerate={handleGenerate}
            onReloadPresets={loadPresets}
          />
        </div>

        <GenerationPanel
          generation={generation}
          beforeUrl={beforeUrl ?? previewUrl}
          presetName={presets.find(p => p.id === generation?.preset_id)?.name ?? selectedPreset?.name ?? ''}
          pollingStopped={pollingStopped}
          sessionExpired={sessionExpired}
          onCancel={handleCancel}
          onCheckAgain={() => setPollNonce(n => n + 1)}
          onRetry={handleGenerate}
          onNewSession={handleResetSession}
        />
      </main>

      <SpecsModal isOpen={isSpecsOpen} onClose={() => setIsSpecsOpen(false)} />

      <footer className="border-t border-slate-900 bg-slate-950/80 py-6 text-center text-xs text-slate-500 mt-16">
        <p>AI Beauty Tool (ABT) • Go Backend + PostgreSQL + S3 Storage + React Web Client</p>
      </footer>
    </div>
  );
}

export default App;
