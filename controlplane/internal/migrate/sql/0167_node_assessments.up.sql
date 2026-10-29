-- Assessment attempts are immutable evidence, including failed/partial attempts.
-- Ordering by start time prevents a delayed old attempt replacing a newer one.
CREATE TABLE node_assessments (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('partial', 'expired', 'unavailable', 'unknown')),
    inventory_generation TEXT NOT NULL,
    evidence JSONB NOT NULL,
    CHECK (completed_at >= started_at)
);
CREATE INDEX node_assessments_latest ON node_assessments (tenant_id, node_id, started_at DESC, id DESC);
