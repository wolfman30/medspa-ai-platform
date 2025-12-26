# E2E Test Results - Revenue MVP

Use this log to record end-to-end runs across hosted numbers, AI conversation, Square deposits, and webhook confirmations.

---

## Quick Start - Automated E2E Test

Run the full automated E2E test with a single command:

```bash
# Full test (8s waits between steps)
make e2e

# Quick test (3s waits)
make e2e-quick

# Or directly:
./scripts/run-e2e-test.sh
./scripts/run-e2e-test.sh --quick
./scripts/run-e2e-test.sh --no-db  # Skip database checks
```

### Windows PowerShell

```powershell
# Full test (8s waits between steps)
.\scripts\run-e2e-test.ps1

# Quick test (3s waits)
.\scripts\run-e2e-test.ps1 -Quick

# Skip database checks
.\scripts\run-e2e-test.ps1 -NoDb
```

**Prerequisites:**
- API running (`make run-api` or `docker compose up`)
- Python 3 with `requests` module
- PostgreSQL accessible (or use `--no-db` flag)

Note: `docker-compose.yml` exposes the API at `http://localhost:8082`; `make run-api` defaults to `http://localhost:8080`. Set `API_URL` accordingly.

---

## What the Automated Test Does

The `scripts/e2e_full_flow.py` script simulates the complete production flow:

1. **Health Check** - Verifies API is running
2. **Seed Knowledge** - Scrapes a prospect website (Boulevard client: Skin House Facial Bar) into knowledge snippets, uploads them, and verifies RAG is working
3. **Create Lead** - Creates a test lead record
4. **Missed Call Webhook** - Simulates Telnyx voice webhook (call.hangup with no_answer)
5. **Customer SMS #1** - "Hi, I want to book Botox for weekday afternoons"
6. **Customer SMS #2** - "Yes, I'm a new patient. What times do you have available?"
7. **Customer SMS #3** - "Friday at 3pm works. Yes, I'll pay the deposit."
8. **Verify Preferences** - Checks database for extracted preferences
9. **Create Checkout** - Generates Square checkout link
10. **Payment Webhook** - Simulates Square payment.completed
11. **Verify Payment** - Checks payment status and outbox events
12. **Final Verification** - Confirms lead deposit status updated

---

## Latest Run

- **Date:** _update after running_
- **Environment:** local API + workers
- **Numbers:** Telnyx hosted (`/webhooks/telnyx/*`)
- **Org/Clinic:** 11111111-1111-1111-1111-111111111111
- **Test Phone:** +15550001234 (simulated customer)
- **Clinic Phone:** +15559998888 (simulated clinic)
- **Flow exercised:** missed call → SMS conversation → AI preference extraction → checkout → Square webhook → confirmation
- **Outcome:** _pending execution_

---

## Manual Verification Steps

After running the automated test, verify these manually:

### 1. Check API Logs
```bash
# Look for conversation flow
docker compose logs api | grep -E "(conversation|deposit|SMS)"
```

### 2. Check Database
```sql
-- Lead preferences
SELECT service_interest, preferred_days, preferred_times, deposit_status
FROM leads WHERE phone = '+15550001234'
ORDER BY created_at DESC LIMIT 1;

-- Payment status
SELECT status, provider_ref, amount_cents
FROM payments
ORDER BY created_at DESC LIMIT 5;

-- Outbox events
SELECT event_type, processed_at, created_at
FROM outbox
ORDER BY created_at DESC LIMIT 10;
```

### 3. Check Redis (Conversation History)
```bash
redis-cli
KEYS conv_*
GET conv_{conversation_id}
```

---

## Test Configuration

Environment variables for customization:

| Variable | Default | Description |
|----------|---------|-------------|
| `API_URL` | http://localhost:8082 | API endpoint |
| `DATABASE_URL` | postgresql://medspa:medspa@localhost:5432/medspa | PostgreSQL connection |
| `TEST_ORG_ID` | 11111111-1111-1111-1111-111111111111 | Test organization UUID |
| `TEST_CUSTOMER_PHONE` | +15550001234 | Simulated customer phone |
| `TEST_CLINIC_PHONE` | +15559998888 | Clinic's hosted number |
| `AI_RESPONSE_WAIT` | 8 | Seconds to wait for AI processing |
| `SKIP_DB_CHECK` | false | Set to 1 to skip database queries |
| `KNOWLEDGE_SCRAPE_URL` | https://skinhousefacialbar.com | Website to scrape into knowledge (set to `off` to disable scraping) |
| `KNOWLEDGE_SCRAPE_MAX_PAGES` | 5 | Max pages to fetch from the site |
| `KNOWLEDGE_SCRAPE_TIMEOUT` | 15 | Per-page HTTP timeout (seconds) |
| `KNOWLEDGE_PREVIEW_FILE` | tmp/knowledge_preview.json | Where the scraper writes a safe preview (titles + short excerpts) |
| `KNOWLEDGE_FILE` | testdata/demo-clinic-knowledge.json | Used only when scraping is disabled |

---

## Manual Notes

Record observations from test runs here:

### Run: [DATE]
- Square webhook payload:
- Payment ID:
- Errors observed:
- SMS delivery status:

---

## Troubleshooting

### API Not Healthy
- Check Docker containers are running: `docker compose ps`
- Check logs: `docker compose logs api`
- Verify PostgreSQL is accessible

### No AI Response
- Check AWS Bedrock credentials configured
- Check logs for Bedrock errors
- Verify Redis is running: `redis-cli ping`

### Square Webhook Fails
- In development, signature verification is bypassed
- Check SQUARE_WEBHOOK_SIGNATURE_KEY if in production mode
- Verify payment metadata contains org_id, lead_id, booking_intent_id

### Database Connection Fails
- Verify DATABASE_URL is correct
- Run with `--no-db` flag to skip database checks
- Check PostgreSQL is running: `docker compose ps postgres`
