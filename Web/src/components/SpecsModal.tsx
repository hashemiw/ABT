import React from 'react';
import { X, CheckCircle, Shield, Server, Cpu, Database, Terminal, AlertTriangle } from 'lucide-react';

interface SpecsModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const SpecsModal: React.FC<SpecsModalProps> = ({ isOpen, onClose }) => {
  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Backdrop */}
      <div className="fixed inset-0 bg-slate-950/70 backdrop-blur-sm" onClick={onClose} />

      {/* Modal */}
      <div className="relative bg-slate-900 border border-slate-800 rounded-2xl max-w-2xl w-full p-6 shadow-2xl max-h-[88vh] overflow-y-auto">
        <div className="flex items-center justify-between pb-4 border-b border-slate-800 mb-5">
          <div className="flex items-center gap-2">
            <Server className="w-5 h-5 text-indigo-400" />
            <h3 className="font-bold text-base text-white">ABT System Architecture &amp; Integration</h3>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-lg text-slate-400 hover:text-slate-200 hover:bg-slate-800"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="space-y-4 text-xs text-slate-300">
          <div className="p-3.5 rounded-xl bg-slate-950 border border-slate-800">
            <h4 className="font-semibold text-slate-100 flex items-center gap-1.5 mb-2">
              <CheckCircle className="w-4 h-4 text-emerald-400" />
              Shared Backend &amp; Architecture Boundaries
            </h4>
            <p className="text-slate-400 leading-relaxed">
              The AI Beauty Tool (ABT) architecture maintains a shared production pipeline in Go:
              <br />
              • <strong>Backend/</strong>: Go modular monolith, PostgreSQL database, private S3-compatible storage, and background worker with lease fencing.
              <br />
              • <strong>iOS/</strong>: Native SwiftUI client with Keychain session store and PhotosPicker.
              <br />
              • <strong>Web/</strong>: React web client with split comparison slider and backend API integration.
            </p>
          </div>

          <div className="p-3.5 rounded-xl bg-slate-950 border border-slate-800">
            <h4 className="font-semibold text-slate-200 flex items-center gap-1.5 mb-2">
              <Terminal className="w-4 h-4 text-indigo-400" />
              How to Run the Shared Go Backend
            </h4>
            <div className="bg-slate-900 p-2.5 rounded-lg font-mono text-[11px] text-slate-300 space-y-1">
              <div>cd Backend</div>
              <div>cp .env.example .env</div>
              <div>docker compose up --build -d</div>
              <div className="text-slate-500"># Verify API readiness:</div>
              <div>curl http://127.0.0.1:8080/health/ready</div>
            </div>
            <p className="text-[11px] text-slate-400 mt-2">
              The Web client proxies all <code>/v1/*</code> and <code>/health/*</code> calls directly to the Go API running on port 8080.
            </p>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800">
              <div className="flex items-center gap-1.5 font-semibold text-slate-200 mb-1">
                <Shield className="w-3.5 h-3.5 text-indigo-400" />
                <span>Identity &amp; Ownership</span>
              </div>
              <p className="text-[11px] text-slate-400">
                Anonymous session credentials (SHA-256 tokens). Every private media asset and generation job is strictly isolated by user ID.
              </p>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800">
              <div className="flex items-center gap-1.5 font-semibold text-slate-200 mb-1">
                <Database className="w-3.5 h-3.5 text-indigo-400" />
                <span>Durable Job Leases</span>
              </div>
              <p className="text-[11px] text-slate-400">
                PostgreSQL-backed queue with lease fencing (<code>claim_token</code> UUID, 60s window) and bounded worker concurrency.
              </p>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800">
              <div className="flex items-center gap-1.5 font-semibold text-slate-200 mb-1">
                <Cpu className="w-3.5 h-3.5 text-indigo-400" />
                <span>Provider Boundaries</span>
              </div>
              <p className="text-[11px] text-slate-400">
                External AI image calls happen outside database transactions. Normalized result images are stored in dedicated result assets.
              </p>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800">
              <div className="flex items-center gap-1.5 font-semibold text-slate-200 mb-1">
                <AlertTriangle className="w-3.5 h-3.5 text-amber-400" />
                <span>Provider Safety &amp; Cost Rules</span>
              </div>
              <p className="text-[11px] text-slate-400">
                Real/paid AI calls remain disabled by default. Tests use an offline mock provider. Real image generation requires explicit user approval.
              </p>
            </div>
          </div>
        </div>

        <div className="mt-6 pt-4 border-t border-slate-800 flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-xl bg-indigo-600 hover:bg-indigo-500 text-white font-medium text-xs transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
};
