import { useEffect, useState } from 'react';
import { getSquareConnectUrl } from '../api/client';

interface Props {
  orgId: string;
  isConnected: boolean;
  merchantId?: string;
  onBack: () => void;
  onContinue: () => void;
}

export function PaymentSetup({ orgId, isConnected, merchantId, onBack, onContinue }: Props) {
  const [connectUrl, setConnectUrl] = useState<string>('');
  const [connectError, setConnectError] = useState<string | null>(null);

  useEffect(() => {
    let isActive = true;
    setConnectError(null);
    getSquareConnectUrl(orgId)
      .then((url) => {
        if (isActive) setConnectUrl(url);
      })
      .catch((err) => {
        if (!isActive) return;
        setConnectError(err instanceof Error ? err.message : 'Failed to load Square connect link');
      });
    return () => {
      isActive = false;
    };
  }, [orgId]);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight text-slate-900">Payment Setup</h2>
        <p className="ui-muted mt-1">
          Connect your Square account to accept deposits and payments from clients.
        </p>
      </div>

      <div className="border border-slate-200/80 bg-slate-50/60 rounded-2xl p-6">
        {isConnected ? (
          <div className="text-center">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-green-100">
              <svg
                className="h-6 w-6 text-green-600"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth="1.5"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M4.5 12.75l6 6 9-13.5"
                />
              </svg>
            </div>
            <h3 className="mt-4 text-lg font-medium text-gray-900">Square Connected!</h3>
            <p className="mt-2 text-sm text-gray-500">
              Your Square account is connected and ready to accept payments.
            </p>
            {merchantId && (
              <p className="mt-1 text-xs text-gray-400">Merchant ID: {merchantId}</p>
            )}
          </div>
        ) : (
          <div className="text-center">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-gray-200">
              <svg
                className="h-6 w-6 text-gray-600"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth="1.5"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M2.25 8.25h19.5M2.25 9h19.5m-16.5 5.25h6m-6 2.25h3m-3.75 3h15a2.25 2.25 0 002.25-2.25V6.75A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25v10.5A2.25 2.25 0 004.5 19.5z"
                />
              </svg>
            </div>
            <h3 className="mt-4 text-lg font-medium text-gray-900">Connect Square</h3>
            <p className="mt-2 text-sm text-gray-500">
              Accept deposits for appointment requests and process payments securely.
            </p>
            {connectError ? (
              <p className="mt-4 text-sm text-red-600">{connectError}</p>
            ) : (
              <a
                href={connectUrl || '#'}
                className="ui-btn ui-btn-dark mt-4"
                aria-disabled={!connectUrl}
              >
                <svg className="h-5 w-5 mr-2" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm0 2c5.523 0 10 4.477 10 10s-4.477 10-10 10S2 17.523 2 12 6.477 2 12 2z" />
                </svg>
                {connectUrl ? 'Connect with Square' : 'Loading link...'}
              </a>
            )}
          </div>
        )}
      </div>

      <div className="border border-slate-200/70 bg-slate-50/40 rounded-2xl p-5">
        <h4 className="ui-kicker text-slate-600">Why Square?</h4>
        <ul className="mt-2 text-sm text-slate-700 space-y-1">
          <li>Secure payment processing with PCI compliance</li>
          <li>Accept deposits for appointment requests</li>
          <li>Automatic payment reminders</li>
          <li>Works alongside your booking system (no EMR/EHR or CRM sync in phase 1)</li>
        </ul>
      </div>

      <div className="flex justify-between pt-4">
        <button
          type="button"
          onClick={onBack}
          className="ui-btn ui-btn-ghost"
        >
          Back
        </button>
        <button
          type="button"
          onClick={onContinue}
          className="ui-btn ui-btn-primary"
        >
          {isConnected ? 'Continue' : 'Skip for Now'}
        </button>
      </div>
    </div>
  );
}
