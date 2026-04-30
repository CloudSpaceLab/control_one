-- Trust Center: public compliance portal tables
-- Subprocessors (third-party service providers)
CREATE TABLE IF NOT EXISTS subprocessors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    purpose TEXT NOT NULL,
    location VARCHAR(255) NOT NULL,
    data_types TEXT[] NOT NULL DEFAULT '{}',
    dpa_in_place BOOLEAN NOT NULL DEFAULT false,
    soc2 BOOLEAN NOT NULL DEFAULT false,
    iso27001 BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subprocessors_tenant_id ON subprocessors(tenant_id);

-- Certifications (compliance certifications)
CREATE TABLE IF NOT EXISTS certifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    type VARCHAR(100) NOT NULL, -- SOC2, ISO27001, PCI-DSS, etc.
    scope TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    auditor VARCHAR(255) NOT NULL,
    report_url TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'active', -- active, expired, pending
    UNIQUE(tenant_id, type, issued_at)
);

CREATE INDEX idx_certifications_tenant_id ON certifications(tenant_id);
CREATE INDEX idx_certifications_status ON certifications(status);

-- Security FAQ (Q&A for public trust center)
CREATE TABLE IF NOT EXISTS security_faq (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    category VARCHAR(100) NOT NULL,
    order_idx INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_security_faq_tenant_id ON security_faq(tenant_id);
CREATE INDEX idx_security_faq_category ON security_faq(category);

-- Incident Reports (published security incidents)
CREATE TABLE IF NOT EXISTS incident_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    incident_id VARCHAR(100) NOT NULL UNIQUE,
    title VARCHAR(255) NOT NULL,
    summary TEXT NOT NULL,
    severity VARCHAR(50) NOT NULL, -- critical, high, medium, low
    status VARCHAR(50) NOT NULL, -- open, resolved, postmortem
    started_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    report_url TEXT
);

CREATE INDEX idx_incident_reports_tenant_id ON incident_reports(tenant_id);
CREATE INDEX idx_incident_reports_status ON incident_reports(status);
CREATE INDEX idx_incident_reports_published_at ON incident_reports(published_at DESC);

COMMENT ON TABLE subprocessors IS 'Third-party service providers that may process customer data';
COMMENT ON TABLE certifications IS 'Compliance certifications and audit reports';
COMMENT ON TABLE security_faq IS 'Security FAQ for public trust center';
COMMENT ON TABLE incident_reports IS 'Published security incident reports';
