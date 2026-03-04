import { useState } from 'react';

interface ContentIdea {
  id: number;
  type: 'reel' | 'carousel' | 'static' | 'story';
  title: string;
  caption: string;
  status: 'idea' | 'creating' | 'ready' | 'posted';
  priority: number;
  notes: string;
}

const TYPE_BADGE: Record<string, { label: string; emoji: string; cls: string }> = {
  reel: { label: 'Reel', emoji: '🎬', cls: 'bg-pink-100 text-pink-800 border-pink-200' },
  carousel: { label: 'Carousel', emoji: '📸', cls: 'bg-blue-100 text-blue-800 border-blue-200' },
  static: { label: 'Static', emoji: '🖼️', cls: 'bg-green-100 text-green-800 border-green-200' },
  story: { label: 'Story', emoji: '⏱️', cls: 'bg-yellow-100 text-yellow-800 border-yellow-200' },
};

const STATUS_COLS = [
  { key: 'idea', label: '💡 Ideas', color: 'border-slate-300' },
  { key: 'creating', label: '✏️ Creating', color: 'border-blue-300' },
  { key: 'ready', label: '✅ Ready to Post', color: 'border-green-300' },
  { key: 'posted', label: '📤 Posted', color: 'border-violet-300' },
] as const;

const INITIAL_CONTENT: ContentIdea[] = [
  {
    id: 1,
    type: 'reel',
    title: 'Demo Screen Recording',
    caption: 'This is what happens when a patient calls your med spa and nobody picks up.\n\nOur AI texts them back in under 5 seconds, qualifies them, checks your REAL calendar, and books the appointment — all via text.\n\nNo staff time. No missed revenue. 24/7.\n\n#medspa #medspas #medspabusiness #aireceptionist #missedcalls #patientbooking',
    status: 'idea',
    priority: 1,
    notes: 'Record the demo at aiwolfsolutions.com/demo on your phone. Keep it under 30 seconds. Show steps 5-8 (availability → book → confirm).',
  },
  {
    id: 2,
    type: 'static',
    title: 'Pain Stat: 62% Calls Unanswered',
    caption: '62% of calls to med spas go unanswered.\n\nThat\'s not a staffing problem.\nThat\'s a revenue problem.\n\nEvery missed call is a patient choosing your competitor instead.\n\nWhat if every call got answered — automatically?\n\n#medspa #medspabusiness #missedcalls #revenue #aesthetics',
    status: 'idea',
    priority: 2,
    notes: 'Create in Canva. Bold text on dark background. Navy + gold to match brand.',
  },
  {
    id: 3,
    type: 'carousel',
    title: 'My Story: Why I Built This',
    caption: 'I\'m a software engineer and father of twins.\n\nI noticed something while researching med spas: most clinics miss MORE calls than they answer.\n\nEvery missed call = a patient who Googled your clinic, was ready to book, and got voicemail.\n\nSo I built an AI that fixes that. It texts back missed calls in under 5 seconds, qualifies the patient, checks real availability, and books the appointment.\n\nNo app to download. No portal to learn. Just SMS.\n\nWe\'re accepting our first 10 founding clients at $1,000/mo with zero setup fee.\n\nLink in bio.\n\n#founder #startup #medspa #ai #softwaredeveloper',
    status: 'idea',
    priority: 3,
    notes: 'Slide 1: Photo of you. Slide 2: The problem (stats). Slide 3: The solution (demo screenshot). Slide 4: CTA.',
  },
  {
    id: 4,
    type: 'static',
    title: 'Before/After: Without AI vs With AI',
    caption: 'WITHOUT AI:\n📞 Missed call → Voicemail → Patient books elsewhere\n\nWITH AI:\n📞 Missed call → Instant text in 5 sec → Qualified → Booked in 3 min\n\nSame patient. Different outcome.\n\n#medspa #aireceptionist #patientexperience #booking',
    status: 'idea',
    priority: 4,
    notes: 'Split screen graphic. Left side: red/negative. Right side: green/positive.',
  },
  {
    id: 5,
    type: 'reel',
    title: 'Demo: Availability + Booking (15 sec)',
    caption: 'Real-time availability check → Patient picks a time → Deposit collected → Booked.\n\nAll via text. All automatic. All in under 3 minutes.\n\nThis is MedSpa Concierge.\n\n#medspa #booking #ai #automation',
    status: 'idea',
    priority: 5,
    notes: 'Crop the demo recording to just steps 5-8. Add trending audio.',
  },
  {
    id: 6,
    type: 'static',
    title: 'Revenue Lost: $150K/Year',
    caption: 'The average med spa loses $150K+ per year from missed calls.\n\nNot bad marketing.\nNot bad service.\nJust... nobody picked up the phone.\n\nFix it for $1,000/mo. The math is obvious.\n\n#medspa #revenue #missedcalls #roi',
    status: 'idea',
    priority: 6,
    notes: 'Calculator-style graphic. Show: 10 missed calls/day × $300 avg treatment × 20% conversion = $150K+ lost.',
  },
  {
    id: 7,
    type: 'static',
    title: 'Founding Client Offer',
    caption: '🐺 Now accepting founding clients.\n\n$1,000/mo. Setup waived.\n\nYour AI receptionist:\n✅ Answers missed calls instantly\n✅ Qualifies patients via text\n✅ Checks real availability\n✅ Books appointments automatically\n✅ Collects deposits\n\n10 spots. First come, first served.\n\naiwolfsolutions.com\n\n#medspa #saas #foundingclient #ai',
    status: 'idea',
    priority: 7,
    notes: 'Wolf logo background. Gold text. Premium feel.',
  },
  {
    id: 8,
    type: 'carousel',
    title: '5 Things That Happen When You Miss a Call',
    caption: 'Every missed call triggers a chain reaction:\n\n1️⃣ Patient Googles your competitor\n2️⃣ Competitor answers immediately\n3️⃣ Patient books with them\n4️⃣ They become a repeat client... somewhere else\n5️⃣ You never even knew they called\n\nStop the chain. Answer every call — automatically.\n\nLink in bio.\n\n#medspa #missedcalls #competition #patientretention',
    status: 'idea',
    priority: 8,
    notes: '5 slides, one point each. Bold numbers. End with CTA slide.',
  },
  {
    id: 9,
    type: 'reel',
    title: 'Full Demo Walkthrough (30 sec)',
    caption: 'From missed call to booked appointment in under 3 minutes.\n\nNo human involved. No app needed. Just SMS.\n\nThis is what AI-powered med spa booking looks like.\n\naiwolfsolutions.com/demo\n\n#medspa #ai #demo #booking #saas',
    status: 'idea',
    priority: 9,
    notes: 'Full demo walkthrough. Voiceover optional. Add captions for muted viewing.',
  },
];

