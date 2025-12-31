-- Align payment_disputes.org_id with text-based org IDs used elsewhere.
ALTER TABLE payment_disputes
    DROP CONSTRAINT IF EXISTS payment_disputes_org_id_fkey;

ALTER TABLE payment_disputes
    ALTER COLUMN org_id TYPE TEXT
    USING org_id::text;
