CREATE TABLE IF NOT EXISTS prospects (
    id              TEXT PRIMARY KEY,
    clinic_name     TEXT NOT NULL,
    owner_name      TEXT NOT NULL DEFAULT '',
    owner_title     TEXT NOT NULL DEFAULT '',
    location        TEXT NOT NULL DEFAULT '',
    phone           TEXT NOT NULL DEFAULT '',
    email           TEXT NOT NULL DEFAULT '',
    website         TEXT NOT NULL DEFAULT '',
    emr             TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'identified',
    configured      BOOLEAN NOT NULL DEFAULT FALSE,
    telnyx_number   TEXT NOT NULL DEFAULT '',
    ten_dlc         BOOLEAN NOT NULL DEFAULT FALSE,
    sms_working     BOOLEAN NOT NULL DEFAULT FALSE,
    org_id          TEXT NOT NULL DEFAULT '',
    services_count  INTEGER NOT NULL DEFAULT 0,
    providers       TEXT[] NOT NULL DEFAULT '{}',
    next_action     TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS prospect_events (
    id          SERIAL PRIMARY KEY,
    prospect_id TEXT NOT NULL REFERENCES prospects(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    event_date  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_prospect_events_prospect ON prospect_events(prospect_id, event_date DESC);
