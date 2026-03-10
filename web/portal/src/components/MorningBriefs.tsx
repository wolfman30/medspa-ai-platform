import { useEffect, useState } from 'react';
import { listBriefs, getBrief, type MorningBrief } from '../api/client';

export function MorningBriefs() {
  const [briefs, setBriefs] = useState<MorningBrief[]>([]);
  const [selected, setSelected] = useState<MorningBrief | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    listBriefs()
      .then((data) => {
        const sorted = (data.briefs || []).sort(
          (a, b) => b.date.localeCompare(a.date),
        );
        setBriefs(sorted);
        setError(null);
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load briefs'))
      .finally(() => setLoading(false));
  }, []);

  const handleSelect = async (brief: MorningBrief) => {
    setDetailLoading(true);
    try {
      const full = await getBrief(brief.date);
      setSelected(full);
    } catch {
      // Fallback to list-level data
      setSelected(brief);
    } finally {
      setDetailLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="animate-spin h-8 w-8 border-4 border-indigo-500 border-t-transparent rounded-full" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-4xl mx-auto p-6">
        <div className="rounded-xl border border-red-200 bg-red-50 p-4">
          <p className="text-sm font-medium text-red-800">{error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto p-4 sm:p-6">
      <div className="flex items-center gap-3 mb-6">
        <span className="text-2xl">📰</span>
        <h1 className="text-xl font-bold text-slate-900">Morning Briefs</h1>
        <span className="ml-auto text-sm text-slate-500">{briefs.length} brief{briefs.length !== 1 ? 's' : ''}</span>
      </div>

      {selected ? (
        <div>
          <button
            className="ui-btn ui-btn-ghost mb-4 text-sm"
            onClick={() => setSelected(null)}
          >
            ← Back to list
          </button>
          <div className="ui-card ui-card-solid p-6">
            <div className="flex items-start justify-between mb-4">
              <div>
                <h2 className="text-lg font-semibold text-slate-900">{selected.title || 'Market Brief'}</h2>
                <p className="text-sm text-slate-500 mt-1">{selected.date}</p>
              </div>
            </div>
            {detailLoading ? (
              <div className="flex justify-center py-8">
                <div className="animate-spin h-6 w-6 border-4 border-indigo-500 border-t-transparent rounded-full" />
              </div>
            ) : (
              <div className="prose prose-sm max-w-none text-slate-700 whitespace-pre-wrap">
                {selected.content || 'No content available.'}
              </div>
            )}
          </div>
        </div>
      ) : briefs.length === 0 ? (
        <div className="ui-card ui-card-solid p-8 text-center">
          <p className="text-slate-500">No morning briefs yet. They are generated daily at 3 AM ET.</p>
        </div>
      ) : (
        <div className="grid gap-3">
          {briefs.map((brief) => (
            <button
              key={brief.id || brief.date}
              onClick={() => handleSelect(brief)}
              className="ui-card ui-card-solid p-4 text-left hover:ring-2 hover:ring-indigo-300 transition-all"
            >
              <div className="flex items-center justify-between">
                <div className="min-w-0">
                  <h3 className="font-medium text-slate-900 truncate">
                    {brief.title || 'Market Brief'}
                  </h3>
                  <p className="text-sm text-slate-500 mt-0.5">{brief.date}</p>
                </div>
                <span className="text-slate-400 text-lg ml-3">→</span>
              </div>
              {brief.content && (
                <p className="text-sm text-slate-600 mt-2 line-clamp-2">
                  {brief.content.slice(0, 200)}
                </p>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
