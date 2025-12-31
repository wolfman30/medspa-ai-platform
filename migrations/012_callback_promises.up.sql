-- Callback promises table for SLA tracking
CREATE TABLE IF NOT EXISTS callback_promises (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id),
    lead_id UUID REFERENCES leads(id),
    conversation_id VARCHAR(255),
    customer_phone VARCHAR(20) NOT NULL,
    customer_name VARCHAR(255),
    type VARCHAR(50) NOT NULL DEFAULT 'CALLBACK',
    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    promise_text TEXT NOT NULL,
    due_at TIMESTAMPTZ NOT NULL,
    remind_at TIMESTAMPTZ NOT NULL,
    reminder_sent BOOLEAN DEFAULT FALSE,
    fulfilled_at TIMESTAMPTZ,
    fulfilled_by VARCHAR(255),
    fulfillment_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_callback_promises_org_id ON callback_promises(org_id);
CREATE INDEX IF NOT EXISTS idx_callback_promises_lead_id ON callback_promises(lead_id);
CREATE INDEX IF NOT EXISTS idx_callback_promises_status ON callback_promises(status);
CREATE INDEX IF NOT EXISTS idx_callback_promises_due_at ON callback_promises(due_at);
CREATE INDEX IF NOT EXISTS idx_callback_promises_remind_at ON callback_promises(remind_at) WHERE reminder_sent = false;
CREATE INDEX IF NOT EXISTS idx_callback_promises_pending_due ON callback_promises(org_id, due_at)
    WHERE status IN ('PENDING', 'REMINDED');

-- Add comment for documentation
COMMENT ON TABLE callback_promises IS 'Tracks callback promises made to customers for SLA compliance';
COMMENT ON COLUMN callback_promises.type IS 'Type: CALLBACK, FOLLOW_UP, INFORMATION, APPOINTMENT, REFUND_STATUS';
COMMENT ON COLUMN callback_promises.status IS 'Status: PENDING, REMINDED, FULFILLED, EXPIRED, CANCELLED';
