import type { ProviderItem } from '../../types/knowledge';
import { ProviderCard } from './ProviderCard';

interface ProvidersTabProps {
  providers: ProviderItem[];
  editing: boolean;
  onChange: (providers: ProviderItem[]) => void;
}

export function ProvidersTab({ providers, editing, onChange }: ProvidersTabProps) {
  const handleChange = (id: string, updated: ProviderItem) => {
    onChange(providers.map((p) => (p.id === id ? updated : p)));
  };

  const handleDelete = (id: string) => {
    onChange(providers.filter((p) => p.id !== id));
  };

  const handleAdd = () => {
    const maxOrder = providers.reduce((max, p) => Math.max(max, p.order), 0);
    onChange([...providers, {
      id: crypto.randomUUID(),
      name: '',
      title: '',
      bio: '',
      specialties: [],
      order: maxOrder + 1,
    }]);
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-slate-500">{providers.length} provider{providers.length !== 1 ? 's' : ''}</p>

      {providers.length === 0 ? (
        <div className="border border-dashed border-slate-200 rounded-2xl p-8 text-center text-sm text-slate-600 bg-slate-50/60">
          No providers yet. Add your team members.
        </div>
      ) : (
        <div className="space-y-3">
          {[...providers].sort((a, b) => a.order - b.order).map((provider) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              editing={editing}
              onChange={(u) => handleChange(provider.id, u)}
              onDelete={() => handleDelete(provider.id)}
            />
          ))}
        </div>
      )}

      {editing && (
        <button type="button" onClick={handleAdd} className="ui-btn ui-btn-ghost w-full">
          + Add Provider
        </button>
      )}
    </div>
  );
}
