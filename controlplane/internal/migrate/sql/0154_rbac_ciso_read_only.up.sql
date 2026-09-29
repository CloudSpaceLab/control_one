-- The CISO baseline is read-only. Keep operational mutations and RBAC/user
-- administration under the administrator role; handlers enforce the same
-- boundary, and this migration makes the displayed baseline truthful.
DELETE FROM role_permissions rp
USING roles r
WHERE rp.role_id = r.id
  AND r.name = 'ciso'
  AND rp.permission_name IN (
    'alerts.acknowledge',
    'compliance.run',
    'dashboards.write',
    'remediation.approve',
    'roles.read',
    'roles.write',
    'rules.write',
    'settings.write',
    'threat_feeds.write',
    'users.read',
    'users.write'
  );
