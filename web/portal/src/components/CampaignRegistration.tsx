import { useState } from 'react';
import { createCampaign, type CreateCampaignRequest } from '../api/client';

interface Props {
  orgId: string;
  brandId?: string;
  onBack: () => void;
  onComplete: (campaignId: string) => void;
}

const USE_CASE_OPTIONS = [
  { value: 'CUSTOMER_CARE', label: 'Customer Care', description: 'Support and service inquiries' },
  { value: 'MARKETING', label: 'Marketing', description: 'Promotional messages and offers' },
  { value: 'APPOINTMENTS', label: 'Appointments', description: 'Scheduling and reminders' },
  { value: 'NOTIFICATIONS', label: 'Notifications', description: 'Alerts and updates' },
];

const DEFAULT_SAMPLE_MESSAGES = [
  'Hi [Name], this is [Clinic]. Your appointment is confirmed for [Date] at [Time]. Reply YES to confirm or call us to reschedule.',
  'Thank you for your inquiry! A member of our team will reach out within 24 hours. Reply STOP to opt out.',
  'Hi [Name], we have availability this week for your consultation. Would you like to book? Reply or call [Phone].',
];

export function CampaignRegistration({ orgId: _orgId, brandId, onBack, onComplete }: Props) {
  // orgId is available for future use (e.g., fetching existing brand info)
  void _orgId;
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [formData, setFormData] = useState({
    useCases: ['CUSTOMER_CARE', 'MARKETING'] as string[],
    description: 'Low volume mixed messaging for medical spa customer support, appointment scheduling, and promotional communications.',
    sampleMessages: DEFAULT_SAMPLE_MESSAGES,
    messageFlow: 'Patients initiate contact via SMS to inquire about services or appointments. Staff responds with information and scheduling options. Promotional messages are sent to opted-in customers about special offers.',
    optInDescription: 'Customers provide consent by texting our business number or through our website booking form where they check a box agreeing to receive SMS communications.',
    optOutDescription: 'Customers can reply STOP at any time to unsubscribe from all messages. They will receive a confirmation that they have been opted out.',
    helpMessage: 'For assistance, reply HELP or call our clinic directly. Msg&data rates may apply. Reply STOP to opt out.',
    stopMessage: 'You have been unsubscribed from our messages. Reply START to resubscribe or contact us directly.',
  });

  const updateSampleMessage = (index: number, value: string) => {
    const updated = [...formData.sampleMessages];
    updated[index] = value;
    setFormData({ ...formData, sampleMessages: updated });
  };

  const addSampleMessage = () => {
    if (formData.sampleMessages.length < 5) {
      setFormData({
        ...formData,
        sampleMessages: [...formData.sampleMessages, ''],
      });
    }
  };

  const removeSampleMessage = (index: number) => {
    if (formData.sampleMessages.length > 2) {
      const updated = formData.sampleMessages.filter((_, i) => i !== index);
      setFormData({ ...formData, sampleMessages: updated });
    }
  };

  const toggleUseCase = (value: string) => {
    const updated = formData.useCases.includes(value)
      ? formData.useCases.filter((v) => v !== value)
      : [...formData.useCases, value];
    setFormData({ ...formData, useCases: updated });
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!brandId) {
      setError('Brand registration is required before creating a campaign');
      return;
    }

    // Validate use cases
    if (formData.useCases.length === 0) {
      setError('At least one use case must be selected');
      return;
    }

    // Validate sample messages
    const validMessages = formData.sampleMessages.filter(m => m.trim().length > 0);
    if (validMessages.length < 2) {
      setError('At least 2 sample messages are required');
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      // Use "LOW_VOLUME_MIXED" as the campaign type with selected use cases
      const request: CreateCampaignRequest = {
        brand_internal_id: brandId,
        use_case: `LOW_VOLUME_MIXED:${formData.useCases.join(',')}`,
        sample_messages: validMessages,
        help_message: formData.helpMessage,
        stop_message: formData.stopMessage,
      };

      const result = await createCampaign(request);
      onComplete(result.campaign_id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to submit campaign');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">10DLC Campaign Registration</h2>
        <p className="mt-1 text-sm text-gray-600">
          Register your messaging use case with carriers. This is required for business SMS in the US.
        </p>
      </div>

      {!brandId && (
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
              <h3 className="text-sm font-medium text-amber-800">Brand Registration Required</h3>
              <p className="mt-1 text-sm text-amber-700">
                You must complete brand registration before creating a campaign. For demo/testing purposes,
                this step can be skipped by your administrator.
              </p>
            </div>
          </div>
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Campaign Type */}
        <div className="bg-indigo-50 border border-indigo-200 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-2">
            <span className="inline-flex items-center rounded-md bg-indigo-100 px-2 py-1 text-xs font-medium text-indigo-700">
              Low Volume Mixed
            </span>
            <span className="text-sm text-gray-600">Campaign Type</span>
          </div>
          <p className="text-xs text-gray-500">
            Recommended for medical spas with less than 6,000 messages per day.
          </p>
        </div>

        {/* Use Cases */}
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-3">
            Use Case Types (select all that apply)
          </label>
          <div className="space-y-3">
            {USE_CASE_OPTIONS.map((option) => (
              <label
                key={option.value}
                className={`flex items-start p-3 rounded-lg border cursor-pointer transition-colors ${
                  formData.useCases.includes(option.value)
                    ? 'border-indigo-500 bg-indigo-50'
                    : 'border-gray-200 hover:border-gray-300'
                }`}
              >
                <input
                  type="checkbox"
                  checked={formData.useCases.includes(option.value)}
                  onChange={() => toggleUseCase(option.value)}
                  className="mt-0.5 h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
                />
                <div className="ml-3">
                  <span className="text-sm font-medium text-gray-900">{option.label}</span>
                  <p className="text-xs text-gray-500">{option.description}</p>
                </div>
              </label>
            ))}
          </div>
        </div>

        {/* Campaign Description */}
        <div>
          <label htmlFor="description" className="block text-sm font-medium text-gray-700">
            Campaign Description
          </label>
          <textarea
            id="description"
            rows={3}
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            placeholder="Describe how you will use SMS messaging..."
          />
          <p className="mt-1 text-xs text-gray-500">
            Describe the purpose and nature of your SMS communications.
          </p>
        </div>

        {/* Sample Messages */}
        <div>
          <div className="flex justify-between items-center mb-2">
            <label className="block text-sm font-medium text-gray-700">
              Sample Messages (2-5 required)
            </label>
            {formData.sampleMessages.length < 5 && (
              <button
                type="button"
                onClick={addSampleMessage}
                className="text-sm text-indigo-600 hover:text-indigo-500"
              >
                + Add Message
              </button>
            )}
          </div>
          <div className="space-y-3">
            {formData.sampleMessages.map((message, index) => (
              <div key={index} className="flex gap-2">
                <textarea
                  rows={2}
                  value={message}
                  onChange={(e) => updateSampleMessage(index, e.target.value)}
                  className="flex-1 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
                  placeholder={`Sample message ${index + 1}...`}
                />
                {formData.sampleMessages.length > 2 && (
                  <button
                    type="button"
                    onClick={() => removeSampleMessage(index)}
                    className="text-red-500 hover:text-red-700 px-2"
                  >
                    <svg className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                      <path
                        fillRule="evenodd"
                        d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z"
                        clipRule="evenodd"
                      />
                    </svg>
                  </button>
                )}
              </div>
            ))}
          </div>
          <p className="mt-1 text-xs text-gray-500">
            Provide realistic examples of messages you will send. Use [brackets] for variable content.
          </p>
        </div>

        {/* Message Flow */}
        <div>
          <label htmlFor="messageFlow" className="block text-sm font-medium text-gray-700">
            Message Flow Description
          </label>
          <textarea
            id="messageFlow"
            rows={3}
            value={formData.messageFlow}
            onChange={(e) => setFormData({ ...formData, messageFlow: e.target.value })}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            placeholder="Describe how messages flow between you and customers..."
          />
          <p className="mt-1 text-xs text-gray-500">
            Explain how customers interact with your SMS service.
          </p>
        </div>

        {/* Opt-In Description */}
        <div>
          <label htmlFor="optIn" className="block text-sm font-medium text-gray-700">
            Opt-In Process
          </label>
          <textarea
            id="optIn"
            rows={2}
            value={formData.optInDescription}
            onChange={(e) => setFormData({ ...formData, optInDescription: e.target.value })}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            placeholder="How do customers consent to receive messages?"
          />
          <p className="mt-1 text-xs text-gray-500">
            Describe how customers provide consent to receive SMS messages.
          </p>
        </div>

        {/* Opt-Out Description */}
        <div>
          <label htmlFor="optOut" className="block text-sm font-medium text-gray-700">
            Opt-Out Process
          </label>
          <textarea
            id="optOut"
            rows={2}
            value={formData.optOutDescription}
            onChange={(e) => setFormData({ ...formData, optOutDescription: e.target.value })}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            placeholder="How can customers stop receiving messages?"
          />
        </div>

        {/* HELP Message */}
        <div>
          <label htmlFor="helpMessage" className="block text-sm font-medium text-gray-700">
            HELP Response Message
          </label>
          <textarea
            id="helpMessage"
            rows={2}
            value={formData.helpMessage}
            onChange={(e) => setFormData({ ...formData, helpMessage: e.target.value })}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            placeholder="Message sent when customer texts HELP..."
          />
          <p className="mt-1 text-xs text-gray-500">
            This message is sent when customers text HELP.
          </p>
        </div>

        {/* STOP Message */}
        <div>
          <label htmlFor="stopMessage" className="block text-sm font-medium text-gray-700">
            STOP Response Message
          </label>
          <textarea
            id="stopMessage"
            rows={2}
            value={formData.stopMessage}
            onChange={(e) => setFormData({ ...formData, stopMessage: e.target.value })}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            placeholder="Message sent when customer texts STOP..."
          />
          <p className="mt-1 text-xs text-gray-500">
            This message is sent when customers text STOP to unsubscribe.
          </p>
        </div>

        {error && (
          <div className="rounded-md bg-red-50 p-4">
            <p className="text-sm text-red-700">{error}</p>
          </div>
        )}

        <div className="bg-blue-50 rounded-lg p-4">
          <h4 className="text-sm font-medium text-blue-900">What happens next?</h4>
          <ul className="mt-2 text-sm text-blue-700 space-y-1 list-disc list-inside">
            <li>Campaign is submitted to The Campaign Registry (TCR)</li>
            <li>Carriers review the campaign (typically 1-3 business days)</li>
            <li>Once approved, phone numbers can be assigned to the campaign</li>
            <li>Your SMS service will be fully activated</li>
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
            type="submit"
            disabled={submitting || !brandId}
            className="rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {submitting ? 'Submitting...' : 'Submit Campaign'}
          </button>
        </div>
      </form>
    </div>
  );
}
