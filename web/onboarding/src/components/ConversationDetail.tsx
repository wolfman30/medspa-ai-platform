import { useEffect, useState, useRef } from 'react';
import { getConversation, type ApiScope } from '../api/client';
import type { ConversationDetailResponse, MessageResponse } from '../types/conversation';

interface ConversationDetailProps {
  orgId: string;
  conversationId: string;
  onBack: () => void;
  scope?: ApiScope;
}

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

function formatTime(dateString: string): string {
  const date = new Date(dateString);
  return date.toLocaleTimeString('en-US', {
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
    timeZone: 'America/New_York',
  });
}

function formatDate(dateString: string): string {
  const date = new Date(dateString);
  return date.toLocaleDateString('en-US', {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    timeZone: 'America/New_York',
  });
}

function MessageBubble({ message }: { message: MessageResponse }) {
  const isUser = message.role === 'user';
  const statusLabel = message.status ? message.status.replace(/_/g, ' ') : '';

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-4`}>
      <div
        className={`max-w-[75%] rounded-lg px-4 py-3 ${
          isUser
            ? 'bg-indigo-600 text-white rounded-br-sm'
            : 'bg-gray-100 text-gray-900 rounded-bl-sm'
        }`}
      >
        <div className="text-sm whitespace-pre-wrap break-words">{message.content}</div>
        <div
          className={`text-xs mt-1 ${isUser ? 'text-indigo-200' : 'text-gray-500'}`}
          title={message.error_reason || undefined}
        >
          {formatTime(message.timestamp)}
          {statusLabel ? (
            <>
              {' '}
              &bull; {statusLabel}
            </>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function ConversationDetail({ orgId, conversationId, onBack, scope = 'admin' }: ConversationDetailProps) {
  const [conversation, setConversation] = useState<ConversationDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let isActive = true;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const data = await getConversation(orgId, conversationId, scope);
        if (isActive) {
          setConversation(data);
        }
      } catch (err) {
        if (isActive) {
          setError(err instanceof Error ? err.message : 'Failed to load conversation');
        }
      } finally {
        if (isActive) {
          setLoading(false);
        }
      }
    }

    load();
    return () => {
      isActive = false;
    };
  }, [orgId, conversationId]);

  // Scroll to bottom when messages load
  useEffect(() => {
    if (conversation?.messages.length) {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [conversation?.messages.length]);

  // Auto-refresh every 10 seconds
  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const data = await getConversation(orgId, conversationId, scope);
        setConversation(data);
      } catch {
        // Silently fail on refresh
      }
    }, 10000);
    return () => clearInterval(interval);
  }, [orgId, conversationId, scope]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="min-h-screen bg-gray-50 py-10">
        <div className="max-w-3xl mx-auto px-4">
          <button
            onClick={onBack}
            className="mb-4 text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
          >
            <span>&larr;</span> Back to conversations
          </button>
          <div className="p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
            {error}
          </div>
        </div>
      </div>
    );
  }

  if (!conversation) {
    return null;
  }

  return (
    <div className="flex-1 flex flex-col bg-gray-50">
      {/* Header */}
      <div className="bg-white border-b border-gray-200 px-4 py-4">
        <div className="max-w-3xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
            >
              <span>&larr;</span> Back
            </button>
            <div>
              <h1 className="text-lg font-semibold text-gray-900">
                {formatPhone(conversation.customer_phone)}
              </h1>
              <p className="text-xs text-gray-500">
                Started {formatDate(conversation.started_at)} &bull;{' '}
                {conversation.metadata.total_messages} messages
              </p>
            </div>
          </div>
          <span
            className={`inline-flex px-2 py-1 text-xs font-semibold rounded-full ${
              conversation.status === 'active'
                ? 'bg-green-100 text-green-800'
                : 'bg-gray-100 text-gray-800'
            }`}
          >
            {conversation.status}
          </span>
        </div>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto py-6">
        <div className="max-w-3xl mx-auto px-4">
          {conversation.messages.length === 0 ? (
            <div className="text-center text-gray-500 py-8">No messages found</div>
          ) : (
            <>
              {conversation.messages.map((msg, index) => (
                <MessageBubble key={msg.id || index} message={msg} />
              ))}
              <div ref={messagesEndRef} />
            </>
          )}
        </div>
      </div>

      {/* Footer */}
      <div className="bg-white border-t border-gray-200 px-4 py-3">
        <div className="max-w-3xl mx-auto">
          <p className="text-xs text-gray-400 text-center">
            Source: {conversation.metadata.source} &bull; Times shown in ET &bull; Auto-refreshes every 10 seconds
          </p>
        </div>
      </div>
    </div>
  );
}
