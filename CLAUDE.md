# CLAUDE.md — MedSpa AI Platform

## Project Overview
24/7 AI receptionist for med spas. Texts back missed calls, qualifies leads, books appointments via Moxie, collects deposits via Square. See `SPEC.md` for full requirements.

## Tech Stack
- **Backend:** Go 1.24+ | PostgreSQL | Redis | Claude via AWS Bedrock
- **SMS:** Telnyx (primary), Twilio (fallback)
- **Payments:** Moxie checkout (auto-booking) or Square (deposits)
- **Browser Sidecar:** TypeScript, Playwright, Node.js
- **Frontend:** React (onboarding portal)
- **Infra:** AWS ECS/Fargate, Terraform

## Quick Commands
```bash
# Go
go test ./...                          # All tests
go test -v ./tests/...                 # Acceptance tests (THE stopping condition)
go test -v -run TestName ./internal/...  # Specific test
go vet ./...                           # Lint
gofmt -s -w .                          # Format

# Browser sidecar
cd browser-sidecar && npm run test:unit  # 96 unit tests
cd browser-sidecar && npm test           # All tests

# Build
go build -v ./...
```

## Architecture
```
cmd/api/                  → HTTP API + webhooks
cmd/conversation-worker/  → SQS worker for LLM + deposits
cmd/messaging-worker/     → Telnyx polling + retry
internal/conversation/    → AI conversation engine (core logic)
internal/messaging/       → SMS handling
internal/clinic/          → Per-clinic config
internal/browser/         → Browser sidecar Go client
internal/payments/        → Square integration
internal/notify/          → Operator notifications (SES email + SMS)
browser-sidecar/          → Playwright scraper + booking automation
web/onboarding/           → React admin portal
```

## Code Standards
- **Errors:** `fmt.Errorf("context: %w", err)`
- **Tests:** Table-driven, `testify/assert`
- **Commits:** Conventional commits (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`)
- **TS tests:** Behavior-focused, not implementation details

## Key Business Rules
1. All 5 qualifications (name, service, patient type, email, time prefs) must be collected before booking
2. Moxie clinics: auto-book via browser sidecar, patient pays on Moxie (no Square)
3. Square clinics: collect refundable deposit, operator manually confirms
4. No medical advice ever — deflect to provider consultation
5. Emergency symptoms → direct to 911 immediately

## First Client
- **Forever 22 Med Spa** (org `d0f9d4b4-...`)
- Moxie booking: `https://app.joinmoxie.com/booking/forever-22`
- Service aliases: botox→Tox, filler→Dermal Filler, lip filler→Lip Filler

## Team Structure
- **Andre (Tech Lead):** Orchestrates, reviews, commits to main
- **Backend Dev:** Go code in `internal/`, `cmd/`, `tests/`
- **Frontend Dev:** TypeScript in `browser-sidecar/`, `web/`
- **QA Engineer:** Runs tests, validates, writes test code + bug reports
