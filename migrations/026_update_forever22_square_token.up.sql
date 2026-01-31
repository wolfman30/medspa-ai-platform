-- Insert or update Forever 22 Med Spa Square OAuth credentials
-- The refresh token was invalid, so we're setting the access token directly
-- Using INSERT ON CONFLICT to handle both new and existing rows
INSERT INTO clinic_square_credentials (
    org_id,
    merchant_id,
    access_token,
    refresh_token,
    token_expires_at,
    location_id,
    phone_number,
    created_at,
    updated_at
) VALUES (
    'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599',
    'sandbox-merchant',
    'EAAAl6IBKyerhM9euX4MyEUdEDaItg13rqbKWv6jk5lBe0aJEkKp3KnCn2Lr4nhJ',
    '',
    '2026-02-28 00:00:00+00',
    'LCHPP6QTXY7VR',
    '+14407448197',
    NOW(),
    NOW()
)
ON CONFLICT (org_id) DO UPDATE SET
    access_token = EXCLUDED.access_token,
    token_expires_at = EXCLUDED.token_expires_at,
    location_id = EXCLUDED.location_id,
    phone_number = EXCLUDED.phone_number,
    last_refresh_failure_at = NULL,
    last_refresh_error = NULL,
    updated_at = NOW();
