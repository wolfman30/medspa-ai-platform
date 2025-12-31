# MVP Status Report
**Last Updated:** 2025-12-31  
**Target:** Revenue MVP (SMS-only AI receptionist with deposit collection)

---

## Overall Progress: 85% Complete
Core plumbing for SMS reception, AI conversations, Square checkout links, and webhook/event plumbing is in place with good test coverage. Square OAuth is now wired through the conversation deposit senders (inline + SQS workers), hosted missed-call triggers exist for Telnyx numbers, and knowledge seeding has a repeatable script + defaults. Remaining gaps are real clinic knowledge content and a paid client portal for registration/login + onboarding.

---

## What’s Complete
- **Messaging stack** (Telnyx primary, Twilio fallback): inbound webhooks with signature validation, STOP/HELP detection, quiet-hours suppression, retry worker, hosted-number + 10DLC onboarding, Prometheus metrics.  
  - `internal/http/handlers/telnyx_webhooks.go`, `internal/messaging/*`, `cmd/messaging-worker`, metrics in `internal/observability/metrics`.
- **AI conversation engine**: Claude (via AWS Bedrock) prompt tuned for qualification + deposit offer, Redis-backed history, RAG hooks, preference extraction into leads, deposit intent classification, job queue (SQS or in-memory + Postgres job store), worker sends SMS replies and dispatches deposits, payment-success + booking confirmation SMS.  
  - `internal/conversation/*`, inline worker wiring in `cmd/api/main.go` and queue worker in `cmd/conversation-worker/main.go`.
- **Lead + clinic data**: lead capture + listing, clinic config (hours, deposits) in Redis, org-scoped stats API (conversations/deposits).  
  - `internal/leads/*`, `internal/clinic/*`.
- **Payments foundation**: Square checkout link generation (per-org credentials supported), Square webhook verifies signature, updates payments, emits outbox events consumed by conversation to confirm bookings and send confirmation SMS. Token refresh worker and admin OAuth callback routes exist.  
  - `internal/payments/*`, outbox delivery `internal/events/*`.
- **Per-clinic Square deposits**: Conversation deposit dispatcher now uses `SquareOAuthService` credentials + DB/hosted phone resolver, in both inline API workers and SQS worker. Deposit SMS use clinic numbers when present.  
  - `cmd/api/main.go`, `cmd/conversation-worker/main.go`, `internal/conversation/deposit_sender.go`.
- **Hosted missed-call trigger**: Telnyx voice/hosted webhook enqueues the same missed-call intro used for Twilio and sends an ack from the clinic number.  
  - `internal/http/handlers/telnyx_webhooks.go`, router at `/webhooks/telnyx/voice`.
- **Knowledge seeding + defaults**: Default RAG docs hydrate on startup when Redis is empty; repeatable seed script + demo payload added.  
  - `internal/app/bootstrap/conversation.go`, `scripts/seed-knowledge.sh`, `testdata/demo-clinic-knowledge.json`.
- **Ops scripting**: E2E smoke harness + latest run recorded.  
  - `scripts/test-e2e.sh`, `docs/E2E_TEST_RESULTS.md`.
- **Bootstrap/runtime**: Docker compose, USE_MEMORY_QUEUE path with inline workers, configs/envs, health checks, metrics, admin endpoints guarded by JWT.

---

## Gaps to Revenue MVP
1) **Clinic knowledge depth (P1)**  
   - Demo payload + defaults exist, but production clinics still need real services/policies seeded via the script or form.

2) **Operational playbooks**  
   - Need a short checklist for ngrok/localstack/Telnyx webhook validation and how to replay Square webhooks with clinic OAuth creds.

3) **Client portal authentication + onboarding (P1)**  
   - Paid clients should register/login, manage clinic profile, connect Square, and complete Telnyx 10DLC in a portal (not via public endpoints).  

---

## Next Steps to Ship Revenue MVP
1) **Repeat E2E in dev/prod and document**  
   - Re-run with real clinic OAuth creds + hosted numbers; capture logs, DB rows, and webhook payloads in `docs/E2E_TEST_RESULTS.md`.

2) **Seed real clinic knowledge**  
   - Use `scripts/seed-knowledge.sh` + `testdata/demo-clinic-knowledge.json` as a template; confirm RAG hydration on startup for each onboarded clinic.

3) **Ops checklist**  
   - Publish a short runbook for webhook tunnel setup, Square webhook replay with OAuth credentials, and monitoring queues/outbox for deposits.

4) **Client portal onboarding**  
   - Implement registration/login, org membership, and portal flows for clinic profile, Square connect, and Telnyx 10DLC.

---

## Current Risk Notes
- **Payments:** Local E2E is complete; still need live clinic OAuth + phone resolution validation in staging/production.  
- **AI quality:** Demo knowledge is seeded; production clinics still need detailed service/policy content for strong responses.

---

## Suggested Milestones
1) E2E happy-path test recorded locally (repeat in dev/prod with real clinic creds).  
2) Knowledge seeded for the first clinic; responses include pricing/policies.  
3) Ops checklist/runbook published for webhook tunnels and Square replay.  
4) Go live with first clinic on hosted SMS using their Square account.
