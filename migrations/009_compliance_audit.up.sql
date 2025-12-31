-- Compliance audit events table for liability protection
-- This table is append-only and should never be modified or deleted
CREATE TABLE IF NOT EXISTS compliance_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type VARCHAR(100) NOT NULL,
    org_id UUID NOT NULL,
    conversation_id VARCHAR(255),
    lead_id UUID,
    user_message TEXT,
    ai_response TEXT,
    details JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for querying by organization
CREATE INDEX idx_compliance_audit_org_id ON compliance_audit_events(org_id);

-- Index for querying by conversation
CREATE INDEX idx_compliance_audit_conversation ON compliance_audit_events(conversation_id) WHERE conversation_id IS NOT NULL;

-- Index for querying by event type
CREATE INDEX idx_compliance_audit_event_type ON compliance_audit_events(event_type);

-- Index for time-based queries (for legal discovery)
CREATE INDEX idx_compliance_audit_created_at ON compliance_audit_events(created_at);

-- Composite index for common query pattern
CREATE INDEX idx_compliance_audit_org_time ON compliance_audit_events(org_id, created_at DESC);

-- Add comment explaining retention policy
COMMENT ON TABLE compliance_audit_events IS 'Immutable compliance audit log. Retain for 7 years per HIPAA requirements. Do not DELETE or UPDATE records.';
