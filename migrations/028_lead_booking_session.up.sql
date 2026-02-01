-- Add booking session fields to leads table for Moxie-based booking flow
ALTER TABLE leads ADD COLUMN booking_session_id text;              -- Sidecar booking session ID
ALTER TABLE leads ADD COLUMN booking_platform text;                 -- "moxie" or "square"
ALTER TABLE leads ADD COLUMN booking_outcome text;                  -- "success", "payment_failed", "timeout", "cancelled", "error"
ALTER TABLE leads ADD COLUMN booking_confirmation_number text;      -- Confirmation number from Moxie
ALTER TABLE leads ADD COLUMN booking_handoff_url text;              -- URL sent to lead for Step 5
ALTER TABLE leads ADD COLUMN booking_handoff_sent_at timestamptz;   -- When the handoff URL was sent
ALTER TABLE leads ADD COLUMN booking_completed_at timestamptz;      -- When booking was completed/failed
