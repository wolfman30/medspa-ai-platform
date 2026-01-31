import { useState } from 'react';
import { NotificationSettings } from './NotificationSettings';
import { AIPersonaSettings } from './AIPersonaSettings';
import { KnowledgeSettings } from './KnowledgeSettings';

interface SettingsPageProps {
  orgId: string;
  scope: 'admin' | 'portal';
  onBack: () => void;
}

type SettingsSection = 'menu' | 'notifications' | 'ai-persona' | 'knowledge';

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

  return (
    <div className="min-h-screen bg-gray-50 py-10">
      <div className="max-w-2xl mx-auto px-4">
        {/* Header */}
        <div className="mb-6 flex items-center gap-4">
          <button
            onClick={onBack}
            className="text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
          >
            <span>&larr;</span> Back
          </button>
          <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
        </div>

        {/* Settings Menu */}
        <div className="space-y-4">
          <button
            onClick={() => setSection('notifications')}
            className="w-full bg-white rounded-lg shadow p-6 text-left hover:shadow-md transition-shadow"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-medium text-gray-900">Notification Settings</h2>
                <p className="text-sm text-gray-500 mt-1">
                  Configure how and when you receive notifications about payments and leads
                </p>
              </div>
              <span className="text-indigo-600 text-xl">&rarr;</span>
            </div>
          </button>

          <button
            onClick={() => setSection('ai-persona')}
            className="w-full bg-white rounded-lg shadow p-6 text-left hover:shadow-md transition-shadow"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-medium text-gray-900">AI Assistant Persona</h2>
                <p className="text-sm text-gray-500 mt-1">
                  Customize how your AI assistant communicates with patients
                </p>
              </div>
              <span className="text-indigo-600 text-xl">&rarr;</span>
            </div>
          </button>

          <button
            onClick={() => setSection('knowledge')}
            className="w-full bg-white rounded-lg shadow p-6 text-left hover:shadow-md transition-shadow"
          >
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-medium text-gray-900">Clinic Knowledge</h2>
                <p className="text-sm text-gray-500 mt-1">
                  Review and update the knowledge the AI uses for your clinic
                </p>
              </div>
              <span className="text-indigo-600 text-xl">&rarr;</span>
            </div>
          </button>
        </div>

        <div className="mt-8">
          <button
            onClick={onBack}
            className="text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
          >
            <span>&larr;</span> Back
          </button>
        </div>
      </div>
    </div>
  );
}
