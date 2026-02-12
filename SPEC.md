# MedSpa AI Platform Specification

> Single source of truth for business requirements, acceptance criteria, and technical architecture.
> This file is model-agnostic—use it with any AI assistant (Claude, Gemini, ChatGPT, etc.).

---

## 1. Business Problem & Solution

**Problem:** Medical spas lose revenue from missed calls. When potential patients call and get voicemail, 80%+ never call back. Every missed lead is lost revenue—a single Botox patient represents $1,500-3,000/year in recurring visits.

**Solution:** SMS-based AI receptionist that instantly engages missed calls, qualifies leads through conversation, collects deposits, and hands off warm leads to staff for confirmation.

**Result:** Staff effort drops from 15-20 minute intake to 2-3 minute confirmation.

---

## 2. The 5-Step Process

### Step 1: Lead Engagement — Instant Text Back
| Trigger | Phone call goes unanswered OR direct inbound SMS |
|---------|--------------------------------------------------|
| Action | Send SMS within 5 seconds |
| Message | Clinic-configured greeting (time-aware: business hours vs after-hours) |
| SLA | <5 seconds from missed call/inbound SMS to response sent |

**Lead sources:**
- Missed calls (voice webhook triggers SMS)
- Direct inbound SMS (patient texts the clinic number)

**Key files:** `internal/http/handlers/telnyx_webhooks.go`, `internal/messaging/handler.go`

### Step 2: AI Qualifies the Lead (5 Qualifications)

| What | Multi-turn SMS conversation via Claude (AWS Bedrock) |
|------|-----------------------------------------------------|
| Extracts | 5 qualifications (see below) |
| Constraints | No medical advice, HIPAA-compliant, warm conversational tone |

**Key files:** `internal/conversation/service.go`, `internal/conversation/llm_service.go`

#### The 5 Qualifications (Trigger for Booking Flow)

The AI collects these 5 pieces of information before the booking flow can begin. **All 5 must be collected** to transition from Step 2 → Step 3. Phone number is already known (patient called/texted in).

| # | Qualification | How Collected | Example | Purpose |
|---|--------------|---------------|---------|---------|
| 1 | **Full name** (first + last) | AI asks | "Sammie Wallens" | Booking form, personalization |
| 2 | **Service** | AI asks or infers from conversation | "Botox", "Lip Filler" | Determine what to book |
| 3 | **Patient type** (new/existing) | AI asks | "new patient" | Operator context in notifications |
| 4 | **Email** | AI asks | "sammie@email.com" | Moxie booking form, operator follow-up |
| 5 | **Time preferences** | AI asks | "Mondays after 4pm" | Filter available slots |

