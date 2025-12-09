-- Remove patient_type column from leads table
ALTER TABLE leads DROP COLUMN IF EXISTS patient_type;
