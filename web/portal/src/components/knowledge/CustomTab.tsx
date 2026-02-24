import { useState } from 'react';
import type { CustomDoc } from '../../types/knowledge';

interface CustomTabProps {
  documents: CustomDoc[];
  editing: boolean;
  onChange: (docs: CustomDoc[]) => void;
}

export function CustomTab({ documents, editing, onChange }: CustomTabProps) {
  const [confirmIdx, setConfirmIdx] = useState<number | null>(null);

  const handleChange = (idx: number, patch: Partial<CustomDoc>) => {
    onChange(documents.map((d, i) => (i === idx ? { ...d, ...patch } : d)));
  };

  const handleDelete = (idx: number) => {
    onChange(documents.filter((_, i) => i !== idx));
    setConfirmIdx(null);
  };

  const handleAdd = () => {
    onChange([...documents, { title: '', content: '' }]);
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-slate-500">
        Add custom knowledge documents for anything not covered by services, providers, or policies.
      </p>

      {documents.length === 0 ? (
        <div className="border border-dashed border-slate-200 rounded-2xl p-8 text-center text-sm text-slate-600 bg-slate-50/60">
          No custom knowledge documents yet.
        </div>
      ) : (
        <div className="space-y-4">
          {documents.map((doc, idx) => (
            <div key={idx} className="border border-slate-200/80 rounded-2xl p-5 bg-slate-50/60">
              <div className="flex items-center justify-between mb-3">
                <span className="text-sm font-semibold text-slate-700">Document {idx + 1}</span>
                {editing && (
                  confirmIdx === idx ? (
                    <div className="flex items-center gap-2">
                      <button onClick={() => handleDelete(idx)} className="text-xs font-semibold text-red-700 hover:text-red-800">Confirm</button>
                      <button onClick={() => setConfirmIdx(null)} className="text-xs text-slate-500">Cancel</button>
                    </div>
                  ) : (
                    <button onClick={() => setConfirmIdx(idx)} className="text-xs font-semibold text-red-700 hover:text-red-800">Remove</button>
                  )
                )}
              </div>
              <label className="ui-label mb-1">Title</label>
              <input className="ui-input mb-3" value={doc.title} onChange={(e) => handleChange(idx, { title: e.target.value })} disabled={!editing} placeholder="Document title" />
              <label className="ui-label mb-1">Content</label>
              <textarea className="ui-textarea h-32" value={doc.content} onChange={(e) => handleChange(idx, { content: e.target.value })} disabled={!editing} placeholder="Document content..." />
            </div>
          ))}
        </div>
      )}

      {editing && (
        <button type="button" onClick={handleAdd} className="ui-btn ui-btn-ghost w-full">
          + Add Document
        </button>
      )}
    </div>
  );
}
