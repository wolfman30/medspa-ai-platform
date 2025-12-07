-- Remove scheduling preferences from leads table
ALTER TABLE leads DROP COLUMN IF EXISTS service_interest;
ALTER TABLE leads DROP COLUMN IF EXISTS preferred_days;
ALTER TABLE leads DROP COLUMN IF EXISTS preferred_times;
ALTER TABLE leads DROP COLUMN IF EXISTS scheduling_notes;
ALTER TABLE leads DROP COLUMN IF EXISTS deposit_status;
ALTER TABLE leads DROP COLUMN IF EXISTS priority_level;
