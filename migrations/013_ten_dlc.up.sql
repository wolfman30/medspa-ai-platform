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
