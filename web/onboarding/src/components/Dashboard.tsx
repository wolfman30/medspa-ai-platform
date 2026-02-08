import { useEffect, useState } from 'react';
import {
  getPortalOverview,
  getClinicConfig,
  getSquareConnectUrl,
  getSquareStatus,
  listConversations,
  listDeposits,
  type PortalDashboardOverview,
  type SquareStatus,
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

function formatDateTimeET(value?: string | null): string {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '--';
  return date.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
    timeZone: 'America/New_York',
    timeZoneName: 'short',
  });
}

function getConversationStatusClass(status: string): string {
  return status === 'active' ? 'ui-badge-success' : 'ui-badge-muted';
}

function getDepositStatusClass(status: string): string {
  switch (status.toLowerCase()) {
    case 'succeeded':
    case 'completed':
      return 'ui-badge-success';
    case 'pending':
    case 'deposit_pending':
      return 'ui-badge-warning';
    case 'failed':
    case 'refunded':
      return 'ui-badge-danger';
    default:
      return 'ui-badge-muted';
  }
}

export function Dashboard({ orgId }: DashboardProps) {
  const [stats, setStats] = useState<PortalDashboardOverview | null>(null);
  const [statsError, setStatsError] = useState<string | null>(null);
  const [statsLoading, setStatsLoading] = useState(true);
  const [bookingPlatform, setBookingPlatform] = useState<string | null>(null);
  const [bookingPlatformLoading, setBookingPlatformLoading] = useState(true);
  const [conversations, setConversations] = useState<ConversationListItem[]>([]);
  const [conversationsLoading, setConversationsLoading] = useState(true);
  const [conversationsError, setConversationsError] = useState<string | null>(null);
  const [deposits, setDeposits] = useState<DepositListItem[]>([]);
  const [depositsLoading, setDepositsLoading] = useState(true);
  const [depositsError, setDepositsError] = useState<string | null>(null);
  const [squareStatus, setSquareStatus] = useState<SquareStatus | null>(null);
  const [squareStatusLoading, setSquareStatusLoading] = useState(true);
  const [squareStatusError, setSquareStatusError] = useState<string | null>(null);
  const [squareConnectUrl, setSquareConnectUrl] = useState<string>('');
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
    setBookingPlatformLoading(true);
    getClinicConfig(orgId)
      .then((config) => {
        if (!isActive) return;
        const platform = (config.booking_platform || 'square').toLowerCase();
        setBookingPlatform(platform);
      })
      .catch(() => {
        if (!isActive) return;
        setBookingPlatform(null);
      })
      .finally(() => {
        if (!isActive) return;
        setBookingPlatformLoading(false);
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

  useEffect(() => {
    let isActive = true;
    setSquareStatusLoading(true);
    setSquareStatusError(null);
    getSquareStatus(orgId, 'portal')
      .then((data) => {
        if (!isActive) return;
        setSquareStatus(data);
      })
      .catch((err) => {
        if (!isActive) return;
        setSquareStatus(null);
        setSquareStatusError(err instanceof Error ? err.message : 'Failed to load Square status');
      })
      .finally(() => {
        if (!isActive) return;
        setSquareStatusLoading(false);
      });
    return () => {
      isActive = false;
    };
  }, [orgId]);

  useEffect(() => {
    let isActive = true;
    getSquareConnectUrl(orgId)
      .then((url) => {
        if (isActive) setSquareConnectUrl(url);
      })
      .catch(() => {
        if (isActive) setSquareConnectUrl('');
      });
    return () => {
      isActive = false;
    };
  }, [orgId]);

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

  const hasRefreshFailure = Boolean(
    squareStatus?.last_refresh_failure_at || squareStatus?.last_refresh_error
  );
  const tokenExpired = squareStatus?.token_expired;
  const refreshTokenMissing =
    squareStatus?.connected && squareStatus?.refresh_token_present === false;
  const squareBadgeLabel = squareStatusLoading
    ? 'Checking...'
    : squareStatus?.connected
      ? 'Connected'
      : 'Not connected';
  const squareBadgeVariant = squareStatusLoading
    ? 'ui-badge-muted'
    : squareStatus?.connected
      ? 'ui-badge-success'
      : 'ui-badge-danger';
  const showSquareConnectionCard = !bookingPlatformLoading && bookingPlatform !== 'moxie';

  return (
    <div className="ui-page">
      <div className="ui-container ui-stack">
        <div>
          <h1 className="ui-h1">Dashboard</h1>
          <p className="ui-muted mt-1">Your latest activity and results at a glance.</p>
        </div>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-4">
          <div className="ui-card ui-card-solid p-6">
            <p className="ui-kicker">Conversations</p>
            <p className="mt-2 text-2xl font-semibold tracking-tight text-slate-900">
              {formatCount(stats.conversations)}
            </p>
          </div>
          <div className="ui-card ui-card-solid p-6">
            <p className="ui-kicker">Deposits Collected</p>
            <p className="mt-2 text-2xl font-semibold tracking-tight text-slate-900">
              {formatCount(stats.successful_deposits)}
            </p>
          </div>
          <div className="ui-card ui-card-solid p-6">
            <p className="ui-kicker">Total Collected</p>
            <p className="mt-2 text-2xl font-semibold tracking-tight text-slate-900">
              {formatCents(stats.total_collected_cents)}
            </p>
          </div>
          <div className="ui-card ui-card-solid p-6">
            <p className="ui-kicker">Conversion</p>
            <p className="mt-2 text-2xl font-semibold tracking-tight text-slate-900">
              {formatPercent(stats.conversion_pct)}
            </p>
          </div>
        </div>
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <div className="ui-card ui-card-solid overflow-hidden">
            <div className="ui-card-header">
              <h2 className="ui-h2">Recent Conversations</h2>
              <span className="ui-muted">Latest {recentLimit}</span>
            </div>
            {conversationsLoading ? (
              <div className="ui-card-body ui-muted">Loading conversations...</div>
            ) : conversationsError ? (
              <div className="ui-card-body text-sm font-medium text-red-700">{conversationsError}</div>
            ) : conversations.length === 0 ? (
              <div className="ui-card-body ui-muted">No conversations yet.</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="ui-table">
                  <thead>
                    <tr>
                      <th className="ui-th">
                        Phone
                      </th>
                      <th className="ui-th">
                        Messages
                      </th>
                      <th className="ui-th">
                        Status
                      </th>
                      <th className="ui-th">
                        Last Activity
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {conversations.map((conv) => (
                      <tr key={conv.id} className="ui-row ui-row-hover">
                        <td className="ui-td whitespace-nowrap font-medium text-slate-900">
                          {formatPhone(conv.customer_phone)}
                        </td>
                        <td className="ui-td whitespace-nowrap text-slate-500">
                          {formatCount(conv.message_count)}
                        </td>
                        <td className="ui-td whitespace-nowrap">
                          <span
                            className={`ui-badge ${getConversationStatusClass(conv.status)}`}
                          >
                            {formatStatusLabel(conv.status)}
                          </span>
                        </td>
                        <td className="ui-td whitespace-nowrap text-slate-500">
                          {timeAgo(conv.last_message_at || conv.started_at)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          <div className="ui-card ui-card-solid overflow-hidden">
            <div className="ui-card-header">
              <h2 className="ui-h2">Recent Deposits</h2>
              <span className="ui-muted">Latest {recentLimit}</span>
            </div>
            {depositsLoading ? (
              <div className="ui-card-body ui-muted">Loading deposits...</div>
            ) : depositsError ? (
              <div className="ui-card-body text-sm font-medium text-red-700">{depositsError}</div>
            ) : deposits.length === 0 ? (
              <div className="ui-card-body ui-muted">No deposits yet.</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="ui-table">
                  <thead>
                    <tr>
                      <th className="ui-th">
                        Patient
                      </th>
                      <th className="ui-th">
                        Service
                      </th>
                      <th className="ui-th">
                        Amount
                      </th>
                      <th className="ui-th">
                        Status
                      </th>
                      <th className="ui-th">
                        Date
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {deposits.map((deposit) => (
                      <tr key={deposit.id} className="ui-row ui-row-hover">
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
                        <td className="ui-td whitespace-nowrap font-medium text-slate-900">
                          {deposit.service_interest || '-'}
                        </td>
                        <td className="ui-td whitespace-nowrap font-semibold text-slate-900">
                          {formatCents(deposit.amount_cents)}
                        </td>
                        <td className="ui-td whitespace-nowrap">
                          <span
                            className={`ui-badge ${getDepositStatusClass(deposit.status)}`}
                          >
                            {formatStatusLabel(deposit.status)}
                          </span>
                        </td>
                        <td className="ui-td whitespace-nowrap text-slate-500">
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
        {showSquareConnectionCard ? (
          <div className="ui-card ui-card-solid">
            <div className="ui-card-header">
              <div>
                <h2 className="ui-h2">Square Connection</h2>
                <p className="ui-muted">
                  Optional. Only needed if you collect deposits via Square payment links.
                </p>
              </div>
              <span
                className={`ui-badge ${squareBadgeVariant}`}
              >
                {squareBadgeLabel}
              </span>
            </div>
            <div className="ui-card-body">
            {squareStatusLoading ? (
              <div className="ui-muted">Loading Square status...</div>
            ) : squareStatusError ? (
              <div className="text-sm font-medium text-red-700">{squareStatusError}</div>
            ) : squareStatus ? (
              <div className="space-y-4">
                {!squareStatus.connected ? (
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <p className="ui-muted">
                      Square is not connected. Connect to enable real checkout links.
                    </p>
                    {squareConnectUrl ? (
                      <a
                        href={squareConnectUrl}
                        className="ui-btn ui-btn-dark"
                      >
                        Connect Square
                      </a>
                    ) : null}
                  </div>
                ) : (
                  <>
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <p className="ui-kicker">Merchant ID</p>
                        <p className="mt-1 text-sm font-semibold text-slate-900">
                          {squareStatus.merchant_id || '--'}
                        </p>
                      </div>
                      <div>
                        <p className="ui-kicker">Location ID</p>
                        <p className="mt-1 text-sm font-semibold text-slate-900">
                          {squareStatus.location_id || '--'}
                        </p>
                      </div>
                      <div>
                        <p className="ui-kicker">Token Expires</p>
                        <p className="mt-1 text-sm font-semibold text-slate-900">
                          {formatDateTimeET(squareStatus.token_expires_at)}
                        </p>
                      </div>
                      <div>
                        <p className="ui-kicker">Last Refresh</p>
                        <p className="mt-1 text-sm font-semibold text-slate-900">
                          {formatDateTimeET(squareStatus.last_refresh_attempt_at)}
                        </p>
                      </div>
                    </div>

                    {hasRefreshFailure ? (
                      <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800">
                        <div className="font-semibold">Square token refresh failed</div>
                        <div className="mt-1 text-xs text-red-700">
                          Last failure: {formatDateTimeET(squareStatus.last_refresh_failure_at)}
                        </div>
                        {squareStatus.last_refresh_error ? (
                          <div className="mt-1 text-xs text-red-700">Error: {squareStatus.last_refresh_error}</div>
                        ) : null}
                      </div>
                    ) : null}

                    {tokenExpired || refreshTokenMissing ? (
                      <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
                        {tokenExpired ? (
                          <div>Square access token is expired. Reconnect to restore payments.</div>
                        ) : null}
                        {refreshTokenMissing ? (
                          <div className="mt-1">
                            Refresh token missing. Reconnect Square to enable automatic refresh.
                          </div>
                        ) : null}
                      </div>
                    ) : null}
                  </>
                )}
              </div>
            ) : (
              <div className="ui-muted">No Square status available.</div>
            )}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
