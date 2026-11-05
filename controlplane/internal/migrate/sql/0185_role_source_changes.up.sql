ALTER TABLE global_role_source_reviews
 ADD COLUMN action text NOT NULL DEFAULT 'claim_legacy' CHECK(action IN ('claim_legacy','set_expiry','revoke')),
 ADD COLUMN before_expiry timestamptz,
 ADD COLUMN after_expiry timestamptz;
