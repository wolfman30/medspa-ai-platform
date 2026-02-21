# MedSpa AI Platform — Architecture

> **How** the platform is built: system design, infrastructure, data models, compliance, and implementation plans.
> For **what** the product does (features, business rules, testing), see [SPEC.md](../SPEC.md).

> **Version:** 2.0 · **Date:** 2026-02-21 · **Last updated:** 2026-02-21

## 1. Overview

The MedSpa AI Platform is evolving from a single-channel SMS text-back system into an **omnichannel AI brain**. Every patient interaction — phone call, text message, Instagram DM, or proactive outreach — flows through a shared conversation engine. Each communication channel is an adapter that translates to and from a unified internal format.

**Vision:** One AI brain. Many channels. Unified patient identity. Proactive lifecycle management.

```
         ┌─────────┐  ┌─────────┐  ┌──────────┐  ┌────────────┐
         │  SMS    │  │  Voice  │  │Instagram │  │ Proactive  │
         │(live ✅)│  │(build🔨)│  │  DM (next)│  │ Rebook (Q3)│
         └────┬────┘  └────┬────┘  └─────┬────┘  └──────┬─────┘
              │            │             │               │
              ▼            ▼             ▼               ▼
         ┌────────────────────────────────────────────────────┐
         │              Channel Adapter Layer                  │
         │  Normalize inbound → internal message format        │
         │  Format outbound  → channel-specific delivery       │
         └──────────────────────┬─────────────────────────────┘
                                │
                                ▼
         ┌────────────────────────────────────────────────────┐
         │             Conversation Engine (Brain)             │
         │                                                     │
         │  ┌─────────────┐  ┌──────────────┐  ┌───────────┐ │
         │  │ Claude Haiku │  │ Qualification│  │  Booking  │ │
         │  │  (Bedrock)   │  │    Logic     │  │  Engine   │ │
         │  └─────────────┘  └──────────────┘  └───────────┘ │
         │                                                     │
         └──────────────────────┬─────────────────────────────┘
                                │
                    ┌───────────┼───────────┐
                    ▼           ▼           ▼
              ┌──────────┐ ┌────────┐ ┌──────────┐
              │ Patient  │ │ Moxie  │ │ Stripe   │
              │ Identity │ │  API   │ │ Connect  │
              │ Store    │ │        │ │          │
              └──────────┘ └────────┘ └──────────┘
```

## 2. Omnichannel Architecture

### 2.1 Channel Roadmap

| # | Channel | Status | Timeline | Notes |
|---|---------|--------|----------|-------|
| 1 | SMS text-back | ✅ Live | — | Missed call → SMS qualification → booking |
| 2 | Voice AI | 🔨 Building | Q1 2026 | Sub-second latency voice conversation |
| 3 | Instagram DM | 📋 Next | Q2 2026 | 60-70% of med spa patients discover via IG |
| 4 | Proactive rebooking | 📋 Planned | Q3 2026 | Outbound: auto-reach when treatments wear off |

### 2.2 Shared Conversation Engine

The conversation engine is **channel-agnostic**. It operates on a unified message format:

```go
// Internal message — every channel normalizes to this
type ConversationMessage struct {
    ID             string
    ConversationID string          // groups messages across channels
    PatientID      string          // resolved patient identity
    OrgID          string
    Channel        ChannelType     // sms | voice | instagram | outbound
    Direction      Direction       // inbound | outbound
    Content        string          // text content (STT output for voice, message text for SMS/IG)
    Metadata       map[string]any  // channel-specific extras (audio duration, IG media, etc.)
    Timestamp      time.Time
}

type ChannelType string
const (
    ChannelSMS       ChannelType = "sms"
    ChannelVoice     ChannelType = "voice"
    ChannelInstagram ChannelType = "instagram"
    ChannelOutbound  ChannelType = "outbound"
)
```

The engine:
1. Receives a `ConversationMessage` from any channel adapter
2. Loads conversation state (qualifications collected so far)
3. Runs the LLM with channel-appropriate system prompt adjustments
4. Returns a response as text
5. The channel adapter delivers it (TTS for voice, SMS API for text, IG API for DMs)

**Channel-specific prompt additions** are injected by the adapter, not hardcoded in the engine:

