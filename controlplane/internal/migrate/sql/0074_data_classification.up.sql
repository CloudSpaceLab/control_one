CREATE TABLE IF NOT EXISTS data_classification_rules (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  pii_type      TEXT NOT NULL,
  regex         TEXT NOT NULL,
  severity      TEXT NOT NULL DEFAULT 'medium',
  enabled       BOOLEAN NOT NULL DEFAULT TRUE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS column_classifications (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL,
  node_id         UUID NOT NULL,
  database_name   TEXT NOT NULL,
  schema_name     TEXT NOT NULL DEFAULT 'public',
  table_name      TEXT NOT NULL,
  column_name     TEXT NOT NULL,
  pii_type        TEXT,
  encrypted       BOOLEAN,
  encryption_kind TEXT,
  min_value_length INT,
  max_value_length INT,
  sample_count    INT,
  last_scanned_at TIMESTAMPTZ,
  UNIQUE (tenant_id, node_id, database_name, schema_name, table_name, column_name)
);

CREATE TABLE IF NOT EXISTS pii_findings (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL,
  column_classification_id UUID REFERENCES column_classifications(id) ON DELETE CASCADE,
  rule_id         UUID REFERENCES data_classification_rules(id) ON DELETE SET NULL,
  severity        TEXT NOT NULL DEFAULT 'medium',
  details         TEXT,
  resolved_at     TIMESTAMPTZ,
  resolved_by     UUID,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pii_findings_tenant ON pii_findings(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_column_class_tenant ON column_classifications(tenant_id);
CREATE INDEX IF NOT EXISTS idx_dcr_tenant ON data_classification_rules(tenant_id);
