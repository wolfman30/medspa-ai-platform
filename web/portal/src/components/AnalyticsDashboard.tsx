import { useEffect, useState } from 'react';
import {
  getLeadStats,
  getConversationStats,
  getDepositStats,
  type LeadStats,
  type ConversationStats,
} from '../api/client';
import type { DepositStatsResponse } from '../types/deposit';

interface AnalyticsDashboardProps {
  orgId: string;
}

export function AnalyticsDashboard({ orgId }: AnalyticsDashboardProps) {
  const [leadStats, setLeadStats] = useState<LeadStats | null>(null);
  const [convStats, setConvStats] = useState<ConversationStats | null>(null);
  const [depositStats, setDepositStats] = useState<DepositStatsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);

    Promise.allSettled([
      getLeadStats(orgId),
      getConversationStats(orgId),
      getDepositStats(orgId, 'admin'),
    ]).then(([leadResult, convResult, depositResult]) => {
      if (leadResult.status === 'fulfilled') setLeadStats(leadResult.value);
      if (convResult.status === 'fulfilled') setConvStats(convResult.value);
      if (depositResult.status === 'fulfilled') setDepositStats(depositResult.value);

      const allFailed = leadResult.status === 'rejected' &&
        convResult.status === 'rejected' &&
        depositResult.status === 'rejected';
      if (allFailed) setError('Failed to load analytics data');
      setLoading(false);
    });
  }, [orgId]);

  if (loading) {
    return (
      <div className="ui-page flex items-center justify-center py-20">
        <div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="ui-container py-8">
        <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-center">
          <p className="text-sm font-medium text-red-800">{error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="ui-container py-8 space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-900">Analytics</h1>
        <p className="ui-muted mt-1">Performance metrics for this clinic</p>
      </div>

      {/* KPI Summary Cards */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <StatCard
          label="Total Conversations"
          value={convStats?.total_conversations ?? 0}
        />
        <StatCard
          label="Total Leads"
          value={leadStats?.total_leads ?? 0}
        />
        <StatCard
          label="Conversion Rate"
          value={`${(leadStats?.conversion_rate ?? 0).toFixed(1)}%`}
        />
        <StatCard
          label="Total Deposits"
          value={depositStats?.total_deposits ?? 0}
          subtext={depositStats?.total_amount_cents ? `$${(depositStats.total_amount_cents / 100).toFixed(2)}` : undefined}
        />
      </div>

      {/* Conversation Trends */}
      {convStats && (
        <div className="ui-card ui-card-solid p-6">
          <h2 className="text-lg font-semibold text-slate-900 mb-4">Conversation Volume</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <TrendItem label="Today" value={convStats.today_count} />
            <TrendItem label="This Week" value={convStats.week_count} />
            <TrendItem label="This Month" value={convStats.month_count} />
            <TrendItem label="6 Months" value={convStats.six_month_count} />
          </div>
          <div className="mt-4 pt-4 border-t border-slate-100">
            <p className="text-sm text-slate-500">
              Total messages exchanged: <span className="font-semibold text-slate-700">{convStats.total_messages.toLocaleString()}</span>
            </p>
          </div>
        </div>
      )}

      {/* Lead Pipeline */}
      {leadStats && (
        <div className="ui-card ui-card-solid p-6">
          <h2 className="text-lg font-semibold text-slate-900 mb-4">Lead Pipeline</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <TrendItem label="New This Week" value={leadStats.new_this_week} />
            <TrendItem label="New This Month" value={leadStats.new_this_month} />
            <TrendItem label="Total" value={leadStats.total_leads} />
          </div>
          {Object.keys(leadStats.by_status).length > 0 && (
            <div className="mt-4 pt-4 border-t border-slate-100">
              <h3 className="text-sm font-medium text-slate-700 mb-2">By Status</h3>
              <div className="flex flex-wrap gap-2">
                {Object.entries(leadStats.by_status)
                  .sort(([, a], [, b]) => b - a)
                  .map(([status, count]) => (
                    <StatusBadge key={status} status={status} count={count} />
                  ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Conversation Status Breakdown */}
      {convStats && Object.keys(convStats.by_status).length > 0 && (
        <div className="ui-card ui-card-solid p-6">
          <h2 className="text-lg font-semibold text-slate-900 mb-4">Conversation Status</h2>
          <div className="flex flex-wrap gap-2">
            {Object.entries(convStats.by_status)
              .sort(([, a], [, b]) => b - a)
              .map(([status, count]) => (
                <StatusBadge key={status} status={status} count={count} />
              ))}
          </div>
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, subtext }: { label: string; value: number | string; subtext?: string }) {
  return (
    <div className="ui-card ui-card-solid p-4 text-center">
      <div className="text-2xl font-bold text-slate-900">
        {typeof value === 'number' ? value.toLocaleString() : value}
      </div>
      <div className="text-xs text-slate-500 mt-1">{label}</div>
      {subtext && <div className="text-sm font-medium text-emerald-600 mt-1">{subtext}</div>}
    </div>
  );
}

function TrendItem({ label, value }: { label: string; value: number }) {
  return (
    <div className="text-center p-3 rounded-lg bg-slate-50">
      <div className="text-xl font-semibold text-slate-900">{value.toLocaleString()}</div>
      <div className="text-xs text-slate-500 mt-1">{label}</div>
    </div>
  );
}

const STATUS_COLORS: Record<string, string> = {
  new: 'bg-blue-100 text-blue-800',
  active: 'bg-green-100 text-green-800',
  qualified: 'bg-emerald-100 text-emerald-800',
  converted: 'bg-violet-100 text-violet-800',
  booked: 'bg-violet-100 text-violet-800',
  completed: 'bg-slate-100 text-slate-700',
  lost: 'bg-red-100 text-red-700',
  stale: 'bg-amber-100 text-amber-800',
};

function StatusBadge({ status, count }: { status: string; count: number }) {
  const colors = STATUS_COLORS[status.toLowerCase()] || 'bg-slate-100 text-slate-700';
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium ${colors}`}>
      {status}
      <span className="font-bold">{count}</span>
    </span>
  );
}
