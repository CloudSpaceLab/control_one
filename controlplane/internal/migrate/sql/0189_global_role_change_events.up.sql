CREATE TABLE global_role_change_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid NOT NULL REFERENCES users(id),
 user_id uuid NOT NULL REFERENCES users(id),
 reviewed_revision text NOT NULL,
 previous_grants jsonb NOT NULL,
 current_grants jsonb NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
