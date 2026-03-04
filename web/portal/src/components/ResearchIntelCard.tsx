import { useEffect, useState } from 'react';
import { getResearchDocs, type ResearchDoc } from '../api/client';

const CATEGORY_ICONS: Record<string, string> = {
  'competitive': '⚔️',
  'pricing': '💰',
  'market': '📊',
  'strategy': '🎯',
  'technology': '🔧',
  'default': '📄',
};

const CATEGORY_COLORS: Record<string, string> = {
  'competitive': 'bg-red-900/30 border-red-800 text-red-300',
  'pricing': 'bg-green-900/30 border-green-800 text-green-300',
  'market': 'bg-blue-900/30 border-blue-800 text-blue-300',
  'strategy': 'bg-violet-900/30 border-violet-800 text-violet-300',
  'technology': 'bg-amber-900/30 border-amber-800 text-amber-300',
  'default': 'bg-slate-800/50 border-slate-700 text-slate-300',
};

function mdToHtml(md: string): string {
  return md
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/^#### (.+)$/gm, '<h4 class="text-sm font-semibold text-slate-200 mt-3 mb-1">$1</h4>')
    .replace(/^### (.+)$/gm, '<h3 class="text-sm font-semibold text-slate-100 mt-4 mb-1">$1</h3>')
    .replace(/^## (.+)$/gm, '<h2 class="text-base font-semibold text-slate-100 mt-4 mb-2">$1</h2>')
    .replace(/^# (.+)$/gm, '<h1 class="text-lg font-bold text-slate-100 mt-4 mb-2">$1</h1>')
    .replace(/\*\*(.+?)\*\*/g, '<strong class="text-violet-400">$1</strong>')
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    .replace(/\|(.+)\|/gm, (match) => {
      const cells = match.split('|').filter(Boolean).map(c => c.trim());
      if (cells.every(c => /^[-:]+$/.test(c))) return '';
      const tag = cells.some(c => /^[-:]+$/.test(c)) ? 'th' : 'td';
      return `<tr>${cells.map(c => `<${tag} class="px-2 py-1 text-xs border border-slate-700">${c}</${tag}>`).join('')}</tr>`;
    })
    .replace(/^- (.+)$/gm, '<li class="ml-4 list-disc text-slate-300 text-sm">$1</li>')
    .replace(/^\d+\. (.+)$/gm, '<li class="ml-4 list-decimal text-slate-300 text-sm">$1</li>')
    .replace(/\n{2,}/g, '<br/>')
    .replace(/\n/g, '<br/>');
}

function DocCard({ doc, onExpand }: { doc: ResearchDoc; onExpand: () => void }) {
  const cat = doc.category || 'default';
  const icon = CATEGORY_ICONS[cat] || CATEGORY_ICONS.default;
  const colorCls = CATEGORY_COLORS[cat] || CATEGORY_COLORS.default;

  return (
    <button
      onClick={onExpand}
      className="w-full text-left rounded-xl border border-slate-800 bg-slate-900 p-4 hover:bg-slate-800/70 transition cursor-pointer"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="text-lg">{icon}</span>
          <h3 className="text-sm font-semibold text-slate-100">{doc.title}</h3>
        </div>
        <span className={`text-[10px] font-semibold px-2 py-0.5 rounded border ${colorCls}`}>
          {cat}
        </span>
      </div>
      {doc.summary && (
        <p className="mt-2 text-xs text-slate-400 line-clamp-2">{doc.summary}</p>
      )}
      <div className="mt-2 flex items-center gap-3 text-[11px] text-slate-500">
        <span>{doc.updatedAt ? new Date(doc.updatedAt).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) : 'No date'}</span>
        {doc.tags?.map(tag => (
          <span key={tag} className="bg-slate-800 px-1.5 py-0.5 rounded text-slate-400">{tag}</span>
        ))}
      </div>
    </button>
  );
}

function DocDetail({ doc, onBack }: { doc: ResearchDoc; onBack: () => void }) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-6">
      <button onClick={onBack} className="text-sm text-violet-400 hover:text-violet-300 mb-4">← Back to Research</button>
      <h2 className="text-lg font-semibold text-slate-100 mb-1">{doc.title}</h2>
      {doc.summary && <p className="text-sm text-slate-400 mb-4">{doc.summary}</p>}
      <div
        className="prose prose-invert prose-sm max-w-none text-slate-300 leading-relaxed"
        dangerouslySetInnerHTML={{ __html: mdToHtml(doc.content) }}
      />
    </div>
  );
}

export function ResearchIntelCard() {
  const [docs, setDocs] = useState<ResearchDoc[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedDoc, setSelectedDoc] = useState<ResearchDoc | null>(null);
  const [filter, setFilter] = useState<string>('all');

  useEffect(() => {
    getResearchDocs()
      .then(data => setDocs(data.docs || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const categories = ['all', ...Array.from(new Set(docs.map(d => d.category || 'default')))];
  const filtered = filter === 'all' ? docs : docs.filter(d => (d.category || 'default') === filter);

  if (selectedDoc) {
    return <DocDetail doc={selectedDoc} onBack={() => setSelectedDoc(null)} />;
  }

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold">🔬 Research Intelligence</h2>
        <div className="flex gap-1">
          {categories.map(cat => (
            <button
              key={cat}
              onClick={() => setFilter(cat)}
              className={`text-[11px] px-2 py-1 rounded transition ${
                filter === cat ? 'bg-violet-600 text-white' : 'bg-slate-800 text-slate-400 hover:text-slate-200'
              }`}
            >
              {cat === 'all' ? 'All' : cat}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="flex justify-center py-8">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-slate-700 border-t-violet-500" />
        </div>
      ) : filtered.length === 0 ? (
        <p className="text-sm text-slate-500 py-4">No research docs yet. Andre will populate this with competitive intelligence, pricing research, and market data.</p>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {filtered.map(doc => (
            <DocCard key={doc.id} doc={doc} onExpand={() => setSelectedDoc(doc)} />
          ))}
        </div>
      )}
    </div>
  );
}
