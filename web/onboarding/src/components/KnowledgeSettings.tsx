import { useEffect, useState } from 'react';
import {
  getAdminKnowledge,
  getPortalKnowledge,
  updateAdminKnowledge,
  updatePortalKnowledge,
} from '../api/client';

interface KnowledgeSettingsProps {
  orgId: string;
  scope: 'admin' | 'portal';
  onBack: () => void;
}

type KnowledgeEntry = {
  title: string;
  content: string;
};

function normalizeDocuments(raw: unknown[]): KnowledgeEntry[] {
  return raw.map((doc) => {
    if (doc && typeof doc === 'object') {
      const record = doc as { title?: string; content?: string };
      return {
        title: (record.title || '').trim(),
        content: (record.content || '').trim(),
      };
    }
    if (typeof doc === 'string') {
      const trimmed = doc.trim();
      const splitIndex = trimmed.indexOf('\n\n');
      if (splitIndex > 0) {
        const title = trimmed.slice(0, splitIndex).trim();
        const content = trimmed.slice(splitIndex + 2).trim();
        return { title, content };
      }
      return { title: '', content: trimmed };
    }
    return { title: '', content: '' };
  }).filter((doc) => doc.title !== '' || doc.content !== '');
}

export function KnowledgeSettings({ orgId, scope, onBack }: KnowledgeSettingsProps) {
  const [documents, setDocuments] = useState<KnowledgeEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    const loader = scope === 'admin' ? getAdminKnowledge : getPortalKnowledge;
    loader(orgId)
      .then((data) => {
        if (!active) return;
        const docs = Array.isArray(data.documents) ? data.documents : [];
        setDocuments(normalizeDocuments(docs));
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
  }, [orgId, scope]);

  const handleSave = async () => {
    setError(null);
    setSuccess(null);
    const payload = documents
      .map((doc) => ({
        title: doc.title.trim(),
        content: doc.content.trim(),
      }))
      .filter((doc) => doc.title !== '' || doc.content !== '');

    if (payload.length === 0) {
      setError('Add at least one knowledge section before saving.');
      return;
    }
    setSaving(true);
    try {
      const saver = scope === 'admin' ? updateAdminKnowledge : updatePortalKnowledge;
      await saver(orgId, payload);
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
          {!loading && (
            <div className="flex flex-wrap gap-3 pb-2 border-b border-gray-200">
              <button
                onClick={() => { setEditing(true); setSuccess(null); }}
                className="px-4 py-2 rounded-md border border-indigo-500 text-indigo-600 hover:bg-indigo-50 disabled:opacity-50"
                disabled={editing}
              >
                Edit
              </button>
              <button
                type="button"
                onClick={() => {
                  setDocuments((prev) => [...prev, { title: '', content: '' }]);
                  setEditing(true);
                }}
                className="px-4 py-2 rounded-md border border-gray-300 text-gray-700 hover:bg-gray-100 disabled:opacity-50"
                disabled={saving}
              >
                Add Section
              </button>
              <button
                onClick={handleSave}
                className="px-4 py-2 rounded-md bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50"
                disabled={!editing || saving}
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
              <span className="ml-auto text-sm text-gray-500 self-center">
                {documents.length} section{documents.length !== 1 ? 's' : ''}
              </span>
            </div>
          )}
          {loading ? (
            <div className="text-sm text-gray-500">Loading knowledge...</div>
          ) : (
            <>
              {documents.length === 0 ? (
                <div className="border border-dashed border-gray-200 rounded-lg p-6 text-sm text-gray-500 bg-gray-50">
                  No knowledge sections yet. Click “Add Section” to start.
                </div>
              ) : (
                <div className="space-y-4">
                  {documents.map((doc, index) => (
                    <div key={`${index}`} className="border border-gray-200 rounded-lg p-4 bg-gray-50">
                      <div className="flex items-center justify-between mb-3">
                        <span className="text-sm font-semibold text-gray-700">
                          Section {index + 1}
                        </span>
                        {editing && (
                          <button
                            type="button"
                            onClick={() => {
                              setDocuments((prev) => prev.filter((_, i) => i !== index));
                            }}
                            className="text-xs text-red-600 hover:text-red-700"
                          >
                            Remove
                          </button>
                        )}
                      </div>
                      <label className="block text-xs font-medium text-gray-600 mb-1">
                        Title (optional)
                      </label>
                      <input
                        className="w-full border border-gray-200 rounded-md px-3 py-2 text-sm text-gray-800 bg-white disabled:bg-gray-100"
                        value={doc.title}
                        onChange={(e) => {
                          const next = e.target.value;
                          setDocuments((prev) => prev.map((item, i) => (
                            i === index ? { ...item, title: next } : item
                          )));
                        }}
                        disabled={!editing}
                      />
                      <label className="block text-xs font-medium text-gray-600 mt-3 mb-1">
                        Details
                      </label>
                      <textarea
                        className="w-full h-32 border border-gray-200 rounded-md p-3 text-sm text-gray-800 bg-white disabled:bg-gray-100"
                        value={doc.content}
                        onChange={(e) => {
                          const next = e.target.value;
                          setDocuments((prev) => prev.map((item, i) => (
                            i === index ? { ...item, content: next } : item
                          )));
                        }}
                        disabled={!editing}
                      />
                    </div>
                  ))}
                </div>
              )}
              <div className="flex flex-wrap gap-3">
                <button
                  onClick={() => { setEditing(true); setSuccess(null); }}
                  className="px-4 py-2 rounded-md border border-indigo-500 text-indigo-600 hover:bg-indigo-50 disabled:opacity-50"
                  disabled={editing}
                >
                  Edit
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setDocuments((prev) => [...prev, { title: '', content: '' }]);
                    setEditing(true);
                  }}
                  className="px-4 py-2 rounded-md border border-gray-300 text-gray-700 hover:bg-gray-100 disabled:opacity-50"
                  disabled={saving}
                >
                  Add Section
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
