# MedSpa AI Platform (Revenue MVP)

SMS-only AI receptionist that converts missed calls and inbound texts into qualified, deposit-backed leads — **without EMR writes**.

## Objective (First Client: $2,000)

This repo is being driven to a single near-term outcome: sell the first medspa client for **$2,000** by shipping the **Revenue MVP**.

What we’re selling in that first deployment:

- After-hours missed-call SMS follow-up from the clinic’s main number (Telnyx voice → SMS trigger).
- Two-way SMS conversation that qualifies the lead (service + timing + new vs existing patient).
- Square-hosted deposit links using the clinic’s own Square account (OAuth), plus payment webhooks.
- Basic per-clinic stats/dashboard to prove ROI.

What is intentionally *not* in scope for the first paid client:

- No EMR/EHR integration, availability lookup, or CRM sync in phase 1 (staff books inside their EMR).
- No auto-finalized bookings; the assistant captures preferred times and patient full names for staff follow-up.
- No voice AI, Instagram DMs, web chat widget, etc.

## Current State

Revenue MVP core plumbing exists (Telnyx webhooks + compliance, conversation engine, Square checkout + webhook + OAuth, workers, metrics). Remaining work is primarily operational validation, clinic-specific knowledge content, and an authenticated client portal for paid onboarding (registration/login + clinic setup).

- Status + gaps: `docs/MVP_STATUS.md`
- Product scope + flows: `docs/revenue-mvp.md`
- Live validation checklist: `docs/LIVE_DEV_CHECKS.md`
- ECS deployment: `docs/DEPLOYMENT_ECS.md` (preferred), `docs/BOOTSTRAP_DEPLOYMENT.md` (deprecated)
- E2E harness + results log: `scripts/e2e_full_flow.py`, `docs/E2E_TEST_RESULTS.md`

## Client Portal (Paid Access) — In Progress

For paid clients, onboarding should happen through a login-protected portal (not via public endpoints).
Planned portal workflow:

- Register + log in (clinic admin)
- Complete clinic profile (hours, services, deposit rules)
- Connect Square OAuth for deposits
- Submit Telnyx hosted messaging + 10DLC registration
- Verify onboarding status and go live

Current implementation relies on admin endpoints and scripts; portal auth and user/org membership are not yet implemented.

## Repo Layout

```
cmd/
  api/                 # HTTP API + webhooks; can run inline workers (USE_MEMORY_QUEUE=true)
  conversation-worker/ # SQS/Dynamo worker for LLM + deposits (USE_MEMORY_QUEUE=false)
  messaging-worker/    # Telnyx hosted-order polling + retry worker
  voice-lambda/        # (ECS deployment) forwards voice webhooks to the API
  migrate/             # DB migrations runner
docs/                  # Product + ops docs
infra/terraform/       # AWS infrastructure (ECS/Fargate + Redis + API Gateway/Lambda)
internal/              # Domains (conversation, messaging, payments, leads, clinic, etc.)
migrations/            # Postgres schema migrations
```

## Local Development

### Prereqs

- Go 1.24+ (`go.mod` sets `go 1.24.0`)
- Docker (recommended for Postgres/Redis/LocalStack)
- Task and/or Make (both are supported by this repo)

### Quickstart (Docker Compose)

```bash
cp .env.example .env
docker compose up --build
DATABASE_URL=postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable go run ./cmd/migrate
curl http://localhost:8082/health
```

Notes:

- `docker-compose.yml` exposes the API at `http://localhost:8082` (container port `8080`).
- LocalStack is used for SQS/Dynamo when `USE_MEMORY_QUEUE=false`. Use `AWS_ENDPOINT_OVERRIDE=http://localstack:4566` (already set in `.env.example`).
- Bedrock calls are real AWS (LocalStack does not emulate Bedrock). Unit tests don’t require Bedrock; E2E does.

### Bootstrap Mode (Inline Workers)

If you want to avoid SQS/Dynamo entirely, run with `USE_MEMORY_QUEUE=true` (inline workers + Postgres-backed job store). See `.env.bootstrap.example` and `docker-compose.bootstrap.yml` (deprecated for prod, still useful for local bootstrap).

## E2E Test Harness

With the API running:

```bash
make e2e
```

- Quick mode: `make e2e-quick`
- Results/notes live in: `docs/E2E_TEST_RESULTS.md`
- Phone-view video recording (Windows-friendly): `powershell -File scripts/run-e2e-with-video.ps1 -ApiUrl http://localhost:8082` (docs: `docs/E2E_WITH_VIDEO.md`, artifacts: `tmp/e2e_videos/` + `tmp/e2e_artifacts/`)

## Key HTTP Endpoints

### Public (no auth)

- `GET /health`
- `GET /metrics` (when enabled)
- `POST /webhooks/telnyx/messages` (Telnyx inbound SMS + receipts)
- `POST /webhooks/telnyx/voice` (Telnyx missed-call trigger)
- `POST /webhooks/telnyx/hosted` (Telnyx hosted-order webhooks)
- `POST /messaging/twilio/webhook` (legacy Twilio inbound SMS)
- `POST /webhooks/twilio/voice` (Twilio missed-call trigger)
- `POST /webhooks/square` (Square payments webhook)
- `GET /oauth/square/callback` (Square OAuth callback)

### Admin (JWT)

Protected by `ADMIN_JWT_SECRET` via `Authorization: Bearer <token>`:

- `POST /admin/hosted/orders` (start Telnyx hosted messaging order)
- `POST /admin/10dlc/brands`, `POST /admin/10dlc/campaigns` (Telnyx 10DLC onboarding)
- `POST /admin/messages:send` (send SMS/MMS via Telnyx with compliance checks)
- `GET /admin/clinics/{orgID}/stats` (Revenue MVP counters)
- `GET /admin/clinics/{orgID}/dashboard` (conversion + LLM latency snapshot)
- `GET /admin/clinics/{orgID}/square/connect` (initiate Square OAuth)
- `GET /admin/clinics/{orgID}/square/status` (Square connection status)

### Tenant-scoped (X-Org-Id)

All tenant APIs require `X-Org-Id: <org-uuid>`:

- `POST /leads/web` (capture web lead)
- `POST /payments/checkout` (create deposit checkout link)
- `POST /conversations/start`, `POST /conversations/message`, `GET /conversations/jobs/{jobID}`
- `POST /knowledge/{clinicID}` (seed clinic knowledge for RAG)

## CI/CD + Testing

- CI: `.github/workflows/ci.yml` runs `go test`, `gofmt` check, `govulncheck`, and Terraform validation.
- Messaging packages have a 90% coverage gate: `make ci-cover` (`scripts/check_package_coverage.sh`).

## License

Proprietary - All rights reserved
