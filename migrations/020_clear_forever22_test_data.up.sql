-- Clear test conversation and deposit data for Forever 22 Med Spa
-- Phone numbers to clear:
--   +1 (330) 333-2654 → +15005550002
--   +1 (937) 896-2713 → +15005550001

-- Forever 22 org ID: d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599

-- Delete deposits associated with these phone numbers
DELETE FROM deposits
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
  AND (lead_phone LIKE '%3303332654%' OR lead_phone LIKE '%5005550001%');

-- Delete leads associated with these phone numbers
DELETE FROM leads
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
  AND (phone LIKE '%3303332654%' OR phone LIKE '%5005550001%');

-- Delete conversation jobs associated with these phone numbers
DELETE FROM conversation_jobs
WHERE conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%3303332654%'
   OR conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%5005550001%';

-- Delete conversations associated with these phone numbers
DELETE FROM conversations
WHERE conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%3303332654%'
   OR conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%5005550001%';
