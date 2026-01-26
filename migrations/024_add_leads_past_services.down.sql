-- Remove past_services column from leads table
ALTER TABLE leads DROP COLUMN IF EXISTS past_services;
