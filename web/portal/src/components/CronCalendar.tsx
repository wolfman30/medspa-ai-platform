import { useState, useMemo } from 'react';

// ── Agent cron schedule ────────────────────────────────────────────
// This is the source of truth for OpenClaw agent cron jobs.
// Update this list when cron jobs are added/removed/changed.

interface CronJob {
  id: string;
  name: string;
  agent: string;
  schedule: string;        // human-readable schedule
  cronExpr?: string;       // cron expression (for reference)
  timezone: string;
  description: string;
  model?: string;
  enabled: boolean;
  lastNote?: string;       // last known outcome
}

const AGENT_CRONS: CronJob[] = [
  {
    id: 'morning-brief',
    name: 'Morning Market Brief',
    agent: 'Market Research',
    schedule: 'Daily at 3:00 AM ET',
    cronExpr: '0 3 * * *',
    timezone: 'America/New_York',
    description: 'Competitive intelligence scan — med spa AI market, PE/M&A activity, pricing trends.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'deploy-ops',
    name: 'Deploy & Ops Monitor',
    agent: 'Deploy/Ops',
    schedule: 'Every 4 hours',
    cronExpr: '0 */4 * * *',
    timezone: 'UTC',
    description: 'Check CI pipeline, ECS health, CloudWatch errors, API readiness.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'client-success-am',
    name: 'Client Success (Morning)',
    agent: 'Client Success',
    schedule: 'Daily at 10:00 AM ET',
    cronExpr: '0 10 * * *',
    timezone: 'America/New_York',
    description: 'Review overnight conversations, check conversion rates, flag issues.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'client-success-pm',
    name: 'Client Success (Evening)',
    agent: 'Client Success',
    schedule: 'Daily at 6:00 PM ET',
    cronExpr: '0 18 * * *',
    timezone: 'America/New_York',
    description: 'End-of-day conversation review, daily metrics summary.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'sales-9',
    name: 'Sales Outreach (9 AM)',
    agent: 'Sales',
    schedule: 'Weekdays at 9:00 AM ET',
    cronExpr: '0 9 * * 1-5',
    timezone: 'America/New_York',
    description: 'Morning outreach batch — new prospect research and DM drafts.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'sales-12',
    name: 'Sales Outreach (12 PM)',
    agent: 'Sales',
    schedule: 'Weekdays at 12:00 PM ET',
    cronExpr: '0 12 * * 1-5',
    timezone: 'America/New_York',
    description: 'Midday follow-ups — check responses, queue second touches.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'sales-15',
    name: 'Sales Outreach (3 PM)',
    agent: 'Sales',
    schedule: 'Weekdays at 3:00 PM ET',
    cronExpr: '0 15 * * 1-5',
    timezone: 'America/New_York',
    description: 'Afternoon batch — warm lead follow-ups, pipeline updates.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'sales-18',
    name: 'Sales Outreach (6 PM)',
    agent: 'Sales',
    schedule: 'Weekdays at 6:00 PM ET',
    cronExpr: '0 18 * * 1-5',
    timezone: 'America/New_York',
    description: 'End-of-day sales wrap — daily outreach summary for Andrew.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'critic',
    name: 'Critic Agent',
    agent: 'Critic',
    schedule: 'Every 4 hours',
    cronExpr: '0 */4 * * *',
    timezone: 'UTC',
    description: 'Reviews all agent output quality, flags issues.',
    model: 'Claude Sonnet',
    enabled: true,
  },
  {
    id: 'memory-maintenance',
    name: 'Memory Maintenance',
    agent: 'Andre (Main)',
    schedule: 'Sundays at 4:00 AM ET',
    cronExpr: '0 4 * * 0',
    timezone: 'America/New_York',
    description: 'Distill daily logs into MEMORY.md, prune stale entries.',
    model: 'Claude Opus',
    enabled: true,
  },
  {
    id: 'portal-agent',
    name: 'Portal Agent',
    agent: 'Portal Dev',
    schedule: 'Every 4 hours',
    cronExpr: '0 */4 * * *',
    timezone: 'UTC',
    description: 'Check for new backend endpoints needing frontend views, build features.',
    model: 'Claude Opus',
    enabled: true,
  },
  {
    id: 'pr-reviewer',
    name: 'PR Reviewer',
    agent: 'Code Review',
    schedule: 'Every 2 hours',
    cronExpr: '0 */2 * * *',
    timezone: 'UTC',
    description: 'Review open PRs, check Sourcery AI comments, approve or request changes.',
    model: 'Claude Opus',
    enabled: true,
  },
];

