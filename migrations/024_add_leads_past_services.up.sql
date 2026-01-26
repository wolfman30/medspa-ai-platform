-- Add past_services column to leads table for tracking services existing patients received before
ALTER TABLE leads ADD COLUMN IF NOT EXISTS past_services TEXT;
