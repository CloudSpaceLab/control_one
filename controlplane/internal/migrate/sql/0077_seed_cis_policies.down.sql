-- Removes the 24 CIS-mapped baseline policies seeded in the up migration.
-- Deletes by deterministic md5()::uuid IDs so this is a no-op if seeding never ran.
-- ON DELETE CASCADE on policy_versions.policy_id removes versions automatically,
-- but we delete versions first to be explicit.

DO $$
DECLARE
    slugs TEXT[] := ARRAY[
        'firewall_enabled','fail2ban_enabled','ssh_password_auth','ssh_root_login','ssh_protocol_2',
        'telnet_closed','ftp_closed','rsh_closed','nis_closed',
        'mysql_public','postgres_public','redis_public',
        'auditd_enabled','auto_updates','shadow_perms','passwd_perms','mac_enabled',
        'umask_secure','password_min_length','password_max_days','account_lockout',
        'nfs_disabled','time_sync','selinux_enforcing'
    ];
    s TEXT;
BEGIN
    FOREACH s IN ARRAY slugs
    LOOP
        DELETE FROM policy_versions WHERE id = md5('cis-policy-version:' || s || ':v1')::uuid;
        DELETE FROM policies        WHERE id = md5('cis-policy:' || s)::uuid;
    END LOOP;
END $$;
