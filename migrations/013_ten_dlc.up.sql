-- Organizations table (required by foreign keys in this migration)
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    operator_phone VARCHAR(20),
    contact_email VARCHAR(255),
    timezone VARCHAR(50) DEFAULT 'America/New_York',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 10DLC Brand registrations
CREATE TABLE IF NOT EXISTS ten_dlc_brands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id),
    telnyx_brand_id VARCHAR(255) NOT NULL UNIQUE,
    business_name VARCHAR(255) NOT NULL,
    ein VARCHAR(20),
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    verification_score INTEGER DEFAULT 0,
    rejection_reason TEXT,
    submitted_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 10DLC Campaign registrations
CREATE TABLE IF NOT EXISTS ten_dlc_campaigns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id),
    brand_id UUID NOT NULL REFERENCES ten_dlc_brands(id),
    telnyx_campaign_id VARCHAR(255) NOT NULL UNIQUE,
    use_case VARCHAR(100) NOT NULL,
    description TEXT,
    sample_messages JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    rejection_reason TEXT,
    numbers_assigned INTEGER DEFAULT 0,
    submitted_at TIMESTAMPTZ NOT NULL,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Legacy schema compatibility (pre-org_id tables)
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS org_id UUID;
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS telnyx_brand_id VARCHAR(255);
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS business_name VARCHAR(255);
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS verification_score INTEGER DEFAULT 0;
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS rejection_reason TEXT;
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS submitted_at TIMESTAMPTZ;
ALTER TABLE ten_dlc_brands ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'ten_dlc_brands' AND column_name = 'clinic_id'
    ) THEN
        EXECUTE 'UPDATE ten_dlc_brands SET org_id = clinic_id WHERE org_id IS NULL AND clinic_id IS NOT NULL';
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'ten_dlc_brands' AND column_name = 'legal_name'
    ) THEN
        EXECUTE 'UPDATE ten_dlc_brands SET business_name = legal_name WHERE business_name IS NULL AND legal_name IS NOT NULL';
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'ten_dlc_brands' AND column_name = 'brand_id'
    ) THEN
        EXECUTE 'UPDATE ten_dlc_brands SET telnyx_brand_id = brand_id WHERE telnyx_brand_id IS NULL AND brand_id IS NOT NULL';
    END IF;
END $$;

UPDATE ten_dlc_brands
SET submitted_at = created_at
WHERE submitted_at IS NULL
  AND created_at IS NOT NULL;

ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS org_id UUID;
ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS telnyx_campaign_id VARCHAR(255);
ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS rejection_reason TEXT;
ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS numbers_assigned INTEGER DEFAULT 0;
ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS submitted_at TIMESTAMPTZ;
ALTER TABLE ten_dlc_campaigns ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'ten_dlc_campaigns' AND column_name = 'campaign_id'
    ) THEN
        EXECUTE 'UPDATE ten_dlc_campaigns SET telnyx_campaign_id = campaign_id WHERE telnyx_campaign_id IS NULL AND campaign_id IS NOT NULL';
    END IF;
END $$;

UPDATE ten_dlc_campaigns c
SET org_id = b.org_id
FROM ten_dlc_brands b
WHERE c.org_id IS NULL
  AND c.brand_id = b.id
  AND b.org_id IS NOT NULL;

UPDATE ten_dlc_campaigns
SET submitted_at = created_at
WHERE submitted_at IS NULL
  AND created_at IS NOT NULL;

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_ten_dlc_brands_org_id ON ten_dlc_brands(org_id);
CREATE INDEX IF NOT EXISTS idx_ten_dlc_brands_status ON ten_dlc_brands(status);
CREATE INDEX IF NOT EXISTS idx_ten_dlc_campaigns_org_id ON ten_dlc_campaigns(org_id);
CREATE INDEX IF NOT EXISTS idx_ten_dlc_campaigns_brand_id ON ten_dlc_campaigns(brand_id);
CREATE INDEX IF NOT EXISTS idx_ten_dlc_campaigns_status ON ten_dlc_campaigns(status);

-- Add comment for documentation
COMMENT ON TABLE ten_dlc_brands IS '10DLC brand registrations for SMS compliance';
COMMENT ON TABLE ten_dlc_campaigns IS '10DLC messaging campaigns for SMS compliance';
COMMENT ON COLUMN ten_dlc_brands.status IS 'Status: PENDING, VERIFIED, REJECTED, FAILED';
COMMENT ON COLUMN ten_dlc_campaigns.status IS 'Status: PENDING, ACTIVE, REJECTED, SUSPENDED, EXPIRED';
