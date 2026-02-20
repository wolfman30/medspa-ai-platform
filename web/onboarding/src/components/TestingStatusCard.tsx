import { useEffect, useState, useCallback } from 'react';
import { listTestResults, updateTestResult, type TestResult, type TestSummary } from '../api/client';

const STATUS_ICONS: Record<string, string> = {
  passed: '✅',
  failed: '❌',
  untested: '⬜',
  skipped: '⏭️',
};

const CATEGORY_LABELS: Record<string, string> = {
  'must-pass': '🎯 Must-Pass (Phone Tests)',
  'smoke-test': '💨 Smoke Tests (Other Clinics)',
  'automated': '🤖 Automated E2E',
};

function formatDate(dateStr: string | null): string {
  if (!dateStr) return '—';
  const d = new Date(dateStr);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) +
    ' ' + d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' });
}

function TestRow({
  result,
  onUpdate,
}: {
  result: TestResult;
  onUpdate: (id: number, status: string, notes: string) => void;
}) {
  const [notes] = useState(result.notes);

  return (
    <div className="flex items-center gap-3 p-2 rounded-lg hover:bg-slate-800/50 transition group">
      <span className="text-lg" title={result.status}>{STATUS_ICONS[result.status] || '⬜'}</span>
      <div className="flex-1 min-w-0">
        <div className="text-sm text-slate-200 truncate">{result.scenario_name}</div>
        <div className="text-xs text-slate-500">
          {result.clinic}
          {result.tested_at && (
            <> · {formatDate(result.tested_at)}</>
          )}
          {result.notes && (
            <> · <span className="text-slate-400">{result.notes}</span></>
          )}
        </div>
      </div>
      <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition">
        {result.status !== 'passed' && (
          <button
            onClick={() => onUpdate(result.id, 'passed', notes)}
            className="px-2 py-0.5 text-xs rounded bg-green-900 text-green-300 hover:bg-green-800"
            title="Mark passed"
          >Pass</button>
        )}
        {result.status !== 'failed' && (
          <button
            onClick={() => {
              const n = prompt('Failure notes:', notes);
              if (n !== null) onUpdate(result.id, 'failed', n);
            }}
            className="px-2 py-0.5 text-xs rounded bg-red-900 text-red-300 hover:bg-red-800"
            title="Mark failed"
          >Fail</button>
        )}
        {result.status !== 'untested' && (
          <button
            onClick={() => onUpdate(result.id, 'untested', '')}
            className="px-2 py-0.5 text-xs rounded bg-slate-700 text-slate-300 hover:bg-slate-600"
            title="Reset"
          >Reset</button>
        )}
      </div>
    </div>
  );
}

export function TestingStatusCard() {
  const [results, setResults] = useState<TestResult[]>([]);
  const [summary, setSummary] = useState<TestSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState(true);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      const data = await listTestResults();
      setResults(data.results);
      setSummary(data.summary);
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  const handleUpdate = async (id: number, status: string, notes: string) => {
    try {
      await updateTestResult(id, { status, tested_by: 'Andrew', notes });
      await refresh();
    } catch {
      // silent
    }
  };

  // Group by category
  const grouped = results.reduce<Record<string, TestResult[]>>((acc, r) => {
    (acc[r.category] = acc[r.category] || []).push(r);
    return acc;
  }, {});

  const categoryOrder = ['must-pass', 'smoke-test', 'automated'];

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold">🧪 Testing Status</h2>
          {summary && (
            <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${
              summary.ready_for_outreach
                ? 'bg-green-900 text-green-300'
                : summary.failed > 0
                  ? 'bg-red-900 text-red-300'
                  : 'bg-yellow-900 text-yellow-300'
            }`}>
              {summary.ready_for_outreach ? '✅ READY FOR OUTREACH' :
               summary.failed > 0 ? `${summary.failed} FAILING` :
               `${summary.untested} REMAINING`}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button onClick={refresh} className="text-sm text-violet-400 hover:text-violet-300 transition">↻</button>
          <button
            onClick={() => setExpanded(!expanded)}
            className="text-sm text-slate-400 hover:text-slate-200 transition"
          >{expanded ? '▼' : '▶'}</button>
        </div>
      </div>

      {loading ? (
        <div className="flex justify-center py-4">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-slate-700 border-t-violet-500" />
        </div>
      ) : (
        <>
          {/* Summary rings */}
          {summary && (
            <div className="flex justify-around mb-4">
              <div className="flex flex-col items-center">
                <div className={`text-2xl font-bold ${summary.must_pass_passed === summary.must_pass_total && summary.must_pass_total > 0 ? 'text-green-400' : 'text-slate-200'}`}>
                  {summary.must_pass_passed}/{summary.must_pass_total}
                </div>
                <span className="text-xs text-slate-400">Must-Pass</span>
              </div>
              <div className="flex flex-col items-center">
                <div className={`text-2xl font-bold ${summary.smoke_passed >= 3 ? 'text-green-400' : 'text-slate-200'}`}>
                  {summary.smoke_passed}/{summary.smoke_total}
                </div>
                <span className="text-xs text-slate-400">Smoke Tests</span>
              </div>
              <div className="flex flex-col items-center">
                <div className={`text-2xl font-bold ${summary.auto_passed === summary.auto_total && summary.auto_total > 0 ? 'text-green-400' : 'text-slate-200'}`}>
                  {summary.auto_passed}/{summary.auto_total}
                </div>
                <span className="text-xs text-slate-400">Automated</span>
              </div>
              <div className="flex flex-col items-center">
                <div className={`text-2xl font-bold ${summary.passed === summary.total ? 'text-green-400' : 'text-slate-200'}`}>
                  {summary.passed}/{summary.total}
                </div>
                <span className="text-xs text-slate-400">Overall</span>
              </div>
            </div>
          )}

          {/* Progress bar */}
          {summary && (
            <div className="mb-4">
              <div className="h-3 bg-slate-800 rounded-full overflow-hidden flex">
                {summary.passed > 0 && (
                  <div className="bg-green-500 h-full transition-all" style={{ width: `${(summary.passed / summary.total) * 100}%` }} />
                )}
                {summary.failed > 0 && (
                  <div className="bg-red-500 h-full transition-all" style={{ width: `${(summary.failed / summary.total) * 100}%` }} />
                )}
                {summary.skipped > 0 && (
                  <div className="bg-yellow-600 h-full transition-all" style={{ width: `${(summary.skipped / summary.total) * 100}%` }} />
                )}
              </div>
              <div className="flex justify-between text-xs text-slate-500 mt-1">
                <span>{summary.passed} passed · {summary.failed} failed · {summary.untested} untested</span>
                <span>{Math.round((summary.passed / summary.total) * 100)}%</span>
              </div>
            </div>
          )}

          {/* Detailed list */}
          {expanded && (
            <div className="space-y-4 max-h-[500px] overflow-y-auto">
              {categoryOrder.map(cat => {
                const items = grouped[cat];
                if (!items || items.length === 0) return null;
                const catPassed = items.filter(i => i.status === 'passed').length;
                return (
                  <div key={cat}>
                    <div className="flex items-center gap-2 mb-2">
                      <h3 className="text-sm font-medium text-slate-300">
                        {CATEGORY_LABELS[cat] || cat}
                      </h3>
                      <span className="text-xs text-slate-500">{catPassed}/{items.length}</span>
                    </div>
                    <div className="space-y-0.5">
                      {items.map(r => (
                        <TestRow key={r.id} result={r} onUpdate={handleUpdate} />
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </>
      )}
    </div>
  );
}
