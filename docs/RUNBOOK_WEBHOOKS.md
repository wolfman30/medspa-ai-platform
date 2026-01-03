# Webhook Operations Runbook

Quick reference for setting up and testing webhooks in development and production.

## Local Development Setup

### 1. Start ngrok Tunnel

```bash
# Install ngrok if needed
# https://ngrok.com/download

# Start tunnel to local API (docker compose exposes 8082; go run uses 8080)
ngrok http 8082

# Note the HTTPS URL, e.g., https://abc123.ngrok.io
```

### 2. Configure Environment

```bash
# .env file
PUBLIC_BASE_URL=https://abc123.ngrok.io
SMS_PROVIDER=telnyx  # or twilio
TELNYX_WEBHOOK_SECRET=your-telnyx-webhook-secret
# Optional Twilio settings (if you use Twilio instead of Telnyx)
# TWILIO_SKIP_SIGNATURE=true  # DEV ONLY - never in production!
```

### 3. Configure Provider Webhooks

**Telnyx:**
1. Go to https://portal.telnyx.com
2. Navigate to Messaging > Profiles
3. Set webhook URL: `https://abc123.ngrok.io/webhooks/telnyx/messages`
4. For voice: `https://abc123.ngrok.io/webhooks/telnyx/voice`

**Twilio:**
1. Go to https://console.twilio.com
2. Navigate to Phone Numbers > Manage > Active Numbers
3. Set webhook URL: `https://abc123.ngrok.io/webhooks/twilio`
4. For voice: `https://abc123.ngrok.io/webhooks/twilio/voice`

## Testing Webhooks

### Simulate Missed Call (Telnyx)

```bash
curl -X POST https://abc123.ngrok.io/webhooks/telnyx/voice \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "event_type": "call.hangup",
      "payload": {
        "call_control_id": "test-123",
        "from": "+15551234567",
        "to": "+18662894911",
        "hangup_cause": "originator_cancel"
      }
    }
  }'
```

### Simulate Inbound SMS (Telnyx)

```bash
curl -X POST https://abc123.ngrok.io/webhooks/telnyx/messages \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "event_type": "message.received",
      "payload": {
        "id": "msg-123",
        "from": { "phone_number": "+15551234567" },
        "to": [{ "phone_number": "+18662894911" }],
        "text": "I want to book an appointment"
      }
    }
  }'
```

### Simulate Square Payment Webhook

```bash
curl -X POST https://abc123.ngrok.io/webhooks/square \
  -H "Content-Type: application/json" \
  -H "X-Square-Signature: test-signature" \
  -d '{
    "type": "payment.completed",
    "data": {
      "object": {
        "payment": {
          "id": "pay-123",
          "order_id": "order-123",
          "status": "COMPLETED",
          "amount_money": { "amount": 5000, "currency": "USD" }
        }
      }
    }
  }'
```

## Production Checklist

### Before Go-Live

- [ ] `TWILIO_SKIP_SIGNATURE=false` or unset
- [ ] `PUBLIC_BASE_URL` set to production domain
- [ ] Telnyx webhook secret configured
- [ ] Twilio auth token configured
- [ ] Square webhook signature key configured
- [ ] SSL certificate valid on production domain

### Verify Signature Validation

```bash
# Should return 401 without valid signature
curl -X POST https://api.yourprod.com/webhooks/twilio \
  -d "From=+15551234567&Body=test"
# Expected: 401 Unauthorized

# Should return 401 without valid signature
curl -X POST https://api.yourprod.com/webhooks/telnyx/messages \
  -H "Content-Type: application/json" \
  -d '{"data":{"event_type":"message.received"}}'
# Expected: 403 Forbidden
```

## Monitoring

### Check Outbox Queue

```sql
-- Pending events
SELECT * FROM outbox_events
WHERE delivered_at IS NULL
ORDER BY created_at DESC
LIMIT 10;

-- Recent failures
SELECT * FROM outbox_events
WHERE error IS NOT NULL
ORDER BY created_at DESC
LIMIT 10;
```

### Check Payment Status

```sql
-- Recent payments
SELECT id, lead_id, status, amount_cents, created_at
FROM payments
ORDER BY created_at DESC
LIMIT 10;

-- Pending deposits
SELECT * FROM payments
WHERE status = 'pending'
ORDER BY created_at DESC;
```

### View Conversation History

```sql
-- Recent conversations
SELECT c.id, c.lead_id, c.phone_number, c.created_at,
       (SELECT COUNT(*) FROM conversation_messages WHERE conversation_id = c.id) as msg_count
FROM conversations c
ORDER BY c.created_at DESC
LIMIT 10;
```

## Troubleshooting

### Webhook Not Receiving

1. Check ngrok is running and URL matches `.env`
2. Verify provider webhook configuration
3. Check ngrok web interface: http://localhost:4040

### Signature Validation Failing

1. Verify webhook secret matches provider config
2. Check `PUBLIC_BASE_URL` matches actual URL
3. For Twilio: ensure auth token is correct

### Deposit Link Not Sending

1. Check Square OAuth credentials for the org
2. Verify `payments` table has pending record
3. Check outbox for failed events
4. Review conversation worker logs

### SMS Not Sending

1. Check provider credentials (Telnyx API key or Twilio SID/token)
2. Verify phone number is registered/verified
3. Check messaging worker logs
4. Review quiet hours settings

## Environment Variables Reference

| Variable | Required | Description |
|----------|----------|-------------|
| `PUBLIC_BASE_URL` | Yes | Base URL for webhooks |
| `SMS_PROVIDER` | No | `twilio`, `telnyx`, or `auto` |
| `TELNYX_API_KEY` | If Telnyx | Telnyx API key |
| `TELNYX_WEBHOOK_SECRET` | If Telnyx | For signature validation |
| `TWILIO_ACCOUNT_SID` | If Twilio | Twilio account SID |
| `TWILIO_AUTH_TOKEN` | If Twilio | For signature validation |
| `TWILIO_SKIP_SIGNATURE` | Dev only | **Never true in production** |
| `SQUARE_WEBHOOK_SIGNATURE_KEY` | Yes | Square webhook verification |
