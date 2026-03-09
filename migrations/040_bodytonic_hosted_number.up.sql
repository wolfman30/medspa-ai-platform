-- Activate BodyTonic Medspa text-back number
INSERT INTO hosted_number_orders (id, clinic_id, e164_number, status, created_at, updated_at)
VALUES (
    gen_random_uuid(),
    'd9558a2d-2110-4e26-8224-1b36cd526e14',
    '+12164900303',
    'activated',
    NOW(),
    NOW()
)
ON CONFLICT (clinic_id, e164_number) DO UPDATE SET
    status = 'activated',
    updated_at = NOW();
