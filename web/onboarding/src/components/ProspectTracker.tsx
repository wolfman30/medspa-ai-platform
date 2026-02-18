import { useEffect, useState, useCallback } from 'react';

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

function formatDateTime(iso: string) {
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) + ' ' +
    d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' });
}

function Indicator({ on, label }: { on: boolean; label: string }) {
  return (
    <span
      title={label}
      className={`inline-block w-2 h-2 rounded-full ${on ? 'bg-green-500' : 'bg-slate-300'}`}
    />
  );
}

interface AddEventFormProps {
  prospectId: string;
  onAdded: () => void;
}

function AddEventForm({ prospectId, onAdded }: AddEventFormProps) {
  const [type, setType] = useState('email');
  const [note, setNote] = useState('');
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!note.trim()) return;
    setSaving(true);
    try {
      const token = localStorage.getItem('admin_token') || '';
      const apiBase = import.meta.env.VITE_API_URL || '';
      await fetch(`${apiBase}/admin/prospects/${prospectId}/events`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
        body: JSON.stringify({ type, note: note.trim() }),
      });
      setNote('');
      onAdded();
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="mt-3 flex gap-2 items-end">
      <select value={type} onChange={e => setType(e.target.value)} className="ui-select text-xs py-1.5 w-28">
        <option value="email">Email</option>
        <option value="call">Call</option>
        <option value="text">Text</option>
        <option value="meeting">Meeting</option>
        <option value="note">Note</option>
        <option value="response">Response</option>
      </select>
      <input
        type="text"
        placeholder="What happened?"
        value={note}
        onChange={e => setNote(e.target.value)}
        className="ui-input text-xs py-1.5 flex-1"
      />
      <button type="submit" disabled={saving || !note.trim()} className="ui-btn ui-btn-primary text-xs py-1.5 px-3">
        {saving ? '...' : 'Add'}
      </button>
    </form>
  );
}

export function ProspectTracker() {
  const [prospects, setProspects] = useState<Prospect[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchProspects = useCallback(async () => {
    try {
      const token = localStorage.getItem('admin_token') || '';
      const apiBase = import.meta.env.VITE_API_URL || '';
      const resp = await fetch(`${apiBase}/admin/prospects`, {
        headers: { 'Authorization': `Bearer ${token}` },
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const data = await resp.json();
      setProspects(data.prospects || []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchProspects(); }, [fetchProspects]);

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
          <button onClick={fetchProspects} className="ui-btn ui-btn-ghost mt-3 text-sm">Retry</button>
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
            const expanded = expandedId === p.id;

            return (
              <div key={p.id} className="ui-card ui-card-solid overflow-hidden">
                <button
                  onClick={() => setExpandedId(expanded ? null : p.id)}
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
                    <span className={`text-slate-400 text-sm transition-transform ${expanded ? 'rotate-90' : ''}`}>›</span>
                  </div>
                </button>

                {expanded && (
                  <div className="px-4 pb-4 border-t border-slate-100 pt-4">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3 text-sm">
                      <div><span className="text-xs text-slate-500 uppercase">Owner</span><div className="text-slate-800">{p.owner || '—'}</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">Phone</span><div className="text-slate-800">{p.phone || '—'}</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">Email</span><div className="text-slate-800">{p.email || '—'}</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">EMR</span><div className="text-slate-800">{p.emr || '—'}</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">Telnyx #</span><div className="text-slate-800">{p.telnyxNumber || '—'}</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">Services</span><div className="text-slate-800">{p.services} configured</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">Providers</span><div className="text-slate-800">{p.providers?.join(', ') || '—'}</div></div>
                      <div><span className="text-xs text-slate-500 uppercase">Notes</span><div className="text-slate-800">{p.notes || '—'}</div></div>
                    </div>

                    {p.timeline && p.timeline.length > 0 && (
                      <div className="mt-4">
                        <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-wide mb-2">Timeline</h3>
                        <div className="space-y-2">
                          {p.timeline.map((ev, i) => (
                            <div key={i} className="flex gap-3 items-start">
                              <span className="inline-flex items-center rounded bg-violet-100 text-violet-700 px-1.5 py-0.5 text-[10px] font-semibold uppercase shrink-0">
                                {ev.type}
                              </span>
                              <div className="min-w-0">
                                <span className="text-xs text-slate-400">{formatDateTime(ev.date)}</span>
                                <p className="text-sm text-slate-700">{ev.note}</p>
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    <AddEventForm prospectId={p.id} onAdded={fetchProspects} />

                    {p.nextAction && (
                      <div className="mt-4 rounded-lg bg-violet-50 border border-violet-200 p-3">
                        <div className="text-[10px] font-semibold text-violet-600 uppercase tracking-wide">Next Action</div>
                        <p className="text-sm text-slate-800 mt-0.5">{p.nextAction}</p>
                      </div>
                    )}
                  </div>
                )}
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
