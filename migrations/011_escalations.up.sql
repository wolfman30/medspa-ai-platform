-- Staff escalations table for tracking issues requiring human attention
CREATE TABLE IF NOT EXISTS escalations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id),
    type VARCHAR(50) NOT NULL,
    priority VARCHAR(20) NOT NULL DEFAULT 'MEDIUM',
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    customer_phone VARCHAR(20),
    customer_name VARCHAR(255),
    lead_id UUID REFERENCES leads(id),
    conversation_id VARCHAR(255),
    payment_id VARCHAR(255),
    amount_cents INTEGER,
    description TEXT NOT NULL,
    recommended_action TEXT,
    transcript_summary TEXT,
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by VARCHAR(255),
    resolved_at TIMESTAMPTZ,
    resolved_by VARCHAR(255),
    resolution TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_escalations_org_id ON escalations(org_id);
CREATE INDEX IF NOT EXISTS idx_escalations_status ON escalations(status);
CREATE INDEX IF NOT EXISTS idx_escalations_priority ON escalations(priority);
CREATE INDEX IF NOT EXISTS idx_escalations_type ON escalations(type);
CREATE INDEX IF NOT EXISTS idx_escalations_lead_id ON escalations(lead_id);
CREATE INDEX IF NOT EXISTS idx_escalations_created_at ON escalations(created_at);
CREATE INDEX IF NOT EXISTS idx_escalations_pending_priority ON escalations(org_id, status, priority, created_at)
    WHERE status = 'PENDING';

-- Add comment for documentation
COMMENT ON TABLE escalations IS 'Staff escalations for issues requiring human attention';
COMMENT ON COLUMN escalations.type IS 'Type: COMPLAINT, DISPUTE, REFUND_REQUEST, VELOCITY_BLOCK, UNAUTHORIZED_CHARGE, MEDICAL_CONCERN, CALLBACK_OVERDUE';
COMMENT ON COLUMN escalations.priority IS 'Priority: HIGH, MEDIUM, LOW';
COMMENT ON COLUMN escalations.status IS 'Status: PENDING, ACKNOWLEDGED, IN_PROGRESS, RESOLVED, CLOSED';
