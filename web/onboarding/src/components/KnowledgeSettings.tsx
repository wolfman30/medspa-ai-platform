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
    <div className="ui-page">
      <div className="ui-container max-w-3xl">
        <div className="mb-6 flex items-center gap-4">
          <button
            onClick={onBack}
            className="ui-link font-semibold flex items-center gap-2"
          >
            <span aria-hidden="true">&larr;</span> Back
          </button>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Clinic Knowledge</h1>
        </div>

        <div className="ui-card ui-card-solid p-6 space-y-4">
          <p className="ui-muted">
            Edit the knowledge the AI uses for this clinic. Do not include any patient-specific information (PHI).
          </p>
          {!loading && (
            <div className="flex flex-wrap gap-3 pb-3 border-b border-slate-200/70">
              <button
                onClick={() => { setEditing(true); setSuccess(null); }}
                className="ui-btn ui-btn-ghost"
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
                className="ui-btn ui-btn-ghost"
                disabled={saving}
              >
                Add Section
              </button>
              <button
                onClick={handleSave}
                className="ui-btn ui-btn-primary"
                disabled={!editing || saving}
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
              <span className="ml-auto text-sm text-slate-500 self-center">
                {documents.length} section{documents.length !== 1 ? 's' : ''}
              </span>
            </div>
          )}
          {loading ? (
            <div className="ui-muted">Loading knowledge...</div>
          ) : (
            <>
              {documents.length === 0 ? (
                <div className="border border-dashed border-slate-200 rounded-2xl p-6 text-sm text-slate-600 bg-slate-50/60">
                  No knowledge sections yet. Click “Add Section” to start.
                </div>
              ) : (
                <div className="space-y-4">
                  {documents.map((doc, index) => (
                    <div key={`${index}`} className="border border-slate-200/80 rounded-2xl p-5 bg-slate-50/60">
                      <div className="flex items-center justify-between mb-3">
                        <span className="text-sm font-semibold text-slate-700">
                          Section {index + 1}
                        </span>
                        {editing && (
                          <button
                            type="button"
                            onClick={() => {
                              setDocuments((prev) => prev.filter((_, i) => i !== index));
                            }}
                            className="text-xs font-semibold text-red-700 hover:text-red-800"
                          >
                            Remove
                          </button>
                        )}
                      </div>
                      <label className="ui-label mb-2">
                        Title (optional)
                      </label>
                      <input
                        className="ui-input"
                        value={doc.title}
                        onChange={(e) => {
                          const next = e.target.value;
                          setDocuments((prev) => prev.map((item, i) => (
                            i === index ? { ...item, title: next } : item
                          )));
                        }}
                        disabled={!editing}
                      />
                      <label className="ui-label mt-4 mb-2">
                        Details
                      </label>
                      <textarea
                        className="ui-textarea h-32"
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
                  className="ui-btn ui-btn-ghost"
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
                  className="ui-btn ui-btn-ghost"
                  disabled={saving}
                >
                  Add Section
                </button>
                <button
                  onClick={handleSave}
                  className="ui-btn ui-btn-primary"
                  disabled={!editing || saving}
                >
                  {saving ? 'Saving...' : 'Save'}
                </button>
              </div>
              {error && (
                <div className="text-sm font-medium text-red-700">{error}</div>
              )}
              {success && (
                <div className="text-sm font-medium text-emerald-700">{success}</div>
              )}
            </>
          )}
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
