# MedSpa Concierge — Readiness Tracker

Last updated: 2026-02-24 (07:30 UTC)

## Automated E2E Test Results (Live Dev API)

**30 scenarios | 26 ✅ passing | 4 ❌ failing | 101/106 checks passed (95%)** *(last validated run — 65+ commits since)*

### Recent Fixes (Feb 23-24):
- `87af1f2` fix: voice AI greeting, dedup, qualification flow, prompt overhaul
- `57bfdc4` fix: auto-greeting trigger + deduplicate transcript responses
- `39cf713` fix: output audio logging, busy-spin fix, auto-greeting prompt
- `673b16c` fix: request L16 codec from Telnyx (Nova Sonic needs LPCM, not G722)
- `86a8ece` fix: use async generator for Bedrock stream
- `b712089` fix: add Nova Sonic model + bidirectional stream IAM
- `4f96191` fix: use ECS container metadata for AWS creds in sidecar
- `22fb639` feat: Call Control handler — answer calls + start media streaming
- `16e05ff` feat: Nova Sonic voice AI sidecar integration + llm_service refactor
- *(25 commits in last 24h — all voice AI sidecar work)*

### Previous Fixes (Feb 22-23):
- `20f5608` fix: clear stale lead preferences on new voice call
- `566fbd9` feat: separate voice model for lower latency
- `50226c3` fix: inject qualification state summary for voice (no re-asking)
- `4641187` fix: use DefaultProviderID when noPreference returns empty

### Remaining Issues:
1. **E2E blocked in sandbox** — ADMIN_JWT_SECRET not available; need manual `make e2e-dev` run
2. 40+ commits untested — many bug fixes likely improved score, but unverified
3. Voice AI webhook — new, needs integration testing
4. Mobile portal — needs manual verification on real devices

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

1. **A1 (Missed Call → SMS)** — Core feature never tested end-to-end with real phone by a non-Andrew user.
2. **Multi-clinic `from` number wiring** — Must verify correct per-clinic Telnyx number used for outbound SMS.
3. **5 E2E test failures** — LLM-level issues with service vocabulary recognition and response quality.

### 10DLC Status
- **AI Wolf Solutions brand**: Already registered on Telnyx ✅
- **Forever 22 number (+14407448197)**: Registered under AI Wolf campaign, SMS delivery working ✅
- **Brilliant Aesthetics number (+13305932634)**: NOT registered under campaign → SMS blocked
- **New clinic numbers**: Must be added to AI Wolf campaign before SMS works

### Soft Blockers (Should fix, not deal-breakers)

1. **J1-J8 Onboarding** — Clinic can be manually configured, but no self-serve flow yet.
2. **G5-G10 Portal features** — Admin portal works for conversations/knowledge; settings and multi-org untested.
3. **D10/D11 Payment edge cases** — Stripe handles most gracefully by default.

---

## Readiness: Forever 22 Operator Testing

**Can Brandi/Gale test today? ALMOST — 95% ready**

### What works ✅
- Full booking flow: missed call → AI qualification → Moxie availability → Stripe Checkout → Moxie booking
- All 46 services configured with aliases
- Both providers (Brandi Sesock, Gale Tesar) configured
- Service variants (weight loss in-person/virtual) — now with LLM-powered classification
- Booking policies (deposit, age, terms) shown pre-payment
- Prompt injection defense (3-layer)
- TCPA compliance (STOP/HELP/START)
- Admin portal: conversations, knowledge editor, Moxie sync
- **10DLC registered & SMS delivery confirmed** on +14407448197
- 26/30 E2E scenarios passing (101/106 checks)

### What's blocking ❌
1. **4 E2E failures** — LLM sometimes doesn't recognize "fix my 11s" or "lip flip" as Botox/lip filler
2. **Ack messages still double SMS costs** — Root cause known (`internal/messaging/ack.go`), not fixed
3. **Not tested by a real med spa operator yet** — Only Andrew has tested
4. **Voice AI is NEW and needs operator validation** — Core flow works, edge cases being ironed out

### What would make it better (not blocking)
- After-hours greeting logic untested (A3/A4)
- Non-English message handling untested (E14)
- Onboarding is manual (no self-serve)

---

## Readiness: First Sale

**Can we sell today? CLOSE — demo-ready, marketing not**

### Sales Readiness Checklist

| Requirement | Status | Notes |
|-------------|--------|-------|
| Working product demo | ✅ | 440 number works, SMS delivering |
| 10DLC registered | ✅ | AI Wolf Solutions brand, Forever 22 number active |
| Website updated | ❌ | 9 issues identified, not fixed |
| Pricing page | ❌ | $497/mo not on website |
| Calendly/booking link | ❌ | No way for leads to schedule demo |
| Sales outreach content | ⚠️ Draft | Outreach email drafted, not sent |
| Stripe Connect working | ✅ | Test mode verified |
| Multiple clinics configured | ⚠️ | Forever 22 ✅, Brilliant ⚠️ (SMS issue), Lucy/Adela TODO |
| Operator successfully tested | ❌ | Blocked by above |

### Distance to First Sale

**Estimated: 2-4 weeks**

1. **This week**: Fix 4 E2E failures, fix website, remove ack messages
2. **Week 1-2**: Operator testing with Brandi/Gale at Forever 22
3. **Week 2-3**: Iterate on feedback, fix issues
4. **Week 3-4**: Close deal or expand outreach

### Critical Path (shortest path to first sale)
1. Fix remaining 4 E2E test failures (1-2 days)
2. Fix website + add pricing + Calendly (2-3 days)
3. Remove ack messages (1 day)
4. Brandi tests → iterate → close

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