const AGENT_COLORS: Record<string, { bg: string; text: string; dot: string }> = {
  'Market Research': { bg: 'bg-purple-50', text: 'text-purple-700', dot: 'bg-purple-400' },
  'Deploy/Ops': { bg: 'bg-orange-50', text: 'text-orange-700', dot: 'bg-orange-400' },
  'Client Success': { bg: 'bg-green-50', text: 'text-green-700', dot: 'bg-green-400' },
  'Sales': { bg: 'bg-blue-50', text: 'text-blue-700', dot: 'bg-blue-400' },
  'Critic': { bg: 'bg-red-50', text: 'text-red-700', dot: 'bg-red-400' },
  'Andre (Main)': { bg: 'bg-slate-50', text: 'text-slate-700', dot: 'bg-slate-400' },
  'Portal Dev': { bg: 'bg-cyan-50', text: 'text-cyan-700', dot: 'bg-cyan-400' },
  'Code Review': { bg: 'bg-amber-50', text: 'text-amber-700', dot: 'bg-amber-400' },
};

type ViewMode = 'list' | 'timeline';

function AgentBadge({ agent }: { agent: string }) {
  const colors = AGENT_COLORS[agent] || { bg: 'bg-slate-50', text: 'text-slate-700', dot: 'bg-slate-400' };
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${colors.bg} ${colors.text}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${colors.dot}`} />
      {agent}
    </span>
  );
}

function CronJobCard({ job }: { job: CronJob }) {
  return (
    <div className={`rounded-xl border p-4 shadow-sm ${job.enabled ? 'border-slate-200 bg-white' : 'border-slate-100 bg-slate-50 opacity-60'}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <h3 className="text-sm font-semibold text-slate-900">{job.name}</h3>
            {!job.enabled && (
              <span className="rounded-full bg-slate-200 px-2 py-0.5 text-xs text-slate-500">Disabled</span>
            )}
          </div>
          <p className="mt-1 text-xs text-slate-500">{job.description}</p>
        </div>
        <AgentBadge agent={job.agent} />
      </div>
      <div className="mt-3 flex items-center gap-4 text-xs text-slate-500">
        <span className="flex items-center gap-1">
          🕐 {job.schedule}
        </span>
        {job.model && (
          <span className="flex items-center gap-1">
            🤖 {job.model}
          </span>
        )}
        {job.cronExpr && (
          <code className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-mono text-slate-400">
            {job.cronExpr}
          </code>
        )}
      </div>
    </div>
  );
}

// ── Timeline View ──────────────────────────────────────────────────

function getHourSlots(): { hour: number; label: string }[] {
  const slots = [];
  for (let h = 0; h < 24; h++) {
    const ampm = h < 12 ? 'AM' : 'PM';
    const display = h === 0 ? 12 : h > 12 ? h - 12 : h;
    slots.push({ hour: h, label: `${display} ${ampm}` });
  }
  return slots;
}

function parseScheduleHours(job: CronJob): number[] {
  // Parse cron expression to get hours (ET)
  if (!job.cronExpr) return [];
  const parts = job.cronExpr.split(' ');
  if (parts.length < 5) return [];
  const hourPart = parts[1];
  if (hourPart === '*') return [];
  if (hourPart.startsWith('*/')) {
    const interval = parseInt(hourPart.slice(2));
    const hours = [];
    // Convert UTC to ET (offset -5 for EST, -4 for EDT)
    const offset = job.timezone === 'America/New_York' ? 0 : -5; // If already ET, no conversion
    for (let h = 0; h < 24; h += interval) {
      hours.push((h + offset + 24) % 24);
    }
    return hours.sort((a, b) => a - b);
  }
  return [parseInt(hourPart)];
}

