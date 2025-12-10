# MVP Status Report
**Last Updated:** 2025-12-09  
**Target:** Revenue MVP (SMS-only AI receptionist with deposit collection)

---

## Overall Progress: 75% Complete
Core plumbing for SMS reception, AI conversations, Square checkout links, and webhook/event plumbing is in place with good test coverage. Remaining gaps are wiring Square per-clinic OAuth into outbound deposit links, seeding clinic knowledge, and running a full end-to-end flow on a hosted number.

---

## What’s Complete
- **Messaging stack** (Telnyx primary, Twilio fallback): inbound webhooks with signature validation, STOP/HELP detection, quiet-hours suppression, retry worker, hosted-number + 10DLC onboarding, Prometheus metrics.  
  - `internal/http/handlers/telnyx_webhooks.go`, `internal/messaging/*`, `cmd/messaging-worker`, metrics in `internal/observability/metrics`.
- **AI conversation engine**: GPT-5-mini prompt tuned for qualification + deposit offer, Redis-backed history, RAG hooks, preference extraction into leads, deposit intent classification, job queue (SQS or in-memory + Postgres job store), worker sends SMS replies and dispatches deposits, payment-success → booking confirmation SMS.  
  - `internal/conversation/*`, inline worker wiring in `cmd/api/main.go` and queue worker in `cmd/conversation-worker/main.go`.
- **Lead + clinic data**: lead capture + listing, clinic config (hours, deposits) in Redis, org-scoped stats API (conversations/deposits).  
  - `internal/leads/*`, `internal/clinic/*`.
- **Payments foundation**: Square checkout link generation (per-org credentials supported), Square webhook verifies signature, updates payments, emits outbox events consumed by conversation to confirm bookings and send confirmation SMS. Token refresh worker and admin OAuth callback routes exist.  
  - `internal/payments/*`, outbox delivery `internal/events/*`.
- **Bootstrap/runtime**: Docker compose, USE_MEMORY_QUEUE path with inline workers, configs/envs, health checks, metrics, admin endpoints guarded by JWT.

---

## Gaps to Revenue MVP
1) **Per-clinic Square OAuth not used for AI-initiated deposits (P1)**  
   - Conversation workers send checkout links using platform `SQUARE_ACCESS_TOKEN/LOCATION_ID` only. OAuth credentials are saved per org, and the API checkout endpoint uses them, but `conversation.NewDepositDispatcher` isn’t wired to `SquareOAuthService` credentials or clinic phone numbers in either inline (`cmd/api`) or SQS worker (`cmd/conversation-worker`). Links today bill the platform account, not the clinic’s.  
   - Needs: inject `SquareOAuthService` as credentials provider + phone resolver into deposit dispatcher and worker wiring.

2) **After-hours missed-call flow only via Twilio voice webhook (P1)**  
   - Hosted Telnyx numbers support SMS, but missed-call auto-text depends on `/webhooks/twilio/voice`. There’s no Telnyx/hosted missed-call trigger, so “same main number” after-hours automation isn’t covered for hosted SMS numbers.

3) **Knowledge base unseeded (P1)**  
   - RAG store + ingestion endpoint exist, but no clinic documents are seeded; default snippets are generic. Without real service/policy data, replies will be generic.

4) **End-to-end validation not executed (P1)**  
   - No recorded test from inbound SMS/missed-call → GPT flow → deposit link → Square webhook → booking confirmation SMS. Quiet-hours/STOP paths are unit-tested but not exercised end-to-end.

5) **Per-clinic sender number for confirmations**  
   - Square webhook confirmation SMS uses `OrgNumberResolver`, but the conversation deposit sender doesn’t pick clinic-specific “from” numbers. Align with Square OAuth phone storage or hosted number mapping.

6) **Operational playbooks**  
   - Need scripts/docs to run ngrok/localstack for Telnyx + Square webhook testing, plus a sample clinic knowledge payload.

---

## Next Steps to Ship Revenue MVP
1) **Wire Square OAuth into deposits**  
   - Pass `SquareOAuthService` (credentials provider + phone lookup) into `conversation.NewDepositDispatcher` in both inline and SQS workers.  
   - Use clinic phone from Square creds (or hosted number) as `From` when sending deposit links/confirmations.

2) **Implement hosted missed-call trigger**  
   - Add Telnyx/hosted call event handler (or fallback polling) to enqueue a conversation start with the same intro used in `TwilioVoiceWebhook`.

3) **Seed clinic knowledge**  
   - Add `scripts/seed-knowledge.sh` and `testdata/demo-clinic-knowledge.json`; POST into `/knowledge/{clinic}` and hydrate RAG on startup.

4) **Run full E2E test and document**  
   - Scenario: inbound SMS or missed call → AI qualifies → deposit intent → Square checkout (clinic OAuth) → webhook → booking insert + confirmation SMS. Capture logs, DB rows, and webhook payloads in `docs/E2E_TEST_RESULTS.md`.

5) **Polish ops**  
   - Confirm quiet-hours/STOP default copy (SmsAckMessage currently has mojibake) and ensure hosted order worker + retry worker are part of the deploy task runner.

---

## Current Risk Notes
- **Payments:** Until OAuth is wired into deposits, links charge the platform Square account. Risk of wrong merchant funds + compliance issues.  
- **Missed-call coverage:** Hosted numbers won’t auto-SMS after-hours without a Telnyx voice/missed-call hook.  
- **AI quality:** No clinic knowledge seeded; responses may be too generic for sales conversion.

---

## Suggested Milestones
1) OAuth wiring + hosted missed-call trigger implemented and smoke-tested on one clinic.  
2) Knowledge seeded for a demo clinic; responses include pricing/policies.  
3) E2E happy-path test recorded (SMS → deposit → Square webhook → confirmation SMS/booking).  
4) Go live with first clinic on hosted SMS using their Square account.
