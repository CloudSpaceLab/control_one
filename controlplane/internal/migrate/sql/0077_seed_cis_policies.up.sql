-- Seeds 24 CIS-mapped baseline policies platform-wide (tenant_id = NULL).
-- Policy and version IDs are derived deterministically via md5()::uuid so
-- re-running the migration is idempotent (ON CONFLICT DO NOTHING).
-- Policy metadata (control_id, severity, evaluator, framework_hints) lives in
-- policy_versions.metadata since the policies table has no `control` column.

DO $$
DECLARE
    p   RECORD;
    pid UUID;
    vid UUID;
BEGIN
    FOR p IN SELECT * FROM (VALUES
        ('firewall_enabled',     'Host firewall enabled',                 'CIS-3.5.1',   'critical', 'facts.security.firewall.any_enabled == true',                                          ARRAY['SOC2:CC6.6','HIPAA:164.312(a)(1)','PCI-DSS:1.1','ISO27001:A.8.20']::text[]),
        ('fail2ban_enabled',     'fail2ban service enabled',              'CIS-5.3.1',   'high',     'facts.security.fail2ban.enabled == true',                                              ARRAY['SOC2:CC6.1','HIPAA:164.308(a)(5)','PCI-DSS:6.3.3','ISO27001:A.8.5']::text[]),
        ('ssh_password_auth',    'SSH password authentication disabled',  'CIS-5.2.10',  'high',     'facts.ssh.password_auth_disabled == true',                                             ARRAY['SOC2:CC6.1','HIPAA:164.312(d)','PCI-DSS:8.3.6','ISO27001:A.8.5']::text[]),
        ('ssh_root_login',       'SSH root login disabled',               'CIS-5.2.11',  'high',     'facts.ssh.root_login_disabled == true',                                                ARRAY['SOC2:CC6.3','HIPAA:164.312(a)(1)','PCI-DSS:7.1','ISO27001:A.8.18']::text[]),
        ('ssh_protocol_2',       'SSH protocol version 2 enforced',       'CIS-5.2.4',   'medium',   'facts.ssh.protocol == 2',                                                              ARRAY['SOC2:CC6.6','HIPAA:164.312(e)(1)','PCI-DSS:4.2','ISO27001:A.8.20']::text[]),
        ('telnet_closed',        'Telnet (port 23) not listening',        'CIS-2.1.1',   'high',     '!facts.ports.listening.contains(23)',                                                  ARRAY['SOC2:CC6.6','HIPAA:164.312(e)(1)','PCI-DSS:4.2.1','ISO27001:A.8.20']::text[]),
        ('ftp_closed',           'FTP (port 21) not listening',           'CIS-2.1.2',   'high',     '!facts.ports.listening.contains(21)',                                                  ARRAY['SOC2:CC6.6','HIPAA:164.312(e)(1)','PCI-DSS:4.2.1','ISO27001:A.8.20']::text[]),
        ('rsh_closed',           'rsh/rlogin/rexec ports closed',         'CIS-2.1.3',   'medium',   '!facts.ports.listening.contains_any([512,513,514])',                                   ARRAY['SOC2:CC6.6','HIPAA:164.312(e)(1)','PCI-DSS:4.2.1','ISO27001:A.8.20']::text[]),
        ('nis_closed',           'NIS service not enabled',               'CIS-2.1.4',   'medium',   '!facts.services.enabled.contains("ypserv")',                                           ARRAY['SOC2:CC6.6','HIPAA:164.312(a)(1)','PCI-DSS:6.5','ISO27001:A.8.20']::text[]),
        ('mysql_public',         'MySQL not bound to public interface',   'CIS-6.3.1',   'critical', '!facts.ports.listening_public.contains(3306)',                                         ARRAY['SOC2:CC6.6','HIPAA:164.312(a)(1)','PCI-DSS:6.5','ISO27001:A.8.20']::text[]),
        ('postgres_public',      'Postgres not bound to public interface','CIS-6.3.2',   'critical', '!facts.ports.listening_public.contains(5432)',                                         ARRAY['SOC2:CC6.6','HIPAA:164.312(a)(1)','PCI-DSS:6.5','ISO27001:A.8.20']::text[]),
        ('redis_public',         'Redis not bound to public interface',   'CIS-6.3.3',   'critical', '!facts.ports.listening_public.contains(6379)',                                         ARRAY['SOC2:CC6.6','HIPAA:164.312(a)(1)','PCI-DSS:6.5','ISO27001:A.8.20']::text[]),
        ('auditd_enabled',       'auditd service enabled',                'CIS-4.1.2',   'high',     'facts.security.auditd.enabled == true',                                                ARRAY['SOC2:CC7.2','HIPAA:164.312(b)','PCI-DSS:10.2','ISO27001:A.8.15']::text[]),
        ('auto_updates',         'OS automatic security updates enabled', 'CIS-1.9',     'high',     'facts.os.auto_updates.enabled == true',                                                ARRAY['SOC2:CC7.1','HIPAA:164.308(a)(5)','PCI-DSS:6.3.3','ISO27001:A.8.8']::text[]),
        ('shadow_perms',         '/etc/shadow mode is 0640 or stricter',  'CIS-6.1.3',   'high',     'facts.files.shadow.mode == "0640"',                                                    ARRAY['SOC2:CC6.3','HIPAA:164.312(a)(1)','PCI-DSS:7.1','ISO27001:A.8.3']::text[]),
        ('passwd_perms',         '/etc/passwd mode is 0644',              'CIS-6.1.2',   'medium',   'facts.files.passwd.mode == "0644"',                                                    ARRAY['SOC2:CC6.3','HIPAA:164.312(a)(1)','PCI-DSS:7.1','ISO27001:A.8.3']::text[]),
        ('mac_enabled',          'Mandatory access control enabled',      'CIS-1.7.1',   'high',     'facts.security.mac.any_enabled == true',                                               ARRAY['SOC2:CC6.1','HIPAA:164.312(a)(1)','PCI-DSS:6.7','ISO27001:A.8.3']::text[]),
        ('umask_secure',         'Default shell umask is 027 or 077',     'CIS-5.5.4',   'medium',   'facts.shell.umask in ["027","077"]',                                                   ARRAY['SOC2:CC6.3','HIPAA:164.312(a)(1)','PCI-DSS:7.1','ISO27001:A.8.3']::text[]),
        ('password_min_length',  'PAM password min length >= 14',         'CIS-5.4.1',   'medium',   'facts.pam.password_min_length >= 14',                                                  ARRAY['SOC2:CC6.1','HIPAA:164.308(a)(5)(ii)(D)','PCI-DSS:8.3.6','ISO27001:A.8.5']::text[]),
        ('password_max_days',    'PAM password max days <= 365',          'CIS-5.4.1',   'medium',   'facts.pam.password_max_days <= 365',                                                   ARRAY['SOC2:CC6.1','HIPAA:164.308(a)(5)(ii)(D)','PCI-DSS:8.3.9','ISO27001:A.8.5']::text[]),
        ('account_lockout',      'PAM account lockout enabled',           'CIS-5.4.2',   'high',     'facts.pam.account_lockout.enabled == true',                                            ARRAY['SOC2:CC6.1','HIPAA:164.308(a)(5)','PCI-DSS:8.3.4','ISO27001:A.8.5']::text[]),
        ('nfs_disabled',         'NFS server not enabled',                'CIS-2.2.7',   'medium',   '!facts.services.enabled.contains("nfs-server")',                                       ARRAY['SOC2:CC6.6','HIPAA:164.312(e)(1)','PCI-DSS:4.2','ISO27001:A.8.20']::text[]),
        ('time_sync',            'Time synchronization service running',  'CIS-2.2.1',   'medium',   'facts.services.enabled.contains_any(["chronyd","systemd-timesyncd"])',                 ARRAY['SOC2:CC7.2','HIPAA:164.312(b)','PCI-DSS:10.6','ISO27001:A.8.15']::text[]),
        ('selinux_enforcing',    'SELinux mode is enforcing',             'CIS-1.7.1.4', 'high',     'facts.security.selinux.mode == "enforcing"',                                           ARRAY['SOC2:CC6.1','HIPAA:164.312(a)(1)','PCI-DSS:6.7','ISO27001:A.8.3']::text[])
    ) AS t(slug, title, control_id, severity, evaluator, framework_hints)
    LOOP
        pid := md5('cis-policy:' || p.slug)::uuid;
        vid := md5('cis-policy-version:' || p.slug || ':v1')::uuid;

        INSERT INTO policies (id, tenant_id, name, description, rule_type, enabled, labels, created_at, updated_at)
        VALUES (
            pid,
            NULL,
            p.slug,
            p.title,
            'cis_check',
            true,
            jsonb_build_object('source', 'cis', 'platform', 'linux', 'control_id', p.control_id),
            NOW(),
            NOW()
        )
        ON CONFLICT (id) DO NOTHING;

        INSERT INTO policy_versions (id, policy_id, version, rule_definition, checksum, metadata, created_at, promoted_at)
        VALUES (
            vid,
            pid,
            1,
            p.evaluator,
            md5(p.evaluator),
            jsonb_build_object(
                'control_id',      p.control_id,
                'severity',        p.severity,
                'evaluator',       p.evaluator,
                'framework_hints', to_jsonb(p.framework_hints)
            ),
            NOW(),
            NOW()
        )
        ON CONFLICT (id) DO NOTHING;
    END LOOP;
END $$;
