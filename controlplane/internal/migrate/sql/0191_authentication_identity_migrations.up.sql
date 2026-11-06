CREATE TABLE authentication_identity_migrations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), actor_id uuid NOT NULL REFERENCES users(id), user_id uuid NOT NULL REFERENCES users(id),
 old_external_id text NOT NULL,new_external_id text NOT NULL,authority_sha256 text NOT NULL,reason text NOT NULL CHECK(length(btrim(reason))>=10),
 previous_grants jsonb NOT NULL,current_grants jsonb NOT NULL,recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
