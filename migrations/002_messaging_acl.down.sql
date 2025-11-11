BEGIN;

CREATE TABLE processed_events_legacy (
    provider text NOT NULL,
    event_id text NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, event_id)
);

INSERT INTO processed_events_legacy (provider, event_id, processed_at)
SELECT
    COALESCE(provider, 'legacy'),
    COALESCE(external_event_id, event_id::text),
    processed_at
FROM processed_events;

DROP TABLE processed_events;

ALTER TABLE processed_events_legacy RENAME TO processed_events;

DROP INDEX IF EXISTS idx_outbox_pending;

ALTER TABLE outbox
    ALTER COLUMN aggregate DROP NOT NULL;

ALTER TABLE outbox RENAME COLUMN aggregate TO org_id;
ALTER TABLE outbox RENAME COLUMN event_type TO type;
ALTER TABLE outbox RENAME COLUMN dispatched_at TO delivered_at;

CREATE INDEX idx_outbox_pending ON outbox (created_at)
WHERE delivered_at IS NULL;

DROP TRIGGER IF EXISTS set_timestamp_hosted_number_orders ON hosted_number_orders;
DROP TRIGGER IF EXISTS set_timestamp_ten_dlc_brands ON ten_dlc_brands;
DROP TRIGGER IF EXISTS set_timestamp_ten_dlc_campaigns ON ten_dlc_campaigns;
DROP TRIGGER IF EXISTS set_timestamp_messages ON messages;
DROP TRIGGER IF EXISTS set_timestamp_unsubscribes ON unsubscribes;

DROP TABLE IF EXISTS ten_dlc_campaigns;
DROP TABLE IF EXISTS ten_dlc_brands;
DROP TABLE IF EXISTS hosted_number_orders;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS unsubscribes;

DROP FUNCTION IF EXISTS set_updated_at();

COMMIT;
