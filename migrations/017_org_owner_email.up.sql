-- Add owner_email to organizations for user-org linking
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS owner_email VARCHAR(255);

-- Create unique index for owner lookup (one org per email for now)
CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_owner_email ON organizations(owner_email) WHERE owner_email IS NOT NULL;

-- Comment
COMMENT ON COLUMN organizations.owner_email IS 'Email of the organization owner (for login/portal access)';
