# Nextech Integration - Quick Start Checklist

**Goal:** Get your Nextech integration working in 30 minutes

---

## ✅ Step-by-Step Checklist

### **Phase 1: Registration (15-30 min)**

- [ ] **1.1** Open [nextech.com/developers-portal](https://www.nextech.com/developers-portal)
- [ ] **1.2** Click "Register" or "Sign Up"
- [ ] **1.3** Fill out company information:
  - Company: "MedSpa AI Booking Platform" (or your company name)
  - Email: Your business email
  - Use case: "AI-powered appointment booking for medical spas"
- [ ] **1.4** Request API scopes:
  - ✅ patient/*.read
  - ✅ patient/*.write
  - ✅ appointment/*.read
  - ✅ appointment/*.write
  - ✅ slot/*.read
- [ ] **1.5** Submit application
- [ ] **1.6** Wait for approval email (1-5 business days)

### **Phase 2: Configuration (5 min)**

Once you receive credentials:

- [ ] **2.1** Copy environment template:
  ```bash
  cp .env.bootstrap.example .env
  ```

- [ ] **2.2** Edit `.env` and update:
  ```bash
  NEXTECH_BASE_URL=https://api-sandbox.nextech.com  # From Nextech email
  NEXTECH_CLIENT_ID=your-actual-client-id            # From Nextech email
  NEXTECH_CLIENT_SECRET=your-actual-secret           # From Nextech email
  ```

- [ ] **2.3** Verify `.env` is in `.gitignore` (security check)

### **Phase 3: Testing (5 min)**

- [ ] **3.1** Test OAuth authentication:
  ```bash
  ./scripts/test-nextech-auth.sh
  ```
  **Expected:** ✅ SUCCESS! OAuth authentication working.

- [ ] **3.2** Run unit tests:
  ```bash
  go test ./internal/emr/nextech -v
  ```
  **Expected:** PASS (all tests green)

- [ ] **3.3** Test API call (if sandbox has data):
  ```bash
  ./scripts/test-nextech-patient.sh  # Optional
  ```

### **Phase 4: Integration (Optional - for later)**

- [ ] **4.1** Wire EMR client into conversation service
- [ ] **4.2** Test end-to-end booking flow
- [ ] **4.3** Request production credentials
- [ ] **4.4** Go live with first client

---

## 🚨 Troubleshooting

### **Application Still Pending After 3 Days?**

Email Nextech support:
```
Subject: Developer Portal Application Status

Hello,

I registered for API access on [DATE] for [YOUR COMPANY].
Could you provide an update on my application status?

Company: [YOUR COMPANY]
Email: [YOUR EMAIL]
Use Case: AI appointment booking for medical spas

Thank you!
```

### **"Invalid Client" Error?**

- Double-check `NEXTECH_CLIENT_ID` and `NEXTECH_CLIENT_SECRET` in `.env`
- Ensure no extra spaces or quotes
- Verify using sandbox URL if you have sandbox credentials

### **"Insufficient Scope" Error?**

- Check the scopes you requested match what you're using
- Format should be: `patient/*.read` (with asterisk)
- Re-request scopes if needed

---

## 📞 Support

**Nextech Resources:**
- Developer Portal: [nextech.com/developers-portal](https://www.nextech.com/developers-portal)
- API Docs: [nextechsystems.github.io/selectapidocspub/](https://nextechsystems.github.io/selectapidocspub/)
- GitHub: [github.com/NextechSystems](https://github.com/NextechSystems)

**Our Documentation:**
- Full Registration Guide: [NEXTECH_REGISTRATION.md](NEXTECH_REGISTRATION.md)
- Integration Details: [../internal/emr/nextech/README.md](../internal/emr/nextech/README.md)
- Package Overview: [../internal/emr/README.md](../internal/emr/README.md)

---

## ✅ Success Criteria

You're ready to move forward when:
- ✅ `./scripts/test-nextech-auth.sh` returns SUCCESS
- ✅ `go test ./internal/emr/nextech` passes all tests
- ✅ You have valid OAuth token in test output

**Next:** Integrate EMR calls into your conversation service to enable real booking!
