import { useState } from 'react';
import { syncMoxieKnowledge } from '../../api/client';
import type { ApiScope } from '../../api/client';
import type { StructuredKnowledge } from '../../types/knowledge';

interface SyncMoxieButtonProps {
  orgId: string;
  scope: ApiScope;
  onSync: (data: StructuredKnowledge) => void;
}

export function SyncMoxieButton({ orgId, scope, onSync }: SyncMoxieButtonProps) {
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<StructuredKnowledge | null>(null);

  const handleSync = async () => {
    setSyncing(true);
    setError(null);
    try {
      const data = await syncMoxieKnowledge(orgId, scope);
      setPreview(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to sync from Moxie');
    } finally {
      setSyncing(false);
    }
  };

  const handleConfirm = () => {
    if (preview) {
      onSync(preview);
      setPreview(null);
    }
  };

  return (
    <div>
      <button
        onClick={handleSync}
        disabled={syncing}
        className="ui-btn ui-btn-primary flex items-center gap-2"
      >
        {syncing ? (
          <>
            <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
            </svg>
            Syncing from Moxie...
          </>
        ) : (
          <>
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Sync from Moxie
          </>
        )}
      </button>
      {error && <p className="text-sm text-red-700 mt-2">{error}</p>}

      {preview && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl p-6 max-w-md w-full shadow-xl">
            <h3 className="text-lg font-semibold text-slate-900 mb-3">Sync Preview</h3>
            <p className="text-sm text-slate-600 mb-4">
              Found <strong>{(preview.sections.services.items || []).length}</strong> services and{' '}
              <strong>{(preview.sections.providers.items || []).length}</strong> providers from Moxie.
              This will replace your current services and providers. Continue?
            </p>
            <div className="flex gap-3 justify-end">
              <button onClick={() => setPreview(null)} className="ui-btn ui-btn-ghost">
                Cancel
              </button>
              <button onClick={handleConfirm} className="ui-btn ui-btn-primary">
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
