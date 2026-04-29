package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// defaultPolicySpec is the catalog entry used to seed a default security policy.
type defaultPolicySpec struct {
	Name        string
	Description string
	Severity    string
	Framework   string
	Control     string
	Remediation string
	Conditions  []map[string]any
}

// defaultPolicyLabel is the label added to every seeded default policy so we
// can detect them later and avoid re-seeding.
const defaultPolicyLabel = "control-one-default"

// defaultSecurityPolicies is the catalog of security baseline policies that are
// seeded for every tenant on first node enrollment. All conditions reference
// security facts collected by the node agent via securityfacts.Collect().
var defaultSecurityPolicies = []defaultPolicySpec{
	// ── fail2ban ──────────────────────────────────────────────────────────────
	{
		Name:        "[Default] fail2ban active",
		Description: "fail2ban must be installed and running to protect against brute-force attacks",
		Severity:    "medium",
		Framework:   "CIS",
		Control:     "5.3.1",
		Remediation: "Install and enable fail2ban: apt install fail2ban && systemctl enable --now fail2ban",
		Conditions: []map[string]any{
			{"field": "facts.security.fail2ban.installed", "op": "eq", "value": "true"},
			{"field": "facts.security.fail2ban.active", "op": "eq", "value": "true"},
		},
	},

	// ── firewall ──────────────────────────────────────────────────────────────
	{
		Name:        "[Default] firewall enabled",
		Description: "A host-based firewall (ufw, firewalld, or iptables rules) must be active",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "3.5.1",
		Remediation: "Enable ufw: ufw default deny incoming && ufw allow ssh && ufw enable",
		Conditions: []map[string]any{
			{"field": "facts.security.firewall.any_enabled", "op": "eq", "value": "true"},
		},
	},

	// ── SSH hardening ─────────────────────────────────────────────────────────
	{
		Name:        "[Default] SSH password authentication disabled",
		Description: "SSH must use key-based authentication only; password auth allows brute-force",
		Severity:    "high",
		Framework:   "CIS",
		Control:     "5.2.11",
		Remediation: "Set PasswordAuthentication no in /etc/ssh/sshd_config and restart sshd",
		Conditions: []map[string]any{
			{"field": "facts.security.ssh.password_auth", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] SSH root login disabled",
		Description: "Direct root login via SSH must be disabled",
		Severity:    "high",
		Framework:   "CIS",
		Control:     "5.2.10",
		Remediation: "Set PermitRootLogin no in /etc/ssh/sshd_config and restart sshd",
		Conditions: []map[string]any{
			{"field": "facts.security.ssh.root_login", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] SSH not on default port",
		Description: "Running SSH on port 22 (default) makes the server a target for automated scans; consider a non-standard port",
		Severity:    "low",
		Framework:   "CIS",
		Control:     "5.2.1",
		Remediation: "Change Port in /etc/ssh/sshd_config to a non-standard value (e.g. 2222) and update firewall rules",
		Conditions: []map[string]any{
			{"field": "facts.security.ssh.default_port", "op": "eq", "value": "false"},
		},
	},

	// ── dangerous ports ───────────────────────────────────────────────────────
	{
		Name:        "[Default] Telnet port closed",
		Description: "Port 23 (Telnet) must not be open; Telnet transmits data in cleartext",
		Severity:    "high",
		Framework:   "CIS",
		Control:     "2.1.1",
		Remediation: "Disable and remove telnetd: systemctl disable telnet.socket && apt remove telnetd",
		Conditions: []map[string]any{
			{"field": "facts.security.port.23.open", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] FTP port closed",
		Description: "Port 21 (FTP) must not be open; FTP transmits credentials in cleartext",
		Severity:    "medium",
		Framework:   "CIS",
		Control:     "2.1.2",
		Remediation: "Disable FTP service. Use SFTP or SCP instead: apt remove vsftpd proftpd",
		Conditions: []map[string]any{
			{"field": "facts.security.port.21.open", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] SMTP port not publicly exposed",
		Description: "Port 25 (SMTP) should not be open on internet-facing servers unless this is a mail server",
		Severity:    "medium",
		Framework:   "CIS",
		Control:     "2.2.8",
		Remediation: "Disable the local MTA or bind it to localhost only: postfix: inet_interfaces = loopback-only",
		Conditions: []map[string]any{
			{"field": "facts.security.port.25.open", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] RDP port closed",
		Description: "Port 3389 (RDP) must not be open on internet-facing Linux servers",
		Severity:    "high",
		Framework:   "CIS",
		Control:     "2.1.3",
		Remediation: "Disable xrdp: systemctl disable --now xrdp; restrict via firewall if required internally",
		Conditions: []map[string]any{
			{"field": "facts.security.port.3389.open", "op": "eq", "value": "false"},
		},
	},

	// ── database port exposure ────────────────────────────────────────────────
	{
		Name:        "[Default] MySQL/MariaDB not publicly bound",
		Description: "Port 3306 must not be open on public interfaces; databases should bind to localhost or private networks",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "6.3.1",
		Remediation: "Set bind-address = 127.0.0.1 in /etc/mysql/mysql.conf.d/mysqld.cnf and restart MySQL",
		Conditions: []map[string]any{
			{"field": "facts.security.port.3306.open", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] PostgreSQL not publicly bound",
		Description: "Port 5432 must not be open on public interfaces",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "6.3.2",
		Remediation: "Set listen_addresses = 'localhost' in postgresql.conf and restrict pg_hba.conf",
		Conditions: []map[string]any{
			{"field": "facts.security.port.5432.open", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] Redis not publicly accessible without auth",
		Description: "Redis must not be accessible without authentication from public interfaces",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "6.3.3",
		Remediation: "Set requirepass in /etc/redis/redis.conf and bind 127.0.0.1; restart Redis",
		Conditions: []map[string]any{
			{"field": "facts.security.redis.no_auth", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] MongoDB not publicly bound",
		Description: "Port 27017 must not be open on public interfaces",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "6.3.4",
		Remediation: "Set bindIp: 127.0.0.1 in /etc/mongod.conf and restart MongoDB",
		Conditions: []map[string]any{
			{"field": "facts.security.port.27017.open", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] Elasticsearch/OpenSearch not publicly bound",
		Description: "Port 9200 must not be open on public interfaces without authentication",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "6.3.5",
		Remediation: "Set network.host: localhost in elasticsearch.yml / opensearch.yml",
		Conditions: []map[string]any{
			{"field": "facts.security.port.9200.open", "op": "eq", "value": "false"},
		},
	},

	// ── system hardening ──────────────────────────────────────────────────────
	{
		Name:        "[Default] Automatic security updates enabled",
		Description: "Unattended-upgrades must be installed and enabled for automatic security patching",
		Severity:    "medium",
		Framework:   "CIS",
		Control:     "1.9",
		Remediation: "apt install unattended-upgrades && dpkg-reconfigure -plow unattended-upgrades",
		Conditions: []map[string]any{
			{"field": "facts.security.unattended_upgrades.installed", "op": "eq", "value": "true"},
		},
	},
	{
		Name:        "[Default] Mandatory access control active",
		Description: "SELinux or AppArmor must be enabled and active for mandatory access control",
		Severity:    "medium",
		Framework:   "CIS",
		Control:     "1.7.1",
		Remediation: "Enable AppArmor: apt install apparmor apparmor-utils && systemctl enable --now apparmor",
		Conditions: []map[string]any{
			{"field": "facts.security.mac.enabled", "op": "eq", "value": "true"},
		},
	},
	{
		Name:        "[Default] /etc/shadow not world-readable",
		Description: "The shadow password file must not be world-readable",
		Severity:    "critical",
		Framework:   "CIS",
		Control:     "6.1.3",
		Remediation: "chmod 640 /etc/shadow && chown root:shadow /etc/shadow",
		Conditions: []map[string]any{
			{"field": "facts.security.shadow.world_readable", "op": "eq", "value": "false"},
		},
	},
	{
		Name:        "[Default] Audit daemon running",
		Description: "auditd must be running to record system call activity for forensics",
		Severity:    "medium",
		Framework:   "CIS",
		Control:     "4.1.2",
		Remediation: "apt install auditd && systemctl enable --now auditd",
		Conditions: []map[string]any{
			{"field": "facts.security.auditd.running", "op": "eq", "value": "true"},
		},
	},
	{
		Name:        "[Default] VNC port closed",
		Description: "Port 5900 (VNC) must not be open on internet-facing servers; use SSH tunnelling instead",
		Severity:    "high",
		Framework:   "CIS",
		Control:     "2.1.4",
		Remediation: "Stop VNC server and use SSH -L tunnel for remote desktop access if needed",
		Conditions: []map[string]any{
			{"field": "facts.security.port.5900.open", "op": "eq", "value": "false"},
		},
	},
}

// ensureDefaultPolicies creates the default security baseline policies for a
// tenant if they don't already exist. Called on first node enrollment.
// Safe to call multiple times — skips policies that are already present.
func (s *Server) ensureDefaultPolicies(ctx context.Context, tenantID uuid.UUID) {
	if s.store == nil || tenantID == uuid.Nil {
		return
	}

	// Check if defaults already seeded for this tenant.
	trueVal := true
	existing, _, err := s.store.ListPolicies(ctx, storage.PolicyFilter{
		TenantID: tenantID,
		Enabled:  &trueVal,
	}, 1, 0)
	if err != nil {
		s.logger.Warn("ensureDefaultPolicies: list existing", zap.String("tenant_id", tenantID.String()), zap.Error(err))
		return
	}
	// Check for the default label
	for _, p := range existing {
		if p.Labels[defaultPolicyLabel] == "true" {
			// Already seeded — skip.
			return
		}
	}

	s.logger.Info("seeding default security policies", zap.String("tenant_id", tenantID.String()), zap.Int("count", len(defaultSecurityPolicies)))

	for _, spec := range defaultSecurityPolicies {
		if err := s.seedOneDefaultPolicy(ctx, tenantID, spec); err != nil {
			s.logger.Warn("seed default policy",
				zap.String("name", spec.Name),
				zap.String("tenant_id", tenantID.String()),
				zap.Error(err),
			)
		}
	}
}

// seedOneDefaultPolicy creates one policy, one version, promotes it, and
// assigns it tenant-wide. Idempotent — skips if a policy with the same name
// already exists for this tenant.
func (s *Server) seedOneDefaultPolicy(ctx context.Context, tenantID uuid.UUID, spec defaultPolicySpec) error {
	// Build JSON-DSL rule definition.
	type dslRule struct {
		Framework   string           `json:"framework"`
		Control     string           `json:"control"`
		Severity    string           `json:"severity"`
		Description string           `json:"description"`
		Conditions  []map[string]any `json:"conditions"`
		Remediation string           `json:"remediation"`
	}
	rule := dslRule{
		Framework:   spec.Framework,
		Control:     spec.Control,
		Severity:    spec.Severity,
		Description: spec.Description,
		Conditions:  spec.Conditions,
		Remediation: spec.Remediation,
	}
	ruleJSON, err := json.Marshal(rule)
	if err != nil {
		return fmt.Errorf("marshal rule: %w", err)
	}

	desc := spec.Description
	policy, err := s.store.CreatePolicy(ctx, storage.CreatePolicyParams{
		TenantID:    tenantID,
		Name:        spec.Name,
		Description: &desc,
		RuleType:    "json-dsl",
		Enabled:     true,
		Labels:      map[string]string{defaultPolicyLabel: "true"},
	})
	if err != nil {
		return fmt.Errorf("create policy: %w", err)
	}

	version, err := s.store.CreatePolicyVersion(ctx, storage.CreatePolicyVersionParams{
		PolicyID:       policy.ID,
		RuleDefinition: string(ruleJSON),
		Metadata:       map[string]any{"seeded_by": "control-one-defaults"},
	})
	if err != nil {
		return fmt.Errorf("create policy version: %w", err)
	}

	if _, err := s.store.PromotePolicyVersion(ctx, policy.ID, version.Version); err != nil {
		return fmt.Errorf("promote policy version: %w", err)
	}

	if _, err := s.store.CreatePolicyAssignment(ctx, storage.CreatePolicyAssignmentParams{
		PolicyID: policy.ID,
		TenantID: tenantID,
		// NodeID = uuid.Nil → tenant-wide
	}); err != nil {
		return fmt.Errorf("assign policy: %w", err)
	}

	return nil
}
