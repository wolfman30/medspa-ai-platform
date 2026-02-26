import { useEffect, useMemo, useState } from 'react';
import { getRevenueDashboard, type RevenueDashboardResponse } from '../api/client';

interface Props {
  orgId?: string;
  period?: 'week' | 'month' | 'all';
}

const money = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 });

export function RevenueAttributionCard({ orgId, period = 'month' }: Props) {
  const [data, setData] = useState<RevenueDashboardResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!orgId) return;
    setLoading(true);
    getRevenueDashboard(orgId, period)
      .then(setData)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load revenue attribution'))
      .finally(() => setLoading(false));
  }, [orgId, period]);

  const funnel = useMemo(() => {
    if (!data) return [];
    return [
      { label: 'Missed Calls', count: data.funnel.missed_calls.count, pct: data.funnel.missed_calls.percentage },
      { label: 'Conversations', count: data.funnel.conversations.count, pct: data.funnel.conversations.percentage },
      { label: 'Qualified', count: data.funnel.qualified.count, pct: data.funnel.qualified.percentage },
      { label: 'Booked', count: data.funnel.booked.count, pct: data.funnel.booked.percentage },
    ];
  }, [data]);

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold">Revenue Attribution</h2>
        <span className="text-xs px-2 py-1 rounded bg-violet-500/20 text-violet-300 border border-violet-500/30 uppercase">{period}</span>
      </div>

      {!orgId && <p className="text-sm text-slate-500">Select a clinic org to view revenue attribution.</p>}
      {loading && <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-700 border-t-violet-500 mx-auto my-6" />}
      {error && <div className="rounded border border-red-900 bg-red-950 p-3 text-sm text-red-300">{error}</div>}

      {data && !loading && (
        <div className="space-y-6">
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            {[
              { label: 'Revenue Recovered', value: money.format(data.revenue_recovered), accent: 'text-violet-300' },
              { label: 'Missed Calls Caught', value: data.missed_calls_caught.toLocaleString(), accent: 'text-slate-100' },
              { label: 'Appointments Booked', value: data.appointments_booked.toLocaleString(), accent: 'text-slate-100' },
              { label: 'ROI Multiplier', value: `${data.roi_multiplier.toFixed(2)}x`, accent: 'text-emerald-300' },
            ].map((m) => (
              <div key={m.label} className="rounded-lg border border-slate-800 bg-slate-950/60 p-3">
                <div className="text-xs text-slate-500 uppercase tracking-wide">{m.label}</div>
                <div className={`text-2xl font-bold mt-1 ${m.accent}`}>{m.value}</div>
              </div>
            ))}
          </div>

          <div>
            <h3 className="text-sm font-semibold text-slate-200 mb-2">Lead Funnel</h3>
            <div className="space-y-2">
              {funnel.map((stage, idx) => (
                <div key={stage.label}>
                  <div className="flex justify-between text-xs text-slate-400 mb-1">
                    <span>{idx + 1}. {stage.label}</span>
                    <span>{stage.count} ({stage.pct.toFixed(1)}%)</span>
                  </div>
                  <div className="h-4 rounded-full bg-slate-800 overflow-hidden">
                    <div className="h-full bg-gradient-to-r from-violet-700 to-violet-400" style={{ width: `${Math.max(stage.pct, stage.count > 0 ? 6 : 0)}%` }} />
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div>
            <h3 className="text-sm font-semibold text-slate-200 mb-2">Top Services</h3>
            <div className="overflow-x-auto rounded-lg border border-slate-800">
              <table className="w-full text-sm">
                <thead className="bg-slate-950">
                  <tr className="text-slate-400">
                    <th className="text-left p-2">Service</th>
                    <th className="text-right p-2">Count</th>
                    <th className="text-right p-2">Revenue</th>
                  </tr>
                </thead>
                <tbody>
                  {data.top_services.length === 0 ? (
                    <tr><td colSpan={3} className="p-3 text-slate-500 text-center">No booked service data yet.</td></tr>
                  ) : data.top_services.map((s) => (
                    <tr key={s.service} className="border-t border-slate-800">
                      <td className="p-2 text-slate-200">{s.service}</td>
                      <td className="p-2 text-right font-mono text-slate-300">{s.count}</td>
                      <td className="p-2 text-right font-mono text-violet-300">{money.format(s.revenue)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
