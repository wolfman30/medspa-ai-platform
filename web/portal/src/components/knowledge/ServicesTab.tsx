import { useMemo } from 'react';
import type { ServiceItem, ProviderItem } from '../../types/knowledge';
import { ServiceCard } from './ServiceCard';

interface ServicesTabProps {
  services: ServiceItem[];
  providers: ProviderItem[];
  editing: boolean;
  onChange: (services: ServiceItem[]) => void;
}

function newService(order: number): ServiceItem {
  return {
    id: crypto.randomUUID(),
    name: '',
    category: '',
    price: '',
    price_type: 'fixed',
    duration_minutes: 30,
    description: '',
    provider_ids: [],
    booking_id: '',
    aliases: [],
    deposit_amount_cents: 0,
    is_addon: false,
    order,
  };
}

export function ServicesTab({ services, providers, editing, onChange }: ServicesTabProps) {
  const grouped = useMemo(() => {
    const map = new Map<string, ServiceItem[]>();
    const sorted = [...services].sort((a, b) => a.order - b.order);
    for (const s of sorted) {
      const cat = s.category || 'Uncategorized';
      if (!map.has(cat)) map.set(cat, []);
      map.get(cat)!.push(s);
    }
    return map;
  }, [services]);

  const handleChange = (id: string, updated: ServiceItem) => {
    onChange(services.map((s) => (s.id === id ? updated : s)));
  };

  const handleDelete = (id: string) => {
    onChange(services.filter((s) => s.id !== id));
  };

  const handleMove = (id: string, direction: -1 | 1) => {
    const sorted = [...services].sort((a, b) => a.order - b.order);
    const idx = sorted.findIndex((s) => s.id === id);
    const swapIdx = idx + direction;
    if (swapIdx < 0 || swapIdx >= sorted.length) return;
    const tempOrder = sorted[idx].order;
    sorted[idx] = { ...sorted[idx], order: sorted[swapIdx].order };
    sorted[swapIdx] = { ...sorted[swapIdx], order: tempOrder };
    onChange(sorted);
  };

  const handleAdd = () => {
    const maxOrder = services.reduce((max, s) => Math.max(max, s.order), 0);
    onChange([...services, newService(maxOrder + 1)]);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <p className="text-sm text-slate-500">{services.length} service{services.length !== 1 ? 's' : ''}</p>
      </div>

      {services.length === 0 ? (
        <div className="border border-dashed border-slate-200 rounded-2xl p-8 text-center text-sm text-slate-600 bg-slate-50/60">
          No services yet. Click "Add Service" or sync from Moxie to get started.
        </div>
      ) : (
        Array.from(grouped.entries()).map(([category, items]) => (
          <div key={category}>
            <h3 className="text-sm font-semibold text-slate-500 uppercase tracking-wider mb-3">{category}</h3>
            <div className="space-y-3">
              {items.map((service) => (
                <ServiceCard
                  key={service.id}
                  service={service}
                  providers={providers}
                  editing={editing}
                  onChange={(u) => handleChange(service.id, u)}
                  onDelete={() => handleDelete(service.id)}
                  onMoveUp={service.order > 1 ? () => handleMove(service.id, -1) : undefined}
                  onMoveDown={() => handleMove(service.id, 1)}
                />
              ))}
            </div>
          </div>
        ))
      )}

      {editing && (
        <button type="button" onClick={handleAdd} className="ui-btn ui-btn-ghost w-full">
          + Add Service
        </button>
      )}
    </div>
  );
}
