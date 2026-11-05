ALTER TABLE global_role_sources ADD COLUMN grant_id uuid NOT NULL DEFAULT gen_random_uuid();
