# Booking Session Manager Design

## Overview

The Booking Session Manager handles the "automate then handoff" flow for Moxie bookings:

1. **Automate Steps 1-4** - Service, provider, date/time, contact details
2. **Handoff at Step 5** - Give lead the payment page URL
3. **Monitor for Outcome** - Detect if booking succeeded or failed

## Key Insight: Moxie Handles Payments (No Square)

When a clinic's `booking_platform` is set to `moxie`, **Square is never used**:
- **No Square checkout links** — all payment happens inside Moxie
- **We automate Steps 1-4** (service, provider, date/time, contact info) via browser
- **We hand off at Step 5** — the patient receives a link to Moxie's payment page where they enter their card details and click to finalize the booking
- **We detect the outcome** — the sidecar monitors the page and reports success/failure back to the platform

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         BOOKING SESSION LIFECYCLE                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  State: CREATED                                                              │
│  ┌────────────────────────────────────────────────────────────────┐        │
│  │ POST /api/v1/booking/start                                      │        │
│  │ {bookingUrl, date, time, lead: {name, phone, email}, service}  │        │
│  └────────────────────────────────────────────────────────────────┘        │
│                              │                                              │
│                              ▼                                              │
│  State: NAVIGATING (Steps 1-4)                                             │
│  ┌────────────────────────────────────────────────────────────────┐        │
│  │ Browser automates:                                              │        │
│  │ • Step 1: Select service                                        │        │
│  │ • Step 2: Select provider                                       │        │
│  │ • Step 3: Select date & time                                    │        │
│  │ • Step 4: Fill contact details (name, phone, email)             │        │
│  └────────────────────────────────────────────────────────────────┘        │
│                              │                                              │
│                              ▼                                              │
│  State: READY_FOR_HANDOFF                                                   │
│  ┌────────────────────────────────────────────────────────────────┐        │
│  │ GET /api/v1/booking/:sessionId/handoff-url                      │        │
│  │ Returns: { handoffUrl, expiresAt }                              │        │
│  │ → Send this URL to lead via SMS                                 │        │
│  └────────────────────────────────────────────────────────────────┘        │
│                              │                                              │
│                              ▼                                              │
│  State: MONITORING                                                          │
│  ┌────────────────────────────────────────────────────────────────┐        │
│  │ Browser watches for:                                            │        │
│  │ • URL change to confirmation/success page                       │        │
│  │ • Text: "Booking Confirmed", "Appointment Scheduled"            │        │
│  │ • Error: "Payment Failed", "Card Declined"                      │        │
│  │ • Timeout: 10 minutes of inactivity → ABANDONED                 │        │
│  └────────────────────────────────────────────────────────────────┘        │
│                              │                                              │
│                              ▼                                              │
│  State: COMPLETED / FAILED / ABANDONED                                      │
│  ┌────────────────────────────────────────────────────────────────┐        │
│  │ GET /api/v1/booking/:sessionId/status                           │        │
│  │ Returns: { state, outcome, confirmationDetails? }               │        │
│  │                                                                  │        │
│  │ Webhook/callback to platform when outcome detected              │        │
│  └────────────────────────────────────────────────────────────────┘        │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## API Endpoints

### 1. Start Booking Session

```
POST /api/v1/booking/start
```

**Request:**
```typescript
{
  bookingUrl: string;         // e.g., "https://forever22medspa.com/book"
  date: string;               // "2026-02-05"
  time: string;               // "2:30pm"
  lead: {
    firstName: string;        // "John"
    lastName: string;         // "Smith"
    phone: string;            // "+14407031022"
    email: string;            // "john@example.com"
    notes?: string;           // "First time patient"
  };
  service?: string;           // "Botox" - optional, uses first available if omitted
  provider?: string;          // "Brandi" - optional, uses first available if omitted
  callbackUrl?: string;       // URL to POST outcome to when detected
}
```

**Response:**
```typescript
{
  success: boolean;
  sessionId: string;          // UUID for tracking
  state: BookingSessionState;
  error?: string;
}
```

### 2. Get Handoff URL

```
GET /api/v1/booking/:sessionId/handoff-url
```

**Response:**
```typescript
{
  success: boolean;
  sessionId: string;
  handoffUrl: string;         // URL lead opens to complete booking
  expiresAt: string;          // ISO timestamp - session expires after this
  state: BookingSessionState;
}
```

### 3. Get Session Status

```
GET /api/v1/booking/:sessionId/status
```

**Response:**
```typescript
{
  success: boolean;
  sessionId: string;
  state: BookingSessionState;
  outcome?: BookingOutcome;
  confirmationDetails?: {
    confirmationNumber?: string;
    appointmentTime?: string;
    provider?: string;
    service?: string;
  };
  error?: string;
}
```

### 4. Cancel Session

```
DELETE /api/v1/booking/:sessionId
```

---

## Data Types

```typescript
type BookingSessionState =
  | 'created'           // Session created, not started
  | 'navigating'        // Browser automating steps 1-4
  | 'ready_for_handoff' // Reached step 5, waiting for lead
  | 'monitoring'        // Lead has handoff URL, watching for outcome
  | 'completed'         // Booking succeeded
  | 'failed'            // Booking failed (payment error, etc.)
  | 'abandoned'         // Timeout - lead didn't complete
  | 'cancelled';        // Manually cancelled

type BookingOutcome =
  | 'success'           // Booking confirmed
  | 'payment_failed'    // Card declined, payment error
  | 'slot_unavailable'  // Time slot was taken
  | 'timeout'           // Lead didn't complete in time
  | 'cancelled'         // Manually cancelled
  | 'error';            // Unknown error

interface BookingSession {
  id: string;
  state: BookingSessionState;
  outcome?: BookingOutcome;

  // Request data
  bookingUrl: string;
  date: string;
  time: string;
  lead: LeadInfo;
  service?: string;
  provider?: string;

  // Handoff
  handoffUrl?: string;
  handoffExpiresAt?: Date;

  // Outcome
  confirmationDetails?: ConfirmationDetails;
  errorMessage?: string;

  // Timestamps
  createdAt: Date;
  updatedAt: Date;
  completedAt?: Date;

  // Browser context (internal)
  browserContextId?: string;
  pageUrl?: string;
}
```

