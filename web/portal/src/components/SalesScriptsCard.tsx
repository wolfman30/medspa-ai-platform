import { useState } from 'react';

interface SalesScript {
  clinic: string;
  owner: string;
  context: string;
  opening: string;
  discoveryQuestions: string[];
  observableFacts: string[];
  objections: Array<{ says: string; respond: string }>;
  close: string;
  keyStats: string[];
}

const SCRIPTS: SalesScript[] = [
  {
    clinic: 'Forever 22 Med Spa',
    owner: 'Brandi Sesock',
    context: 'Solo practitioner, Valley City OH. 5.0★ / 118 Google reviews. Booking via Moxie. Contact page is broken (404). No chat widget. Already received email + text + DM with no response — this is a follow-up attempt.',
    opening: `"Hi Brandi, this is Andrew Wolf — I run a small tech company here in Ohio. I came across Forever 22 online and I was really impressed with your reviews. Do you have just a couple minutes?"`,
    discoveryQuestions: [
      '"When someone tries to reach you after hours, what happens to that inquiry?"',
      '"How do you currently handle missed calls during treatments?"',
      '"Have you clicked your own Contact Us page recently?" (It returns a 404 — she can verify on her phone right now)',
      '"With your no-walk-ins policy, what options does a new person have if they can\'t call right now?"',
    ],
    observableFacts: [
      '❌ Contact page is broken (404) — anyone clicking "Contact Us" hits a dead end',
      '❌ No chat widget or contact form — only way to reach her is calling or emailing',
      '❌ No after-hours lead capture — someone at 9pm can only bookmark and hope to remember',
      '⚠️ No owner replies on Google reviews — missing trust-building + SEO',
      '✅ Moxie online booking works — her one functioning lead capture path',
    ],
    objections: [
      {
        says: '"I\'m doing fine, I stay busy"',
        respond: '"That\'s great — your reviews confirm it. The question isn\'t whether you\'re busy now, it\'s how many people tried to reach you and couldn\'t. Have you checked your Contact Us page lately?"',
      },
      {
        says: '"I don\'t need that stuff"',
        respond: '"Totally fair. Can I just show you one thing? Go to your site and click Contact Us." (Let the 404 speak for itself)',
      },
      {
        says: '"It\'s too expensive"',
        respond: '"What would even one extra Botox appointment per week be worth? That contact page alone is probably sending people to competitors."',
      },
      {
        says: '"I\'ll get to it eventually"',
        respond: '"Completely understand. Just know that every day it\'s broken, some percentage of people who Google you and click Contact are hitting a wall."',
      },
    ],
    close: `"I built something that texts people back instantly when they can't reach you — 24/7. It books them right into your Moxie calendar. $497 a month, no setup fee. Want me to show you how it works?"`,
    keyStats: [
      '78% of customers buy from the first business that responds (Harvard Business Review)',
      'Average visitor who can\'t find contact in 10 seconds leaves and doesn\'t come back',
    ],
  },
  {
    clinic: 'Brilliant Aesthetics',
    owner: 'Kimberly Enochs-Smith, NP',
    context: 'Strongsville OH. Near-zero Google reviews. Website has raw JSON/placeholder code visible. Instagram handle confusion. Solo NP in IMAGE Studios suite. Already configured in our system.',
    opening: `"Hi Kimberly, this is Andrew Wolf — I run a small tech company here in Ohio focused on med spas. I was looking into your practice and your EvexiPEL reviews are really positive. Can I walk you through what a new patient sees when they search for you?"`,
    discoveryQuestions: [
      '"If I Google \'med spa Strongsville,\' do you know where you show up?"',
      '"What Instagram handle do you give people?" (Check if she\'s using the wrong one)',
      '"Have you looked at your website homepage recently on a phone?"',
      '"How are most of your new patients finding you right now?"',
    ],
    observableFacts: [
      '❌ Raw code/JSON visible on homepage — Squarespace template never replaced',
      '❌ Near-zero Google reviews while competitors have 100+',
      '❌ Instagram @brilliantaesthetics_ohio doesn\'t exist — dead end on marketing materials',
      '❌ No chat widget on website',
      '⚠️ Gmail address — less professional for a medical practice',
      '⚠️ Two different addresses in online listings',
    ],
    objections: [
      {
        says: '"I\'m doing fine with referrals"',
        respond: '"Referrals are amazing — and those people will Google you to confirm. Right now they find almost no reviews and a site with broken template code."',
      },
      {
        says: '"I don\'t need a fancy website"',
        respond: '"It doesn\'t have to be fancy. But right now there\'s literally code showing on your homepage — can I show you?"',
      },
      {
        says: '"Too expensive"',
        respond: '"Getting 25 Google reviews costs $0 — just text your happy patients a link. That alone would transform how you show up."',
      },
    ],
    close: `"I built an AI receptionist that texts people back instantly when they can't reach you — even at 10pm. It books them right into your calendar. $497 a month, no setup fee for our first 10 clients. Want to see a quick demo?"`,
    keyStats: [
      '92% of consumers read online reviews before choosing a local business (BrightLocal)',
      'First impressions are 94% design-related — visitors judge credibility in 0.05 seconds (Stanford)',
    ],
  },
  {
    clinic: 'The Cru Clinic',
    owner: 'Owner (Ontario, OH)',
    context: '5.0★ / 119 Google reviews. Strongest Instagram (3,092 followers, 1,574 posts). Website is an SEO text wall with no clear booking button. No chat widget. Some service pages return 404.',
    opening: `"Hi, this is Andrew Wolf — I run a small tech company here in Ohio that works with med spas. Your team is clearly doing something right — 5.0 stars, 119 reviews, 3,000+ Instagram followers. I just noticed a few things on your website that might mean some of those people are falling off before they book."`,
    discoveryQuestions: [
      '"When someone visits your website at 8pm and wants to book, what can they do?"',
      '"Do you know how many website visitors you get per month vs. how many actually book?"',
      '"How quickly do DMs on Instagram get answered — same day? Next day?"',
      '"What happens when someone calls and you\'re with a patient?"',
      '"Have you ever tried clicking through your own Services page recently?" (Some pages return 404)',
    ],
    observableFacts: [
      '❌ No visible online booking button on homepage — wall of SEO text, no call to action',
      '❌ No chat widget — can\'t ask a quick question without calling',
      '❌ Some service pages return 404 — visitors hitting dead ends',
      '⚠️ Homepage reads like a blog article, not a booking-focused site',
      '✅ Professional email (info@cruclinic.com)',
      '✅ Strong Instagram (3,092 followers)',
      '✅ Great reviews — 5.0 / 119 on Google',
    ],
    objections: [
      {
        says: '"We\'re already busy enough"',
        respond: '"Your reviews and IG prove that. The question is how many DMs and after-hours calls are falling through the cracks. Even one lost Botox client per week is $2,000+/month."',
      },
      {
        says: '"We have a receptionist"',
        respond: '"That\'s great during business hours. What about 7pm when someone sees your Instagram post and tries to book? Or when your receptionist is on the other line?"',
      },
    ],
    close: `"I built an AI that texts people back the second they can't reach you — 24/7, even from Instagram. $497 a month, no setup fee. Want to see a 2-minute demo?"`,
    keyStats: [
      '46% of all Google searches have local intent, 88% of local mobile searches result in a call or visit within 24 hours (Google/Acquisio)',
    ],
  },
];

