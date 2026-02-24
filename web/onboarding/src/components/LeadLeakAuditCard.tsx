import { useState, useEffect } from 'react';

interface AuditChannel {
  channel: string;
  grade: string;
  notes: string;
}

interface LeadLeakAudit {
  id: string;
  clinic: string;
  owner: string;
  phone: string;
  overallGrade: string;
  leakScore: string;
  missedLeadsLow: number;
  missedLeadsHigh: number;
  lostRevenueLow: number;
  lostRevenueHigh: number;
  channels: AuditChannel[];
  topFixes: string[];
  auditDate: string;
}

const AUDITS: LeadLeakAudit[] = [
  {
    id: 'forever-22',
    clinic: 'Forever 22 Med Spa',
    owner: 'Brandi Sesock',
    phone: '(440) 703-1022',
    overallGrade: 'C-',
    leakScore: 'C-',
    missedLeadsLow: 17,
    missedLeadsHigh: 29,
    lostRevenueLow: 6800,
    lostRevenueHigh: 11600,
    channels: [
      { channel: 'Google Reviews', grade: 'A', notes: '5.0 stars, 118 reviews, no complaints' },
      { channel: 'Instagram', grade: 'D', notes: '619 followers, low output, inverted ratio' },
      { channel: 'Website Booking', grade: 'C+', notes: 'Moxie booking works, only path' },
      { channel: 'Website Contact', grade: 'F', notes: 'Contact page returns 404' },
      { channel: 'Chat/Messaging', grade: 'F', notes: 'No chat widget, no contact form' },
    ],
    topFixes: [
      'Fix broken contact page (404) — costing thousands/month',
      'Add chat widget or text-to-book option',
      'Instagram strategy: 3 Reels/week, before/afters',
    ],
    auditDate: '2026-02-24',
  },
  {
    id: 'brilliant-aesthetics',
    clinic: 'Brilliant Aesthetics',
    owner: 'Kimberly Enochs-Smith',
    phone: '(440) 732-5929',
    overallGrade: 'D-',
    leakScore: 'D-',
    missedLeadsLow: 28,
    missedLeadsHigh: 44,
    lostRevenueLow: 11200,
    lostRevenueHigh: 17600,
    channels: [
      { channel: 'Google Reviews', grade: 'F', notes: 'Near-zero Google review presence' },
      { channel: 'Instagram', grade: 'D-', notes: 'Wrong handle circulating, low visibility' },
      { channel: 'Website Booking', grade: 'D', notes: 'Fresha exists but poorly integrated' },
      { channel: 'Website Quality', grade: 'F', notes: 'Broken template with raw JSON visible' },
      { channel: 'Chat/Messaging', grade: 'F', notes: 'No chat widget' },
    ],
    topFixes: [
      'Fix website NOW — raw JSON code visible on homepage',
      'Launch Google review campaign (goal: 25+ in 30 days)',
      'Consolidate Instagram handle + add booking link in bio',
    ],
    auditDate: '2026-02-24',
  },
  {
    id: 'cru-clinic',
    clinic: 'The Cru Clinic',
    owner: 'Owner TBD',
    phone: '(419) 775-5457',
    overallGrade: 'C+',
    leakScore: 'C+',
    missedLeadsLow: 22,
    missedLeadsHigh: 35,
    lostRevenueLow: 8800,
    lostRevenueHigh: 14000,
    channels: [
      { channel: 'Google Reviews', grade: 'A', notes: '5.0 stars, 119 reviews' },
      { channel: 'Instagram', grade: 'B', notes: '3K followers, 1,574 posts, consistent' },
      { channel: 'Website Booking', grade: 'D', notes: 'No visible online booking system' },
      { channel: 'Website Design', grade: 'C-', notes: 'SEO text wall, no visual hierarchy' },
      { channel: 'Chat/Messaging', grade: 'F', notes: 'No chat widget' },
    ],
    topFixes: [
      'Add online booking with prominent "Book Now" button',
      'Redesign homepage for conversion (not SEO text wall)',
      'Add chat widget or SMS opt-in',
    ],
    auditDate: '2026-02-24',
  },
];

function gradeColor(grade: string): string {
  if (grade.startsWith('A')) return 'text-green-400';
  if (grade.startsWith('B')) return 'text-blue-400';
  if (grade.startsWith('C')) return 'text-yellow-400';
  if (grade.startsWith('D')) return 'text-orange-400';
  return 'text-red-400';
}

function gradeBg(grade: string): string {
  if (grade.startsWith('A')) return 'bg-green-950 border-green-800';
  if (grade.startsWith('B')) return 'bg-blue-950 border-blue-800';
  if (grade.startsWith('C')) return 'bg-yellow-950 border-yellow-800';
  if (grade.startsWith('D')) return 'bg-orange-950 border-orange-800';
  return 'bg-red-950 border-red-800';
}

