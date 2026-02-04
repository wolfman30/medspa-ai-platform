# MedSpa AI Platform Specification

> Single source of truth for business requirements, acceptance criteria, and technical architecture.
> This file is model-agnostic—use it with any AI assistant (Claude, Gemini, ChatGPT, etc.).

---

## 1. Business Problem & Solution

**Problem:** Medical spas lose revenue from missed calls. When potential patients call and get voicemail, 80%+ never call back. Every missed lead is lost revenue—a single Botox patient represents $1,500-3,000/year in recurring visits.

**Solution:** SMS-based AI receptionist that instantly engages missed calls, qualifies leads through conversation, collects deposits, and hands off warm leads to staff for confirmation.

**Result:** Staff effort drops from 15-20 minute intake to 2-3 minute confirmation.

---

## 2. The 4-Step Process

### Step 1: Missed Call → Instant Text Back
| Trigger | Phone call goes unanswered (after-hours) |
|---------|------------------------------------------|
| Action | Send SMS within 5 seconds |
| Message | "Sorry we missed your call, I can help by text..." |
| SLA | <5 seconds from missed call to SMS sent |

**Key files:** `internal/http/handlers/telnyx_webhooks.go`, `internal/messaging/handler.go`

### Step 2: AI Qualifies the Lead
| What | Multi-turn SMS conversation via Claude (AWS Bedrock) |
|------|-----------------------------------------------------|
| Extracts | Desired service, preferred date/time, new vs existing patient, full name |
| Constraints | No medical advice, HIPAA-compliant, warm conversational tone |

**Key files:** `internal/conversation/service.go`, `internal/conversation/llm_service.go`

#### Conversation → Browser Sidecar Integration

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

### Step 3: Book and Collect Deposit

Booking behavior depends on the clinic's platform:

#### Step 3a: Moxie Clinics — Browser Sidecar Booking Automation

When the patient selects a time slot from a Moxie-powered clinic, the system automates the booking flow via the browser sidecar:

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

#### Step 3b: Square Clinics — Deposit Collection

| Deposit Eligibility | Per-clinic configuration (admin sets which services require deposits) |
|--------------------|-----------------------------------------------------------------------|
| Payment | Square checkout link (PCI-compliant hosted page) |
| On Success | SMS confirmation sent to patient |

**Key files:** `internal/conversation/deposit_sender.go`, `internal/payments/square_checkout.go`

### Step 4: Confirm to Patient and Operator
| To Patient | Confirmation SMS with appointment details + deposit status |
|------------|-----------------------------------------------------------|
| To Operator | Notification of qualified lead ready for confirmation |
| Reminders | Future phase: 1 week, 1 day, 3 hours before appointment |

**Key files:** `internal/payments/handler.go` (webhook), `internal/events/outbox.go`

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
| 1 | Missed call triggers SMS within 5 seconds | Webhook → SMS timestamp delta |
| 2 | AI extracts: service, date/time, patient type, name | Lead record has all fields populated |
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
| Explain services offered | Advise what treatment is right |
| Describe procedures | Diagnose symptoms or conditions |
| Share general pricing | Recommend treatment for complaints |
| Answer FAQs (recovery, prep) | Advise on medical emergencies |
| Refer to medical professionals | Provide any clinical guidance |

### PCI (Payment Security)
| Requirement | Implementation |
|-------------|----------------|
| No card storage | Square-hosted checkout only |
| Webhook verification | Square signature validation |

---

## 5. Technical Architecture

### Tech Stack
- **Backend:** Go 1.24
- **Database:** PostgreSQL
- **Cache:** Redis
- **AI:** Claude via AWS Bedrock
- **SMS:** Telnyx (primary), Twilio (fallback)
- **Payments:** Square (OAuth + checkout links)
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

## 8. Future Phases (Out of Scope for MVP)

- **Appointment reminders:** 1 week, 1 day, 3 hours before
- **Voice AI:** Live call handling with sub-second response
- **EMR integration:** Direct booking writes to Nextech, Boulevard, etc.
- **Multi-channel:** Instagram DMs, Google Business Messages, web chat

---

## 9. Operational References

- **Deployment:** See `s3://medspa-ai-platform-dev/docs/DEPLOYMENT_ECS.md`
- **Webhook Setup:** See `s3://medspa-ai-platform-dev/docs/RUNBOOK_WEBHOOKS.md`
