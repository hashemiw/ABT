import React from 'react';
import { Palette, Wand2, Check } from 'lucide-react';
import { StylePreset } from '../types';

interface StylePresetSelectorProps {
  presets: StylePreset[];
  selectedPresetId: string;
  instruction: string;
  onSelectPreset: (id: string) => void;
  onInstructionChange: (inst: string) => void;
}

export const StylePresetSelector: React.FC<StylePresetSelectorProps> = ({
  presets,
  selectedPresetId,
  instruction,
  onSelectPreset,
  onInstructionChange
}) => {
  return (
    <div className="bg-slate-900/70 border border-slate-800 rounded-2xl p-6 shadow-xl backdrop-blur-sm flex flex-col justify-between">
      <div>
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-lg font-bold text-white flex items-center gap-2">
              <Palette className="w-5 h-5 text-indigo-400" />
              2. Choose Style Preset
            </h2>
            <p className="text-xs text-slate-400">
              Select a fashion direction or curate a specific aesthetic look.
            </p>
          </div>
        </div>

        {/* Preset Cards Grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-5">
          {presets.map(preset => {
            const isSelected = preset.id === selectedPresetId;
            return (
              <button
                key={preset.id}
                onClick={() => onSelectPreset(preset.id)}
                className={`text-left p-3.5 rounded-xl border transition-all relative flex flex-col justify-between cursor-pointer ${
                  isSelected
                    ? 'bg-indigo-600/15 border-indigo-500 shadow-md shadow-indigo-500/10 ring-1 ring-indigo-500/50'
                    : 'bg-slate-950/60 border-slate-800 hover:border-slate-700 hover:bg-slate-800/40'
                }`}
              >
                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <span className="font-semibold text-sm text-slate-100 flex items-center gap-1.5">
                      {preset.name}
                    </span>
                    {isSelected && (
                      <span className="w-5 h-5 rounded-full bg-indigo-500 text-white flex items-center justify-center">
                        <Check className="w-3 h-3 stroke-[3]" />
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-slate-400 line-clamp-2 leading-relaxed">
                    {preset.description}
                  </p>
                </div>

                {/* Tags */}
                {preset.tags && (
                  <div className="flex flex-wrap gap-1 mt-2.5 pt-2 border-t border-slate-800/60">
                    {preset.tags.slice(0, 3).map(tag => (
                      <span
                        key={tag}
                        className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-300 font-medium"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
              </button>
            );
          })}
        </div>

        {/* Optional Custom Instruction */}
        <div className="mb-6">
          <label className="block text-xs font-semibold text-slate-300 mb-1.5 flex items-center justify-between">
            <span>Optional Custom Refinement</span>
            <span className="text-[11px] text-slate-500 font-normal">e.g. &quot;Navy blazer&quot;, &quot;Add sunglasses&quot;</span>
          </label>
          <input
            type="text"
            value={instruction}
            onChange={e => onInstructionChange(e.target.value)}
            placeholder="Add subtle fashion tweak (color palette, fabric, accessories)…"
            maxLength={140}
            className="w-full px-3.5 py-2 rounded-xl bg-slate-950 border border-slate-800 focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 text-xs text-slate-200 placeholder:text-slate-600 transition-all outline-none"
          />
        </div>
      </div>

      {/* Generate Actions */}
      <div className="space-y-3">
        <button
          disabled
          className="w-full py-3.5 px-4 rounded-xl font-semibold text-sm flex items-center justify-center gap-2 bg-slate-800 text-slate-500 border border-slate-700/50 cursor-not-allowed"
        >
          <Wand2 className="w-4 h-4" />
          <span>Generate Transformation</span>
        </button>
        <p className="text-[11px] text-amber-300/80 text-center">
          Generation is not available yet: the Go backend implements sessions and media only (M1). Styles shown here are a preview list; real generation arrives in M2.
        </p>
      </div>
    </div>
  );
};
