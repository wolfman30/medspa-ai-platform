BEGIN;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE hosted_number_orders (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    clinic_id uuid NOT NULL,
    e164_number text NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'verifying', 'documents_submitted', 'activated', 'failed')),
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_hosted_number_orders_clinic_number ON hosted_number_orders (clinic_id, e164_number);

CREATE TRIGGER set_timestamp_hosted_number_orders
BEFORE UPDATE ON hosted_number_orders
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ten_dlc_brands (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    clinic_id uuid NOT NULL,
    legal_name text NOT NULL,
    ein text,
    website text,
    address_line1 text NOT NULL,
    address_line2 text,
    city text NOT NULL,
    state text NOT NULL,
    postal_code text NOT NULL,
    country text NOT NULL,
    contact_name text NOT NULL,
    contact_email text NOT NULL,
    contact_phone text,
    brand_id text,
    status text NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'rejected', 'suspended')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_ten_dlc_brands_brand_id ON ten_dlc_brands (brand_id) WHERE brand_id IS NOT NULL;

CREATE TRIGGER set_timestamp_ten_dlc_brands
BEFORE UPDATE ON ten_dlc_brands
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ten_dlc_campaigns (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    brand_id uuid NOT NULL REFERENCES ten_dlc_brands(id) ON DELETE CASCADE,
    use_case text NOT NULL,
    sample_messages jsonb NOT NULL DEFAULT '[]'::jsonb,
    help_message text NOT NULL,
    stop_message text NOT NULL,
    campaign_id text,
    status text NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'live', 'suspended', 'closed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_ten_dlc_campaigns_brand ON ten_dlc_campaigns (brand_id);
CREATE UNIQUE INDEX idx_ten_dlc_campaigns_campaign_id ON ten_dlc_campaigns (campaign_id) WHERE campaign_id IS NOT NULL;

CREATE TRIGGER set_timestamp_ten_dlc_campaigns
BEFORE UPDATE ON ten_dlc_campaigns
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    clinic_id uuid NOT NULL,
    from_e164 text NOT NULL,
    to_e164 text NOT NULL,
    direction text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    body text,
    mms_media jsonb NOT NULL DEFAULT '[]'::jsonb,
    provider_status text,
    delivered_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_messages_clinic_created ON messages (clinic_id, created_at DESC);
CREATE INDEX idx_messages_recipient_direction ON messages (clinic_id, to_e164, direction);

CREATE TRIGGER set_timestamp_messages
BEFORE UPDATE ON messages
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE unsubscribes (
    clinic_id uuid NOT NULL,
    recipient_e164 text NOT NULL,
    source text NOT NULL CHECK (source IN ('STOP', 'admin', 'import')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (clinic_id, recipient_e164)
);

CREATE TRIGGER set_timestamp_unsubscribes
BEFORE UPDATE ON unsubscribes
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE outbox RENAME COLUMN org_id TO aggregate;
ALTER TABLE outbox RENAME COLUMN type TO event_type;
ALTER TABLE outbox RENAME COLUMN delivered_at TO dispatched_at;

ALTER TABLE outbox
    ALTER COLUMN aggregate SET NOT NULL,
    ALTER COLUMN event_type SET NOT NULL;

DROP INDEX IF EXISTS idx_outbox_pending;
CREATE INDEX idx_outbox_pending ON outbox (created_at)
WHERE dispatched_at IS NULL;

ALTER TABLE processed_events RENAME TO processed_events_legacy;

CREATE TABLE processed_events (
    event_id uuid PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now(),
    provider text,
    external_event_id text
);

CREATE UNIQUE INDEX idx_processed_events_provider_external
ON processed_events (provider, external_event_id)
WHERE provider IS NOT NULL AND external_event_id IS NOT NULL;

INSERT INTO processed_events (event_id, processed_at, provider, external_event_id)
SELECT uuid_generate_v5('6ba7b811-9dad-11d1-80b4-00c04fd430c8'::uuid, provider || ':' || event_id), processed_at, provider, event_id
FROM processed_events_legacy;

DROP TABLE processed_events_legacy;

COMMIT;
