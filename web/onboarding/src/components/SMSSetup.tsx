import { useState } from 'react';
import { activatePhoneNumber } from '../api/client';

interface Props {
  orgId: string;
  phoneNumber?: string;
  status: 'pending' | 'verified' | 'active' | 'not_started';
  onBack: () => void;
  onComplete: () => void;
  onPhoneActivated?: (phone: string) => void;
}

export function SMSSetup({ orgId, phoneNumber, status, onBack, onComplete, onPhoneActivated }: Props) {
  const isReady = status === 'active' || status === 'verified';
  const [manualPhone, setManualPhone] = useState('');
  const [activating, setActivating] = useState(false);
  const [activationError, setActivationError] = useState<string | null>(null);
  const [activationSuccess, setActivationSuccess] = useState(false);

  const handleActivatePhone = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!manualPhone.trim()) return;

    setActivating(true);
    setActivationError(null);
    setActivationSuccess(false);

    try {
      const result = await activatePhoneNumber(orgId, manualPhone.trim());
      setActivationSuccess(true);
      setManualPhone('');
      if (onPhoneActivated) {
        onPhoneActivated(result.phone_number);
      }
    } catch (err) {
      setActivationError(err instanceof Error ? err.message : 'Failed to activate phone number');
    } finally {
      setActivating(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">SMS Setup</h2>
        <p className="mt-1 text-sm text-gray-600">
          Configure your SMS phone number for client communications.
        </p>
      </div>

      <div className="bg-green-50 border border-green-200 rounded-lg p-4">
        <h3 className="text-sm font-medium text-green-800 mb-2">Quick Activation (Dev/Testing)</h3>
        <p className="text-sm text-green-700 mb-3">
          If you already have a Telnyx phone number configured externally, enter it here to activate it for this clinic.
        </p>
        <form onSubmit={handleActivatePhone} className="flex gap-2">
          <input
            type="tel"
            value={manualPhone}
            onChange={(e) => setManualPhone(e.target.value)}
            placeholder="+1234567890"
            className="flex-1 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-green-500 focus:outline-none focus:ring-1 focus:ring-green-500"
          />
          <button
            type="submit"
            disabled={activating || !manualPhone.trim()}
            className="rounded-md bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2 disabled:opacity-50"
          >
            {activating ? 'Activating...' : 'Activate'}
          </button>
        </form>
        {activationError && (
          <p className="mt-2 text-sm text-red-600">{activationError}</p>
        )}
        {activationSuccess && (
          <p className="mt-2 text-sm text-green-600">Phone number activated successfully!</p>
        )}
      </div>

      <div className="bg-amber-50 border border-amber-200 rounded-lg p-4">
        <div className="flex">
          <div className="flex-shrink-0">
            <svg className="h-5 w-5 text-amber-400" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 5a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 5zm0 9a1 1 0 100-2 1 1 0 000 2z"
                clipRule="evenodd"
              />
            </svg>
          </div>
          <div className="ml-3">
            <h3 className="text-sm font-medium text-amber-800">10DLC Registration Required</h3>
            <div className="mt-2 text-sm text-amber-700">
              <p>
                Business SMS messaging in the US requires 10DLC (10-Digit Long Code) registration.
                This process typically takes 2-4 weeks for approval.
              </p>
              <p className="mt-2">
                We'll help you through the registration process after you complete onboarding.
              </p>
            </div>
          </div>
        </div>
      </div>

      <div className="bg-gray-50 rounded-lg p-6">
        <h3 className="text-lg font-medium text-gray-900 mb-4">Registration Status</h3>

        <div className="space-y-4">
          <StatusItem
            label="Brand Registration"
            description="Register your business with carriers"
            status={status === 'not_started' ? 'pending' : 'completed'}
          />
          <StatusItem
            label="Campaign Registration"
            description="Register your messaging use case"
            status={status === 'not_started' ? 'pending' : status === 'pending' ? 'in_progress' : 'completed'}
          />
          <StatusItem
            label="Phone Number Setup"
            description="Assign a phone number to your campaign"
            status={isReady ? 'completed' : 'pending'}
          />
        </div>

        {phoneNumber && (
          <div className="mt-4 p-3 bg-white rounded border">
            <p className="text-sm text-gray-500">Assigned Number:</p>
            <p className="text-lg font-mono text-gray-900">{phoneNumber}</p>
          </div>
        )}
      </div>

      <div className="bg-blue-50 rounded-lg p-4">
        <h4 className="text-sm font-medium text-blue-900">What happens next?</h4>
        <ol className="mt-2 text-sm text-blue-700 space-y-1 list-decimal list-inside">
          <li>We'll submit your brand for registration</li>
          <li>Carriers review and approve (typically 1-2 weeks)</li>
          <li>We'll register your messaging campaign</li>
          <li>Your SMS number will be activated</li>
        </ol>
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
          onClick={onComplete}
          className="rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
        >
          Complete Setup
        </button>
      </div>
    </div>
  );
}

function StatusItem({
  label,
  description,
  status,
}: {
  label: string;
  description: string;
  status: 'pending' | 'in_progress' | 'completed';
}) {
  return (
    <div className="flex items-start">
      <div className="flex-shrink-0">
        {status === 'completed' ? (
          <div className="h-6 w-6 rounded-full bg-green-100 flex items-center justify-center">
            <svg className="h-4 w-4 text-green-600" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                clipRule="evenodd"
              />
            </svg>
          </div>
        ) : status === 'in_progress' ? (
          <div className="h-6 w-6 rounded-full bg-yellow-100 flex items-center justify-center">
            <div className="h-3 w-3 rounded-full bg-yellow-500 animate-pulse" />
          </div>
        ) : (
          <div className="h-6 w-6 rounded-full bg-gray-100 flex items-center justify-center">
            <div className="h-3 w-3 rounded-full bg-gray-300" />
          </div>
        )}
      </div>
      <div className="ml-3">
        <p className="text-sm font-medium text-gray-900">{label}</p>
        <p className="text-sm text-gray-500">{description}</p>
      </div>
    </div>
  );
}
