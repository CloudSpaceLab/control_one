CREATE TABLE IF NOT EXISTS compliance_evidence (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  evidence_type   TEXT NOT NULL,
  framework       TEXT,
  control_ref     TEXT,
  title           TEXT NOT NULL,
  description     TEXT,
  file_path       TEXT,
  file_size_bytes BIGINT,
  mime_type       TEXT,
  checksum        TEXT,
  uploaded_by     UUID NOT NULL,
  uploaded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at      TIMESTAMPTZ,
  metadata        JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS compliance_reviews (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  review_type   TEXT NOT NULL,
  scheduled_for DATE,
  completed_at  TIMESTAMPTZ,
  reviewed_by   UUID,
  status        TEXT NOT NULL DEFAULT 'pending',
  notes         TEXT,
  recurrence    TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_reports (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  framework     TEXT NOT NULL,
  period_start  DATE NOT NULL,
  period_end    DATE NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending',
  pdf_path      TEXT,
  generated_by  UUID,
  generated_at  TIMESTAMPTZ,
  metadata      JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_evidence_tenant ON compliance_evidence(tenant_id, uploaded_at DESC);
CREATE INDEX IF NOT EXISTS idx_reviews_due ON compliance_reviews(tenant_id, scheduled_for) WHERE completed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_reports_tenant ON audit_reports(tenant_id, period_end DESC);
