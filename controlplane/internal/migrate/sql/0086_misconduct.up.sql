-- Use Case 7: Misconduct & Whistleblowing
-- Tables for case management, anonymous whistleblower intake (with PoW + rate
-- limits enforced at the application tier), evidence locker links into the
-- existing compliance_evidence table, and a per-case risk-signal stream
-- consumed by the misconduct.score job.

-- Cases tracked by investigators. risk_score is recomputed by the
-- misconduct.score job from audit_logs, security_events, behavioral_baselines
-- and compliance_results signals (capped at 100). subject_user_id and
-- subject_label are mutually informative — a case may target a known user
-- account or a free-form label (e.g. role / contractor).
CREATE TABLE IF NOT EXISTS misconduct_cases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'open'
                       CHECK (status IN ('open','investigating','closed')),
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    opened_by       UUID,
    summary         TEXT NOT NULL DEFAULT '',
    risk_score      INTEGER NOT NULL DEFAULT 0,
    subject_user_id UUID,
    subject_label   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_misconduct_cases_tenant_status
    ON misconduct_cases (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_misconduct_cases_subject_user
    ON misconduct_cases (subject_user_id) WHERE subject_user_id IS NOT NULL;

-- Whistleblower intake — intentionally PII-free. We store a bcrypt hash of
-- the one-time token (so the submitter can poll status), the body encrypted
-- with the platform sealer (AES-256-GCM), and a retention deadline. No IP,
-- email, name, or user-agent column exists by design.
CREATE TABLE IF NOT EXISTS whistleblower_submissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID,
    token_hash      TEXT NOT NULL,
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    body_encrypted  BYTEA,
    body_nonce      BYTEA,
    retention_until TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'received'
);

CREATE INDEX IF NOT EXISTS idx_whistleblower_token_hash
    ON whistleblower_submissions (token_hash);
CREATE INDEX IF NOT EXISTS idx_whistleblower_retention
    ON whistleblower_submissions (retention_until);

-- Case evidence locker — references the existing compliance_evidence table so
-- evidence reuses the upload + checksum + retention infra from PR 26.
CREATE TABLE IF NOT EXISTS case_evidence (
    case_id     UUID NOT NULL REFERENCES misconduct_cases(id) ON DELETE CASCADE,
    evidence_id UUID NOT NULL REFERENCES compliance_evidence(id) ON DELETE CASCADE,
    attached_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (case_id, evidence_id)
);

-- Risk signals — append-only, written by the misconduct.score job. Each row
-- captures one signal contributing to the case's score; severity drives
-- weight (critical=30, high=15, medium=5, low=1), capped at 100.
CREATE TABLE IF NOT EXISTS risk_signals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id      UUID NOT NULL REFERENCES misconduct_cases(id) ON DELETE CASCADE,
    signal_type  TEXT NOT NULL,
    severity     TEXT NOT NULL CHECK (severity IN ('critical','high','medium','low')),
    source_id    UUID,
    source_table TEXT,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    weight       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_risk_signals_case_occurred
    ON risk_signals (case_id, occurred_at DESC);

-- Seed the investigator role so the RBAC gate on /api/v1/misconduct/cases*
-- has something to grant. Existing seed in 0005 only covers admin/operator/viewer.
INSERT INTO roles (id, name, description)
VALUES ('44444444-4444-4444-4444-444444444444', 'investigator',
        'Misconduct & whistleblowing investigator')
ON CONFLICT (name) DO NOTHING;

COMMENT ON TABLE misconduct_cases IS 'UC7: investigator cases driving risk-score aggregation.';
COMMENT ON TABLE whistleblower_submissions IS 'UC7: PII-free anonymous intake. Body sealed via secretbox; bcrypt-hashed one-time token for status polling.';
COMMENT ON TABLE case_evidence IS 'UC7: link table — case <-> compliance_evidence rows.';
COMMENT ON TABLE risk_signals IS 'UC7: per-case append-only signals written by the misconduct.score job.';
