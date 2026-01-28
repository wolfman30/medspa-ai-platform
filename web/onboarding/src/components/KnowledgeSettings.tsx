import { useEffect, useState } from 'react';
import { getPortalKnowledge, updatePortalKnowledge } from '../api/client';

interface KnowledgeSettingsProps {
  orgId: string;
  onBack: () => void;
}

export function KnowledgeSettings({ orgId, onBack }: KnowledgeSettingsProps) {
  const [value, setValue] = useState('[]');
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    getPortalKnowledge(orgId)
      .then((data) => {
        if (!active) return;
        const docs = Array.isArray(data.documents) ? data.documents : [];
        setValue(JSON.stringify(docs, null, 2));
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load knowledge');
      })
      .finally(() => {
        if (!active) return;
        setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [orgId]);

  const handleSave = async () => {
    setError(null);
    setSuccess(null);
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      setError('Invalid JSON. Please fix and try again.');
      return;
    }
    if (!Array.isArray(parsed)) {
      setError('Documents must be a JSON array.');
      return;
    }
    setSaving(true);
    try {
      await updatePortalKnowledge(orgId, parsed);
      setEditing(false);
      setSuccess('Knowledge saved.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save knowledge');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="min-h-screen bg-gray-50 py-10">
      <div className="max-w-3xl mx-auto px-4">
        <div className="mb-6 flex items-center gap-4">
          <button
            onClick={onBack}
            className="text-indigo-600 hover:text-indigo-800 flex items-center gap-1"
          >
            <span>&larr;</span> Back
          </button>
          <h1 className="text-2xl font-bold text-gray-900">Clinic Knowledge</h1>
        </div>

        <div className="bg-white rounded-lg shadow p-6 space-y-4">
          <p className="text-sm text-gray-600">
            Edit the knowledge the AI uses for this clinic. Do not include any patient-specific information (PHI).
          </p>
          {loading ? (
            <div className="text-sm text-gray-500">Loading knowledge...</div>
          ) : (
            <>
              <textarea
                className="w-full h-80 border border-gray-200 rounded-md p-3 font-mono text-xs text-gray-800 disabled:bg-gray-50"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                disabled={!editing}
              />
              <div className="flex flex-wrap gap-3">
                <button
                  onClick={() => { setEditing(true); setSuccess(null); }}
                  className="px-4 py-2 rounded-md border border-indigo-500 text-indigo-600 hover:bg-indigo-50 disabled:opacity-50"
                  disabled={editing}
                >
                  Edit
                </button>
                <button
                  onClick={handleSave}
                  className="px-4 py-2 rounded-md bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50"
                  disabled={!editing || saving}
                >
                  {saving ? 'Saving...' : 'Save'}
                </button>
              </div>
              {error && (
                <div className="text-sm text-red-600">{error}</div>
              )}
              {success && (
                <div className="text-sm text-green-600">{success}</div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