**Collection priority order (micro-commitment psychology — easy/exciting first, administrative last):**
1. Name (personalizes the conversation immediately)
2. Service (matches their intent — this is why they called)
3. Patient type (natural follow-up: "Have you visited us before?")
4. Email (by now they're invested, feels reasonable for booking)
5. Time preferences (days + times — triggers booking flow)

**What happens when all 5 are collected:**
- **Moxie clinics →** Check availability via browser sidecar → Present matching time slots → Patient picks → Auto-book → **Patient completes payment in Moxie** (Square is not used)
- **Square clinics →** Offer refundable deposit → Send Square checkout link → Patient pays → Operator manually confirms appointment

**How the system decides which path:** By clinic config `booking_platform` (`moxie` or `square`, default: `square`). When `booking_platform=moxie`, **Square is not used**; payment runs through Moxie's checkout flow.

#### Conversation → Browser Sidecar Integration (Moxie Path)

The AI conversation extracts customer preferences and passes them to the browser sidecar for availability checking:

| Input from Conversation | Type | Example | Purpose |
|------------------------|------|---------|---------|
| `serviceName` | string | `"Ablative Erbium Laser Resurfacing"` | Exact service to book (uses search to find) |
| `bookingUrl` | string | `"https://app.joinmoxie.com/booking/forever-22"` | Clinic's Moxie booking widget URL |
| `date` | string (YYYY-MM-DD) | `"2026-02-19"` | Specific date to check availability |
| Customer preferences (optional) | object | See below | Time/day filters |

**Customer Preference Filters:**
```typescript
// Example: "I want Mondays or Thursdays, after 4pm"
{
  serviceName: "Ablative Erbium Laser Resurfacing",
  daysOfWeek: [1, 4],  // 0=Sun, 1=Mon, ..., 6=Sat
  afterTime: "16:00"   // 24-hour format
}
```

**Typical Flow:**
1. Customer says: "I want laser resurfacing on Mondays or Thursdays after 4pm"
2. Conversation service extracts: `{ service: "Ablative Erbium Laser Resurfacing", days: [1,4], afterTime: "16:00" }`
3. Call browser sidecar: `getAvailableDates(url, year, month, timeout, serviceName)`
4. Filter results using `filterSlots(date, slots, { daysOfWeek: [1,4], afterTime: "16:00" })`
5. Return ONLY matching times to customer via SMS

**Important:** For availability checking, the browser sidecar operates in **dry-run mode** (checks availability but does NOT book). For booking automation (Step 3a), the sidecar navigates Steps 1-4 of the Moxie flow and hands off at the payment page for the patient to complete.

#### Time Slot Selection (Patient Picks a Time)

After presenting available times, the system must detect which slot the patient selected. The detection is **natural language aware** — patients should not be forced into a rigid format.

**Supported selection formats:**
| Patient Says | Detection Method | Example |
|-------------|-----------------|---------|
| Slot number | Index match | "2", "option 3", "#1", "the first one" |
| Explicit time | Time match against presented slots | "I'll take the 2pm", "10:30 works" |
| Bare hour | Hour match with disambiguation | "6" → match against slot hours |
| Natural language | LLM interprets if regex fails | "the afternoon one", "the Monday slot" |

**Disambiguation rules for bare hours (e.g., patient says "6"):**

1. If only ONE presented slot has that hour (e.g., only 6:00 PM exists) → select it automatically
2. If MULTIPLE slots share that hour (e.g., both 6:00 AM and 6:00 PM) AND the patient previously stated a time preference (e.g., "after 3pm") → use the preference to disambiguate (select 6:00 PM)
3. If MULTIPLE slots share that hour AND NO preference helps → return no match; the LLM will ask: "Did you mean 6:00 AM or 6:00 PM?"
4. If the bare number is a valid slot index AND does not match any slot hour → treat as slot index (e.g., "3" with 3 slots but none at 3:00)

**Key files:** `internal/conversation/time_selection.go` (DetectTimeSelection), `internal/conversation/llm_service.go`

### Step 3: Book or Collect Deposit

Booking behavior depends on the clinic's `booking_platform` configuration (`moxie` vs `square`):

#### Step 3a: Moxie Clinics — Auto-Booking + Payment in Moxie (No Square)

When all 5 qualifications are met and the patient selects a time slot, the system automates the booking flow via the browser sidecar. **Moxie handles payment directly** — we do not collect a separate deposit.

| Phase | What Happens | Actor |
|-------|-------------|-------|
| 1. Start session | Worker sends booking request to sidecar with patient info, service, provider, date/time | Go Worker |
| 2. Automate Steps 1-4 | Sidecar navigates Moxie booking widget: service → provider → date/time → contact info | Browser Sidecar |
| 3. Handoff at Step 5 | Sidecar stops at the payment page, returns handoff URL | Browser Sidecar |
| 4. Send handoff SMS | Worker sends patient the payment page link via SMS | Go Worker |
| 5. Monitor outcome | Sidecar polls the page every 2s for booking outcome (success, payment failure, timeout) | Browser Sidecar |
| 6. Callback | Sidecar POSTs outcome to `POST /webhooks/booking/callback` | Browser Sidecar → Go API |
| 7. Outcome SMS | Callback handler sends confirmation or failure SMS to patient | Go API |

**Booking Session States:** `created → navigating → ready_for_handoff → monitoring → completed/failed/abandoned`

**Outcome SMS Messages:**
- `success` → "Your appointment is confirmed! Confirmation #..."
- `payment_failed` → "Your payment didn't go through. Reply YES to try again."
- `slot_unavailable` → "That time slot is no longer available. Want me to check other times?"
- `timeout` → "Your booking session expired. Want to try again?"

**Key files:** `internal/conversation/worker.go` (handleMoxieBooking), `internal/conversation/handler.go` (BookingCallbackHandler), `internal/browser/client.go` (booking session methods)

#### Step 3b: Square Clinics — Deposit Collection (No Auto-Booking)

When all 5 qualifications are met and the clinic does NOT have a Moxie booking URL, the system collects a refundable deposit. **The operator manually confirms the appointment** — the system does not auto-book.

| Deposit Eligibility | Per-clinic configuration (admin sets which services require deposits) |
|--------------------|-----------------------------------------------------------------------|
| Payment | Square checkout link (PCI-compliant hosted page) |
| On Success | SMS confirmation sent to patient |
| Next Step | Operator manually contacts patient to finalize appointment time |

**Key files:** `internal/conversation/deposit_sender.go`, `internal/payments/square_checkout.go`

### Step 4: Notify Patient and Operator

| To Patient | Confirmation SMS with appointment details (Moxie) or deposit receipt (Square) |
|------------|-------------------------------------------------------------------------------|
| To Operator | Email + SMS notification with lead details (name, phone, service, patient type, time preferences, deposit amount) |
| Reminders | Future phase: 1 week, 1 day, 3 hours before appointment |

**Operator notification channels:**
- **Email:** HTML-formatted notification via AWS SES with lead details and priority status
- **SMS:** Short notification with customer name, service, and deposit amount

**Key files:** `internal/notify/service.go`, `internal/payments/handler.go` (webhook), `internal/events/outbox.go`

### Step 5: Clinic Knowledge & AI Persona (Per-Clinic Configuration)

Each clinic configures:
- **AI Persona:** Provider name(s), tone, custom greetings (business hours vs after-hours), busy messages
- **Clinic Knowledge:** Up to 50 sections of clinic-specific information (services, pricing, policies, FAQs) that the AI uses as context

These are managed via the admin portal and stored in Redis.

**Key files:** `internal/clinic/config.go`, `internal/conversation/knowledge_repository.go`, `web/onboarding/src/components/AIPersonaSettings.tsx`, `web/onboarding/src/components/KnowledgeSettings.tsx`

---

## 3. Acceptance Criteria (Definition of Done)

All acceptance tests must pass:
```bash
# Go backend acceptance tests
go test -v ./tests/...

# Browser sidecar tests
cd browser-sidecar && npm run test:unit
```

### Per-Step Criteria

| Step | Criteria | Test |
|------|----------|------|
| 1 | Missed call OR inbound SMS triggers response within 5 seconds | Webhook → SMS timestamp delta |
| 2 | AI extracts all 5 qualifications: name, email, service, patient type, time prefs | Lead record has all fields populated |
| 2 | Check available appointment times from Moxie | Browser sidecar returns time slots |
| 2 | Navigate Moxie multi-step booking flow | Integration tests verify flow completion |
| 3 | Moxie booking: sidecar automates Steps 1-4, handoff URL sent | Worker test verifies session start + handoff SMS |
| 3 | Moxie booking: callback handles outcome (success/failure) | Handler test verifies outcome SMS + lead update |
| 3 | Deposit link sent for configured services (Square clinics) | Checkout URL in SMS for deposit-eligible services |
| 3 | Payment processed and recorded | Square webhook updates payment status |
| 4 | Patient receives confirmation SMS | Outbox message sent on payment success |
| 4 | Operator notified | Lead marked qualified with preferences |

### Browser Sidecar TDD Criteria

**Test Coverage Requirements:**
- ✅ **96 unit tests** passing (types, scraper, server endpoints)
- ✅ **Public APIs** validated (health check, availability endpoints)
- ✅ **Edge cases** covered (time parsing, retries, errors)
- ✅ **Request validation** enforced (date format, timeout bounds, URL validation)

**Core Booking Flow Tests:**

| Function | Acceptance Criteria | Test Coverage |
|----------|---------------------|---------------|
| `scrapeAvailability()` | Extract time slots from booking page | Unit + Integration |
| `getAvailableDates()` | Return available dates for month | Integration |
| Time parsing | Handle 12/24-hour, noon, midnight correctly | Unit (42 tests) |
| Error handling | Graceful failures for unreachable URLs, timeouts | Unit + Integration |
| API endpoints | `/health`, `/ready`, `/api/v1/availability` validated | Unit (28 tests) |
| Retry logic | Configurable retries (0, 1, N) with exponential backoff | Unit |
| Moxie flow | Navigate service selection → provider → calendar → times | Integration |

**Request Validation:**
```typescript
// All must return 400 errors
- Missing bookingUrl
- Invalid date format (not YYYY-MM-DD)
- Timeout < 1000ms or > 60000ms
- Batch request > 7 dates
- Malformed JSON
```

**Time Parsing Edge Cases:**
```typescript
- 12:00 PM (noon) → 720 minutes ✅
- 12:00 AM (midnight) → 0 minutes ✅
- Case insensitive: 9:00am, 9:00AM, 9:00Am ✅
- Invalid formats → 0 (graceful degradation) ✅
```

**Slot Filtering (Customer Preferences):**

The browser sidecar includes utilities for filtering time slots based on customer preferences:

```typescript
// Example: "Mondays through Thursdays after 3pm"
import { filterSlots, dayNamesToNumbers } from './utils/slot-filter';

const preferences = {
  daysOfWeek: dayNamesToNumbers(['Monday', 'Tuesday', 'Wednesday', 'Thursday']),
  afterTime: '15:00', // 3pm in 24-hour format
};

const filtered = filterSlots(date, slots, preferences);
// Returns only slots on Mon-Thu after 3pm
```

| Filter Type | Parameter | Example | Description |
|-------------|-----------|---------|-------------|
| Day of week | `daysOfWeek` | `[1, 2, 3, 4]` | Mon=1, Tue=2, ..., Sun=0 |
| After time | `afterTime` | `"15:00"` | Only times at or after 3pm |
| Before time | `beforeTime` | `"17:00"` | Only times before 5pm |
| Combined | Both | See above | Apply day AND time filters |

**Common Preferences (Pre-configured):**
- `weekdays`: Monday-Friday only
- `weekends`: Saturday-Sunday only
- `businessHours`: 9am-5pm
- `afterWork`: After 5pm
- `morningOnly`: Before noon
- `afternoonOnly`: After noon

**Dry Run Mode:**

```typescript
{
  "dryRun": true  // Default: only check availability, do NOT book
}
```

**IMPORTANT:** The browser sidecar currently **ALWAYS** operates in dry-run mode (availability check only). It does NOT complete bookings. The `dryRun` flag is reserved for future functionality:

- `dryRun: true` (current behavior): Navigate → Select service → Select provider → Extract time slots → STOP
- `dryRun: false` (future): Complete the above + Fill patient info + Submit booking form

**Real-World Testing:**

The scraper has been tested against the live Forever 22 Med Spa Moxie widget:
- URL: `https://app.joinmoxie.com/booking/forever-22`
- Successfully extracted 12 available dates for February 2026
- Successfully extracted 6 time slots (9:30am - 1:30pm range)
- Confirmed NO BOOKING was made (dry-run verification)

---

## 4. Compliance Requirements

### HIPAA (PHI Protection)
| Requirement | Implementation |
|-------------|----------------|
| BAA with AI provider | AWS Bedrock (BAA signed) |
| Encryption in transit | TLS 1.2+ on all endpoints |
| Encryption at rest | AWS RDS/Redis encryption |
| PHI detection | Auto-redact SSN, DOB, medical IDs in logs |
| Audit logging | All PHI access logged with timestamps |
| Access controls | JWT auth, org-scoped data isolation |

### TCPA/A2P (SMS Compliance)
| Requirement | Implementation |
|-------------|----------------|
| STOP handling | Immediate opt-out, no further messages |
| HELP handling | Return clinic contact info |
| START handling | Re-enable messaging |
| Quiet hours | Respect clinic timezone, no late-night SMS |
| 10DLC registration | Per-clinic via Telnyx hosted messaging |

### Medical Liability (No Medical Advice)
| AI CAN | AI CANNOT |
|--------|-----------|
| Explain services offered | Diagnose symptoms or conditions |
| Describe procedures in general terms | Recommend specific treatments for conditions |
| Share general pricing | Prescribe dosages (specific units/syringes for an individual) |
| Answer FAQs (recovery time, prep) | Clear patients for treatment based on medical conditions |
| Provide general dosage ranges from knowledge base | Advise on medication interactions or contraindications |
| Refer to medical professionals | Minimize post-procedure symptoms ("that's normal") |
| Direct emergencies to 911/ER | Say whether treatments are safe during pregnancy/breastfeeding |

**Emergency Protocol:** Vision problems, breathing difficulty, vascular compromise → Immediately direct to 911/ER. Do NOT diagnose, minimize, or mention callback times.

**Contraindication Deflection:** Pregnancy, autoimmune conditions, blood thinners, Accutane, keloids, etc. → Always defer to provider consultation.

**Key files:** `internal/conversation/llm_service.go` (system prompt guardrails)

### PCI (Payment Security)
| Requirement | Implementation |
|-------------|----------------|
| No card storage | Square-hosted checkout only |
| Webhook verification | Square signature validation |

---

## 5. Technical Architecture

### Tech Stack
- **Backend:** Go 1.24+ (tested with Go 1.25.3)
- **Database:** PostgreSQL
- **Cache:** Redis
- **AI:** Claude via AWS Bedrock
- **SMS:** Telnyx (primary), Twilio (fallback)
- **Payments:** Moxie checkout (when `booking_platform=moxie`) OR Square (OAuth + checkout links, when `booking_platform=square`)
- **Infrastructure:** AWS ECS/Fargate

### File Organization
```
cmd/
  api/                 # HTTP API + webhooks
  conversation-worker/ # SQS worker for LLM + deposits
  messaging-worker/    # Telnyx polling + retry
  voice-lambda/        # Voice webhook forwarder
  migrate/             # DB migrations
internal/
  conversation/        # AI conversation engine (Step 2)
  messaging/           # SMS handling (Step 1)
  payments/            # Square integration (Step 3)
  leads/               # Lead capture
  clinic/              # Per-clinic config
  http/handlers/       # Webhook handlers
  events/              # Outbox for confirmations (Step 4)
  browser/             # Browser sidecar client (Go) — availability + booking session methods
browser-sidecar/       # Playwright service (TypeScript/Node.js)
  src/
    scraper.ts         # Availability scraper (Moxie)
    booking-session.ts # Booking session manager (Steps 1-4 automation + outcome monitoring)
    server.ts          # HTTP API for availability + booking sessions
    types.ts           # Request/response schemas
  tests/
    unit/              # 96 unit tests
    integration/       # Browser integration tests
    e2e/               # End-to-end tests
tests/                 # Acceptance tests
migrations/            # Database migrations
```

### Key Endpoints

**Main API (Go):**
| Endpoint | Purpose |
|----------|---------|
| `POST /webhooks/telnyx/messages` | Inbound SMS (Step 1, 2) |
| `POST /webhooks/telnyx/voice` | Missed call trigger (Step 1) |
| `POST /webhooks/square` | Payment notifications (Step 3) |
| `GET /admin/orgs/{orgID}/conversations` | List conversations |
| `GET /admin/orgs/{orgID}/deposits` | List deposits |

**Browser Sidecar (TypeScript):**
| Endpoint | Purpose | SLA |
|----------|---------|-----|
| `GET /health` | Health check + browser status | <100ms |
| `GET /ready` | K8s readiness probe | <100ms |
| `POST /api/v1/availability` | Scrape single date availability | <45s |
| `POST /api/v1/availability/batch` | Scrape 1-7 dates (max) | <5min |
| `POST /api/v1/booking/start` | Start booking session (automates Steps 1-4) | <90s |
| `GET /api/v1/booking/:sessionId/handoff-url` | Get payment page URL for patient handoff | <1s |
| `GET /api/v1/booking/:sessionId/status` | Check session state/outcome | <1s |
| `DELETE /api/v1/booking/:sessionId` | Cancel active booking session | <5s |

**Booking Callback (Go API):**
| Endpoint | Purpose |
|----------|---------|
| `POST /webhooks/booking/callback` | Receive booking outcome from sidecar |

---

## 6. Development Workflow

### Local Setup
```bash
cp .env.example .env
docker compose up -d
DATABASE_URL=postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable go run ./cmd/migrate
curl http://localhost:8082/health
```

### Running Tests
```bash
# Go backend tests
make test                    # Unit tests
go test -v ./tests/...       # Acceptance tests (THE stopping condition)
make cover                   # Coverage report

# Browser sidecar tests
cd browser-sidecar
npm test                     # All tests
npm run test:unit            # Unit tests (96 tests, ~86 sec)
npm run test:integration     # Integration tests (requires mock server)
npm run test:coverage        # Coverage report

# E2E (requires running API)
python scripts/e2e_full_flow.py
```

### Building
```bash
go build -v ./...                      # Build all
go build -v -o bin/medspa-api ./cmd/api # Build API binary
docker compose up --build              # Docker build
```

### Code Standards

**Go:**
- Format: `gofmt -s -w .`
- Lint: `go vet ./...`
- Tests: Table-driven, use `testify/assert`
- Errors: Wrap with `fmt.Errorf("context: %w", err)`

**TypeScript (Browser Sidecar):**
- Tests: Behavior-focused (not implementation details)
- Pattern: Table-driven with clear test names
- Coverage: Unit tests for public APIs, integration tests for browser flows
- Philosophy: Test behavior, not implementation; avoid brittle DOM mocking

---

## 7. Environment Variables

### Required (Local Dev)
```bash
DATABASE_URL=postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable
REDIS_URL=redis://localhost:6379
SMS_PROVIDER=twilio  # or telnyx
```

### Production
```bash
AWS_REGION=us-east-1
BEDROCK_MODEL_ID=anthropic.claude-3-haiku-20240307-v1:0
TELNYX_API_KEY=...
TWILIO_ACCOUNT_SID=...
TWILIO_AUTH_TOKEN=...
SQUARE_APP_ID=...
SQUARE_APP_SECRET=...
```

---

## 8. Phase II: Voice AI Agent

> **Status:** Architecture complete, implementation planned (4-6 weeks)
> **Architecture doc:** `research/voice-ai-architecture-2026-02-12.md`

### Overview

Real-time voice AI receptionist that answers inbound calls, qualifies patients through natural conversation, checks Moxie availability, and books appointments — using the same qualification logic as the SMS flow.

### Tech Stack (Voice-Specific)

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Telephony | Telnyx Voice API + WebSocket media streams | Already integrated, cheapest, native WebSocket |
| Speech-to-Text | Deepgram Nova-3 (streaming) | Lowest latency (~200ms), built-in VAD |
| Text-to-Speech | ElevenLabs Turbo v2.5 (streaming) | Most natural voice, custom voice per clinic |
| LLM | Claude 3.5 Haiku via AWS Bedrock | Same as SMS flow, streaming responses |
| Orchestration | Go service on ECS Fargate | WebSocket handler + audio pipeline |

### Latency Budget (~500ms target)

| Stage | Baseline | Optimized | How |
|-------|----------|-----------|-----|
| Speech endpointing (VAD) | 200ms | 100ms | Aggressive VAD threshold + interim STT results |
| STT final result | 50ms | 0ms | Use streaming interim results, don't wait for final |
| LLM first token | 400ms | 200ms | Claude Haiku + minimal system prompt + pre-warmed connection |
| TTS first audio byte | 200ms | 100ms | ElevenLabs streaming from first sentence fragment |
| Network | 100ms | 50ms | Co-locate in us-east-1, persistent WebSocket |
| **Baseline total** | **~950ms** | | |
| **Optimized total** | | **~450ms** | |

**Key optimizations for ~500ms:**
1. **LLM→TTS pipelining** — Stream LLM tokens directly to TTS. Start audio playback while LLM is still generating the rest of the sentence. Patient hears the first word within 300ms of LLM start.
2. **Streaming STT** — Process interim (partial) transcripts immediately. Don't wait for Deepgram's "final" result. Correct course if the final differs from interim.
3. **Pre-warmed connections** — Keep persistent connections to Deepgram, ElevenLabs, and Bedrock. No cold-start handshake per utterance.
4. **Aggressive VAD** — Detect end-of-speech in 100ms using energy + silence threshold. For short replies ("yes", "Tuesday"), respond almost instantly.
5. **Speculative pre-fetch** — During qualification flow, pre-generate TTS for the next likely question (e.g., after getting name, pre-generate "And what service are you interested in?").
6. **Edge-case acceleration** — For single-word/number replies ("3", "yes", "Botox"), skip full LLM call and use pattern matching to select pre-generated responses.
7. **Sentence-level chunking** — Split LLM output at sentence boundaries, send each sentence to TTS independently. First sentence plays while second generates.
8. **Natural pacing layer** — Raw ~450ms is too fast; feels robotic. Add human-like pacing:
   - Simple questions ("What's your name?"): add 150ms padding → ~600ms total (matches natural human turn-taking ~600ms)
   - Complex responses (availability options): no padding, natural TTS duration handles it
   - After emotional/empathetic moments: add 300ms pause — feels thoughtful, not instant
   - After patient says "um" or pauses mid-sentence: wait longer before responding (they're still thinking)
   - Target perceived response time: **500-800ms** — fast enough to feel attentive, slow enough to feel human

### Call Flow

1. Inbound call → Telnyx routes to Voice AI service
2. WebSocket media stream established (bidirectional audio)
3. Play greeting: "Hi! Thanks for calling [Clinic]. How can I help?"
4. Real-time STT converts patient speech to text
5. LLM processes with same 5-qualification logic (name, service, patient type, email, time)
6. TTS converts response to speech, streamed back to caller
7. After all 5 qualifications → check Moxie availability (existing API, ~1s)
8. Patient selects slot via voice → book via Moxie sidecar
9. Send Stripe/Moxie payment link via SMS (existing flow)
10. Voice confirmation + end call

### Interruption Handling

- Deepgram VAD detects speech during TTS playback
- Immediately cancel TTS audio buffer (stop speaking)
- Process new STT input as next conversation turn
- Resume from new context

### Fallback Scenarios

| Scenario | Action |
|----------|--------|
| STT failure (10s no transcript) | "Let me text you instead" → SMS flow |
| LLM timeout (>3s) | Play filler ("One moment...") + retry |
| TTS failure | Auto-switch to AWS Polly |
| Full system failure | Route to voicemail, trigger SMS follow-up |
| Caller silence (15s) | "Are you still there?" → 10s more → SMS handoff |

### Voice-Specific LLM Adaptations

- Short responses (1-2 sentences per turn)
- Natural fillers ("Sure!", "Great question.")
- No URLs in speech — "I'll text you a link"
- Confirm key info back: "So that's Botox on Monday at 4:30?"

### Multi-Language

- **Launch:** English
- **Fast-follow (2-3 weeks):** Spanish (auto-detect via Deepgram, switch STT/TTS/prompt)

### Cost Per Call

| Component | Cost/min |
|-----------|----------|
| Telnyx (inbound + WebSocket) | $0.010 |
| Deepgram STT | $0.004 |
| ElevenLabs TTS | $0.030 |
| Claude Haiku LLM | $0.005 |
| Recording + storage | $0.002 |
| **Total** | **$0.051/min** |

Average 3-min call = ~$0.15. Target <$0.15/min all-in: ✅

### Implementation Phases

| Phase | Scope | Timeline |
|-------|-------|----------|
| **2a** | Basic voice AI conversation + qualification (MVP) | Weeks 1-3 |
| **2b** | Real-time availability check + Moxie booking | Week 4 |
| **2c** | Interruption handling, barge-in, fallbacks, load testing | Weeks 5-6 |
| **2d** | Spanish, call analytics, quality scoring, multi-location | Post-launch |

### Multi-Location Support

- **Per-clinic voice:** ElevenLabs voice ID per clinic (different persona, accent)
- **Call routing:** Telnyx DID → clinic mapping. Each location has its own number.
- **Shared patient DB:** Cross-location patient lookup by phone
- **Centralized analytics:** Calls, bookings, revenue per location + aggregate

### Database

New `voice_calls` table tracks call sessions, transcripts, recordings, qualification data, costs.

### Deployment

Separate ECS Fargate service (1 vCPU, 2GB RAM). Auto-scales 2-10 instances based on concurrent WebSocket connections (~10 calls/instance).

---

## 9. Future Phases (Out of Scope for Phase I & II)

- **Appointment reminders:** 1 week, 1 day, 3 hours before
- **EMR integration:** Direct booking writes to Nextech, Boulevard, etc.
- **Multi-channel:** Instagram DMs, Google Business Messages, web chat
- **Outbound voice:** Appointment confirmations, re-engagement calls
- **Voice analytics:** Sentiment analysis, conversion optimization

---

## 9. Operational References

- **Deployment:** See `s3://medspa-ai-platform-dev/docs/DEPLOYMENT_ECS.md`
- **Webhook Setup:** See `s3://medspa-ai-platform-dev/docs/RUNBOOK_WEBHOOKS.md`
