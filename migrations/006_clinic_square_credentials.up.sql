-- Square OAuth credentials per clinic/org
CREATE TABLE IF NOT EXISTS clinic_square_credentials (
    org_id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    token_expires_at TIMESTAMPTZ NOT NULL,
    location_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for finding tokens that need refresh
CREATE INDEX idx_square_credentials_expires_at ON clinic_square_credentials(token_expires_at);

COMMENT ON TABLE clinic_square_credentials IS 'Stores Square OAuth tokens per clinic for multi-tenant payment processing';
COMMENT ON COLUMN clinic_square_credentials.access_token IS 'Square API access token (should be encrypted at rest in production)';
COMMENT ON COLUMN clinic_square_credentials.refresh_token IS 'Square refresh token for obtaining new access tokens';
COMMENT ON COLUMN clinic_square_credentials.token_expires_at IS 'When the access token expires (Square tokens last 30 days)';
