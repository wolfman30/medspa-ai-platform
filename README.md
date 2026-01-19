# MedSpa AI Platform

AI-powered lead recovery platform that converts missed calls into qualified, deposit-backed appointments.

## The Problem

**Medical spas lose revenue from leads that go cold before staff can respond.**

When potential patients call and get voicemail, 80%+ never call back. Manual follow-up is slow, inconsistent, and doesn't scale. Every missed lead is lost revenue—a single Botox patient represents $1,500-3,000/year in recurring visits.

## The Solution

**Instant AI engagement that qualifies leads, collects deposits, and hands off warm leads to staff for confirmation.**

This platform recovers lost revenue by:
1. **Instantly responding** to missed calls via SMS (within 5 seconds)
2. **Qualifying leads** through natural AI conversation
3. **Collecting deposits** to secure commitment and reduce no-shows
4. **Submitting bookings** via the medspa's existing booking widget
5. **Notifying staff** with pre-qualified leads ready for confirmation

### The Workflow

```
Missed Call
    ↓
Instant SMS Text-Back (< 5 seconds)
    ↓
AI Qualifies Lead via Conversation
    ├── Service interest
    ├── Full name
    ├── New or existing patient
    └── Scheduling preferences
    ↓
Collect Deposit Payment (Square checkout)
    ↓
AI Submits Booking via Public Widget
    ↓
Staff Receives Notification
    ↓
Staff Confirms Appointment (2-3 min call vs 15-20 min intake)
```

### Staff Effort: Before vs After

| Without Platform | With Platform |
|------------------|---------------|
| Missed call notification | Qualified lead + deposit + booking request |
| Staff calls back, qualifies from scratch | Lead already qualified |
| Staff collects: name, service, timing | Info already captured by AI |
| Staff sends deposit link manually | Deposit already paid |
| Staff enters booking into system | Booking already submitted |
| **15-20 minute intake** | **2-3 minute confirmation** |

---

## Critical Requirements

### HIPAA Compliance (PHI Protection)

Medical spas handle Protected Health Information (PHI). This platform is designed for HIPAA compliance to prevent fines and lawsuits.

**Why AWS Bedrock:** We use Claude via AWS Bedrock (not direct Anthropic API) because AWS offers a **Business Associate Agreement (BAA)**. This is required for HIPAA-compliant AI processing of patient data.

| Compliance Measure | Implementation |
|--------------------|----------------|
| BAA with AI provider | AWS Bedrock (BAA signed) |
| Data encryption in transit | TLS 1.2+ on all endpoints |
| Data encryption at rest | AWS RDS/Redis encryption |
| PHI detection | Auto-redact SSN, DOB, medical IDs in logs |
| Audit logging | All PHI access logged with timestamps |
| Access controls | JWT auth, org-scoped data isolation |

**What counts as PHI:** Names + health info, phone numbers + appointment details, payment info linked to health services.

### No Medical Advice (Liability Protection)

The AI **never** provides medical advice. This protects the medspa from liability.

| AI CAN | AI CANNOT |
|--------|-----------|
| Explain services offered | Advise what treatment is right for patient |
| Describe what a procedure involves | Diagnose symptoms or conditions |
| Share general pricing | Recommend treatment for complaints |
| Answer FAQs about recovery, prep | Advise on medical emergencies |
| Refer to medical professionals | Provide any clinical guidance |

### Tone & Style

- **Warm and hospitable** — welcoming, never robotic
- **Informal** — conversational, like texting a real person
- **Transparent** — patient knows it's AI, but experience feels natural
- **Concise** — SMS-appropriate message lengths

Example:
> "Hey! Thanks for reaching out. I can definitely help you get booked for Botox. Are you a new patient or have you been in before?"

### Response Time Requirements

Fast response times are critical for natural conversation flow and user experience.

**SMS (Phase 1):**
| Event | Target | Why |
|-------|--------|-----|
| Missed call → ack SMS | <5 seconds | Capture lead before they move on |
| User message → quick ack | <2 seconds | "Got it - one moment..." feels responsive |
| Quick ack → full AI response | <10 seconds | LLM processing + quality response |

The quick ack pattern ensures the patient never waits in silence—they get immediate acknowledgment while the AI generates the full response.

**Voice AI (Phase 2):**
| Event | Target | Why |
|-------|--------|-----|
| User finishes speaking → AI response | <800ms | Natural conversation has 200-500ms gaps |
| End-to-end turn latency | <1 second | Longer feels like dead air / confusion |

Sub-second voice response requires: streaming LLM output, edge processing, and/or response pre-generation.

---

## Roadmap

### Phase 1: SMS Lead Recovery (Current)
- Missed call → text-back
- AI qualification via SMS
- Square deposit collection
- Headless booking via widget
- Staff notification and confirmation

### Phase 2: Voice AI Agent (Future)
- **Mode A:** AI answers calls during business hours, offers callback after hours
- **Mode B:** Patient texts "voice" or "talk" → AI calls them back
- Mode configurable per medspa owner preference

### Future Phases
- Social media lead capture (Instagram comments/DMs, Facebook)
- Website form integration
- Google Business Messages
- Multi-location support

---

## Technical Overview

### Architecture

```
cmd/
  api/                 # HTTP API + webhooks
  conversation-worker/ # SQS worker for LLM + deposits
  messaging-worker/    # Telnyx polling + retry
  voice-lambda/        # Voice webhook forwarder
  migrate/             # DB migrations
internal/              # Core domains
  conversation/        # AI conversation engine
  messaging/           # SMS (Telnyx/Twilio)
  payments/            # Square integration
  leads/               # Lead capture
  clinic/              # Per-clinic config
infra/terraform/       # AWS infrastructure
```

### Tech Stack
- **Backend:** Go 1.24
- **Database:** PostgreSQL
- **Cache:** Redis
- **AI:** Claude via AWS Bedrock
- **SMS:** Telnyx (primary), Twilio (fallback)
- **Payments:** Square (OAuth + checkout links)
- **Infrastructure:** AWS ECS/Fargate

### Key Endpoints

**Webhooks (public):**
- `POST /webhooks/telnyx/messages` — Inbound SMS
- `POST /webhooks/telnyx/voice` — Missed call trigger
- `POST /webhooks/square` — Payment notifications

**Admin (JWT protected):**
- `GET /admin/orgs/{orgID}/conversations` — List conversations
- `GET /admin/orgs/{orgID}/deposits` — List deposits
- `GET /admin/clinics/{orgID}/config` — Clinic configuration

---

## Local Development

### Prerequisites
- Go 1.24+
- Docker (for Postgres/Redis)
- Make or Task

### Quickstart

```bash
cp .env.example .env
docker compose up --build
DATABASE_URL=postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable go run ./cmd/migrate
curl http://localhost:8082/health
```

### Testing

```bash
# Unit tests
make test

# Regression tests (fast, deterministic)
make regression

# E2E tests (requires running API)
make e2e
```

---

## Documentation

- **MVP Status:** `docs/MVP_STATUS.md`
- **Product Flows:** `docs/revenue-mvp.md`
- **Deployment:** `docs/DEPLOYMENT_ECS.md`
- **Live Validation:** `docs/LIVE_DEV_CHECKS.md`
- **E2E Results:** `docs/E2E_TEST_RESULTS.md`

---

## License

Proprietary - All rights reserved
