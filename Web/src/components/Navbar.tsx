import React from 'react';
import { Sparkles, ShieldCheck, RefreshCw, FileText } from 'lucide-react';
import { UserSession } from '../types';

interface NavbarProps {
  session: UserSession | null;
  isLive: boolean | null;
  onOpenSpecs: () => void;
  onResetSession: () => void;
}

export const Navbar: React.FC<NavbarProps> = ({
  session,
  isLive,
  onOpenSpecs,
  onResetSession,
}) => {
  return (
    <header className="border-b border-slate-800 bg-slate-900/80 backdrop-blur-md sticky top-0 z-40">
      <div className="max-w-6xl mx-auto px-4 h-16 flex items-center justify-between">
        {/* Brand */}
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-indigo-600 to-violet-500 flex items-center justify-center shadow-lg shadow-indigo-500/20">
            <Sparkles className="w-5 h-5 text-white" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-bold text-lg text-white tracking-tight">AI Beauty Tool</span>
              <span className="text-xs px-2 py-0.5 rounded-full bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 font-medium">
                ABT Fashion
              </span>
            </div>
            <p className="text-xs text-slate-400 hidden sm:block">Fashion Transformation MVP</p>
          </div>
        </div>

        {/* Actions & Session */}
        <div className="flex items-center gap-3">
          {/* Health indicator */}
          <div className="hidden md:flex items-center gap-1.5 text-xs text-slate-400 bg-slate-800/60 px-2.5 py-1 rounded-md border border-slate-700/50">
            <span
              className={`w-2 h-2 rounded-full ${
                isLive === true
                  ? 'bg-emerald-400 animate-pulse'
                  : 'bg-amber-400'
              }`}
            />
            <span>
              {isLive === true
                ? 'Go Backend Live'
                : 'Go Backend Unreachable'}
            </span>
          </div>

          {/* Anonymous Session */}
          {session && (
            <div className="hidden lg:flex items-center gap-2 text-xs text-slate-300 bg-slate-800/80 px-3 py-1 rounded-md border border-slate-700">
              <ShieldCheck className="w-3.5 h-3.5 text-emerald-400" />
              <span>User: <span className="font-mono text-slate-400">{session.userId.slice(0, 8)}…</span></span>
            </div>
          )}

          {/* Specs / Docs button */}
          <button
            onClick={onOpenSpecs}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-slate-300 bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors border border-slate-700 cursor-pointer"
            title="Go Backend Architecture & Setup"
          >
            <FileText className="w-3.5 h-3.5 text-slate-400" />
            <span className="hidden sm:inline">Backend Specs</span>
          </button>

          {/* Reset session */}
          <button
            onClick={onResetSession}
            title="Reset Anonymous Session"
            className="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-800 rounded-lg transition-colors cursor-pointer"
          >
            <RefreshCw className="w-4 h-4" />
          </button>
        </div>
      </div>
    </header>
  );
};
