-- Rollback: Restore andrew+f22 as the owner email
UPDATE organizations
SET owner_email = 'andrew+f22@aiwolfsolutions.com'
WHERE owner_email = 'clinic@example.com';
