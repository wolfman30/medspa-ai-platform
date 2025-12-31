-- Organizations table (required by foreign keys in this migration)
-- Note: This is duplicated here to avoid ordering failures when running from scratch.
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    operator_phone VARCHAR(20),
    contact_email VARCHAR(255),
    timezone VARCHAR(50) DEFAULT 'America/New_York',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Payment disputes table for tracking Square chargebacks/disputes
CREATE TABLE IF NOT EXISTS payment_disputes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id VARCHAR(255) NOT NULL UNIQUE,
    payment_id VARCHAR(255) NOT NULL,
    org_id UUID REFERENCES organizations(id),
    state VARCHAR(100) NOT NULL,
    reason VARCHAR(100),
    amount_cents INTEGER NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    due_at TIMESTAMPTZ,
    card_brand VARCHAR(50),
    customer_phone VARCHAR(20),
    customer_email VARCHAR(255),
    resolved_at TIMESTAMPTZ,
    outcome VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Dispute evidence table for chargeback defense
CREATE TABLE IF NOT EXISTS dispute_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id VARCHAR(255) NOT NULL REFERENCES payment_disputes(dispute_id),
    conversation_transcript TEXT,
    payment_confirmation TEXT,
    customer_consent TEXT,
    service_description TEXT,
    additional_notes TEXT,
    submitted_to_square BOOLEAN DEFAULT FALSE,
    submitted_at TIMESTAMPTZ,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_payment_disputes_payment_id ON payment_disputes(payment_id);
CREATE INDEX IF NOT EXISTS idx_payment_disputes_org_id ON payment_disputes(org_id);
CREATE INDEX IF NOT EXISTS idx_payment_disputes_state ON payment_disputes(state);
CREATE INDEX IF NOT EXISTS idx_payment_disputes_due_at ON payment_disputes(due_at);
CREATE INDEX IF NOT EXISTS idx_payment_disputes_created_at ON payment_disputes(created_at);
CREATE INDEX IF NOT EXISTS idx_dispute_evidence_dispute_id ON dispute_evidence(dispute_id);

-- Add comment for documentation
COMMENT ON TABLE payment_disputes IS 'Tracks Square payment disputes/chargebacks';
COMMENT ON TABLE dispute_evidence IS 'Evidence collected for dispute defense';
