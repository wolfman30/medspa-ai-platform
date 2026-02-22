CREATE TABLE IF NOT EXISTS subscriptions (
    id              BIGSERIAL PRIMARY KEY,
    stripe_customer_id     TEXT NOT NULL,
    stripe_subscription_id TEXT NOT NULL UNIQUE,
    email           TEXT,
    customer_name   TEXT,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subscriptions_customer ON subscriptions (stripe_customer_id);
CREATE INDEX idx_subscriptions_status ON subscriptions (status);
