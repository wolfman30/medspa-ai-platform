import { useState } from 'react';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

interface SMSSendToolProps {
  orgId: string;
}

interface SendResult {
  success: boolean;
  message: string;
  messageId?: string;
  suppressed?: boolean;
  suppressedReason?: string;
}

export function SMSSendTool({ orgId }: SMSSendToolProps) {
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [body, setBody] = useState('');
  const [purpose, setPurpose] = useState('transactional');
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<SendResult | null>(null);
  const [history, setHistory] = useState<Array<{ to: string; body: string; time: string; success: boolean }>>([]);

  const handleSend = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!from.trim() || !to.trim() || !body.trim()) return;

    setSending(true);
    setResult(null);

    try {
      const token = localStorage.getItem('admin_jwt') || '';
      const { fetchAuthSession } = await import('aws-amplify/auth');
      let authToken = token;
      try {
        const session = await fetchAuthSession();
        const cognitoToken = session.tokens?.idToken?.toString() || session.tokens?.accessToken?.toString();
        if (cognitoToken) authToken = cognitoToken;
      } catch {
        // Use localStorage token
      }

      const res = await fetch(`${API_BASE}/admin/messages:send`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${authToken}`,
        },
        body: JSON.stringify({
          clinic_id: orgId,
          from: from.trim(),
          to: to.trim(),
          body: body.trim(),
          purpose,
        }),
      });

      if (!res.ok) {
        const errText = await res.text();
        setResult({ success: false, message: errText || `Error ${res.status}` });
      } else {
        const data = await res.json();
        const suppressed = data.suppressed || false;
        setResult({
          success: !suppressed,
          message: suppressed
            ? `Suppressed: ${data.suppressed_reason || 'unknown'}`
            : `Sent! Message ID: ${data.message_id || 'n/a'}`,
          messageId: data.message_id,
          suppressed,
          suppressedReason: data.suppressed_reason,
        });
        setHistory(prev => [
          { to: to.trim(), body: body.trim(), time: new Date().toLocaleTimeString(), success: !suppressed },
          ...prev.slice(0, 19),
        ]);
        setBody('');
      }
    } catch (err) {
      setResult({ success: false, message: err instanceof Error ? err.message : 'Network error' });
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="ui-page">
      <div className="ui-container py-8 max-w-2xl">
        <div className="flex items-center gap-3 mb-6">
          <span className="text-2xl">💬</span>
          <div>
            <h1 className="text-xl font-semibold text-slate-900">SMS Send Tool</h1>
            <p className="text-sm text-slate-500">Send test SMS messages via the platform</p>
          </div>
        </div>

        <div className="ui-card ui-card-solid p-6">
          <form onSubmit={handleSend} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label htmlFor="sms-from" className="ui-label">From (E.164)</label>
                <input
                  id="sms-from"
                  type="tel"
                  className="ui-input mt-1"
                  placeholder="+13304600937"
                  value={from}
                  onChange={e => setFrom(e.target.value)}
                  required
                />
              </div>
              <div>
                <label htmlFor="sms-to" className="ui-label">To (E.164)</label>
                <input
                  id="sms-to"
                  type="tel"
                  className="ui-input mt-1"
                  placeholder="+13303339270"
                  value={to}
                  onChange={e => setTo(e.target.value)}
                  required
                />
              </div>
            </div>

            <div>
              <label htmlFor="sms-body" className="ui-label">Message</label>
              <textarea
                id="sms-body"
                className="ui-input mt-1"
                rows={3}
                placeholder="Type your SMS message..."
                value={body}
                onChange={e => setBody(e.target.value)}
                required
                maxLength={1600}
              />
              <p className="text-xs text-slate-400 mt-1">{body.length}/1600 characters</p>
            </div>

            <div>
              <label htmlFor="sms-purpose" className="ui-label">Purpose</label>
              <select
                id="sms-purpose"
                className="ui-select mt-1"
                value={purpose}
                onChange={e => setPurpose(e.target.value)}
              >
                <option value="transactional">Transactional</option>
                <option value="marketing">Marketing</option>
                <option value="test">Test</option>
              </select>
            </div>

            <div className="flex items-center gap-3">
              <button
                type="submit"
                disabled={sending || !from.trim() || !to.trim() || !body.trim()}
                className="ui-btn ui-btn-primary"
              >
                {sending ? 'Sending...' : 'Send SMS'}
              </button>
              <span className="text-xs text-slate-400">Org: {orgId.slice(0, 8)}...</span>
            </div>
          </form>

          {result && (
            <div className={`mt-4 rounded-xl border p-4 ${
              result.success
                ? 'border-emerald-200 bg-emerald-50 text-emerald-800'
                : 'border-red-200 bg-red-50 text-red-800'
            }`}>
              <p className="text-sm font-medium">{result.message}</p>
            </div>
          )}
        </div>

        {history.length > 0 && (
          <div className="mt-6">
            <h2 className="text-sm font-semibold text-slate-700 mb-3">Recent Messages</h2>
            <div className="space-y-2">
              {history.map((msg, i) => (
                <div key={i} className="ui-card p-3 flex items-start gap-3">
                  <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${
                    msg.success ? 'bg-emerald-100 text-emerald-700' : 'bg-red-100 text-red-700'
                  }`}>
                    {msg.success ? 'Sent' : 'Failed'}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-sm text-slate-700 truncate">→ {msg.to}</p>
                    <p className="text-xs text-slate-500 mt-0.5">{msg.body}</p>
                  </div>
                  <span className="text-xs text-slate-400 shrink-0">{msg.time}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
