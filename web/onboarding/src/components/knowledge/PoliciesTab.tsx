import { useState } from 'react';
import type { PolicySection } from '../../types/knowledge';

interface PoliciesTabProps {
  policies: PolicySection;
  editing: boolean;
  onChange: (policies: PolicySection) => void;
}

export function PoliciesTab({ policies, editing, onChange }: PoliciesTabProps) {
  const [newBookingPolicy, setNewBookingPolicy] = useState('');
  const [newCustomPolicy, setNewCustomPolicy] = useState('');

  const update = (patch: Partial<PolicySection>) => onChange({ ...policies, ...patch });

  const addBookingPolicy = () => {
    const trimmed = newBookingPolicy.trim();
    if (trimmed) {
      update({ booking_policies: [...policies.booking_policies, trimmed] });
      setNewBookingPolicy('');
    }
  };

  const removeBookingPolicy = (idx: number) => {
    update({ booking_policies: policies.booking_policies.filter((_, i) => i !== idx) });
  };

  const moveBookingPolicy = (idx: number, dir: -1 | 1) => {
    const arr = [...policies.booking_policies];
    const swapIdx = idx + dir;
    if (swapIdx < 0 || swapIdx >= arr.length) return;
    [arr[idx], arr[swapIdx]] = [arr[swapIdx], arr[idx]];
    update({ booking_policies: arr });
  };

  const addCustomPolicy = () => {
    const trimmed = newCustomPolicy.trim();
    if (trimmed) {
      update({ custom: [...(policies.custom || []), trimmed] });
      setNewCustomPolicy('');
    }
  };

  const removeCustomPolicy = (idx: number) => {
    update({ custom: (policies.custom || []).filter((_, i) => i !== idx) });
  };

  return (
    <div className="space-y-6">
      <div>
        <label className="ui-label mb-1">Cancellation Policy</label>
        {editing ? (
          <textarea className="ui-textarea h-24" value={policies.cancellation} onChange={(e) => update({ cancellation: e.target.value })} placeholder="Describe your cancellation policy..." />
        ) : (
          <p className="text-sm text-slate-700 whitespace-pre-wrap bg-slate-50/60 rounded-xl p-3">{policies.cancellation || '—'}</p>
        )}
      </div>

      <div>
        <label className="ui-label mb-1">Deposit Policy</label>
        {editing ? (
          <textarea className="ui-textarea h-24" value={policies.deposit} onChange={(e) => update({ deposit: e.target.value })} placeholder="Describe your deposit policy..." />
        ) : (
          <p className="text-sm text-slate-700 whitespace-pre-wrap bg-slate-50/60 rounded-xl p-3">{policies.deposit || '—'}</p>
        )}
      </div>

      <div>
        <label className="ui-label mb-1">Age Requirement</label>
        {editing ? (
          <textarea className="ui-textarea h-16" value={policies.age_requirement} onChange={(e) => update({ age_requirement: e.target.value })} placeholder="e.g., Must be 18+ or have guardian present" />
        ) : (
          <p className="text-sm text-slate-700 bg-slate-50/60 rounded-xl p-3">{policies.age_requirement || '—'}</p>
        )}
      </div>

      <div>
        <label className="ui-label mb-1">Terms URL</label>
        {editing ? (
          <input className="ui-input" value={policies.terms_url || ''} onChange={(e) => update({ terms_url: e.target.value })} placeholder="https://..." />
        ) : (
          <p className="text-sm text-slate-700">{policies.terms_url || '—'}</p>
        )}
      </div>

      {/* Booking Policies */}
      <div>
        <label className="ui-label mb-2">Booking Policies (shown to patients before payment)</label>
        {policies.booking_policies.length === 0 && !editing && (
          <p className="text-sm text-slate-500 italic">No booking policies set.</p>
        )}
        <div className="space-y-2">
          {policies.booking_policies.map((policy, idx) => (
            <div key={idx} className="flex items-start gap-2 bg-slate-50/60 rounded-xl p-3">
              <span className="text-sm text-slate-400 font-mono shrink-0">{idx + 1}.</span>
              <p className="text-sm text-slate-700 flex-1">{policy}</p>
              {editing && (
                <div className="flex items-center gap-1 shrink-0">
                  {idx > 0 && <button onClick={() => moveBookingPolicy(idx, -1)} className="text-slate-400 hover:text-slate-600 text-xs">↑</button>}
                  {idx < policies.booking_policies.length - 1 && <button onClick={() => moveBookingPolicy(idx, 1)} className="text-slate-400 hover:text-slate-600 text-xs">↓</button>}
                  <button onClick={() => removeBookingPolicy(idx)} className="text-slate-400 hover:text-red-600 text-xs ml-1">×</button>
                </div>
              )}
            </div>
          ))}
        </div>
        {editing && (
          <div className="flex gap-2 mt-2">
            <input className="ui-input flex-1" value={newBookingPolicy} onChange={(e) => setNewBookingPolicy(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addBookingPolicy(); } }} placeholder="Add booking policy bullet..." />
            <button type="button" onClick={addBookingPolicy} className="ui-btn ui-btn-ghost text-sm">Add</button>
          </div>
        )}
      </div>

      {/* Custom Policies */}
      <div>
        <label className="ui-label mb-2">Custom Policies</label>
        {(policies.custom || []).length === 0 && !editing && (
          <p className="text-sm text-slate-500 italic">No custom policies.</p>
        )}
        <div className="space-y-2">
          {(policies.custom || []).map((policy, idx) => (
            <div key={idx} className="flex items-start gap-2 bg-slate-50/60 rounded-xl p-3">
              <p className="text-sm text-slate-700 flex-1">{policy}</p>
              {editing && (
                <button onClick={() => removeCustomPolicy(idx)} className="text-slate-400 hover:text-red-600 text-xs shrink-0">×</button>
              )}
            </div>
          ))}
        </div>
        {editing && (
          <div className="flex gap-2 mt-2">
            <input className="ui-input flex-1" value={newCustomPolicy} onChange={(e) => setNewCustomPolicy(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addCustomPolicy(); } }} placeholder="Add custom policy..." />
            <button type="button" onClick={addCustomPolicy} className="ui-btn ui-btn-ghost text-sm">Add</button>
          </div>
        )}
      </div>
    </div>
  );
}
