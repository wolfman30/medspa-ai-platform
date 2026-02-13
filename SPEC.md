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
- **Moxie clinics (Stripe Connect) →** Check availability via **Moxie GraphQL API** (~1s) → Present matching time slots → Patient picks → **Stripe Checkout link** sent → Patient pays → **Moxie API books appointment** → Confirmation SMS
- **Square clinics →** Offer refundable deposit → Send Square checkout link → Patient pays → Operator manually confirms appointment
- **Stripe clinics (non-Moxie) →** Same Stripe Checkout flow for deposit collection, booking handled by operator or external platform

**How the system decides which path:** By clinic config `booking_platform` (`moxie`, `square`, or `stripe`) and `PaymentProvider` field. When `booking_platform=moxie`, the system uses Moxie's GraphQL API for availability/booking and Stripe Connect for payment. When `booking_platform=square`, Square handles payments. The `MultiCheckoutService` routes to the correct payment provider based on clinic config.

#### Conversation → Moxie API Integration (Moxie Path)

The AI conversation extracts customer preferences and queries the Moxie GraphQL API for real-time availability:

| Input from Conversation | Type | Example | Purpose |
|------------------------|------|---------|---------|
| `serviceName` | string | `"Ablative Erbium Laser Resurfacing"` | Exact service to book |
| `medspaId` | string | `"clinic-uuid"` | Clinic's Moxie medspa ID |
| `startDate` / `endDate` | string (YYYY-MM-DD) | `"2026-02-19"` / `"2026-02-26"` | Date range to check |
| Customer preferences (optional) | object | See below | Time/day filters |

**Moxie GraphQL API:**
- **Endpoint:** `https://graphql.joinmoxie.com/v1/graphql`
- **Query:** `AvailableTimeSlots` — takes `medspaId`, `startDate`, `endDate`, `services` → returns dates with available slots (~1s response time)
- **Mutation:** `createAppointmentByClient` — takes patient info + service + time → creates appointment (uses `MAIA_BOOKING` flow)

**Customer Preference Filters:**
```json
// Example: "I want Mondays or Thursdays, after 4pm"
{
  "serviceName": "Ablative Erbium Laser Resurfacing",
  "daysOfWeek": [1, 4],
  "afterTime": "16:00"
}
```

**Typical Flow:**
1. Customer says: "I want laser resurfacing on Mondays or Thursdays after 4pm"
2. Conversation service extracts: `{ service: "Ablative Erbium Laser Resurfacing", days: [1,4], afterTime: "16:00" }`
3. Query Moxie API: `AvailableTimeSlots(medspaId, startDate, endDate, services)` — returns in ~1s
4. Filter results using customer preferences (days of week, time ranges)
5. Return ONLY matching times to customer via SMS

**Key files:** `internal/moxie/client.go`, `internal/moxie/availability.go`

##### Legacy Fallback: Browser Sidecar

When a clinic does not have Moxie API credentials configured (`medspaId` not set), the system falls back to the browser sidecar for availability scraping. This is significantly slower (~30-60s vs ~1s) and should be considered a transitional path.

**Key files:** `internal/browser/client.go`, `browser-sidecar/src/scraper.ts`

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

#### Step 3a: Moxie Clinics — Stripe Connect Payment + Moxie API Booking

When all 5 qualifications are met and the patient selects a time slot, the system collects a deposit via **Stripe Connect** and books the appointment via the **Moxie GraphQL API**. Deposits go directly to the clinic's connected Stripe account.

| Phase | What Happens | Actor |
|-------|-------------|-------|
| 1. Patient selects slot | Patient picks a time from presented options | Patient |
| 2. Create checkout | System creates Stripe Checkout Session with deposit amount + `transfer_data.destination` for clinic's Stripe account | Go Worker |
| 3. Send payment link | Stripe Checkout URL sent to patient via SMS (mobile-optimized) | Go Worker |
| 4. Patient pays | Patient completes payment on Stripe's hosted page | Patient |
| 5. Webhook fires | Stripe `checkout.session.completed` webhook → emits `PaymentSucceededV1` event | Stripe → Go API |
| 6. Book appointment | Worker calls Moxie `createAppointmentByClient` mutation to create the appointment | Go Worker |
| 7. Confirmation SMS | Patient receives appointment confirmation with details | Go Worker |
| 8. Notify operator | Clinic operator notified via email + SMS with lead details | Go Worker |

**Stripe Connect Architecture:**
- Each clinic onboards via Stripe Connect OAuth → receives a connected account ID
- Checkout sessions use `transfer_data.destination` to route deposits directly to the clinic's Stripe account
- Platform (MedSpa AI) can optionally take an application fee
- Metadata includes `org_id`, `lead_id` for tracing

