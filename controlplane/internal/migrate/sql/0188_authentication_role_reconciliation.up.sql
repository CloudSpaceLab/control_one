ALTER TABLE global_role_sources DROP CONSTRAINT global_role_sources_source_check;
ALTER TABLE global_role_sources ADD CONSTRAINT global_role_sources_source_check
 CHECK(source IN ('administrator','directory','legacy_unknown','provisioning','authentication'));
CREATE TABLE authentication_role_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id),
 authority_sha256 text NOT NULL,
 previous_roles jsonb NOT NULL,
 current_roles jsonb NOT NULL,
 observed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
