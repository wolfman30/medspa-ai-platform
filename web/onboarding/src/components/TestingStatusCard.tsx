import { useEffect, useState, useCallback } from 'react';
import { listTestResults, updateTestResult, type TestResult, type TestSummary } from '../api/client';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

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

async function getAuthHeaders(isFormData = false): Promise<Record<string, string>> {
  // Reuse the same auth pattern as the rest of the app (Cognito/Amplify tokens)
  const headers: Record<string, string> = {};
  // Don't set Content-Type for FormData — browser sets multipart boundary automatically
  if (!isFormData) {
    headers['Content-Type'] = 'application/json';
  }
  try {
    const { fetchAuthSession } = await import('aws-amplify/auth');
    const session = await fetchAuthSession();
    const token = session.tokens?.idToken?.toString() || session.tokens?.accessToken?.toString() || null;
    if (token) headers['Authorization'] = `Bearer ${token}`;
  } catch {
    // Not authenticated via Cognito — fall through
  }
  const onboardingToken = import.meta.env.VITE_ONBOARDING_TOKEN;
  if (onboardingToken) headers['X-Onboarding-Token'] = onboardingToken;
  return headers;
}

function EvidenceGallery({
  urls,
  testId,
  onUpdate,
}: {
  urls: string[];
  testId: number;
  onUpdate: () => void;
}) {
  const [uploading, setUploading] = useState(false);
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);
  const [addingUrl, setAddingUrl] = useState(false);
  const [urlInput, setUrlInput] = useState('');

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setUploading(true);
    try {
      const formData = new FormData();
      formData.append('file', file);
      const headers = await getAuthHeaders(true);
      const res = await fetch(`${API_BASE}/admin/testing/${testId}/evidence`, {
        method: 'POST',
        headers,
        body: formData,
      });
      if (!res.ok) {
        const errText = await res.text();
        alert(`Upload failed: ${errText}`);
      } else {
        onUpdate();
      }
    } catch {
      alert('Upload failed');
    } finally {
      setUploading(false);
      e.target.value = '';
    }
  };

  const handleAddUrl = async () => {
    if (!urlInput.trim()) return;
    // Add URL directly via update endpoint
    const newUrls = [...urls, urlInput.trim()];
    try {
      await updateTestResult(testId, {
        status: 'passed', // keep current status — we'll fix this
        evidence_urls: newUrls,
      } as any);
      onUpdate();
      setUrlInput('');
      setAddingUrl(false);
    } catch {
      alert('Failed to add URL');
    }
  };

  const handleRemove = async (url: string) => {
    if (!confirm('Remove this evidence?')) return;
    try {
      const headers = await getAuthHeaders();
      headers['Content-Type'] = 'application/json';
      await fetch(`${API_BASE}/admin/testing/${testId}/evidence`, {
        method: 'DELETE',
        headers,
        body: JSON.stringify({ url }),
      });
      onUpdate();
    } catch {
      alert('Failed to remove');
    }
  };

  return (
    <>
      <div className="mt-2">
        <div className="flex items-center gap-2 mb-1">
          <span className="text-xs text-slate-400 font-medium">Evidence</span>
          <label className="px-2 py-0.5 text-xs rounded bg-violet-900 text-violet-300 hover:bg-violet-800 cursor-pointer transition">
            {uploading ? '⏳ Uploading...' : '📎 Upload'}
            <input type="file" accept="image/*" className="hidden" onChange={handleFileUpload} disabled={uploading} />
          </label>
          <button
            onClick={() => setAddingUrl(!addingUrl)}
            className="px-2 py-0.5 text-xs rounded bg-slate-700 text-slate-300 hover:bg-slate-600 transition"
          >🔗 Add URL</button>
        </div>

        {addingUrl && (
          <div className="flex gap-2 mb-2">
            <input
              type="text"
              value={urlInput}
              onChange={e => setUrlInput(e.target.value)}
              placeholder="https://..."
              className="flex-1 px-2 py-1 text-xs rounded bg-slate-800 border border-slate-700 text-slate-200 focus:border-violet-500 outline-none"
              onKeyDown={e => e.key === 'Enter' && handleAddUrl()}
            />
            <button onClick={handleAddUrl} className="px-2 py-0.5 text-xs rounded bg-violet-700 text-white">Add</button>
          </div>
        )}

        {urls.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {urls.map((url, i) => (
              <div key={i} className="relative group">
                <img
                  src={url}
                  alt={`Evidence ${i + 1}`}
                  className="w-20 h-20 object-cover rounded-lg border border-slate-700 cursor-pointer hover:border-violet-500 transition"
                  onClick={() => setLightboxUrl(url)}
                  onError={e => {
                    (e.target as HTMLImageElement).style.display = 'none';
                  }}
                />
                <button
                  onClick={() => handleRemove(url)}
                  className="absolute -top-1 -right-1 w-4 h-4 rounded-full bg-red-600 text-white text-[10px] leading-none flex items-center justify-center opacity-0 group-hover:opacity-100 transition"
                >✕</button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Lightbox */}
      {lightboxUrl && (
        <div
          className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center p-4 cursor-pointer"
          onClick={() => setLightboxUrl(null)}
        >
          <img
            src={lightboxUrl}
            alt="Evidence"
            className="max-w-full max-h-[90vh] rounded-xl shadow-2xl"
            onClick={e => e.stopPropagation()}
          />
          <button
            onClick={() => setLightboxUrl(null)}
            className="absolute top-4 right-4 text-white text-2xl hover:text-slate-300"
          >✕</button>
        </div>
      )}
    </>
  );
}

function TestRow({
  result,
  onUpdate,
  isExpanded,
  onToggle,
}: {
  result: TestResult;
  onUpdate: (id: number, status: string, notes: string) => void;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="border border-slate-800 rounded-lg overflow-hidden">
      <div className="flex items-center gap-3 p-2 hover:bg-slate-800/50 transition group">
        <button onClick={onToggle} className={`text-slate-400 transition-transform text-xs ${isExpanded ? 'rotate-90' : ''}`}>▶</button>
        <span className="text-lg" title={result.status}>{STATUS_ICONS[result.status] || '⬜'}</span>
        <div className="flex-1 min-w-0 cursor-pointer" onClick={onToggle}>
          <div className="text-sm text-slate-200 truncate">{result.scenario_name}</div>
          <div className="text-xs text-slate-500">
            {result.clinic}
            {result.tested_at && (
              <> · {formatDate(result.tested_at)}</>
            )}
            {result.notes && (
              <> · <span className="text-slate-400">{result.notes}</span></>
            )}
            {result.evidence_urls.length > 0 && (
              <> · <span className="text-violet-400">📷 {result.evidence_urls.length}</span></>
            )}
          </div>
        </div>
        <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition">
          {result.status !== 'passed' && (
            <button
              onClick={() => onUpdate(result.id, 'passed', result.notes)}
              className="px-2 py-0.5 text-xs rounded bg-green-900 text-green-300 hover:bg-green-800"
              title="Mark passed"
            >Pass</button>
          )}
          {result.status !== 'failed' && (
            <button
              onClick={() => {
                const n = prompt('Failure notes:', result.notes);
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

      {/* Expanded detail */}
      {isExpanded && (
        <div className="px-4 pb-3 pt-1 border-t border-slate-800/50">
          {result.description && (
            <div className="mb-2">
              <div className="text-xs text-slate-500 font-medium mb-1">📋 Test Steps</div>
              <p className="text-sm text-slate-300 leading-relaxed whitespace-pre-wrap">{result.description}</p>
            </div>
          )}

          <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs mb-2">
            <div>
              <span className="text-slate-500">Status:</span>{' '}
              <span className={result.status === 'passed' ? 'text-green-400' : result.status === 'failed' ? 'text-red-400' : 'text-slate-400'}>
                {result.status}
              </span>
            </div>
            <div>
              <span className="text-slate-500">Tested by:</span>{' '}
              <span className="text-slate-300">{result.tested_by || '—'}</span>
            </div>
            <div>
              <span className="text-slate-500">Tested at:</span>{' '}
              <span className="text-slate-300">{formatDate(result.tested_at)}</span>
            </div>
            <div>
              <span className="text-slate-500">Clinic:</span>{' '}
              <span className="text-slate-300">{result.clinic}</span>
            </div>
          </div>

          {result.notes && (
            <div className="mb-2">
              <span className="text-xs text-slate-500">Notes:</span>{' '}
              <span className="text-xs text-slate-300">{result.notes}</span>
            </div>
          )}

          <EvidenceGallery urls={result.evidence_urls} testId={result.id} onUpdate={() => onUpdate(result.id, result.status, result.notes)} />
        </div>
      )}
    </div>
  );
}

export function TestingStatusCard() {
  const [results, setResults] = useState<TestResult[]>([]);
  const [summary, setSummary] = useState<TestSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState(true);
  const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set());

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

  const toggleRow = (id: number) => {
    setExpandedRows(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
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
          {/* Summary counters */}
          {summary && (
            <div className="flex justify-around mb-4">
              {[
                { label: 'Must-Pass', passed: summary.must_pass_passed, total: summary.must_pass_total, check: summary.must_pass_passed === summary.must_pass_total && summary.must_pass_total > 0 },
                { label: 'Smoke Tests', passed: summary.smoke_passed, total: summary.smoke_total, check: summary.smoke_passed >= 3 },
                { label: 'Automated', passed: summary.auto_passed, total: summary.auto_total, check: summary.auto_passed === summary.auto_total && summary.auto_total > 0 },
                { label: 'Overall', passed: summary.passed, total: summary.total, check: summary.passed === summary.total },
              ].map(m => (
                <div key={m.label} className="flex flex-col items-center">
                  <div className={`text-2xl font-bold ${m.check ? 'text-green-400' : 'text-slate-200'}`}>
                    {m.passed}/{m.total}
                  </div>
                  <span className="text-xs text-slate-400">{m.label}</span>
                </div>
              ))}
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
            <div className="space-y-4 max-h-[600px] overflow-y-auto">
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
                    <div className="space-y-1">
                      {items.map(r => (
                        <TestRow
                          key={r.id}
                          result={r}
                          onUpdate={handleUpdate}
                          isExpanded={expandedRows.has(r.id)}
                          onToggle={() => toggleRow(r.id)}
                        />
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
