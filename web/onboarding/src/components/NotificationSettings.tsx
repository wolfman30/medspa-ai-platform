import { useEffect, useState } from 'react';
import { getNotificationSettings, updateNotificationSettings } from '../api/client';
import type { NotificationSettings as NotificationSettingsType } from '../api/client';

interface NotificationSettingsProps {
  orgId: string;
  onBack: () => void;
}

function formatPhone(phone: string): string {
  const digits = phone.replace(/\D/g, '');
  if (digits.length === 10) {
    return `(${digits.slice(0, 3)}) ${digits.slice(3, 6)}-${digits.slice(6)}`;
  }
  if (digits.length === 11 && digits.startsWith('1')) {
    return `+1 (${digits.slice(1, 4)}) ${digits.slice(4, 7)}-${digits.slice(7)}`;
  }
  return phone;
}

function normalizePhone(phone: string): string {
  const digits = phone.replace(/\D/g, '');
  if (digits.length === 10) {
    return '+1' + digits;
  }
  if (digits.length === 11 && digits.startsWith('1')) {
    return '+' + digits;
  }
  return phone;
}

export function NotificationSettings({ orgId, onBack }: NotificationSettingsProps) {
  const [settings, setSettings] = useState<NotificationSettingsType | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Input states
  const [newSmsRecipient, setNewSmsRecipient] = useState('');
  const [newEmailRecipient, setNewEmailRecipient] = useState('');

  useEffect(() => {
    loadSettings();
  }, [orgId]);

  const loadSettings = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getNotificationSettings(orgId);
      setSettings(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load settings');
    } finally {
      setLoading(false);
    }
  };

  const saveSettings = async (updates: Partial<NotificationSettingsType>) => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const data = await updateNotificationSettings(orgId, updates);
      setSettings(data);
      setSuccess('Settings saved successfully');
      setTimeout(() => setSuccess(null), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = (field: keyof NotificationSettingsType) => {
    if (!settings) return;
    const newValue = !settings[field];
    setSettings({ ...settings, [field]: newValue });
    saveSettings({ [field]: newValue });
  };

  const addSmsRecipient = () => {
    if (!settings || !newSmsRecipient.trim()) return;
    const normalized = normalizePhone(newSmsRecipient.trim());
    if (settings.sms_recipients.includes(normalized)) {
      setError('This phone number is already added');
      return;
    }
    const updated = [...settings.sms_recipients, normalized];
    setSettings({ ...settings, sms_recipients: updated });
    saveSettings({ sms_recipients: updated });
    setNewSmsRecipient('');
  };

  const removeSmsRecipient = (phone: string) => {
    if (!settings) return;
    const updated = settings.sms_recipients.filter((p) => p !== phone);
    setSettings({ ...settings, sms_recipients: updated });
    saveSettings({ sms_recipients: updated });
  };

  const addEmailRecipient = () => {
    if (!settings || !newEmailRecipient.trim()) return;
    const email = newEmailRecipient.trim().toLowerCase();
    if (settings.email_recipients.includes(email)) {
      setError('This email is already added');
      return;
    }
    const updated = [...settings.email_recipients, email];
    setSettings({ ...settings, email_recipients: updated });
    saveSettings({ email_recipients: updated });
    setNewEmailRecipient('');
  };

  const removeEmailRecipient = (email: string) => {
    if (!settings) return;
    const updated = settings.email_recipients.filter((e) => e !== email);
    setSettings({ ...settings, email_recipients: updated });
    saveSettings({ email_recipients: updated });
  };

  if (loading) {
    return (
      <div className="ui-page flex items-center justify-center">
        <div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  if (!settings) {
    return (
      <div className="ui-page">
        <div className="ui-container max-w-2xl space-y-4">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back
          </button>
          <div className="p-4 bg-red-50 border border-red-200 rounded-xl text-red-800">
            {error || 'Failed to load settings'}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="ui-page">
      <div className="ui-container max-w-2xl">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="ui-link font-semibold flex items-center gap-2"
            >
              <span aria-hidden="true">&larr;</span> Back
            </button>
            <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Notification Settings</h1>
          </div>
          {saving && (
            <span className="text-sm text-slate-500">Saving...</span>
          )}
        </div>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-xl text-red-800">
            {error}
          </div>
        )}

        {success && (
          <div className="mb-4 p-4 bg-emerald-50 border border-emerald-200 rounded-xl text-emerald-800">
            {success}
          </div>
        )}

        {/* Event Toggles */}
        <div className="ui-card ui-card-solid p-6 mb-6">
          <h2 className="text-lg font-semibold tracking-tight text-slate-900 mb-4">Notification Events</h2>
          <div className="space-y-4">
            <label className="flex items-center justify-between">
              <div>
                <span className="font-semibold text-slate-800">Payment Received</span>
                <p className="ui-muted mt-1">Get notified when a patient pays their deposit</p>
              </div>
              <button
                onClick={() => handleToggle('notify_on_payment')}
                className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out ${
                  settings.notify_on_payment ? 'bg-gradient-to-r from-violet-600 to-indigo-600' : 'bg-slate-200'
                }`}
              >
                <span
                  className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                    settings.notify_on_payment ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </label>

            <label className="flex items-center justify-between">
              <div>
                <span className="font-semibold text-slate-800">New Lead</span>
                <p className="ui-muted mt-1">Get notified when a new lead comes in</p>
              </div>
              <button
                onClick={() => handleToggle('notify_on_new_lead')}
                className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out ${
                  settings.notify_on_new_lead ? 'bg-gradient-to-r from-violet-600 to-indigo-600' : 'bg-slate-200'
                }`}
              >
                <span
                  className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                    settings.notify_on_new_lead ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </label>
          </div>
        </div>

        {/* SMS Settings */}
        <div className="ui-card ui-card-solid p-6 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold tracking-tight text-slate-900">SMS Notifications</h2>
            <button
              onClick={() => handleToggle('sms_enabled')}
              className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out ${
                settings.sms_enabled ? 'bg-gradient-to-r from-violet-600 to-indigo-600' : 'bg-slate-200'
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                  settings.sms_enabled ? 'translate-x-5' : 'translate-x-0'
                }`}
              />
            </button>
          </div>

          {settings.sms_enabled && (
            <>
              <p className="ui-muted mb-4">
                Add phone numbers to receive SMS notifications
              </p>

              {/* Add phone input */}
              <div className="flex gap-2 mb-4">
                <input
                  type="tel"
                  placeholder="(555) 123-4567"
                  value={newSmsRecipient}
                  onChange={(e) => setNewSmsRecipient(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && addSmsRecipient()}
                  className="ui-input flex-1"
                />
                <button
                  onClick={addSmsRecipient}
                  disabled={!newSmsRecipient.trim()}
                  className="ui-btn ui-btn-primary px-4"
                >
                  Add
                </button>
              </div>

              {/* Phone list */}
              <div className="space-y-2">
                {settings.sms_recipients.length === 0 ? (
                  <p className="text-sm text-slate-500 italic">No SMS recipients configured</p>
                ) : (
                  settings.sms_recipients.map((phone) => (
                    <div
                      key={phone}
                      className="flex items-center justify-between gap-3 py-2 px-3 rounded-xl border border-slate-200/70 bg-white/70"
                    >
                      <span className="text-sm font-semibold text-slate-700">
                        {formatPhone(phone)}
                      </span>
                      <button
                        onClick={() => removeSmsRecipient(phone)}
                        className="text-red-700 hover:text-red-800 text-sm font-semibold"
                      >
                        Remove
                      </button>
                    </div>
                  ))
                )}
              </div>
            </>
          )}
        </div>

        {/* Email Settings */}
        <div className="ui-card ui-card-solid p-6 mb-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold tracking-tight text-slate-900">Email Notifications</h2>
            <button
              onClick={() => handleToggle('email_enabled')}
              className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out ${
                settings.email_enabled ? 'bg-gradient-to-r from-violet-600 to-indigo-600' : 'bg-slate-200'
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                  settings.email_enabled ? 'translate-x-5' : 'translate-x-0'
                }`}
              />
            </button>
          </div>

          {settings.email_enabled && (
            <>
              <p className="ui-muted mb-4">
                Add email addresses to receive email notifications
              </p>

              {/* Add email input */}
              <div className="flex gap-2 mb-4">
                <input
                  type="email"
                  placeholder="email@example.com"
                  value={newEmailRecipient}
                  onChange={(e) => setNewEmailRecipient(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && addEmailRecipient()}
                  className="ui-input flex-1"
                />
                <button
                  onClick={addEmailRecipient}
                  disabled={!newEmailRecipient.trim()}
                  className="ui-btn ui-btn-primary px-4"
                >
                  Add
                </button>
              </div>

              {/* Email list */}
              <div className="space-y-2">
                {settings.email_recipients.length === 0 ? (
                  <p className="text-sm text-slate-500 italic">No email recipients configured</p>
                ) : (
                  settings.email_recipients.map((email) => (
                    <div
                      key={email}
                      className="flex items-center justify-between gap-3 py-2 px-3 rounded-xl border border-slate-200/70 bg-white/70"
                    >
                      <span className="text-sm font-semibold text-slate-700">{email}</span>
                      <button
                        onClick={() => removeEmailRecipient(email)}
                        className="text-red-700 hover:text-red-800 text-sm font-semibold"
                      >
                        Remove
                      </button>
                    </div>
                  ))
                )}
              </div>
            </>
          )}
        </div>

        <p className="text-xs text-slate-400 text-center">
          Changes are saved automatically
        </p>

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
