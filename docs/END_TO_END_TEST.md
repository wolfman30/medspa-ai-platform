# End-to-End Test Guide

This guide walks through testing the complete MedSpa AI booking flow from SMS → AI conversation → preference capture → deposit collection.

## Prerequisites

### 1. Environment Setup
```bash
# Copy example env file
cp .env.bootstrap.example .env

# Edit .env with your credentials
# REQUIRED:
# - OPENAI_API_KEY (get from platform.openai.com)
# - DATABASE_URL (or use docker-compose postgres)

# REQUIRED for SMS (choose one):
# Option A: Twilio (recommended for testing)
# - TWILIO_ACCOUNT_SID
# - TWILIO_AUTH_TOKEN  
# - TWILIO_FROM_NUMBER
# - TWILIO_ORG_MAP_JSON={"your-twilio-number":"org-123"}

# Option B: Telnyx (production)
# - TELNYX_API_KEY
# - TELNYX_MESSAGING_PROFILE_ID
```

### 2. Start Local Stack
```bash
# Option A: With Docker (includes Postgres + Redis)
docker-compose -f docker-compose.bootstrap.yml up -d

# Option B: Local (requires local Postgres + Redis)
# Make sure Postgres is running on localhost:5432
# Make sure Redis is running on localhost:6379
go run cmd/api/main.go
```

### 3. Run Database Migrations
```bash
# Install golang-migrate if needed
# brew install golang-migrate (Mac)
# choco install golang-migrate (Windows)

# Run migrations
migrate -path migrations -database "postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable" up
```

---

## Test Flow

### Step 1: Verify Server is Running

```bash
# Health check
curl http://localhost:8080/health

# Expected response:
# {"status":"ok"}
```

### Step 2: Create a Test Lead

```bash
# Create lead via API
curl -X POST http://localhost:8080/api/leads \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Customer",
    "phone": "+15555551234",
    "email": "test@example.com",
    "message": "Interested in Botox",
    "source": "manual_test"
  }'

# Save the lead ID from response
```

### Step 3: Simulate Incoming SMS Webhook

**Option A: Using ngrok for real webhooks (Twilio or Telnyx)**
```bash
# Install ngrok: https://ngrok.com/download
ngrok http 8080

# Copy the https URL (e.g., https://abc123.ngrok.io)

# For Twilio:
# Go to Twilio Console → Phone Numbers → Active Numbers
# Click your number → Messaging Configuration
# Set webhook URL to: https://abc123.ngrok.io/webhooks/twilio
# Send SMS to your Twilio number: "I want to book Botox for weekday afternoons"

# For Telnyx:
# Go to Telnyx Portal → Messaging → Messaging Profiles  
# Set webhook URL to: https://abc123.ngrok.io/webhooks/telnyx/messages
# Send SMS to your Telnyx number
```

**Option B: Manual Twilio webhook simulation (no ngrok needed)**
```bash
# Create webhook payload: test-twilio.json
cat > test-twilio.json << 'EOF'
{
  "MessageSid": "SM1234567890abcdef",
  "AccountSid": "AC1234567890abcdef",
  "From": "+15555551234",
  "To": "+15555559999",
  "Body": "I want to book Botox for weekday afternoons"
}
EOF

# Send webhook (update To number to match your TWILIO_ORG_MAP_JSON)
curl -X POST http://localhost:8080/webhooks/twilio \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM$(date +%s)" \
  -d "AccountSid=AC1234567890abcdef" \
  -d "From=%2B15555551234" \
  -d "To=%2B15555559999" \
  -d "Body=I+want+to+book+Botox+for+weekday+afternoons"
```

**Option C: Manual Telnyx webhook simulation**
```bash
# Create webhook payload file: test-sms.json
cat > test-sms.json << 'EOF'
{
  "data": {
    "event_type": "message.received",
    "id": "test-msg-001",
    "occurred_at": "2025-12-06T10:00:00.000000Z",
    "payload": {
      "id": "test-payload-001",
      "type": "SMS",
      "from": {
        "phone_number": "+15555551234"
      },
      "to": [
        {
          "phone_number": "+15555559999"
        }
      ],
      "text": "I want to book Botox for weekday afternoons",
      "received_at": "2025-12-06T10:00:00.000000Z"
    }
  }
}
EOF

# Send webhook (note: signature verification will fail without real Telnyx secret)
curl -X POST http://localhost:8080/webhooks/telnyx/messages \
  -H "Content-Type: application/json" \
  -H "Telnyx-Signature-Ed25519: test-signature" \
  -H "Telnyx-Timestamp: $(date +%s)" \
  -d @test-sms.json
```

