import { useEffect, useState, useCallback } from 'react';
import { listProspects } from '../api/client';
import { ProspectDetail } from './ProspectDetail';

interface ProspectEvent {
  id: number;
  prospectId: string;
  type: string;
  date: string;
  note: string;
}

interface Prospect {
  id: string;
  clinic: string;
  owner: string;
  title: string;
  location: string;
  phone: string;
  email: string;
  website: string;
  emr: string;
  status: string;
  configured: boolean;
  telnyxNumber: string;
  '10dlc': boolean;
  smsWorking: boolean;
  orgId: string;
  services: number;
  providers: string[];
  nextAction: string;
  notes: string;
  timeline: ProspectEvent[];
  createdAt: string;
  updatedAt: string;
}

const STATUS_MAP: Record<string, { label: string; color: string; bg: string }> = {
  outreach_sent: { label: 'Outreach Sent', color: 'text-yellow-700', bg: 'bg-yellow-50 border-yellow-200' },
  responded: { label: 'Responded', color: 'text-blue-700', bg: 'bg-blue-50 border-blue-200' },
  testing: { label: 'Testing', color: 'text-orange-700', bg: 'bg-orange-50 border-orange-200' },
  converted: { label: 'Converted', color: 'text-green-700', bg: 'bg-green-50 border-green-200' },
  configured: { label: 'Configured', color: 'text-blue-700', bg: 'bg-blue-50 border-blue-200' },
  identified: { label: 'Identified', color: 'text-slate-600', bg: 'bg-slate-50 border-slate-200' },
  lost: { label: 'Lost', color: 'text-red-700', bg: 'bg-red-50 border-red-200' },
};

function Indicator({ on, label }: { on: boolean; label: string }) {
  return (
    <span
      title={label}
      className={`inline-block w-2 h-2 rounded-full ${on ? 'bg-green-500' : 'bg-slate-300'}`}
    />
  );
}

export function ProspectTracker() {
  const [prospects, setProspects] = useState<Prospect[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const data = await listProspects();
      setProspects((data.prospects || []) as Prospect[]);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchData(); }, [fetchData]);

  const selectedProspect = selectedId ? prospects.find(p => p.id === selectedId) : null;
  if (selectedProspect) {
    return (
      <ProspectDetail
        prospect={selectedProspect}
        onBack={() => setSelectedId(null)}
        onRefresh={fetchData}
      />
    );
  }

  if (loading) {
    return (
      <div className="ui-page flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="ui-container py-8">
        <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-center">
          <p className="text-sm text-red-800 font-medium">Failed to load prospects: {error}</p>
          <button onClick={fetchData} className="ui-btn ui-btn-ghost mt-3 text-sm">Retry</button>
        </div>
      </div>
    );
  }

  const total = prospects.length;
  const active = prospects.filter(p => ['outreach_sent', 'responded', 'testing'].includes(p.status)).length;
  const configured = prospects.filter(p => p.configured).length;
  const smsReady = prospects.filter(p => p.smsWorking).length;

  const statusOrder = ['outreach_sent', 'responded', 'testing', 'converted', 'configured', 'identified', 'lost'];
  const sorted = [...prospects].sort((a, b) => statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status));

  return (
    <div className="ui-container py-6 space-y-6">
      {/* Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <div className="ui-card ui-card-solid p-4">
          <div className="text-xs font-medium text-slate-500 uppercase tracking-wide">Total</div>
          <div className="text-2xl font-bold text-slate-900 mt-1">{total}</div>
        </div>
        <div className="ui-card ui-card-solid p-4">
          <div className="text-xs font-medium text-slate-500 uppercase tracking-wide">Active Outreach</div>
          <div className="text-2xl font-bold text-violet-600 mt-1">{active}</div>
        </div>
        <div className="ui-card ui-card-solid p-4">
          <div className="text-xs font-medium text-slate-500 uppercase tracking-wide">Configured</div>
          <div className="text-2xl font-bold text-slate-900 mt-1">{configured}/{total}</div>
        </div>
        <div className="ui-card ui-card-solid p-4">
          <div className="text-xs font-medium text-slate-500 uppercase tracking-wide">SMS Ready</div>
          <div className="text-2xl font-bold text-green-600 mt-1">{smsReady}/{total}</div>
        </div>
      </div>

      {/* Pipeline */}
      <div>
        <h2 className="text-sm font-semibold text-slate-500 uppercase tracking-wide mb-3">Pipeline</h2>
        <div className="space-y-3">
          {sorted.map(p => {
            const s = STATUS_MAP[p.status] || { label: p.status, color: 'text-slate-600', bg: 'bg-slate-50 border-slate-200' };

            return (
              <div key={p.id} className="ui-card ui-card-solid overflow-hidden">
                <button
                  onClick={() => setSelectedId(p.id)}
                  className="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-slate-50 transition-colors"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="w-9 h-9 rounded-lg bg-violet-100 text-violet-700 flex items-center justify-center text-xs font-bold shrink-0">
                      {p.clinic.split(' ').map(w => w[0]).join('').slice(0, 2)}
                    </div>
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-slate-900 truncate">{p.clinic}</div>
                      <div className="text-xs text-slate-500 truncate">
                        {[p.owner, p.location, p.emr].filter(Boolean).join(' · ')}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 shrink-0">
                    <div className="flex gap-1" title="Config · 10DLC · SMS">
                      <Indicator on={p.configured} label="Configured" />
                      <Indicator on={p['10dlc']} label="10DLC" />
                      <Indicator on={p.smsWorking} label="SMS Working" />
                    </div>
                    <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${s.bg} ${s.color}`}>
                      {s.label}
                    </span>
                    <span className="text-slate-400 text-sm">›</span>
                  </div>
                </button>
              </div>
            );
          })}

          {sorted.length === 0 && (
            <div className="text-center py-12 text-slate-500 text-sm">
              No prospects yet. They'll appear here once added via the API.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
