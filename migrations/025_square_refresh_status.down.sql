ALTER TABLE clinic_square_credentials
    DROP COLUMN IF EXISTS last_refresh_error,
    DROP COLUMN IF EXISTS last_refresh_failure_at,
    DROP COLUMN IF EXISTS last_refresh_attempt_at;
