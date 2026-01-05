-- Remediation scripts storage
CREATE TABLE IF NOT EXISTS remediation_scripts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id VARCHAR(255) NOT NULL,
    platform VARCHAR(50) NOT NULL, -- 'linux', 'windows', 'all'
    script_type VARCHAR(50) NOT NULL, -- 'shell', 'powershell', 'ansible', 'puppet'
    script_content TEXT NOT NULL,
    checksum VARCHAR(64), -- SHA256 checksum
    version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT true,
    metadata JSONB DEFAULT '{}',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(rule_id, platform, version)
);

CREATE INDEX idx_remediation_scripts_rule_id ON remediation_scripts(rule_id);
CREATE INDEX idx_remediation_scripts_platform ON remediation_scripts(platform);
CREATE INDEX idx_remediation_scripts_enabled ON remediation_scripts(enabled);
CREATE INDEX idx_remediation_scripts_rule_platform ON remediation_scripts(rule_id, platform) WHERE enabled = true;

COMMENT ON TABLE remediation_scripts IS 'Stores remediation scripts for compliance rules';
COMMENT ON COLUMN remediation_scripts.rule_id IS 'Compliance rule identifier';
COMMENT ON COLUMN remediation_scripts.platform IS 'Target platform: linux, windows, or all';
COMMENT ON COLUMN remediation_scripts.script_type IS 'Script type: shell, powershell, ansible, puppet';
COMMENT ON COLUMN remediation_scripts.script_content IS 'The remediation script content';
COMMENT ON COLUMN remediation_scripts.checksum IS 'SHA256 checksum of script content';


