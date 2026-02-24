import { useEffect, useState } from 'react';
import { getDeposit, type ApiScope } from '../api/client';
import type { DepositDetailResponse } from '../types/deposit';

interface DepositDetailProps {
  orgId: string;
  depositId: string;
  onBack: () => void;
  onViewConversation?: (conversationId: string) => void;
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

function InfoCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="ui-card ui-card-solid p-5">
      <h3 className="ui-kicker mb-3">{title}</h3>
      {children}
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-2 border-b border-slate-100 last:border-0">
      <span className="text-sm text-slate-500">{label}</span>
      <span className="text-sm font-semibold text-slate-900">{value || '-'}</span>
    </div>
  );
}

export function DepositDetail({ orgId, depositId, onBack, onViewConversation, scope = 'admin' }: DepositDetailProps) {
  const [deposit, setDeposit] = useState<DepositDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let isActive = true;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const data = await getDeposit(orgId, depositId, scope);
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
      <div className="ui-page flex items-center justify-center">
        <div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="ui-page">
        <div className="ui-container max-w-3xl space-y-4">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back to deposits
          </button>
          <div className="p-4 bg-red-50 border border-red-200 rounded-xl text-red-800">
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
    <div className="ui-page">
      <div className="ui-container max-w-3xl">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between gap-4">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="ui-link font-semibold flex items-center gap-2"
            >
              <span aria-hidden="true">&larr;</span> Back
            </button>
            <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Deposit Details</h1>
          </div>
          <span
            className={`ui-badge ${getStatusColor(deposit.status)}`}
          >
            {deposit.status}
          </span>
        </div>

        {/* Amount Hero */}
        <div className="ui-card ui-card-solid p-8 mb-6 text-center">
          <p className="ui-kicker mb-2">Deposit Amount</p>
          <p className="text-4xl font-semibold tracking-tight text-emerald-700">{formatCents(deposit.amount_cents)}</p>
          <p className="ui-muted mt-2">{formatDate(deposit.created_at)}</p>
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
                    className={`ui-badge ${deposit.patient_type === 'new' ? 'bg-indigo-100 text-indigo-900' : 'ui-badge-muted'}`}
                  >
                    {deposit.patient_type}
                  </span>
                )
              }
            />
          </InfoCard>

          {/* Service Details */}
          <InfoCard title="Scheduling Preferences">
            <InfoRow label="Service" value={deposit.service_interest} />
            <InfoRow label="Preferred Days" value={deposit.preferred_days} />
            <InfoRow label="Preferred Times" value={deposit.preferred_times} />
            {deposit.scheduling_notes && (
              <div className="mt-2 pt-2 border-t border-gray-100">
                <p className="ui-kicker mb-1">Scheduling Notes</p>
                <p className="text-sm text-slate-700">{deposit.scheduling_notes}</p>
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
                className="ui-btn ui-btn-primary w-full py-3 mb-2"
              >
                View Conversation
              </button>
            )}
            <a
              href={`tel:${deposit.lead_phone}`}
              className="ui-btn ui-btn-dark w-full py-3 text-center"
            >
              Call Patient
            </a>
            {deposit.lead_email && (
              <a
                href={`mailto:${deposit.lead_email}`}
                className="ui-btn ui-btn-ghost w-full py-3 text-center mt-2"
              >
                Email Patient
              </a>
            )}
          </InfoCard>
        </div>

        {/* IDs (for debugging) */}
        <div className="text-xs text-slate-400 mt-6">
          <p>Deposit ID: {deposit.id}</p>
          {deposit.lead_id && <p>Lead ID: {deposit.lead_id}</p>}
          {deposit.booking_intent_id && <p>Booking Intent ID: {deposit.booking_intent_id}</p>}
          {deposit.conversation_id && <p>Conversation ID: {deposit.conversation_id}</p>}
        </div>

        <div className="mt-8">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back to deposits
          </button>
        </div>
      </div>
    </div>
  );
}
