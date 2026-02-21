# Voice AI Technical Design Document

> **Version:** 1.0 · **Date:** 2026-02-21 · **Status:** RFC (Request for Comments)
> **Author:** Voice AI Agent · **Reviewers:** Engineering Team

## 1. Overview

This document defines the technical architecture for adding real-time Voice AI to the MedSpa AI Platform. The system answers inbound calls, qualifies patients using the same 5-step flow as SMS, and books appointments — all with sub-second response latency.

**Goal:** Patient calls → AI answers instantly → natural conversation → appointment booked. Total call: 2-3 minutes. Response latency: <500ms perceived.

## 2. Provider Selection Summary

| Component | Primary | Fallback | Rationale |
|-----------|---------|----------|-----------|
| Telephony | Telnyx Voice API | — | Already integrated for SMS; WebSocket media streams; single vendor |
| STT | Deepgram Nova-3 | Amazon Transcribe Streaming | 200ms latency, built-in VAD, interim results, $0.0077/min |
| TTS | Cartesia Sonic | ElevenLabs Turbo v2.5 → AWS Polly | 90ms first-byte, streaming WebSocket, HIPAA BAA, $0.03/min |
| LLM | Claude 3.5 Haiku (Bedrock) | — | Same as SMS; streaming; ~200ms first token |

All providers selected have HIPAA BAAs available.

## 3. Architecture

### 3.1 High-Level Component Diagram

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
│                        │   Voice Call Session     │              │
│                        │   (1 goroutine per call) │              │
│                        └──────────┬──────────────┘              │
│                                   │                              │
│                    ┌──────────────┼──────────────┐              │
│                    ▼              ▼              ▼              │
│           ┌──────────────┐ ┌──────────┐ ┌──────────────┐       │
│           │  STT Stream  │ │   LLM    │ │  TTS Stream  │       │
│           │  (Deepgram)  │ │ (Bedrock)│ │  (Cartesia)  │       │
│           │  WebSocket   │ │ Streaming│ │  WebSocket   │       │
│           └──────────────┘ └──────────┘ └──────────────┘       │
│                                   │                              │
│                    ┌──────────────┼──────────────┐              │
│                    ▼              ▼              ▼              │
│           ┌──────────────┐ ┌──────────┐ ┌──────────────┐       │
│           │ Conversation │ │  Moxie   │ │  SMS Service │       │
│           │   Service    │ │   API    │ │  (handoff)   │       │
│           │ (shared SMS) │ └──────────┘ └──────────────┘       │
│           └──────────────┘                                      │
│                    │                                             │
│               ┌────┴────┐                                       │
│               ▼         ▼                                       │
│         PostgreSQL    Redis                                     │
│        (voice_calls) (session                                   │
│                       cache)                                    │
└─────────────────────────────────────────────────────────────────┘
```

### 3.2 Audio Pipeline (Per Active Call)

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
              │ Claude Haiku│───┘              │
              │ (streaming) │                  │
              └─────────────┘                  │
                                               ▼
                                    PCM → μ-law encode
                                               │
                                    Telnyx WS ◄─┘
                                               │
                                          Patient hears
```

### 3.3 Latency Budget

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

## 4. Call Lifecycle

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
   b. Deepgram transcript → Claude Haiku (streaming)
   c. Claude response chunks → Cartesia TTS WS → encode → Telnyx WS → Patient
   d. On barge-in (VAD detects speech during TTS): flush TTS buffer, resume STT
