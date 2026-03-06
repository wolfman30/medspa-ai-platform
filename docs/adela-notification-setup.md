# Adela Medical Spa — Notification Configuration

## Problem
Adela Medical Spa's notification config shows `sms_enabled: false` with no recipients.
The operator never gets notified when a patient pays a deposit.

## Fix
Update Adela's clinic config via the admin API to enable notifications.
The notification config is stored in the clinic config JSON (not a separate table).

### Fields to set via Admin API

```json
{
  "notify_on_payment": true,
  "email_enabled": true,
  "email_recipients": ["adela@adelamedicalspa.com"],
  "sms_enabled": true,
  "sms_recipients": ["+13304600937"]
}
```

**Note:** The email and phone above are placeholders. Andrew needs to provide:
- The real operator email address
- The real operator cell phone number (currently using the clinic's Telnyx number as placeholder)

### How to update
Use the clinic admin API endpoint to PATCH the clinic config with the notification fields above.
