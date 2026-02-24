import { useEffect, useState, useCallback, useRef } from 'react';
import { getRule100Today, logTouch, listProspects, type Rule100Response } from '../api/client';

interface SimpleProspect {
  id: string;
  clinic: string;
}

const TOUCH_TYPES = [
  { key: 'text_sent', label: 'Text', icon: '📱' },
  { key: 'dm_sent', label: 'DM', icon: '💬' },
  { key: 'email_sent', label: 'Email', icon: '📧' },
  { key: 'call_made', label: 'Call', icon: '📞' },
  { key: 'comment_posted', label: 'Comment', icon: '💬' },
] as const;

const TYPE_TO_SIMPLE: Record<string, string> = {
  text_sent: 'text',
  dm_sent: 'dm',
  email_sent: 'email',
  call_made: 'call',
  comment_posted: 'comment',
};

function ProgressRing({ value, max, size = 160 }: { value: number; max: number; size?: number }) {
  const stroke = 10;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const pct = Math.min(value / max, 1);
  const offset = circumference * (1 - pct);

  const color = value >= max ? '#22c55e' : value > 0 ? '#8b5cf6' : '#ef4444';
  const pulseClass = value === 0 ? 'animate-pulse' : '';

  return (
    <svg width={size} height={size} className={pulseClass}>
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="#1e293b"
        strokeWidth={stroke}
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke={color}
        strokeWidth={stroke}
        strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        style={{ transition: 'stroke-dashoffset 0.6s ease, stroke 0.3s ease' }}
      />
    </svg>
  );
}

