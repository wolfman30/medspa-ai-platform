import { useEffect, useState, useCallback } from 'react';
import { listBriefs, type MorningBrief } from '../api/client';

type Brief = MorningBrief & { summary?: string };

export function MorningBriefs() {
  const [briefs, setBriefs] = useState<Brief[]>([]);
  const [selectedBrief, setSelectedBrief] = useState<Brief | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchBriefs = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await listBriefs();
      setBriefs(data.briefs || []);
      // Auto-select the latest brief
      if (data.briefs?.length && !selectedBrief) {
        setSelectedBrief(data.briefs[0]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load briefs');
    } finally {
      setLoading(false);
    }
  }, [selectedBrief]);

  useEffect(() => {
    fetchBriefs();
  }, [fetchBriefs]);

  const handleSelect = (brief: Brief) => {
    setSelectedBrief(brief);
  };

  if (loading) {
    return (
      <div className="ui-page">
        <div className="ui-container py-8 flex items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="ui-page">
        <div className="ui-container py-8">
          <div className="rounded-xl border border-red-200 bg-red-50 p-4">
            <p className="text-sm font-medium text-red-800">{error}</p>
            <button onClick={fetchBriefs} className="ui-btn ui-btn-ghost mt-2 text-sm">
              Retry
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (briefs.length === 0) {
    return (
      <div className="ui-page">
        <div className="ui-container py-8">
          <h2 className="text-lg font-semibold text-slate-900">Morning Briefs</h2>
          <p className="ui-muted mt-2">No briefs yet. The market research agent generates these daily at 3 AM ET.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="ui-page">
      <div className="ui-container py-6">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h2 className="text-lg font-semibold tracking-tight text-slate-900">
              📰 Morning Briefs
            </h2>
            <p className="ui-muted mt-1">
              Daily market intelligence &amp; competitive analysis
            </p>
          </div>
          <span className="text-xs text-slate-400">{briefs.length} briefs</span>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
          {/* Brief list sidebar */}
          <div className="lg:col-span-4 xl:col-span-3">
            <div className="space-y-2 max-h-[70vh] overflow-y-auto pr-1">
              {briefs.map((brief) => (
                <button
                  key={brief.id}
                  onClick={() => handleSelect(brief)}
                  className={`w-full text-left rounded-xl border p-3 transition-colors ${
                    selectedBrief?.id === brief.id
                      ? 'border-violet-300 bg-violet-50 shadow-sm'
                      : 'border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50'
                  }`}
                >
                  <div className="text-xs font-medium text-slate-400">{brief.date}</div>
                  <div className="text-sm font-medium text-slate-900 mt-1 line-clamp-2">
                    {brief.title}
                  </div>
                  {brief.summary && (
                    <div className="text-xs text-slate-500 mt-1 line-clamp-2">
                      {brief.summary}
                    </div>
                  )}
                </button>
              ))}
            </div>
          </div>

          {/* Brief content */}
          <div className="lg:col-span-8 xl:col-span-9">
            {selectedBrief ? (
              <div className="ui-card ui-card-solid p-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h3 className="text-base font-semibold text-slate-900">
                      {selectedBrief.title}
                    </h3>
                    <span className="text-xs text-slate-400">{selectedBrief.date}</span>
                  </div>
                </div>
                <div className="prose prose-sm prose-slate max-w-none">
                  <BriefContent content={selectedBrief.content} />
                </div>
              </div>
            ) : (
              <div className="ui-card ui-card-solid p-6 text-center">
                <p className="ui-muted">Select a brief to read</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

/** Simple markdown-ish renderer for brief content */
function BriefContent({ content }: { content: string }) {
  const lines = content.split('\n');
  const elements: React.ReactElement[] = [];
  let i = 0;

  for (const line of lines) {
    i++;
    const trimmed = line.trimEnd();

    if (trimmed.startsWith('# ')) {
      elements.push(<h1 key={i} className="text-lg font-bold text-slate-900 mt-4 mb-2">{trimmed.slice(2)}</h1>);
    } else if (trimmed.startsWith('## ')) {
      elements.push(<h2 key={i} className="text-base font-semibold text-slate-800 mt-4 mb-1">{trimmed.slice(3)}</h2>);
    } else if (trimmed.startsWith('### ')) {
      elements.push(<h3 key={i} className="text-sm font-semibold text-slate-700 mt-3 mb-1">{trimmed.slice(4)}</h3>);
    } else if (trimmed.startsWith('- ') || trimmed.startsWith('* ')) {
      elements.push(
        <div key={i} className="flex gap-2 text-sm text-slate-700 ml-2">
          <span className="text-slate-400 shrink-0">•</span>
          <span>{formatInline(trimmed.slice(2))}</span>
        </div>
      );
    } else if (trimmed === '') {
      elements.push(<div key={i} className="h-2" />);
    } else {
      elements.push(<p key={i} className="text-sm text-slate-700 leading-relaxed">{formatInline(trimmed)}</p>);
    }
  }

  return <>{elements}</>;
}

/** Bold and link formatting */
function formatInline(text: string): React.ReactNode {
  // Handle **bold**
  const parts = text.split(/(\*\*[^*]+\*\*)/);
  return parts.map((part, i) => {
    if (part.startsWith('**') && part.endsWith('**')) {
      return <strong key={i} className="font-semibold text-slate-900">{part.slice(2, -2)}</strong>;
    }
    return <span key={i}>{part}</span>;
  });
}
