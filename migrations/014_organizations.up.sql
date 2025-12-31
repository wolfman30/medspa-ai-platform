-- Organizations table (required by foreign keys in migrations 010-012)
-- Note: This may already exist in some deployments. Using IF NOT EXISTS for safety.
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    operator_phone VARCHAR(20),
    contact_email VARCHAR(255),
    timezone VARCHAR(50) DEFAULT 'America/New_York',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for lookups
CREATE INDEX IF NOT EXISTS idx_organizations_name ON organizations(name);

-- Add comment for documentation
COMMENT ON TABLE organizations IS 'Business organizations/clinics using the platform';
