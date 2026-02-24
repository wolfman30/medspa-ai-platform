import { useEffect, useState } from 'react';
import { getClinicConfig, updateClinicConfig } from '../api/client';

interface BookingSettingsProps {
  orgId: string;
  onBack: () => void;
}

const PLATFORM_OPTIONS = [
  {
    value: 'square',
    label: 'Square (Deposit)',
    description: 'Collect a deposit via Square payment link before confirming the appointment',
  },
  {
    value: 'moxie',
    label: 'Moxie',
    description: 'Book directly through your Moxie booking page with real-time availability',
  },
];

export function BookingSettings({ orgId, onBack }: BookingSettingsProps) {
  const [platform, setPlatform] = useState('square');
  const [bookingUrl, setBookingUrl] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  useEffect(() => {
    loadConfig();
  }, [orgId]);

  const loadConfig = async () => {
    setLoading(true);
    setError(null);
    try {
      const config = await getClinicConfig(orgId);
      setPlatform(config.booking_platform || 'square');
      setBookingUrl(config.booking_url || '');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load booking settings');
    } finally {
      setLoading(false);
    }
  };

  const saveConfig = async (updates: Record<string, string>) => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await updateClinicConfig(orgId, updates);
      setSuccess('Booking settings saved');
      setTimeout(() => setSuccess(null), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save booking settings');
    } finally {
      setSaving(false);
    }
  };

  const handlePlatformChange = (value: string) => {
    setPlatform(value);
    saveConfig({ booking_platform: value, booking_url: bookingUrl });
  };

  const handleUrlBlur = () => {
    saveConfig({ booking_platform: platform, booking_url: bookingUrl });
  };

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
        <div className="mb-6 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="ui-link font-semibold flex items-center gap-2"
            >
              <span aria-hidden="true">&larr;</span> Back
            </button>
            <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Booking Configuration</h1>
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

        {/* Booking Platform */}
        <div className="ui-card ui-card-solid p-6 mb-6">
          <h2 className="text-lg font-semibold tracking-tight text-slate-900 mb-2">Booking Platform</h2>
          <p className="ui-muted mb-4">
            Choose how the AI books appointments for your clinic
          </p>
          <div className="space-y-3">
            {PLATFORM_OPTIONS.map((option) => (
              <label
                key={option.value}
                className={`flex items-start p-4 rounded-2xl border cursor-pointer transition-colors ${
                  platform === option.value
                    ? 'border-violet-300 bg-violet-50/50'
                    : 'border-slate-200/80 bg-white/60 hover:border-slate-300'
                }`}
              >
                <input
                  type="radio"
                  name="booking_platform"
                  value={option.value}
                  checked={platform === option.value}
                  onChange={() => handlePlatformChange(option.value)}
                  className="mt-0.5 h-4 w-4 text-violet-600 focus:ring-violet-500 border-slate-300"
                />
                <div className="ml-3">
                  <span className="block font-semibold text-slate-900">{option.label}</span>
                  <span className="block text-sm text-slate-500">{option.description}</span>
                </div>
              </label>
            ))}
          </div>
        </div>

        {/* Booking URL (shown for Moxie) */}
        {platform === 'moxie' && (
          <div className="ui-card ui-card-solid p-6 mb-6">
            <h2 className="text-lg font-semibold tracking-tight text-slate-900 mb-2">Moxie Booking URL</h2>
            <p className="ui-muted mb-4">
              Your Moxie booking page URL. The AI will check this page for real-time availability.
            </p>
            <input
              type="url"
              placeholder="https://app.joinmoxie.com/booking/your-clinic"
              value={bookingUrl}
              onChange={(e) => setBookingUrl(e.target.value)}
              onBlur={handleUrlBlur}
              className="ui-input"
            />
            <p className="ui-help mt-2">
              Usually looks like: https://app.joinmoxie.com/booking/your-clinic-name
            </p>
          </div>
        )}

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
