import { useEffect, useState } from 'react';
import { getStructuredKnowledge, updateStructuredKnowledge } from '../api/client';
import type { ApiScope } from '../api/client';
import type { StructuredKnowledge, KnowledgeSections, PolicySection } from '../types/knowledge';
import { ServicesTab } from './knowledge/ServicesTab';
import { ProvidersTab } from './knowledge/ProvidersTab';
import { PoliciesTab } from './knowledge/PoliciesTab';
import { CustomTab } from './knowledge/CustomTab';
import { SyncMoxieButton } from './knowledge/SyncMoxieButton';

interface KnowledgeSettingsProps {
  orgId: string;
  scope: 'admin' | 'portal';
  onBack: () => void;
}

type Tab = 'services' | 'providers' | 'policies' | 'custom';

const TAB_LABELS: Record<Tab, string> = {
  services: 'Services',
  providers: 'Providers',
  policies: 'Policies',
  custom: 'Custom',
};

function emptyPolicies(): PolicySection {
  return { cancellation: '', deposit: '', age_requirement: '', terms_url: '', booking_policies: [], custom: [] };
}

function emptySections(): KnowledgeSections {
  return { services: { items: [] }, providers: { items: [] }, policies: emptyPolicies(), custom: [] };
}

export function KnowledgeSettings({ orgId, scope, onBack }: KnowledgeSettingsProps) {
  const [data, setData] = useState<StructuredKnowledge | null>(null);
  const [sections, setSections] = useState<KnowledgeSections>(emptySections());
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>('services');
  const [showPreview, setShowPreview] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    getStructuredKnowledge(orgId, scope)
      .then((result) => {
        if (!active) return;
        if (result) {
          setData(result);
          setSections(result.sections);
        } else {
          setData(null);
          setSections(emptySections());
        }
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load knowledge');
      })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [orgId, scope]);

  const validate = (): string | null => {
    if (sections.services.items.length === 0) return 'Add at least one service.';
    for (const s of sections.services.items) {
      if (!s.name.trim()) return 'Every service must have a name.';
    }
    if (!sections.policies.cancellation.trim()) return 'Cancellation policy is required.';
    if (!sections.policies.deposit.trim()) return 'Deposit policy is required.';
    return null;
  };

  const handleSave = async () => {
    const validationError = validate();
    if (validationError) { setError(validationError); return; }
    setError(null);
    setSuccess(null);
    setSaving(true);
    try {
      const payload: StructuredKnowledge = {
        org_id: data?.org_id || orgId,
        version: (data?.version || 0) + 1,
        sections,
        updated_at: new Date().toISOString(),
      };
      await updateStructuredKnowledge(orgId, scope, payload);
      setData(payload);
      setEditing(false);
      setSuccess('Knowledge saved successfully.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save knowledge');
    } finally {
      setSaving(false);
    }
  };

  const handleMoxieSync = (synced: StructuredKnowledge) => {
    setSections((prev) => ({
      ...prev,
      services: { items: synced.sections?.services?.items || [] },
      providers: { items: synced.sections?.providers?.items || [] },
    }));
    setEditing(true);
    setSuccess(null);
  };

  const buildPreviewText = (): string => {
    const lines: string[] = [];
    lines.push('=== SERVICES ===');
    for (const s of sections.services.items) {
      lines.push(`${s.name} (${s.category}) — ${s.price} — ${s.duration_minutes}min`);
      if (s.description) lines.push(`  ${s.description}`);
      if (s.aliases.length) lines.push(`  Also known as: ${s.aliases.join(', ')}`);
    }
    lines.push('');
    lines.push('=== PROVIDERS ===');
    for (const p of sections.providers.items) {
      lines.push(`${p.name}${p.title ? `, ${p.title}` : ''}`);
      if (p.bio) lines.push(`  ${p.bio}`);
    }
    lines.push('');
    lines.push('=== POLICIES ===');
    if (sections.policies.cancellation) lines.push(`Cancellation: ${sections.policies.cancellation}`);
    if (sections.policies.deposit) lines.push(`Deposit: ${sections.policies.deposit}`);
    if (sections.policies.age_requirement) lines.push(`Age: ${sections.policies.age_requirement}`);
    for (const bp of sections.policies.booking_policies) lines.push(`• ${bp}`);
    if ((sections.custom || []).length > 0) {
      lines.push('');
      lines.push('=== CUSTOM ===');
      for (const c of sections.custom || []) {
        lines.push(`[${c.title}] ${c.content}`);
      }
    }
    return lines.join('\n');
  };

  return (
    <div className="ui-page">
      <div className="ui-container max-w-4xl">
        <div className="mb-6 flex items-center gap-4">
          <button onClick={onBack} className="ui-link font-semibold flex items-center gap-2">
            <span aria-hidden="true">&larr;</span> Back
          </button>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">Clinic Knowledge</h1>
        </div>

        <div className="ui-card ui-card-solid p-6 space-y-4">
          <p className="ui-muted">
            Manage the structured knowledge your AI uses — services, providers, policies, and custom info.
          </p>

          {/* Toolbar */}
          {!loading && (
            <div className="flex flex-wrap gap-3 pb-3 border-b border-slate-200/70">
              <button onClick={() => { setEditing(true); setSuccess(null); }} className="ui-btn ui-btn-ghost" disabled={editing}>Edit</button>
              <button onClick={handleSave} className="ui-btn ui-btn-primary" disabled={!editing || saving}>
                {saving ? 'Saving...' : 'Save'}
              </button>
              {editing && (
                <button onClick={() => { setSections(data?.sections || emptySections()); setEditing(false); setError(null); }} className="ui-btn ui-btn-ghost">Cancel</button>
              )}
              <div className="ml-auto">
                <SyncMoxieButton orgId={orgId} scope={scope as ApiScope} onSync={handleMoxieSync} />
              </div>
            </div>
          )}

          {/* Tabs */}
          {!loading && (
            <div className="flex gap-1 border-b border-slate-200/70">
              {(Object.keys(TAB_LABELS) as Tab[]).map((tab) => (
                <button
                  key={tab}
                  onClick={() => setActiveTab(tab)}
                  className={`px-4 py-2 text-sm font-medium transition-colors border-b-2 -mb-px ${
                    activeTab === tab
                      ? 'border-slate-900 text-slate-900'
                      : 'border-transparent text-slate-500 hover:text-slate-700'
                  }`}
                >
                  {TAB_LABELS[tab]}
                </button>
              ))}
            </div>
          )}

          {/* Content */}
          {loading ? (
            <div className="ui-muted py-8 text-center">Loading knowledge...</div>
          ) : (
            <div className="mt-4">
              {activeTab === 'services' && (
                <ServicesTab
                  services={sections.services.items}
                  providers={sections.providers.items}
                  editing={editing}
                  onChange={(items) => setSections((prev) => ({ ...prev, services: { items } }))}
                />
              )}
              {activeTab === 'providers' && (
                <ProvidersTab
                  providers={sections.providers.items}
                  editing={editing}
                  onChange={(items) => setSections((prev) => ({ ...prev, providers: { items } }))}
                />
              )}
              {activeTab === 'policies' && (
                <PoliciesTab
                  policies={sections.policies}
                  editing={editing}
                  onChange={(policies) => setSections((prev) => ({ ...prev, policies }))}
                />
              )}
              {activeTab === 'custom' && (
                <CustomTab
                  documents={sections.custom || []}
                  editing={editing}
                  onChange={(custom) => setSections((prev) => ({ ...prev, custom }))}
                />
              )}
            </div>
          )}

          {/* Messages */}
          {error && <div className="text-sm font-medium text-red-700">{error}</div>}
          {success && <div className="text-sm font-medium text-emerald-700">{success}</div>}

          {/* AI Preview */}
          {!loading && (
            <div className="border-t border-slate-200/70 pt-4">
              <button
                onClick={() => setShowPreview(!showPreview)}
                className="text-sm font-medium text-slate-500 hover:text-slate-700 flex items-center gap-1"
              >
                {showPreview ? '▼' : '▶'} AI Preview — What the AI sees
              </button>
              {showPreview && (
                <pre className="mt-3 p-4 bg-slate-50 rounded-xl text-xs text-slate-600 overflow-auto max-h-96 whitespace-pre-wrap font-mono">
                  {buildPreviewText()}
                </pre>
              )}
            </div>
          )}
        </div>

        <div className="mt-8">
          <button onClick={onBack} className="ui-link font-semibold flex items-center gap-2">
            <span aria-hidden="true">&larr;</span> Back
          </button>
        </div>
      </div>
    </div>
  );
}
