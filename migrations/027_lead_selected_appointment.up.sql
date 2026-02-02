-- Add selected appointment fields to leads table for time-selection booking flow
ALTER TABLE leads ADD COLUMN selected_datetime timestamptz;     -- The specific date/time the lead selected
ALTER TABLE leads ADD COLUMN selected_service text;             -- The specific service selected for booking