export function ContentCalendar() {
  const [content, setContent] = useState<ContentIdea[]>(INITIAL_CONTENT);
  const [expanded, setExpanded] = useState<number | null>(null);

  const moveContent = (id: number, newStatus: ContentIdea['status']) => {
    setContent((prev) =>
      prev.map((c) => (c.id === id ? { ...c, status: newStatus } : c))
    );
  };

  const statusOrder: ContentIdea['status'][] = ['idea', 'creating', 'ready', 'posted'];
  const nextStatus = (current: ContentIdea['status']): ContentIdea['status'] | null => {
    const idx = statusOrder.indexOf(current);
    return idx < statusOrder.length - 1 ? statusOrder[idx + 1] : null;
  };

  return (
    <div className="ui-container py-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-slate-900">📱 Content Calendar</h1>
          <p className="text-sm text-slate-500 mt-1">Instagram content pipeline for @aiwolfsolutions</p>
        </div>
        <div className="flex gap-3 text-sm">
          <span className="px-2 py-1 bg-slate-100 rounded-lg font-medium">
            {content.filter((c) => c.status === 'idea').length} ideas
          </span>
          <span className="px-2 py-1 bg-blue-100 rounded-lg font-medium text-blue-800">
            {content.filter((c) => c.status === 'creating').length} creating
          </span>
          <span className="px-2 py-1 bg-green-100 rounded-lg font-medium text-green-800">
            {content.filter((c) => c.status === 'ready').length} ready
          </span>
          <span className="px-2 py-1 bg-violet-100 rounded-lg font-medium text-violet-800">
            {content.filter((c) => c.status === 'posted').length} posted
          </span>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        {STATUS_COLS.map((col) => {
          const items = content
            .filter((c) => c.status === col.key)
            .sort((a, b) => a.priority - b.priority);
          return (
            <div key={col.key} className={`rounded-xl border-2 ${col.color} bg-white p-3`}>
              <h3 className="text-sm font-semibold text-slate-700 mb-3 px-1">
                {col.label} <span className="text-slate-400">({items.length})</span>
              </h3>
              <div className="space-y-2">
                {items.map((item) => {
                  const badge = TYPE_BADGE[item.type];
                  const next = nextStatus(item.status);
                  const isExpanded = expanded === item.id;
                  return (
                    <div
                      key={item.id}
                      className="bg-white border border-slate-200 rounded-xl p-3 shadow-sm hover:shadow-md transition-shadow cursor-pointer"
                      onClick={() => setExpanded(isExpanded ? null : item.id)}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <span className={`text-[10px] font-semibold px-1.5 py-0.5 rounded border ${badge.cls} mr-1`}>
                            {badge.emoji} {badge.label}
                          </span>
                          <h4 className="text-sm font-medium text-slate-900 mt-1 leading-snug">{item.title}</h4>
                        </div>
                        <span className="text-xs text-slate-400">#{item.priority}</span>
                      </div>

                      {isExpanded && (
                        <div className="mt-3 space-y-3">
                          <div>
                            <p className="text-[11px] font-semibold text-slate-500 uppercase mb-1">Caption</p>
                            <pre className="text-xs text-slate-700 whitespace-pre-wrap bg-slate-50 p-2 rounded-lg border border-slate-100 max-h-40 overflow-y-auto">
                              {item.caption}
                            </pre>
                            <button
                              className="mt-1 text-[11px] text-violet-600 font-medium hover:underline"
                              onClick={(e) => {
                                e.stopPropagation();
                                navigator.clipboard.writeText(item.caption);
                              }}
                            >
                              📋 Copy caption
                            </button>
                          </div>
                          {item.notes && (
                            <div>
                              <p className="text-[11px] font-semibold text-slate-500 uppercase mb-1">Notes</p>
                              <p className="text-xs text-slate-600">{item.notes}</p>
                            </div>
                          )}
                          {next && (
                            <button
                              className="w-full py-1.5 text-xs font-semibold rounded-lg bg-violet-600 text-white hover:bg-violet-500 transition"
                              onClick={(e) => {
                                e.stopPropagation();
                                moveContent(item.id, next);
                              }}
                            >
                              Move to {STATUS_COLS.find((c) => c.key === next)?.label} →
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
                {items.length === 0 && (
                  <p className="text-xs text-slate-400 text-center py-4">No items</p>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
