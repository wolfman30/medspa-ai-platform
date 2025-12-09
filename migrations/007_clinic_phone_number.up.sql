-- Add phone_number to store the clinic's SMS from number for confirmation messages
ALTER TABLE clinic_square_credentials 
ADD COLUMN IF NOT EXISTS phone_number TEXT;

COMMENT ON COLUMN clinic_square_credentials.phone_number IS 'E.164 format phone number used as the "from" number for SMS confirmations';