| Channel | Prompt Addition |
|---------|----------------|
| Voice | "Keep responses to 1-2 sentences. Use spoken language. Say 'I'll text you a link' for URLs." |
| SMS | (current behavior — no change) |
| Instagram | "Use casual tone. Emoji OK. Can send images. Link to booking page." |
| Outbound | "You are reaching out proactively. Be warm, not salesy. Mention their last visit." |

### 2.3 Channel Adapter Interface

```go
// Every channel implements this interface
type ChannelAdapter interface {
    // Type returns the channel type
    Type() ChannelType

    // HandleInbound processes an inbound event from the channel.
    // Normalizes it to ConversationMessage(s) and feeds the engine.
    HandleInbound(ctx context.Context, event any) error

    // DeliverResponse sends the engine's text response via the channel.
    // For voice: text → TTS → audio stream. For SMS: text → Telnyx API. Etc.
    DeliverResponse(ctx context.Context, conversationID string, text string) error
}
```

### 2.4 Patient Identity Resolution

A single patient may interact across multiple channels. The system must unify identity:

```
Phone call from +1-555-0100  ──┐
SMS from +1-555-0100           ──┼──► Patient: Sarah Johnson (id: pat_abc123)
IG DM from @sarah.j.beauty    ──┘
```

**Resolution strategy:**

| Signal | Priority | Notes |
|--------|----------|-------|
| Phone number (E.164) | Primary | Matches SMS and Voice immediately |
| Instagram username | Secondary | Linked when patient provides phone in IG DM |
| Name + clinic combo | Tertiary | Fuzzy match for edge cases |

The `patient_identities` table links a single patient to multiple channel identifiers (phone, IG user ID, etc.). See **Section 4.2** for the canonical schema definition.

**Cross-channel conversation continuity:** If a patient calls and gets handed off to SMS, or DMs on IG after seeing a missed call, the engine loads the existing conversation state. The patient doesn't repeat themselves.

### 2.5 Treatment Lifecycle Tracking (Proactive Rebooking)

Store treatment dates and known durations to power proactive outreach. The `treatment_records` table tracks each treatment with computed rebooking dates. See **Section 4.3** for the canonical schema definition.

**Known treatment durations:**

| Treatment | Typical Duration | Rebook Window |
|-----------|-----------------|---------------|
| Botox / Dysport / Xeomin | 12 weeks | Reach out at week 10 |
| Juvederm / Restylane (lips) | 6–9 months | Reach out at month 5 |
| Juvederm / Restylane (cheeks) | 12–18 months | Reach out at month 10 |
| Sculptra | 2 years | Reach out at month 20 |
| Hydrafacial | 4 weeks | Reach out at week 3 |
| Microneedling (series) | 4–6 weeks between | Reach out at week 3 |
| IPL/BBL | 4 weeks (series), then annual | Reach out at week 3 / month 10 |

**Proactive outreach flow:**
1. Nightly job queries `treatment_records WHERE next_due_date <= NOW() + interval '14 days' AND rebook_status = 'pending'`
2. Channel router selects best channel (SMS preferred, IG DM if no phone)
3. Outbound adapter sends warm message: "Hi Sarah! It's been about 3 months since your last Botox at Forever 22. Ready to schedule your touch-up? 💉"
4. If patient responds → conversation engine handles booking (same qualification flow)

### 2.6 Channel Router

Determines how to reach a patient and routes responses:

```go
type ChannelRouter struct {
    adapters map[ChannelType]ChannelAdapter
}

// SelectChannel picks the best channel for outbound communication
func (r *ChannelRouter) SelectChannel(patient Patient, purpose string) ChannelType {
    switch purpose {
    case "rebook_outreach":
        // Prefer SMS (highest open rate), fall back to IG
        if patient.HasPhone() { return ChannelSMS }
        if patient.HasInstagram() { return ChannelInstagram }
    case "booking_confirmation":
        // Always SMS for confirmations (reliable delivery)
        return ChannelSMS
    case "payment_link":
        return ChannelSMS
    }
    return ChannelSMS
}
```

---

## 3. Voice AI — Detailed Design

Everything below is the voice channel adapter implementation. The voice pipeline is the most complex adapter due to real-time audio requirements.

### 3.1 Provider Selection

| Component | Primary | Fallback | Rationale |
|-----------|---------|----------|-----------|
| Telephony | Telnyx Voice API | — | Already integrated for SMS; WebSocket media streams; single vendor |
| STT | Deepgram Nova-3 | Amazon Transcribe Streaming | 200ms latency, built-in VAD, interim results, $0.0077/min |
| TTS | Cartesia Sonic | ElevenLabs Turbo v2.5 → AWS Polly | 90ms first-byte, streaming WebSocket, HIPAA BAA, $0.03/min |
| LLM | Claude 3.5 Haiku (Bedrock) | — | Same as SMS; streaming; ~200ms first token |

