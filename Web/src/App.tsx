import React, { useEffect, useState } from 'react';
import { Navbar } from './components/Navbar';
import { PhotoUploadSection, UploadPhase } from './components/PhotoUploadSection';
import { StylePresetSelector } from './components/StylePresetSelector';
import { PREVIEW_PRESETS } from './presets';
import { SpecsModal } from './components/SpecsModal';
import { api } from './api/client';
import { UserSession, MediaAsset } from './types';
import { Sparkles, Server, ServerOff, ShieldAlert } from 'lucide-react';

export function App() {
  const [session, setSession] = useState<UserSession | null>(null);
  const presets = PREVIEW_PRESETS;
  const [selectedPresetId, setSelectedPresetId] = useState<string>('old_money');
  const [instruction, setInstruction] = useState<string>('');
  const [isBackendOnline, setIsBackendOnline] = useState<boolean | null>(null);

  // Upload state
  const [phase, setPhase] = useState<UploadPhase>({ type: 'idle' });
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [readyAsset, setReadyAsset] = useState<MediaAsset | null>(null);

  const [initError, setInitError] = useState<string | null>(null);
  const [isSpecsOpen, setIsSpecsOpen] = useState<boolean>(false);

  // Real connection check + anonymous session; failures are shown, never replaced by a demo.
  useEffect(() => {
    (async () => {
      const online = await api.checkBackendHealth();
      setIsBackendOnline(online);
      if (!online) return;
      try {
        setSession(await api.ensureSession());
      } catch (err: any) {
        setInitError(err.message);
      }
    })();
  }, []);

  const handleResetSession = async () => {
    api.clearSession();
    setPhase({ type: 'idle' });
    setPreviewUrl(null);
    setReadyAsset(null);
    setInitError(null);
    try {
      setSession(await api.ensureSession());
    } catch (err: any) {
      setSession(null);
      setInitError(err.message);
    }
  };

  const selectedPreset = presets.find(p => p.id === selectedPresetId);

  return (
    <div className="min-h-screen flex flex-col bg-slate-950 text-slate-100 selection:bg-indigo-500/30">
      <Navbar
        session={session}
        isLive={isBackendOnline}
        onOpenSpecs={() => setIsSpecsOpen(true)}
        onResetSession={handleResetSession}
      />

      {/* Backend Connectivity Status Banner */}
      <div className={`border-b px-4 py-2 text-xs flex items-center justify-between transition-colors ${
        isBackendOnline !== false
          ? 'bg-emerald-950/40 border-emerald-900/50 text-emerald-300'
          : 'bg-amber-950/40 border-amber-900/50 text-amber-300'
      }`}>
        <div className="max-w-6xl mx-auto w-full flex items-center justify-between">
          <div className="flex items-center gap-2">
            {isBackendOnline === null ? (
              <span>Checking the Go backend…</span>
            ) : isBackendOnline ? (
              <>
                <Server className="w-3.5 h-3.5 text-emerald-400" />
                <span><strong>Go backend connected.</strong>{initError ? ` Session failed: ${initError}` : ' Sessions, uploads and deletion run against the real API.'}</span>
              </>
            ) : (
              <>
                <ServerOff className="w-3.5 h-3.5 text-amber-400 shrink-0" />
                <span><strong>Go backend unreachable.</strong> Nothing will be uploaded. Start it with <code>docker compose up</code> in <code>Backend/</code>, then reload.</span>
              </>
            )}
          </div>
          <button
            onClick={() => setIsSpecsOpen(true)}
            className="underline hover:text-white transition-colors shrink-0 ml-4"
          >
            Backend Architecture
          </button>
        </div>
      </div>

      <main className="flex-1 max-w-6xl w-full mx-auto px-4 py-8">
        {/* Hero Banner */}
        <div className="text-center max-w-2xl mx-auto mb-10">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-indigo-500/10 border border-indigo-500/20 text-indigo-400 text-xs font-semibold mb-4">
            <Sparkles className="w-3.5 h-3.5" />
            AI Beauty Tool (ABT) • Fashion Transformation
          </div>
          <h1 className="text-3xl sm:text-4xl font-extrabold text-white tracking-tight mb-3">
            Visualize Realistic Fashion Styles On Your Photo
          </h1>
          <p className="text-slate-400 text-sm leading-relaxed">
            Upload your portrait, select a tailored aesthetic (Old Money, Minimalist, Streetwear, Smart Casual, or Formal Elegant), and edit the clothing of the person in the photo while preserving facial likeness and pose.
          </p>
        </div>

        {/* Workflow Grid */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 items-stretch">
          {/* Step 1: Photo Upload */}
          <PhotoUploadSection
            phase={phase}
            previewUrl={previewUrl}
            onPhaseChange={setPhase}
            onPreviewChange={setPreviewUrl}
            onAssetReady={setReadyAsset}
          />

          {/* Step 2: Style Preset Selection */}
          <StylePresetSelector
            presets={presets}
            selectedPresetId={selectedPresetId}
            instruction={instruction}
            onSelectPreset={setSelectedPresetId}
            onInstructionChange={setInstruction}
          />
        </div>
      </main>

      {/* Architecture Specs Modal */}
      <SpecsModal
        isOpen={isSpecsOpen}
        onClose={() => setIsSpecsOpen(false)}
      />

      {/* Footer */}
      <footer className="border-t border-slate-900 bg-slate-950/80 py-6 text-center text-xs text-slate-500 mt-16">
        <p>AI Beauty Tool (ABT) • Architecture: Go Backend + PostgreSQL + S3 Storage + React Web Client</p>
      </footer>
    </div>
  );
}

export default App;
