-- Revert payment_disputes.org_id to UUID with FK.
ALTER TABLE payment_disputes
    DROP CONSTRAINT IF EXISTS payment_disputes_org_id_fkey;

ALTER TABLE payment_disputes
    ALTER COLUMN org_id TYPE UUID
    USING NULLIF(org_id, '')::uuid;

ALTER TABLE payment_disputes
    ADD CONSTRAINT payment_disputes_org_id_fkey
    FOREIGN KEY (org_id) REFERENCES organizations(id);
