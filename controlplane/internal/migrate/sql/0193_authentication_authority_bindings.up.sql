-- Public provider coordinates, not tokens. No legacy hash is reversed/backfilled.
CREATE TABLE authentication_authority_bindings (
 user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 external_id text NOT NULL UNIQUE,
 authority text NOT NULL CHECK(authority='static' OR authority LIKE 'oidc:%'),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 512),
 verified_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(authority,subject)
);
