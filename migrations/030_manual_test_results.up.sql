CREATE TABLE IF NOT EXISTS manual_test_results (
    id              SERIAL PRIMARY KEY,
    scenario_id     TEXT NOT NULL,
    scenario_name   TEXT NOT NULL,
    clinic          TEXT NOT NULL DEFAULT 'Forever 22 Med Spa',
    category        TEXT NOT NULL DEFAULT 'must-pass',
    status          TEXT NOT NULL DEFAULT 'untested',  -- untested, passed, failed, skipped
    tested_at       TIMESTAMPTZ,
    tested_by       TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(scenario_id, clinic)
);

CREATE INDEX idx_manual_test_results_clinic ON manual_test_results(clinic);
CREATE INDEX idx_manual_test_results_status ON manual_test_results(status);

-- Seed the 7 must-pass scenarios for Forever 22
INSERT INTO manual_test_results (scenario_id, scenario_name, clinic, category, status) VALUES
    ('phone-1', 'Happy Path Botox — New patient, full booking flow', 'Forever 22 Med Spa', 'must-pass', 'passed'),
    ('phone-2', 'Lip Filler with Provider Preference (Gale)', 'Forever 22 Med Spa', 'must-pass', 'passed'),
    ('phone-3', 'New Patient — No Time Preference', 'Forever 22 Med Spa', 'must-pass', 'untested'),
    ('phone-4', 'Returning Patient Flow', 'Forever 22 Med Spa', 'must-pass', 'untested'),
    ('phone-5', 'Price Question Handling', 'Forever 22 Med Spa', 'must-pass', 'untested'),
    ('phone-6', 'What Services Do You Offer?', 'Forever 22 Med Spa', 'must-pass', 'untested'),
    ('phone-7', 'STOP / START Opt-Out Compliance', 'Forever 22 Med Spa', 'must-pass', 'untested')
ON CONFLICT (scenario_id, clinic) DO NOTHING;

-- Smoke test scenarios for other clinics
INSERT INTO manual_test_results (scenario_id, scenario_name, clinic, category, status) VALUES
    ('smoke-1', 'Happy Path Booking', 'Brilliant Aesthetics', 'smoke-test', 'untested'),
    ('smoke-2', 'Happy Path Booking', 'Lucy''s Laser & MedSpa', 'smoke-test', 'untested'),
    ('smoke-3', 'Happy Path Booking', 'Adela Medical Spa', 'smoke-test', 'untested')
ON CONFLICT (scenario_id, clinic) DO NOTHING;

-- Automated E2E test results (latest run)
INSERT INTO manual_test_results (scenario_id, scenario_name, clinic, category, status) VALUES
    ('e2e-suite', 'Automated E2E Suite (30 scenarios)', 'Forever 22 Med Spa', 'automated', 'passed')
ON CONFLICT (scenario_id, clinic) DO NOTHING;
