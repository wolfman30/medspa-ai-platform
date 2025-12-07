-- Add scheduling preferences to leads table
ALTER TABLE leads ADD COLUMN service_interest text;
ALTER TABLE leads ADD COLUMN preferred_days text;       -- e.g., "weekdays", "weekends", "any"
ALTER TABLE leads ADD COLUMN preferred_times text;      -- e.g., "morning", "afternoon", "evening", "any"
ALTER TABLE leads ADD COLUMN scheduling_notes text;     -- free-form notes from conversation
ALTER TABLE leads ADD COLUMN deposit_status text;       -- "pending", "paid", "refunded"
ALTER TABLE leads ADD COLUMN priority_level text DEFAULT 'normal';  -- "normal", "priority" (paid deposit)
