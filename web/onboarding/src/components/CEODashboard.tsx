import { useEffect, useState, useCallback } from 'react';
import { listProspects } from '../api/client';

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
  status: string;
  configured: boolean;
  smsWorking: boolean;
  orgId: string;
  nextAction: string;
  timeline: ProspectEvent[];
  createdAt: string;
  updatedAt: string;
  [key: string]: unknown;
}

// Revenue model constants
const PRICE_PER_CLIENT = 497;
const TARGET_ARR = 5_000_000;
const TARGET_CLIENTS = Math.ceil(TARGET_ARR / (PRICE_PER_CLIENT * 12));
const TARGET_DATE = new Date('2027-05-01');

function generateMilestones(): Array<{ date: string; clients: number; mrr: number }> {
  const milestones: Array<{ date: string; clients: number; mrr: number }> = [];
  const start = new Date('2025-03-01');
  const months = (TARGET_DATE.getFullYear() - start.getFullYear()) * 12 + (TARGET_DATE.getMonth() - start.getMonth());
  for (let i = 0; i <= months; i += 3) {
    const d = new Date(start);
    d.setMonth(d.getMonth() + i);
    const frac = i / months;
    const clients = Math.round(TARGET_CLIENTS * frac);
    milestones.push({
      date: d.toLocaleDateString('en-US', { month: 'short', year: 'numeric' }),
      clients,
      mrr: clients * PRICE_PER_CLIENT,
    });
  }
  // Ensure final milestone
  const last = milestones[milestones.length - 1];
  if (last && last.clients < TARGET_CLIENTS) {
    milestones.push({ date: 'May 2027', clients: TARGET_CLIENTS, mrr: TARGET_CLIENTS * PRICE_PER_CLIENT });
  }
  return milestones;
}

const MILESTONES = generateMilestones();

const FUNNEL_STAGES = [
  { key: 'identified', label: 'Identified', color: 'bg-slate-500' },
  { key: 'outreach_sent', label: 'Outreach Sent', color: 'bg-yellow-500' },
  { key: 'responded,testing', label: 'Demo / Trial', color: 'bg-blue-500' },
  { key: 'converted', label: 'Paying', color: 'bg-green-500' },
  { key: 'lost', label: 'Churned', color: 'bg-red-500' },
];

const ACTION_ITEMS = [
  { text: 'Approve outreach templates for next batch', priority: 'high' },
  { text: 'Refresh AWS credentials (expiring soon)', priority: 'high' },
  { text: 'Review and approve Glow Medspa demo results', priority: 'medium' },
  { text: 'Sign off on 10DLC campaign for new prospects', priority: 'medium' },
  { text: 'Update investor deck with latest metrics', priority: 'low' },
];

const WEEKLY_BRIEF = `## Weekly Brief — Feb 17, 2025

**Pipeline:** 12 prospects identified, 4 outreach emails sent, 1 demo scheduled.

**Platform:** SMS routing stable. Telnyx numbers provisioned for 2 new clinics. 10DLC campaigns pending approval.

**Blockers:** AWS creds need rotation. Outreach templates need CEO sign-off before next batch.

**Next Week:** Target 10 new outreach emails. Follow up on Glow Medspa demo. Begin Stripe integration testing.`;

