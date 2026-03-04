# Test Checklists

## Checklist 1: Adela Text-Back Number (+13304600937) Readiness

### Infrastructure (verify via API/logs)
- [ ] **API healthy**: `GET /ready` returns `{"ready":true}` with database, redis, sms all "ok"
- [ ] **Hosted number active**: `+13304600937` maps to Adela org `4440091b-b73f-49fa-87a2-ae22d0110981` (not the stale `bb507f20` generic MedSpa)
- [ ] **Telnyx connection correct**: Number is on "Medspa Missed Calls" connection (`2861615565930235546`), NOT "Nova Sonic Voice AI"
- [ ] **MOXIE_DRY_RUN=true**: Confirmed in ECS env — no real appointments created during testing

### Clinic Config (verify via admin API)
- [ ] **name**: "Adela Medical Spa" ✅
- [ ] **timezone**: "America/New_York" ✅
- [ ] **payment_provider**: "stripe" ✅
- [ ] **deposit_amount_cents**: 5000 ($50) ✅
- [ ] **service_aliases**: 110 entries ✅
- [ ] **booking_policies**: 3 policies set ✅
- [ ] **moxie_medspa_id**: ⚠️ NOT SET — needs `1349`
- [ ] **moxie_slug**: ⚠️ NOT SET — needs `adela-medical-spa`
- [ ] **default_provider_id**: ⚠️ NOT SET — needs Adela's default Moxie provider ID
- [ ] **greeting**: ⚠️ NOT SET — needs custom greeting
- [ ] **after_hours_greeting**: ⚠️ NOT SET — needs after-hours greeting
- [ ] **stripe_account_id**: ⚠️ NOT SET — Stripe Connect not linked (blocks deposit flow)
- [ ] **voice_ai_enabled**: false ✅ (correct for text-back only)

### SMS Flow Tests (manual — call from TextNow +13303339270)
1. [ ] **Missed call → text-back**: Call `+13304600937`, let it ring/reject → receive SMS within 10 seconds
2. [ ] **Correct greeting**: SMS says "Adela Medical Spa" (not "MedSpa" or generic)
3. [ ] **Service inquiry**: Reply "I'm interested in Botox" → AI asks qualification questions
4. [ ] **Name extraction**: Reply "My name is Jane" → AI captures name correctly
5. [ ] **Patient type**: Reply "I'm a new patient" → AI captures patient type
6. [ ] **Provider preference**: Reply "No preference" → AI moves to scheduling
7. [ ] **Availability fetch**: AI returns real Moxie time slots (requires moxie_medspa_id set)
8. [ ] **Time selection**: Pick a time → AI confirms selection
9. [ ] **Deposit flow**: AI sends booking policies + Stripe payment link (requires stripe_account_id)
10. [ ] **No duplicate messages**: No consecutive duplicate questions at any step
11. [ ] **After-hours behavior**: Test during off-hours — should show after-hours greeting (requires after_hours_greeting set)

### Blockers for Full Flow
- **moxie_medspa_id not set** → availability fetch will fail (steps 7-9 blocked)
- **stripe_account_id not set** → deposit/payment link won't generate (step 9 blocked)
- **greeting not set** → first SMS will use generic fallback
- Steps 1-6 (text-back + qualification) should work NOW

---

## Checklist 2: Voice AI Agent Readiness

### Infrastructure
- [ ] **Telnyx "Nova Sonic Voice AI" connection** (`2901962572544607439`) has a number assigned
- [ ] **Nova Sonic sidecar running**: Sidecar URL accessible from ECS (env: `NOVA_SONIC_SIDECAR_URL`)
- [ ] **Call Control stream URL set**: `NOVA_SONIC_STREAM_URL` configured in ECS
- [ ] **voice_ai_enabled=true** in clinic config for the test clinic
- [ ] **Bedrock Nova Sonic model access**: `us.amazon.nova-sonic-v1:0` enabled in AWS Console
- [ ] **MOXIE_DRY_RUN=true**: No real appointments during testing

### Voice System Prompt
- [ ] **Dynamic prompt builds**: `BuildVoiceSystemPrompt()` pulls clinic name, providers, deposit from config
- [ ] **Service aliases injected**: Voice prompt includes available services and alias mappings
- [ ] **Availability pre-fetched**: `FetchAvailabilitySummary()` runs on call start, real times in prompt

### Call Flow Tests (manual — call from any phone)
1. [ ] **Call answered**: Phone rings, Telnyx answers, hear greeting within 3 seconds
2. [ ] **Greeting correct**: AI says clinic name (e.g., "Hi, this is Adela Medical Spa...")
3. [ ] **AI responds to speech**: Say "I'd like to book Botox" → AI responds with relevant questions
4. [ ] **Qualification flow**: AI asks name, patient type, scheduling preference (in order)
5. [ ] **Availability spoken**: AI reads real available times from Moxie
6. [ ] **Time selection**: Say a time → AI confirms and explains deposit
7. [ ] **SMS handoff**: AI sends payment link via SMS after call (or during)
8. [ ] **Audio quality**: No echo, no long pauses (>3 sec), no garbled speech
9. [ ] **No tool audio dropout**: If tools are enabled, audio output continues (known Nova Sonic issue — tools break audio)
10. [ ] **Graceful hangup**: Call ends cleanly, no orphaned sessions

### Call Edge Cases
11. [ ] **Caller silent for 10s**: AI prompts "Are you still there?"
12. [ ] **Unintelligible speech**: AI asks to repeat, doesn't hallucinate
13. [ ] **Non-voice clinic call**: Call to number with `voice_ai_enabled=false` → call rejected → text-back SMS sent
14. [ ] **Multiple concurrent calls**: Two calls to same number don't crash sidecar

### Known Issues to Verify Fixed
- [ ] **Nova Sonic tools DON'T break audio**: `toolConfiguration` in promptStart causes text-only mode — verify tools are disabled OR audio works with tools
- [ ] **TTS greeting before Nova Sonic**: Telnyx TTS speaks greeting → `speak.ended` → starts Nova Sonic streaming (not concurrent)
- [ ] **No crossmodal text→audio issue**: System doesn't mix USER AUDIO + USER TEXT turns

### Post-Call Verification
- [ ] **Lead created**: Check leads table for caller's phone number
- [ ] **Conversation logged**: Transcript persisted (Redis or DB)
- [ ] **SMS sent**: If deposit flow triggered, check outbound SMS logs
- [ ] **No real booking created**: Verify MOXIE_DRY_RUN prevented actual appointment

---

## Quick Smoke Test (5 min, text-back only)

The minimum test to verify Adela's number works:

1. Call `+13304600937` from TextNow (`+13303339270`)
2. Wait for SMS response (should arrive within 10 sec)
3. Verify SMS says "Adela Medical Spa" (not generic)
4. Reply "I want Botox" → should get qualification question
5. Reply "Jane Smith" → should acknowledge name and ask next question

If all 5 pass, text-back is working. The full qualification + deposit flow needs the missing config fields set first.
