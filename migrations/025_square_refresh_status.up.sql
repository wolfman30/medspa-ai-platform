ALTER TABLE clinic_square_credentials
    ADD COLUMN IF NOT EXISTS last_refresh_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_refresh_failure_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_refresh_error TEXT;
