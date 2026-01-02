# Claude Code Instructions for MedSpa AI Platform

## Project Overview
SMS-based AI receptionist for medical spas with deposit collection via Square.

**Tech Stack:** Go 1.24, PostgreSQL, Redis, AWS (Bedrock for Claude, SQS, ECS)
**Messaging:** Telnyx (primary), Twilio (fallback)
**Payments:** Square OAuth + checkout links

## MVP Status: 85% Complete

See `docs/MVP_STATUS.md` for full details. Key gaps:
1. Client portal authentication + onboarding
2. Operational playbooks/runbooks
3. Production clinic knowledge content

## Autonomous Operation Instructions

### Definition of Done (Stop Condition)
**Stop working only when ALL acceptance tests pass:**
```bash
go test -v ./tests/...
```

If any test fails, fix it and continue working.

### Work Priority Order
1. Fix any failing tests first
2. Complete features from MVP_STATUS.md gaps
3. Write tests for new code
4. Update documentation

### Running Tests
```bash
# Quick unit tests
make test

# Full test suite with coverage
make cover

# MVP acceptance tests (THE stopping condition)
go test -v ./tests/...

# E2E tests (requires running API)
python scripts/e2e_full_flow.py
```

### Building
```bash
# Build all
go build -v ./...

# Build API binary
go build -v -o bin/medspa-api ./cmd/api

# Docker
docker compose up --build
```

## Code Standards

### Go Style
- Use standard Go formatting: `gofmt -s -w .`
- Run `go vet ./...` before committing
- Table-driven tests preferred
- Error wrapping with `fmt.Errorf("context: %w", err)`

### File Organization
```
cmd/           # Entry points (api, workers)
internal/      # Private packages
  http/handlers/  # HTTP handlers
  conversation/   # AI conversation engine
  payments/       # Square integration
  messaging/      # SMS (Telnyx/Twilio)
  leads/          # Lead capture
  clinic/         # Clinic config
pkg/           # Public packages
scripts/       # Operational scripts
tests/         # Acceptance tests
migrations/    # Database migrations
```

### Testing Requirements
- New features require tests
- Handlers need request/response tests
- Services need unit tests with mocks
- Use `testify/assert` and `testify/require`

## Key Files to Understand

### Conversation Flow
- `internal/conversation/service.go` - Main conversation orchestration
- `internal/conversation/llm_service.go` - Claude/Bedrock integration
- `internal/conversation/deposit_sender.go` - Square checkout links

### Messaging
- `internal/http/handlers/telnyx_webhooks.go` - Telnyx webhook handling
- `internal/messaging/handler.go` - SMS processing
- `internal/messaging/twilioclient/` - Twilio client

### Payments
- `internal/payments/square_checkout.go` - Checkout link generation
- `internal/payments/handler.go` - Payment webhooks
- `internal/payments/oauth.go` - Square OAuth

## Environment Variables

### Required for Local Dev
```bash
DATABASE_URL=postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable
REDIS_URL=redis://localhost:6379
SMS_PROVIDER=twilio  # or telnyx
```

### Optional/Production
```bash
AWS_REGION=us-east-1
BEDROCK_MODEL_ID=anthropic.claude-3-haiku-20240307-v1:0
TELNYX_API_KEY=...
TWILIO_ACCOUNT_SID=...
TWILIO_AUTH_TOKEN=...
SQUARE_APP_ID=...
SQUARE_APP_SECRET=...
```

## Current Work Items (From MVP_STATUS.md)

### P1 - Must Complete
1. [ ] Client portal authentication (Cognito)
2. [ ] Clinic profile management UI
3. [ ] Square OAuth connect in portal
4. [ ] Telnyx 10DLC registration in portal

### P2 - Hardening
1. [ ] Remove TWILIO_SKIP_SIGNATURE for production
2. [ ] E2E test documentation update
3. [ ] Webhook tunnel runbook

## Autonomous Session Checklist

When running autonomously:
1. Run `go test -v ./tests/...` to check current status
2. Fix any failing tests
3. Check `docs/MVP_STATUS.md` for next work item
4. Implement feature with tests
5. Run full test suite
6. Update MVP_STATUS.md with progress
7. Repeat until all acceptance tests pass

## Common Commands Reference

```bash
# Development
make run-api           # Start API server
make run-worker        # Start messaging worker
docker compose up -d   # Start all services

# Database
make migrate           # Run migrations
psql $DATABASE_URL     # Connect to DB

# Testing
make test              # Unit tests
make cover             # With coverage
go test -v ./tests/... # Acceptance tests

# Linting
make fmt               # Format code
make vet               # Static analysis
make lint              # Full lint check
```

## Notes for AI Agents

- Always read the file before editing
- Run tests after each significant change
- Prefer small, focused commits
- Don't add features beyond MVP scope
- Keep solutions simple - avoid over-engineering
- Update MVP_STATUS.md when completing items