---

## Outcome Detection Strategy

### What We Monitor

The sidecar keeps the browser page open and polls/listens for changes:

```typescript
interface OutcomeDetector {
  // Poll interval (e.g., every 2 seconds)
  pollInterval: number;

  // Max time to wait for outcome (e.g., 10 minutes)
  timeout: number;

  // Success indicators
  successIndicators: {
    urlPatterns: string[];     // ['/confirmation', '/success', '/thank-you']
    textPatterns: string[];    // ['Booking Confirmed', 'Appointment Scheduled']
    elementSelectors: string[]; // ['.confirmation-number', '[data-testid="success"]']
  };

  // Failure indicators
  failureIndicators: {
    textPatterns: string[];    // ['Payment Failed', 'Card Declined', 'Try Again']
    elementSelectors: string[]; // ['.error-message', '[data-testid="error"]']
  };
}
```

### Detection Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    OUTCOME DETECTION LOOP                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  while (state === 'monitoring' && !timeout) {                   │
│    1. Check current URL                                          │
│       → /confirmation, /success, /thank-you → SUCCESS           │
│       → /error, /failed → FAILURE                                │
│                                                                  │
│    2. Check page text                                            │
│       → "Booking Confirmed" → SUCCESS                            │
│       → "Payment Failed" → FAILURE                               │
│                                                                  │
│    3. Check for elements                                         │
│       → .confirmation-number exists → SUCCESS                    │
│       → .error-message exists → FAILURE                          │
│                                                                  │
│    4. Wait pollInterval (2s)                                     │
│  }                                                               │
│                                                                  │
│  if (timeout) → state = 'abandoned'                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Session Storage

For MVP, sessions are stored **in-memory** with cleanup after completion:

```typescript
class BookingSessionManager {
  private sessions: Map<string, BookingSession> = new Map();
  private pages: Map<string, Page> = new Map();  // Playwright pages

  // Session TTL: 15 minutes from creation
  private sessionTTL = 15 * 60 * 1000;

  // Cleanup completed sessions after 5 minutes
  private cleanupDelay = 5 * 60 * 1000;
}
```

**Future:** Move to Redis/database for multi-instance support.

---

## Integration with Main Platform

### Go Platform Changes

1. **Lead Model** - Add `booking_session_id` and `booking_outcome` fields
2. **Conversation Flow** - Detect Moxie clinic → skip Square payment link
3. **Browser Client** - Add methods for booking session management
4. **Webhook Handler** - Receive booking outcome callbacks

### Conversation Flow Change

```
Square-clinic flow (booking_platform=square):
1. Qualify lead (5 qualifications)
2. Select service & time preferences
3. Send Square checkout link for refundable deposit
4. Wait for Square payment webhook
5. Operator manually confirms appointment

Moxie-clinic flow (booking_platform=moxie — NO Square involved):
1. Qualify lead (6 qualifications, adds provider preference)
2. Select service & time from real Moxie availability
3. START BOOKING SESSION → sidecar auto-fills Moxie Steps 1-4
4. Send patient a link to Moxie's Step 5 payment page
   → Patient enters their card details and clicks to finalize
5. Sidecar monitors the page for outcome (success/failure/timeout)
6. Callback notifies platform → send confirmation or failure SMS
```

---

## Mock Booking Page Updates

The mock page needs to simulate Step 5 with:

1. **Payment form** - Card number, expiry, CVV (fake inputs)
2. **Book Appointment button** - Triggers success/failure
3. **Success page** - Shows confirmation number
4. **Failure simulation** - Test card numbers that fail

### Test Scenarios

| Card Number | Outcome |
|-------------|---------|
| 4111111111111111 | Success |
| 4000000000000002 | Declined |
| 4000000000000069 | Expired Card |
| Any other | Success |

---

## Implementation Order

1. **Types & Interfaces** (`types.ts`) - Add booking session types
2. **Session Manager** (`booking-session.ts`) - Core session logic
3. **API Endpoints** (`server.ts`) - Add booking endpoints
4. **Step 3-4 Automation** (`scraper.ts`) - Add phone/contact filling
5. **Outcome Detector** (`outcome-detector.ts`) - Monitor for success/failure
6. **Mock Page Update** (`mock_booking.go`) - Add payment simulation
7. **Go Client** (`browser/client.go`) - Add booking methods
8. **Tests** - Integration tests for full flow

---

## Open Questions

1. **Handoff mechanism**:
   - Option A: Give lead the current page URL (they continue our session)
   - Option B: Open a new browser tab they can interact with
   - Option C: NoVNC/browser streaming (complex)

   **Recommendation**: Option A - Session URL handoff. The lead opens the same URL and our session cookies/state are maintained.

2. **Session limit**: How many concurrent booking sessions?
   - Start with 5 concurrent sessions
   - Queue additional requests

3. **Callback vs Polling**:
   - Platform polls sidecar for status?
   - Or sidecar calls back to platform webhook?

   **Recommendation**: Both - callback for real-time + polling as fallback

---

## Next Steps

1. Review this design with stakeholder
2. Implement booking session types
3. Add Step 3-4 automation to scraper
4. Build outcome detector
5. Update mock page
6. Integration tests
