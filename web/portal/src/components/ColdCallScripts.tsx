import { useEffect, useState } from 'react';

type ScriptStatus = 'not_called' | 'called' | 'follow_up' | 'not_interested';

interface CallScript {
  clinicName: string;
  ownerName: string;
  phone: string;
  personalization: string;
  opener: string;
  painQuestion: string;
  ifInstagram: string;
  ifPhone: string;
  ifBoth: string;
  objectionPrice: string;
  objectionNotInterested: string;
  status: ScriptStatus;
}

const STATUS_CONFIG: Record<ScriptStatus, { label: string; color: string; bg: string }> = {
  not_called: { label: 'Not Called', color: 'text-slate-600', bg: 'bg-slate-100' },
  called: { label: 'Called', color: 'text-blue-700', bg: 'bg-blue-50' },
  follow_up: { label: 'Follow Up', color: 'text-amber-700', bg: 'bg-amber-50' },
  not_interested: { label: 'Not Interested', color: 'text-red-700', bg: 'bg-red-50' },
};

const STATUSES: ScriptStatus[] = ['not_called', 'called', 'follow_up', 'not_interested'];

export function ColdCallScripts() {
  const [scripts, setScripts] = useState<CallScript[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);
  const [filter, setFilter] = useState<ScriptStatus | 'all'>('all');

  useEffect(() => {
    // Load from localStorage first, then merge with JSON
    const saved = localStorage.getItem('cold-call-statuses');
    const savedStatuses: Record<string, ScriptStatus> = saved ? JSON.parse(saved) : {};

    fetch('/cold-call-scripts.json')
      .then((r) => r.json())
      .then((data) => {
        const loaded: CallScript[] = (data.scripts || []).map((s: CallScript) => ({
          ...s,
          status: savedStatuses[s.clinicName] || s.status,
        }));
        setScripts(loaded);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, []);

  const updateStatus = (idx: number, status: ScriptStatus) => {
    setScripts((prev) => {
      const next = [...prev];
      next[idx] = { ...next[idx], status };
      // Persist to localStorage
      const statuses: Record<string, ScriptStatus> = {};
      next.forEach((s) => { statuses[s.clinicName] = s.status; });
      localStorage.setItem('cold-call-statuses', JSON.stringify(statuses));
      return next;
    });
  };

  const filtered = filter === 'all' ? scripts : scripts.filter((s) => s.status === filter);

  const counts = scripts.reduce(
    (acc, s) => { acc[s.status] = (acc[s.status] || 0) + 1; return acc; },
    {} as Record<string, number>,
  );

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <div className="mb-6">
        <h1 className="text-xl font-bold text-slate-900">Cold Call Scripts</h1>
        <p className="mt-1 text-sm text-slate-500">
          {scripts.length} prospects &middot; {counts['not_called'] || 0} to call &middot; {counts['follow_up'] || 0} follow-ups
        </p>
      </div>

      {/* Filter pills */}
      <div className="mb-4 flex flex-wrap gap-2">
        <button
          onClick={() => setFilter('all')}
          className={`rounded-full px-3 py-1 text-xs font-medium transition ${
            filter === 'all' ? 'bg-slate-900 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
          }`}
        >
          All ({scripts.length})
        </button>
        {STATUSES.map((s) => {
          const cfg = STATUS_CONFIG[s];
          return (
            <button
              key={s}
              onClick={() => setFilter(s)}
              className={`rounded-full px-3 py-1 text-xs font-medium transition ${
                filter === s ? 'bg-slate-900 text-white' : `${cfg.bg} ${cfg.color} hover:opacity-80`
              }`}
            >
              {cfg.label} ({counts[s] || 0})
            </button>
          );
        })}
      </div>

      {/* Script cards */}
      <div className="space-y-3">
        {filtered.map((script) => {
          const realIdx = scripts.indexOf(script);
          const isExpanded = expandedIdx === realIdx;
          const cfg = STATUS_CONFIG[script.status];

          return (
            <div key={script.clinicName} className="rounded-xl border border-slate-200 bg-white shadow-sm overflow-hidden">
              {/* Header - always visible */}
              <button
                onClick={() => setExpandedIdx(isExpanded ? null : realIdx)}
                className="w-full px-4 py-3 flex items-start gap-3 text-left hover:bg-slate-50 transition"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-semibold text-slate-900 text-sm">{script.clinicName}</span>
                    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium ${cfg.bg} ${cfg.color}`}>
                      {cfg.label}
                    </span>
                  </div>
                  <div className="mt-0.5 text-xs text-slate-500 truncate">{script.ownerName}</div>
                  {script.phone && (
                    <a
                      href={`tel:${script.phone}`}
                      onClick={(e) => e.stopPropagation()}
                      className="mt-0.5 inline-block text-xs font-medium text-violet-600 hover:text-violet-800"
                    >
                      📞 {script.phone}
                    </a>
                  )}
                </div>
                <svg
                  className={`h-5 w-5 text-slate-400 shrink-0 transition-transform ${isExpanded ? 'rotate-180' : ''}`}
                  fill="none" viewBox="0 0 24 24" stroke="currentColor"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
              </button>

              {/* Expanded content */}
              {isExpanded && (
                <div className="border-t border-slate-100 px-4 py-4 space-y-4">
                  {/* Personalization */}
                  <div className="rounded-lg bg-violet-50 p-3">
                    <div className="text-[10px] font-semibold uppercase tracking-wider text-violet-600 mb-1">Intel</div>
                    <p className="text-sm text-violet-900">{script.personalization}</p>
                  </div>

                  {/* Script flow */}
                  <div className="space-y-3">
                    <ScriptBlock emoji="👋" label="Opener" text={script.opener} />
                    <ScriptBlock emoji="🎯" label="Pain Question" text={script.painQuestion} />
                    <ScriptBlock emoji="📱" label="If Instagram" text={script.ifInstagram} />
                    <ScriptBlock emoji="📞" label="If Phone" text={script.ifPhone} />
                    <ScriptBlock emoji="🔁" label="If Both" text={script.ifBoth} />
                    <ScriptBlock emoji="💰" label='Objection: "Too expensive"' text={script.objectionPrice} />
                    <ScriptBlock emoji="🚫" label='Objection: "Not interested"' text={script.objectionNotInterested} />
                  </div>

                  {/* Status buttons */}
                  <div className="flex flex-wrap gap-2 pt-2 border-t border-slate-100">
                    <span className="text-xs text-slate-500 self-center mr-1">Mark as:</span>
                    {STATUSES.map((s) => {
                      const sc = STATUS_CONFIG[s];
                      const active = script.status === s;
                      return (
                        <button
                          key={s}
                          onClick={() => updateStatus(realIdx, s)}
                          className={`rounded-full px-3 py-1 text-xs font-medium transition ${
                            active ? 'ring-2 ring-slate-900 ring-offset-1' : ''
                          } ${sc.bg} ${sc.color} hover:opacity-80`}
                        >
                          {sc.label}
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {filtered.length === 0 && (
        <div className="text-center py-12 text-sm text-slate-400">No scripts match this filter.</div>
      )}
    </div>
  );
}

function ScriptBlock({ emoji, label, text }: { emoji: string; label: string; text: string }) {
  return (
    <div>
      <div className="text-[10px] font-semibold uppercase tracking-wider text-slate-400 mb-0.5">
        {emoji} {label}
      </div>
      <p className="text-sm text-slate-700 leading-relaxed">{text}</p>
    </div>
  );
}