export function CEODashboard() {
  const [prospects, setProspects] = useState<Prospect[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      const data = await listProspects();
      setProspects(data.prospects as Prospect[]);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  // Compute metrics
  const totalProspects = prospects.length;
  const activeOutreach = prospects.filter(p => p.status === 'outreach_sent' || p.status === 'responded').length;
  const configuredClinics = prospects.filter(p => p.configured).length;
  const smsWorking = prospects.filter(p => p.smsWorking).length;
  const payingClients = prospects.filter(p => p.status === 'converted').length;
  const currentMRR = payingClients * PRICE_PER_CLIENT;

  // Funnel counts
  const funnelCounts = FUNNEL_STAGES.map(stage => {
    const keys = stage.key.split(',');
    const count = prospects.filter(p => keys.includes(p.status)).length;
    return { ...stage, count };
  });
  const maxFunnel = Math.max(...funnelCounts.map(f => f.count), 1);

  // All timeline events, sorted recent first
  const allEvents = prospects
    .flatMap(p => (p.timeline || []).map(e => ({ ...e, clinic: p.clinic })))
    .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime())
    .slice(0, 20);

  if (loading) {
    return (
      <div className="min-h-screen bg-slate-950 flex items-center justify-center">
        <div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-700 border-t-violet-500" />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 p-4 sm:p-8">
      <div className="max-w-7xl mx-auto space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">CEO Dashboard</h1>
            <p className="text-sm text-slate-400 mt-1">Medspa AI Platform — Road to $50M</p>
          </div>
          <button onClick={refresh} className="text-sm text-violet-400 hover:text-violet-300 transition">
            ↻ Refresh
          </button>
        </div>

        {error && (
          <div className="rounded-lg border border-red-800 bg-red-950 p-3 text-sm text-red-300">{error}</div>
        )}

        {/* Key Metrics Cards */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {[
            { label: 'Total Prospects', value: totalProspects, icon: '🎯' },
            { label: 'Active Outreach', value: activeOutreach, icon: '📧' },
            { label: 'Configured Clinics', value: configuredClinics, icon: '⚙️' },
            { label: 'SMS Working', value: smsWorking, icon: '💬' },
          ].map(m => (
            <div key={m.label} className="rounded-xl border border-slate-800 bg-slate-900 p-4">
              <div className="flex items-center gap-2 text-sm text-slate-400">
                <span>{m.icon}</span>
                <span>{m.label}</span>
              </div>
              <div className="mt-2 text-3xl font-bold">{m.value}</div>
            </div>
          ))}
        </div>

        <div className="grid lg:grid-cols-2 gap-6">
          {/* Pipeline Funnel */}
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-lg font-semibold mb-4">Pipeline Funnel</h2>
            <div className="space-y-3">
              {funnelCounts.map(stage => (
                <div key={stage.key}>
                  <div className="flex justify-between text-sm mb-1">
                    <span className="text-slate-300">{stage.label}</span>
                    <span className="font-mono text-slate-400">{stage.count}</span>
                  </div>
                  <div className="h-6 bg-slate-800 rounded-full overflow-hidden">
                    <div
                      className={`h-full ${stage.color} rounded-full transition-all duration-500`}
                      style={{ width: `${Math.max((stage.count / maxFunnel) * 100, stage.count > 0 ? 8 : 0)}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Revenue Tracker */}
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-lg font-semibold mb-2">Revenue Tracker</h2>
            <p className="text-xs text-slate-500 mb-4">$50M exit = ~10x revenue = $5M ARR = {TARGET_CLIENTS} clients @ ${PRICE_PER_CLIENT}/mo</p>

            <div className="flex items-baseline gap-3 mb-4">
              <div>
                <div className="text-xs text-slate-500 uppercase tracking-wide">Current MRR</div>
                <div className="text-2xl font-bold text-green-400">${currentMRR.toLocaleString()}</div>
              </div>
              <div>
                <div className="text-xs text-slate-500 uppercase tracking-wide">Target MRR</div>
                <div className="text-2xl font-bold text-slate-400">${Math.round(TARGET_ARR / 12).toLocaleString()}</div>
              </div>
              <div>
                <div className="text-xs text-slate-500 uppercase tracking-wide">Clients</div>
                <div className="text-2xl font-bold">{payingClients}<span className="text-slate-500 text-lg">/{TARGET_CLIENTS}</span></div>
              </div>
            </div>

            {/* Progress bar */}
            <div className="mb-4">
              <div className="h-4 bg-slate-800 rounded-full overflow-hidden">
                <div
                  className="h-full bg-gradient-to-r from-violet-600 to-green-500 rounded-full transition-all"
                  style={{ width: `${Math.min((payingClients / TARGET_CLIENTS) * 100, 100)}%` }}
                />
              </div>
              <div className="flex justify-between text-xs text-slate-500 mt-1">
                <span>{((payingClients / TARGET_CLIENTS) * 100).toFixed(1)}%</span>
                <span>May 2027</span>
              </div>
            </div>

            {/* Milestones */}
            <div className="max-h-36 overflow-y-auto pr-3">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-slate-500 border-b border-slate-800">
                    <th className="text-left py-1">Date</th>
                    <th className="text-right py-1">Clients</th>
                    <th className="text-right py-1">MRR</th>
                  </tr>
                </thead>
                <tbody>
                  {MILESTONES.map(m => (
                    <tr key={m.date} className="border-b border-slate-800/50">
                      <td className="py-1 text-slate-400">{m.date}</td>
                      <td className="py-1 text-right font-mono">{m.clients}</td>
                      <td className="py-1 text-right font-mono text-green-400">${m.mrr.toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div className="grid lg:grid-cols-2 gap-6">
          {/* Weekly Brief */}
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-lg font-semibold mb-3">📋 Weekly Brief</h2>
            <div id="weekly-brief" className="text-sm text-slate-300 space-y-2 leading-relaxed">
              {WEEKLY_BRIEF.split('\n').map((line, i) => {
                if (line.startsWith('## ')) return <h3 key={i} className="text-base font-semibold text-slate-100 mt-2">{line.replace('## ', '')}</h3>;
                if (line.startsWith('**') && line.includes(':**')) {
                  const [label, ...rest] = line.split(':**');
                  return <p key={i}><strong className="text-violet-400">{label.replace(/\*\*/g, '')}:</strong>{rest.join(':**').replace(/\*\*/g, '')}</p>;
                }
                if (line.trim()) return <p key={i}>{line}</p>;
                return null;
              })}
            </div>
          </div>

          {/* Action Items */}
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-lg font-semibold mb-3">🚨 Action Items <span className="text-sm font-normal text-slate-500">(blocked on you)</span></h2>
            <div className="space-y-2">
              {ACTION_ITEMS.map((item, i) => (
                <div key={i} className="flex items-start gap-3 p-2 rounded-lg hover:bg-slate-800 transition">
                  <span className={`mt-0.5 w-2 h-2 rounded-full flex-shrink-0 ${
                    item.priority === 'high' ? 'bg-red-500' : item.priority === 'medium' ? 'bg-yellow-500' : 'bg-slate-500'
                  }`} />
                  <span className="text-sm text-slate-300">{item.text}</span>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Timeline */}
        <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
          <h2 className="text-lg font-semibold mb-3">📜 Recent Activity</h2>
          {allEvents.length === 0 ? (
            <p className="text-sm text-slate-500">No activity yet.</p>
          ) : (
            <div className="space-y-2 max-h-80 overflow-y-auto">
              {allEvents.map((ev, i) => (
                <div key={i} className="flex items-start gap-3 p-2 rounded-lg hover:bg-slate-800/50 transition text-sm">
                  <span className="text-xs text-slate-500 whitespace-nowrap font-mono min-w-[110px]">
                    {new Date(ev.date).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}{' '}
                    {new Date(ev.date).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
                  </span>
                  <span className="px-1.5 py-0.5 rounded text-xs bg-slate-800 text-slate-400 whitespace-nowrap">{ev.type}</span>
                  <span className="text-slate-300">
                    <span className="text-violet-400 font-medium">{ev.clinic}</span> — {ev.note}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
