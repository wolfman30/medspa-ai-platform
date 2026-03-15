-- Instagram page → org mapping for multi-tenant routing
CREATE TABLE IF NOT EXISTS instagram_page_mappings (
    page_id    TEXT PRIMARY KEY,
    org_id     UUID NOT NULL REFERENCES organizations(id),
    page_name  TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Cross-channel patient identity linking (Instagram ↔ SMS)
CREATE TABLE IF NOT EXISTS patient_instagram_identities (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instagram_scoped_id TEXT NOT NULL,
    org_id              UUID NOT NULL,
    phone               TEXT,
    patient_name        TEXT,
    linked_at           TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(instagram_scoped_id, org_id)
);
