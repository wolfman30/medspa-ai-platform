-- Rollback: Restore deleted orgs and rename
-- Note: This cannot fully restore the deleted rows, only recreate empty placeholders

-- Recreate deleted organizations (with minimal data)
INSERT INTO organizations (id, name, created_at, updated_at)
VALUES
    ('8bf4137f-a44c-476e-9308-8312b0c1d968', 'AI Wolf Solutions Demo Clinic', NOW(), NOW()),
    ('934fb4b3-8fbf-4fd2-b40a-8a998aa63cbe', 'Glow Medspa', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Rename AI Wolf Solutions back to Cleveland Primecare MedSpa
UPDATE organizations
SET name = 'Cleveland Primecare MedSpa'
WHERE id = 'bb507f20-7fcc-4941-9eac-9ed93b7834ed';
