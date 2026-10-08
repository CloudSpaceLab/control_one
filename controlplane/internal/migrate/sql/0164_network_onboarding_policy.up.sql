CREATE TABLE network_onboarding_settings (
    setting_key TEXT PRIMARY KEY CHECK (setting_key = 'allowed_cidrs'),
    allowed_cidrs TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
