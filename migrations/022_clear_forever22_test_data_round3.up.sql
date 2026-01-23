-- Clear test conversation and deposit data for Forever 22 Med Spa (round 3)
-- Phone numbers to clear:
--   +1 (330) 333-9270 = 3303339270
--   +1 (330) 805-9026 = 3308059026
--   +1 (330) 333-2654 = 3303332654
--   +1 (937) 896-2713 = 9378962713

-- Forever 22 org ID: d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599

-- 1. Delete conversation_messages (references conversations.conversation_id)
DELETE FROM conversation_messages
WHERE conversation_id IN (
  SELECT conversation_id FROM conversations
  WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
    AND (phone LIKE '%3303339270%' OR phone LIKE '%3308059026%' OR phone LIKE '%3303332654%' OR phone LIKE '%9378962713%')
);

-- 2. Delete conversations (references leads.id via lead_id)
DELETE FROM conversations
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
  AND (phone LIKE '%3303339270%' OR phone LIKE '%3308059026%' OR phone LIKE '%3303332654%' OR phone LIKE '%9378962713%');

-- 3. Delete bookings associated with these phone numbers (via lead_id)
DELETE FROM bookings
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
  AND lead_id IN (
    SELECT id FROM leads
    WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
      AND (phone LIKE '%3303339270%' OR phone LIKE '%3308059026%' OR phone LIKE '%3303332654%' OR phone LIKE '%9378962713%')
  );

-- 4. Delete payments associated with these phone numbers (via lead_id)
DELETE FROM payments
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
  AND lead_id IN (
    SELECT id FROM leads
    WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
      AND (phone LIKE '%3303339270%' OR phone LIKE '%3308059026%' OR phone LIKE '%3303332654%' OR phone LIKE '%9378962713%')
  );

-- 5. Delete leads associated with these phone numbers
DELETE FROM leads
WHERE org_id = 'd0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599'
  AND (phone LIKE '%3303339270%' OR phone LIKE '%3308059026%' OR phone LIKE '%3303332654%' OR phone LIKE '%9378962713%');

-- 6. Delete conversation jobs associated with these phone numbers
DELETE FROM conversation_jobs
WHERE conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%3303339270%'
   OR conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%3308059026%'
   OR conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%3303332654%'
   OR conversation_id LIKE '%d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599%9378962713%';
