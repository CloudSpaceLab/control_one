INSERT INTO role_permissions (role_id, permission_name)
SELECT r.id, permission_name
FROM roles r
CROSS JOIN unnest(ARRAY[
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
]) AS grants(permission_name)
WHERE r.name = 'ciso'
ON CONFLICT DO NOTHING;
