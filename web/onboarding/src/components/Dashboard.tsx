import { useEffect, useState, useCallback } from 'react';
import {
  getPortalOverview,
  listConversations,
  listDeposits,
  clearAllPatientData,
  clearPatientDataByPhone,
  type PortalDashboardOverview,
} from '../api/client';
import type { ConversationListItem } from '../types/conversation';
import type { DepositListItem } from '../types/deposit';

interface DashboardProps {
  orgId: string;
}

function formatCount(value?: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return '--';
  return value.toLocaleString('en-US');
}

function formatPercent(value?: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return '--';
  return `${value.toFixed(1)}%`;
}

function formatCents(value?: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return '--';
  const dollars = value / 100;
  return `$${dollars.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
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

function formatStatusLabel(status: string): string {
  if (!status) return '-';
  return status.replace(/_/g, ' ');
}

function getConversationStatusClass(status: string): string {
  return status === 'active' ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800';
}

function getDepositStatusClass(status: string): string {
  switch (status.toLowerCase()) {
    case 'succeeded':
    case 'completed':
      return 'bg-green-100 text-green-800';
    case 'pending':
    case 'deposit_pending':
      return 'bg-yellow-100 text-yellow-800';
    case 'failed':
    case 'refunded':
      return 'bg-red-100 text-red-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}

export function Dashboard({ orgId }: DashboardProps) {
  const [stats, setStats] = useState<PortalDashboardOverview | null>(null);
  const [statsError, setStatsError] = useState<string | null>(null);
  const [statsLoading, setStatsLoading] = useState(true);
  const [conversations, setConversations] = useState<ConversationListItem[]>([]);
  const [conversationsLoading, setConversationsLoading] = useState(true);
  const [conversationsError, setConversationsError] = useState<string | null>(null);
  const [deposits, setDeposits] = useState<DepositListItem[]>([]);
  const [depositsLoading, setDepositsLoading] = useState(true);
  const [depositsError, setDepositsError] = useState<string | null>(null);
  const recentLimit = 5;

  useEffect(() => {
    let isActive = true;
    setStatsLoading(true);
    setStatsError(null);
    getPortalOverview(orgId)
      .then(data => {
        if (!isActive) return;
        setStats(data);
        setStatsError(null);
      })
      .catch(err => {
        if (!isActive) return;
        setStats(null);
        setStatsError(err instanceof Error ? err.message : 'Failed to load dashboard');
      })
      .finally(() => {
        if (!isActive) return;
        setStatsLoading(false);
      });
    return () => {
      isActive = false;
    };
  }, [orgId]);

  useEffect(() => {
    let isActive = true;
    setConversationsLoading(true);
    setConversationsError(null);
    setConversations([]);
    listConversations(
      orgId,
      {
        page: 1,
        pageSize: recentLimit,
      },
      'portal'
    )
      .then(data => {
        if (!isActive) return;
        setConversations(data.conversations);
      })
      .catch(err => {
        if (!isActive) return;
        setConversationsError(err instanceof Error ? err.message : 'Failed to load conversations');
        setConversations([]);
      })
      .finally(() => {
        if (!isActive) return;
        setConversationsLoading(false);
      });
    return () => {
      isActive = false;
    };
  }, [orgId, recentLimit]);

  useEffect(() => {
    let isActive = true;
    setDepositsLoading(true);
    setDepositsError(null);
    setDeposits([]);
    listDeposits(
      orgId,
      {
        page: 1,
        pageSize: recentLimit,
      },
      'portal'
    )
      .then(data => {
        if (!isActive) return;
        setDeposits(data.deposits);
      })
      .catch(err => {
        if (!isActive) return;
        setDepositsError(err instanceof Error ? err.message : 'Failed to load deposits');
        setDeposits([]);
      })
      .finally(() => {
        if (!isActive) return;
        setDepositsLoading(false);
      });
    return () => {
      isActive = false;
    };
  }, [orgId, recentLimit]);

  if (statsLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-gray-600">Loading dashboard...</span>
      </div>
    );
  }

  if (statsError) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-red-600">{statsError}</span>
      </div>
    );
  }

  if (!stats) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="text-sm text-gray-600">No dashboard data available.</span>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50 py-10">
      <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 space-y-8">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Dashboard</h1>
          <p className="mt-1 text-sm text-gray-500">Your latest activity and results at a glance.</p>
        </div>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-4">
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Conversations</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatCount(stats.conversations)}
            </p>
          </div>
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Deposits Collected</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatCount(stats.successful_deposits)}
            </p>
          </div>
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Total Collected</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatCents(stats.total_collected_cents)}
            </p>
          </div>
          <div className="bg-white shadow rounded-lg p-6">
            <p className="text-sm text-gray-500">Conversion</p>
            <p className="mt-2 text-2xl font-semibold text-gray-900">
              {formatPercent(stats.conversion_pct)}
            </p>
          </div>
        </div>
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <div className="bg-white shadow rounded-lg overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
              <h2 className="text-lg font-semibold text-gray-900">Recent Conversations</h2>
              <span className="text-xs text-gray-400">Latest {recentLimit}</span>
            </div>
            {conversationsLoading ? (
              <div className="px-6 py-6 text-sm text-gray-500">Loading conversations...</div>
            ) : conversationsError ? (
              <div className="px-6 py-6 text-sm text-red-600">{conversationsError}</div>
            ) : conversations.length === 0 ? (
              <div className="px-6 py-8 text-sm text-gray-500">No conversations yet.</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Phone
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Messages
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Status
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Last Activity
                      </th>
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {conversations.map((conv) => (
                      <tr key={conv.id}>
                        <td className="px-4 py-4 whitespace-nowrap text-sm text-gray-900 sm:px-6">
                          {formatPhone(conv.customer_phone)}
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap text-sm text-gray-500 sm:px-6">
                          {formatCount(conv.message_count)}
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap sm:px-6">
                          <span
                            className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getConversationStatusClass(
                              conv.status
                            )}`}
                          >
                            {formatStatusLabel(conv.status)}
                          </span>
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap text-sm text-gray-500 sm:px-6">
                          {timeAgo(conv.last_message_at || conv.started_at)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          <div className="bg-white shadow rounded-lg overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
              <h2 className="text-lg font-semibold text-gray-900">Recent Deposits</h2>
              <span className="text-xs text-gray-400">Latest {recentLimit}</span>
            </div>
            {depositsLoading ? (
              <div className="px-6 py-6 text-sm text-gray-500">Loading deposits...</div>
            ) : depositsError ? (
              <div className="px-6 py-6 text-sm text-red-600">{depositsError}</div>
            ) : deposits.length === 0 ? (
              <div className="px-6 py-8 text-sm text-gray-500">No deposits yet.</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Patient
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Service
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Amount
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Status
                      </th>
                      <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider sm:px-6">
                        Date
                      </th>
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {deposits.map((deposit) => (
                      <tr key={deposit.id}>
                        <td className="px-4 py-4 whitespace-nowrap sm:px-6">
                          <div className="flex flex-col">
                            <span className="text-sm font-medium text-gray-900">
                              {deposit.lead_name || 'Unknown'}
                            </span>
                            <span className="text-sm text-gray-500">
                              {formatPhone(deposit.lead_phone)}
                            </span>
                          </div>
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap text-sm text-gray-900 sm:px-6">
                          {deposit.service_interest || '-'}
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap text-sm font-semibold text-gray-900 sm:px-6">
                          {formatCents(deposit.amount_cents)}
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap sm:px-6">
                          <span
                            className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${getDepositStatusClass(
                              deposit.status
                            )}`}
                          >
                            {formatStatusLabel(deposit.status)}
                          </span>
                        </td>
                        <td className="px-4 py-4 whitespace-nowrap text-sm text-gray-500 sm:px-6">
                          {timeAgo(deposit.created_at)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
