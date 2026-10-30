ALTER TABLE squid_proxies DROP CONSTRAINT IF EXISTS squid_proxies_status_check;
ALTER TABLE squid_proxies ADD CONSTRAINT squid_proxies_status_check
    CHECK (status IN ('registered', 'installing', 'healthy', 'degraded', 'removing', 'removed'));
ALTER TABLE squid_proxies ALTER COLUMN status SET DEFAULT 'registered';
