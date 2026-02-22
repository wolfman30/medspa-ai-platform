CREATE TABLE IF NOT EXISTS rebook_reminders (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      TEXT NOT NULL,
    patient_id  UUID NOT NULL,
    phone       TEXT NOT NULL DEFAULT '',
    patient_name TEXT NOT NULL DEFAULT '',
    service     TEXT NOT NULL,
    provider    TEXT NOT NULL DEFAULT '',
    booked_at   TIMESTAMPTZ NOT NULL,
    rebook_after TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    channel     TEXT NOT NULL DEFAULT 'sms',
    sent_at     TIMESTAMPTZ,
    dismissed_at TIMESTAMPTZ,
    rebooked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_rebook_reminders_due ON rebook_reminders (rebook_after, status) WHERE status = 'pending';
CREATE INDEX idx_rebook_reminders_org ON rebook_reminders (org_id, status);
CREATE INDEX idx_rebook_reminders_patient ON rebook_reminders (patient_id);
