-- Remove selected appointment fields from leads table
ALTER TABLE leads DROP COLUMN IF EXISTS selected_datetime;
ALTER TABLE leads DROP COLUMN IF EXISTS selected_service;
