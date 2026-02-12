import { useEffect, useState } from 'react';
import { getStripeStatus, getStripeConnectUrl, type ApiScope } from '../api/client';

interface Props {
  orgId: string;
  scope?: ApiScope;
  onBack: () => void;
}

export function StripePaymentSettings({ orgId, scope = 'portal', onBack }: Props) {
  const [connected, setConnected] = useState(false);
  const [accountId, setAccountId] = useState<string | null>(null);
  const [connectUrl, setConnectUrl] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);

    Promise.all([
      getStripeStatus(orgId, scope),
      getStripeConnectUrl(orgId),
    ])
      .then(([status, url]) => {
        if (!active) return;
        setConnected(status.connected);
        setAccountId(status.account_id || null);
        setConnectUrl(url);
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load Stripe status');
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => { active = false; };
  }, [orgId, scope]);

  if (loading) {
    return (
      <div className="ui-page flex items-center justify-center">
        <div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  return (
    <div className="ui-page">
      <div className="ui-container max-w-2xl">
        {/* Header */}
        <div className="mb-6 flex items-center gap-4">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back
          </button>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">
            Payment Settings
          </h1>
        </div>

        {error && (
          <div className="rounded-xl border border-red-200 bg-red-50 p-4 mb-6">
            <p className="text-sm font-medium text-red-800">{error}</p>
          </div>
        )}

        {/* Stripe Connect Card */}
        <div className="ui-card ui-card-solid p-6">
          <div className="flex items-start gap-4">
            {/* Stripe icon */}
            <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-violet-100">
              <svg className="h-6 w-6 text-violet-600" viewBox="0 0 24 24" fill="currentColor">
                <path d="M13.976 9.15c-2.172-.806-3.356-1.426-3.356-2.409 0-.831.683-1.305 1.901-1.305 2.227 0 4.515.858 6.09 1.631l.89-5.494C18.252.975 15.697 0 12.165 0 9.667 0 7.589.654 6.104 1.872 4.56 3.147 3.757 4.992 3.757 7.218c0 4.039 2.467 5.76 6.476 7.219 2.585.92 3.445 1.574 3.445 2.583 0 .98-.84 1.545-2.354 1.545-1.875 0-4.965-.921-6.99-2.109l-.9 5.555C5.175 22.99 8.385 24 11.714 24c2.641 0 4.843-.624 6.328-1.813 1.664-1.305 2.525-3.236 2.525-5.732 0-4.128-2.524-5.851-6.591-7.305z" />
              </svg>
            </div>
            <div className="flex-1">
              <h2 className="text-lg font-semibold tracking-tight text-slate-900">
                Stripe Connect
              </h2>

              {connected ? (
                <>
                  <div className="mt-2 flex items-center gap-2">
                    <span className="inline-flex h-2 w-2 rounded-full bg-green-500" />
                    <span className="text-sm font-medium text-green-700">Connected</span>
                  </div>
                  {accountId && (
                    <p className="mt-1 text-xs text-slate-400">
                      Account ID: {accountId}
                    </p>
                  )}
                  <p className="mt-3 text-sm text-slate-600">
                    Your Stripe account is connected and ready to collect deposits from patients.
                  </p>
                  {/* Re-connect option */}
                  <a
                    href={connectUrl || '#'}
                    className="ui-btn ui-btn-ghost mt-4 inline-flex items-center text-sm"
                  >
                    Reconnect account
                  </a>
                </>
              ) : (
                <>
                  <p className="mt-2 text-sm text-slate-600">
                    Connect your Stripe account to collect deposits from patients when they book
                    appointments. Deposits reduce no-shows and secure bookings.
                  </p>
                  <div className="mt-3 rounded-xl border border-amber-200 bg-amber-50 p-3">
                    <p className="text-sm text-amber-800">
                      <strong>Why connect Stripe?</strong> To collect deposits from patients when
                      they confirm an appointment. This reduces no-shows and ensures commitment.
                    </p>
                  </div>
                  <a
                    href={connectUrl || '#'}
                    className="ui-btn ui-btn-primary mt-4 inline-flex items-center gap-2"
                    aria-disabled={!connectUrl}
                  >
                    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M13.976 9.15c-2.172-.806-3.356-1.426-3.356-2.409 0-.831.683-1.305 1.901-1.305 2.227 0 4.515.858 6.09 1.631l.89-5.494C18.252.975 15.697 0 12.165 0 9.667 0 7.589.654 6.104 1.872 4.56 3.147 3.757 4.992 3.757 7.218c0 4.039 2.467 5.76 6.476 7.219 2.585.92 3.445 1.574 3.445 2.583 0 .98-.84 1.545-2.354 1.545-1.875 0-4.965-.921-6.99-2.109l-.9 5.555C5.175 22.99 8.385 24 11.714 24c2.641 0 4.843-.624 6.328-1.813 1.664-1.305 2.525-3.236 2.525-5.732 0-4.128-2.524-5.851-6.591-7.305z" />
                    </svg>
                    Connect with Stripe
                  </a>
                </>
              )}
            </div>
          </div>
        </div>

        {/* Back */}
        <div className="mt-8">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back to Settings
          </button>
        </div>
      </div>
    </div>
  );
}
