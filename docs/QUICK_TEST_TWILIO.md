# Quick Test Guide - Twilio Setup

## 🚀 Quick Start (3 steps)

### 1. Check your .env has these values:
```bash
# REQUIRED
OPENAI_API_KEY=sk-your-key-here
DATABASE_URL=postgresql://...
REDIS_ADDR=localhost:6379

# TWILIO (for testing)
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your-auth-token
TWILIO_FROM_NUMBER=+15559998888
TWILIO_ORG_MAP_JSON={"\\+15559998888":"org-demo-001"}
# Note: The number in JSON must match TWILIO_FROM_NUMBER
```

### 2. Start the server:
```bash
# Make sure Postgres + Redis are running
go run cmd/api/main.go
```

### 3. Run the test:
```bash
python scripts/test_e2e.py
```

---

## 📋 What the Test Does

1. ✅ Checks if API is healthy
2. ✅ Creates a test lead
3. ✅ Sends fake Twilio webhook: "I want to book Botox for weekday afternoons"
4. ✅ Waits 5 seconds for AI to respond
5. ✅ Shows you how to check database

---

## 🔍 Verify Results

### Check Database:
```bash
psql $DATABASE_URL
```
```sql
SELECT 
  service_interest,
  preferred_days,
  preferred_times,
  scheduling_notes
FROM leads 
WHERE phone = '+15555551234'
ORDER BY created_at DESC 
LIMIT 1;
```

**Expected:**
- `service_interest` = `botox`
- `preferred_days` = `weekdays`
- `preferred_times` = `afternoon`
- `scheduling_notes` = `Auto-extracted from conversation...`

---

## 🐛 Common Issues

### ❌ "Org mapping not found"
**Fix:** Update TWILIO_ORG_MAP_JSON in .env:
```bash
TWILIO_ORG_MAP_JSON={"\\+15559998888":"org-demo-001"}
```
The number must match your Twilio number.

### ❌ "OpenAI API error"
**Fix:** Check OPENAI_API_KEY is valid and has credits.

### ❌ "Database connection failed"
**Fix:** Make sure Postgres is running:
```bash
# Test connection
psql $DATABASE_URL -c "SELECT 1;"

# Run migrations if needed
migrate -path migrations -database "$DATABASE_URL" up
```

### ❌ "Redis connection failed"
**Fix:** Make sure Redis is running:
```bash
redis-cli ping
# Should return: PONG
```

---

## 🎯 Next Steps After Success

1. **Test with real Twilio SMS:**
   - Use ngrok: `ngrok http 8080`
   - Update Twilio webhook URL to: `https://xxx.ngrok.io/webhooks/twilio`
   - Send real SMS to your Twilio number

2. **Test deposit flow:**
   - Continue conversation
   - Customer agrees to deposit
   - Verify Square payment link generated

3. **Seed knowledge base:**
   - Add real clinic data
   - Test RAG retrieval

---

## 📞 Real Twilio Setup (Optional)

If you want to test with real SMS:

1. **Install ngrok:**
   ```bash
   # Download from https://ngrok.com/download
   ngrok http 8080
   ```

2. **Update Twilio webhook:**
   - Go to: https://console.twilio.com
   - Phone Numbers → Active Numbers
   - Click your number
   - Messaging Configuration → Webhook URL:
     ```
     https://abc123.ngrok.io/webhooks/twilio
     ```
   - Method: `POST`
   - Save

3. **Send SMS to your Twilio number:**
   ```
   I want to book Botox for weekday afternoons
   ```

4. **Check logs and database** for captured preferences!
