-- Target identity is independent of agent enrollment. For compatibility a
-- compute target uses its node UUID; network targets never require a node row.
ALTER TABLE nodes ADD CONSTRAINT nodes_id_tenant_unique UNIQUE (id, tenant_id);

CREATE TABLE targets (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id UUID UNIQUE,
    family TEXT NOT NULL CHECK (family IN ('managed_compute', 'network_security', 'other_infrastructure')),
    type TEXT NOT NULL,
    subtype TEXT NOT NULL DEFAULT '',
    hostname TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL,
    site TEXT NOT NULL DEFAULT '',
    device_group TEXT NOT NULL DEFAULT '',
    vendor TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    platform TEXT NOT NULL DEFAULT '',
    firmware TEXT NOT NULL DEFAULT '',
    serial TEXT NOT NULL DEFAULT '',
    lifecycle_state TEXT NOT NULL DEFAULT 'active' CHECK (lifecycle_state IN ('active', 'retired')),
    reachability_state TEXT NOT NULL DEFAULT 'unknown' CHECK (reachability_state IN ('unknown', 'reachable', 'unreachable')),
    reachability_mode TEXT NOT NULL DEFAULT 'unknown',
    collection_state TEXT NOT NULL DEFAULT 'discovered' CHECK (collection_state IN (
        'discovered', 'reachable', 'authenticated', 'inventory_ready', 'telemetry_partial',
        'telemetry_ready', 'stale', 'auth_failed', 'unreachable', 'unsupported', 'policy_blocked')),
    management_modes JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(management_modes) = 'array'),
    capabilities JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(capabilities) = 'array'),
    classification JSONB NOT NULL DEFAULT '{"source":"unknown","confidence":0,"evidence":[]}',
    last_observed_at TIMESTAMPTZ,
    last_successful_collection_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (id, tenant_id),
    FOREIGN KEY (node_id, tenant_id) REFERENCES nodes(id, tenant_id),
    CHECK (node_id IS NULL OR (family = 'managed_compute' AND id = node_id)),
    CHECK (
        (family = 'managed_compute' AND type IN ('server', 'vm', 'cloud_instance', 'domain_controller', 'workstation', 'laptop', 'personal_pc', 'kiosk', 'unknown')) OR
        (family = 'network_security' AND type IN ('router', 'switch', 'firewall', 'load_balancer', 'waf', 'vpn_gateway', 'wireless_controller', 'access_point', 'ids_ips', 'network_appliance')) OR
        (family = 'other_infrastructure' AND type IN ('hypervisor', 'storage_appliance', 'unknown'))
    )
);
CREATE INDEX targets_tenant_site_type_idx ON targets(tenant_id, site, type, id);
CREATE INDEX targets_tenant_group_idx ON targets(tenant_id, device_group, id);
CREATE INDEX targets_family_type_idx ON targets(family, type, id);
CREATE INDEX targets_tenant_vendor_idx ON targets(tenant_id, vendor, model);

-- Keep management addresses distinct from observed addresses, preserving old
-- evidence when an address changes. Addresses are not canonical identity.
CREATE TABLE target_addresses (
    target_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('management', 'observed')),
    address TEXT NOT NULL CHECK (length(address) > 0),
    source TEXT NOT NULL CHECK (length(source) > 0),
    confidence INTEGER NOT NULL CHECK (confidence BETWEEN 0 AND 100),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    current BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (target_id, purpose, address, source),
    FOREIGN KEY (target_id, tenant_id) REFERENCES targets(id, tenant_id) ON DELETE CASCADE ON UPDATE CASCADE,
    CHECK (last_seen_at >= first_seen_at)
);
CREATE INDEX target_addresses_lookup_idx ON target_addresses(tenant_id, address) WHERE current;

CREATE FUNCTION sync_compute_target(n nodes) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    target_type TEXT;
    confidence INTEGER := 0;
    evidence JSONB := '[]';
    agent_capabilities JSONB := '[]';