All providers selected have HIPAA BAAs available.

### 3.2 Voice Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     ECS Fargate (Go Service)                     │
│                                                                  │
│  ┌──────────────┐                                                │
│  │ Telnyx Voice  │  call.initiated webhook                       │
│  │  Webhook      │─────────────────────┐                         │
│  │  Handler      │                     │                         │
│  └──────────────┘                     ▼                         │
│                              ┌─────────────────┐                │
│  ┌──────────────┐            │  Call Router     │                │
│  │ Feature       │◄───────── │  (voice_ai_      │                │
│  │ Toggle        │ enabled?  │   enabled check) │                │
│  │ Service       │           └────────┬────────┘                │
│  └──────────────┘                    │ yes                      │
│                                      ▼                          │
│                        ┌─────────────────────────┐              │
│                        │   Voice Channel Adapter  │              │
│                        │   (1 goroutine per call) │              │
│                        └──────────┬──────────────┘              │
│                                   │                              │
│                    ┌──────────────┼──────────────┐              │
│                    ▼              ▼              ▼              │
│           ┌──────────────┐ ┌──────────┐ ┌──────────────┐       │
│           │  STT Stream  │ │Conversa- │ │  TTS Stream  │       │
│           │  (Deepgram)  │ │tion      │ │  (Cartesia)  │       │
│           │  WebSocket   │ │Engine    │ │  WebSocket   │       │
│           └──────────────┘ │(shared)  │ └──────────────┘       │
│                            └──────────┘                         │
│                                   │                              │
│                    ┌──────────────┼──────────────┐              │
│                    ▼              ▼              ▼              │
│           ┌──────────────┐ ┌──────────┐ ┌──────────────┐       │
│           │ Patient      │ │  Moxie   │ │  SMS Adapter │       │
│           │ Identity     │ │   API    │ │  (handoff)   │       │
│           └──────────────┘ └──────────┘ └──────────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

### 3.3 Audio Pipeline (Per Active Call)

Each call maintains 3 persistent WebSocket connections plus a Bedrock streaming session:

```
Patient ──phone──► Telnyx ──WebSocket──► Go Service
                                            │
                     ┌──────────────────────┤
                     │                      │
                     ▼                      ▼
              ┌─────────────┐    ┌──────────────────┐
              │ μ-law 8kHz  │    │ Audio Mixer /    │
              │ → PCM 16kHz │    │ Barge-in Ctrl    │
              │   Decoder   │    │ (flush TTS on    │
              └──────┬──────┘    │  patient speech)  │
                     │           └──────────────────┘
                     ▼                      ▲
              Deepgram STT WS              │
                     │                      │
                     ▼                      │
              ┌─────────────┐              │
              │ Transcript  │    Cartesia TTS WS
              │  + VAD      │         ▲
              │  Events     │         │
              └──────┬──────┘    ┌────┴────────┐
                     │           │ Text chunks  │
                     ▼           │ (sentence    │
              ┌─────────────┐   │  boundaries) │
              │Conversation │───┘              │
              │  Engine     │                  │
              │  (shared)   │                  │
              └─────────────┘                  │
                                               ▼
                                    PCM → μ-law encode
                                               │
                                    Telnyx WS ◄─┘
                                               │
                                          Patient hears
```

### 3.4 Latency Budget

**Target: <500ms** from patient-stops-speaking to patient-hears-first-word.

| Stage | Budget | Implementation |
|-------|--------|----------------|
| VAD endpointing | 100ms | Deepgram `endpointing=100` (adaptive: 300ms for longer utterances) |
| STT final → transcript | ~0ms | Use interim results; don't wait for `is_final` |
| LLM first token | 200ms | Claude Haiku streaming via Bedrock; pre-warmed connection |
| TTS first audio byte | 100ms | Cartesia streaming; send first sentence as soon as LLM emits it |
| Network + encoding | 50ms | All services co-located in us-east-1; persistent WebSockets |
| **Total** | **~450ms** | |

**Key latency optimizations:**

