# Structured Clinic Knowledge Schema

## Overview
Replace freeform knowledge documents with structured, section-based knowledge that auto-derives internal config (service aliases, Moxie IDs, provider counts, pricing).

## Data Model

### ClinicKnowledge (top-level, stored in Redis as JSON per org)
```json
{
  "org_id": "uuid",
  "version": 1,
  "sections": {
    "services": { ... },     // Required
    "providers": { ... },    // Required  
    "policies": { ... },     // Required
    "custom": [ ... ]        // Optional freeform docs
  },
  "updated_at": "2026-02-15T..."
}
```

### Services Section
```json
{
  "items": [
    {
      "id": "auto-uuid",
      "name": "Tox (Botox, Jeuveau, Letybo, Xeomin, Daxxify)",
      "category": "Wrinkle Relaxers",
      "price": "$12/unit",
      "price_type": "variable",  // "fixed" | "variable" | "free" | "starting_at"
      "duration_minutes": 30,
      "description": "Soften fine lines and wrinkles...",
      "provider_ids": ["33950", "38627"],
      "booking_id": "18430",        // Moxie service menu item ID
      "aliases": ["botox", "jeuveau", "xeomin", "daxxify", "letybo", "dysport", "neurotoxin", "wrinkle relaxer"],
      "deposit_amount_cents": 5000,
      "is_addon": false,
      "order": 1
    }
  ]
}
```

### Providers Section
```json
{
  "items": [
    {
      "id": "33950",
      "name": "Brandi Sesock",
      "title": "Nurse Practitioner",
      "bio": "...",
      "specialties": ["Injectables", "Laser"],
      "order": 1
    }
  ]
}
```

### Policies Section
```json
{
  "cancellation": "Late cancellations (less than 24 hours) or no-shows forfeit the $50 deposit.",
  "deposit": "A $50 refundable deposit secures your appointment and applies toward your treatment cost.",
  "age_requirement": "You must be 18+ or have a legal guardian present.",
  "terms_url": "",
  "booking_policies": [
    "A $50 refundable deposit secures your appointment...",
    "You confirm you are 18+...",
    "By paying, you agree to Forever 22 Med Spa's privacy policy..."
  ],
  "custom": []
}
```

### Custom Section
```json
[
  {
    "title": "Parking",
    "content": "Free parking available in the rear lot."
  }
]
```

## API Endpoints

### GET /admin/clinics/{orgID}/knowledge/structured
Returns the full structured knowledge.

### PUT /admin/clinics/{orgID}/knowledge/structured  
Replaces structured knowledge. Auto-derives:
- `config.Services` from service names
- `config.ServiceAliases` from service aliases
- `config.ServicePriceText` from service prices
- `config.MoxieConfig.ServiceMenuItems` from booking IDs
- `config.MoxieConfig.ServiceProviderCount` from provider_ids length
- `config.BookingPolicies` from policies.booking_policies

### POST /admin/clinics/{orgID}/knowledge/sync-moxie
Pulls services + providers from Moxie booking page, populates structured knowledge.

### Portal equivalents:
- GET/PUT /portal/orgs/{orgID}/knowledge/structured
- POST /portal/orgs/{orgID}/knowledge/sync-moxie

## Config Auto-Derivation
On every structured knowledge save:
1. Build `Services []string` from service names
2. Build `ServiceAliases map[string]string` from aliases → resolved service name
3. Build `ServicePriceText map[string]string` from prices
4. Build `MoxieConfig.ServiceMenuItems` from booking_ids
5. Build `MoxieConfig.ServiceProviderCount` from provider_ids
6. Build `BookingPolicies` from policies section
7. Also regenerate the flat knowledge documents (for RAG/LLM context)
8. Save config + knowledge atomically

## Moxie Sync
Fetches `/_next/data/{buildId}/booking/{slug}.json` and:
1. Extracts all service categories + items
2. Extracts all providers
3. Maps prices, durations, descriptions
4. Populates structured knowledge
5. Operator reviews and saves
