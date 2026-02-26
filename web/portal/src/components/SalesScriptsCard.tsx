import { useState } from 'react';

interface WhaleScript {
  target: string;
  subtitle: string;
  coldEmail: string;
  linkedIn: string;
  callScript: string;
}

const SCRIPTS: WhaleScript[] = [
  {
    target: 'Juvly Aesthetics',
    subtitle: '10 locations • Columbus HQ • Multi-state chain',
    coldEmail: `Subject: Quick question about phone coverage across Juvly’s locations

Hi [Name],

I’ve been following Juvly’s growth from Columbus to a multi-state footprint — seriously impressive execution.

I work with multi-location med spas on one specific leak: inbound calls that hit voicemail after hours, during treatment-time rushes, or Saturday overflow windows.

For a 10-location group, even modest missed-call recovery can add up quickly:
• 100 recovered calls/day × 20% booking × $500 average treatment value = ~$10,000/day in recovered revenue

We deploy AI voice coverage that answers in seconds, handles common service/pricing questions, and books directly into your existing scheduling flow.

Would you be open to a 30-day pilot at one location (Cleveland is usually a great low-risk test)?

No platform swap required, and we’ll deliver a clean ROI report at the end.

Best,
[Your Name]
[Title] | [Company]
[Phone] | [Email]`,
    linkedIn: `Hi [Name] — impressive work scaling Juvly across multiple states.

I help multi-location med spas recover missed-call revenue by using AI voice coverage to answer and book when front desk teams are busy or offline.

If useful, I can share a quick 2-minute breakdown of what this looks like in a 10-location environment and typical ROI from a 30-day pilot.`,
    callScript: `Hi [Name], this is [Your Name] with [Company] — I’ll keep it brief.

I’m reaching out because teams running multi-location med spas usually face the same issue: valuable inbound calls coming in after hours or during peak treatment blocks and never getting converted.

At Juvly’s scale, even recovering a fraction of missed calls can create meaningful monthly revenue.

We provide AI voice coverage that answers in seconds, handles FAQs, and books directly into your scheduling flow — without changing your existing systems.

Would you be open to a low-risk 30-day pilot at one location so we can measure exactly how many calls get recovered and how many book?`,
  },
  {
    target: 'Radiant Divine Aesthetics',
    subtitle: 'Whale target • Draft prepared (no outreach file found)',
    coldEmail: `Subject: Idea to increase booked consults without adding front-desk headcount

Hi [Name],

I’m reaching out because fast-growing aesthetics groups often lose high-intent callers when lines are busy, after-hours, or during treatment-time surges.

We help med spa groups deploy AI voice coverage that:
• answers every inbound call in seconds
• handles common service and pricing questions
• books directly into your current scheduling workflow
• escalates complex calls to staff when needed

Most teams start with a 30-day pilot at one location, then scale once the recovered bookings and revenue are clear.

Would you be open to a short call to see if this could fit your current growth goals?

Best,
[Your Name]
[Title] | [Company]
[Phone] | [Email]`,
    linkedIn: `Hi [Name] — I work with multi-location med spas to improve inbound conversion by covering calls that happen when front desks are busy or offline.

If helpful, I can share a short pilot framework we use to prove recovered bookings and ROI in 30 days before wider rollout.`,
    callScript: `Hi [Name], this is [Your Name] from [Company] — quick reason for the call:

We help aesthetics groups capture callers that normally hit voicemail during peak hours and after-hours.

Our AI voice layer answers immediately, handles common questions, and books into your existing system.

Teams usually pilot one location for 30 days and decide based on hard numbers: recovered calls, booked consults, and revenue impact.

Would you be open to a 15-minute conversation this week to see if that’s worth testing?`,
  },
  {
    target: 'Ideal Image',
    subtitle: '157 locations • PE-backed enterprise chain',
    coldEmail: `Subject: Enterprise call-conversion pilot for Ideal Image locations

Hi [Name],

For national chains, inbound conversion gaps often hide in plain sight: overflow calls, after-hours demand, and inconsistent scripting across markets.

We deploy AI voice coverage that standardizes first-touch experience, captures overflow demand, and books appointments directly into existing workflows.

At enterprise scale, small lift per location compounds quickly.

If helpful, we can run a controlled 30-day pilot in one region and report:
• calls recovered from voicemail/abandonment
• incremental booked consults
• attributable revenue lift

Would you be open to discussing a regional pilot design?

Best,
[Your Name]`,
    linkedIn: `Hi [Name] — I help multi-location aesthetics brands improve first-call conversion using AI voice coverage integrated with existing booking workflows.

Happy to share an enterprise pilot format focused on measurable outcomes (recovered calls, booked consults, revenue lift).`,
    callScript: `Hi [Name], [Your Name] here with [Company].

Quick context: we work with large aesthetics organizations to capture high-intent callers that currently fall into voicemail or abandonment during overflow and off-hours windows.

Our AI voice agent answers immediately, follows brand-safe scripting, and books into current workflows.

Would it make sense to scope a 30-day regional pilot with success metrics tied to recovered calls and conversion lift?`,
  },
  {
    target: 'LaserAway',
    subtitle: '200 locations • High-volume, 7-day demand profile',
    coldEmail: `Subject: Reducing missed high-intent calls across high-volume clinics

Hi [Name],

LaserAway’s scale and volume create a huge upside from improving inbound response speed and consistency.

We help multi-location med spa brands answer every call in seconds with AI voice coverage that qualifies, handles common objections, and books into existing scheduling workflows.

A typical pilot gives operations teams clear numbers in 30 days: recovered calls, additional booked consults, and labor-offset impact.

Would you be open to reviewing a pilot plan for one market?

Best,
[Your Name]`,
    linkedIn: `Hi [Name] — we help high-volume aesthetics chains convert more inbound demand by covering overflow and after-hours calls with AI voice booking.

Open to sharing a short pilot framework if that’s relevant to your operations roadmap.`,
    callScript: `Hi [Name], this is [Your Name] with [Company].

I’m calling because high-volume clinic groups often lose conversion during peak call spikes and off-hours windows.

We solve that with AI voice coverage that answers instantly, handles routine questions, and books directly into your workflow.

Would you be open to a 30-day pilot in one market so your team can evaluate measurable lift before any broader rollout?`,
  },
  {
    target: 'VIO Med Spa',
    subtitle: '60+ locations • Franchise expansion model',
    coldEmail: `Subject: Franchise-ready call coverage model for VIO locations

Hi [Name],

As VIO expands, franchise consistency becomes critical at first touch.

We provide AI voice coverage that gives every location a consistent, brand-safe call experience while still routing locally when needed.

This helps franchise systems reduce missed-call leakage, increase booked consults, and improve operator consistency without requiring extra front-desk hiring.

Would you be open to a 30-day pilot in one expansion market, then scale playbook-style if metrics are strong?

Best,
[Your Name]`,
    linkedIn: `Hi [Name] — I work with franchise and multi-location med spa groups on consistent inbound conversion.

We use AI voice coverage to answer, qualify, and book while preserving brand standards across locations.

Happy to share a pilot model if useful.`,
    callScript: `Hi [Name], [Your Name] calling from [Company].

For franchise med spa groups, missed-call leakage and inconsistent call handling can become a growth drag.

We deploy AI voice coverage that standardizes first-touch quality and books directly into existing workflows.

Would you be open to a quick pilot discussion for one market to validate conversion lift before broader rollout?`,
  },
];