1. **Interim STT → LLM pipelining:** Begin LLM inference on interim transcripts. If the final differs (rare, ~5%), cancel and reprocess.
2. **LLM → TTS sentence streaming:** Stream LLM output to TTS at sentence/clause boundaries. Patient hears sentence 1 while LLM generates sentence 2.
3. **Pre-warmed connections:** All WebSockets kept alive across calls. No per-utterance handshake overhead.
4. **Pattern-match shortcuts:** For simple responses ("yes", "no", days of week, common services), skip LLM and use pre-computed TTS audio clips cached in memory.
5. **Speculative pre-generation:** After collecting each qualification, pre-generate the next prompt's TTS before the patient responds.

### 3.5 Call Lifecycle

```
1. Telnyx sends call.initiated webhook
2. Call Router checks: voice_ai_enabled? business hours? service healthy?
   → If NO to any: fall through to voicemail → SMS text-back (current behavior)
3. Answer call via Telnyx API (<1s)
4. Open Deepgram STT WebSocket
5. Open Cartesia TTS WebSocket
6. Stream greeting TTS → Telnyx → Patient
7. Begin bidirectional audio loop:
   a. Patient audio → Telnyx WS → decode → Deepgram STT WS
   b. Deepgram transcript → Conversation Engine (streaming)
   c. Engine response → Cartesia TTS WS → encode → Telnyx WS → Patient
   d. On barge-in (VAD detects speech during TTS): flush TTS buffer, resume STT
8. Conversation Engine manages qualification state (shared with SMS)
9. On booking: call Moxie API, send SMS confirmation, wrap up call
10. On completion/failure: store voice_calls record, upload recording to S3
```

### 3.6 Concurrency Limits & Backpressure

Each active voice call maintains **4 long-lived connections**:

| Connection | Type | Lifetime |
|-----------|------|----------|
| Telnyx media stream | WebSocket | Duration of call |
| Deepgram STT | WebSocket | Duration of call |
| Cartesia TTS | WebSocket | Duration of call |
| Bedrock Claude Haiku | HTTP/2 stream | Per-utterance (kept alive via connection pool) |

**Per-task concurrency:**

- Each ECS task can handle **~25 concurrent calls** (100 WebSockets + 25 Bedrock streams).
- Go's goroutine model handles this easily; bottleneck is external connection limits.
- Deepgram: 100 concurrent connections per API key (default). Cartesia: similar.
- `max_concurrent_calls` per clinic (default 5) prevents any single clinic from monopolizing capacity.

**Backpressure strategy:**

1. **Connection pool pre-warming:** Maintain a pool of pre-authenticated WebSocket connections to Deepgram and Cartesia. New calls grab from the pool instead of handshaking.
2. **Admission control:** Track active calls per ECS task. When at capacity (25), reject new calls with Telnyx `call.rejected` → falls back to voicemail → SMS text-back. Patient experience is graceful, not broken.
3. **Circuit breaker per provider:** If Deepgram/Cartesia/Bedrock error rate exceeds 50% over 30s, trip the circuit → all new calls fall back to SMS text-back. Existing calls attempt graceful handoff: "I'm having trouble hearing you — let me text you instead."
4. **ECS auto-scaling:** CloudWatch alarm on custom metric `active_voice_calls / task_count`. Scale out at 80% capacity (20 calls/task), scale in at 20% (5 calls/task).
5. **Connection exhaustion protection:** Hard limit of 120 WebSockets per task (30 calls × 4 connections). Beyond this, Go's `net` layer returns errors caught by admission control.

**ECS task sizing for voice:**

| Resource | Current (SMS-only) | With Voice AI |
|----------|-------------------|---------------|
| CPU | 256 (0.25 vCPU) | 512 (0.5 vCPU) — audio encoding overhead |
| Memory | 512 MB | 1024 MB — WebSocket buffers, audio frame queues |
| Tasks (dev) | 1 | 1 (sufficient for testing) |
| Tasks (prod) | 2 | 2-4 (auto-scaled on call volume) |

### 3.7 Feature Toggle

#### Per-Clinic Configuration

Stored in PostgreSQL `organizations` table (new JSONB column) and cached in Redis:

```json
{
  "voice_ai_enabled": false,
  "voice_ai_config": {
    "greeting": "Hi! Thanks for calling {clinic_name}. How can I help you today?",
    "after_hours_greeting": "...",
    "voice_id": "cartesia_default_warm_female",
    "transfer_number": "+15551234567",
    "max_concurrent_calls": 5,
    "recording_enabled": true,
    "recording_consent_message": "This call may be recorded for quality purposes."
  }
}
```

