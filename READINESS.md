# MedSpa Concierge — Readiness Tracker

Last updated: 2026-02-18

## Pre-Operator Testing Checklist (SPEC.md §3b)

**Total: 102 scenarios | ✅ 64 passing | ❌ 0 failing | 🔲 38 untested**
**Completion: 63%**

### By Category

| Category | Total | ✅ | 🔲 | Notes |
|----------|-------|----|----|-------|
| A. Lead Engagement | 6 | 3 | 3 | A1 (missed call→SMS), A3/A4 (hours-based greeting) untested |
| B. AI Qualification | 16 | 16 | 0 | **COMPLETE** ✅ |
| C. Availability & Booking | 10 | 8 | 2 | C7 (no slots), C9 (unconfigured service) |
| D. Payment & Deposit | 11 | 6 | 5 | D5 (mobile), D10 (expiry), D11 (abandon) |
| E. Conversation Quality | 15 | 12 | 3 | E10 (tone), E14 (non-English), E15 (long msgs) |
| F. Compliance & Security | 5 | 4 | 1 | F5 (rate limiting) |
| G. Admin Portal | 10 | 3 | 7 | G4-G10 (settings, knowledge, multi-org) |
| H. Edge Cases | 10 | 8 | 2 | Mostly covered |
| I. Stress & Race Conditions | 7 | 4 | 3 | I6 (simultaneous booking), I7 (cancel mid-flow) |
| J. Onboarding | 8 | 0 | 8 | **ALL UNTESTED** — onboarding flow not ready |

### Hard Blockers for Operator Testing

1. **10DLC Registration** — Unregistered A2P SMS fully blocked since Feb 2025. Need EIN from Andrew. Timeline: ~2-3 weeks after submission.
2. **A1 (Missed Call → SMS)** — Core feature never tested end-to-end with real phone. Brilliant Aesthetics test showed SMS delivery issues.
3. **SMS delivery verification** — Need to confirm texts actually arrive on real phones (not just Telnyx delivery webhooks).
4. **Multi-clinic `from` number wiring** — Must verify correct per-clinic Telnyx number used for outbound SMS.

### Soft Blockers (Should fix, not deal-breakers)

1. **J1-J8 Onboarding** — Clinic can be manually configured, but no self-serve flow yet.
2. **G5-G10 Portal features** — Admin portal works for conversations/knowledge; settings and multi-org untested.
3. **D10/D11 Payment edge cases** — Stripe handles most gracefully by default.

---

## Readiness: Forever 22 Operator Testing

**Can Brandi/Gale test today? NO**

### What works ✅
- Full booking flow: missed call → AI qualification → Moxie availability → Stripe Checkout → Moxie booking
- All 46 services configured with aliases
- Both providers (Brandi Sesock, Gale Tesar) configured
- Service variants (weight loss in-person/virtual)
- Booking policies (deposit, age, terms) shown pre-payment
- Prompt injection defense (3-layer)
- TCPA compliance (STOP/HELP/START)
- Admin portal: conversations, knowledge editor, Moxie sync

### What's blocking ❌
1. **10DLC not registered** — SMS will be blocked by carriers. HARD BLOCKER.
2. **SMS delivery not verified on real phones** — Only webhook confirmations seen.
3. **Ack messages still double SMS costs** — Root cause known (`internal/messaging/ack.go`), not fixed.

### What would make it better (not blocking)
- After-hours greeting logic untested (A3/A4)
- Non-English message handling untested (E14)
- Onboarding is manual (no self-serve)

---

## Readiness: First Sale

**Can we sell today? NO**

### Sales Readiness Checklist

| Requirement | Status | Notes |
|-------------|--------|-------|
| Working product demo | ⚠️ Partial | Blocked by 10DLC/SMS delivery |
| 10DLC registered | ❌ | Need EIN from Andrew |
| Website updated | ❌ | 9 issues identified, not fixed |
| Pricing page | ❌ | $497/mo not on website |
| Calendly/booking link | ❌ | No way for leads to schedule demo |
| Sales outreach content | ⚠️ Draft | Outreach email drafted, not sent |
| Stripe Connect working | ✅ | Test mode verified |
| Multiple clinics configured | ⚠️ | Forever 22 ✅, Brilliant ⚠️ (SMS issue), Lucy/Adela TODO |
| Operator successfully tested | ❌ | Blocked by above |

### Distance to First Sale

**Estimated: 4-6 weeks**

1. **Week 1-2**: 10DLC registration (after Andrew provides EIN)
2. **Week 1**: Fix SMS delivery, verify missed call → SMS works, fix website
3. **Week 2-3**: Operator testing with Brandi/Gale at Forever 22
4. **Week 3-4**: Iterate on feedback, fix issues
5. **Week 4-6**: Sales outreach to configured clinics, close first deal

### Critical Path (shortest path to first sale)
1. Andrew provides EIN → 10DLC registration starts (2-3 weeks)
2. Fix SMS delivery issue (1-2 days)
3. Remove/gate ack messages (1 day)
4. Fix website (2-3 days)
5. Test with Brandi → iterate → close

---

## Codebase Health

| Metric | Value |
|--------|-------|
| Go packages | 30+ |
| Test coverage | All packages passing (0 failures) |
| E2E test scenarios | 16 automated, all passing |
| Pre-operator checklist | 64/102 (63%) |
| Known bugs | Chemical peel patient type extraction |
| Technical debt | Ack messages, filler SMS, browser sidecar (deprecated) |
| Security | 3-layer prompt injection, TCPA compliance, PII redaction |
| EMR integrations | Moxie (deep), Square (basic), Boulevard (planned) |
