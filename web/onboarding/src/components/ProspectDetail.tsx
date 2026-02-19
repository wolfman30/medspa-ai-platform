import { useState } from 'react';
import { addProspectEvent } from '../api/client';

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
  outreach_sent: { label: 'Outreach Sent', color: 'text-yellow-300', bg: 'bg-yellow-900/40 border-yellow-700' },
  responded: { label: 'Responded', color: 'text-blue-300', bg: 'bg-blue-900/40 border-blue-700' },
  testing: { label: 'Testing', color: 'text-orange-300', bg: 'bg-orange-900/40 border-orange-700' },
  converted: { label: 'Converted', color: 'text-green-300', bg: 'bg-green-900/40 border-green-700' },
  configured: { label: 'Configured', color: 'text-blue-300', bg: 'bg-blue-900/40 border-blue-700' },
  identified: { label: 'Identified', color: 'text-slate-400', bg: 'bg-slate-800 border-slate-700' },
  lost: { label: 'Lost', color: 'text-red-300', bg: 'bg-red-900/40 border-red-700' },
};

const EVENT_TYPE_COLORS: Record<string, string> = {
  email_sent: 'bg-yellow-900/50 text-yellow-300',
  email: 'bg-yellow-900/50 text-yellow-300',
  dm_sent: 'bg-purple-900/50 text-purple-300',
  call: 'bg-blue-900/50 text-blue-300',
  text: 'bg-green-900/50 text-green-300',
  note: 'bg-slate-700 text-slate-300',
  system_configured: 'bg-cyan-900/50 text-cyan-300',
  telnyx_activated: 'bg-teal-900/50 text-teal-300',
  e2e_tested: 'bg-emerald-900/50 text-emerald-300',
  response: 'bg-indigo-900/50 text-indigo-300',
  meeting: 'bg-pink-900/50 text-pink-300',
};

function formatDate(iso: string) {
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' }) +
    ' at ' + d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' });
}

function StatusIndicator({ on, label }: { on: boolean; label: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className={`w-2.5 h-2.5 rounded-full ${on ? 'bg-green-500' : 'bg-red-500/60'}`} />
      <span className="text-sm text-slate-300">{label}</span>
      <span className={`text-xs ${on ? 'text-green-400' : 'text-slate-500'}`}>{on ? 'Yes' : 'No'}</span>
    </div>
  );
}

interface Props {
  prospect: Prospect;
  onBack: () => void;
  onRefresh: () => void;
  dark?: boolean;
}

