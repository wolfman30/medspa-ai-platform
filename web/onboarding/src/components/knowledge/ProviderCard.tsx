import { useState } from 'react';
import type { ProviderItem } from '../../types/knowledge';

interface ProviderCardProps {
  provider: ProviderItem;
  editing: boolean;
  onChange: (updated: ProviderItem) => void;
  onDelete: () => void;
}

export function ProviderCard({ provider, editing, onChange, onDelete }: ProviderCardProps) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [specialtyInput, setSpecialtyInput] = useState('');

  const update = (patch: Partial<ProviderItem>) => onChange({ ...provider, ...patch });

  const addSpecialty = () => {
    const trimmed = specialtyInput.trim();
    if (trimmed && !(provider.specialties || []).includes(trimmed)) {
      update({ specialties: [...(provider.specialties || []), trimmed] });
    }
    setSpecialtyInput('');
  };

  const removeSpecialty = (s: string) => {
    update({ specialties: (provider.specialties || []).filter((x) => x !== s) });
  };

  return (
    <div className="border border-slate-200/80 rounded-2xl p-5 bg-slate-50/60">
      <div className="flex items-start justify-between gap-3">
        <div className="flex-1 space-y-3">
          <div>
            <label className="ui-label mb-1">Name</label>
            {editing ? (
              <input className="ui-input" value={provider.name} onChange={(e) => update({ name: e.target.value })} placeholder="Provider name" />
            ) : (
              <p className="text-lg font-semibold text-slate-900">{provider.name || 'Unnamed'}</p>
            )}
          </div>
          <div>
            <label className="ui-label mb-1">Title / Credentials</label>
            {editing ? (
              <input className="ui-input" value={provider.title || ''} onChange={(e) => update({ title: e.target.value })} placeholder="e.g., Nurse Practitioner" />
            ) : (
              <p className="text-sm text-slate-600">{provider.title || '—'}</p>
            )}
          </div>
          <div>
            <label className="ui-label mb-1">Bio</label>
            {editing ? (
              <textarea className="ui-textarea h-20" value={provider.bio || ''} onChange={(e) => update({ bio: e.target.value })} placeholder="Short bio..." />
            ) : (
              <p className="text-sm text-slate-700 whitespace-pre-wrap">{provider.bio || '—'}</p>
            )}
          </div>
          <div>
            <label className="ui-label mb-1">Specialties</label>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {(provider.specialties || []).map((s) => (
                <span key={s} className="inline-flex items-center gap-1 px-2 py-0.5 bg-slate-200/70 rounded-full text-xs">
                  {s}
                  {editing && <button onClick={() => removeSpecialty(s)} className="text-slate-400 hover:text-red-600">×</button>}
                </span>
              ))}
            </div>
            {editing && (
              <div className="flex gap-2">
                <input
                  className="ui-input flex-1"
                  value={specialtyInput}
                  onChange={(e) => setSpecialtyInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addSpecialty(); } }}
                  placeholder="Add specialty"
                />
                <button type="button" onClick={addSpecialty} className="ui-btn ui-btn-ghost text-sm">Add</button>
              </div>
            )}
          </div>
        </div>
        {editing && (
          <div className="shrink-0">
            {confirmDelete ? (
              <div className="flex flex-col items-end gap-1">
                <button onClick={onDelete} className="text-xs font-semibold text-red-700 hover:text-red-800">Confirm</button>
                <button onClick={() => setConfirmDelete(false)} className="text-xs text-slate-500">Cancel</button>
              </div>
            ) : (
              <button onClick={() => setConfirmDelete(true)} className="p-1 text-slate-400 hover:text-red-600" title="Delete">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
