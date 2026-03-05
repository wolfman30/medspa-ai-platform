import { useEffect, useState, useCallback } from 'react';
import { listLeads, type Lead, type ApiScope } from '../api/client';

interface LeadsListProps {
  orgId: string;
  scope?: ApiScope;
}

const STATUS_LABELS: Record<string, { label: string; color: string }> = {
  new: { label: 'New', color: 'bg-blue-100 text-blue-800' },
  contacted: { label: 'Contacted', color: 'bg-yellow-100 text-yellow-800' },
  qualified: { label: 'Qualified', color: 'bg-purple-100 text-purple-800' },
  booked: { label: 'Booked', color: 'bg-green-100 text-green-800' },
  paid: { label: 'Paid', color: 'bg-emerald-100 text-emerald-800' },
  lost: { label: 'Lost', color: 'bg-red-100 text-red-800' },
  no_show: { label: 'No Show', color: 'bg-orange-100 text-orange-800' },
};

function formatPhone(phone: string): string {
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
  return `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
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

function StatusBadge({ status }: { status: string }) {
  const info = STATUS_LABELS[status] || { label: status, color: 'bg-gray-100 text-gray-800' };
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${info.color}`}>
      {info.label}
    </span>
  );
}

export function LeadsList({ orgId, scope = 'admin' }: LeadsListProps) {
  const [leads, setLeads] = useState<Lead[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [total, setTotal] = useState(0);
  const [search, setSearch] = useState('');
  const [searchInput, setSearchInput] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [sortBy, setSortBy] = useState('created_at');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');

  const loadLeads = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await listLeads(orgId, {
        page,
        page_size: 20,
        status: statusFilter || undefined,
        search: search || undefined,
        sort_by: sortBy,
        sort_order: sortOrder,
      }, scope);
      setLeads(res.leads || []);
      setTotal(res.total);
      setTotalPages(res.total_pages);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load leads');
    } finally {
      setLoading(false);
    }
  }, [orgId, page, statusFilter, search, sortBy, sortOrder, scope]);

  useEffect(() => { loadLeads(); }, [loadLeads]);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setPage(1);
    setSearch(searchInput);
  };

  const handleSort = (column: string) => {
    if (sortBy === column) {
      setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc');
    } else {
      setSortBy(column);
      setSortOrder('desc');
    }
    setPage(1);
  };

  const SortIcon = ({ column }: { column: string }) => {
    if (sortBy !== column) return <span className="text-slate-300 ml-1">↕</span>;
    return <span className="text-violet-600 ml-1">{sortOrder === 'asc' ? '↑' : '↓'}</span>;
  };

  // Summary stats
  const bookedCount = leads.filter(l => l.status === 'booked' || l.status === 'paid').length;
  const totalRevenue = leads.reduce((sum, l) => sum + l.payment_total_cents, 0);

  return (
    <div className="ui-container py-6">
      <div className="flex flex-col gap-4">
        {/* Header */}
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">Leads</h2>
            <p className="text-sm text-slate-500">{total} total leads</p>
          </div>
          {/* Quick stats */}
          <div className="flex gap-4 text-sm">
            <div className="text-center">
              <div className="font-semibold text-green-700">{bookedCount}</div>
              <div className="text-slate-500 text-xs">Booked/Paid</div>
            </div>
            <div className="text-center">
              <div className="font-semibold text-emerald-700">{formatCents(totalRevenue)}</div>
              <div className="text-slate-500 text-xs">Revenue (page)</div>
            </div>
          </div>
        </div>

        {/* Filters */}
        <div className="flex flex-col sm:flex-row gap-2">
          <form onSubmit={handleSearch} className="flex gap-2 flex-1">
            <input
              type="text"
              placeholder="Search name, phone, email..."
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              className="ui-input flex-1"
            />
            <button type="submit" className="ui-btn ui-btn-primary">Search</button>
          </form>
          <select
            value={statusFilter}
            onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}
            className="ui-select w-full sm:w-40"
          >
            <option value="">All Statuses</option>
            {Object.entries(STATUS_LABELS).map(([key, { label }]) => (
              <option key={key} value={key}>{label}</option>
            ))}
          </select>
        </div>

        {/* Error */}
        {error && (
          <div className="rounded-xl border border-red-200 bg-red-50 p-4">
            <p className="text-sm text-red-800">{error}</p>
            <button onClick={loadLeads} className="mt-2 text-sm font-medium text-red-700 underline">Retry</button>
          </div>
        )}

        {/* Table */}
        {loading ? (
          <div className="flex justify-center py-12">
            <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
          </div>
        ) : leads.length === 0 ? (
          <div className="text-center py-12 text-slate-500">
            {search || statusFilter ? 'No leads match your filters.' : 'No leads yet. They\u2019ll appear here when patients text in.'}
          </div>
        ) : (
          <>
            {/* Desktop table */}
            <div className="hidden md:block overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-slate-500">
                    <th className="pb-2 pr-4 font-medium cursor-pointer" onClick={() => handleSort('name')}>
                      Name <SortIcon column="name" />
                    </th>
                    <th className="pb-2 pr-4 font-medium">Phone</th>
                    <th className="pb-2 pr-4 font-medium cursor-pointer" onClick={() => handleSort('status')}>
                      Status <SortIcon column="status" />
                    </th>
                    <th className="pb-2 pr-4 font-medium">Services</th>
                    <th className="pb-2 pr-4 font-medium text-right">Revenue</th>
                    <th className="pb-2 pr-4 font-medium text-right">Bookings</th>
                    <th className="pb-2 font-medium cursor-pointer" onClick={() => handleSort('created_at')}>
                      Created <SortIcon column="created_at" />
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {leads.map((lead) => (
                    <tr
                      key={lead.id}
                      className="border-b border-slate-100 hover:bg-slate-50 transition-colors"
                    >
                      <td className="py-3 pr-4">
                        <div className="font-medium text-slate-900">{lead.name || '—'}</div>
                        {lead.email && <div className="text-xs text-slate-400 truncate max-w-[180px]">{lead.email}</div>}
                      </td>
                      <td className="py-3 pr-4 text-slate-600 whitespace-nowrap">{formatPhone(lead.phone)}</td>
                      <td className="py-3 pr-4"><StatusBadge status={lead.status} /></td>
                      <td className="py-3 pr-4">
                        <div className="flex flex-wrap gap-1">
                          {(lead.interested_services || []).slice(0, 2).map((svc, i) => (
                            <span key={i} className="inline-block rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600">
                              {svc}
                            </span>
                          ))}
                          {(lead.interested_services || []).length > 2 && (
                            <span className="text-xs text-slate-400">+{(lead.interested_services || []).length - 2}</span>
                          )}
                        </div>
                      </td>
                      <td className="py-3 pr-4 text-right tabular-nums">
                        {lead.payment_total_cents > 0 ? (
                          <span className="text-emerald-700 font-medium">{formatCents(lead.payment_total_cents)}</span>
                        ) : (
                          <span className="text-slate-300">—</span>
                        )}
                      </td>
                      <td className="py-3 pr-4 text-right tabular-nums">
                        {lead.booking_count > 0 ? (
                          <span className="text-green-700 font-medium">{lead.booking_count}</span>
                        ) : (
                          <span className="text-slate-300">0</span>
                        )}
                      </td>
                      <td className="py-3 text-slate-500 whitespace-nowrap">{timeAgo(lead.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* Mobile cards */}
            <div className="md:hidden flex flex-col gap-3">
              {leads.map((lead) => (
                <div key={lead.id} className="ui-card p-4">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="font-medium text-slate-900 truncate">{lead.name || formatPhone(lead.phone)}</div>
                      {lead.name && <div className="text-xs text-slate-500">{formatPhone(lead.phone)}</div>}
                    </div>
                    <StatusBadge status={lead.status} />
                  </div>
                  {(lead.interested_services || []).length > 0 && (
                    <div className="mt-2 flex flex-wrap gap-1">
                      {lead.interested_services!.map((svc, i) => (
                        <span key={i} className="inline-block rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600">
                          {svc}
                        </span>
                      ))}
                    </div>
                  )}
                  <div className="mt-2 flex items-center gap-4 text-xs text-slate-500">
                    {lead.payment_total_cents > 0 && (
                      <span className="text-emerald-700 font-medium">{formatCents(lead.payment_total_cents)}</span>
                    )}
                    {lead.booking_count > 0 && (
                      <span>{lead.booking_count} booking{lead.booking_count > 1 ? 's' : ''}</span>
                    )}
                    <span>{timeAgo(lead.created_at)}</span>
                  </div>
                </div>
              ))}
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between pt-2">
                <button
                  onClick={() => setPage(p => Math.max(1, p - 1))}
                  disabled={page <= 1}
                  className="ui-btn ui-btn-ghost disabled:opacity-30"
                >
                  ← Previous
                </button>
                <span className="text-sm text-slate-500">
                  Page {page} of {totalPages}
                </span>
                <button
                  onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                  disabled={page >= totalPages}
                  className="ui-btn ui-btn-ghost disabled:opacity-30"
                >
                  Next →
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
