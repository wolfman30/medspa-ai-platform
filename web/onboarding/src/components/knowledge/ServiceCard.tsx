import { useState } from 'react';
import type { ServiceItem, ProviderItem } from '../../types/knowledge';

const CATEGORY_SUGGESTIONS = [
  'Wrinkle Relaxers', 'Dermal Filler', 'Consultation', 'Weight Loss',
  'Tixel', 'Laser Hair Removal', 'IPL', 'Tattoo Removal',
  'Microneedling', 'Chemical Peel', 'Wellness', 'Skincare',
];

const PRICE_TYPE_LABELS: Record<ServiceItem['price_type'], string> = {
  fixed: 'Fixed',
  variable: 'Variable',
  free: 'Free',
  starting_at: 'Starting at',
};

interface ServiceCardProps {
  service: ServiceItem;
  providers: ProviderItem[];
  editing: boolean;
  onChange: (updated: ServiceItem) => void;
  onDelete: () => void;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
}

export function ServiceCard({ service, providers, editing, onChange, onDelete, onMoveUp, onMoveDown }: ServiceCardProps) {
  const [expanded, setExpanded] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [aliasInput, setAliasInput] = useState('');

  const update = (patch: Partial<ServiceItem>) => onChange({ ...service, ...patch });

  const addAlias = () => {
    const trimmed = aliasInput.trim();
    if (trimmed && !service.aliases.includes(trimmed)) {
      update({ aliases: [...service.aliases, trimmed] });
    }
    setAliasInput('');
  };

  const removeAlias = (alias: string) => {
    update({ aliases: service.aliases.filter((a) => a !== alias) });
  };

  const toggleProvider = (id: string) => {
    const ids = service.provider_ids.includes(id)
      ? service.provider_ids.filter((p) => p !== id)
      : [...service.provider_ids, id];
    update({ provider_ids: ids });
  };

  const depositDollars = service.deposit_amount_cents ? (service.deposit_amount_cents / 100).toFixed(2) : '';

  return (
    <div className="border border-slate-200/80 rounded-2xl p-5 bg-slate-50/60">
      <div className="flex items-start justify-between gap-3">
        <div className="flex-1 min-w-0">
          {editing ? (
            <input
              className="ui-input font-semibold text-lg"
              value={service.name}
              onChange={(e) => update({ name: e.target.value })}
              placeholder="Service name"
            />
          ) : (
            <h3 className="text-lg font-semibold text-slate-900 truncate">{service.name || 'Untitled Service'}</h3>
          )}
        </div>
        <div className="flex items-center gap-1 shrink-0">
          {editing && onMoveUp && (
            <button onClick={onMoveUp} className="p-1 text-slate-400 hover:text-slate-600" title="Move up">↑</button>
          )}
          {editing && onMoveDown && (
            <button onClick={onMoveDown} className="p-1 text-slate-400 hover:text-slate-600" title="Move down">↓</button>
          )}
          <button
            onClick={() => setExpanded(!expanded)}
            className="p-1 text-slate-400 hover:text-slate-600"
          >
            {expanded ? '▲' : '▼'}
          </button>
          {editing && (
            confirmDelete ? (
              <div className="flex items-center gap-1 ml-2">
                <button onClick={onDelete} className="text-xs font-semibold text-red-700 hover:text-red-800">Confirm</button>
                <button onClick={() => setConfirmDelete(false)} className="text-xs text-slate-500">Cancel</button>
              </div>
            ) : (
              <button onClick={() => setConfirmDelete(true)} className="p-1 text-slate-400 hover:text-red-600 ml-1" title="Delete">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            )
          )}
        </div>
      </div>

      {/* Summary row */}
      <div className="flex flex-wrap items-center gap-2 mt-2 text-sm text-slate-600">
        <span className="bg-slate-200/70 px-2 py-0.5 rounded-full text-xs font-medium">{service.category || 'No category'}</span>
        <span>{service.price || 'No price'}</span>
        <span>·</span>
        <span>{service.duration_minutes}min</span>
        {service.is_addon && <span className="bg-amber-100 text-amber-800 px-2 py-0.5 rounded-full text-xs font-medium">Add-on</span>}
      </div>

      {expanded && (
        <div className="mt-4 space-y-4">
          {/* Category */}
          <div>
            <label className="ui-label mb-1">Category</label>
            {editing ? (
              <>
                <input
                  className="ui-input"
                  value={service.category}
                  onChange={(e) => update({ category: e.target.value })}
                  list="category-suggestions"
                  placeholder="e.g., Wrinkle Relaxers"
                />
                <datalist id="category-suggestions">
                  {CATEGORY_SUGGESTIONS.map((c) => <option key={c} value={c} />)}
                </datalist>
              </>
            ) : (
              <p className="text-sm text-slate-700">{service.category || '—'}</p>
            )}
          </div>

          {/* Price row */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div>
              <label className="ui-label mb-1">Price</label>
              {editing ? (
                <input className="ui-input" value={service.price} onChange={(e) => update({ price: e.target.value })} placeholder="$50 or $12/unit" />
              ) : (
                <p className="text-sm text-slate-700">{service.price || '—'}</p>
              )}
            </div>
            <div>
              <label className="ui-label mb-1">Price Type</label>
              {editing ? (
                <select className="ui-input" value={service.price_type} onChange={(e) => update({ price_type: e.target.value as ServiceItem['price_type'] })}>
                  {Object.entries(PRICE_TYPE_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                </select>
              ) : (
                <p className="text-sm text-slate-700">{PRICE_TYPE_LABELS[service.price_type]}</p>
              )}
            </div>
            <div>
              <label className="ui-label mb-1">Duration (min)</label>
              {editing ? (
                <input className="ui-input" type="number" min={0} value={service.duration_minutes} onChange={(e) => update({ duration_minutes: parseInt(e.target.value) || 0 })} />
              ) : (
                <p className="text-sm text-slate-700">{service.duration_minutes} min</p>
              )}
            </div>
          </div>

          {/* Description */}
          <div>
            <label className="ui-label mb-1">Description</label>
            {editing ? (
              <textarea className="ui-textarea h-24" value={service.description} onChange={(e) => update({ description: e.target.value })} placeholder="Describe this service..." />
            ) : (
              <p className="text-sm text-slate-700 whitespace-pre-wrap">{service.description || '—'}</p>
            )}
          </div>

          {/* Providers */}
          <div>
            <label className="ui-label mb-1">Providers</label>
            <div className="flex flex-wrap gap-2">
              {providers.map((p) => {
                const selected = service.provider_ids.includes(p.id);
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => editing && toggleProvider(p.id)}
                    disabled={!editing}
                    className={`px-3 py-1 rounded-full text-xs font-medium transition-colors ${
                      selected
                        ? 'bg-blue-100 text-blue-800 border border-blue-300'
                        : 'bg-slate-100 text-slate-500 border border-slate-200'
                    } ${editing ? 'cursor-pointer hover:opacity-80' : 'cursor-default'}`}
                  >
                    {p.name}
                  </button>
                );
              })}
              {providers.length === 0 && <span className="text-xs text-slate-400">Add providers in the Providers tab first</span>}
            </div>
          </div>

          {/* Aliases */}
          <div>
            <label className="ui-label mb-1">Aliases (what patients might call this)</label>
            <div className="flex flex-wrap gap-1.5 mb-2">
              {service.aliases.map((alias) => (
                <span key={alias} className="inline-flex items-center gap-1 px-2 py-0.5 bg-slate-200/70 rounded-full text-xs">
                  {alias}
                  {editing && (
                    <button onClick={() => removeAlias(alias)} className="text-slate-400 hover:text-red-600">×</button>
                  )}
                </span>
              ))}
            </div>
            {editing && (
              <div className="flex gap-2">
                <input
                  className="ui-input flex-1"
                  value={aliasInput}
                  onChange={(e) => setAliasInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addAlias(); } }}
                  placeholder="Type alias and press Enter"
                />
                <button type="button" onClick={addAlias} className="ui-btn ui-btn-ghost text-sm">Add</button>
              </div>
            )}
          </div>

          {/* Booking ID, Deposit, Add-on */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div>
              <label className="ui-label mb-1">Booking ID</label>
              {editing ? (
                <input className="ui-input text-sm" value={service.booking_id} onChange={(e) => update({ booking_id: e.target.value })} placeholder="Moxie ID" />
              ) : (
                <p className="text-sm text-slate-500 font-mono">{service.booking_id || '—'}</p>
              )}
            </div>
            <div>
              <label className="ui-label mb-1">Deposit ($)</label>
              {editing ? (
                <input className="ui-input" type="number" min={0} step={0.01} value={depositDollars} onChange={(e) => update({ deposit_amount_cents: Math.round(parseFloat(e.target.value || '0') * 100) })} placeholder="0.00" />
              ) : (
                <p className="text-sm text-slate-700">{depositDollars ? `$${depositDollars}` : '—'}</p>
              )}
            </div>
            <div className="flex items-end pb-2">
              <label className="flex items-center gap-2 text-sm text-slate-700">
                <input type="checkbox" checked={service.is_addon} onChange={(e) => editing && update({ is_addon: e.target.checked })} disabled={!editing} className="rounded" />
                Add-on service
              </label>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
