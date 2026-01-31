-- Update Forever 22 Med Spa Square OAuth access token and location ID
-- The refresh token was invalid, so we're updating with the new access token directly
UPDATE clinic_square_credentials
SET
    access_token = 'EAAAl6IBKyerhM9euX4MyEUdEDaItg13rqbKWv6jk5lBe0aJEkKp3KnCn2Lr4nhJ',
    token_expires_at = '2026-02-28 00:00:00+00',
    location_id = 'LCHPP6QTXY7VR',
    last_refresh_failure_at = NULL,
    last_refresh_error = NULL,
    updated_at = NOW()
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599';