BEGIN
    target_type := COALESCE(n.labels->>'target.type', 'unknown');
    IF target_type NOT IN ('server', 'vm', 'cloud_instance', 'domain_controller', 'workstation', 'laptop', 'personal_pc', 'kiosk', 'unknown') THEN
        target_type := 'unknown';
    END IF;
    IF COALESCE(n.labels->>'target.classification_confidence', '') ~ '^\d{1,3}$' THEN
        confidence := LEAST(100, (n.labels->>'target.classification_confidence')::INTEGER);
    END IF;
    IF jsonb_typeof(n.labels->'target.classification_evidence') = 'array' THEN
        SELECT COALESCE(jsonb_agg(value), '[]') INTO evidence
        FROM jsonb_array_elements(n.labels->'target.classification_evidence') WHERE jsonb_typeof(value) = 'string';
    END IF;
    IF jsonb_typeof(n.labels->'agent.capabilities') = 'array' THEN
        SELECT COALESCE(jsonb_agg(value ORDER BY value), '[]') INTO agent_capabilities
        FROM (SELECT DISTINCT value FROM jsonb_array_elements(n.labels->'agent.capabilities')
            WHERE jsonb_typeof(value) = 'string') c;
    END IF;
    INSERT INTO targets (id, tenant_id, node_id, family, type, hostname, display_name, site, device_group,
        lifecycle_state, reachability_mode, management_modes, capabilities, classification, last_observed_at, created_at, updated_at)
    VALUES (n.id, n.tenant_id, n.id, 'managed_compute', target_type, n.hostname, n.hostname,
        COALESCE(n.labels->>'site', ''), COALESCE(n.labels->>'server_group', ''),
        CASE WHEN n.state = 'retired' THEN 'retired' ELSE 'active' END,
        COALESCE(n.labels->>'target.reachability_mode', 'unknown'), '["agent"]', agent_capabilities,
        jsonb_build_object('source', COALESCE(n.labels->>'target.type_source', 'default'), 'confidence', confidence, 'evidence', evidence),
        n.last_seen_at, n.created_at, n.updated_at)
    ON CONFLICT (id) DO UPDATE SET
        tenant_id = EXCLUDED.tenant_id, node_id = EXCLUDED.node_id, type = EXCLUDED.type,
        hostname = EXCLUDED.hostname, display_name = EXCLUDED.display_name,
        site = EXCLUDED.site, device_group = EXCLUDED.device_group,
        lifecycle_state = EXCLUDED.lifecycle_state, reachability_mode = EXCLUDED.reachability_mode,
        capabilities = EXCLUDED.capabilities,
        classification = EXCLUDED.classification, last_observed_at = EXCLUDED.last_observed_at,
        updated_at = EXCLUDED.updated_at;

    UPDATE target_addresses SET current = FALSE
    WHERE target_id = n.id AND purpose = 'observed' AND source = 'node.public_ip' AND current;
    IF NULLIF(btrim(n.public_ip), '') IS NOT NULL THEN
        INSERT INTO target_addresses (target_id, tenant_id, purpose, address, source, confidence, first_seen_at, last_seen_at)
        VALUES (n.id, n.tenant_id, 'observed', btrim(n.public_ip), 'node.public_ip', 100,
            COALESCE(n.last_seen_at, n.updated_at), COALESCE(n.last_seen_at, n.updated_at))
        ON CONFLICT (target_id, purpose, address, source) DO UPDATE SET
            current = TRUE, last_seen_at = GREATEST(target_addresses.last_seen_at, EXCLUDED.last_seen_at);
    END IF;
END $$;

CREATE FUNCTION sync_node_target_trigger() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE targets SET node_id = NULL, lifecycle_state = 'retired', updated_at = NOW() WHERE node_id = OLD.id;
        RETURN OLD;
    END IF;
    PERFORM sync_compute_target(NEW);
    RETURN NEW;
END $$;
CREATE TRIGGER nodes_target_sync AFTER INSERT OR UPDATE OF tenant_id, hostname, labels, public_ip, state, last_seen_at ON nodes
    FOR EACH ROW EXECUTE FUNCTION sync_node_target_trigger();
CREATE TRIGGER nodes_target_retire BEFORE DELETE ON nodes
    FOR EACH ROW EXECUTE FUNCTION sync_node_target_trigger();
SELECT sync_compute_target(n) FROM nodes n;

INSERT INTO permissions(name, description, category) VALUES
    ('targets.read', 'Read estate targets and network device identity', 'inventory'),
    ('targets.write', 'Create agentless network target identity', 'inventory');
INSERT INTO role_permissions(role_id, permission_name)
SELECT id, 'targets.read' FROM roles WHERE name IN ('admin', 'operator', 'viewer', 'investigator', 'ciso');
INSERT INTO role_permissions(role_id, permission_name)
SELECT id, 'targets.write' FROM roles WHERE name IN ('admin', 'operator');
