-- Diagnostic queries for conversation viewer
-- Run against the database to check configuration

-- 1. Check all phone number mappings
SELECT
    ho.e164_number,
    ho.status,
    ho.clinic_id,
    o.name as org_name,
    ho.created_at
FROM hosted_number_orders ho
LEFT JOIN organizations o ON o.id = ho.clinic_id
ORDER BY ho.created_at DESC;

-- 2. Check if +13304600937 (Wolf Aesthetics phone) is mapped
SELECT
    ho.e164_number,
    ho.status,
    ho.clinic_id,
    o.name as org_name
FROM hosted_number_orders ho
LEFT JOIN organizations o ON o.id = ho.clinic_id
WHERE ho.e164_number IN ('+13304600937', '13304600937', '3304600937');

-- 3. Check all conversation jobs
SELECT
    conversation_id,
    status,
    request_type,
    created_at
FROM conversation_jobs
ORDER BY created_at DESC
LIMIT 20;

-- 4. Check conversation jobs for a specific phone (test patient 937-896-2713)
SELECT
    conversation_id,
    status,
    request_type,
    created_at
FROM conversation_jobs
WHERE conversation_id LIKE '%19378962713%'
   OR conversation_id LIKE '%9378962713%'
ORDER BY created_at DESC;

-- 5. List all organizations
SELECT id, name, operator_phone, timezone, created_at
FROM organizations
ORDER BY created_at DESC;

-- 6. Check clinic_square_credentials (phone number config)
SELECT org_id, merchant_id, phone_number, created_at
FROM clinic_square_credentials
ORDER BY created_at DESC;