**Checkout Session metadata:**
```json
{
  "org_id": "clinic-uuid",
  "lead_id": "lead-uuid",
  "service": "Botox",
  "appointment_time": "2026-02-19T14:30:00Z"
}
```

**Outcome SMS Messages:**
- `success` → "Your appointment is confirmed! [Date] at [Time] for [Service]. See you then!"
- `payment_failed` → "Your payment didn't go through. Reply YES to try again."
- `slot_unavailable` → "That time slot is no longer available. Want me to check other times?"
- `session_expired` → "Your payment link has expired. Want me to send a new one?"

**`MOXIE_DRY_RUN` mode:** When enabled, the system skips the actual `createAppointmentByClient` call and logs what would have been booked. Useful for testing the full payment flow without creating real Moxie appointments.

**Key files:** `internal/conversation/worker.go` (handleMoxieBooking), `internal/payments/stripe_checkout.go`, `internal/payments/stripe_webhook.go`, `internal/moxie/client.go` (createAppointmentByClient)

#### Step 3b: Square Clinics — Deposit Collection (No Auto-Booking)

When all 5 qualifications are met and the clinic uses Square (`booking_platform=square`), the system collects a refundable deposit. **The operator manually confirms the appointment** — the system does not auto-book.

| Deposit Eligibility | Per-clinic configuration (admin sets which services require deposits) |
|--------------------|-----------------------------------------------------------------------|
| Payment | Square checkout link (PCI-compliant hosted page) |
| On Success | SMS confirmation sent to patient |
| Next Step | Operator manually contacts patient to finalize appointment time |

> **Note:** Clinics that don't use Moxie but want Stripe-based payments can set `PaymentProvider=stripe` with `booking_platform=stripe`. The flow is identical to Square (deposit collection, manual booking) but uses Stripe Checkout instead.

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
| 2 | Check available appointment times from Moxie | Moxie GraphQL API returns time slots (~1s) |
| 2 | Availability results match patient preferences | Filtered slots match days/times requested |
| 3 | Moxie booking: Stripe Checkout link sent after slot selection | Worker test verifies checkout session + SMS |
| 3 | Moxie booking: Stripe webhook triggers Moxie API booking | Handler test verifies appointment creation + confirmation SMS |
| 3 | Deposit link sent for configured services (Square clinics) | Checkout URL in SMS for deposit-eligible services |
| 3 | Payment processed and recorded | Square webhook updates payment status |
| 4 | Patient receives confirmation SMS | Outbox message sent on payment success |
| 4 | Operator notified | Lead marked qualified with preferences |

### Moxie API + Stripe Connect Test Criteria

**Core Booking Flow Tests:**

| Function | Acceptance Criteria | Test Coverage |
|----------|---------------------|---------------|
| `AvailableTimeSlots` query | Returns available slots within ~1s | Unit + Integration |
| `createAppointmentByClient` | Creates Moxie appointment with correct patient/service/time | Unit + Integration |
| Stripe Checkout session | Creates session with correct amount, metadata, transfer_data | Unit |
| Stripe webhook handler | `checkout.session.completed` → triggers Moxie booking | Unit + Integration |
| `MultiCheckoutService` | Routes to Stripe or Square based on clinic config | Unit |
| `MOXIE_DRY_RUN` mode | Skips Moxie API call, logs intended booking | Unit |
| Slot filtering | Customer preference filters (day/time) applied correctly | Unit |

**Slot Filtering (Customer Preferences):**

| Filter Type | Parameter | Example | Description |
|-------------|-----------|---------|-------------|
| Day of week | `daysOfWeek` | `[1, 2, 3, 4]` | Mon=1, Tue=2, ..., Sun=0 |
| After time | `afterTime` | `"15:00"` | Only times at or after 3pm |
| Before time | `beforeTime` | `"17:00"` | Only times before 5pm |
| Combined | Both | See above | Apply day AND time filters |

### Payment Providers

The system supports multiple payment providers, routed by the `MultiCheckoutService` based on clinic configuration:

#### Stripe Connect (Moxie Clinics — Primary)

- **How it works:** Each clinic onboards via Stripe Connect OAuth, linking their Stripe account. Deposits are collected via Stripe Checkout Sessions with `transfer_data.destination` set to the clinic's connected account ID.
- **Clinic onboarding:** Admin portal initiates Stripe Connect OAuth → clinic authorizes → connected account ID stored in clinic config.
- **Checkout flow:** Create session → send link via SMS → patient pays on Stripe's mobile-optimized page → webhook fires → appointment booked.
- **Key files:** `internal/payments/stripe_checkout.go`, `internal/payments/stripe_webhook.go`

#### Square (Non-Moxie Clinics)

