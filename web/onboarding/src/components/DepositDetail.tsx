import { useEffect, useState } from 'react';
import { getDeposit } from '../api/client';
import type { DepositDetailResponse } from '../types/deposit';

interface DepositDetailProps {
  orgId: string;
  depositId: string;
  onBack: () => void;
  onViewConversation?: (conversationId: string) => void;
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

function formatDate(dateString: string): string {
  const date = new Date(dateString);
  return date.toLocaleDateString('en-US', {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  });
}

function getStatusColor(status: string): string {
  switch (status.toLowerCase()) {
    case 'succeeded':
    case 'completed':
      return 'bg-green-100 text-green-800';
    case 'pending':
      return 'bg-yellow-100 text-yellow-800';
    case 'failed':
    case 'refunded':
      return 'bg-red-100 text-red-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}

function InfoCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-white rounded-lg shadow p-4">
      <h3 className="text-sm font-medium text-gray-500 mb-3">{title}</h3>
      {children}
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between py-2 border-b border-gray-100 last:border-0">
      <span className="text-sm text-gray-500">{label}</span>
      <span className="text-sm font-medium text-gray-900">{value || '-'}</span>
    </div>
  );
}

export function DepositDetail({ orgId, depositId, onBack, onViewConversation }: DepositDetailProps) {
  const [deposit, setDeposit] = useState<DepositDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let isActive = true;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const data = await getDeposit(orgId, depositId);
        if (isActive) {
          setDeposit(data);
        }
      } catch (err) {
        if (isActive) {
          setError(err instanceof Error ? err.message : 'Failed to load deposit');
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
  }, [orgId, depositId]);

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
            <span>&larr;</span> Back to deposits
          </button>
          <div className="p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
            {error}
          </div>
        </div>
      </div>
    );
  }

  if (!deposit) {
    return null;
  }

  return (
    <div className="min-h-screen bg-gray-50 py-10">
      <div className="max-w-3xl mx-auto px-4">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
            >
              <span>&larr;</span> Back
            </button>
            <h1 className="text-2xl font-bold text-gray-900">Deposit Details</h1>
          </div>
          <span
            className={`inline-flex px-3 py-1 text-sm font-semibold rounded-full ${getStatusColor(
              deposit.status
            )}`}
          >
            {deposit.status}
          </span>
        </div>

        {/* Amount Hero */}
        <div className="bg-white rounded-lg shadow p-6 mb-6 text-center">
          <p className="text-sm text-gray-500 mb-1">Deposit Amount</p>
          <p className="text-4xl font-bold text-green-600">{formatCents(deposit.amount_cents)}</p>
          <p className="text-sm text-gray-500 mt-2">{formatDate(deposit.created_at)}</p>
        </div>

        {/* Info Cards Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
          {/* Patient Info */}
          <InfoCard title="Patient Information">
            <InfoRow label="Name" value={deposit.lead_name} />
            <InfoRow label="Phone" value={formatPhone(deposit.lead_phone)} />
            <InfoRow label="Email" value={deposit.lead_email} />
            <InfoRow
              label="Patient Type"
              value={
                deposit.patient_type && (
                  <span
                    className={`inline-flex px-2 py-0.5 text-xs font-semibold rounded-full ${
                      deposit.patient_type === 'new'
                        ? 'bg-blue-100 text-blue-800'
                        : 'bg-gray-100 text-gray-800'
                    }`}
                  >
                    {deposit.patient_type}
                  </span>
                )
              }
            />
          </InfoCard>

          {/* Service Details */}
          <InfoCard title="Service Details">
            <InfoRow label="Service Interest" value={deposit.service_interest} />
            <InfoRow label="Preferred Days" value={deposit.preferred_days} />
            <InfoRow label="Preferred Times" value={deposit.preferred_times} />
            {deposit.scheduling_notes && (
              <div className="mt-2 pt-2 border-t border-gray-100">
                <p className="text-sm text-gray-500 mb-1">Scheduling Notes</p>
                <p className="text-sm text-gray-700">{deposit.scheduling_notes}</p>
              </div>
            )}
          </InfoCard>

          {/* Payment Details */}
          <InfoCard title="Payment Details">
            <InfoRow label="Provider" value={deposit.provider} />
            <InfoRow
              label="Reference"
              value={
                deposit.provider_ref && (
                  <span className="font-mono text-xs">{deposit.provider_ref}</span>
                )
              }
            />
            <InfoRow label="Status" value={deposit.status} />
            {deposit.scheduled_for && (
              <InfoRow label="Scheduled For" value={formatDate(deposit.scheduled_for)} />
            )}
          </InfoCard>

          {/* Actions */}
          <InfoCard title="Actions">
            {deposit.conversation_id && onViewConversation && (
              <button
                onClick={() => onViewConversation(deposit.conversation_id!)}
                className="w-full py-2 px-4 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 mb-2"
              >
                View Conversation
              </button>
            )}
            <a
              href={`tel:${deposit.lead_phone}`}
              className="block w-full py-2 px-4 bg-green-600 text-white rounded-md hover:bg-green-700 text-center"
            >
              Call Patient
            </a>
            {deposit.lead_email && (
              <a
                href={`mailto:${deposit.lead_email}`}
                className="block w-full py-2 px-4 bg-gray-100 text-gray-700 rounded-md hover:bg-gray-200 text-center mt-2"
              >
                Email Patient
              </a>
            )}
          </InfoCard>
        </div>

        {/* IDs (for debugging) */}
        <div className="text-xs text-gray-400 mt-6">
          <p>Deposit ID: {deposit.id}</p>
          {deposit.lead_id && <p>Lead ID: {deposit.lead_id}</p>}
          {deposit.booking_intent_id && <p>Booking Intent ID: {deposit.booking_intent_id}</p>}
          {deposit.conversation_id && <p>Conversation ID: {deposit.conversation_id}</p>}
        </div>
      </div>
    </div>
  );
}