function MiniBarChart({ history }: { history: Array<{ date: string; touches: number }> }) {
  const maxTouches = Math.max(...history.map(d => d.touches), 1);
  const reversed = [...history].reverse(); // oldest first

  return (
    <div className="flex items-end gap-1 h-20">
      {reversed.map(day => {
        const heightPct = Math.max((day.touches / maxTouches) * 100, 2);
        const isGoal = day.touches >= 100;
        const dateLabel = new Date(day.date + 'T12:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
        return (
          <div key={day.date} className="flex-1 flex flex-col items-center group relative">
            <div
              className={`w-full rounded-t transition-all duration-300 ${isGoal ? 'bg-green-500' : 'bg-slate-600'}`}
              style={{ height: `${heightPct}%`, minHeight: '2px' }}
            />
            <div className="absolute -top-8 bg-slate-800 text-xs text-slate-300 px-1.5 py-0.5 rounded opacity-0 group-hover:opacity-100 transition whitespace-nowrap pointer-events-none z-10">
              {dateLabel}: {day.touches}
            </div>
          </div>
        );
      })}
    </div>
  );
}

export function Rule100Card() {
  const [data, setData] = useState<Rule100Response | null>(null);
  const [loading, setLoading] = useState(true);
  const [prospects, setProspects] = useState<SimpleProspect[]>([]);
  const [activeDropdown, setActiveDropdown] = useState<string | null>(null);
  const [animatingCount, setAnimatingCount] = useState<number | null>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    try {
      const result = await getRule100Today();
      setData(result);
    } catch {
      // silently fail — card just shows empty
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    listProspects()
      .then(d => setProspects((d.prospects as SimpleProspect[]) || []))
      .catch(() => {});
  }, [refresh]);

  // Close dropdown on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setActiveDropdown(null);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  const handleLogTouch = async (prospectId: string, touchType: string) => {
    setActiveDropdown(null);
    try {
      await logTouch(prospectId, touchType);
      // Animate count increment
      if (data) {
        const newCount = data.touches + 1;
        setAnimatingCount(newCount);
        setTimeout(() => setAnimatingCount(null), 600);
      }
      await refresh();
    } catch {
      // ignore
    }
  };

  if (loading) {
    return (
      <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
        <div className="flex justify-center py-8">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-slate-700 border-t-violet-500" />
        </div>
      </div>
    );
  }

  if (!data) return null;

  const displayCount = animatingCount ?? data.touches;

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold">🎯 Rule of 100</h2>
        <button onClick={refresh} className="text-xs text-violet-400 hover:text-violet-300 transition">↻</button>
      </div>

      <div className="grid lg:grid-cols-3 gap-6">
        {/* Left: Progress Ring */}
        <div className="flex flex-col items-center justify-center">
          <div className="relative">
            <ProgressRing value={data.touches} max={data.goal} />
            <div className="absolute inset-0 flex flex-col items-center justify-center">
              <span
                className={`text-3xl font-bold transition-all duration-300 ${
                  animatingCount !== null ? 'scale-110 text-green-400' : ''
                }`}
              >
                {displayCount}
              </span>
              <span className="text-sm text-slate-500">/ {data.goal}</span>
            </div>
          </div>
          <div className="mt-3 text-sm">
            {data.streak > 0 ? (
              <span className="text-orange-400">🔥 {data.streak}-day streak</span>
            ) : (
              <span className="text-slate-500">⚡ Start your streak!</span>
            )}
          </div>
        </div>

        {/* Middle: Breakdown + Quick Log */}
        <div className="space-y-4">
          {/* Breakdown by type */}
          <div>
            <h3 className="text-xs text-slate-500 uppercase tracking-wide mb-2">By Type</h3>
            <div className="space-y-1.5">
              {TOUCH_TYPES.map(t => {
                const simpleKey = TYPE_TO_SIMPLE[t.key] || t.key;
                const count = data.byType[simpleKey] || 0;
                const maxType = Math.max(...Object.values(data.byType), 1);
                return (
                  <div key={t.key} className="flex items-center gap-2 text-xs">
                    <span className="w-14 text-slate-400">{t.icon} {t.label}</span>
                    <div className="flex-1 h-3 bg-slate-800 rounded-full overflow-hidden">
                      <div
                        className="h-full bg-violet-500 rounded-full transition-all duration-500"
                        style={{ width: `${count > 0 ? Math.max((count / maxType) * 100, 8) : 0}%` }}
                      />
                    </div>
                    <span className="w-6 text-right font-mono text-slate-400">{count}</span>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Quick Log */}
          <div ref={dropdownRef}>
            <h3 className="text-xs text-slate-500 uppercase tracking-wide mb-2">Quick Log</h3>
            <div className="flex flex-wrap gap-1.5">
              {TOUCH_TYPES.map(t => (
                <div key={t.key} className="relative">
                  <button
                    onClick={() => setActiveDropdown(activeDropdown === t.key ? null : t.key)}
                    className={`px-2.5 py-1.5 rounded-lg text-xs font-medium transition ${
                      activeDropdown === t.key
                        ? 'bg-violet-600 text-white'
                        : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
                    }`}
                  >
                    {t.icon} {t.label}
                  </button>
                  {activeDropdown === t.key && (
                    <div className="absolute top-full left-0 mt-1 w-56 bg-slate-800 border border-slate-700 rounded-lg shadow-xl z-20 max-h-48 overflow-y-auto">
                      {prospects.length === 0 ? (
                        <div className="p-2 text-xs text-slate-500">No prospects</div>
                      ) : (
                        prospects.map(p => (
                          <button
                            key={p.id}
                            onClick={() => handleLogTouch(p.id, t.key)}
                            className="w-full text-left px-3 py-2 text-xs text-slate-300 hover:bg-slate-700 transition truncate"
                          >
                            {p.clinic || p.id}
                          </button>
                        ))
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Right: 14-day history */}
        <div>
          <h3 className="text-xs text-slate-500 uppercase tracking-wide mb-2">Last 14 Days</h3>
          {data.history.length > 0 ? (
            <MiniBarChart history={data.history} />
          ) : (
            <p className="text-xs text-slate-500">No history yet</p>
          )}
          {data.byProspect.length > 0 && (
            <div className="mt-4">
              <h3 className="text-xs text-slate-500 uppercase tracking-wide mb-1">Top Prospects Today</h3>
              <div className="space-y-1">
                {data.byProspect.slice(0, 5).map(p => (
                  <div key={p.id} className="flex justify-between text-xs">
                    <span className="text-slate-400 truncate">{p.clinic}</span>
                    <span className="font-mono text-violet-400">{p.count}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
