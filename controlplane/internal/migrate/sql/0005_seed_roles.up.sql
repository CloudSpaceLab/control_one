INSERT INTO roles (id, name, description)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'admin', 'Platform administrator'),
    ('22222222-2222-2222-2222-222222222222', 'operator', 'Operational user'),
    ('33333333-3333-3333-3333-333333333333', 'viewer', 'Read-only user')
ON CONFLICT (name) DO NOTHING;
