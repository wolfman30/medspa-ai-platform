CREATE TABLE IF NOT EXISTS morning_briefs (
    id         SERIAL PRIMARY KEY,
    date       DATE NOT NULL UNIQUE,
    title      TEXT NOT NULL DEFAULT 'Morning Brief',
    content    TEXT NOT NULL,
    summary    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_morning_briefs_date ON morning_briefs (date DESC);