8. Conversation Service manages qualification state (shared with SMS)
9. On booking: call Moxie API, send SMS confirmation, wrap up call
10. On completion/failure: store voice_calls record, upload recording to S3
```

## 5. Feature Toggle Design

### 5.1 Per-Clinic Configuration

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

### 5.2 Toggle Behavior Matrix

| `voice_ai_enabled` | Business Hours | Behavior |
|--------------------|---------------|----------|
| `false` | Any | Current flow: voicemail → missed call → SMS text-back |
| `true` | During hours | Voice AI answers → qualification → booking |
| `true` | After hours (voice_during_business_hours_only=false) | Voice AI answers with after-hours greeting |
| `true` | After hours (voice_during_business_hours_only=true) | Current flow: voicemail → SMS text-back |

### 5.3 Rollout Stages

Environment variable: `VOICE_AI_ROLLOUT=internal|alpha|beta|ga`

1. **internal** — AI Wolf team test clinics only
2. **alpha** — First client (Forever 22 Med Spa)
3. **beta** — 3-5 high-volume SMS clinics
4. **ga** — Available to all via admin toggle

## 6. Fallback & Resilience

| Failure | Detection | Fallback |
|---------|-----------|----------|
| Voice AI service down | Health check fails | Voicemail → SMS text-back |
| Mid-call crash | Goroutine panic recovery | "Let me text you instead" → SMS handoff |
| Poor audio quality | STT confidence <0.3 for 3 turns | "I'm having trouble hearing. I'll text you." → SMS handoff |
| Deepgram down | Circuit breaker (3 failures/30s) | Switch to Amazon Transcribe Streaming |
| Cartesia down | Circuit breaker | Switch to ElevenLabs → AWS Polly |
| All STT/TTS down | All circuit breakers open | Voicemail → SMS text-back |
| Call exceeds 10 min | Timer | "Let me text you to wrap up." → SMS handoff |

**Circuit breaker config:** 3 consecutive failures → open for 30 seconds → half-open (try 1 request) → close on success.

## 7. Data Model

### 7.1 voice_calls Table

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

### 7.2 Recording Storage

- S3 bucket: `medspa-voice-recordings/{org_id}/{call_id}.wav`
- Retention: 90 days (configurable per clinic)
- Encryption: AES-256 SSE-S3

## 8. Go Package Structure

```
internal/
  voice/
    voice.go              # Package entry, Service struct, interfaces
    session.go            # Per-call session goroutine, state machine
    router.go             # Call routing (toggle check, business hours)
    pipeline.go           # Audio pipeline orchestration
    stt/
      stt.go              # STT interface
      deepgram.go         # Deepgram Nova-3 WebSocket client
      transcribe.go       # Amazon Transcribe fallback
    tts/
      tts.go              # TTS interface
      cartesia.go         # Cartesia Sonic WebSocket client
      elevenlabs.go       # ElevenLabs fallback
      polly.go            # AWS Polly emergency fallback
    audio/
      codec.go            # μ-law ↔ PCM conversion
      mixer.go            # Audio mixing, barge-in flush
      vad.go              # Supplemental VAD / backchannel detection
    telnyx/
      media.go            # Telnyx WebSocket media stream handler
      commands.go         # Call control (answer, hangup, transfer)
    store/
      store.go            # voice_calls CRUD
    metrics.go            # Prometheus metrics (latency, call outcomes)
    circuit_breaker.go    # Circuit breaker for external services
```

## 9. Key Interfaces

```go
// STT provider interface
type STTProvider interface {
    // StreamAudio opens a streaming STT session.
    // Caller writes PCM audio to the returned writer.
    // Transcripts arrive on the returned channel.
    StreamAudio(ctx context.Context, opts STTOptions) (io.WriteCloser, <-chan Transcript, error)
}

type Transcript struct {
    Text      string
    IsFinal   bool
    Confidence float64
    Words     []Word
}

// TTS provider interface
type TTSProvider interface {
    // Synthesize streams TTS audio for the given text.
    // Audio chunks arrive on the returned channel.
    Synthesize(ctx context.Context, text string, opts TTSOptions) (<-chan []byte, error)
}

// VoiceSession manages a single active call
type VoiceSession interface {
    Start(ctx context.Context, call TelnyxCall, clinic ClinicConfig) error
    HandleBargeIn()
    Terminate(reason string)
}
```

## 10. Cost Estimate (Per Call)

Assuming average 2.5 minute call, 50% patient speech / 50% AI speech:

| Component | Usage | Unit Cost | Cost/Call |
|-----------|-------|-----------|-----------|
| Telnyx inbound | 2.5 min | $0.01/min | $0.025 |
| Deepgram STT | 1.25 min | $0.0077/min | $0.010 |
| Cartesia TTS | 1.25 min | $0.030/min | $0.038 |
| Claude Haiku (Bedrock) | ~5 turns | ~$0.001/turn | $0.005 |
| **Total** | | | **~$0.08/call** |

At 500 calls/month per clinic: ~$40/month marginal cost.

## 11. Implementation Plan (PR Sequence)

| PR | Title | Content |
|----|-------|---------|
| 1 | Technical design doc | This document |
| 2 | Voice AI config/toggle infrastructure | DB migration, config structs, feature toggle service |
| 3 | WebSocket media stream handler | Telnyx media stream connect/disconnect, audio codec |
| 4 | STT integration | Deepgram client, STT interface, Amazon Transcribe fallback |
| 5 | TTS integration | Cartesia client, TTS interface, ElevenLabs/Polly fallbacks |
| 6 | Voice conversation orchestrator | Session manager, audio pipeline, barge-in, ties STT→LLM→TTS |
| 7 | Voice call routing + webhook | Telnyx voice webhook, call router, toggle checks |
| 8 | Recording & storage | S3 upload, voice_calls CRUD |
| 9 | Metrics & monitoring | Prometheus metrics, latency histograms, call outcome counters |
| 10 | Integration tests | End-to-end test with mocked providers |

## 12. Open Questions

1. **Telnyx WebSocket vs Fork:** Telnyx supports both WebSocket media streaming and `fork` (sending media to an external endpoint). WebSocket gives more control; fork is simpler. **Recommendation: WebSocket** for barge-in control.
2. **ECS task sizing:** Voice calls are WebSocket-heavy, not CPU-heavy. Separate ECS service for voice, or same task? **Recommendation: Same task initially**, separate when scale requires it.
3. **Concurrent call limits:** Default 5 per clinic. Need load testing to determine per-task limits. Estimate ~50 concurrent calls per Fargate task (1 vCPU, 2GB).
4. **Recording consent:** Two-party consent states require explicit disclosure. Should the AI always say the consent message, or only in required states? **Recommendation: Always** — simplest compliance.
