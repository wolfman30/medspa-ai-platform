# Adela Medical Spa - Configuration Status

**Org ID:** 4440091b-b73f-49fa-87a2-ae22d0110981  
**Configured:** 2026-02-19  
**Status:** ✅ Demo-Ready (pending phone number)

## What Was Configured

### 1. Clinic Config (PUT /admin/clinics/{orgId}/config)
- ✅ **booking_url:** https://app.joinmoxie.com/booking/adela-medical-spa
- ✅ **booking_platform:** moxie
- ✅ **payment_provider:** stripe
- ✅ **deposit_amount_cents:** 5000 ($50)
- ✅ **Moxie medspa_id:** 1349
- ✅ **Moxie medspa_slug:** adela-medical-spa
- ✅ **service_aliases:** 100 aliases mapping common names → Moxie service names
- ✅ **service_price_text:** 71 services with pricing (laser hair: $125-$700, tox: $4/unit, fillers: $500+/syringe, most others: "Contact for pricing")
- ✅ **booking_policies:** 3 standard policies (deposit, age 18+, cancellation/terms)
- ✅ **ai_persona:** Name "Adela", warm tone, custom greeting + after-hours greeting + busy message
- ✅ **Moxie service_menu_items:** 71 services mapped to Moxie IDs
- ✅ **Moxie provider_names:** 11 providers mapped to Moxie IDs
- ✅ **Moxie service_provider_count:** 71 services with provider counts

### 2. Knowledge Base (PUT /admin/clinics/{orgId}/knowledge)
- ✅ **48 knowledge documents** covering:
  - All major service categories with descriptions, pricing, duration, providers, and aliases
  - 8 provider bios (Brady Steineck, Tiffany Steineck, McKenna Zehnder, Amy Petrillo, Demetria Soles, Tannah Hada, Angela Solenthaler & Brandy Roberts combined)
  - Cancellation policy, deposit policy, age requirement
  - Morpheus8 competitive differentiator (only provider in area)
  - Ora Doro satellite location info
  - Full "About" section with address, hours, contact, email

### 3. Service Categories Covered
- **Injectables:** Dysport, $9 Tox Offer, Lip Flip, Restylane Fillers, Filler Dissolve
- **Advanced Consultations:** Morpheus8, Lumecca/IPL, Sculptra, Body FX, Chemical Peel
- **Laser:** Hair Reduction (XS-Full Body, $125-$700), Forma skin tightening
- **Skincare:** Microneedling, ZO Facials, Dermaplaning, Nanoneedling, Acne Facial, Peels
- **Lashes/Brows:** Full sets (classic/hybrid/volume), fills (1-4 week), lifts, lamination
- **Makeup:** Traditional, Glamour, Bridal (spa & venue), Lessons
- **Wellness:** Vitamin Injections, Weight Loss (GLP-1), Hormone Therapy (women & men)
- **Other:** Permanent Jewelry, Waxing, Extractions
- **Ora Doro Satellite:** Dysport & Restylane at second location

## ⚠️ Still Needed (Requires Andrew)

1. **Telnyx Phone Number** — Need to purchase and assign a local number for Adela
2. **10DLC Registration** — Required for SMS text-back flow
3. **moxie_dry_run / after_hours_only** — These fields don't exist in the current API schema. May need backend update to support them, OR they may be configured elsewhere. **CRITICAL: Ensure dry_run is enabled before any live demo to prevent real appointment creation.**
4. **Stripe Account** — No stripe_account_id set yet (needed for deposit collection)
5. **Notification Recipients** — Email/SMS notification recipients not configured (need clinic owner's preferred contacts)
6. **Business Hours** — Currently set to Mon-Thu 9-6, Fri 9-5 but not confirmed by owner

## Notes
- 11 providers, 71 Moxie services — largest/most complex clinic configured so far
- Ora Doro satellite location adds booking complexity (separate service entries for injectables there)
- $9 Tox Offer is a promotional service — may need special handling in conversations
- Laser hair reduction has explicit pricing ($125-$700) from research
- Most other services are "Contact for pricing" — typical for med spas
