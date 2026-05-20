CREATE INDEX IF NOT EXISTS idx_compliance_evidence_scope_uploaded
  ON compliance_evidence(tenant_id, framework, control_ref, uploaded_at DESC);

CREATE INDEX IF NOT EXISTS idx_compliance_evidence_type_uploaded
  ON compliance_evidence(tenant_id, evidence_type, uploaded_at DESC);
