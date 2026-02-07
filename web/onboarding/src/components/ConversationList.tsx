import { useEffect, useState, useCallback } from 'react';
import {
  listConversations,
  clearAllPatientData,
  clearPatientDataByPhone,
  type ApiScope,
} from '../api/client';
import type { ConversationListItem } from '../types/conversation';

interface ConversationListProps {
  orgId: string;
  onSelect: (conversationId: string) => void;
  scope?: ApiScope;
}

function formatPhone(phone: string): string {
  // Format as +1 (XXX) XXX-XXXX if possible
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

export function ConversationList({ orgId, onSelect, scope = 'admin' }: ConversationListProps) {
  const [conversations, setConversations] = useState<ConversationListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [phoneFilter, setPhoneFilter] = useState('');

  // Clear patient data state (admin only)
  const [showClearAllConfirm, setShowClearAllConfirm] = useState(false);
  const [clearingAll, setClearingAll] = useState(false);
  const [clearingPhone, setClearingPhone] = useState<string | null>(null);
  const [clearConfirmPhone, setClearConfirmPhone] = useState<string | null>(null);
  const [clearMessage, setClearMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const isAdmin = scope === 'admin';

  const loadConversations = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await listConversations(orgId, {
        page,
        pageSize: 20,
        phone: phoneFilter || undefined,
      }, scope);
      setConversations(data.conversations);
      setTotalPages(data.total_pages);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load conversations');
    } finally {
      setLoading(false);
    }
  }, [orgId, page, phoneFilter, scope]);

  useEffect(() => {
    loadConversations();
  }, [loadConversations]);

  // Auto-refresh every 30 seconds
  useEffect(() => {
    const interval = setInterval(loadConversations, 30000);
    return () => clearInterval(interval);
  }, [loadConversations]);

  // Clear all patient data handler
  const handleClearAll = useCallback(async () => {
    setClearingAll(true);
    setClearMessage(null);
    try {
      await clearAllPatientData(orgId);
      setClearMessage({ type: 'success', text: 'All patient data cleared successfully' });
      setShowClearAllConfirm(false);
      // Refresh the list
      await loadConversations();
    } catch (err) {
      setClearMessage({
        type: 'error',
        text: err instanceof Error ? err.message : 'Failed to clear patient data',
      });
    } finally {
      setClearingAll(false);
    }
  }, [orgId, loadConversations]);

  // Clear individual patient data handler
  const handleClearPhone = useCallback(async (phone: string) => {
    setClearingPhone(phone);
    setClearMessage(null);
    try {
      await clearPatientDataByPhone(orgId, phone);
      setClearMessage({ type: 'success', text: `Data for ${formatPhone(phone)} cleared successfully` });
      setClearConfirmPhone(null);
      // Refresh the list
      await loadConversations();
    } catch (err) {
      setClearMessage({
        type: 'error',
        text: err instanceof Error ? err.message : 'Failed to clear patient data',
      });
    } finally {
      setClearingPhone(null);
    }
  }, [orgId, loadConversations]);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setPage(1);
    loadConversations();
  };

  return (
    <div className="ui-page">
      <div className="ui-container">
        <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:justify-between sm:items-center">
          <h1 className="text-xl sm:text-2xl font-semibold tracking-tight text-slate-900">Conversations</h1>
          <div className="flex gap-2">
            {isAdmin && (
              <button
                onClick={() => setShowClearAllConfirm(true)}
                disabled={clearingAll || loading}
                className="ui-btn ui-btn-danger"
              >
                {clearingAll ? 'Clearing...' : 'Clear All Patient Data'}
              </button>
            )}
            <button
              onClick={loadConversations}
              disabled={loading}
              className="ui-btn ui-btn-primary w-full sm:w-auto"
            >
              {loading ? 'Loading...' : 'Refresh'}
            </button>
          </div>
        </div>

        {/* Clear All Confirmation Modal */}
        {showClearAllConfirm && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
            <div className="ui-card ui-card-solid max-w-md w-full p-6">
              <h3 className="text-lg font-semibold tracking-tight text-slate-900 mb-2">Clear all patient data?</h3>
              <p className="ui-muted mb-4">
                This will permanently delete all conversations, leads, deposits, and payment records.
                <strong className="block mt-2 text-slate-900">Clinic configuration and knowledge will be preserved.</strong>
              </p>
              <div className="flex gap-3 justify-end">
                <button
                  onClick={() => setShowClearAllConfirm(false)}
                  disabled={clearingAll}
                  className="ui-btn ui-btn-ghost"
                >
                  Cancel
                </button>
                <button
                  onClick={handleClearAll}
                  disabled={clearingAll}
                  className="ui-btn ui-btn-danger"
                >
                  {clearingAll ? 'Clearing...' : 'Yes, Clear All'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Clear Phone Confirmation Modal */}
        {clearConfirmPhone && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
            <div className="ui-card ui-card-solid max-w-md w-full p-6">
              <h3 className="text-lg font-semibold tracking-tight text-slate-900 mb-2">Clear patient data?</h3>
              <p className="ui-muted mb-4">
                This will permanently delete all data for <strong>{formatPhone(clearConfirmPhone)}</strong>:
                conversations, lead info, deposits, and payment records.
              </p>
              <div className="flex gap-3 justify-end">
                <button
                  onClick={() => setClearConfirmPhone(null)}
                  disabled={!!clearingPhone}
                  className="ui-btn ui-btn-ghost"
                >
                  Cancel
                </button>
                <button
                  onClick={() => handleClearPhone(clearConfirmPhone)}
                  disabled={!!clearingPhone}
                  className="ui-btn ui-btn-danger"
                >
                  {clearingPhone === clearConfirmPhone ? 'Clearing...' : 'Yes, Clear'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Success/Error Message */}
        {clearMessage && (
          <div
            className={`mb-4 p-4 rounded-md ${
              clearMessage.type === 'success'
                ? 'bg-emerald-50 border border-emerald-200 text-emerald-800'
                : 'bg-red-50 border border-red-200 text-red-800'
            }`}
          >
            {clearMessage.text}
            <button
              onClick={() => setClearMessage(null)}
              className="ml-2 text-sm font-semibold underline hover:no-underline"
            >
              Dismiss
            </button>
          </div>
        )}

        {/* Search */}
        <form onSubmit={handleSearch} className="mb-4 sm:mb-6">
          <div className="flex flex-col gap-2 sm:flex-row">
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
          </div>
        </form>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-xl text-red-800">
            {error}
          </div>
        )}

        {/* Conversations Table */}
        <div className="ui-card ui-card-solid overflow-hidden">
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
                  {isAdmin && (
                    <th className="ui-th">
                      Actions
                    </th>
                  )}
                </tr>
              </thead>
              <tbody>
                {conversations.length === 0 && !loading ? (
                  <tr className="ui-row">
                    <td colSpan={isAdmin ? 5 : 4} className="ui-td py-10 text-center text-slate-500">
                      No conversations found
                    </td>
                  </tr>
                ) : (
                  conversations.map((conv) => (
                    <tr
                      key={conv.id}
                      className="ui-row ui-row-hover"
                    >
                      <td
                        className="ui-td whitespace-nowrap cursor-pointer font-medium text-slate-900"
                        onClick={() => onSelect(conv.id)}
                      >
                        {formatPhone(conv.customer_phone)}
                      </td>
                      <td
                        className="ui-td whitespace-nowrap cursor-pointer"
                        onClick={() => onSelect(conv.id)}
                      >
                        <div className="text-sm text-slate-600">
                          <span>{conv.message_count}</span>
                          <span className="hidden sm:inline text-xs text-slate-400 ml-1">
                            ({conv.customer_message_count} user / {conv.ai_message_count} AI)
                          </span>
                        </div>
                      </td>
                      <td
                        className="ui-td whitespace-nowrap cursor-pointer"
                        onClick={() => onSelect(conv.id)}
                      >
                        <span
                          className={`ui-badge ${conv.status === 'active' ? 'ui-badge-success' : 'ui-badge-muted'}`}
                        >
                          {conv.status}
                        </span>
                      </td>
                      <td
                        className="ui-td whitespace-nowrap cursor-pointer text-slate-500"
                        onClick={() => onSelect(conv.id)}
                      >
                        {conv.last_message_at ? timeAgo(conv.last_message_at) : timeAgo(conv.started_at)}
                      </td>
                      {isAdmin && (
                        <td className="ui-td whitespace-nowrap">
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              setClearConfirmPhone(conv.customer_phone);
                            }}
                            disabled={clearingPhone === conv.customer_phone}
                            className="ui-btn ui-btn-ghost px-3 py-1.5 text-xs rounded-lg"
                          >
                            {clearingPhone === conv.customer_phone ? 'Clearing...' : 'Clear'}
                          </button>
                        </td>
                      )}
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