#### Toggle Behavior Matrix

| `voice_ai_enabled` | Business Hours | Behavior |
|--------------------|---------------|----------|
| `false` | Any | Current flow: voicemail → missed call → SMS text-back |
| `true` | During hours | Voice AI answers → qualification → booking |
| `true` | After hours (voice_during_business_hours_only=false) | Voice AI answers with after-hours greeting |
| `true` | After hours (voice_during_business_hours_only=true) | Current flow: voicemail → SMS text-back |

#### Rollout Stages

Environment variable: `VOICE_AI_ROLLOUT=internal|alpha|beta|ga`

1. **internal** — AI Wolf team test clinics only
2. **alpha** — First client (Forever 22 Med Spa)
3. **beta** — 3-5 high-volume SMS clinics
4. **ga** — Available to all via admin toggle

### 3.8 Fallback & Resilience

| Failure | Detection | Fallback |
|---------|-----------|----------|
| Voice AI service down | Health check fails | Voicemail → SMS text-back |
| Mid-call crash | Goroutine panic recovery | "Let me text you instead" → SMS handoff |
| Poor audio quality | STT confidence <0.3 for 3 turns | "I'm having trouble hearing. I'll text you." → SMS handoff |
| Deepgram down | Circuit breaker (3 failures/30s) | Switch to Amazon Transcribe Streaming |
| Cartesia down | Circuit breaker | Switch to ElevenLabs → AWS Polly |
| All STT/TTS down | All circuit breakers open | Voicemail → SMS text-back |
| Call exceeds 10 min | Timer | "Let me text you to wrap up." → SMS handoff |

Circuit breaker: 3 consecutive failures → open 30s → half-open (1 request) → close on success.

---

## 4. Data Model

### 4.1 voice_calls Table

```sql
CREATE TABLE voice_calls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    lead_id         UUID REFERENCES leads(id),
    telnyx_call_id  TEXT NOT NULL UNIQUE,
    caller_phone    TEXT NOT NULL,
    direction       TEXT NOT NULL DEFAULT 'inbound',
    status          TEXT NOT NULL DEFAULT 'in_progress',
    outcome         TEXT,  -- booked | qualified | transferred | abandoned | sms_handoff
    duration_sec    INTEGER,
    recording_url   TEXT,
    transcript      JSONB, -- [{role, text, timestamp}]
    qualifications  JSONB, -- same structure as SMS
    cost_cents      INTEGER,
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_voice_calls_org ON voice_calls(org_id);
CREATE INDEX idx_voice_calls_phone ON voice_calls(caller_phone);
CREATE INDEX idx_voice_calls_started ON voice_calls(started_at);
```

### 4.2 patient_identities Table (Canonical Schema)

> This is the canonical schema definition. Section 2.4 provides the conceptual overview.

```sql
CREATE TABLE patient_identities (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id         UUID NOT NULL REFERENCES leads(id),
    channel            TEXT NOT NULL,       -- sms, voice, instagram
    channel_identifier TEXT NOT NULL,       -- phone (E.164), IG user ID, etc.
    linked_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(channel, channel_identifier)
);

CREATE INDEX idx_patient_identities_lookup
    ON patient_identities(channel, channel_identifier);
```

### 4.3 treatment_records Table (Canonical Schema)

> This is the canonical schema definition. Section 2.5 provides the conceptual overview.

```sql
CREATE TABLE treatment_records (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id      UUID NOT NULL REFERENCES leads(id),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    service_name    TEXT NOT NULL,
    treatment_date  DATE NOT NULL,
    next_due_date   DATE,
    rebook_status   TEXT DEFAULT 'pending', -- pending | contacted | booked | declined
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_treatment_records_due
    ON treatment_records(next_due_date, rebook_status);
```

### 4.4 Recording Storage

- S3 bucket: `medspa-voice-recordings/{org_id}/{call_id}.wav`
- Retention: 90 days (configurable per clinic)
- Encryption: AES-256 SSE-S3

### 4.5 PHI Handling & Compliance

Voice calls and transcripts contain PHI (Protected Health Information). All handling must comply with HIPAA requirements.

**Logging & metrics redaction:**

