-- Cleanup duplicate organizations and rename Cleveland Primecare MedSpa
-- This migration addresses duplicate test organizations created during development

-- Delete duplicate orgs with no conversations:
-- 8bf4137f-a44c-476e-9308-8312b0c1d968: AI Wolf Solutions Demo Clinic (0 conversations)
-- 934fb4b3-8fbf-4fd2-b40a-8a998aa63cbe: Glow Medspa (0 conversations, no owner)
DELETE FROM organizations
WHERE id IN (
    '8bf4137f-a44c-476e-9308-8312b0c1d968',
    '934fb4b3-8fbf-4fd2-b40a-8a998aa63cbe'
);

-- Rename Cleveland Primecare MedSpa back to AI Wolf Solutions
-- This was the original org bb507f20-7fcc-4941-9eac-9ed93b7834ed
UPDATE organizations
SET name = 'AI Wolf Solutions'
WHERE id = 'bb507f20-7fcc-4941-9eac-9ed93b7834ed';
