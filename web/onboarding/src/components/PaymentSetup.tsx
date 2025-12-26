import { getSquareConnectUrl } from '../api/client';

interface Props {
  orgId: string;
  isConnected: boolean;
  merchantId?: string;
  onBack: () => void;
  onContinue: () => void;
}

export function PaymentSetup({ orgId, isConnected, merchantId, onBack, onContinue }: Props) {
  const connectUrl = getSquareConnectUrl(orgId);

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">Payment Setup</h2>
        <p className="mt-1 text-sm text-gray-600">
          Connect your Square account to accept deposits and payments from clients.
        </p>
      </div>

      <div className="bg-gray-50 rounded-lg p-6">
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
              Accept deposits for appointments and process payments securely.
            </p>
            <a
              href={connectUrl}
              className="mt-4 inline-flex items-center rounded-md border border-transparent bg-black py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-gray-800"
            >
              <svg className="h-5 w-5 mr-2" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm0 2c5.523 0 10 4.477 10 10s-4.477 10-10 10S2 17.523 2 12 6.477 2 12 2z" />
              </svg>
              Connect with Square
            </a>
          </div>
        )}
      </div>

      <div className="bg-blue-50 rounded-lg p-4">
        <h4 className="text-sm font-medium text-blue-900">Why Square?</h4>
        <ul className="mt-2 text-sm text-blue-700 space-y-1">
          <li>- Secure payment processing with PCI compliance</li>
          <li>- Accept deposits for appointment bookings</li>
          <li>- Automatic payment reminders</li>
          <li>- Integrated with your booking system</li>
        </ul>
      </div>

      <div className="flex justify-between pt-4">
        <button
          type="button"
          onClick={onBack}
          className="rounded-md border border-gray-300 bg-white py-2 px-4 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
        >
          Back
        </button>
        <button
          type="button"
          onClick={onContinue}
          className="rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
        >
          {isConnected ? 'Continue' : 'Skip for Now'}
        </button>
      </div>
    </div>
  );
}
