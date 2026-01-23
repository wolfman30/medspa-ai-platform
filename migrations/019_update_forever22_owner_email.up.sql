-- Update Forever 22 Med Spa owner email from andrew+f22 to brandi's actual email
UPDATE organizations
SET owner_email = 'clinic@example.com'
WHERE owner_email = 'andrew+f22@aiwolfsolutions.com';
