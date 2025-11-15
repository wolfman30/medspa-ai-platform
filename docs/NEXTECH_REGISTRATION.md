# Nextech Developer Portal Registration Guide

**Time Required:** 15-30 minutes
**Prerequisites:** Business email address, basic company information

---

## Step 1: Access the Developer Portal

1. **Navigate to:** [https://www.nextech.com/developers-portal](https://www.nextech.com/developers-portal)

2. **Look for:** "Register" or "Sign Up" button (usually top-right corner)

---

## Step 2: Create Your Account

Fill out the registration form with:

### **Required Information:**

```
Business Name: [Your Company Name] (e.g., "MedSpa AI Booking Platform")
Contact Name: [Your Full Name]
Email: [Your Business Email]
Phone: [Your Business Phone]
Company Website: [If you have one]
```

### **Application Details:**

```
Application Name: MedSpa AI Booking Platform
Application Type: Integration Partner / Third-Party Application
Integration Purpose: AI-powered appointment booking and lead capture for medical spas

Brief Description:
"We're building an AI-first platform that helps medical spas capture missed leads
and convert them into booked, deposit-secured appointments through automated
multi-channel communication. Our integration with Nextech will enable real-time
availability queries, direct calendar writes, and patient record creation."

Use Case:
- Real-time appointment availability queries
- Automated appointment booking via AI conversations
- Patient profile creation and search
- SMS-based booking confirmations
```

### **Technical Information:**

```
API Access Needed:
☑️ Patient Read/Write (FHIR Patient resource)
☑️ Appointment Read/Write (FHIR Appointment resource)
☑️ Slot Read (FHIR Slot resource for availability)
☑️ Schedule Read (FHIR Schedule resource)

Expected API Usage:
- Volume: 100-500 requests/day initially (growing to 5,000+/day)
- Peak Times: After-hours (5pm-9pm), weekends
- Rate: Within 20 req/sec limit per endpoint

Development Environment:
☑️ Sandbox access requested
☐ Production access (will request after testing)
```

---

## Step 3: Submit & Wait for Approval

### **What Happens Next:**

1. **Confirmation Email** (immediate)
   - Confirms your registration was received
   - May include next steps or documentation links

2. **Account Review** (1-5 business days)
   - Nextech reviews your application
   - May contact you for additional information
   - Could request a brief call to discuss use case

3. **Approval Email** (after review)
   - Credentials provided (Client ID + Client Secret)
   - Sandbox API endpoint URL
   - Documentation links
   - Rate limits and usage guidelines

### **If You Don't Hear Back in 3 Days:**

**Contact Nextech Support:**
- Email: developer-support@nextech.com (if available)
- Or use the "Contact Us" form on developers portal
- Reference your application/company name

**Sample Follow-Up Email:**
```
Subject: Developer Portal Registration Follow-Up

Hello Nextech Developer Team,

I submitted a developer portal registration for [Your Company Name] on [Date]
to integrate with Nextech's FHIR API for medical spa appointment booking.

Could you please provide an update on the status of my application?
I'm eager to begin testing the integration.

Application Details:
- Company: [Your Company]
- Email: [Your Email]
- Use Case: AI-powered appointment booking platform

Thank you,
[Your Name]
```

---

## Step 4: Receive Your Credentials

Once approved, you'll receive:

### **OAuth 2.0 Credentials:**

```
Client ID: abc123def456 (example)
Client Secret: secret_xyz789 (example - keep this secure!)

Sandbox API Base URL: https://api-sandbox.nextech.com
Token Endpoint: https://api-sandbox.nextech.com/connect/token

Scopes Available:
- patient/*.read
- patient/*.write
- appointment/*.read
- appointment/*.write
- slot/*.read
- schedule/*.read
```

### **Important Security Notes:**

⚠️ **Never commit credentials to Git:**
```bash
# Add to .gitignore (should already be there)
.env
.env.local
*.secret
```

⚠️ **Store in environment variables only:**
```bash
# .env (never commit this file)
NEXTECH_BASE_URL=https://api-sandbox.nextech.com
NEXTECH_CLIENT_ID=your-actual-client-id
NEXTECH_CLIENT_SECRET=your-actual-client-secret
```

⚠️ **Rotate secrets regularly:**
- Change credentials every 90 days
- Immediately rotate if compromised
- Use different credentials for dev/staging/production

---

## Step 5: Configure Your Application

Once you have credentials:

### **1. Update Environment Variables:**

```bash
# Copy the bootstrap example
cp .env.bootstrap.example .env

# Edit .env and update these lines:
NEXTECH_BASE_URL=https://api-sandbox.nextech.com  # Exact URL from Nextech
NEXTECH_CLIENT_ID=your-client-id-from-email
NEXTECH_CLIENT_SECRET=your-client-secret-from-email
```

### **2. Verify Configuration:**

Create a quick test script:

```bash
cat > scripts/test-nextech-auth.sh << 'EOF'
#!/bin/bash
set -e

echo "Testing Nextech OAuth Authentication..."
echo ""

# Load environment variables
source .env

# Request OAuth token
TOKEN_RESPONSE=$(curl -s -X POST "$NEXTECH_BASE_URL/connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=$NEXTECH_CLIENT_ID" \
  -d "client_secret=$NEXTECH_CLIENT_SECRET" \
  -d "scope=patient/*.read appointment/*.read slot/*.read")

echo "Response:"
echo "$TOKEN_RESPONSE" | jq .

# Check if token was received
if echo "$TOKEN_RESPONSE" | jq -e '.access_token' > /dev/null; then
    echo ""
    echo "✅ SUCCESS! OAuth authentication working."
    echo "Access token received (first 20 chars):"
    echo "$TOKEN_RESPONSE" | jq -r '.access_token' | cut -c1-20
else
    echo ""
    echo "❌ FAILED! Check your credentials."
    exit 1
fi
EOF

chmod +x scripts/test-nextech-auth.sh
./scripts/test-nextech-auth.sh
```

### **3. Run Integration Test:**

```bash
# Test the Nextech client
go test ./internal/emr/nextech -v

# If you have sandbox access, test a real API call
go run scripts/test-nextech-integration.go
```

---

## Step 6: Test API Access

Once authenticated, try a simple API call:

### **Test Patient Search:**

```bash
# Get OAuth token first
TOKEN=$(curl -s -X POST "$NEXTECH_BASE_URL/connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=$NEXTECH_CLIENT_ID" \
  -d "client_secret=$NEXTECH_CLIENT_SECRET" \
  -d "scope=patient/*.read" \
  | jq -r '.access_token')

# Search for a test patient
curl -X GET "$NEXTECH_BASE_URL/Patient?name=test" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/fhir+json" \
  | jq .
```

### **Expected Response (Success):**

```json
{
  "resourceType": "Bundle",
  "type": "searchset",
  "total": 0,
  "entry": []
}
```

This empty response is OK for sandbox - it means authentication works!

### **Common Errors:**

**401 Unauthorized:**
```json
{
  "error": "invalid_client"
}
```
**Fix:** Check your Client ID and Client Secret

**403 Forbidden:**
```json
{
  "error": "insufficient_scope"
}
```
**Fix:** Request missing scopes in token request

**404 Not Found:**
```
Cannot GET /Patient
```
**Fix:** Check API base URL is correct

---

## Step 7: Document Your Setup

Create a record of your configuration:

```bash
# Create a setup log (DO NOT commit this!)
cat > .nextech-setup.log << EOF
Nextech Developer Portal Setup
===============================

Registration Date: $(date)
Approval Date: [Fill in when approved]

Credentials:
- Client ID: [First 8 chars only for reference]
- Sandbox URL: $NEXTECH_BASE_URL

Test Results:
- OAuth Authentication: [PASS/FAIL]
- Patient API: [PASS/FAIL]
- Appointment API: [PASS/FAIL]
- Slot API: [PASS/FAIL]

Next Steps:
1. [ ] Integrate into conversation service
2. [ ] Test end-to-end booking flow
3. [ ] Request production credentials
4. [ ] Go live with first client

Notes:
[Add any important notes about the setup process]

EOF

# Add to .gitignore
echo ".nextech-setup.log" >> .gitignore
```

---

## Troubleshooting Common Issues

### **"Application Pending" for > 5 Days**

**Action:** Email Nextech support with reference number

### **"Invalid Scope" Errors**

**Check:**
- Scopes match what you registered for
- Using correct scope format: `patient/*.read` not `patient:read`

### **"Rate Limited" (429 Errors)**

**Nextech Limit:** 20 requests/second per endpoint

**Solution:** Add rate limiting to your client:
```go
import "golang.org/x/time/rate"

limiter := rate.NewLimiter(rate.Limit(20), 1)
limiter.Wait(ctx) // Before each API call
```

### **"Sandbox Data Not Available"**

Some sandboxes have limited test data. You may need to:
1. Create test patients manually in Nextech sandbox UI
2. Request test data from Nextech support
3. Use mock data for initial development

---

## Next Steps After Registration

Once you have working credentials:

✅ **1. Update docs/MVP_STATUS.md** - Mark EMR integration as "in testing"
✅ **2. Wire into conversation service** - Connect availability queries to AI flow
✅ **3. Test end-to-end** - Full booking flow with sandbox
✅ **4. Document for first client** - Create onboarding guide
✅ **5. Request production access** - After sandbox testing complete

---

## Production Migration Checklist

Before requesting production credentials:

- [ ] All sandbox tests passing
- [ ] Error handling implemented
- [ ] Rate limiting added
- [ ] Logging and monitoring set up
- [ ] Security audit completed (credential management)
- [ ] HIPAA compliance verified
- [ ] First client ready to onboard
- [ ] Runbook created for common issues

---

## Support Resources

**Nextech Developer Documentation:**
- Main Docs: [https://nextechsystems.github.io/selectapidocspub/](https://nextechsystems.github.io/selectapidocspub/)
- FHIR Spec: [http://hl7.org/fhir/STU3/](http://hl7.org/fhir/STU3/)
- GitHub: [https://github.com/NextechSystems](https://github.com/NextechSystems)

**Contact:**
- Developer Portal: Contact form on website
- General Support: Check Nextech website for current contact info

---

**Last Updated:** 2025-01-14
**Status:** Ready for registration
