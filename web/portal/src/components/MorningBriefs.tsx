import { useEffect, useState, useCallback } from 'react';
import { listBriefs, type MorningBrief } from '../api/client';

// Simple markdown-to-HTML (headings, bold, italic, lists, links, code)
function mdToHtml(md: string): string {
  return md
    .replace(/^### (.+)$/gm, '<h3 class="text-base font-semibold text-slate-800 mt-4 mb-1">$1</h3>')
    .replace(/^## (.+)$/gm, '<h2 class="text-lg font-bold text-slate-900 mt-5 mb-2">$1</h2>')
    .replace(/^# (.+)$/gm, '<h1 class="text-xl font-bold text-slate-900 mt-6 mb-2">$1</h1>')
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    .replace(/`([^`]+)`/g, '<code class="bg-slate-100 px-1 py-0.5 rounded text-sm font-mono">$1</code>')
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener" class="text-violet-600 underline">$1</a>')
    .replace(/^- (.+)$/gm, '<li class="ml-4 list-disc text-sm text-slate-700">$1</li>')
    .replace(/\n/g, '<br/>');
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + 'T12:00:00');
  return d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
}

function formatShortDate(dateStr: string): string {
  const d = new Date(dateStr + 'T12:00:00');
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export function MorningBriefs() {
  const [briefs, setBriefs] = useState<MorningBrief[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedBrief, setSelectedBrief] = useState<MorningBrief | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  const fetchBriefs = useCallback(() => {
    setLoading(true);
    setError(null);
    listBriefs()
      .then(data => {
        const sorted = (data.briefs || []).sort((a, b) => b.date.localeCompare(a.date));
        setBriefs(sorted);
      })
      .catch(err => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { fetchBriefs(); }, [fetchBriefs]);

  const filtered = searchQuery.trim()
    ? briefs.filter(b =>
        b.title.toLowerCase().includes(searchQuery.toLowerCase()) ||
        b.content.toLowerCase().includes(searchQuery.toLowerCase()) ||
        b.date.includes(searchQuery)
      )
    : briefs;

  // Detail view
  if (selectedBrief) {
    return (
      <div className="ui-container py-6 max-w-4xl">
        <button
          onClick={() => setSelectedBrief(null)}
          className="ui-btn ui-btn-ghost mb-4 text-sm"
        >
          ← Back to briefs
        </button>
        <div className="ui-card ui-card-solid p-6 sm:p-8">
          <div className="mb-4">
            <p className="text-sm text-slate-500">{formatDate(selectedBrief.date)}</p>
            <h1 className="text-xl font-bold text-slate-900 mt-1">{selectedBrief.title}</h1>
          </div>
          <hr className="border-slate-200 mb-4" />
          <div
            className="prose prose-sm prose-slate max-w-none text-sm text-slate-700 leading-relaxed"
            dangerouslySetInnerHTML={{ __html: mdToHtml(selectedBrief.content) }}
          />
        </div>
      </div>
    );
  }

  // List view
  return (
    <div className="ui-container py-6 max-w-4xl">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-6">
        <div>
          <h1 className="text-xl font-bold text-slate-900">📰 Morning Briefs</h1>
          <p className="text-sm text-slate-500 mt-1">
            Daily market research & competitive intelligence
          </p>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="text"
            placeholder="Search briefs..."
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            className="ui-input w-full sm:w-64"
          />
          <button onClick={fetchBriefs} className="ui-btn ui-btn-ghost text-sm" title="Refresh">
            🔄
          </button>
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
        </div>
      ) : error ? (
        <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          Failed to load briefs: {error}
        </div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12 text-sm text-slate-500">
          {searchQuery ? 'No briefs match your search.' : 'No briefs available yet.'}
        </div>
      ) : (
        <div className="space-y-3">
          {filtered.map(brief => {
            // Extract first meaningful line as preview
            const lines = brief.content.split('\n').filter(l => l.trim() && !l.startsWith('#'));
            const preview = lines[0]?.replace(/\*\*/g, '').replace(/\*/g, '').slice(0, 150) || '';

            return (
              <button
                key={brief.id}
                onClick={() => setSelectedBrief(brief)}
                className="w-full text-left ui-card ui-card-solid p-4 hover:bg-slate-50 transition-colors"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="inline-block rounded bg-violet-100 px-2 py-0.5 text-xs font-medium text-violet-800">
                        {formatShortDate(brief.date)}
                      </span>
                    </div>
                    <h3 className="text-sm font-semibold text-slate-900 mt-1 truncate">
                      {brief.title}
                    </h3>
                    {preview && (
                      <p className="text-xs text-slate-500 mt-1 line-clamp-2">{preview}...</p>
                    )}
                  </div>
                  <span className="text-slate-400 text-sm shrink-0">→</span>
                </div>
              </button>
            );
          })}
        </div>
      )}

      <div className="mt-6 text-center text-xs text-slate-400">
        {filtered.length} brief{filtered.length !== 1 ? 's' : ''} total
      </div>
    </div>
  );
}