| Data type | Log behavior | Example |
|-----------|-------------|---------|
| Patient name | Redacted → `[NAME]` | `"Processing call for [NAME]"` |
| Phone number | Last 4 only | `"Inbound call from ***2713"` |
| Service interest | Allowed (not PHI) | `"Service: Botox"` |
| Transcript text | Never logged | Stored only in S3 (encrypted) and DB |
| Call metadata (duration, status) | Allowed | `"Call ended, duration=142s, status=booked"` |

**Implementation:**

- `internal/voice/redact.go` — `RedactPHI(msg string) string` applied to all log output
- Structured logger fields use redacted variants: `"patient": redact.Name(name)`
- CloudWatch log group `/ecs/medspa-voice` — retention: **30 days** (shorter than app logs)
- No PHI in CloudWatch metrics — only aggregate counts (calls/min, avg duration, booking rate)

**Recording & transcript storage:**

- Recordings: S3 with SSE-S3 encryption, bucket policy denies unencrypted uploads
- Transcripts: Stored in `voice_calls.transcript` (PostgreSQL) — encrypted at rest via RDS encryption
- Access: Admin portal only, authenticated via Cognito, audit-logged
- Deletion: Automated lifecycle policy deletes recordings after retention period; patient deletion request triggers immediate purge of all recordings + transcripts

**Provider BAAs:**

All voice AI providers selected have HIPAA Business Associate Agreements:
- Telnyx: BAA available (already signed for SMS)
- Deepgram: BAA available for Enterprise tier
- Cartesia: BAA available
- AWS Bedrock: Covered under existing AWS BAA

**Log retention summary:**

| Log source | Retention | Contains PHI? |
|-----------|-----------|---------------|
| Application logs (`/ecs/medspa-*-api`) | 90 days | No (redacted) |
| Voice logs (`/ecs/medspa-voice`) | 30 days | No (redacted) |
| S3 recordings | 90 days (configurable) | Yes (encrypted) |
| DB transcripts | Until patient deletion | Yes (encrypted at rest) |

## 5. Go Package Structure

```
internal/
  conversation/                 # ← SHARED BRAIN (channel-agnostic)
    engine.go                   # ConversationEngine — runs LLM, manages state
    message.go                  # ConversationMessage, ChannelType, Direction
    state.go                    # Qualification state machine
    prompts.go                  # Base system prompts (channel adapters add their own)

  identity/                     # Patient identity resolution
    resolver.go                 # Cross-channel identity matching
    store.go                    # patient_identities CRUD

  channel/                      # Channel adapter layer
    adapter.go                  # ChannelAdapter interface
    router.go                   # ChannelRouter — selects channel for outbound

    sms/                        # SMS adapter (refactor existing)
      adapter.go                # Implements ChannelAdapter
      telnyx.go                 # Telnyx SMS API client (existing, relocated)

    voice/                      # Voice adapter (new)
      adapter.go                # Implements ChannelAdapter
      session.go                # Per-call session goroutine, state machine
      pipeline.go               # Audio pipeline orchestration
      call_router.go            # Toggle check, business hours, health
      stt/
        stt.go                  # STT interface
        deepgram.go             # Deepgram Nova-3 WebSocket client
        transcribe.go           # Amazon Transcribe fallback
      tts/
        tts.go                  # TTS interface
        cartesia.go             # Cartesia Sonic WebSocket client
        elevenlabs.go           # ElevenLabs fallback
        polly.go                # AWS Polly emergency fallback
      audio/
        codec.go                # μ-law ↔ PCM conversion
        mixer.go                # Audio mixing, barge-in flush
        vad.go                  # Supplemental VAD / backchannel detection
      telnyx/
        media.go                # Telnyx WebSocket media stream handler
        commands.go             # Call control (answer, hangup, transfer)
      store/
        store.go                # voice_calls CRUD
      metrics.go                # Voice-specific Prometheus metrics

    instagram/                  # Instagram DM adapter (future)
      adapter.go                # Implements ChannelAdapter
      webhook.go                # IG Messaging API webhook handler
      api.go                    # IG send message client

  rebook/                       # Proactive rebooking engine (future)
    scheduler.go                # Nightly job: query due treatments
    outreach.go                 # Generate rebook messages, send via ChannelRouter
    store.go                    # treatment_records CRUD

  circuit_breaker.go            # Shared circuit breaker for external services
```

## 6. Key Interfaces

