CREATE TABLE node_patch_health (
 node_id UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 observed_at TIMESTAMPTZ NOT NULL,
 evidence JSONB NOT NULL
);
CREATE TABLE patch_verifications (
 job_id UUID PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 deployment_id UUID NOT NULL REFERENCES patch_deployments(id) ON DELETE CASCADE,
 state TEXT NOT NULL CHECK(state IN ('execution_pending','execution_failed','verification_pending','verification_unknown','verification_failed','verified_fixed','verified_removed')),
 baseline JSONB NOT NULL,
 execution_completed_at TIMESTAMPTZ,
 execution_health JSONB,
 assessment_id UUID REFERENCES node_assessments(id),
 evidence JSONB NOT NULL DEFAULT '{}',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX patch_verifications_node ON patch_verifications(tenant_id,node_id,updated_at DESC);
-- Historical success has no captured baseline/health and cannot be certified.
INSERT INTO patch_verifications(job_id,tenant_id,node_id,deployment_id,state,baseline,execution_completed_at,evidence)
 SELECT p.job_id,p.tenant_id,p.node_id,p.deployment_id,
 CASE WHEN p.status='failed' THEN 'execution_failed' ELSE 'verification_unknown' END,
 '[]'::jsonb,COALESCE(p.applied_at,j.finished_at,p.requested_at),'{"reason":"legacy_no_authoritative_baseline_or_health"}'::jsonb
 FROM node_patch_state p JOIN jobs j ON j.id=p.job_id AND j.tenant_id=p.tenant_id
 WHERE p.status IN ('applied','failed') ON CONFLICT DO NOTHING;
CREATE TABLE patch_verification_events (
 id UUID PRIMARY KEY,
 job_id UUID NOT NULL REFERENCES patch_verifications(job_id) ON DELETE CASCADE,
 assessment_id UUID REFERENCES node_assessments(id),
 state TEXT NOT NULL,
 evidence JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(job_id,assessment_id)
);