### Step 4: Verify AI Response

Check logs for:
```
✓ Lead created/found
✓ Conversation started
✓ GPT response generated
✓ Scheduling preferences extracted:
  - Service: botox
  - Days: weekdays
  - Times: afternoon
✓ SMS reply queued
```

### Step 5: Check Database for Captured Preferences

```bash
# Connect to database
psql postgresql://medspa:medspa@localhost:5432/medspa

# Query leads with preferences
SELECT 
  id, 
  name, 
  phone, 
  service_interest, 
  preferred_days, 
  preferred_times, 
  scheduling_notes,
  deposit_status,
  priority_level,
  created_at
FROM leads 
WHERE phone = '+15555551234'
ORDER BY created_at DESC
LIMIT 1;
```

**Expected output:**
```
id              | test-lead-123
name            | Test Customer  
phone           | +15555551234
service_interest| botox
preferred_days  | weekdays
preferred_times | afternoon
scheduling_notes| Auto-extracted from conversation at 2025-12-06T10:00:00Z
deposit_status  | (null)
priority_level  | (null)
```

### Step 6: Continue Conversation to Trigger Deposit

```bash
# Send follow-up message agreeing to deposit
cat > test-deposit.json << 'EOF'
{
  "data": {
    "event_type": "message.received",
    "id": "test-msg-002",
    "occurred_at": "2025-12-06T10:05:00.000000Z",
    "payload": {
      "id": "test-payload-002",
      "type": "SMS",
      "from": {
        "phone_number": "+15555551234"
      },
      "to": [
        {
          "phone_number": "+15555559999"
        }
      ],
      "text": "Yes, I'd like to secure my spot with a deposit",
      "received_at": "2025-12-06T10:05:00.000000Z"
    }
  }
}
EOF

curl -X POST http://localhost:8080/webhooks/telnyx/messages \
  -H "Content-Type: application/json" \
  -d @test-deposit.json
```

### Step 7: Verify Square Payment Link

Check logs for:
```
✓ Deposit intent detected
✓ Square checkout link created
✓ SMS with payment link sent
```

Check outbox table:
```sql
SELECT 
  event_type,
  aggregate_id,
  payload,
  processed_at,
  created_at
FROM outbox
ORDER BY created_at DESC
LIMIT 5;
```

---

## Troubleshooting

### No AI Response
- Check `OPENAI_API_KEY` is set correctly
- Check logs for OpenAI API errors
- Verify Redis is running: `redis-cli ping` → should return `PONG`

### Preferences Not Saved
- Verify conversation contains booking keywords: "book", "appointment", "schedule"
- Check conversation history in Redis:
  ```bash
  redis-cli
  KEYS conv_*
  GET conv_{conversation_id}
  ```
- Enable debug logging: `LOG_LEVEL=debug`

### SMS Not Sent
- **Twilio:** Check `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM_NUMBER`
- **Telnyx:** Check `TELNYX_API_KEY`, `TELNYX_MESSAGING_PROFILE_ID`
- Look for rate limit errors in logs
- Verify quiet hours configuration isn't blocking sends

### Twilio "Failed to resolve org" Error
- Check `TWILIO_ORG_MAP_JSON` maps your Twilio number to an org ID
- Example: `TWILIO_ORG_MAP_JSON={"\+15555559999":"org-demo-001"}`
- The `To` number in webhook must match a key in this JSON

### Database Connection Issues
```bash
# Test connection
psql $DATABASE_URL -c "SELECT 1;"

# Check migrations ran
psql $DATABASE_URL -c "SELECT version FROM schema_migrations;"
```

---

## Success Criteria

✅ **Flow Complete When:**
1. Incoming SMS creates conversation
2. AI responds with booking questions
3. Customer provides preferences → saved to `leads` table
4. Customer agrees to deposit → Square link generated
5. All events logged to `outbox` table
6. SMS replies sent successfully

---

## Next Steps After Successful Test

1. **Test deposit payment flow:**
   - Click Square payment link
   - Complete test payment
   - Verify webhook updates `deposit_status = 'paid'`

2. **Test knowledge base:**
   - Seed clinic-specific data
   - Ask AI about services/pricing
   - Verify RAG retrieval works

3. **Load testing:**
   - Simulate concurrent conversations
   - Monitor Redis/Postgres performance
