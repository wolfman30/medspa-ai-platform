CREATE TABLE booking_snapshots (
    id SERIAL PRIMARY KEY,
    org_id UUID NOT NULL,
    snapshot_date DATE NOT NULL,
    service_name TEXT NOT NULL,
    service_id INT NOT NULL,
    provider_name TEXT,
    provider_id INT,
    target_date DATE NOT NULL,
    total_slots INT NOT NULL,
    available_slots INT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (org_id, service_id, provider_id, target_date, snapshot_date)
);

CREATE INDEX idx_booking_snapshots_org_date ON booking_snapshots(org_id, snapshot_date);
CREATE INDEX idx_booking_snapshots_target ON booking_snapshots(org_id, target_date);