```go
// ChannelAdapter — every communication channel implements this
type ChannelAdapter interface {
    Type() ChannelType
    HandleInbound(ctx context.Context, event any) error
    DeliverResponse(ctx context.Context, conversationID string, text string) error
}

// ConversationEngine — the shared brain
type ConversationEngine interface {
    // ProcessMessage takes a normalized message and returns a response.
    // Channel adapter is responsible for delivery.
    ProcessMessage(ctx context.Context, msg ConversationMessage) (string, error)
    // LoadState returns the current qualification state for a conversation.
    LoadState(ctx context.Context, conversationID string) (*QualificationState, error)
}

// IdentityResolver — cross-channel patient matching
type IdentityResolver interface {
    Resolve(ctx context.Context, channel ChannelType, identifier string) (patientID string, err error)
    Link(ctx context.Context, patientID string, channel ChannelType, identifier string) error
}

// STT provider interface (voice-specific)
type STTProvider interface {
    StreamAudio(ctx context.Context, opts STTOptions) (io.WriteCloser, <-chan Transcript, error)
}

// TTS provider interface (voice-specific)
type TTSProvider interface {
    Synthesize(ctx context.Context, text string, opts TTSOptions) (<-chan []byte, error)
}
```

## 7. Cost Estimates

### Voice (per call, 2.5 min average)

| Component | Usage | Unit Cost | Cost/Call |
|-----------|-------|-----------|-----------|
| Telnyx inbound | 2.5 min | $0.01/min | $0.025 |
| Deepgram STT | 1.25 min | $0.0077/min | $0.010 |
| Cartesia TTS | 1.25 min | $0.030/min | $0.038 |
| Claude Haiku (Bedrock) | ~5 turns | ~$0.001/turn | $0.005 |
| **Total** | | | **~$0.08/call** |

At 500 calls/month per clinic: ~$40/month marginal cost.

### Instagram DM (per conversation, estimated)

| Component | Cost/Conversation |
|-----------|-------------------|
| IG API | Free |
| Claude Haiku | ~$0.005 |
| **Total** | **~$0.005** |

### Proactive Rebooking (per outreach)

| Component | Cost/Message |
|-----------|-------------|
| SMS send | $0.01 |
| Claude Haiku | ~$0.001 |
| **Total** | **~$0.011** |

## 8. Implementation Plan

### Phase 1: Voice AI (Q1 2026) — PR Sequence

| PR | Title | Content |
|----|-------|---------|
| 1 | Omnichannel AI design doc | This document |
| 2 | Shared conversation engine refactor | Extract `internal/conversation/` from existing SMS code |
| 3 | Patient identity resolution | `internal/identity/`, `patient_identities` migration |
| 4 | Voice AI config/toggle | DB migration, config structs, feature toggle |
| 5 | Telnyx WebSocket media stream handler | Audio codec, media stream connect/disconnect |
| 6 | STT integration (Deepgram) | Deepgram client, STT interface, Transcribe fallback |
| 7 | TTS integration (Cartesia) | Cartesia client, TTS interface, ElevenLabs/Polly fallbacks |
| 8 | Voice conversation orchestrator | Session manager, audio pipeline, barge-in, STT→LLM→TTS |
| 9 | Voice call routing + webhook | Telnyx voice webhook, call router, toggle checks |
| 10 | Recording & storage | S3 upload, voice_calls CRUD |
| 11 | Metrics & monitoring | Prometheus metrics, latency histograms |
| 12 | Integration tests | End-to-end test with mocked providers |

### Phase 2: Instagram DM (Q2 2026)

- IG Messaging API webhook + adapter
- Media message handling (patients send photos)
- IG-specific conversation UX

### Phase 3: Proactive Rebooking (Q3 2026)

- Treatment lifecycle tracking
- Nightly scheduler
- Outbound message engine via ChannelRouter

## 9. Open Questions

1. **Conversation engine refactor scope:** How much of the current SMS handler can be extracted cleanly? Need to audit `internal/` to understand coupling.
2. **Instagram API access:** Requires Meta Business verification + IG Professional account. Timeline for approval?
3. **Telnyx WebSocket vs Fork:** WebSocket gives barge-in control; fork is simpler. **Recommendation: WebSocket.**
4. **ECS task sizing for voice:** WebSocket-heavy, not CPU-heavy. Same task initially, separate at scale.
5. **Recording consent:** Always announce (simplest compliance) or state-by-state? **Recommendation: Always.**
6. **Rebooking opt-in:** Patients must consent to proactive outreach. Capture during booking flow.
