ALTER TABLE policy_assignments
    ADD COLUMN IF NOT EXISTS scope_type TEXT NOT NULL DEFAULT 'tenant',
    ADD COLUMN IF NOT EXISTS scope_id UUID,
    ADD COLUMN IF NOT EXISTS selector JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE policy_assignments
SET scope_type = CASE
        WHEN node_id IS NULL THEN 'tenant'
        ELSE 'node'
    END,
    scope_id = node_id,
    selector = '{}'::jsonb
WHERE scope_type = 'tenant'
  AND selector = '{}'::jsonb;

ALTER TABLE policy_assignments
    DROP CONSTRAINT IF EXISTS policy_assignments_policy_id_tenant_id_node_id_key;

DO $$ BEGIN
    ALTER TABLE policy_assignments
        ADD CONSTRAINT policy_assignments_scope_check
        CHECK (
            scope_type IN ('tenant', 'node', 'label_selector', 'cluster', 'hypervisor_host', 'enrollment_token')
            AND (
                (scope_type = 'tenant' AND scope_id IS NULL AND selector = '{}'::jsonb)
                OR (scope_type IN ('node', 'cluster', 'hypervisor_host', 'enrollment_token') AND scope_id IS NOT NULL AND selector = '{}'::jsonb)
                OR (scope_type = 'label_selector' AND scope_id IS NULL AND selector <> '{}'::jsonb)
            )
        );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS policy_assignments_scope_unique_idx
    ON policy_assignments (
        policy_id,
        tenant_id,
        scope_type,
        COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid),
        md5(selector::text)
    );

CREATE INDEX IF NOT EXISTS policy_assignments_scope_lookup_idx
    ON policy_assignments (tenant_id, scope_type, scope_id);

CREATE INDEX IF NOT EXISTS policy_assignments_selector_gin_idx
    ON policy_assignments USING GIN (selector);

ALTER TABLE provisioning_templates
    ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

ALTER TABLE provisioning_templates
    DROP CONSTRAINT IF EXISTS provisioning_templates_name_key;

CREATE INDEX IF NOT EXISTS provisioning_templates_tenant_id_idx
    ON provisioning_templates (tenant_id);

CREATE UNIQUE INDEX IF NOT EXISTS provisioning_templates_tenant_name_unique_idx
    ON provisioning_templates (
        COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid),
        name
    );

CREATE TABLE IF NOT EXISTS provisioning_template_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES provisioning_templates(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL DEFAULT 'tenant',
    scope_id UUID,
    selector JSONB NOT NULL DEFAULT '{}'::jsonb,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_by UUID REFERENCES users(id),
    expires_at TIMESTAMPTZ,
    CHECK (
        scope_type IN ('tenant', 'node', 'label_selector', 'cluster', 'hypervisor_host', 'enrollment_token')
        AND (
            (scope_type = 'tenant' AND scope_id IS NULL AND selector = '{}'::jsonb)
            OR (scope_type IN ('node', 'cluster', 'hypervisor_host', 'enrollment_token') AND scope_id IS NOT NULL AND selector = '{}'::jsonb)
            OR (scope_type = 'label_selector' AND scope_id IS NULL AND selector <> '{}'::jsonb)
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS provisioning_template_assignments_scope_unique_idx
    ON provisioning_template_assignments (
        template_id,
        tenant_id,
        scope_type,
        COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid),
        md5(selector::text)
    );

CREATE INDEX IF NOT EXISTS provisioning_template_assignments_template_idx
    ON provisioning_template_assignments (template_id);

CREATE INDEX IF NOT EXISTS provisioning_template_assignments_scope_lookup_idx
    ON provisioning_template_assignments (tenant_id, scope_type, scope_id);

CREATE INDEX IF NOT EXISTS provisioning_template_assignments_selector_gin_idx
    ON provisioning_template_assignments USING GIN (selector);
