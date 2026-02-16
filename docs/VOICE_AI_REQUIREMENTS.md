# Voice AI Requirements — MedSpa Concierge Platform

> **Version:** 1.0 · **Date:** 2026-02-16 · **Author:** Voice AI Architect (AI Wolf Solutions)
> **Status:** Requirements Complete — Ready for Engineering
> **Cross-reference:** SPEC.md §8 (Phase II Voice AI Agent)

---

## Table of Contents

1. [User Experience Requirements](#1-user-experience-requirements)
2. [Technical Architecture](#2-technical-architecture)
3. [Feature Toggle Design](#3-feature-toggle-design)
4. [Booking Flow via Voice](#4-booking-flow-via-voice)
5. [Compliance Requirements](#5-compliance-requirements)
6. [Competitive Analysis](#6-competitive-analysis)
7. [Cost Analysis](#7-cost-analysis)
8. [Implementation Phases](#8-implementation-phases)
9. [Testing Requirements](#9-testing-requirements)
10. [Infrastructure](#10-infrastructure)

---

## 1. User Experience Requirements

### 1.1 Call Pickup Behavior

| Parameter | Requirement |
|-----------|-------------|
| Ring count before answer | 0 — instant answer (no artificial ring delay) |
| Maximum pickup latency | <1 second from Telnyx `call.initiated` webhook |
| Pre-greeting pause | 200ms silence (prevents clipping the first syllable) |
| Greeting delivery | Streaming TTS, first audio byte within 400ms of pickup |

**Greeting template (per-clinic configurable):**
```
"Hi! Thanks for calling [Clinic Name]. This is [AI Name], your virtual assistant.
How can I help you today?"
```

**After-hours variant:**
```
"Hi! Thanks for calling [Clinic Name]. We're currently closed, but I can help you
schedule an appointment. What service are you interested in?"
```

### 1.2 Voice Persona

| Attribute | Specification |
|-----------|---------------|
| Tone | Warm, friendly, professional — like a confident front desk coordinator |
| Pace | Natural conversational speed (~150 wpm). Slow down for appointment details. |
| Filler words | Use sparingly: "Sure!", "Great!", "Of course." — never "um" or "uh" |
| Voice gender | Female by default (configurable per clinic via ElevenLabs voice ID) |
| Accent | Standard American English. Neutral, no strong regional accent. |
| Personality | Helpful and efficient. Not overly chatty. Gets to the point while being warm. |

**Anti-patterns (MUST NOT):**
- Sound robotic or monotone
- Use overly formal language ("I would be delighted to assist you with your inquiry")
- Speak in paragraphs — keep turns to 1-2 sentences max
- Use technical jargon ("I'll query our availability system")

### 1.3 Conversation Flow

Mirror the SMS 5-qualification flow but adapted for voice:

```
1. GREETING → "Hi! Thanks for calling [Clinic]. How can I help?"
2. SERVICE  → Patient states intent → AI confirms service
3. NAME     → "Great! And what's your name?"
4. TYPE     → "Have you visited us before, or would this be your first time?"
5. SCHEDULE → "What days and times work best for you?"
   → Check availability (~1-2s with filler)
   → Present 3 options verbally
6. CONFIRM  → "I have [Day] at [Time]. Does that work?"
7. DEPOSIT  → "To hold your spot, there's a $[amount] refundable deposit.
               I'll text you a secure payment link right now."
8. WRAP-UP  → "You're all set! Check your phone for the confirmation. Have a great day!"
```

**Collection order adapts to conversation:** If the patient volunteers info out of order (e.g., "Hi, I'm Sarah and I want Botox"), the AI skips already-collected qualifications. Same logic as SMS flow.

### 1.4 Interruption Handling (Barge-In)

| Scenario | Behavior |
|----------|----------|
| Patient speaks while AI is talking | Immediately stop TTS playback. Process patient's speech. |
| Patient says "wait" or "hold on" | Pause, say "Take your time." Wait up to 30 seconds. |
| Patient interrupts with correction | "Actually, not Monday — Tuesday." → AI acknowledges and corrects. |
| Backchannel sounds ("uh-huh", "mm") | Do NOT treat as interruption. Continue speaking. |

**Implementation:** Deepgram VAD detects speech onset. When speech energy exceeds threshold during TTS playback:
1. Flush TTS audio buffer immediately (stop speaking within 100ms)
2. Continue STT processing of patient's speech
3. When patient finishes, respond from new context

**Backchannel detection:** Short utterances (<500ms) with low energy that match common backchannels ("uh-huh", "mm", "yeah") → suppress barge-in. This requires a lightweight classifier on the STT interim results.

### 1.5 Language Handling

| Scenario | Behavior |
|----------|----------|
| Patient speaks English | Normal flow |
| Patient speaks Spanish | Phase 2d: auto-detect via Deepgram, switch to Spanish STT/TTS/prompt |
| Patient speaks other language | "I'm sorry, I can only help in English right now. Let me connect you with someone who can help." → Transfer to clinic or SMS handoff |
| Patient has heavy accent | Deepgram Nova-3 handles well. If confidence <0.6 on key fields, ask to repeat: "I want to make sure I got that right — could you spell your last name for me?" |

### 1.6 Audio Edge Cases

| Scenario | Behavior |
|----------|----------|
| Background noise (driving, kids) | Deepgram noise cancellation handles most cases. If STT confidence drops, ask to repeat. |
| Mumbling / unclear speech | "I didn't quite catch that — could you say that again?" (max 2 retries, then SMS handoff) |
| Extended silence (>10s) | "Are you still there?" → wait 10s more → "It seems like we got disconnected. I'll send you a text so we can continue." → SMS handoff |
| Very long patient monologue (>30s) | Let them finish. Extract all qualifications mentioned. Don't interrupt unless they pause. |
| Hold / phone on speaker | Handle gracefully. Deepgram performs well with speakerphone audio. |

### 1.7 Human Transfer

| Trigger | Behavior |
|---------|----------|
| Patient explicitly requests human | "Of course! Let me transfer you now." → SIP transfer to clinic main line |
| Patient is upset / frustrated | After 2 failed comprehension attempts: "Let me get someone who can help you directly." → Transfer |
| Complex medical question | "That's a great question for our providers. Let me connect you." → Transfer |
| Emergency (vision loss, breathing) | "That sounds urgent — please call 911 or go to your nearest emergency room right away." → End call |
| Transfer unavailable (after hours) | "Our team isn't available right now, but I'll have someone call you back first thing tomorrow. Can I also help you schedule an appointment?" |

**Transfer mechanism:** Telnyx `transfer` command with SIP URI of clinic's main phone line. Configurable per clinic in admin settings.

### 1.8 Availability Lookup UX

When the AI needs to query Moxie API (~1-2 seconds):

```
AI: "Let me check what's available for you... [1-2 second pause with soft hold tone or silence]
     I found some great options! I have openings on Monday February 24th at 2pm,
     Wednesday the 26th at 10am, and Thursday the 27th at 4:30pm.
     Which of those works best for you?"
```

**Filler strategy:**
- Say "Let me check..." with natural trailing off
- Play 1-2 seconds of soft ambient tone (NOT hold music) OR comfortable silence
- If lookup takes >3 seconds: "Still looking... just a moment"
- If lookup takes >5 seconds: "I'm having trouble checking availability. Let me text you some options instead." → SMS handoff

### 1.9 Call Duration Targets

| Call Type | Target Duration | Max Duration |
|-----------|----------------|--------------|
| Full booking (all 5 quals + slot selection) | 2-3 minutes | 5 minutes |
| Information inquiry (pricing, services) | 1-2 minutes | 3 minutes |
| Transfer to human | <30 seconds | 1 minute |
| Failed comprehension → SMS handoff | <1 minute | 2 minutes |

**Hard cutoff:** 10 minutes. After 10 minutes: "I want to make sure I'm helping you efficiently. Let me text you so we can wrap this up." → SMS handoff.

---

## 2. Technical Architecture

### 2.1 Provider Recommendations

#### Speech-to-Text: **Deepgram Nova-3** (RECOMMENDED)

| Criteria | Deepgram Nova-3 | AssemblyAI | AWS Transcribe | Whisper (OpenAI) |
|----------|----------------|------------|----------------|------------------|
| Streaming latency | ~200ms | ~300ms | ~500ms | N/A (batch only) |
| Built-in VAD | ✅ Yes | ✅ Yes | ❌ No | N/A |
| Accuracy (WER) | ~8% | ~10% | ~12% | ~10% |
| Streaming WebSocket | ✅ Native | ✅ Yes | ✅ Yes | ❌ No |
| Price/min (streaming) | $0.0077 | $0.0065 | $0.024 | $0.006 (batch) |
| HIPAA BAA | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes |
| Interim results | ✅ Yes | ✅ Yes | ❌ Limited | N/A |
| Custom vocabulary | ✅ Keywords | ✅ Custom | ✅ Custom | ❌ No |

**Why Deepgram:** Lowest streaming latency with built-in VAD is critical for our 500ms budget. Interim results let us start LLM processing before the patient finishes speaking. Medical term accuracy is strong. Price is competitive at $0.0077/min. HIPAA BAA available.

**Custom vocabulary boost:** Add med spa terms: "Botox", "Juvederm", "Kybella", "microneedling", "IPL", "Sculptra", "Restylane", "Dysport", "Xeomin", "PRF", "PRP", "hydrafacial".

#### Text-to-Speech: **Cartesia Sonic** (RECOMMENDED) with ElevenLabs fallback

| Criteria | Cartesia Sonic | ElevenLabs Turbo v2.5 | PlayHT | AWS Polly |
|----------|---------------|----------------------|--------|-----------|
| Streaming latency (first byte) | ~90ms | ~150ms | ~200ms | ~100ms |
| Voice quality | Excellent | Best-in-class | Very good | Robotic |
| Custom voice cloning | ✅ Yes | ✅ Yes | ✅ Yes | ❌ No |
| Price/min | ~$0.030 | ~$0.040 | ~$0.035 | $0.016 |
| Streaming WebSocket | ✅ Yes | ✅ Yes | ✅ Yes | ❌ (HTTP) |
| HIPAA BAA | ✅ Yes | ✅ Enterprise | ❌ No | ✅ Yes |
| Emotion/style control | ✅ Yes | ✅ Yes | Limited | ❌ No |

**Why Cartesia as primary:** 90ms first-byte latency is best-in-class and critical for our 500ms budget. Voice quality is excellent and near-indistinguishable from ElevenLabs. At $0.03/min it's 25% cheaper than ElevenLabs. Native streaming WebSocket. HIPAA BAA available. Emotion control lets us sound warm and empathetic.

**Why ElevenLabs as fallback:** Best voice quality overall. Wider voice library. If Cartesia has quality issues or outages, ElevenLabs Turbo v2.5 is the drop-in replacement. Custom voice per clinic is a premium upsell feature.

**AWS Polly as emergency fallback:** If both Cartesia and ElevenLabs fail, Polly keeps the call alive. It sounds robotic but it's available.

#### LLM: **Claude 3.5 Haiku via AWS Bedrock** (SAME AS SMS)

- Same model, same system prompt structure, same qualification logic
- Streaming responses via Bedrock invoke-with-response-stream
- Voice-specific system prompt additions (see §2.5)
- First token latency: ~200ms with pre-warmed connection

#### Telephony: **Telnyx Voice API** (ALREADY INTEGRATED)

- Already using Telnyx for SMS — single vendor for voice + SMS
- WebSocket media streams for bidirectional audio
- Per-minute pricing: ~$0.01/min inbound
- SIP transfer support for human handoff
- Call recording built-in
- Call control commands: answer, hangup, transfer, play audio

### 2.2 System Architecture

```
                    ┌──────────────────────────────────────────────┐
                    │              VOICE AI SERVICE                │
                    │           (ECS Fargate - Go)                 │
                    │                                              │
  Telnyx           │  ┌─────────┐   ┌──────────┐   ┌──────────┐ │
  Voice ──WebSocket──►│  Call    │──►│ Audio    │──►│ Session  │ │
  Media            │  │  Router  │   │ Pipeline │   │ Manager  │ │
  Stream           │  └─────────┘   └──────────┘   └──────────┘ │
                    │       │              │              │        │
                    │       ▼              ▼              ▼        │
                    │  ┌─────────┐   ┌──────────┐   ┌──────────┐ │
                    │  │Deepgram │   │ Claude   │   │ Cartesia │ │
                    │  │  STT    │◄─►│ Haiku    │◄─►│  TTS     │ │
                    │  │(WebSock)│   │(Bedrock) │   │(WebSock) │ │
                    │  └─────────┘   └──────────┘   └──────────┘ │
                    │                      │                       │
                    │                      ▼                       │
                    │              ┌──────────────┐                │
                    │              │ Conversation  │                │
                    │              │   Service     │◄──── Same logic│
                    │              │ (shared w/SMS)│      as SMS    │
                    │              └──────────────┘                │
                    │                      │                       │
                    │          ┌───────────┼───────────┐          │
                    │          ▼           ▼           ▼          │
                    │     ┌────────┐  ┌────────┐  ┌────────┐    │
                    │     │ Moxie  │  │ Stripe │  │  SMS   │    │
                    │     │  API   │  │Connect │  │Service │    │
                    │     └────────┘  └────────┘  └────────┘    │
                    └──────────────────────────────────────────────┘
```

### 2.3 Audio Pipeline (Per-Call)

Each active call maintains 3 persistent WebSocket connections:

1. **Telnyx ↔ Service:** Bidirectional audio (μ-law 8kHz from Telnyx, converted to PCM 16kHz for STT)
2. **Service → Deepgram:** Outbound audio stream for transcription
3. **Service ← Cartesia:** Inbound TTS audio, converted back to μ-law for Telnyx

**Audio flow (patient speaks):**
```
Patient audio → Telnyx WS → μ-law decode → PCM 16kHz → Deepgram STT WS
                                                              │
                                              interim/final transcript
                                                              │
                                                              ▼
                                                     LLM (Claude Haiku)
                                                              │
                                                        response text
                                                              │
                                                              ▼
                                                   Cartesia TTS WS → PCM audio
                                                              │
                                                     PCM → μ-law encode
                                                              │
                                                              ▼
                                                     Telnyx WS → Patient
```

### 2.4 Latency Budget

Target: **<500ms** from patient stops speaking → patient hears first word of response.

| Stage | Budget | Technique |
|-------|--------|-----------|
| VAD endpointing | 100ms | Deepgram `endpointing=100` parameter. Aggressive but acceptable for short answers. Use 300ms for longer utterances. |
| STT final result | 0ms | Use interim results, don't wait for `is_final`. Correct if final differs. |
| LLM first token | 200ms | Claude Haiku streaming. Pre-warmed Bedrock connection. Minimal system prompt. |
| TTS first audio byte | 100ms | Cartesia streaming. Send first sentence fragment as soon as LLM emits it. |
| Network + encoding | 50ms | Co-locate in us-east-1. Persistent WebSockets. Pre-allocated buffers. |
| **Total** | **~450ms** | |
| Natural pacing pad | +100-300ms | Simple yes/no: +100ms. Complex: +0ms (TTS duration handles it). Empathetic: +300ms. |
| **Perceived** | **550-750ms** | Feels natural and attentive |

**Key optimizations from SPEC.md §8 (validated and expanded):**

1. **LLM→TTS pipelining:** Stream LLM tokens to TTS at sentence boundaries. Patient hears first sentence while LLM generates second.
2. **Interim STT processing:** Start LLM inference on interim transcript. If final differs, cancel and re-process (rare — Deepgram interims are >95% accurate).
3. **Pre-warmed connections:** Persistent WebSockets to all 3 services. No per-utterance handshake.
4. **Speculative pre-generation:** After collecting name, pre-generate TTS for "And what service are you interested in?" before the patient even finishes their response.
5. **Pattern matching shortcuts:** For single-word answers ("yes", "Botox", "3", "Tuesday"), skip LLM entirely and use pre-computed responses.
6. **Sentence chunking:** Split LLM output at sentence/clause boundaries. Send each chunk to TTS independently.

### 2.5 Voice-Specific LLM Adaptations

Add to the existing SMS system prompt when in voice mode:

```
VOICE MODE INSTRUCTIONS:
- Keep responses to 1-2 short sentences per turn. Patients can't re-read voice.
- Use natural spoken language, not written language.
- Never say URLs, email addresses, or spell things out letter-by-letter unless asked.
- Say "I'll text you a link" instead of reading URLs.
- Confirm key info back: "So that's Botox on Monday at 4:30, right?"
- Use conversational fillers sparingly: "Sure!", "Great!", "Of course."
- When presenting time slots, use natural speech: "Monday the 24th at 2pm,
  Wednesday the 26th at 10am, or Thursday the 27th at 4:30."
- For numbers, say "four thirty" not "sixteen thirty" or "4:30 PM".
- Do not use markdown, bullet points, or any visual formatting.
- Do not say "asterisk" or "bullet point" or "number one".
```

### 2.6 Voice Activity Detection (VAD)

**Deepgram built-in VAD parameters:**
```json
{
  "vad_events": true,
  "endpointing": 100,
  "utterance_end_ms": 1000,
  "interim_results": true
}
```

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `endpointing` | 100ms | Aggressive. Triggers end-of-speech quickly for short replies. |
| `utterance_end_ms` | 1000ms | Longer pause detection for multi-sentence patient speech. |
| `vad_events` | true | Get explicit speech_started/speech_ended events for barge-in detection. |

**Adaptive endpointing:** Start at 100ms. If patient is mid-thought (detected by incomplete sentence in interim), extend to 300ms dynamically.

### 2.7 Recording & Transcription Storage

| Data | Storage | Retention | Encryption |
|------|---------|-----------|------------|
| Call recording (audio) | S3 (`s3://medspa-voice-recordings/{org_id}/{call_id}.wav`) | 90 days (configurable per clinic) | AES-256 SSE-S3 |
| Full transcript | PostgreSQL `voice_calls.transcript` (JSONB) | Same as conversation data | RDS encryption at rest |
| Call metadata | PostgreSQL `voice_calls` table | Indefinite | RDS encryption at rest |
| STT interim results | Not stored (ephemeral) | N/A | N/A |

**`voice_calls` table schema:**
```sql
CREATE TABLE voice_calls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    lead_id         UUID REFERENCES leads(id),
    telnyx_call_id  TEXT NOT NULL UNIQUE,
    caller_phone    TEXT NOT NULL,
    direction       TEXT NOT NULL DEFAULT 'inbound', -- inbound | outbound
    status          TEXT NOT NULL DEFAULT 'in_progress', -- in_progress | completed | failed | transferred
    outcome         TEXT, -- booked | qualified | transferred | abandoned | sms_handoff
    duration_sec    INTEGER,
    recording_url   TEXT,
    transcript      JSONB, -- [{role: "patient"|"agent", text: "...", timestamp: "..."}]
    qualifications  JSONB, -- {name: "...", service: "...", ...} — same as SMS
    cost_cents      INTEGER, -- total cost of the call in cents
    started_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_voice_calls_org ON voice_calls(org_id);
CREATE INDEX idx_voice_calls_phone ON voice_calls(caller_phone);
CREATE INDEX idx_voice_calls_started ON voice_calls(started_at);
```

---

## 3. Feature Toggle Design

### 3.1 Clinic Configuration

Add to existing clinic config (Redis + PostgreSQL):

```json
{
  "voice_ai_enabled": false,
  "voice_ai_config": {
    "greeting": "Hi! Thanks for calling {clinic_name}. How can I help you today?",
    "after_hours_greeting": "Hi! Thanks for calling {clinic_name}. We're currently closed, but I can help you schedule an appointment.",
    "voice_id": "cartesia_default_warm_female",
    "transfer_number": "+15551234567",
    "business_hours": {
      "timezone": "America/New_York",
      "monday": { "open": "09:00", "close": "18:00" },
      "tuesday": { "open": "09:00", "close": "18:00" },
      "wednesday": { "open": "09:00", "close": "18:00" },
      "thursday": { "open": "09:00", "close": "18:00" },
      "friday": { "open": "09:00", "close": "17:00" },
      "saturday": null,
      "sunday": null
    },
    "voice_during_business_hours_only": false,
    "max_concurrent_calls": 5,
    "recording_enabled": true,
    "recording_consent_message": "This call may be recorded for quality and training purposes."
  }
}
```

### 3.2 Toggle Behavior Matrix

| `voice_ai_enabled` | Business Hours | Call Behavior |
|--------------------|---------------|---------------|
| `false` | Any | Call → voicemail → missed call → SMS text-back (current behavior) |
| `true` | During hours | Call → Voice AI answers → qualification → booking |
| `true` | After hours + `voice_during_business_hours_only=false` | Call → Voice AI answers (after-hours greeting) |
| `true` | After hours + `voice_during_business_hours_only=true` | Call → voicemail → SMS text-back |

### 3.3 Fallback Behavior

| Failure | Fallback | Detection |
|---------|----------|-----------|
| Voice AI service unavailable | Route to voicemail → SMS text-back | Health check fails, Telnyx webhook gets 5xx |
| Voice AI crashes mid-call | "I'm sorry, let me text you instead." → SMS handoff | Goroutine panic recovery |
| Call quality too poor | "I'm having trouble hearing you. I'll text you instead." → SMS handoff | STT confidence <0.3 for 3 consecutive utterances |
| All STT/TTS providers fail | Voicemail → SMS text-back | Circuit breaker opens |

**Telnyx webhook routing logic:**
```go
func handleInboundCall(call TelnyxCall) {
    clinic := getClinic(call.To)

    if !clinic.VoiceAIEnabled {
        // Current behavior: let it ring → voicemail → SMS
        return
    }

    if clinic.VoiceDuringBusinessHoursOnly && !isBusinessHours(clinic) {
        return // voicemail → SMS
    }

    if !voiceAIService.IsHealthy() {
        log.Warn("Voice AI unhealthy, falling back to SMS")
        return // voicemail → SMS
    }

    // Answer and stream to Voice AI
    call.Answer()
    voiceAIService.HandleCall(call, clinic)
}
```

### 3.4 Admin Portal Toggle

Add to Settings page in `web/onboarding/`:
- **Voice AI** section with on/off toggle
- Business hours schedule editor
- Voice selection (dropdown of available voices)
- Transfer phone number input
- Recording consent toggle
- Test call button (calls admin's phone with Voice AI)

### 3.5 Gradual Rollout

1. **Internal testing:** Enable for AI Wolf team test clinics only
2. **Alpha:** Enable for Forever 22 Med Spa (first client)
3. **Beta:** Enable for 3-5 selected clinics with high SMS volume
4. **GA:** Available to all clinics via admin toggle

Feature flag in environment: `VOICE_AI_ROLLOUT=internal|alpha|beta|ga`

---

## 4. Booking Flow via Voice

### 4.1 Complete Call Script (Happy Path)

```
[RING] → Voice AI answers in <1 second

AI:  "Hi! Thanks for calling Forever 22 Med Spa. This is Mia, your virtual
      assistant. How can I help you today?"

PT:  "Hi, I'd like to get Botox."

AI:  "Great choice! And what's your name?"

PT:  "Sarah Johnson."

AI:  "Nice to meet you, Sarah! Have you visited Forever 22 before, or would
      this be your first time?"

PT:  "First time!"

AI:  "Welcome! What days and times work best for you?"

PT:  "Mondays or Tuesdays, preferably after 3."

AI:  "Let me check what's available for you..."
     [1-2 second pause — Moxie API query]
     "I found some great options! I have Monday February 24th at 3:30,
      Tuesday the 25th at 4pm, or Monday March 3rd at 3pm.
      Which works best?"

PT:  "Tuesday at 4 sounds perfect."

AI:  "Tuesday February 25th at 4pm — perfect! To hold your spot, there's a
      $50 refundable deposit. I'm going to text you a secure payment link
      right now. You can complete it while we're on the phone or anytime
      before your appointment."

     [System sends SMS with Stripe Checkout link]

AI:  "I just sent that to your phone. One last thing — what's the best email
      for your appointment confirmation?"

PT:  "sarah@email.com"

AI:  "Got it! You're all set, Sarah. Once you complete the deposit, you'll
      get a confirmation text with all the details. We can't wait to see
      you! Have a wonderful day."

PT:  "Thanks, bye!"

AI:  "Bye!"
     [Call ends]
```

**Total duration: ~2 minutes.**

### 4.2 Availability Lookup

- Same Moxie GraphQL `AvailableTimeSlots` query as SMS flow
- Same customer preference filtering (day of week, time range)
- Voice presentation: max 3 slots verbally (more is confusing to hear)
- If patient wants more: "I can also check other days. Any particular day you're thinking of?"

### 4.3 Time Slot Selection (Voice-Specific)

| Patient Says | Detection |
|-------------|-----------|
| "The first one" / "option one" | Index match |
| "Tuesday at 4" | Time + day match against presented slots |
| "The 4pm" | Time match |
| "Tuesday" | Day match (if only one Tuesday slot presented) |
| "That second one sounds good" | Index match with natural language |
| Ambiguous | AI asks: "Just to confirm — was that the Tuesday at 4 or the Monday at 3:30?" |

### 4.4 Deposit Collection via SMS During Call

**PCI compliance requires we NEVER collect card details over voice.**

Flow:
1. AI announces deposit and says "I'll text you a link"
2. System sends SMS with Stripe Checkout short URL (same as SMS flow)
3. AI continues conversation (collect email if not yet gathered)
4. Patient can pay during or after the call
5. Stripe webhook triggers Moxie booking (same as SMS flow)

### 4.5 Edge Cases

| Scenario | Handling |
|----------|----------|
| Patient wants multiple services | Book primary service. "I can also help you book [second service] — want me to check availability for that too?" |
| Patient asks about pricing | Answer from knowledge base. "Botox starts at $12 per unit. Would you like to schedule a consultation?" |
| Patient wants to book for someone else | "Sure! What's the patient's name?" — proceed with their info |
| Patient already has an appointment | "I see! If you need to reschedule, I'd recommend calling during business hours so our team can help with that directly." |
| Slot becomes unavailable between presentation and selection | "I'm sorry, that slot just got taken. Let me find you another option." → re-query |

---

## 5. Compliance Requirements

### 5.1 Call Recording Consent

**Strategy: Always disclose recording at the start of the call.** This satisfies both one-party and two-party (all-party) consent states.

Recording disclosure is part of the greeting:
```
"This call may be recorded for quality and training purposes."
```

**Two-party consent states (as of 2026):** California, Connecticut, Delaware, Florida, Illinois, Maryland, Massachusetts, Michigan, Montana, Nevada, New Hampshire, Oregon, Pennsylvania, Vermont, Washington. Plus: Hawaii (private places).

**Implementation:** The recording consent message plays before the AI greeting. It's configurable per clinic but defaults to always-on. Clinics in one-party states may optionally disable the disclosure (not recommended).

**If patient objects to recording:** "No problem at all. I've turned off recording for this call." → Set `recording_enabled=false` for this call session. Continue without recording.

### 5.2 HIPAA

| Requirement | Implementation |
|-------------|----------------|
| Voice recordings are PHI | Encrypted at rest (S3 SSE-AES256), in transit (TLS 1.2+) |
| Access control | Only org-scoped admin access. No cross-org leakage. |
| BAA with providers | Deepgram ✅, Cartesia ✅, ElevenLabs (Enterprise) ✅, Telnyx ✅, AWS ✅ |
| Audit logging | All recording access logged with user ID, timestamp, reason |
| Minimum necessary | Transcripts redact SSN, DOB, full address when stored |
| Data retention | Configurable per clinic (default 90 days). Auto-delete recordings after retention. |
| Breach notification | Standard HIPAA breach protocol if recordings compromised |

### 5.3 PCI DSS

**ABSOLUTE RULE: Never collect, transmit, or store card details over voice.**

- Payment always via Stripe Checkout link sent by SMS
- Voice agent says "I'll text you a secure link" — never asks for card number
- If patient tries to read their card number: "For your security, I can't take card details over the phone. I've texted you a secure link instead."
- Stripe handles all PCI compliance on their hosted checkout page

### 5.4 Medical Liability

Same guardrails as SMS (SPEC.md §4):
- No diagnosis, no treatment recommendations for conditions
- No dosage advice for specific individuals
- Emergency protocol: vision loss, breathing difficulty, vascular compromise → 911 immediately
- Contraindication questions → defer to provider consultation
- Voice adds: spoken emergencies may be more urgent-sounding. AI must NOT minimize symptoms.

### 5.5 TCPA (Voice-Specific)

- Inbound calls are exempt from most TCPA restrictions (patient initiated)
- **Outbound calls (Phase 2c):** Require prior express consent. Appointment reminders require prior express consent (not written). Marketing calls require prior express written consent.
- Do Not Call list compliance for outbound
- Time restrictions: no outbound calls before 8am or after 9pm local time

---

## 6. Competitive Analysis

### 6.1 Competitive Landscape (Feb 2026)

| Competitor | Focus | Voice AI? | Booking? | Payment? | Med Spa Specific? |
|------------|-------|-----------|----------|----------|--------------------|
| **Sully.ai** | All-in-one AI medical workforce | ✅ Yes (Speechmatics STT) | ✅ EHR-integrated | ❌ No deposit collection | ❌ General healthcare |
| **Vocca.ai** | Voice AI for healthcare | ✅ Yes | ✅ Limited | ❌ No | ❌ General healthcare |
| **Podium** | Patient communication platform | ✅ Basic voice | ✅ Yes | ✅ Basic | ❌ General SMB |
| **Weave** | Patient communication | ✅ AI phone | ✅ Yes | ✅ Basic | ❌ Dental/optometry focus |
| **Smith.ai** | Hybrid AI + human receptionist | ✅ Hybrid | ✅ Basic | ❌ No | ❌ General SMB |
| **BookingBee.ai** | AI receptionist for salons | ✅ Yes | ✅ Yes | ❌ Unknown | ⚠️ Salon/beauty |
| **My AI Front Desk** | Simple AI receptionist | ✅ Yes | ✅ Basic | ❌ No | ❌ General SMB |
| **Retell AI** | Voice AI platform (build-your-own) | ✅ Platform | ❌ Build yourself | ❌ Build yourself | ❌ Platform only |

### 6.2 Our Differentiation

| Feature | Us | Competitors |
|---------|-----|-------------|
| **Moxie integration** | ✅ Deep API: real-time availability + auto-booking | ❌ None have Moxie integration |
| **Deposit collection** | ✅ Stripe Connect during call → SMS payment link | ❌ Most don't handle payments |
| **Full qualification → booking → payment** | ✅ End-to-end in one call | ⚠️ Most stop at scheduling |
| **SMS fallback** | ✅ Seamless voice → SMS handoff | ❌ Voice-only or separate channels |
| **Med spa specialization** | ✅ Service aliases, knowledge base, pricing | ❌ Generic healthcare or generic SMB |
| **Per-clinic voice persona** | ✅ Custom TTS voice per clinic | ⚠️ Limited customization |
| **Cost per call** | ~$0.15 for 3-min call | Smith.ai: $4-6/call. Podium: $300-500/mo flat |

### 6.3 Features to Match

- **Multilingual support** (Sully, Vocca): Spanish launch in Phase 2d
- **Call analytics dashboard** (Weave, Podium): Phase 2d
- **Sentiment analysis** (Sully): Future phase
- **Appointment reminders via voice** (all competitors): Phase 2c

### 6.4 Competitive Moat

Nobody else does: **Voice AI → 5-step qualification → Moxie real-time availability → Stripe deposit → Moxie auto-booking** in a single call. This is our moat. The SMS flow already works — voice is the natural extension.

---

## 7. Cost Analysis

### 7.1 Per-Minute Cost Breakdown

| Component | Provider | Cost/min | Notes |
|-----------|----------|----------|-------|
| Telephony (inbound) | Telnyx | $0.010 | Includes WebSocket media streaming |
| Speech-to-Text | Deepgram Nova-3 | $0.008 | Streaming, with VAD |
| Text-to-Speech | Cartesia Sonic | $0.030 | Streaming, custom voice |
| LLM | Claude 3.5 Haiku (Bedrock) | $0.005 | ~500 tokens/turn × ~8 turns = ~4K tokens |
| Recording storage | S3 | $0.001 | ~0.5MB/min WAV, 90-day retention |
| **Total** | | **$0.054/min** | |

### 7.2 Cost Per Call

| Call Type | Duration | Cost |
|-----------|----------|------|
| Full booking | 3 min | **$0.16** |
| Info inquiry | 1.5 min | $0.08 |
| Quick transfer | 0.5 min | $0.03 |
| Failed → SMS handoff | 1 min | $0.05 |

**Target: <$0.50 per 3-minute call → ACHIEVED at $0.16** ✅

### 7.3 Provider Pricing Comparison

#### STT Providers
| Provider | Streaming $/min | Batch $/min | Notes |
|----------|----------------|-------------|-------|
| Deepgram Nova-3 | $0.0077 | $0.0044 | Best latency + VAD |
| AssemblyAI | $0.0065 | $0.0037 | Slightly cheaper, higher latency |
| AWS Transcribe | $0.024 | $0.024 | 3x more expensive |
| OpenAI Whisper | N/A | $0.006 | No streaming — unusable for real-time |

#### TTS Providers
| Provider | $/min (est.) | First byte latency | Quality |
|----------|-------------|--------------------|---------| 
| Cartesia Sonic | $0.030 | ~90ms | Excellent |
| ElevenLabs Turbo v2.5 | $0.040 | ~150ms | Best |
| PlayHT | $0.035 | ~200ms | Very good |
| AWS Polly Neural | $0.016 | ~100ms | Robotic |

### 7.4 Monthly Cost Projections

| Scale | Calls/day | Avg duration | Monthly cost |
|-------|-----------|-------------|--------------|
| Single clinic (Forever 22) | 15 | 2.5 min | ~$61 |
| 10 clinics | 150 | 2.5 min | ~$608 |
| 50 clinics | 750 | 2.5 min | ~$3,038 |
| 100 clinics | 1,500 | 2.5 min | ~$6,075 |

**Infrastructure cost adds ~$150-400/mo for ECS (see §10).**

---

## 8. Implementation Phases

### Phase 2a: Basic Voice AI + Qualification (Weeks 1-3)

**Scope:**
- Telnyx voice webhook handler — answer inbound calls
- WebSocket media stream setup (bidirectional audio)
- Deepgram STT integration (streaming)
- Cartesia TTS integration (streaming)
- Audio pipeline: μ-law ↔ PCM conversion
- Basic conversation: greeting → 5-step qualification
- SMS handoff for payment ("I'll text you a link")
- Call recording to S3
- Transcript storage in PostgreSQL
- Feature toggle (`voice_ai_enabled` per clinic)
- Fallback: voice failure → SMS text-back

**Deliverable:** Patient calls → AI answers → collects all 5 qualifications → says "I'll text you a payment link" → SMS with Stripe link sent → call ends. Same booking flow from there.

**Key files to create:**
- `cmd/voice-service/main.go` — Voice AI service entry point
- `internal/voice/handler.go` — Telnyx voice webhook + call management
- `internal/voice/pipeline.go` — Audio pipeline orchestration
- `internal/voice/stt.go` — Deepgram STT client
- `internal/voice/tts.go` — Cartesia TTS client (+ ElevenLabs + Polly fallbacks)
- `internal/voice/session.go` — Per-call session state management

### Phase 2b: Real-Time Availability + Full Booking (Week 4)

**Scope:**
- Moxie availability lookup during call (with filler audio)
- Verbal time slot presentation (max 3 options)
- Voice-based slot selection (natural language matching)
- Stripe Checkout link sent via SMS during call
- End-to-end: call → qualify → availability → select → deposit link → done

**Deliverable:** Complete booking flow via voice. Patient doesn't need to interact with SMS at all (except clicking payment link).

### Phase 2c: Polish + Production Hardening (Weeks 5-6)

**Scope:**
- Interruption/barge-in handling
- Backchannel detection (don't interrupt on "uh-huh")
- Adaptive VAD endpointing
- Human transfer (SIP transfer to clinic line)
- Call quality monitoring + alerting
- Load testing (concurrent calls)
- Admin portal: voice toggle, voice settings, call logs
- Accent/noise tolerance tuning
- Natural pacing layer

**Deliverable:** Production-ready voice AI with all edge cases handled.

### Phase 2d: Enhancements (Post-Launch)

**Scope:**
- Spanish language support (auto-detect + switch)
- Outbound calls: appointment reminders
- Call analytics dashboard (duration, outcome, conversion rate)
- Per-clinic custom voice (ElevenLabs voice cloning)
- Sentiment analysis + quality scoring
- Multi-location call routing
- Re-engagement outbound calls

**Timeline:** 2-4 weeks per feature, prioritized by customer demand.

### Timeline Summary

| Phase | Duration | Cumulative |
|-------|----------|------------|
| 2a: Basic voice + qualification | 3 weeks | Week 3 |
| 2b: Availability + booking | 1 week | Week 4 |
| 2c: Polish + hardening | 2 weeks | Week 6 |
| 2d: Enhancements | Ongoing | Post-launch |

---

## 9. Testing Requirements

### 9.1 Voice E2E Test Scenarios

| # | Scenario | Expected Outcome |
|---|----------|-----------------|
| V1 | Happy path: full booking call | All 5 quals collected, availability presented, slot selected, SMS with Stripe link sent, call ends gracefully |
| V2 | Patient provides all info in first sentence | AI skips redundant questions, goes to availability immediately |
| V3 | Patient interrupts AI mid-sentence | AI stops talking, processes interruption, responds appropriately |
| V4 | Patient is silent for 15 seconds | AI prompts "Are you still there?" → SMS handoff after 25s total |
| V5 | Patient asks for human | AI transfers to clinic phone number |
| V6 | Patient mentions emergency symptoms | AI immediately directs to 911 |
| V7 | STT returns low confidence | AI asks patient to repeat |
| V8 | Moxie API timeout during availability | AI says "Let me text you options instead" → SMS handoff |
| V9 | Patient says "STOP" or "cancel" | AI acknowledges and ends call politely |
| V10 | Patient speaks Spanish | (Phase 2d) Auto-detect and switch. Before 2d: politely redirect |
| V11 | Background noise (car, music) | AI still understands and responds correctly |
| V12 | Patient changes mind mid-booking | "Actually, not Botox — I want filler." AI adapts. |
| V13 | Multiple services in one call | AI books primary, offers to book second |
| V14 | Patient tries to read card number | AI stops them, redirects to SMS link |
| V15 | Concurrent calls to same clinic | Both calls handled independently, no cross-contamination |
| V16 | Voice AI service crashes mid-call | Call falls back to SMS text-back flow |
| V17 | Feature toggle OFF | Call goes to voicemail, SMS text-back as normal |
| V18 | After-hours call with voice enabled | After-hours greeting plays, booking still works |
| V19 | Call transfer to human | SIP transfer executes, patient hears ringing of clinic line |
| V20 | Recording consent — patient objects | Recording disabled for that call, conversation continues |

### 9.2 Load Testing

| Metric | Target |
|--------|--------|
| Concurrent calls per instance | 10 |
| Total concurrent calls (scaled) | 50 |
| P99 response latency | <800ms |
| P50 response latency | <500ms |
| Call drop rate | <1% |
| STT accuracy under load | >90% |

**Tool:** Custom load test using Telnyx test calls + recorded audio playback. Simulate 50 concurrent calls with varied scripts.

### 9.3 Accent & Dialect Testing

Test with recorded audio samples covering:
- Standard American English
- Southern American English
- New York / Northeast accent
- Hispanic-accented English
- Asian-accented English
- British English
- African American Vernacular English (AAVE)

**Acceptance:** >85% qualification extraction accuracy across all accent groups.

### 9.4 Background Noise Testing

Test with recorded audio mixed with:
- Car driving / road noise
- Children playing
- Restaurant / café ambiance
- Music playing
- TV / other conversation in background
- Speakerphone echo

**Acceptance:** >80% qualification extraction accuracy with moderate background noise.

---

## 10. Infrastructure

### 10.1 ECS Service Design

**Separate service from API.** Voice AI has fundamentally different resource and scaling characteristics.

| Parameter | Voice AI Service | API Service (existing) |
|-----------|-----------------|----------------------|
| CPU | 1 vCPU | 0.5 vCPU |
| Memory | 2 GB | 1 GB |
| Min instances | 1 (during business hours) | 1 |
| Max instances | 10 | 5 |
| Scale metric | Concurrent WebSocket connections | Request count |
| Scale threshold | 8 concurrent calls per instance | 100 req/s |
| Health check | `/health` (checks Deepgram, Cartesia, Telnyx WS connectivity) | `/ready` |

**Why separate service:**
- WebSocket connections are long-lived (2-5 min per call) vs. short HTTP requests
- Memory usage is higher (audio buffers per call)
- Scaling is based on concurrent calls, not request rate
- Deployment can be independent (update voice without touching API)
- Failure isolation: voice crash doesn't affect SMS

### 10.2 WebSocket Scaling

Each voice call maintains 3 WebSockets + 1 Bedrock stream = 4 connections per call.

| Instances | Concurrent calls | WebSocket connections |
|-----------|-----------------|---------------------|
| 1 | 10 | 40 |
| 2 | 20 | 80 |
| 5 | 50 | 200 |
| 10 | 100 | 400 |

**ALB WebSocket support:** AWS ALB natively supports WebSocket. Sticky sessions via connection ID for Telnyx media stream.

**Connection pooling:** Maintain persistent connections to Deepgram and Cartesia (shared across calls on same instance). Only Telnyx media stream is per-call.

### 10.3 Call Queue Management

| Scenario | Behavior |
|----------|----------|
| All instances at capacity | Queue call with hold message: "We're helping other callers. One moment please." |
| Queue wait >30 seconds | "I'm sorry for the wait. Let me text you instead so we don't keep you on hold." → SMS handoff |
| Queue depth >10 | Stop accepting new voice calls, fall back to voicemail → SMS |

**Implementation:** Telnyx call control: `queue` command with configurable hold audio.

### 10.4 Monitoring & Alerting

| Metric | Source | Alert Threshold |
|--------|--------|----------------|
| Response latency (P99) | Custom CloudWatch metric | >1000ms |
| Response latency (P50) | Custom CloudWatch metric | >600ms |
| Call drop rate | Telnyx CDR + custom metric | >5% in 5-min window |
| STT error rate | Deepgram error callbacks | >10% |
| TTS error rate | Cartesia error callbacks | >10% |
| Concurrent calls | Custom gauge metric | >80% capacity |
| Call duration (avg) | `voice_calls` table | >5 min average (indicates conversation issues) |
| Booking conversion rate | `voice_calls` outcome | <20% (investigate) |
| Cost per call (avg) | Calculated metric | >$0.30 |
| Service health | `/health` endpoint | Any component unhealthy |

**Dashboard:** CloudWatch dashboard with:
- Real-time concurrent calls
- Latency distribution (histogram)
- Call outcomes pie chart (booked / qualified / transferred / abandoned)
- Cost tracking
- Error rate trends

**Alerting:** CloudWatch Alarms → SNS → PagerDuty (production). Slack webhook for non-critical.

### 10.5 Overnight Scaling

Same pattern as API service:
- Scale to 0 instances at midnight ET (no calls expected)
- Scale to 1 instance at 7am ET
- Auto-scale up during business hours based on demand
- Keep 1 warm instance during clinic business hours

**Cold start mitigation:** If a call comes in during scaled-to-zero, Telnyx webhook hits API service (always running) which triggers ECS scale-up. Patient hears a brief hold message (~15-30s) while instance starts. If instance doesn't start in 30s → SMS fallback.

---

## Appendix A: Corrections to SPEC.md §8

The SPEC.md Section 8 is largely accurate. The following updates/corrections are recommended:

1. **TTS Provider:** SPEC recommends ElevenLabs as primary. This document recommends **Cartesia Sonic as primary** with ElevenLabs as fallback, due to 40% lower latency (90ms vs 150ms first byte) and 25% lower cost ($0.03 vs $0.04/min). Voice quality is comparable.

2. **Cost per call:** SPEC estimates $0.051/min and ~$0.15 for 3-min call. This document confirms $0.054/min and **$0.16** for 3-min call (slightly higher due to updated Cartesia pricing, but still well under $0.50 target).

3. **Implementation phases:** SPEC has 4 phases (2a-2d). This document restructures to 3 implementation phases (2a-2c) with 2d as ongoing enhancements. Phase 2c (interruption handling, load testing) is now part of the main implementation rather than a separate phase, as it's critical for launch quality.

4. **Natural pacing:** SPEC §8 mentions natural pacing. This document provides specific timing values (100-300ms padding based on context).

5. **Backchannel detection:** Not mentioned in SPEC. Added as critical UX requirement to prevent false barge-in on "uh-huh", "mm-hmm", etc.

6. **Recording consent:** SPEC doesn't address call recording consent laws. This document adds comprehensive state-by-state consent handling.

## Appendix B: Open Questions for Andrew

1. **Custom voice per clinic as premium feature?** ElevenLabs voice cloning costs ~$100/voice + higher per-minute. Should this be a paid add-on?
2. **Recording retention policy:** Default 90 days. Should clinics be able to extend? What's the max?
3. **Outbound call consent collection:** How do we capture prior express consent for Phase 2c appointment reminders? During booking? Separate opt-in?
4. **Pricing model for voice:** Per-call? Per-minute? Flat monthly fee? Or bundled with SMS tier?
5. **Spanish launch priority:** Is Spanish support for Phase 2d sufficient, or should it be Phase 2c?