function TimelineView({ jobs }: { jobs: CronJob[] }) {
  const hours = getHourSlots();
  const enabledJobs = jobs.filter(j => j.enabled);
  
  // Map jobs to their ET hours
  const jobHours = useMemo(() => {
    return enabledJobs.map(job => ({
      job,
      hours: parseScheduleHours(job),
    }));
  }, [enabledJobs]);

  return (
    <div className="overflow-x-auto">
      <div className="min-w-[800px]">
        {/* Hour headers */}
        <div className="flex border-b border-slate-200 pb-2">
          <div className="w-36 shrink-0 text-xs font-medium text-slate-500 pr-2">Agent</div>
          <div className="flex flex-1">
            {hours.map(({ hour, label }) => (
              <div key={hour} className="flex-1 text-center text-[10px] text-slate-400">
                {hour % 3 === 0 ? label : ''}
              </div>
            ))}
          </div>
        </div>
        
        {/* Job rows */}
        {jobHours.map(({ job, hours: activeHours }) => {
          const colors = AGENT_COLORS[job.agent] || { bg: 'bg-slate-50', text: 'text-slate-700', dot: 'bg-slate-400' };
          return (
            <div key={job.id} className="flex items-center border-b border-slate-100 py-2">
              <div className="w-36 shrink-0 pr-2">
                <div className="text-xs font-medium text-slate-700 truncate">{job.name}</div>
                <div className="text-[10px] text-slate-400 truncate">{job.agent}</div>
              </div>
              <div className="flex flex-1">
                {getHourSlots().map(({ hour }) => {
                  const isActive = activeHours.includes(hour);
                  return (
                    <div key={hour} className="flex-1 flex justify-center">
                      {isActive ? (
                        <div className={`h-3 w-3 rounded-full ${colors.dot}`} title={`${job.name} — ${hour}:00 ET`} />
                      ) : (
                        <div className="h-3 w-3" />
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── Main Component ─────────────────────────────────────────────────

export function CronCalendar() {
  const [viewMode, setViewMode] = useState<ViewMode>('list');
  const [filterAgent, setFilterAgent] = useState<string>('all');

  const agents = useMemo(() => {
    const set = new Set(AGENT_CRONS.map(j => j.agent));
    return Array.from(set).sort();
  }, []);

  const filteredJobs = useMemo(() => {
    if (filterAgent === 'all') return AGENT_CRONS;
    return AGENT_CRONS.filter(j => j.agent === filterAgent);
  }, [filterAgent]);

  const enabledCount = AGENT_CRONS.filter(j => j.enabled).length;
  const uniqueAgents = new Set(AGENT_CRONS.filter(j => j.enabled).map(j => j.agent)).size;

  return (
    <div className="ui-page">
      <div className="ui-container py-6 space-y-6">
        {/* Header */}
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-xl font-bold tracking-tight text-slate-900">Cron Calendar</h1>
            <p className="mt-1 text-sm text-slate-500">
              {enabledCount} active jobs across {uniqueAgents} agents
            </p>
          </div>
          <div className="flex items-center gap-2">
            <select
              value={filterAgent}
              onChange={(e) => setFilterAgent(e.target.value)}
              className="ui-select text-sm"
            >
              <option value="all">All Agents</option>
              {agents.map(a => (
                <option key={a} value={a}>{a}</option>
              ))}
            </select>
            <div className="flex rounded-lg border border-slate-200 bg-white">
              <button
                onClick={() => setViewMode('list')}
                className={`px-3 py-1.5 text-xs font-medium rounded-l-lg ${viewMode === 'list' ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-50'}`}
              >
                List
              </button>
              <button
                onClick={() => setViewMode('timeline')}
                className={`px-3 py-1.5 text-xs font-medium rounded-r-lg ${viewMode === 'timeline' ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-50'}`}
              >
                Timeline
              </button>
            </div>
          </div>
        </div>

        {/* Content */}
        {viewMode === 'list' ? (
          <div className="grid gap-3 sm:grid-cols-2">
            {filteredJobs.map(job => (
              <CronJobCard key={job.id} job={job} />
            ))}
          </div>
        ) : (
          <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
            <TimelineView jobs={filteredJobs} />
          </div>
        )}

        {/* Legend */}
        <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
          <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">Agent Legend</h3>
          <div className="flex flex-wrap gap-3">
            {agents.map(agent => (
              <AgentBadge key={agent} agent={agent} />
            ))}
          </div>
          <p className="mt-3 text-xs text-slate-400">
            All times shown in Eastern Time (ET). Update the schedule in <code className="bg-slate-100 px-1 rounded">CronCalendar.tsx</code> when cron jobs change.
          </p>
        </div>
      </div>
    </div>
  );
}
