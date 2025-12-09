-- Add patient_type to leads table for new vs existing patient qualification
ALTER TABLE leads ADD COLUMN patient_type text;  -- "new", "existing"
