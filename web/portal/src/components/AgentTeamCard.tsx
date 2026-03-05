import { useEffect, useState } from 'react';
import { listAgents, type AgentStatus } from '../api/client';

export function AgentTeamCard() {
  const [agents, setAgents] = useState<AgentStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    listAgents()
      .then(data => setAgents(data.agents || []))
      .catch(err => setError(err instanceof Error ? err.message : 'Failed to load'))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="min-h-[400px] flex items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-700 border-t-violet-500" />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 p-4 sm:p-8">
      <div className="max-w-7xl mx-auto space-y-6">
        {/* Mission Banner */}
        <div className="rounded-xl border border-violet-700/50 bg-gradient-to-r from-violet-950/60 to-slate-900 p-5">
          <p className="text-base sm:text-lg font-semibold text-violet-200 leading-relaxed">
            🐺 Mission: Land 5 paying med spa clients at $500–$1,000/mo within 90 days.
            Prove ROI. Build toward $50M exit by May 2027.
          </p>
        </div>

        {/* Header */}
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Agent Team</h1>
          <p className="text-sm text-slate-400 mt-1">
            {agents.length} agents &middot; {agents.filter(a => a.enabled).length} active
          </p>
        </div>

        {error && (
          <div className="rounded-lg border border-red-800 bg-red-950 p-3 text-sm text-red-300">{error}</div>
        )}

        {/* Agent Cards Grid */}
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {agents.map(agent => (
            <div
              key={agent.id}
              className="rounded-xl border border-slate-800 bg-slate-900 p-5 flex flex-col gap-3 hover:border-slate-700 transition"
            >
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-2xl">{agent.emoji}</span>
                  <h3 className="text-base font-semibold text-slate-100">{agent.name}</h3>
                </div>
                <span
                  className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${
                    agent.enabled
                      ? 'bg-green-950 text-green-400 border border-green-800'
                      : 'bg-slate-800 text-slate-500 border border-slate-700'
                  }`}
                >
                  <span className={`h-1.5 w-1.5 rounded-full ${agent.enabled ? 'bg-green-400' : 'bg-slate-500'}`} />
                  {agent.enabled ? 'Active' : 'Disabled'}
                </span>
              </div>

              <p className="text-sm text-slate-400">{agent.role}</p>

              <div className="flex flex-wrap gap-1.5">
                {agent.goals.map((goal, i) => (
                  <span
                    key={i}
                    className="inline-block rounded-md bg-violet-950/60 border border-violet-800/40 px-2 py-0.5 text-xs text-violet-300"
                  >
                    {goal}
                  </span>
                ))}
              </div>

              <div className="mt-auto pt-2 border-t border-slate-800 flex items-center gap-2 text-xs text-slate-500">
                <span>⏱</span>
                <span>{agent.schedule}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