export function SalesScriptsCard() {
  const [expandedClinic, setExpandedClinic] = useState<string | null>(null);
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const copyText = (text: string, field: string) => {
    navigator.clipboard.writeText(text.replace(/^"|"$/g, ''));
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5">
      <h2 className="text-lg font-semibold mb-1">📞 Sales Call Scripts</h2>
      <p className="text-xs text-slate-500 mb-4">Click a clinic to expand the full script. Tap any quote to copy it.</p>

      <div className="space-y-2">
        {SCRIPTS.map(script => {
          const isExpanded = expandedClinic === script.clinic;
          return (
            <div key={script.clinic} className="border border-slate-800 rounded-lg overflow-hidden">
              <button
                onClick={() => setExpandedClinic(isExpanded ? null : script.clinic)}
                className="w-full flex items-center gap-3 p-3 hover:bg-slate-800/50 transition text-left"
              >
                <span className={`text-slate-400 transition-transform ${isExpanded ? 'rotate-90' : ''}`}>▶</span>
                <span className="text-sm text-slate-200 font-medium flex-1">{script.clinic}</span>
                <span className="text-xs text-slate-500">{script.owner}</span>
              </button>

              {isExpanded && (
                <div className="px-4 pb-4 space-y-4">
                  {/* Context */}
                  <div className="text-xs text-slate-500 bg-slate-800/50 rounded-lg p-3">
                    {script.context}
                  </div>

                  {/* Opening */}
                  <div>
                    <h3 className="text-xs font-semibold text-violet-400 uppercase tracking-wide mb-1">Opening</h3>
                    <button
                      onClick={() => copyText(script.opening, `${script.clinic}-opening`)}
                      className="text-sm text-slate-200 bg-slate-800 rounded-lg p-3 w-full text-left hover:bg-slate-700 transition relative"
                    >
                      {script.opening}
                      {copiedField === `${script.clinic}-opening` && (
                        <span className="absolute top-1 right-2 text-xs text-green-400">✓ Copied</span>
                      )}
                    </button>
                  </div>

                  {/* Discovery Questions */}
                  <div>
                    <h3 className="text-xs font-semibold text-violet-400 uppercase tracking-wide mb-2">Discovery Questions</h3>
                    <div className="space-y-1">
                      {script.discoveryQuestions.map((q, i) => (
                        <button
                          key={i}
                          onClick={() => copyText(q, `${script.clinic}-q-${i}`)}
                          className="text-sm text-slate-300 bg-slate-800 rounded-lg p-2 px-3 w-full text-left hover:bg-slate-700 transition relative"
                        >
                          {q}
                          {copiedField === `${script.clinic}-q-${i}` && (
                            <span className="absolute top-1 right-2 text-xs text-green-400">✓</span>
                          )}
                        </button>
                      ))}
                    </div>
                  </div>

                  {/* Observable Facts */}
                  <div>
                    <h3 className="text-xs font-semibold text-violet-400 uppercase tracking-wide mb-2">Observable Facts (she can verify)</h3>
                    <div className="space-y-1">
                      {script.observableFacts.map((f, i) => (
                        <div key={i} className="text-sm text-slate-300 pl-2">{f}</div>
                      ))}
                    </div>
                  </div>

                  {/* Objection Handling */}
                  <div>
                    <h3 className="text-xs font-semibold text-violet-400 uppercase tracking-wide mb-2">Objection Handling</h3>
                    <div className="space-y-2">
                      {script.objections.map((o, i) => (
                        <div key={i} className="bg-slate-800 rounded-lg p-3">
                          <div className="text-xs text-red-400 mb-1">They say: {o.says}</div>
                          <button
                            onClick={() => copyText(o.respond, `${script.clinic}-obj-${i}`)}
                            className="text-sm text-slate-200 w-full text-left hover:text-white transition relative"
                          >
                            → {o.respond}
                            {copiedField === `${script.clinic}-obj-${i}` && (
                              <span className="absolute top-0 right-0 text-xs text-green-400">✓</span>
                            )}
                          </button>
                        </div>
                      ))}
                    </div>
                  </div>

                  {/* Key Stats */}
                  <div>
                    <h3 className="text-xs font-semibold text-violet-400 uppercase tracking-wide mb-1">Key Stats to Reference</h3>
                    {script.keyStats.map((s, i) => (
                      <div key={i} className="text-xs text-slate-400 italic pl-2">📊 {s}</div>
                    ))}
                  </div>

                  {/* Close */}
                  <div>
                    <h3 className="text-xs font-semibold text-green-400 uppercase tracking-wide mb-1">Close</h3>
                    <button
                      onClick={() => copyText(script.close, `${script.clinic}-close`)}
                      className="text-sm text-green-300 bg-green-950 border border-green-800 rounded-lg p-3 w-full text-left hover:bg-green-900 transition relative"
                    >
                      {script.close}
                      {copiedField === `${script.clinic}-close` && (
                        <span className="absolute top-1 right-2 text-xs text-green-400">✓ Copied</span>
                      )}
                    </button>
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
