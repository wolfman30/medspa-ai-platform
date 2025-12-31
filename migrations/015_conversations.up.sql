-- Conversations table for long-term history and analytics
-- conversation_id format: "sms:{orgID}:{phone}"
CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id TEXT UNIQUE NOT NULL,
    org_id TEXT NOT NULL,
    lead_id UUID REFERENCES leads(id),
    phone TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    channel TEXT NOT NULL DEFAULT 'sms',
    message_count INTEGER NOT NULL DEFAULT 0,
    customer_message_count INTEGER NOT NULL DEFAULT 0,
    ai_message_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_message_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Messages table for persistent message history
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id TEXT NOT NULL REFERENCES conversations(conversation_id),
    role TEXT NOT NULL,  -- 'user' or 'assistant'
    content TEXT NOT NULL,
    from_phone TEXT,
    to_phone TEXT,
    provider_message_id TEXT,
    status TEXT DEFAULT 'delivered',
    error_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_conversations_org_id ON conversations(org_id);
CREATE INDEX IF NOT EXISTS idx_conversations_phone ON conversations(phone);
CREATE INDEX IF NOT EXISTS idx_conversations_lead_id ON conversations(lead_id);
CREATE INDEX IF NOT EXISTS idx_conversations_status ON conversations(status);
CREATE INDEX IF NOT EXISTS idx_conversations_started_at ON conversations(started_at);
CREATE INDEX IF NOT EXISTS idx_conversations_last_message_at ON conversations(last_message_at);
CREATE INDEX IF NOT EXISTS idx_conversations_org_started ON conversations(org_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_messages_role ON messages(role);

-- Full-text search index for message content (optional but useful for search)
CREATE INDEX IF NOT EXISTS idx_messages_content_search ON messages USING gin(to_tsvector('english', content));

-- Comments
COMMENT ON TABLE conversations IS 'Long-term conversation history for analytics and review';
COMMENT ON TABLE messages IS 'Persistent message storage for conversation history';
COMMENT ON COLUMN conversations.conversation_id IS 'Format: sms:{orgID}:{phone}';
