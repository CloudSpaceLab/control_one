UPDATE squid_proxies SET status = 'degraded',
    last_error = COALESCE(last_error, 'Registered proxy; target readiness has not been verified')
WHERE status = 'registered';
ALTER TABLE squid_proxies DROP CONSTRAINT IF EXISTS squid_proxies_status_check;
ALTER TABLE squid_proxies ADD CONSTRAINT squid_proxies_status_check
    CHECK (status IN ('installing', 'healthy', 'degraded', 'removing', 'removed'));
ALTER TABLE squid_proxies ALTER COLUMN status SET DEFAULT 'installing';
