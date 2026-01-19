-- Remove owner_email column
DROP INDEX IF EXISTS idx_organizations_owner_email;
ALTER TABLE organizations DROP COLUMN IF EXISTS owner_email;
