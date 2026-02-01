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

### Step 3: Book and Collect Deposit (if applicable)
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
| 3 | Deposit link sent for configured services | Checkout URL in SMS for deposit-eligible services |
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
  browser/             # Browser sidecar client (Go)
browser-sidecar/       # Playwright service (TypeScript/Node.js)
  src/
    scraper.ts         # Availability scraper (Moxie)
    server.ts          # HTTP API for booking checks
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
