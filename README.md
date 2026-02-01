# MedSpa AI Platform

AI-powered SMS receptionist that converts missed calls into qualified, deposit-backed appointments.

## How It Works

```
Missed Call → Instant SMS → AI Qualifies Lead → Collect Deposit → Confirm Booking
     │              │              │                   │               │
   < 5 sec      Service?       Extracts:          Square          Notify
                Timing?        preferences        checkout         staff
```

**Result:** Staff effort drops from 15-20 min intake to 2-3 min confirmation.

## Quick Start

```bash
cp .env.example .env
docker compose up -d
DATABASE_URL=postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable go run ./cmd/migrate
curl http://localhost:8082/health
```

## Testing

```bash
make test                  # Unit tests
go test -v ./tests/...     # Acceptance tests
```

## Documentation

See [CLAUDE.md](CLAUDE.md) for complete specification including:
- Business requirements & 4-step process
- Acceptance criteria
- Compliance (HIPAA, TCPA, PCI)
- Technical architecture
- Development workflow

## License

Proprietary - All rights reserved
