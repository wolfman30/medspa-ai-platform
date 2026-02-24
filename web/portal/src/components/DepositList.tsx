import { useEffect, useState, useCallback } from 'react';
import { listDeposits, getDepositStats, type ApiScope } from '../api/client';
import type { DepositListItem, DepositStatsResponse } from '../types/deposit';

interface DepositListProps {
  orgId: string;
  onSelect: (depositId: string) => void;
  scope?: ApiScope;
}

function formatPhone(phone: string): string {
  if (!phone) return '-';
  const digits = phone.replace(/\D/g, '');
  if (digits.length === 11 && digits.startsWith('1')) {
    return `+1 (${digits.slice(1, 4)}) ${digits.slice(4, 7)}-${digits.slice(7)}`;
  }
  if (digits.length === 10) {
    return `(${digits.slice(0, 3)}) ${digits.slice(3, 6)}-${digits.slice(6)}`;
  }
  return phone;
}

function formatCents(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

function timeAgo(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000);

  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  if (seconds < 604800) return `${Math.floor(seconds / 86400)}d ago`;
  return date.toLocaleDateString();
}

function getStatusColor(status: string): string {
  switch (status.toLowerCase()) {
    case 'succeeded':
    case 'completed':
      return 'ui-badge-success';
    case 'pending':
      return 'ui-badge-warning';
    case 'failed':
    case 'refunded':
      return 'ui-badge-danger';
    default:
      return 'ui-badge-muted';
  }
}

