# Lucy's Laser & MedSpa - Configuration Status

**Org ID:** de85b4e1-8fbb-455b-af7c-c3a5fdf4851a  
**Date Configured:** 2026-02-19  
**Status:** ✅ Demo-Ready (pending phone number)

## What Was Configured

### 1. Clinic Config (PUT /admin/clinics/{orgId}/config)
- ✅ **booking_url:** https://app.joinmoxie.com/booking/lucys-laser-medspa
- ✅ **booking_platform:** moxie
- ✅ **payment_provider:** stripe
- ✅ **moxie_config:** medspa_id=167, slug=lucys-laser-medspa, 55 service menu items, 5 providers mapped
- ✅ **services:** 55 services fully listed
- ✅ **service_aliases:** 107 aliases mapping common names → Moxie service names
- ✅ **service_price_text:** 55 services with pricing (known prices: Botox $13.50/unit, Dysport $4/unit, Lip Flip $175, Sculptra $700-$2,200, Microneedling $300/$750 for 3, Microneedling+PRP $600/$1,680 for 3, Microneedling+Ariessence $650)
- ✅ **booking_policies:** 3 policies (deposit $50, age 18+, cancellation 24hr)
- ✅ **business_hours:** Mon 7:30-5, Tue 7:30-7, Wed 7:30-4, Thu 7:30-7, Fri 7:30-3
- ✅ **ai_persona:** Provider name "Lucy", warm tone, custom greeting, after-hours greeting, special services highlighted
- ✅ **deposit_amount_cents:** 5000 ($50)
- ✅ **All confirmation flags:** clinic_info, business_hours, services, contact_info = true

### 2. Knowledge Base (PUT /admin/clinics/{orgId}/knowledge)
- ✅ **39 documents** stored covering:
  - All services with descriptions, pricing, duration, and providers
  - 6 provider bios (Lucy Sason CNP, Megan Landry RN, Sarah Jusko RN, Jess Petush, Halie Rosser, Dr. Jason Wright MD)
  - Cancellation, deposit, and age policies
  - Clinic info (location, hours, parking, contact, skincare brands)
  - FAQs (Botox vs filler, Sofwave, weight loss, HRT, payment plans)
  - Aftercare instructions

### 3. Verification
- ✅ Config GET confirmed all fields populated correctly
- ✅ Knowledge endpoint confirmed 39 documents stored

## What's Still Needed

### ⚠️ Requires Andrew:
1. **Telnyx phone number** — Need to purchase and assign a local number for inbound calls
2. **10DLC registration** — Required for SMS text-back flow
3. **moxie_dry_run flag** — This field doesn't appear in the config schema; may need to be set at system/env level to prevent real appointment creation during demos
4. **after_hours_only flag** — Same as above; may need system-level toggle for 24/7 demo mode
5. **Stripe account** — Need to connect Lucy's Stripe account (or create one) for deposit collection
6. **Notification recipients** — Email/SMS recipients not yet configured (currently disabled)

### Nice to Have:
- Service deposit amounts per service (currently using flat $50)
- More specific pricing for services marked "Contact for pricing"
- Provider headshot URLs for rich messages
