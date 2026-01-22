import { useEffect, useState, useCallback } from 'react';
import { listConversations, type ApiScope } from '../api/client';
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

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setPage(1);
    loadConversations();
  };

  return (
    <div className="min-h-screen bg-gray-50 py-6 sm:py-10">
      <div className="max-w-6xl mx-auto px-3 sm:px-6 lg:px-8">
        <div className="mb-4 sm:mb-6 flex flex-col gap-3 sm:flex-row sm:justify-between sm:items-center">
          <h1 className="text-xl sm:text-2xl font-bold text-gray-900">Conversations</h1>
          <button
            onClick={loadConversations}
            disabled={loading}
            className="px-3 py-2 text-sm sm:px-4 sm:text-base bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50 w-full sm:w-auto"
          >
            {loading ? 'Loading...' : 'Refresh'}
          </button>
        </div>

        {/* Search */}
        <form onSubmit={handleSearch} className="mb-4 sm:mb-6">
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              type="text"
              placeholder="Filter by phone..."
              value={phoneFilter}
              onChange={(e) => setPhoneFilter(e.target.value)}
              className="flex-1 px-3 py-2 text-sm sm:px-4 sm:text-base border border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
            />
            <button
              type="submit"
              className="px-3 py-2 text-sm sm:px-4 sm:text-base bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200"
            >
              Search
            </button>
          </div>
        </form>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
            {error}
          </div>
        )}

        {/* Conversations Table */}
        <div className="bg-white shadow rounded-lg overflow-hidden">
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
                {conversations.length === 0 && !loading ? (
                  <tr>
                    <td colSpan={4} className="px-4 py-8 text-center text-gray-500 sm:px-6">
                      No conversations found
                    </td>
                  </tr>
                ) : (
                  conversations.map((conv) => (
                    <tr
                      key={conv.id}
                      onClick={() => onSelect(conv.id)}
                      className="hover:bg-gray-50 cursor-pointer"
                    >
                      <td className="px-4 py-4 whitespace-nowrap sm:px-6">
                        <span className="text-sm font-medium text-gray-900">
                          {formatPhone(conv.customer_phone)}
                        </span>
                      </td>
                      <td className="px-4 py-4 whitespace-nowrap sm:px-6">
                        <div className="text-sm text-gray-500">
                          <span>{conv.message_count}</span>
                          <span className="hidden sm:inline text-xs text-gray-400 ml-1">
                            ({conv.customer_message_count} user / {conv.ai_message_count} AI)
                          </span>
                        </div>
                      </td>
                      <td className="px-4 py-4 whitespace-nowrap sm:px-6">
                        <span
                          className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${
                            conv.status === 'active'
                              ? 'bg-green-100 text-green-800'
                              : 'bg-gray-100 text-gray-800'
                          }`}
                        >
                          {conv.status}
                        </span>
                      </td>
                      <td className="px-4 py-4 whitespace-nowrap text-sm text-gray-500 sm:px-6">
                        {conv.last_message_at ? timeAgo(conv.last_message_at) : timeAgo(conv.started_at)}
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
              className="px-3 py-2 text-sm sm:px-4 bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200 disabled:opacity-50"
            >
              Prev
            </button>
            <span className="text-xs sm:text-sm text-gray-600">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page === totalPages}
              className="px-3 py-2 text-sm sm:px-4 bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200 disabled:opacity-50"
            >
              Next
            </button>
          </div>
        )}

        <p className="mt-4 text-xs text-gray-400 text-center">
          Auto-refreshes every 30 seconds
        </p>
      </div>
    </div>
  );
}
