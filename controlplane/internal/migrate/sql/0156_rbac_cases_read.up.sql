-- Add the read permission used by the SOC cases collection and detail routes.
INSERT INTO permissions (name, description, category)
VALUES ('cases.read', 'View SOC investigation cases', 'cases')
ON CONFLICT (name) DO NOTHING;

-- Preserve the built-in read policy while allowing custom roles to opt in later.
INSERT INTO role_permissions (role_id, permission_name)
SELECT r.id, 'cases.read'
FROM roles r
WHERE r.name IN ('admin', 'ciso', 'operator')
ON CONFLICT DO NOTHING;
