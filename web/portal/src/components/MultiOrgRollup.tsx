import { useEffect, useState, useCallback } from 'react';
import { listOrgs, getPortalOverview, type OrgListItem, type PortalDashboardOverview } from '../api/client';

interface OrgStats {
  org: OrgListItem;
  overview: PortalDashboardOverview | null;
  error?: string;
}

function formatCents(cents: number): string {
  return `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

function formatPct(pct: number): string {
  return `${pct.toFixed(1)}%`;
}

export function MultiOrgRollup() {
  const [orgStats, setOrgStats] = useState<OrgStats[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const { organizations } = await listOrgs();
      const results = await Promise.allSettled(
        organizations.map(async (org) => {
          try {
            const overview = await getPortalOverview(org.id);
            return { org, overview } as OrgStats;
          } catch (err) {
            return { org, overview: null, error: err instanceof Error ? err.message : 'Failed' } as OrgStats;
          }
        })
      );
      setOrgStats(
        results.map((r) => (r.status === 'fulfilled' ? r.value : { org: { id: '', name: '?', created_at: '' }, overview: null, error: 'Unknown' }))
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load organizations');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchAll(); }, [fetchAll]);

  const withData = orgStats.filter((s) => s.overview !== null);
  const totals = withData.reduce(
    (acc, s) => {
      if (!s.overview) return acc;
      acc.conversations += s.overview.conversations;
      acc.deposits += s.overview.successful_deposits;
      acc.collected += s.overview.total_collected_cents;
      return acc;
    },
    { conversations: 0, deposits: 0, collected: 0 }
  );
  const overallConversion = totals.conversations > 0
    ? (totals.deposits / totals.conversations) * 100
    : 0;

  if (loading) {
    return (
      <div className="p-6 flex items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-6 text-center">
        <p className="text-red-600 text-sm">{error}</p>
        <button onClick={fetchAll} className="mt-2 ui-btn ui-btn-ghost text-sm">Retry</button>
      </div>
    );
  }

  return (
    <div className="p-4 md:p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-slate-900">Multi-Location Revenue Rollup</h2>
        <button onClick={fetchAll} className="ui-btn ui-btn-ghost text-sm">↻ Refresh</button>
      </div>

      {/* Aggregate totals */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard label="Total Conversations" value={totals.conversations.toLocaleString()} />
        <StatCard label="Total Deposits" value={totals.deposits.toLocaleString()} />
        <StatCard label="Total Collected" value={formatCents(totals.collected)} highlight />
        <StatCard label="Avg Conversion" value={formatPct(overallConversion)} />
      </div>

      {/* Per-org breakdown */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200 text-left text-slate-500">
              <th className="py-2 pr-4 font-medium">Clinic</th>
              <th className="py-2 pr-4 font-medium text-right">Conversations</th>
              <th className="py-2 pr-4 font-medium text-right">Deposits</th>
              <th className="py-2 pr-4 font-medium text-right">Collected</th>
              <th className="py-2 font-medium text-right">Conversion</th>
            </tr>
          </thead>
          <tbody>
            {orgStats.map((s) => (
              <tr key={s.org.id} className="border-b border-slate-100 hover:bg-slate-50">
                <td className="py-2 pr-4">
                  <div className="font-medium text-slate-900">{s.org.name || s.org.id.slice(0, 8)}</div>
                  {s.org.owner_email && (
                    <div className="text-xs text-slate-400">{s.org.owner_email}</div>
                  )}
                </td>
                {s.overview ? (
                  <>
                    <td className="py-2 pr-4 text-right tabular-nums">{s.overview.conversations}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{s.overview.successful_deposits}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{formatCents(s.overview.total_collected_cents)}</td>
                    <td className="py-2 text-right tabular-nums">{formatPct(s.overview.conversion_pct)}</td>
                  </>
                ) : (
                  <td colSpan={4} className="py-2 text-right text-slate-400 italic">
                    {s.error || 'No data'}
                  </td>
                )}
              </tr>
            ))}
            {orgStats.length === 0 && (
              <tr>
                <td colSpan={5} className="py-8 text-center text-slate-400">No clinics found</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {withData.length > 1 && (
        <div className="text-xs text-slate-400 text-center">
          Showing current billing period across {withData.length} clinic{withData.length !== 1 ? 's' : ''}
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className={`rounded-lg border p-4 ${highlight ? 'border-green-200 bg-green-50' : 'border-slate-200 bg-white'}`}>
      <div className="text-xs text-slate-500 mb-1">{label}</div>
      <div className={`text-xl font-semibold tabular-nums ${highlight ? 'text-green-700' : 'text-slate-900'}`}>
        {value}
      </div>
    </div>
  );
}
