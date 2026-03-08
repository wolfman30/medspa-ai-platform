import { useEffect, useState, useCallback } from 'react';
import { getClinicStats, type ClinicStats } from '../api/client';

type Period = '7d' | '30d' | '90d' | 'all';

const PERIODS: { key: Period; label: string }[] = [
  { key: '7d', label: '7 Days' },
  { key: '30d', label: '30 Days' },
  { key: '90d', label: '90 Days' },
  { key: 'all', label: 'All Time' },
];

function periodToRange(period: Period): { start?: string; end?: string } {
  if (period === 'all') return {};
  const now = new Date();
  const end = now.toISOString();
  const days = period === '7d' ? 7 : period === '30d' ? 30 : 90;
  const start = new Date(now.getTime() - days * 86400000).toISOString();
  return { start, end };
}

function formatCents(cents: number): string {
  const dollars = cents / 100;
  return `$${dollars.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

function conversionRate(started: number, paid: number): string {
  if (started === 0) return '--';
  return `${((paid / started) * 100).toFixed(1)}%`;
}

interface Props {
  orgId: string;
}

export function ClinicStatsCard({ orgId }: Props) {
  const [period, setPeriod] = useState<Period>('30d');
  const [stats, setStats] = useState<ClinicStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const range = periodToRange(period);
      const data = await getClinicStats(orgId, range.start, range.end);
      setStats(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load stats');
      setStats(null);
    } finally {
      setLoading(false);
    }
  }, [orgId, period]);

  useEffect(() => { load(); }, [load]);

  const kpis = stats ? [
    { label: 'Conversations', value: stats.conversations_started.toLocaleString(), icon: '💬' },
    { label: 'Deposits Requested', value: stats.deposits_requested.toLocaleString(), icon: '📋' },
    { label: 'Deposits Paid', value: stats.deposits_paid.toLocaleString(), icon: '✅' },
    { label: 'Revenue Collected', value: formatCents(stats.deposit_amount_total_cents), icon: '💰' },
    { label: 'Conversion Rate', value: conversionRate(stats.conversations_started, stats.deposits_paid), icon: '📈' },
    { label: 'Avg Deposit', value: stats.deposits_paid > 0 ? formatCents(Math.round(stats.deposit_amount_total_cents / stats.deposits_paid)) : '--', icon: '🎯' },
  ] : [];

  return (
    <div className="ui-card ui-card-solid p-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-slate-900">📊 Clinic Performance</h2>
        <div className="flex gap-1">
          {PERIODS.map(p => (
            <button
              key={p.key}
              onClick={() => setPeriod(p.key)}
              className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                period === p.key
                  ? 'bg-violet-600 text-white'
                  : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-8">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
        </div>
      ) : error ? (
        <div className="text-sm text-red-600 py-4">{error}</div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
          {kpis.map(kpi => (
            <div key={kpi.label} className="text-center">
              <div className="text-2xl mb-1">{kpi.icon}</div>
              <div className="text-xl font-bold text-slate-900">{kpi.value}</div>
              <div className="text-xs text-slate-500 mt-0.5">{kpi.label}</div>
            </div>
          ))}
        </div>
      )}

      {stats && stats.conversations_started > 0 && !loading && (
        <div className="mt-4 pt-4 border-t border-slate-100">
          <div className="flex items-center gap-4 text-xs text-slate-500">
            <span>Period: {stats.period_start === 'all-time' ? 'All time' : new Date(stats.period_start).toLocaleDateString()} – {stats.period_end === 'now' ? 'Now' : new Date(stats.period_end).toLocaleDateString()}</span>
            {stats.deposits_paid > 0 && (
              <span className="text-green-600 font-medium">
                💡 ROI: {stats.deposit_amount_total_cents > 50000
                  ? `${(stats.deposit_amount_total_cents / 50000).toFixed(1)}x vs $500/mo subscription`
                  : 'Building momentum...'}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
