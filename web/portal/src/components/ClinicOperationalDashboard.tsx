import { useEffect, useState } from 'react';
import {
  getClinicOperationalDashboard,
  type ClinicOperationalDashboard,
} from '../api/client';

interface Props {
  orgId: string;
}

const PERIOD_OPTIONS = [
  { label: '7 days', value: 7 },
  { label: '14 days', value: 14 },
  { label: '30 days', value: 30 },
  { label: '90 days', value: 90 },
];

function StatCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string;
  value: string;
  sub?: string;
  accent?: string;
}) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <p className="text-xs font-medium uppercase tracking-wider text-slate-500">
        {label}
      </p>
      <p className={`mt-1 text-2xl font-bold ${accent || 'text-slate-900'}`}>
        {value}
      </p>
      {sub && <p className="mt-0.5 text-xs text-slate-400">{sub}</p>}
    </div>
  );
}

function MiniBarChart({
  data,
}: {
  data: Array<{ day: string; missed_call_leads: number; paid_leads: number }>;
}) {
  if (!data.length) {
    return (
      <p className="py-8 text-center text-sm text-slate-400">
        No data for this period
      </p>
    );
  }

  const maxVal = Math.max(...data.map((d) => d.missed_call_leads), 1);

  return (
    <div className="flex items-end gap-1" style={{ height: 120 }}>
      {data.map((d) => {
        const missedH = (d.missed_call_leads / maxVal) * 100;
        const paidH = (d.paid_leads / maxVal) * 100;
        const dayLabel = d.day.slice(5); // MM-DD
        return (
          <div
            key={d.day}
            className="flex flex-1 flex-col items-center gap-0.5"
            title={`${d.day}: ${d.missed_call_leads} missed-call leads, ${d.paid_leads} paid`}
          >
            <div className="relative flex w-full flex-col items-center" style={{ height: 100 }}>
              <div
                className="absolute bottom-0 w-full max-w-[24px] rounded-t bg-slate-200"
                style={{ height: `${missedH}%` }}
              />
              <div
                className="absolute bottom-0 w-full max-w-[24px] rounded-t bg-violet-500"
                style={{ height: `${paidH}%` }}
              />
            </div>
            <span className="text-[10px] text-slate-400">{dayLabel}</span>
          </div>
        );
      })}
    </div>
  );
}

export function ClinicOperationalDashboard({ orgId }: Props) {
  const [data, setData] = useState<ClinicOperationalDashboard | null>(null);
  const [days, setDays] = useState(7);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    getClinicOperationalDashboard(orgId, days)
      .then((d) => {
        if (active) setData(d);
      })
      .catch((err) => {
        if (active) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [orgId, days]);

  return (
    <div className="mx-auto max-w-4xl px-4 py-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight text-slate-900">
            📊 Operational Dashboard
          </h2>
          <p className="text-sm text-slate-500">
            Missed-call recovery &amp; AI performance
          </p>
        </div>
        <div className="flex gap-1">
          {PERIOD_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              onClick={() => setDays(opt.value)}
              className={
                days === opt.value
                  ? 'ui-btn ui-btn-dark text-xs'
                  : 'ui-btn ui-btn-ghost text-xs'
              }
            >
              {opt.label}
            </button>
          ))}
        </div>
      </div>

      {loading && (
        <div className="mt-8 flex justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
        </div>
      )}

      {error && (
        <div className="mt-4 rounded-xl border border-red-200 bg-red-50 p-4">
          <p className="text-sm text-red-700">{error}</p>
        </div>
      )}

      {!loading && !error && data && (
        <>
          {/* KPI cards */}
          <div className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <StatCard
              label="Missed-Call Leads"
              value={data.missed_call_leads.toLocaleString()}
              sub={`Last ${days} days`}
            />
            <StatCard
              label="Paid / Booked"
              value={data.missed_call_paid_leads.toLocaleString()}
              accent="text-emerald-600"
            />
            <StatCard
              label="Conversion"
              value={`${data.missed_call_conversion_pct.toFixed(1)}%`}
              accent={
                data.missed_call_conversion_pct >= 20
                  ? 'text-emerald-600'
                  : data.missed_call_conversion_pct >= 10
                    ? 'text-amber-600'
                    : 'text-red-600'
              }
            />
            <StatCard
              label="AI Response (p90)"
              value={
                data.llm_latency.p90_ms > 0
                  ? `${(data.llm_latency.p90_ms / 1000).toFixed(1)}s`
                  : '--'
              }
              sub={
                data.llm_latency.p95_ms > 0
                  ? `p95: ${(data.llm_latency.p95_ms / 1000).toFixed(1)}s`
                  : undefined
              }
            />
          </div>

          {/* Daily trend chart */}
          <div className="mt-6 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
            <div className="mb-3 flex items-center gap-4">
              <h3 className="text-sm font-medium text-slate-700">
                Daily Trend
              </h3>
              <div className="flex items-center gap-3 text-xs text-slate-400">
                <span className="flex items-center gap-1">
                  <span className="inline-block h-2 w-2 rounded bg-slate-200" />
                  Missed-call leads
                </span>
                <span className="flex items-center gap-1">
                  <span className="inline-block h-2 w-2 rounded bg-violet-500" />
                  Paid
                </span>
              </div>
            </div>
            <MiniBarChart data={data.daily} />
          </div>

          {/* ROI callout */}
          {data.missed_call_paid_leads > 0 && (
            <div className="mt-4 rounded-xl border border-emerald-200 bg-emerald-50 p-4">
              <p className="text-sm font-medium text-emerald-800">
                💰 Without AI text-back, these{' '}
                <strong>{data.missed_call_paid_leads}</strong> bookings would
                have been lost. At ~$200 avg booking value, that's{' '}
                <strong>
                  ${(data.missed_call_paid_leads * 200).toLocaleString()}
                </strong>{' '}
                in recovered revenue.
              </p>
            </div>
          )}
        </>
      )}
    </div>
  );
}
