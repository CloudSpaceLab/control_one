-- Read-only projection; original atomic evidence remains in its source tables.
CREATE VIEW global_identity_audit AS
SELECT id, tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata, created_at
FROM audit_logs WHERE tenant_id IS NULL AND resource_type IN ('user','identity','session')
UNION ALL
SELECT id, NULL::uuid, actor_id, 'user', 'user.roles.change', 'user', user_id::text,
 jsonb_build_object('reviewed_revision',reviewed_revision,'previous_grants',previous_grants,'current_grants',current_grants), recorded_at
FROM global_role_change_events
UNION ALL
SELECT id, NULL::uuid, actor_id, 'user', 'user.role_source.'||action, 'user', user_id::text,
 jsonb_build_object('assignment_id',assignment_id,'role',role_name,'reason',reason,'before_expiry',before_expiry,'after_expiry',after_expiry),reviewed_at
FROM global_role_source_reviews
UNION ALL
SELECT id, NULL::uuid, NULL::uuid, 'system', 'user.authentication_roles.reconcile', 'user', user_id::text,
 jsonb_build_object('authority_sha256',authority_sha256,'previous_roles',previous_roles,'current_roles',current_roles),observed_at
FROM authentication_role_events;