export function SalesScriptsCard() {
  const [expandedClinic, setExpandedClinic] = useState<string | null>(null);
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const copyText = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
      <h2 className="text-lg font-semibold mb-1">📞 Enterprise Outreach Scripts</h2>
      <p className="text-xs text-slate-500 mb-4">Expand a whale target to view cold email, LinkedIn message, and 60-sec call script.</p>

      <div className="space-y-2">
        {SCRIPTS.map(script => {
          const isExpanded = expandedClinic === script.target;
          return (
            <div key={script.target} className="border border-slate-800 rounded-lg overflow-hidden">
              <button
                onClick={() => setExpandedClinic(isExpanded ? null : script.target)}
                className="w-full flex items-center gap-3 p-3 hover:bg-slate-800/50 transition text-left"
              >
                <span className={`text-slate-400 transition-transform ${isExpanded ? 'rotate-90' : ''}`}>▶</span>
                <span className="text-sm text-slate-200 font-medium flex-1">{script.target}</span>
                <span className="text-xs text-slate-500">{script.subtitle}</span>
              </button>

              {isExpanded && (
                <div className="px-4 pb-4 space-y-4">
                  {[
                    { key: 'cold-email', label: 'Cold Email Draft', value: script.coldEmail },
                    { key: 'linkedin', label: 'LinkedIn Message', value: script.linkedIn },
                    { key: 'call', label: '60-Second Call Script', value: script.callScript },
                  ].map(section => {
                    const fieldKey = `${script.target}-${section.key}`;
                    return (
                      <div key={section.key}>
                        <div className="flex items-center justify-between mb-1">
                          <h3 className="text-xs font-semibold text-violet-400 uppercase tracking-wide">{section.label}</h3>
                          <button
                            onClick={() => copyText(section.value, fieldKey)}
                            className="text-xs px-2 py-1 rounded border border-slate-700 text-slate-300 hover:bg-slate-800 transition"
                          >
                            {copiedField === fieldKey ? '✓ Copied' : 'Copy to clipboard'}
                          </button>
                        </div>
                        <pre className="text-sm text-slate-200 bg-slate-800 rounded-lg p-3 w-full whitespace-pre-wrap font-sans leading-relaxed">
                          {section.value}
                        </pre>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
