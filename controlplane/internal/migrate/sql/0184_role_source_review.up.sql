CREATE TABLE global_role_source_reviews (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid NOT NULL REFERENCES users(id),
 user_id uuid NOT NULL REFERENCES users(id),
 assignment_id uuid NOT NULL,
 role_name text NOT NULL,
 reason text NOT NULL CHECK(length(btrim(reason)) >= 10),
 reviewed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
