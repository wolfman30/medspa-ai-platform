import { useState } from 'react';
import { NotificationSettings } from './NotificationSettings';
import { AIPersonaSettings } from './AIPersonaSettings';
import { KnowledgeSettings } from './KnowledgeSettings';
import { BookingSettings } from './BookingSettings';
import { StripePaymentSettings } from './StripePaymentSettings';

interface SettingsPageProps {
  orgId: string;
  scope: 'admin' | 'portal';
  onBack: () => void;
}

type SettingsSection = 'menu' | 'notifications' | 'ai-persona' | 'knowledge' | 'booking' | 'payment';

export function SettingsPage({ orgId, scope, onBack }: SettingsPageProps) {
  const [section, setSection] = useState<SettingsSection>('menu');

  if (section === 'notifications') {
    return (
      <NotificationSettings
        orgId={orgId}
        onBack={() => setSection('menu')}
      />
    );
  }

  if (section === 'ai-persona') {
    return (
      <AIPersonaSettings
        orgId={orgId}
        onBack={() => setSection('menu')}
      />
    );
  }

  if (section === 'knowledge') {
    return (
      <KnowledgeSettings
        orgId={orgId}
        scope={scope}
        onBack={() => setSection('menu')}
      />
    );
  }

  if (section === 'booking') {
    return (
      <BookingSettings
        orgId={orgId}
        onBack={() => setSection('menu')}
      />
    );
  }

  if (section === 'payment') {
    return (
      <StripePaymentSettings
        orgId={orgId}
        scope={scope}
        onBack={() => setSection('menu')}
      />
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
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Settings</h1>
        </div>

        {/* Settings Menu */}
        <div className="space-y-4">
          <button
            onClick={() => setSection('notifications')}
            className="w-full ui-card ui-card-solid p-6 text-left transition-shadow hover:shadow-lg"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold tracking-tight text-slate-900">Notification Settings</h2>
                <p className="ui-muted mt-1">
                  Configure how and when you receive notifications about payments and leads
                </p>
              </div>
              <span className="text-slate-400 text-xl" aria-hidden="true">&rarr;</span>
            </div>
          </button>

          <button
            onClick={() => setSection('ai-persona')}
            className="w-full ui-card ui-card-solid p-6 text-left transition-shadow hover:shadow-lg"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold tracking-tight text-slate-900">AI Assistant Persona</h2>
                <p className="ui-muted mt-1">
                  Customize how your AI assistant communicates with patients
                </p>
              </div>
              <span className="text-slate-400 text-xl" aria-hidden="true">&rarr;</span>
            </div>
          </button>

          <button
            onClick={() => setSection('knowledge')}
            className="w-full ui-card ui-card-solid p-6 text-left transition-shadow hover:shadow-lg"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold tracking-tight text-slate-900">Clinic Knowledge</h2>
                <p className="ui-muted mt-1">
                  Review and update the knowledge the AI uses for your clinic
                </p>
              </div>
              <span className="text-slate-400 text-xl" aria-hidden="true">&rarr;</span>
            </div>
          </button>

          <button
            onClick={() => setSection('booking')}
            className="w-full ui-card ui-card-solid p-6 text-left transition-shadow hover:shadow-lg"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold tracking-tight text-slate-900">Booking Configuration</h2>
                <p className="ui-muted mt-1">
                  Choose your booking platform and configure how the AI schedules appointments
                </p>
              </div>
              <span className="text-slate-400 text-xl" aria-hidden="true">&rarr;</span>
            </div>
          </button>

          <button
            onClick={() => setSection('payment')}
            className="w-full ui-card ui-card-solid p-6 text-left transition-shadow hover:shadow-lg"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold tracking-tight text-slate-900">Payment Settings</h2>
                <p className="ui-muted mt-1">
                  Connect your Stripe account to collect deposits from patients
                </p>
              </div>
              <span className="text-slate-400 text-xl" aria-hidden="true">&rarr;</span>
            </div>
          </button>
        </div>

        <div className="mt-8">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back
          </button>
        </div>
      </div>
    </div>
  );
}
