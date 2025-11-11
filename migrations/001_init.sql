CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE leads (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    name text,
    email text,
    phone text,
    message text,
    source text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_leads_org_created ON leads (org_id, created_at DESC);

CREATE TABLE payments (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    lead_id uuid REFERENCES leads(id),
    provider text NOT NULL,
    provider_ref text UNIQUE,
    booking_intent_id uuid,
    amount_cents integer NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_payments_org_created ON payments (org_id, created_at DESC);
CREATE UNIQUE INDEX idx_payments_provider_ref ON payments (provider_ref);

CREATE TABLE bookings (
    id uuid PRIMARY KEY,
    org_id text NOT NULL,
    lead_id uuid REFERENCES leads(id),
    status text NOT NULL,
    confirmed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    scheduled_for timestamptz
);

CREATE INDEX idx_bookings_org_created ON bookings (org_id, created_at DESC);

CREATE TABLE processed_events (
    provider text NOT NULL,
    event_id text NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, event_id)
);

CREATE TABLE outbox (
    id uuid PRIMARY KEY,
    org_id text,
    type text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);

CREATE INDEX idx_outbox_pending ON outbox (created_at)
WHERE delivered_at IS NULL;