export function ProspectDetail({ prospect: p, onBack, onRefresh, dark = false }: Props) {
  const [addingEvent, setAddingEvent] = useState(false);
  const [eventType, setEventType] = useState('note');
  const [eventNote, setEventNote] = useState('');
  const [saving, setSaving] = useState(false);

  const s = STATUS_MAP[p.status] || { label: p.status, color: 'text-slate-400', bg: 'bg-slate-800 border-slate-700' };

  const sortedTimeline = [...(p.timeline || [])].sort(
    (a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()
  );

  const handleAddEvent = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!eventNote.trim()) return;
    setSaving(true);
    try {
      await addProspectEvent(p.id, eventType, eventNote.trim());
      setEventNote('');
      setAddingEvent(false);
      onRefresh();
    } finally {
      setSaving(false);
    }
  };

  // Use dark theme classes when in CEO Dashboard context
  const base = dark
    ? 'min-h-screen bg-slate-950 text-slate-100 p-4 sm:p-8'
    : 'min-h-screen bg-slate-950 text-slate-100 p-4 sm:p-8';

  return (
    <div className={base}>
      <div className="max-w-4xl mx-auto space-y-6">
        {/* Back button */}
        <button
          onClick={onBack}
          className="flex items-center gap-2 text-sm text-violet-400 hover:text-violet-300 transition mb-2"
        >
          ← Back to list
        </button>

        {/* Header */}
        <div className="rounded-xl border border-slate-800 bg-slate-900 p-6">
          <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
            <div className="flex items-start gap-4">
              <div className="w-14 h-14 rounded-xl bg-violet-900/50 text-violet-300 flex items-center justify-center text-lg font-bold shrink-0">
                {p.clinic.split(' ').map(w => w[0]).join('').slice(0, 2)}
              </div>
              <div>
                <h1 className="text-xl font-bold">{p.clinic}</h1>
                <div className="text-sm text-slate-400 mt-1">
                  {[p.owner && `${p.owner}${p.title ? ` (${p.title})` : ''}`, p.location].filter(Boolean).join(' · ')}
                </div>
                {p.emr && <div className="text-xs text-slate-500 mt-1">EMR: {p.emr}</div>}
              </div>
            </div>
            <span className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold uppercase tracking-wide ${s.bg} ${s.color} shrink-0`}>
              {s.label}
            </span>
          </div>
        </div>

        {/* Next Action - Prominent */}
        {p.nextAction && (
          <div className="rounded-xl border border-violet-700 bg-violet-950/50 p-5">
            <div className="text-xs font-semibold text-violet-400 uppercase tracking-wide mb-1">⚡ Next Action</div>
            <p className="text-base text-slate-200">{p.nextAction}</p>
          </div>
        )}

        {/* Info Grid */}
        <div className="grid md:grid-cols-2 gap-6">
          {/* Contact Info */}
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-sm font-semibold text-slate-400 uppercase tracking-wide mb-4">Contact Info</h2>
            <div className="space-y-3">
              <div>
                <div className="text-xs text-slate-500">Phone</div>
                <div className="text-sm text-slate-200">{p.phone || '—'}</div>
              </div>
              <div>
                <div className="text-xs text-slate-500">Email</div>
                <div className="text-sm text-slate-200">{p.email ? <a href={`mailto:${p.email}`} className="text-violet-400 hover:underline">{p.email}</a> : '—'}</div>
              </div>
              <div>
                <div className="text-xs text-slate-500">Website</div>
                <div className="text-sm text-slate-200">{p.website ? <a href={p.website} target="_blank" rel="noopener noreferrer" className="text-violet-400 hover:underline">{p.website}</a> : '—'}</div>
              </div>
            </div>
          </div>

          {/* System Status */}
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-sm font-semibold text-slate-400 uppercase tracking-wide mb-4">System Status</h2>
            <div className="space-y-3">
              <StatusIndicator on={p.configured} label="Configured" />
              <StatusIndicator on={!!p.telnyxNumber} label="Telnyx Number" />
              {p.telnyxNumber && <div className="text-xs text-slate-500 ml-5">{p.telnyxNumber}</div>}
              <StatusIndicator on={p['10dlc']} label="10DLC Approved" />
              <StatusIndicator on={p.smsWorking} label="SMS Working" />
            </div>
            {p.notes && (
              <div className="mt-4 pt-4 border-t border-slate-800">
                <div className="text-xs text-slate-500">Notes</div>
                <p className="text-sm text-slate-300 mt-1">{p.notes}</p>
              </div>
            )}
          </div>
        </div>

        {/* Quick Actions */}
        <div className="flex gap-3">
          <button
            onClick={() => setAddingEvent(!addingEvent)}
            className="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition"
          >
            {addingEvent ? '✕ Cancel' : '+ Add Event'}
          </button>
        </div>

        {/* Add Event Form */}
        {addingEvent && (
          <form onSubmit={handleAddEvent} className="rounded-xl border border-slate-800 bg-slate-900 p-5 space-y-4">
            <h3 className="text-sm font-semibold text-slate-300">Add Event</h3>
            <div className="flex gap-3">
              <select
                value={eventType}
                onChange={e => setEventType(e.target.value)}
                className="rounded-lg bg-slate-800 border border-slate-700 text-slate-200 text-sm px-3 py-2 focus:outline-none focus:ring-2 focus:ring-violet-500"
              >
                <option value="note">Note</option>
                <option value="email_sent">Email Sent</option>
                <option value="email">Email</option>
                <option value="call">Call</option>
                <option value="text">Text</option>
                <option value="dm_sent">DM Sent</option>
                <option value="meeting">Meeting</option>
                <option value="response">Response</option>
                <option value="system_configured">System Configured</option>
                <option value="telnyx_activated">Telnyx Activated</option>
                <option value="e2e_tested">E2E Tested</option>
              </select>
            </div>
            <textarea
              value={eventNote}
              onChange={e => setEventNote(e.target.value)}
              placeholder="Full event details, outreach content, notes..."
              rows={6}
              className="w-full rounded-lg bg-slate-800 border border-slate-700 text-slate-200 text-sm px-3 py-2 focus:outline-none focus:ring-2 focus:ring-violet-500 placeholder-slate-500"
            />
            <div className="flex gap-3">
              <button
                type="submit"
                disabled={saving || !eventNote.trim()}
                className="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 text-white text-sm font-medium transition"
              >
                {saving ? 'Saving...' : 'Save Event'}
              </button>
              <button
                type="button"
                onClick={() => setAddingEvent(false)}
                className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-sm transition"
              >
                Cancel
              </button>
            </div>
          </form>
        )}

        {/* Outreach History / Timeline */}
        <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
          <h2 className="text-sm font-semibold text-slate-400 uppercase tracking-wide mb-4">
            Outreach History ({sortedTimeline.length} events)
          </h2>
          {sortedTimeline.length === 0 ? (
            <p className="text-sm text-slate-500">No events recorded yet.</p>
          ) : (
            <div className="space-y-4">
              {sortedTimeline.map((ev, i) => {
                const typeColor = EVENT_TYPE_COLORS[ev.type] || 'bg-slate-700 text-slate-300';
                return (
                  <div key={ev.id || i} className="border-l-2 border-slate-700 pl-4 py-1">
                    <div className="flex items-center gap-3 mb-1">
                      <span className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-semibold uppercase ${typeColor}`}>
                        {ev.type.replace(/_/g, ' ')}
                      </span>
                      <span className="text-xs text-slate-500 font-mono">{formatDate(ev.date)}</span>
                    </div>
                    <div className="text-sm text-slate-300 whitespace-pre-wrap leading-relaxed">{ev.note}</div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
