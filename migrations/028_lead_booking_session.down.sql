-- Remove booking session fields from leads table
ALTER TABLE leads DROP COLUMN IF EXISTS booking_session_id;
ALTER TABLE leads DROP COLUMN IF EXISTS booking_platform;
ALTER TABLE leads DROP COLUMN IF EXISTS booking_outcome;
ALTER TABLE leads DROP COLUMN IF EXISTS booking_confirmation_number;
ALTER TABLE leads DROP COLUMN IF EXISTS booking_handoff_url;
ALTER TABLE leads DROP COLUMN IF EXISTS booking_handoff_sent_at;
ALTER TABLE leads DROP COLUMN IF EXISTS booking_completed_at;
