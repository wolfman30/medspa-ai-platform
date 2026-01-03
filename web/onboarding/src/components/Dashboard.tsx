import { useEffect, useState } from 'react';
import { getDashboardStats, type DashboardStats } from '../api/client';

interface DashboardProps {
  orgId: string;
}

function formatCurrency(cents?: number | null) {
  if (cents === null || cents === undefined || Number.isNaN(cents)) return '--';
  return `$${(cents / 100).toFixed(2)}`;
}

function formatCount(value?: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return '--';
  return value.toLocaleString('en-US');
}

function formatPercent(value?: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return '--';
  const normalized = value <= 1 ? value * 100 : value;
  return `${normalized.toFixed(1)}%`;
}

export function Dashboard({ orgId }: DashboardProps) {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [statsOrgId, setStatsOrgId] = useState<string | null>(null);
  const [error, setError] = useState<{ orgId: string; message: string } | null>(null);

  useEffect(() => {
    let isActive = true;
    getDashboardStats(orgId)
      .then(data => {
        if (!isActive) return;
        setStats(data);
        setStatsOrgId(orgId);
        setError(null);
      })
      .catch(err => {
        if (!isActive) return;
        setError({
          orgId,
          message: err instanceof Error ? err.message : 'Failed to load dashboard',
        });
        setStats(null);
        setStatsOrgId(orgId);
      });
    return () => {
      isActive = false;
    };
  }, [orgId]);

  const activeStats = statsOrgId === orgId ? stats : null;
  const activeError = error?.orgId === orgId ? error.message : null;
  const loading = !activeStats && !activeError;

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-gray-600">Loading ROI data...</span>
      </div>
    );
  }

  if (activeError) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-red-600">{activeError}</span>
      </div>
    );
  }

  if (!activeStats) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-gray-600">No dashboard data available.</span>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50 py-10">
      <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900">Performance Dashboard</h1>
        </div>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Total Revenue Captured</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatCurrency(activeStats.payments?.total_collected_cents)}
            </p>
          </div>
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Conversations handled (this week)</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatCount(activeStats.conversations?.this_week)}
            </p>
          </div>
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Conversion rate</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatPercent(activeStats.leads?.conversion_rate)}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