- **How it works:** Existing Square OAuth + checkout link flow. Deposits collected via Square-hosted payment page.
- **Clinic onboarding:** Square OAuth flow via admin portal.
- **Key files:** `internal/payments/square_checkout.go`, `internal/payments/handler.go`

#### Routing Logic

```go
// MultiCheckoutService selects provider based on clinic config
switch clinic.PaymentProvider {
case "stripe":  // Moxie clinics
    return stripeCheckout.CreateSession(...)
case "square":  // Non-Moxie clinics
    return squareCheckout.CreateLink(...)
}
```

### Legacy/Fallback: Browser Sidecar

> **The browser sidecar is retained as a fallback only.** It is used when a clinic does not have Moxie API credentials configured (no `medspaId`). For clinics with Moxie API access, the sidecar is not used.

**When sidecar is used:** Clinic has `bookingUrl` but no `medspaId` — system falls back to browser-based availability scraping (~30-60s vs ~1s for API).

**Test Coverage (96 unit tests):**
- Availability scraping, time parsing, retry logic, request validation
- See `browser-sidecar/tests/` for full test suite

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
| No card storage | Stripe/Square-hosted checkout only |
| Webhook verification | Stripe signature validation (`stripe-signature` header) + Square signature validation |

---

## 5. Technical Architecture

### Tech Stack
- **Backend:** Go 1.24+ (tested with Go 1.25.3)
- **Database:** PostgreSQL
- **Cache:** Redis
- **AI:** Claude via AWS Bedrock
- **SMS:** Telnyx (primary), Twilio (fallback)
- **Payments:** Stripe Connect (Moxie clinics) OR Square (OAuth + checkout links)
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
  moxie/               # Moxie GraphQL API client (availability + booking)
  payments/            # Stripe Connect + Square integration (Step 3)
  leads/               # Lead capture
  clinic/              # Per-clinic config
  http/handlers/       # Webhook handlers
  events/              # Outbox for confirmations (Step 4)
  browser/             # Browser sidecar client (Go) — legacy fallback for availability
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
| `POST /webhooks/square` | Square payment notifications (Step 3b) |
| `POST /webhooks/stripe` | Stripe `checkout.session.completed` → triggers Moxie booking (Step 3a) |
| `GET /admin/orgs/{orgID}/conversations` | List conversations |
| `GET /admin/orgs/{orgID}/deposits` | List deposits |

**Browser Sidecar (TypeScript) — Legacy Fallback:**
| Endpoint | Purpose | SLA |
|----------|---------|-----|
| `GET /health` | Health check + browser status | <100ms |
| `GET /ready` | K8s readiness probe | <100ms |
| `POST /api/v1/availability` | Scrape single date availability (fallback only) | <45s |
| `POST /api/v1/availability/batch` | Scrape 1-7 dates (fallback only) | <5min |

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
STRIPE_SECRET_KEY=...
STRIPE_WEBHOOK_SECRET=...
MOXIE_API_URL=https://graphql.joinmoxie.com/v1/graphql
MOXIE_DRY_RUN=false  # Set to true to skip actual Moxie booking
```

---

## 8. Phase II: Voice AI Agent

> **Status:** Architecture complete, implementation planned (4-6 weeks)
> **Architecture doc:** `research/voice-ai-architecture-2026-02-12.md`
>
> ⚠️ **Competitive urgency (Feb 2026):** ConvoCore and others are shipping voice-first AI receptionists targeting med spas NOW. Our SMS-first approach with Moxie booking + Stripe deposit collection remains differentiated (nobody else does qualification → payment → auto-booking in one SMS flow), but Phase II voice AI should be accelerated where possible. The longer we wait, the harder it becomes to differentiate on voice alone.

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
7. After all 5 qualifications → check Moxie availability via GraphQL API (~1s)
8. Patient selects slot via voice → Stripe Checkout link sent via SMS
9. Patient pays → Moxie API books appointment (existing flow)
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
- **AI Care Standard™ compliance:** Launched Feb 11, 2026 — first operational standard for AI communicating directly with patients. Initially aimed at health systems, but expected to trickle down to all patient-facing AI. Enterprise prospects and PE acquirers will ask about compliance certifications. Evaluate requirements and pursue certification as we scale beyond single clinics.
- **AI search visibility integration:** Partner with platforms like Birdeye to help clinics appear in AI-generated search results (ChatGPT, Gemini, Perplexity). Natural upsell alongside the receptionist product as AI search increasingly drives how patients find med spas.

---

## 9. Operational References

- **Deployment:** See `s3://medspa-ai-platform-dev/docs/DEPLOYMENT_ECS.md`
- **Webhook Setup:** See `s3://medspa-ai-platform-dev/docs/RUNBOOK_WEBHOOKS.md`
