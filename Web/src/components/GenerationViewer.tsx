import React, { useState } from 'react';
import { Download, Sparkles, Sliders, Columns, RefreshCw, Star, CheckCircle, AlertCircle, Layers, ShieldAlert } from 'lucide-react';
import { Generation, StylePreset } from '../types';
import { api } from '../api/client';

interface GenerationViewerProps {
  generation: Generation | null;
  originalImageUrl: string | null;
  preset: StylePreset | undefined;
  onRefine: (genId: string, currentInstruction: string) => void;
  onCancel: (genId: string) => void;
  onRetry: () => void;
}

export const GenerationViewer: React.FC<GenerationViewerProps> = ({
  generation,
  originalImageUrl,
  preset,
  onRefine,
  onCancel,
  onRetry
}) => {
  const [sliderPosition, setSliderPosition] = useState<number>(50);
  const [viewMode, setViewMode] = useState<'slider' | 'side-by-side'>('slider');
  const [feedbackRating, setFeedbackRating] = useState<number>(0);
  const [feedbackSubmitted, setFeedbackSubmitted] = useState<boolean>(false);
  const [refinementInput, setRefinementInput] = useState<string>('');
  const [showRefineModal, setShowRefineModal] = useState<boolean>(false);

  if (!generation) return null;

  const resultUrl = generation.result?.download_url || null;
  const isMock = generation.result?.provider?.includes('mock');

  const handleFeedback = async (rating: number) => {
    try {
      setFeedbackRating(rating);
      await api.submitFeedback(generation.id, rating);
      setFeedbackSubmitted(true);
    } catch (err) {
      console.error('Feedback submission failed:', err);
    }
  };

  const handleDownload = () => {
    if (!resultUrl) return;
    const a = document.createElement('a');
    a.href = resultUrl;
    a.download = `abt_fashion_${preset?.id || 'style'}_${Date.now()}.jpg`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  };

  return (
    <div className="bg-slate-900/80 border border-slate-800 rounded-2xl p-6 shadow-2xl backdrop-blur-md mt-8">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 mb-6 pb-4 border-b border-slate-800">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-xl font-bold text-white flex items-center gap-2">
              <Sparkles className="w-5 h-5 text-indigo-400" />
              Transformation Result
            </h2>
            <span className={`text-xs px-2.5 py-0.5 rounded-full font-medium ${
              generation.status === 'completed'
                ? isMock
                  ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                  : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                : generation.status === 'failed'
                ? 'bg-red-500/10 text-red-400 border border-red-500/20'
                : 'bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 animate-pulse'
            }`}>
              {generation.status.toUpperCase()}
              {isMock && ' (MOCK FIXTURE)'}
            </span>
          </div>

          <p className="text-xs text-slate-400 mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
            <span>Style: <span className="text-slate-200 font-semibold">{preset?.name || 'Custom'}</span></span>
            <span>•</span>
            <span>
              Engine: <span className="text-indigo-300 font-medium">
                {generation.result?.provider === 'gemini'
                  ? `Gemini (${generation.result.model})`
                  : isMock
                  ? 'Offline Mock Fixture (Demo Mode)'
                  : 'Go Backend Service'}
              </span>
            </span>
            <span>•</span>
            <span>{new Date(generation.created_at).toLocaleTimeString()}</span>
          </p>
        </div>

        {generation.status === 'completed' && (
          <div className="flex items-center gap-2">
            {/* View Mode Toggle */}
            <div className="bg-slate-950 p-1 rounded-xl border border-slate-800 flex items-center gap-1">
              <button
                onClick={() => setViewMode('slider')}
                className={`p-1.5 rounded-lg text-xs flex items-center gap-1 transition-colors ${
                  viewMode === 'slider' ? 'bg-indigo-600 text-white' : 'text-slate-400 hover:text-slate-200'
                }`}
                title="Split slider view"
              >
                <Sliders className="w-3.5 h-3.5" />
                <span className="hidden sm:inline">Slider</span>
              </button>
              <button
                onClick={() => setViewMode('side-by-side')}
                className={`p-1.5 rounded-lg text-xs flex items-center gap-1 transition-colors ${
                  viewMode === 'side-by-side' ? 'bg-indigo-600 text-white' : 'text-slate-400 hover:text-slate-200'
                }`}
                title="Side-by-side view"
              >
                <Columns className="w-3.5 h-3.5" />
                <span className="hidden sm:inline">Side by Side</span>
              </button>
            </div>

            {/* Download */}
            <button
              onClick={handleDownload}
              className="px-3.5 py-1.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-xs font-medium text-slate-200 border border-slate-700 flex items-center gap-1.5 transition-colors"
            >
              <Download className="w-3.5 h-3.5" />
              Download
            </button>
          </div>
        )}
      </div>

      {/* Main Content Area */}
      {generation.status === 'queued' || generation.status === 'processing' ? (
        <div className="py-20 flex flex-col items-center justify-center text-center">
          <div className="relative mb-6">
            <div className="w-20 h-20 rounded-full border-4 border-indigo-500/20 border-t-indigo-500 animate-spin" />
            <div className="absolute inset-0 flex items-center justify-center">
              <Sparkles className="w-8 h-8 text-indigo-400 animate-pulse" />
            </div>
          </div>
          <h3 className="text-lg font-bold text-white mb-2">
            {generation.status === 'queued' ? 'Queued in Go Backend Worker…' : 'Processing Image-to-Image Transformation…'}
          </h3>
          <p className="text-xs text-slate-400 max-w-sm mb-6">
            Applying the {preset?.name} aesthetic with realistic garment contours and lighting balance.
          </p>
          <button
            onClick={() => onCancel(generation.id)}
            className="text-xs text-slate-400 hover:text-red-400 transition-colors"
          >
            Cancel Request
          </button>
        </div>
      ) : generation.status === 'failed' ? (
        <div className="p-8 text-center bg-red-950/20 border border-red-800/40 rounded-xl">
          <AlertCircle className="w-10 h-10 text-red-400 mx-auto mb-3" />
          <h3 className="text-base font-bold text-red-200 mb-1">Transformation Not Completed</h3>
          <p className="text-xs text-red-300 max-w-md mx-auto mb-5 leading-relaxed">
            {generation.error?.message || 'We could not generate your image. Please try again with a different photo or style.'}
          </p>
          <button
            onClick={onRetry}
            className="px-4 py-2 rounded-xl bg-red-900/40 hover:bg-red-800/60 text-red-200 border border-red-700 text-xs font-semibold inline-flex items-center gap-2 transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            Retry Transformation
          </button>
        </div>
      ) : generation.status === 'completed' && resultUrl ? (
        <div>
          {/* Explicit Mock Notice if using mock provider */}
          {isMock && (
            <div className="mb-4 p-3 bg-amber-950/40 border border-amber-800/50 rounded-xl flex items-center gap-3 text-xs text-amber-300">
              <ShieldAlert className="w-4 h-4 text-amber-400 shrink-0" />
              <div>
                <span className="font-semibold">[DEMO MOCK FIXTURE - NOT A REAL AI TRANSFORMATION]</span>
                <p className="text-[11px] text-amber-400/80 mt-0.5">
                  This deterministic offline fixture verifies the pipeline and Before/After slider wiring without incurring provider costs or network calls.
                </p>
              </div>
            </div>
          )}

          {/* Interactive Split Slider View */}
          {viewMode === 'slider' ? (
            <div className="relative w-full max-w-2xl mx-auto h-[480px] rounded-2xl overflow-hidden bg-slate-950 border border-slate-800 select-none shadow-2xl">
              {/* After Image (Background) */}
              <img
                src={resultUrl}
                alt="AI Fashion Transformed"
                className="absolute inset-0 w-full h-full object-contain"
              />

              {/* Before Image (Clipped Overlay) */}
              {originalImageUrl && (
                <div
                  className="absolute inset-0 overflow-hidden"
                  style={{ width: `${sliderPosition}%` }}
                >
                  <img
                    src={originalImageUrl}
                    alt="Original Photo"
                    className="absolute inset-0 max-w-none w-full h-full object-contain"
                    style={{ width: '100%', height: '100%' }}
                  />
                  <span className="absolute top-4 left-4 bg-slate-900/80 backdrop-blur-md px-2.5 py-1 rounded-md text-[11px] font-semibold text-slate-300 border border-slate-700">
                    Before (Original)
                  </span>
                </div>
              )}

              {/* Transformed Label */}
              <span className="absolute top-4 right-4 bg-indigo-950/80 backdrop-blur-md px-2.5 py-1 rounded-md text-[11px] font-semibold text-indigo-300 border border-indigo-700">
                After ({preset?.name})
              </span>

              {/* Slider Divider Bar */}
              {originalImageUrl && (
                <div
                  className="absolute top-0 bottom-0 w-1 bg-white cursor-ew-resize shadow-[0_0_10px_rgba(0,0,0,0.5)] z-10"
                  style={{ left: `${sliderPosition}%` }}
                >
                  <div className="absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-8 h-8 rounded-full bg-white text-slate-900 flex items-center justify-center shadow-lg cursor-grab active:cursor-grabbing border border-slate-300">
                    <Sliders className="w-4 h-4" />
                  </div>
                </div>
              )}

              {/* Invisible touch/drag slider input */}
              {originalImageUrl && (
                <input
                  type="range"
                  min="0"
                  max="100"
                  value={sliderPosition}
                  onChange={e => setSliderPosition(Number(e.target.value))}
                  className="absolute inset-0 opacity-0 cursor-ew-resize w-full h-full z-20"
                />
              )}
            </div>
          ) : (
            /* Side by Side Mode */
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 max-w-4xl mx-auto">
              <div className="bg-slate-950 border border-slate-800 rounded-xl overflow-hidden p-3 text-center">
                <span className="text-xs font-semibold text-slate-400 mb-2 block">Before (Original Photo)</span>
                <div className="h-[360px] flex items-center justify-center">
                  <img
                    src={originalImageUrl || ''}
                    alt="Original"
                    className="max-h-full max-w-full object-contain rounded-lg"
                  />
                </div>
              </div>
              <div className="bg-slate-950 border border-indigo-900/40 rounded-xl overflow-hidden p-3 text-center">
                <span className="text-xs font-semibold text-indigo-400 mb-2 block">After ({preset?.name} Look)</span>
                <div className="h-[360px] flex items-center justify-center">
                  <img
                    src={resultUrl}
                    alt="Transformed"
                    className="max-h-full max-w-full object-contain rounded-lg shadow-lg"
                  />
                </div>
              </div>
            </div>
          )}

          {/* Slider drag tip */}
          {viewMode === 'slider' && (
            <p className="text-[11px] text-slate-500 text-center mt-3">
              Drag the center slider horizontally to compare original vs. transformed look.
            </p>
          )}

          {/* Refinement & Feedback Bar */}
          <div className="mt-8 pt-6 border-t border-slate-800 flex flex-wrap items-center justify-between gap-4">
            {/* Refinement Trigger */}
            <div className="flex items-center gap-3">
              <button
                onClick={() => setShowRefineModal(true)}
                className="px-4 py-2 rounded-xl bg-indigo-600/20 hover:bg-indigo-600/30 text-indigo-300 font-semibold text-xs border border-indigo-500/30 transition-all flex items-center gap-2"
              >
                <Layers className="w-3.5 h-3.5" />
                Refine This Look
              </button>
              <span className="text-xs text-slate-400 hidden sm:inline">
                Iterate with custom styling instructions
              </span>
            </div>

            {/* Feedback Rating */}
            <div className="flex items-center gap-3 bg-slate-950/60 px-3.5 py-1.5 rounded-xl border border-slate-800">
              <span className="text-xs text-slate-400">Rate result:</span>
              {feedbackSubmitted ? (
                <span className="text-xs text-emerald-400 flex items-center gap-1 font-medium">
                  <CheckCircle className="w-3.5 h-3.5" /> Thank you!
                </span>
              ) : (
                <div className="flex items-center gap-1">
                  {[1, 2, 3, 4, 5].map(star => (
                    <button
                      key={star}
                      onClick={() => handleFeedback(star)}
                      className={`p-1 transition-colors ${
                        star <= feedbackRating ? 'text-amber-400' : 'text-slate-600 hover:text-amber-400'
                      }`}
                    >
                      <Star className="w-3.5 h-3.5 fill-current" />
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      ) : null}

      {/* Refine Modal */}
      {showRefineModal && (
        <div className="fixed inset-0 bg-slate-950/80 backdrop-blur-md flex items-center justify-center p-4 z-50">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl max-w-md w-full p-6 shadow-2xl">
            <h3 className="text-base font-bold text-white mb-2 flex items-center gap-2">
              <Layers className="w-4 h-4 text-indigo-400" />
              Refine Fashion Look
            </h3>
            <p className="text-xs text-slate-400 mb-4">
              Add specific adjustments to the current {preset?.name} look (e.g. &quot;Change blazer to dark burgundy&quot;, &quot;Add gold watch&quot;).
            </p>
            <input
              type="text"
              value={refinementInput}
              onChange={e => setRefinementInput(e.target.value)}
              placeholder="Enter refinement instructions…"
              className="w-full px-3.5 py-2.5 rounded-xl bg-slate-950 border border-slate-800 focus:border-indigo-500 text-xs text-slate-200 outline-none mb-4"
              autoFocus
            />
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setShowRefineModal(false)}
                className="px-3.5 py-1.5 rounded-lg text-xs font-medium text-slate-400 hover:text-slate-200"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  setShowRefineModal(false);
                  onRefine(generation.id, refinementInput);
                }}
                disabled={!refinementInput.trim()}
                className="px-4 py-1.5 rounded-lg text-xs font-semibold bg-indigo-600 hover:bg-indigo-500 text-white disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Generate Refinement
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
