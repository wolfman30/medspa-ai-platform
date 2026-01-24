import { useEffect, useState } from 'react';
import { getAIPersona, updateAIPersona } from '../api/client';
import type { AIPersona } from '../api/client';

interface AIPersonaSettingsProps {
  orgId: string;
  onBack: () => void;
}

const TONE_OPTIONS: { value: AIPersona['tone']; label: string; description: string }[] = [
  { value: 'warm', label: 'Warm', description: 'Friendly and approachable, makes patients feel comfortable' },
  { value: 'professional', label: 'Professional', description: 'Efficient and straightforward communication' },
  { value: 'clinical', label: 'Clinical', description: 'Medical accuracy and patient safety focused' },
];

export function AIPersonaSettings({ orgId, onBack }: AIPersonaSettingsProps) {
  const [persona, setPersona] = useState<AIPersona | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Input states
  const [newService, setNewService] = useState('');

  useEffect(() => {
    loadPersona();
  }, [orgId]);

  const loadPersona = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getAIPersona(orgId);
      setPersona(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load AI persona settings');
    } finally {
      setLoading(false);
    }
  };

  const savePersona = async (updates: Partial<AIPersona>) => {
    if (!persona) return;
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const updated = { ...persona, ...updates };
      const data = await updateAIPersona(orgId, updated);
      setPersona(data);
      setSuccess('Settings saved successfully');
      setTimeout(() => setSuccess(null), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  const handleTextChange = (field: keyof AIPersona, value: string) => {
    if (!persona) return;
    setPersona({ ...persona, [field]: value });
  };

  const handleTextBlur = (field: keyof AIPersona) => {
    if (!persona) return;
    savePersona({ [field]: persona[field] });
  };

  const handleToggle = (field: keyof AIPersona) => {
    if (!persona) return;
    const newValue = !persona[field];
    setPersona({ ...persona, [field]: newValue });
    savePersona({ [field]: newValue });
  };

  const handleToneChange = (tone: AIPersona['tone']) => {
    if (!persona) return;
    setPersona({ ...persona, tone });
    savePersona({ tone });
  };

  const addSpecialService = () => {
    if (!persona || !newService.trim()) return;
    const services = persona.special_services || [];
    if (services.includes(newService.trim())) {
      setError('This service is already added');
      return;
    }
    const updated = [...services, newService.trim()];
    setPersona({ ...persona, special_services: updated });
    savePersona({ special_services: updated });
    setNewService('');
  };

  const removeSpecialService = (service: string) => {
    if (!persona) return;
    const updated = (persona.special_services || []).filter((s) => s !== service);
    setPersona({ ...persona, special_services: updated });
    savePersona({ special_services: updated });
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600" />
      </div>
    );
  }

  if (!persona) {
    return (
      <div className="min-h-screen bg-gray-50 py-10">
        <div className="max-w-2xl mx-auto px-4">
          <button
            onClick={onBack}
            className="mb-4 text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
          >
            <span>&larr;</span> Back
          </button>
          <div className="p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
            {error || 'Failed to load AI persona settings'}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50 py-10">
      <div className="max-w-2xl mx-auto px-4">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
            >
              <span>&larr;</span> Back
            </button>
            <h1 className="text-2xl font-bold text-gray-900">AI Assistant Persona</h1>
          </div>
          {saving && (
            <span className="text-sm text-gray-500">Saving...</span>
          )}
        </div>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
            {error}
          </div>
        )}

        {success && (
          <div className="mb-4 p-4 bg-green-50 border border-green-200 rounded-md text-green-700">
            {success}
          </div>
        )}

        {/* Provider Info */}
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <h2 className="text-lg font-medium text-gray-900 mb-4">Provider Information</h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Provider Name
              </label>
              <input
                type="text"
                placeholder="e.g., Brandi"
                value={persona.provider_name || ''}
                onChange={(e) => handleTextChange('provider_name', e.target.value)}
                onBlur={() => handleTextBlur('provider_name')}
                className="w-full px-4 py-2 border border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
              />
              <p className="mt-1 text-xs text-gray-500">
                The name of the primary provider (used when identifying the AI as their assistant)
              </p>
            </div>

            <label className="flex items-center justify-between pt-2">
              <div>
                <span className="font-medium text-gray-700">Solo Operator</span>
                <p className="text-sm text-gray-500">
                  Enable if this clinic is run by a single provider with no front desk staff
                </p>
              </div>
              <button
                onClick={() => handleToggle('is_solo_operator')}
                className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out ${
                  persona.is_solo_operator ? 'bg-indigo-600' : 'bg-gray-200'
                }`}
              >
                <span
                  className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                    persona.is_solo_operator ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </label>
          </div>
        </div>

        {/* Communication Tone */}
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <h2 className="text-lg font-medium text-gray-900 mb-4">Communication Tone</h2>
          <div className="space-y-3">
            {TONE_OPTIONS.map((option) => (
              <label
                key={option.value}
                className={`flex items-start p-3 rounded-lg border cursor-pointer transition-colors ${
                  persona.tone === option.value
                    ? 'border-indigo-600 bg-indigo-50'
                    : 'border-gray-200 hover:border-gray-300'
                }`}
              >
                <input
                  type="radio"
                  name="tone"
                  value={option.value}
                  checked={persona.tone === option.value}
                  onChange={() => handleToneChange(option.value)}
                  className="mt-0.5 h-4 w-4 text-indigo-600 focus:ring-indigo-500 border-gray-300"
                />
                <div className="ml-3">
                  <span className="block font-medium text-gray-900">{option.label}</span>
                  <span className="block text-sm text-gray-500">{option.description}</span>
                </div>
              </label>
            ))}
          </div>
        </div>

        {/* Custom Messages */}
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <h2 className="text-lg font-medium text-gray-900 mb-4">Custom Messages</h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Business Hours Greeting
              </label>
              <textarea
                rows={3}
                placeholder="Hi! This is Brandi's AI assistant at Forever 22 Med Spa. Brandi is currently with a patient..."
                value={persona.custom_greeting || ''}
                onChange={(e) => handleTextChange('custom_greeting', e.target.value)}
                onBlur={() => handleTextBlur('custom_greeting')}
                className="w-full px-4 py-2 border border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
              />
              <p className="mt-1 text-xs text-gray-500">
                Greeting used during business hours when the provider is working
              </p>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                After Hours Greeting
              </label>
              <textarea
                rows={3}
                placeholder="Hi! This is Brandi's AI assistant at Forever 22 Med Spa. Brandi is off for the evening..."
                value={persona.after_hours_greeting || ''}
                onChange={(e) => handleTextChange('after_hours_greeting', e.target.value)}
                onBlur={() => handleTextBlur('after_hours_greeting')}
                className="w-full px-4 py-2 border border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
              />
              <p className="mt-1 text-xs text-gray-500">
                Greeting used outside business hours (evenings, weekends, holidays)
              </p>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Busy Message
              </label>
              <textarea
                rows={2}
                placeholder="Brandi is currently with a patient and can't come to the phone right now."
                value={persona.busy_message || ''}
                onChange={(e) => handleTextChange('busy_message', e.target.value)}
                onBlur={() => handleTextBlur('busy_message')}
                className="w-full px-4 py-2 border border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
              />
              <p className="mt-1 text-xs text-gray-500">
                Explains why the provider can't answer (used during business hours)
              </p>
            </div>
          </div>
        </div>

        {/* Special Services */}
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <h2 className="text-lg font-medium text-gray-900 mb-2">Special Medical Services</h2>
          <p className="text-sm text-gray-500 mb-4">
            Non-cosmetic medical treatments that require special handling (e.g., hyperhidrosis, migraines)
          </p>

          {/* Add service input */}
          <div className="flex gap-2 mb-4">
            <input
              type="text"
              placeholder="e.g., Hyperhidrosis treatment"
              value={newService}
              onChange={(e) => setNewService(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && addSpecialService()}
              className="flex-1 px-4 py-2 border border-gray-300 rounded-md focus:ring-indigo-500 focus:border-indigo-500"
            />
            <button
              onClick={addSpecialService}
              disabled={!newService.trim()}
              className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50"
            >
              Add
            </button>
          </div>

          {/* Services list */}
          <div className="space-y-2">
            {(!persona.special_services || persona.special_services.length === 0) ? (
              <p className="text-sm text-gray-400 italic">No special services configured</p>
            ) : (
              persona.special_services.map((service) => (
                <div
                  key={service}
                  className="flex items-center justify-between py-2 px-3 bg-gray-50 rounded-md"
                >
                  <span className="text-sm font-medium text-gray-700">{service}</span>
                  <button
                    onClick={() => removeSpecialService(service)}
                    className="text-red-600 hover:text-red-800 text-sm"
                  >
                    Remove
                  </button>
                </div>
              ))
            )}
          </div>
        </div>

        <p className="text-xs text-gray-400 text-center">
          Changes are saved automatically
        </p>
      </div>
    </div>
  );
}
