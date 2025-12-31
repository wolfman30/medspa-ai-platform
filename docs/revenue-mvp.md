# Revenue MVP – SMS-Only AI Receptionist (No EMR Write)

> **Goal:** Ship a version of the platform that justifies real subscription revenue **without** full EMR integration, by converting missed calls and inbound texts into **qualified, deposit-backed leads** via SMS. :contentReference[oaicite:0]{index=0}  

---

## 1. Overview

The **Revenue MVP** is an SMS-only AI receptionist that runs on a clinic’s **existing main phone number** (via hosted SMS) and:

- Responds automatically to **after-hours missed calls** with an SMS.
- Holds a multi-turn conversation to **qualify** the lead (service, timing, new vs existing patient).
- Sends a **Square-hosted deposit link** using the clinic’s own Square account (per-org OAuth).
- Logs the lead, conversation, and deposit in our system.
- Sends a confirmation SMS once payment is confirmed.

There is **no direct EMR booking write** in this phase. Staff still finalize appointments inside the EMR/scheduler.

---

## 2. Scope (In vs Out)

### In Scope (Revenue MVP)

- **Channel**
  - SMS only:
    - Incoming SMS to clinic’s existing number.
    - After-hours missed call → outbound SMS.

- **Conversation**
  - AI qualifies lead:
    - New vs existing patient.
    - Service interest (Botox, filler, facial, etc.).
    - Preferred day/time windows.
  - AI explains deposit policy using clinic-configured rules.
  - AI deflects medical questions: “I can only help with booking; for medical advice please contact your provider.”

- **Payments**
  - Per-org **Square OAuth** connection:
    - Store Square merchant/token info per `org_id`.
  - Generate **Square Checkout** links per deposit request:
    - Hosted payment pages (no card data in our system).
  - Handle Square webhooks:
    - Mark deposit as `paid` or `failed`.
  - Confirmation SMS on successful payment.

- **Data & Multi-Tenancy**
  - All operations keyed by `org_id` / `clinic_id`.
  - Store:
    - Leads
    - Conversation transcripts (or message log)
    - Deposit requests + payment state
  - Minimal metrics for each clinic:
    - `conversations_started`
    - `deposits_requested`
    - `deposits_paid`
    - `deposit_amount_total`

- **Clinic Onboarding (Paid Access Portal)**
  - Registration + login for clinic admins.
  - Clinic profile setup (hours, services, deposit rules).
  - Square OAuth connect + status checks.
  - Telnyx hosted messaging + A2P 10DLC onboarding.
  - Onboarding status + go-live checklist.

- **Compliance Guardrails**
  - TCPA/A2P basics:
    - STOP/HELP handling.
    - Quiet hours based on clinic time zone.
  - Medical liability:
    - System prompt + simple classifier to avoid medical advice.
  - PCI:
    - Only Square-hosted checkout links; no PAN/PCI data stored.

---

### Explicitly Out of Scope (First 14 Days)

- ❌ Direct **EMR calendar writes** (no automatic booking in Aesthetic Record / Boulevard / Nextech, etc.).
- ❌ Real-time EMR **availability lookups** during the conversation.
- ❌ Additional channels:
  - Instagram DMs
  - Google Business Messages
  - Web chat widget
- ❌ Voice AI (no live call handling).
- ❌ Complex products:
  - Membership logic
  - Packages
  - Subscriptions
- ❌ Multi-location routing for a single phone number (one primary clinic per number in MVP).

---

## 3. Core Flows

### 3.1 After-Hours Missed Call → Deposit

```mermaid
sequenceDiagram
    participant Patient
    participant Telco as Clinic Number
    participant API as AI Wolf Backend
    participant LLM as LLM Service
    participant Square as Square (Clinic Account)

    Patient->>Telco: Calls clinic (after-hours)
    Telco->>API: Missed call event (clinic_number, caller_number)
    API->>API: Resolve org/clinic by clinic_number
    API->>Patient: SMS "Sorry we missed your call, I can help by text..."

    Patient->>API: SMS describing need
    API->>LLM: Conversation context + message
    LLM-->>API: Next reply + updated state (service, timing, etc.)
    API->>Patient: SMS reply

    loop Qualify
        Patient->>API: Answers questions
        API->>LLM: Updated context + message
        LLM-->>API: Next reply + updated state
        API->>Patient: SMS reply
    end

    API->>Square: Create checkout link (org's Square credentials)
    Square-->>API: Hosted payment URL
    API->>Patient: SMS deposit link

    Patient->>Square: Pays deposit
    Square-->>API: Payment webhook (success)
    API->>API: Mark deposit_paid + record event
    API->>Patient: SMS confirmation