function AuditDetail({ audit, onClose }: { audit: LeadLeakAudit; onClose: () => void }) {
  return (
    <div className="fixed inset-0 bg-black/70 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-slate-900 border border-slate-700 rounded-2xl max-w-2xl w-full max-h-[85vh] overflow-y-auto p-6" onClick={e => e.stopPropagation()}>
        <div className="flex justify-between items-start mb-4">
          <div>
            <h2 className="text-xl font-bold">{audit.clinic}</h2>
            <p className="text-sm text-slate-400">{audit.owner} · {audit.phone}</p>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-white text-2xl">×</button>
        </div>

        {/* Overall Grade */}
        <div className={`rounded-xl border p-4 mb-4 ${gradeBg(audit.overallGrade)}`}>
          <div className="flex items-center justify-between">
            <span className="text-sm text-slate-300">Overall Leak Score</span>
            <span className={`text-3xl font-bold ${gradeColor(audit.overallGrade)}`}>{audit.overallGrade}</span>
          </div>
          <div className="mt-2 grid grid-cols-2 gap-4">
            <div>
              <div className="text-xs text-slate-500">Missed Leads/Mo</div>
              <div className="text-lg font-bold">{audit.missedLeadsLow}–{audit.missedLeadsHigh}</div>
            </div>
            <div>
              <div className="text-xs text-slate-500">Lost Revenue/Mo</div>
              <div className="text-lg font-bold text-red-400">
                ${audit.lostRevenueLow.toLocaleString()}–${audit.lostRevenueHigh.toLocaleString()}
              </div>
            </div>
          </div>
        </div>

        {/* Channel Breakdown */}
        <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wide mb-2">Channel Grades</h3>
        <div className="space-y-2 mb-4">
          {audit.channels.map(ch => (
            <div key={ch.channel} className="flex items-center gap-3 p-2 rounded-lg bg-slate-800/50">
              <span className={`text-lg font-bold w-8 text-center ${gradeColor(ch.grade)}`}>{ch.grade}</span>
              <div className="flex-1">
                <div className="text-sm font-medium text-slate-200">{ch.channel}</div>
                <div className="text-xs text-slate-400">{ch.notes}</div>
              </div>
            </div>
          ))}
        </div>

        {/* Top Fixes */}
        <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wide mb-2">Top 3 Fixes</h3>
        <div className="space-y-2">
          {audit.topFixes.map((fix, i) => (
            <div key={i} className="flex items-start gap-2 p-2 rounded-lg bg-slate-800/50">
              <span className="text-violet-400 font-bold">{i + 1}.</span>
              <span className="text-sm text-slate-300">{fix}</span>
            </div>
          ))}
        </div>

        <p className="text-xs text-slate-600 mt-4">Audited {audit.auditDate} · Public data only</p>
      </div>
    </div>
  );
}

export function LeadLeakAuditCard() {
  const [selectedAudit, setSelectedAudit] = useState<LeadLeakAudit | null>(null);

  const totalMissedLow = AUDITS.reduce((s, a) => s + a.missedLeadsLow, 0);
  const totalMissedHigh = AUDITS.reduce((s, a) => s + a.missedLeadsHigh, 0);
  const totalLostLow = AUDITS.reduce((s, a) => s + a.lostRevenueLow, 0);
  const totalLostHigh = AUDITS.reduce((s, a) => s + a.lostRevenueHigh, 0);

  return (
    <>
      <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold">🔍 Lead Leak Audits</h2>
          <div className="text-xs text-slate-500">{AUDITS.length} clinics audited</div>
        </div>

        {/* Summary stats */}
        <div className="grid grid-cols-2 gap-3 mb-4">
          <div className="rounded-lg bg-slate-800/50 p-3">
            <div className="text-xs text-slate-500">Total Missed Leads/Mo</div>
            <div className="text-xl font-bold text-orange-400">{totalMissedLow}–{totalMissedHigh}</div>
          </div>
          <div className="rounded-lg bg-slate-800/50 p-3">
            <div className="text-xs text-slate-500">Total Lost Revenue/Mo</div>
            <div className="text-xl font-bold text-red-400">
              ${totalLostLow.toLocaleString()}–${totalLostHigh.toLocaleString()}
            </div>
          </div>
        </div>

        {/* Clinic list */}
        <div className="space-y-2">
          {AUDITS.map(audit => (
            <button
              key={audit.id}
              onClick={() => setSelectedAudit(audit)}
              className="w-full flex items-center gap-3 p-3 rounded-lg bg-slate-800/30 hover:bg-slate-800 transition text-left cursor-pointer"
            >
              <span className={`text-2xl font-bold w-10 text-center ${gradeColor(audit.overallGrade)}`}>
                {audit.overallGrade}
              </span>
              <div className="flex-1 min-w-0">
                <div className="text-sm font-medium text-slate-200 truncate">{audit.clinic}</div>
                <div className="text-xs text-slate-400">{audit.owner}</div>
              </div>
              <div className="text-right">
                <div className="text-xs text-slate-500">Lost/mo</div>
                <div className="text-sm font-mono text-red-400">
                  ${audit.lostRevenueLow.toLocaleString()}+
                </div>
              </div>
              <span className="text-slate-600">›</span>
            </button>
          ))}
        </div>

        <p className="text-xs text-slate-600 mt-3 text-center">Click a clinic for full audit · Use in sales calls</p>
      </div>

      {selectedAudit && <AuditDetail audit={selectedAudit} onClose={() => setSelectedAudit(null)} />}
    </>
  );
}
