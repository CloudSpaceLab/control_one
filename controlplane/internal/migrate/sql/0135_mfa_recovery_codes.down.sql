DELETE FROM user_mfa_factors
WHERE factor_type = 'recovery';

ALTER TABLE user_mfa_factors
    DROP CONSTRAINT IF EXISTS user_mfa_factors_factor_type_check;

ALTER TABLE user_mfa_factors
    ADD CONSTRAINT user_mfa_factors_factor_type_check
    CHECK (factor_type IN ('totp','webauthn'));
