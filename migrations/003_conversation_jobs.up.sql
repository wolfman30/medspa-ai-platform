CREATE TABLE IF NOT EXISTS conversation_jobs (
    job_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    request_type TEXT NOT NULL,
    conversation_id TEXT,
    start_request JSONB,
    message_request JSONB,
    response JSONB,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS conversation_jobs_status_idx ON conversation_jobs (status);
CREATE INDEX IF NOT EXISTS conversation_jobs_expires_idx ON conversation_jobs (expires_at);