export function DepositList({ orgId, onSelect, scope = 'admin' }: DepositListProps) {
  const [deposits, setDeposits] = useState<DepositListItem[]>([]);
  const [stats, setStats] = useState<DepositStatsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [statusFilter, setStatusFilter] = useState('');
  const [phoneFilter, setPhoneFilter] = useState('');

  const loadDeposits = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [depositsData, statsData] = await Promise.all([
        listDeposits(orgId, {
          page,
          pageSize: 20,
          status: statusFilter || undefined,
          phone: phoneFilter || undefined,
        }, scope),
        getDepositStats(orgId, scope),
      ]);
      setDeposits(depositsData.deposits);
      setTotalPages(depositsData.total_pages);
      setStats(statsData);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load deposits');
    } finally {
      setLoading(false);
    }
  }, [orgId, page, statusFilter, phoneFilter, scope]);

  useEffect(() => {
    loadDeposits();
  }, [loadDeposits]);

  // Auto-refresh every 30 seconds
  useEffect(() => {
    const interval = setInterval(loadDeposits, 30000);
    return () => clearInterval(interval);
  }, [loadDeposits]);

  const handleStatusChange = (newStatus: string) => {
    setStatusFilter(newStatus);
    setPage(1);
  };

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setPage(1);
    loadDeposits();
  };

  return (
    <div className="ui-page">
      <div className="ui-container">
        <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:justify-between sm:items-center">
          <h1 className="text-xl sm:text-2xl font-semibold tracking-tight text-slate-900">Deposits</h1>
          <button
            onClick={loadDeposits}
            disabled={loading}
            className="ui-btn ui-btn-primary w-full sm:w-auto"
          >
            {loading ? 'Loading...' : 'Refresh'}
          </button>
        </div>

        {/* Stats Cards */}
        {stats && (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3 sm:gap-4 mb-4 sm:mb-6">
            <div className="ui-card ui-card-solid p-4">
              <p className="ui-kicker ui-gradient-text">Total Collected</p>
              <p className="mt-2 text-lg sm:text-2xl font-semibold tracking-tight text-emerald-700">
                {formatCents(stats.total_amount_cents)}
              </p>
              <p className="ui-help">{stats.total_deposits} deposits</p>
            </div>
            <div className="ui-card ui-card-solid p-4">
              <p className="ui-kicker ui-gradient-text">This Week</p>
              <p className="mt-2 text-lg sm:text-2xl font-semibold tracking-tight text-violet-700">
                {formatCents(stats.week_amount_cents)}
              </p>
              <p className="ui-help">{stats.week_count} deposits</p>
            </div>
            <div className="ui-card ui-card-solid p-4">
              <p className="ui-kicker ui-gradient-text">Today</p>
              <p className="mt-2 text-lg sm:text-2xl font-semibold tracking-tight text-indigo-700">
                {formatCents(stats.today_amount_cents)}
              </p>
              <p className="ui-help">{stats.today_count} deposits</p>
            </div>
            <div className="ui-card ui-card-solid p-4">
              <p className="ui-kicker ui-gradient-text">Average</p>
              <p className="mt-2 text-lg sm:text-2xl font-semibold tracking-tight text-slate-900">
                {formatCents(stats.average_amount_cents)}
              </p>
              <p className="ui-help">per deposit</p>
            </div>
          </div>
        )}

        {/* Filters */}
        <div className="mb-4 sm:mb-6 flex flex-col gap-3 sm:gap-4 sm:flex-row sm:items-center">
          <select
            value={statusFilter}
            onChange={(e) => handleStatusChange(e.target.value)}
            className="ui-select sm:w-56"
          >
            <option value="">All Statuses</option>
            <option value="succeeded">Succeeded</option>
            <option value="pending">Pending</option>
            <option value="failed">Failed</option>
            <option value="refunded">Refunded</option>
          </select>
          <form onSubmit={handleSearch} className="flex flex-col gap-2 sm:flex-row sm:flex-1">
            <input
              type="text"
              placeholder="Filter by phone..."
              value={phoneFilter}
              onChange={(e) => setPhoneFilter(e.target.value)}
              className="ui-input flex-1"
            />
            <button
              type="submit"
              className="ui-btn ui-btn-ghost"
            >
              Search
            </button>
          </form>
        </div>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-xl text-red-800">
            {error}
          </div>
        )}

        {/* Deposits Table */}
        <div className="ui-card ui-card-solid overflow-hidden">
          <div className="overflow-x-auto">
            <table className="ui-table">
              <thead>
                <tr>
                  <th className="ui-th">
                    <span className="ui-gradient-text-subtle">Patient</span>
                  </th>
                  <th className="ui-th">
                    <span className="ui-gradient-text-subtle">Service</span>
                  </th>
                  <th className="ui-th">
                    <span className="ui-gradient-text-subtle">Amount</span>
                  </th>
                  <th className="ui-th">
                    <span className="ui-gradient-text-subtle">Status</span>
                  </th>
                  <th className="ui-th">
                    <span className="ui-gradient-text-subtle">Date</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {deposits.length === 0 && !loading ? (
                  <tr className="ui-row">
                    <td colSpan={5} className="ui-td py-10 text-center text-slate-500">
                      No deposits found
                    </td>
                  </tr>
                ) : (
                  deposits.map((deposit) => (
                    <tr
                      key={deposit.id}
                      onClick={() => onSelect(deposit.id)}
                      className="ui-row ui-row-hover cursor-pointer"
                    >
                      <td className="ui-td whitespace-nowrap">
                        <div className="flex flex-col">
                          <span className="text-sm font-semibold text-slate-900">
                            {deposit.lead_name || 'Unknown'}
                          </span>
                          <span className="text-sm text-slate-500">
                            {formatPhone(deposit.lead_phone)}
                          </span>
                        </div>
                      </td>
                      <td className="ui-td whitespace-nowrap">
                        <span className="text-sm font-medium text-slate-900">
                          {deposit.service_interest || '-'}
                        </span>
                        {deposit.patient_type && (
                          <span className="hidden sm:inline ml-2 text-xs text-slate-500">
                            ({deposit.patient_type})
                          </span>
                        )}
                      </td>
                      <td className="ui-td whitespace-nowrap">
                        <span className="text-sm font-semibold text-slate-900">
                          {formatCents(deposit.amount_cents)}
                        </span>
                      </td>
                      <td className="ui-td whitespace-nowrap">
                        <span
                          className={`ui-badge ${getStatusColor(deposit.status)}`}
                        >
                          {deposit.status}
                        </span>
                      </td>
                      <td className="ui-td whitespace-nowrap text-slate-500">
                        {timeAgo(deposit.created_at)}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex justify-between items-center gap-2">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
              className="ui-btn ui-btn-ghost"
            >
              Prev
            </button>
            <span className="text-xs sm:text-sm text-slate-600">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page === totalPages}
              className="ui-btn ui-btn-ghost"
            >
              Next
            </button>
          </div>
        )}

        <p className="mt-6 text-xs text-slate-400 text-center">
          Auto-refreshes every 30 seconds
        </p>
      </div>
    </div>
  );
}
