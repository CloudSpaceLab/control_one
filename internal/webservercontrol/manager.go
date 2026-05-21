package webservercontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/appcatalog"
	"go.uber.org/zap"
)

const (
	defaultManagedRoot = "/var/lib/control-one/webserver"
	defaultLogDir      = "/var/log/control-one"
)

type Manager struct {
	log      *zap.Logger
	adapters map[string]WebServerAdapter
}

func NewManager(log *zap.Logger) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{log: log.Named("webserver-control"), adapters: map[string]WebServerAdapter{}}
	for _, kind := range webserverKinds() {
		m.adapters[kind] = &adapter{kind: kind, log: m.log.Named(kind)}
	}
	return m
}

func (m *Manager) Inventory(ctx context.Context) ([]WebServerInstance, error) {
	var out []WebServerInstance
	for _, kind := range webserverKinds() {
		instances, err := m.adapters[kind].Detect(ctx)
		if err != nil {
			m.log.Debug("webserver detect failed", zap.String("kind", kind), zap.Error(err))
			continue
		}
		out = append(out, instances...)
	}
	return out, nil
}

func webserverKinds() []string {
	return []string{
		"nginx", "apache", "lighttpd", "tomcat", "haproxy",
		"iis", "caddy", "envoy", "traefik", "jetty", "wildfly", "weblogic", "websphere",
	}
}

func (m *Manager) Plan(ctx context.Context, action string, instance WebServerInstance, policy WebPolicy) (ConfigPlan, error) {
	kind := strings.ToLower(strings.TrimSpace(instance.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(policyString(policy.Metadata, "kind")))
	}
	if kind == "" {
		return ConfigPlan{}, errors.New("webserver kind required")
	}
	adapter, ok := m.adapters[kind]
	if !ok {
		return ConfigPlan{}, fmt.Errorf("unsupported webserver kind %q", kind)
	}
	if policy.Mode == "" {
		switch action {
		case "webserver.blocklist_update":
			policy.Mode = "enforce"
		default:
			policy.Mode = "capture"
		}
	}
	plan, err := adapter.Plan(ctx, instance, policy)
	if err != nil {
		return ConfigPlan{}, err
	}
	plan.Action = action
	return plan, nil
}

func (m *Manager) Apply(ctx context.Context, plan ConfigPlan) (ConfigReceipt, error) {
	adapter, ok := m.adapters[strings.ToLower(plan.Instance.Kind)]
	if !ok {
		return ConfigReceipt{}, fmt.Errorf("unsupported webserver kind %q", plan.Instance.Kind)
	}
	return adapter.Apply(ctx, plan)
}

func (m *Manager) Rollback(ctx context.Context, receipt ConfigReceipt) error {
	kind := strings.ToLower(policyString(receipt.Metadata, "kind"))
	if kind == "" {
		kind = strings.ToLower(policyString(receipt.Metadata, "webserver_kind"))
	}
	if kind == "" {
		return errors.New("rollback receipt missing webserver kind")
	}
	adapter, ok := m.adapters[kind]
	if !ok {
		return fmt.Errorf("unsupported webserver kind %q", kind)
	}
	return adapter.Rollback(ctx, receipt)
}

type adapter struct {
	kind string
	log  *zap.Logger
}

func (a *adapter) Detect(ctx context.Context) ([]WebServerInstance, error) {
	if runtime.GOOS == "windows" && a.kind != "iis" {
		return nil, nil
	}
	c := detectionCandidate(a.kind)
	if len(c.binaries) == 0 {
		return nil, nil
	}
	found := false
	for _, bin := range c.binaries {
		if _, err := exec.LookPath(bin); err == nil {
			found = true
			break
		}
	}
	for _, path := range c.configs {
		if fileExists(path) {
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	inst := WebServerInstance{
		Kind:          a.kind,
		Version:       firstLine(commandOutput(ctx, 2*time.Second, c.versionCommand...)),
		ServiceName:   c.serviceName,
		ConfigPath:    firstExisting(c.configs),
		AccessLogPath: firstExisting(c.accessLogs),
		ErrorLogPath:  firstExisting(c.errorLogs),
		Capabilities: map[string]any{
			"capture":             true,
			"capture_supported":   true,
			"enforce":             enforceSupported(a.kind),
			"enforce_supported":   enforceSupported(a.kind),
			"blocklist_supported": enforceSupported(a.kind),
			"managed_hook":        managedHookSupported(a.kind),
			"validate":            len(c.validateCommand) > 0,
			"reload":              len(c.reloadCommand) > 0,
			"restart_risky":       restartRisky(a.kind),
		},
	}
	enrichDetectedInstance(&inst)
	if inst.Version == "" {
		inst.Version = "unknown"
	}
	if a.kind == "apache" {
		if modules := apacheLoadedModules(ctx, c.binaries); len(modules) > 0 {
			inst.Capabilities["loaded_modules"] = modules
		}
	}
	return []WebServerInstance{inst}, nil
}

func (a *adapter) Plan(_ context.Context, instance WebServerInstance, desired WebPolicy) (ConfigPlan, error) {
	instance.Kind = strings.ToLower(strings.TrimSpace(instance.Kind))
	if instance.Kind == "" {
		instance.Kind = a.kind
	}
	if desired.ManagedDir == "" {
		desired.ManagedDir = filepath.Join(defaultManagedRoot, a.kind)
	}
	if desired.LogDir == "" {
		desired.LogDir = defaultLogDir
	}
	c := detectionCandidate(a.kind)
	if instance.ServiceName == "" {
		instance.ServiceName = c.serviceName
	}
	if instance.ConfigPath == "" {
		instance.ConfigPath = firstExisting(c.configs)
	}
	plan := ConfigPlan{
		Mode:                normalizeMode(desired.Mode),
		Instance:            instance,
		ManagedDir:          desired.ManagedDir,
		LogDir:              desired.LogDir,
		ConfigHookStrategy:  normalizeConfigHookStrategy(desired.ConfigHookStrategy),
		HealthCheckURL:      strings.TrimSpace(desired.HealthCheckURL),
		ValidationCommand:   c.validateCommand,
		ReloadCommand:       reloadCommand(c.serviceName, c.reloadCommand),
		EstimatedBlockCount: len(desired.BlockCIDRs),
	}
	if plan.Mode == "enforce" && desired.MaxBlockChanges > 0 && len(desired.BlockCIDRs) > desired.MaxBlockChanges {
		return ConfigPlan{}, fmt.Errorf("block update exceeds max_block_changes (%d > %d)", len(desired.BlockCIDRs), desired.MaxBlockChanges)
	}
	switch a.kind {
	case "nginx":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one.conf"), Content: nginxCaptureSnippet(desired), Mode: 0o644})
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-blocklist.conf"), Content: nginxBlocklistSnippet(desired.BlockCIDRs), Mode: 0o644})
		plan.ConfigHookLine = "include " + filepath.ToSlash(filepath.Join(desired.ManagedDir, "control-one.conf")) + ";"
	case "apache":
		apachePolicy := desired
		if len(apachePolicy.TrustedProxyCIDRs) > 0 && !apacheModuleAvailable(instance, "remoteip_module") {
			apachePolicy.TrustedProxyCIDRs = nil
			plan.Warnings = append(plan.Warnings, "Apache mod_remoteip is not loaded; trusted proxy CIDRs were not written to the managed snippet")
		}
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one.conf"), Content: apacheCaptureSnippet(apachePolicy), Mode: 0o644})
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-blocklist.conf"), Content: apacheBlocklistSnippet(desired.BlockCIDRs), Mode: 0o644})
		plan.ConfigHookLine = "IncludeOptional " + filepath.ToSlash(filepath.Join(desired.ManagedDir, "control-one.conf"))
	case "lighttpd":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one.conf"), Content: lighttpdCaptureSnippet(desired), Mode: 0o644})
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-blocklist.conf"), Content: lighttpdBlocklistSnippet(desired.BlockCIDRs), Mode: 0o644})
		plan.ConfigHookLine = "include \"" + filepath.ToSlash(filepath.Join(desired.ManagedDir, "control-one.conf")) + "\""
	case "tomcat":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-accesslog-valve.xml"), Content: tomcatCaptureSnippet(desired), Mode: 0o644})
		plan.RequiresRestart = true
		plan.RequiresApproval = !desired.Approved || !desired.MaintenanceWindowApproved || !desired.AllowRestart
		plan.Warnings = append(plan.Warnings, "tomcat capture changes require a restart; enforce at the edge proxy when Tomcat is behind nginx/apache/haproxy")
		return plan, nil
	case "haproxy":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one.cfg"), Content: haproxyCaptureSnippet(desired), Mode: 0o644})
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-blocklist.cfg"), Content: haproxyBlocklistSnippet(desired.BlockCIDRs), Mode: 0o644})
		plan.ConfigHookLine = "include " + filepath.ToSlash(filepath.Join(desired.ManagedDir, "control-one.cfg"))
		plan.Warnings = append(plan.Warnings, "HAProxy managed capture must be included in an HTTP frontend/listen context; response header capture is context-sensitive and should be canaried")
	case "caddy":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one.caddy"), Content: caddyCaptureSnippet(desired), Mode: 0o644})
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-blocklist.caddy"), Content: caddyBlocklistSnippet(desired.BlockCIDRs), Mode: 0o644})
		plan.ConfigHookLine = "import " + filepath.ToSlash(filepath.Join(desired.ManagedDir, "control-one.caddy"))
		plan.Warnings = append(plan.Warnings, "Caddy managed capture should be imported inside the intended site block and canaried before fleet rollout")
	case "envoy":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-envoy.yaml"), Content: envoyCaptureSnippet(desired), Mode: 0o644})
		plan.RequiresApproval = true
		plan.Warnings = append(plan.Warnings, "Envoy capture is a manual merge or xDS template change; Control One will not rewrite listener/filter chains automatically")
	case "traefik":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-traefik.yaml"), Content: traefikCaptureSnippet(desired), Mode: 0o644})
		plan.RequiresApproval = true
		plan.Warnings = append(plan.Warnings, "Traefik capture uses file-provider/dynamic configuration; enable the provider and canary the change before rollout")
	case "iis":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-iis.json"), Content: enterpriseCaptureSnippet("iis", desired), Mode: 0o644})
		plan.RequiresApproval = true
		plan.Warnings = append(plan.Warnings, "IIS support is inventory and planning first; apply logging/enforcement through approved PowerShell/appcmd workflow")
	case "jetty", "wildfly", "weblogic", "websphere":
		plan.Files = append(plan.Files, ManagedFile{Path: filepath.Join(desired.ManagedDir, "control-one-"+a.kind+".xml"), Content: enterpriseCaptureSnippet(a.kind, desired), Mode: 0o644})
		plan.RequiresRestart = true
		plan.RequiresApproval = !desired.Approved || !desired.MaintenanceWindowApproved || !desired.AllowRestart
		if plan.RequiresApproval {
			plan.Warnings = append(plan.Warnings, a.kind+" capture changes require explicit approval, a maintenance window, and restart permission")
		}
		plan.Warnings = append(plan.Warnings, "Prefer capture/enforcement at nginx/apache/haproxy/caddy edge when this app server is behind a proxy")
	}
	if instance.ConfigPath == "" {
		plan.RequiresApproval = true
		plan.Warnings = append(plan.Warnings, "no config path detected; managed snippet can be written but include hook cannot be installed automatically")
	} else {
		plan.ConfigHookPath = instance.ConfigPath
		plan.ChecksumBefore = fileChecksum(instance.ConfigPath)
		if plan.ConfigHookLine != "" && !hookPresent(instance.ConfigPath, plan.ConfigHookLine) {
			if plan.ConfigHookStrategy == "append_at_eof" {
				plan.RequiresApproval = !desired.Approved || !desired.AllowConfigHook
				if plan.RequiresApproval {
					plan.Warnings = append(plan.Warnings, "config include hook is missing; explicit approval is required before appending to customer config")
				} else {
					plan.Warnings = append(plan.Warnings, "config include hook will be appended at end of file; use only when the include context is known to be valid")
				}
			} else {
				plan.RequiresApproval = true
				plan.Warnings = append(plan.Warnings, "config include hook is missing; automatic hook insertion is disabled by default, add the include manually or request config_hook_strategy=append_at_eof")
			}
		}
	}
	plan.Diff = plannedDiff(plan)
	return plan, nil
}

func (a *adapter) Validate(ctx context.Context, plan ConfigPlan) error {
	if len(plan.ValidationCommand) == 0 {
		return nil
	}
	return runCommand(ctx, 20*time.Second, plan.ValidationCommand)
}

func (a *adapter) Apply(ctx context.Context, plan ConfigPlan) (ConfigReceipt, error) {
	receipt := ConfigReceipt{
		Action:           plan.Action,
		ChecksumBefore:   plan.ChecksumBefore,
		ValidationStatus: "not_run",
		ReloadStatus:     "not_run",
		Diff:             plan.Diff,
		Warnings:         append([]string(nil), plan.Warnings...),
		Metadata: map[string]any{
			"kind":          plan.Instance.Kind,
			"config_path":   plan.ConfigHookPath,
			"managed_dir":   plan.ManagedDir,
			"log_dir":       plan.LogDir,
			"mode":          plan.Mode,
			"hook_strategy": plan.ConfigHookStrategy,
		},
		CreatedAt: time.Now().UTC(),
	}
	if plan.Mode == "audit" {
		return auditManagedConfig(plan, receipt), nil
	}
	if plan.RequiresApproval {
		receipt.ValidationStatus = "blocked"
		return receipt, errors.New("approval required before applying webserver config plan")
	}
	if runtime.GOOS != "windows" {
		if plan.LogDir != "" {
			_ = os.MkdirAll(plan.LogDir, 0o755)
		}
	}
	snapshotDir := filepath.Join(plan.ManagedDir, "snapshots")
	managedSnapshots, err := snapshotManagedFiles(plan.Files, snapshotDir)
	if err != nil {
		return receipt, err
	}
	if len(managedSnapshots) > 0 {
		receipt.Metadata["managed_snapshots"] = managedSnapshots
	}
	for _, f := range plan.Files {
		if err := writeManagedFile(f); err != nil {
			_ = restoreManagedSnapshots(managedSnapshots)
			return receipt, err
		}
		receipt.ManagedFiles = append(receipt.ManagedFiles, f.Path)
	}
	snapshot := ""
	if plan.ConfigHookPath != "" && plan.ConfigHookLine != "" && !hookPresent(plan.ConfigHookPath, plan.ConfigHookLine) && plan.ConfigHookStrategy == "append_at_eof" {
		var err error
		snapshot, err = snapshotFile(plan.ConfigHookPath, snapshotDir)
		if err != nil {
			_ = restoreManagedSnapshots(managedSnapshots)
			return receipt, err
		}
		if err := appendHook(plan.ConfigHookPath, plan.ConfigHookLine); err != nil {
			_ = restoreManagedSnapshots(managedSnapshots)
			return receipt, err
		}
		receipt.RollbackRef = snapshot
		receipt.Metadata["snapshot_path"] = snapshot
	}
	receipt.ChecksumAfter = fileChecksum(plan.ConfigHookPath)
	if err := a.Validate(ctx, plan); err != nil {
		receipt.ValidationStatus = "failed"
		_ = restorePlanSnapshots(snapshot, plan.ConfigHookPath, managedSnapshots)
		return receipt, fmt.Errorf("validate webserver config: %w", err)
	}
	receipt.ValidationStatus = "passed"
	if len(plan.ReloadCommand) > 0 {
		if err := runCommand(ctx, 30*time.Second, plan.ReloadCommand); err != nil {
			receipt.ReloadStatus = "failed"
			_ = restorePlanSnapshots(snapshot, plan.ConfigHookPath, managedSnapshots)
			return receipt, fmt.Errorf("reload webserver: %w", err)
		}
		receipt.ReloadStatus = "reloaded"
	}
	if plan.HealthCheckURL != "" {
		if err := healthCheck(ctx, plan.HealthCheckURL); err != nil {
			receipt.ReloadStatus = "health_check_failed"
			_ = restorePlanSnapshots(snapshot, plan.ConfigHookPath, managedSnapshots)
			if len(plan.ReloadCommand) > 0 {
				_ = runCommand(ctx, 30*time.Second, plan.ReloadCommand)
			}
			return receipt, fmt.Errorf("post-reload health check: %w", err)
		}
	}
	return receipt, nil
}

func (a *adapter) Rollback(ctx context.Context, receipt ConfigReceipt) error {
	configPath := policyString(receipt.Metadata, "config_path")
	snapshotPath := receipt.RollbackRef
	if snapshotPath == "" {
		snapshotPath = policyString(receipt.Metadata, "snapshot_path")
	}
	managedSnapshots := managedSnapshotsFromMetadata(receipt.Metadata)
	restored := false
	if configPath != "" && snapshotPath != "" {
		if err := restoreSnapshot(snapshotPath, configPath); err != nil {
			return err
		}
		restored = true
	}
	if len(managedSnapshots) > 0 {
		if err := restoreManagedSnapshots(managedSnapshots); err != nil {
			return err
		}
		restored = true
	}
	if !restored {
		return errors.New("rollback requires config snapshot or managed file snapshots")
	}
	c := detectionCandidate(a.kind)
	if cmd := reloadCommand(c.serviceName, c.reloadCommand); len(cmd) > 0 {
		return runCommand(ctx, 30*time.Second, cmd)
	}
	return nil
}

func auditManagedConfig(plan ConfigPlan, receipt ConfigReceipt) ConfigReceipt {
	drift := make([]map[string]any, 0)
	for _, f := range plan.Files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}
		desired := checksumBytes([]byte(f.Content))
		actual := fileChecksum(path)
		switch {
		case actual == "":
			drift = append(drift, map[string]any{
				"type":   "managed_file_missing",
				"path":   path,
				"wanted": desired,
			})
		case desired != actual:
			drift = append(drift, map[string]any{
				"type":   "managed_file_modified",
				"path":   path,
				"wanted": desired,
				"actual": actual,
			})
		}
		receipt.ManagedFiles = append(receipt.ManagedFiles, path)
	}
	if plan.ConfigHookPath != "" && plan.ConfigHookLine != "" && !hookPresent(plan.ConfigHookPath, plan.ConfigHookLine) {
		drift = append(drift, map[string]any{
			"type": "include_hook_missing",
			"path": plan.ConfigHookPath,
			"line": plan.ConfigHookLine,
		})
	}
	receipt.ValidationStatus = "passed"
	receipt.ReloadStatus = "not_required"
	receipt.ChecksumAfter = fileChecksum(plan.ConfigHookPath)
	receipt.Metadata["audit"] = true
	receipt.Metadata["drift_detected"] = len(drift) > 0
	receipt.Metadata["drift"] = drift
	return receipt
}

type candidate struct {
	binaries        []string
	versionCommand  []string
	validateCommand []string
	reloadCommand   []string
	serviceName     string
	configs         []string
	accessLogs      []string
	errorLogs       []string
}

func detectionCandidate(kind string) candidate {
	switch kind {
	case "nginx":
		return candidate{
			binaries:        []string{"nginx"},
			versionCommand:  []string{"nginx", "-v"},
			validateCommand: []string{"nginx", "-t"},
			reloadCommand:   []string{"nginx", "-s", "reload"},
			serviceName:     "nginx",
			configs:         []string{"/etc/nginx/nginx.conf", "/usr/local/etc/nginx/nginx.conf"},
			accessLogs:      []string{"/var/log/nginx/access.log", "/usr/local/var/log/nginx/access.log"},
			errorLogs:       []string{"/var/log/nginx/error.log", "/usr/local/var/log/nginx/error.log"},
		}
	case "apache":
		return candidate{
			binaries:        []string{"apachectl", "apache2ctl", "httpd"},
			versionCommand:  []string{"apachectl", "-v"},
			validateCommand: []string{"apachectl", "configtest"},
			reloadCommand:   []string{"apachectl", "graceful"},
			serviceName:     "apache2",
			configs:         []string{"/etc/apache2/apache2.conf", "/etc/httpd/conf/httpd.conf", "/usr/local/etc/httpd/httpd.conf"},
			accessLogs:      []string{"/var/log/apache2/access.log", "/var/log/httpd/access_log"},
			errorLogs:       []string{"/var/log/apache2/error.log", "/var/log/httpd/error_log"},
		}
	case "lighttpd":
		return candidate{
			binaries:        []string{"lighttpd"},
			versionCommand:  []string{"lighttpd", "-v"},
			validateCommand: []string{"lighttpd", "-tt", "-f", "/etc/lighttpd/lighttpd.conf"},
			reloadCommand:   []string{"systemctl", "reload", "lighttpd"},
			serviceName:     "lighttpd",
			configs:         []string{"/etc/lighttpd/lighttpd.conf", "/usr/local/etc/lighttpd/lighttpd.conf"},
			accessLogs:      []string{"/var/log/lighttpd/access.log"},
			errorLogs:       []string{"/var/log/lighttpd/error.log"},
		}
	case "tomcat":
		return candidate{
			binaries:    []string{"catalina.sh"},
			serviceName: "tomcat",
			configs:     []string{"/etc/tomcat/server.xml", "/etc/tomcat9/server.xml", "/opt/tomcat/conf/server.xml"},
			accessLogs:  []string{"/var/log/tomcat/localhost_access_log.txt", "/opt/tomcat/logs/localhost_access_log.txt"},
			errorLogs:   []string{"/var/log/tomcat/catalina.out", "/opt/tomcat/logs/catalina.out"},
		}
	case "haproxy":
		return candidate{
			binaries:        []string{"haproxy"},
			versionCommand:  []string{"haproxy", "-v"},
			validateCommand: []string{"haproxy", "-c", "-f", "/etc/haproxy/haproxy.cfg"},
			reloadCommand:   []string{"systemctl", "reload", "haproxy"},
			serviceName:     "haproxy",
			configs:         []string{"/etc/haproxy/haproxy.cfg", "/usr/local/etc/haproxy/haproxy.cfg"},
			accessLogs:      []string{"/var/log/haproxy.log"},
			errorLogs:       []string{"/var/log/haproxy.log"},
		}
	case "caddy":
		return candidate{
			binaries:        []string{"caddy"},
			versionCommand:  []string{"caddy", "version"},
			validateCommand: []string{"caddy", "validate", "--config", "/etc/caddy/Caddyfile"},
			reloadCommand:   []string{"systemctl", "reload", "caddy"},
			serviceName:     "caddy",
			configs:         []string{"/etc/caddy/Caddyfile", "/usr/local/etc/caddy/Caddyfile"},
			accessLogs:      []string{"/var/log/caddy/access.log"},
			errorLogs:       []string{"/var/log/caddy/error.log"},
		}
	case "envoy":
		return candidate{
			binaries:        []string{"envoy"},
			versionCommand:  []string{"envoy", "--version"},
			validateCommand: []string{"envoy", "--mode", "validate", "-c", "/etc/envoy/envoy.yaml"},
			reloadCommand:   []string{"systemctl", "reload", "envoy"},
			serviceName:     "envoy",
			configs:         []string{"/etc/envoy/envoy.yaml", "/etc/envoy/envoy.yml"},
			accessLogs:      []string{"/var/log/envoy/access.log"},
			errorLogs:       []string{"/var/log/envoy/envoy.log"},
		}
	case "traefik":
		return candidate{
			binaries:       []string{"traefik"},
			versionCommand: []string{"traefik", "version"},
			reloadCommand:  []string{"systemctl", "reload", "traefik"},
			serviceName:    "traefik",
			configs:        []string{"/etc/traefik/traefik.yml", "/etc/traefik/traefik.yaml", "/etc/traefik/traefik.toml"},
			accessLogs:     []string{"/var/log/traefik/access.log"},
			errorLogs:      []string{"/var/log/traefik/traefik.log"},
		}
	case "jetty":
		return candidate{
			binaries:    []string{"jetty.sh", "java"},
			serviceName: "jetty",
			configs:     []string{"/etc/jetty/jetty.xml", "/opt/jetty/etc/jetty.xml"},
			accessLogs:  []string{"/var/log/jetty/access.log", "/opt/jetty/logs/access.log"},
			errorLogs:   []string{"/var/log/jetty/jetty.log", "/opt/jetty/logs/jetty.log"},
		}
	case "wildfly":
		return candidate{
			binaries:    []string{"standalone.sh", "jboss-cli.sh", "java"},
			serviceName: "wildfly",
			configs:     []string{"/opt/wildfly/standalone/configuration/standalone.xml", "/etc/wildfly/standalone.xml"},
			accessLogs:  []string{"/opt/wildfly/standalone/log/access.log"},
			errorLogs:   []string{"/opt/wildfly/standalone/log/server.log", "/var/log/wildfly/server.log"},
		}
	case "weblogic":
		return candidate{
			binaries:    []string{"startWebLogic.sh", "java"},
			serviceName: "weblogic",
			configs:     []string{"/u01/oracle/user_projects/domains/base_domain/config/config.xml", "/opt/oracle/user_projects/domains/base_domain/config/config.xml"},
			accessLogs:  []string{"/u01/oracle/user_projects/domains/base_domain/servers/AdminServer/logs/access.log"},
			errorLogs:   []string{"/u01/oracle/user_projects/domains/base_domain/servers/AdminServer/logs/AdminServer.log"},
		}
	case "websphere":
		return candidate{
			binaries:    []string{"serverStatus.sh", "wsadmin.sh", "java"},
			serviceName: "websphere",
			configs:     []string{"/opt/IBM/WebSphere/AppServer/profiles/AppSrv01/config/cells", "/opt/IBM/WebSphere/AppServer/profiles/AppSrv01/config/serverindex.xml"},
			accessLogs:  []string{"/opt/IBM/WebSphere/AppServer/profiles/AppSrv01/logs/server1/http_access.log"},
			errorLogs:   []string{"/opt/IBM/WebSphere/AppServer/profiles/AppSrv01/logs/server1/SystemOut.log"},
		}
	case "iis":
		return candidate{
			binaries:    []string{"appcmd.exe", "powershell.exe"},
			serviceName: "w3svc",
			configs:     []string{"C:/Windows/System32/inetsrv/config/applicationHost.config"},
			accessLogs:  []string{"C:/inetpub/logs/LogFiles/W3SVC1/u_ex*.log"},
			errorLogs:   []string{"C:/Windows/System32/LogFiles/HTTPERR/httperr*.log"},
		}
	default:
		return candidate{}
	}
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "enforce", "rollback", "audit", "observe":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "capture"
	}
}

func normalizeConfigHookStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "append", "append_at_eof", "append-at-eof", "legacy_append":
		return "append_at_eof"
	default:
		return "manual"
	}
}

func enforceSupported(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "nginx", "apache", "lighttpd", "haproxy", "caddy", "envoy", "traefik":
		return true
	default:
		return false
	}
}

func managedHookSupported(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "nginx", "apache", "lighttpd", "haproxy", "caddy":
		return true
	default:
		return false
	}
}

func restartRisky(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "tomcat", "jetty", "wildfly", "weblogic", "websphere":
		return true
	default:
		return false
	}
}

func nginxCaptureSnippet(policy WebPolicy) string {
	realIP := ""
	for _, cidr := range cleanCIDRs(policy.TrustedProxyCIDRs) {
		realIP += "set_real_ip_from " + cidr + ";\n"
	}
	if realIP != "" {
		realIP += "real_ip_header X-Forwarded-For;\nreal_ip_recursive on;\n"
	}
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, "nginx-access.json"))
	blocklistPath := filepath.ToSlash(filepath.Join(policy.ManagedDir, "control-one-blocklist.conf"))
	return "# BEGIN Control One managed web telemetry\n" +
		realIP +
		fmt.Sprintf(`log_format control_one_json escape=json '{"ts":"$time_iso8601","remote_ip":"$remote_addr","xff":"$http_x_forwarded_for","method":"$request_method","request":"$request","status":$status,"bytes":$body_bytes_sent,"request_time":$request_time,"upstream_status":"$upstream_status","upstream_response_time":"$upstream_response_time","request_id":"$request_id","x_request_id":"$http_x_request_id","x_correlation_id":"$http_x_correlation_id","sent_http_x_request_id":"$sent_http_x_request_id","sent_http_x_correlation_id":"$sent_http_x_correlation_id","upstream_http_x_request_id":"$upstream_http_x_request_id","host":"$host","server":"$server_name","proxy_host":"$proxy_host","user_agent":"$http_user_agent","referrer":"$http_referer"}';
access_log %s control_one_json;
include %s;
# END Control One managed web telemetry
`, logPath, blocklistPath)
}

func nginxBlocklistSnippet(cidrs []string) string {
	var b strings.Builder
	b.WriteString("# BEGIN Control One managed blocklist\n")
	for _, cidr := range cleanCIDRs(cidrs) {
		b.WriteString("deny ")
		b.WriteString(cidr)
		b.WriteString(";\n")
	}
	b.WriteString("# END Control One managed blocklist\n")
	return b.String()
}

func apacheCaptureSnippet(policy WebPolicy) string {
	var b strings.Builder
	b.WriteString("# BEGIN Control One managed web telemetry\n")
	if len(policy.TrustedProxyCIDRs) > 0 {
		b.WriteString("RemoteIPHeader X-Forwarded-For\n")
		for _, cidr := range cleanCIDRs(policy.TrustedProxyCIDRs) {
			b.WriteString("RemoteIPTrustedProxy ")
			b.WriteString(cidr)
			b.WriteString("\n")
		}
	}
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, "apache-access.json"))
	blocklistPath := filepath.ToSlash(filepath.Join(policy.ManagedDir, "control-one-blocklist.conf"))
	_, _ = fmt.Fprintf(&b, `LogFormat "{\"ts\":\"%%{%%Y-%%m-%%dT%%H:%%M:%%S%%z}t\",\"remote_ip\":\"%%a\",\"xff\":\"%%{X-Forwarded-For}i\",\"method\":\"%%m\",\"request\":\"%%r\",\"status\":%%>s,\"bytes\":%%B,\"duration_us\":%%D,\"request_id\":\"%%{X-Request-ID}i\",\"x_correlation_id\":\"%%{X-Correlation-ID}i\",\"response_request_id\":\"%%{X-Request-ID}o\",\"response_correlation_id\":\"%%{X-Correlation-ID}o\",\"host\":\"%%v\",\"user_agent\":\"%%{User-agent}i\",\"referrer\":\"%%{Referer}i\"}" control_one_json
CustomLog %s control_one_json
IncludeOptional %s
# END Control One managed web telemetry
`, logPath, blocklistPath)
	return b.String()
}

func apacheBlocklistSnippet(cidrs []string) string {
	var b strings.Builder
	b.WriteString("# BEGIN Control One managed blocklist\n<RequireAll>\nRequire all granted\n")
	for _, cidr := range cleanCIDRs(cidrs) {
		b.WriteString("Require not ip ")
		b.WriteString(cidr)
		b.WriteString("\n")
	}
	b.WriteString("</RequireAll>\n# END Control One managed blocklist\n")
	return b.String()
}

func lighttpdCaptureSnippet(policy WebPolicy) string {
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, "lighttpd-access.log"))
	blocklistPath := filepath.ToSlash(filepath.Join(policy.ManagedDir, "control-one-blocklist.conf"))
	return fmt.Sprintf(`# BEGIN Control One managed web telemetry
accesslog.filename = "%s"
include "%s"
# END Control One managed web telemetry
`, logPath, blocklistPath)
}

func lighttpdBlocklistSnippet(cidrs []string) string {
	var b strings.Builder
	b.WriteString("# BEGIN Control One managed blocklist\n")
	for _, cidr := range cleanCIDRs(cidrs) {
		b.WriteString("$HTTP[\"remoteip\"] == \"")
		b.WriteString(cidr)
		b.WriteString("\" { url.access-deny = ( \"\" ) }\n")
	}
	b.WriteString("# END Control One managed blocklist\n")
	return b.String()
}

func tomcatCaptureSnippet(policy WebPolicy) string {
	logDir := filepath.ToSlash(policy.LogDir)
	return `<Valve className="org.apache.catalina.valves.AccessLogValve"
       directory="` + logDir + `"
       prefix="tomcat-access"
       suffix=".json"
       pattern="{&quot;ts&quot;:&quot;%t&quot;,&quot;remote_ip&quot;:&quot;%a&quot;,&quot;method&quot;:&quot;%m&quot;,&quot;request&quot;:&quot;%r&quot;,&quot;status&quot;:%s,&quot;bytes&quot;:%b,&quot;duration&quot;:%D,&quot;request_id&quot;:&quot;%{X-Request-ID}i&quot;,&quot;x_correlation_id&quot;:&quot;%{X-Correlation-ID}i&quot;,&quot;response_request_id&quot;:&quot;%{X-Request-ID}o&quot;,&quot;response_correlation_id&quot;:&quot;%{X-Correlation-ID}o&quot;,&quot;host&quot;:&quot;%v&quot;,&quot;user_agent&quot;:&quot;%{User-Agent}i&quot;,&quot;xff&quot;:&quot;%{X-Forwarded-For}i&quot;}" />
`
}

func haproxyCaptureSnippet(policy WebPolicy) string {
	blocklistPath := filepath.ToSlash(filepath.Join(policy.ManagedDir, "control-one-blocklist.cfg"))
	return fmt.Sprintf(`# BEGIN Control One managed web telemetry
# Include this file from each HTTP frontend/listen section that needs lifecycle capture.
http-request capture req.hdr(X-Forwarded-For) len 256
http-request capture req.hdr(X-Request-ID) len 128
http-request capture req.hdr(X-Correlation-ID) len 128
http-response capture res.hdr(X-Request-ID) len 128
http-response capture res.hdr(X-Correlation-ID) len 128
log-format '{"ts":"%%t","remote_ip":"%%ci","remote_port":%%cp,"frontend":"%%ft","backend":"%%b","server":"%%s","method":"%%HM","path":"%%HP","status":%%ST,"bytes":%%B,"duration_ms":%%TR,"termination_state":"%%tsc","captured_request_headers":"%%hr","captured_response_headers":"%%hs"}'
include %s
# END Control One managed web telemetry
`, blocklistPath)
}

func haproxyBlocklistSnippet(cidrs []string) string {
	var b strings.Builder
	b.WriteString("# BEGIN Control One managed blocklist\n")
	for _, cidr := range cleanCIDRs(cidrs) {
		b.WriteString("acl control_one_blocked_src src ")
		b.WriteString(cidr)
		b.WriteString("\n")
	}
	if len(cleanCIDRs(cidrs)) > 0 {
		b.WriteString("http-request deny deny_status 403 if control_one_blocked_src\n")
	}
	b.WriteString("# END Control One managed blocklist\n")
	return b.String()
}

func caddyCaptureSnippet(policy WebPolicy) string {
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, "caddy-access.json"))
	blocklistPath := filepath.ToSlash(filepath.Join(policy.ManagedDir, "control-one-blocklist.caddy"))
	return fmt.Sprintf(`# BEGIN Control One managed web telemetry
log {
	output file %s
	format json
}
import %s
# END Control One managed web telemetry
`, logPath, blocklistPath)
}

func caddyBlocklistSnippet(cidrs []string) string {
	var b strings.Builder
	b.WriteString("# BEGIN Control One managed blocklist\n")
	cidrs = cleanCIDRs(cidrs)
	if len(cidrs) > 0 {
		b.WriteString("@control_one_blocked remote_ip ")
		b.WriteString(strings.Join(cidrs, " "))
		b.WriteString("\nrespond @control_one_blocked 403\n")
	}
	b.WriteString("# END Control One managed blocklist\n")
	return b.String()
}

func envoyCaptureSnippet(policy WebPolicy) string {
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, "envoy-access.json"))
	return fmt.Sprintf(`# Control One managed Envoy access-log fragment.
# Merge this into the target HTTP connection manager access_log section or xDS template.
access_log:
  - name: envoy.access_loggers.file
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog
      path: %q
      log_format:
        json_format:
          ts: "%%START_TIME%%"
          remote_ip: "%%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%%"
          method: "%%REQ(:METHOD)%%"
          path: "%%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%%"
          status: "%%RESPONSE_CODE%%"
          bytes: "%%BYTES_SENT%%"
          duration_ms: "%%DURATION%%"
          request_id: "%%REQ(X-REQUEST-ID)%%"
          xff: "%%REQ(X-FORWARDED-FOR)%%"
`, logPath)
}

func traefikCaptureSnippet(policy WebPolicy) string {
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, "traefik-access.json"))
	return fmt.Sprintf(`# Control One managed Traefik dynamic/file-provider fragment.
# Keep this in the file provider scope and canary before fleet rollout.
accessLog:
  filePath: %q
  format: json
  fields:
    headers:
      defaultMode: keep
      names:
        X-Forwarded-For: keep
        X-Request-ID: keep
        X-Correlation-ID: keep
`, logPath)
}

func enterpriseCaptureSnippet(kind string, policy WebPolicy) string {
	logPath := filepath.ToSlash(filepath.Join(policy.LogDir, kind+"-access.json"))
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "iis":
		return fmt.Sprintf(`{
  "managed_by": "Control One",
  "kind": "iis",
  "mode": "capture",
  "recommended_log_directory": %q,
  "fields": ["date", "time", "c-ip", "cs-method", "cs-uri-stem", "sc-status", "sc-bytes", "time-taken", "cs(User-Agent)", "cs(Referer)", "cs(X-Forwarded-For)", "cs(X-Request-ID)"],
  "apply": "Use an approved PowerShell/appcmd workflow; Control One does not rewrite applicationHost.config automatically."
}
`, logPath)
	case "jetty":
		return fmt.Sprintf(`<!-- Control One managed Jetty capture reference.
Add a CustomRequestLog/JSON RequestLogWriter through an approved Jetty XML module.
Target log: %s
Required fields: remote_ip, xff, method, request/path, status, bytes, duration, request_id, user_agent.
-->
`, logPath)
	case "wildfly":
		return fmt.Sprintf(`<!-- Control One managed WildFly/JBoss capture reference.
Enable undertow access-log in the target server/host through CLI or approved XML change.
Target log: %s
Required fields: remote_ip, xff, method, request/path, status, bytes, duration, request_id, user_agent.
-->
`, logPath)
	case "weblogic":
		return fmt.Sprintf(`<!-- Control One managed WebLogic capture reference.
Enable HTTP access logging for the target server/virtual host through WLST or approved domain template.
Target log: %s
Required fields: remote_ip, xff, method, request/path, status, bytes, duration, request_id, user_agent.
-->
`, logPath)
	case "websphere":
		return fmt.Sprintf(`<!-- Control One managed WebSphere capture reference.
Enable HTTP access logging/NCSA custom properties through wsadmin or approved cell template.
Target log: %s
Required fields: remote_ip, xff, method, request/path, status, bytes, duration, request_id, user_agent.
-->
`, logPath)
	default:
		return fmt.Sprintf("# Control One managed capture reference for %s\n# Target log: %s\n", kind, logPath)
	}
}

func enrichDetectedInstance(inst *WebServerInstance) {
	if inst == nil {
		return
	}
	if inst.Capabilities == nil {
		inst.Capabilities = map[string]any{}
	}
	kind := strings.ToLower(strings.TrimSpace(inst.Kind))
	inst.Capabilities["edge_proxy"] = kind == "nginx" || kind == "apache" || kind == "lighttpd" || kind == "haproxy" || kind == "caddy" || kind == "envoy" || kind == "traefik"
	inst.Capabilities["response_header_capture"] = kind == "nginx" || kind == "apache" || kind == "tomcat" || kind == "haproxy" || kind == "caddy" || kind == "envoy" || kind == "traefik"
	inst.Capabilities["app_root_detection"] = kind != "haproxy" && kind != "envoy" && kind != "traefik"

	config := ""
	if inst.ConfigPath != "" {
		if data, err := os.ReadFile(inst.ConfigPath); err == nil {
			config = string(data)
		}
	}
	rootHints := applicationRootsFromConfig(kind, config)
	rootHints = append(rootHints, applicationRootsFromFilesystem(rootHints)...)
	apps := make([]map[string]any, 0, len(rootHints))
	for _, hint := range rootHints {
		app := classifyApplicationRoot(hint)
		apps = append(apps, app)
		inst.VHosts = append(inst.VHosts, map[string]any{
			"name":               firstNonEmptyLocal(stringFromAny(app["vhost"]), "default"),
			"document_root":      app["path"],
			"application_type":   app["application_type"],
			"log_skill_status":   app["log_skill_status"],
			"suggested_skill":    app["suggested_skill"],
			"detection_evidence": app["evidence"],
		})
	}
	if len(apps) > 0 {
		inst.Capabilities["application_roots"] = apps
	}
	if kind == "haproxy" && config != "" {
		inst.Capabilities["load_balancer_backends"] = haproxyBackends(config)
	}
	inst.Capabilities["server_purposes"] = inferWebserverPurposes(kind, config, apps)
}

func applicationRootsFromConfig(kind, config string) []map[string]any {
	if strings.TrimSpace(config) == "" {
		return nil
	}
	var patterns []struct {
		re        *regexp.Regexp
		directive string
	}
	switch kind {
	case "nginx":
		patterns = []struct {
			re        *regexp.Regexp
			directive string
		}{
			{regexp.MustCompile(`(?mi)\broot\s+([^;\s]+)\s*;`), "root"},
			{regexp.MustCompile(`(?mi)\balias\s+([^;\s]+)\s*;`), "alias"},
		}
	case "apache":
		patterns = []struct {
			re        *regexp.Regexp
			directive string
		}{
			{regexp.MustCompile(`(?mi)^\s*DocumentRoot\s+"?([^"\s]+)"?`), "DocumentRoot"},
			{regexp.MustCompile(`(?mi)^\s*Alias\s+\S+\s+"?([^"\s]+)"?`), "Alias"},
		}
	case "lighttpd":
		patterns = []struct {
			re        *regexp.Regexp
			directive string
		}{
			{regexp.MustCompile(`(?mi)^\s*server\.document-root\s*=\s*"([^"]+)"`), "server.document-root"},
		}
	case "tomcat":
		patterns = []struct {
			re        *regexp.Regexp
			directive string
		}{
			{regexp.MustCompile(`(?i)\bappBase\s*=\s*"([^"]+)"`), "appBase"},
			{regexp.MustCompile(`(?i)\bdocBase\s*=\s*"([^"]+)"`), "docBase"},
		}
	case "caddy":
		patterns = []struct {
			re        *regexp.Regexp
			directive string
		}{
			{regexp.MustCompile(`(?mi)^\s*root\s+(?:\*\s+)?([^\s{]+)`), "root"},
		}
	case "jetty", "wildfly", "weblogic", "websphere":
		patterns = []struct {
			re        *regexp.Regexp
			directive string
		}{
			{regexp.MustCompile(`(?i)\b(?:docBase|appBase|war|path)\s*=\s*"([^"]+)"`), "deployment"},
		}
	}
	vhost := firstNonEmptyLocal(firstRegexpGroup(config, regexp.MustCompile(`(?mi)^\s*server_name\s+([^;]+);`)), firstRegexpGroup(config, regexp.MustCompile(`(?mi)^\s*ServerName\s+(\S+)`)))
	var out []map[string]any
	seen := map[string]struct{}{}
	for _, p := range patterns {
		for _, match := range p.re.FindAllStringSubmatch(config, -1) {
			if len(match) < 2 {
				continue
			}
			path := strings.Trim(strings.TrimSpace(match[1]), `"`)
			if path == "" || strings.Contains(path, "$") {
				continue
			}
			key := strings.ToLower(path)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, map[string]any{
				"path":      filepath.Clean(path),
				"directive": p.directive,
				"vhost":     vhost,
			})
		}
	}
	return out
}

func applicationRootsFromFilesystem(existing []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	for _, row := range existing {
		if path := stringFromAny(row["path"]); path != "" {
			seen[strings.ToLower(filepath.Clean(path))] = struct{}{}
		}
	}

	var out []map[string]any
	add := func(path, source string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == string(filepath.Separator) {
			return
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			return
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return
		}
		detected := appcatalog.DetectRootWithFS(path, fileExists, readFileForDetection)
		if detected.ProfileID == "unknown" && !directoryHasAppRootMarker(path) {
			return
		}
		seen[key] = struct{}{}
		out = append(out, map[string]any{
			"path":      path,
			"directive": "filesystem_scan",
			"vhost":     source,
		})
	}

	for _, root := range []string{"/var/www/html", "/var/www", "/srv/www", "/srv/http", "/usr/share/nginx/html", "/usr/local/www", "/opt/apps", "/opt/app"} {
		add(root, "common-web-root")
	}
	for _, parent := range []string{"/var/www", "/srv/www", "/srv/http"} {
		for _, child := range boundedChildDirectories(parent, 2, 40) {
			add(child, "common-web-root")
		}
	}
	return out
}

func boundedChildDirectories(root string, maxDepth, maxCount int) []string {
	root = filepath.Clean(strings.TrimSpace(root))
	if maxDepth <= 0 || maxCount <= 0 {
		return nil
	}
	var out []string
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if len(out) >= maxCount || depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if len(out) >= maxCount {
				return
			}
			if !entry.IsDir() || skipAppRootDirName(entry.Name()) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			out = append(out, path)
			walk(path, depth+1)
		}
	}
	walk(root, 1)
	return out
}

func skipAppRootDirName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "cache", "tmp", "logs", "log", "run", "sessions", "uploads":
		return true
	}
	return false
}

func directoryHasAppRootMarker(root string) bool {
	for _, marker := range []string{
		"index.html", "index.htm", "package.json", "composer.json", "requirements.txt", "pyproject.toml",
		"manage.py", "Gemfile", "config.ru", "artisan", "go.mod", "pom.xml", "build.gradle", "web.config",
	} {
		if fileExists(filepath.Join(root, marker)) {
			return true
		}
	}
	return false
}

func classifyApplicationRoot(hint map[string]any) map[string]any {
	path := stringFromAny(hint["path"])
	detected := appcatalog.DetectRootWithFS(path, fileExists, readFileForDetection)
	appType := detected.ProfileID
	if appType == "" {
		appType = "unknown"
	}
	status := detected.CoverageState
	skill := firstNonEmptyLocal(detected.ParserProfileID, detected.RemediationSkillID, "custom-parser-profile")
	evidence := append([]string{"webserver_config:" + stringFromAny(hint["directive"])}, detected.Evidence...)
	out := map[string]any{
		"path":                  path,
		"directive":             hint["directive"],
		"vhost":                 hint["vhost"],
		"application_type":      appType,
		"application_name":      detected.Name,
		"application_category":  detected.Category,
		"catalog_version":       detected.CatalogVersion,
		"stack_tags":            detected.StackTags,
		"confidence":            detected.Confidence,
		"evidence":              evidence,
		"log_skill_status":      status,
		"suggested_skill":       skill,
		"parser_profile_id":     detected.ParserProfileID,
		"remediation_skill_id":  detected.RemediationSkillID,
		"coverage_state":        detected.CoverageState,
		"auto_remediation_note": "install or assign this application log skill when no matching local parser profile exists",
	}
	if _, err := os.Stat(path); err != nil {
		out["path_status"] = "unverified"
		out["confidence"] = 30
	} else {
		out["path_status"] = "exists"
	}
	return out
}

func readFileForDetection(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	return data, err == nil
}

func inferWebserverPurposes(kind, config string, apps []map[string]any) []string {
	seen := map[string]struct{}{}
	add := func(v string) {
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	switch kind {
	case "haproxy", "envoy", "traefik":
		add("load_balancer")
	case "tomcat", "jetty", "wildfly", "weblogic", "websphere", "iis":
		add("app_node")
	default:
		add("web_server")
	}
	lower := strings.ToLower(config)
	if strings.Contains(lower, "proxy_pass") || strings.Contains(lower, "proxypass") || strings.Contains(lower, "upstream ") || strings.Contains(lower, "\nbackend ") || strings.Contains(lower, "loadbalancer") || strings.Contains(lower, "service:") {
		add("load_balancer")
	}
	if len(apps) > 0 {
		add("app_node")
	}
	order := []string{"load_balancer", "web_server", "app_node"}
	out := make([]string, 0, len(seen))
	for _, v := range order {
		if _, ok := seen[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

func haproxyBackends(config string) []map[string]any {
	backendRe := regexp.MustCompile(`(?mi)^\s*backend\s+(\S+)`)
	serverRe := regexp.MustCompile(`(?mi)^\s*server\s+(\S+)\s+(\S+)`)
	var out []map[string]any
	current := ""
	for _, line := range strings.Split(config, "\n") {
		if match := backendRe.FindStringSubmatch(line); len(match) == 2 {
			current = match[1]
			out = append(out, map[string]any{"backend": current})
			continue
		}
		if current != "" {
			if match := serverRe.FindStringSubmatch(line); len(match) == 3 {
				out = append(out, map[string]any{"backend": current, "server": match[1], "target": match[2]})
			}
		}
	}
	return out
}

func apacheLoadedModules(ctx context.Context, binaries []string) []string {
	for _, bin := range binaries {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		out := commandOutput(ctx, 2*time.Second, bin, "-M")
		if modules := parseApacheModules(out); len(modules) > 0 {
			return modules
		}
	}
	return nil
}

func parseApacheModules(out string) []string {
	var modules []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "loaded modules") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		module := strings.TrimSpace(fields[0])
		if module == "" {
			continue
		}
		if _, ok := seen[module]; ok {
			continue
		}
		seen[module] = struct{}{}
		modules = append(modules, module)
	}
	return modules
}

func apacheModuleAvailable(instance WebServerInstance, module string) bool {
	module = strings.ToLower(strings.TrimSpace(module))
	if module == "" || instance.Capabilities == nil {
		return false
	}
	raw, ok := instance.Capabilities["loaded_modules"]
	if !ok {
		return false
	}
	switch mods := raw.(type) {
	case []string:
		for _, m := range mods {
			if strings.EqualFold(strings.TrimSpace(m), module) {
				return true
			}
		}
	case []any:
		for _, m := range mods {
			if strings.EqualFold(stringFromAny(m), module) {
				return true
			}
		}
	}
	return false
}

func firstRegexpGroup(s string, re *regexp.Regexp) string {
	match := re.FindStringSubmatch(s)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstNonEmptyLocal(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func plannedDiff(plan ConfigPlan) string {
	var b strings.Builder
	for _, f := range plan.Files {
		b.WriteString("write managed file ")
		b.WriteString(f.Path)
		b.WriteString("\n")
	}
	if plan.ConfigHookPath != "" && plan.ConfigHookLine != "" && !hookPresent(plan.ConfigHookPath, plan.ConfigHookLine) {
		if plan.ConfigHookStrategy == "append_at_eof" {
			b.WriteString("append managed include hook to ")
			b.WriteString(plan.ConfigHookPath)
			b.WriteString(": ")
			b.WriteString(plan.ConfigHookLine)
			b.WriteString("\n")
		} else {
			b.WriteString("requires managed include hook in ")
			b.WriteString(plan.ConfigHookPath)
			b.WriteString(": ")
			b.WriteString(plan.ConfigHookLine)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func reloadCommand(serviceName string, fallback []string) []string {
	if runtime.GOOS != "linux" {
		return fallback
	}
	if serviceName != "" {
		if _, err := exec.LookPath("systemctl"); err == nil {
			return []string{"systemctl", "reload", serviceName}
		}
	}
	return fallback
}

func writeManagedFile(f ManagedFile) error {
	mode := os.FileMode(f.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f.Path, []byte(f.Content), mode)
}

func appendHook(path, line string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), line) {
		return nil
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	block := "\n# BEGIN Control One managed include\n" + line + "\n# END Control One managed include\n"
	return os.WriteFile(path, append(data, []byte(block)...), mode)
}

type managedFileSnapshot struct {
	Path     string `json:"path"`
	Snapshot string `json:"snapshot,omitempty"`
	Existed  bool   `json:"existed"`
	Mode     uint32 `json:"mode,omitempty"`
}

func snapshotManagedFiles(files []ManagedFile, snapshotDir string) ([]managedFileSnapshot, error) {
	out := make([]managedFileSnapshot, 0, len(files))
	for _, f := range files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}
		snap := managedFileSnapshot{Path: path}
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			snap.Existed = true
			snap.Mode = uint32(st.Mode().Perm())
			snapshot, err := snapshotFile(path, snapshotDir)
			if err != nil {
				return out, err
			}
			snap.Snapshot = snapshot
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return out, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func restorePlanSnapshots(configSnapshot, configPath string, managedSnapshots []managedFileSnapshot) error {
	var errs []error
	if err := restoreSnapshot(configSnapshot, configPath); err != nil {
		errs = append(errs, err)
	}
	if err := restoreManagedSnapshots(managedSnapshots); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func restoreManagedSnapshots(snapshots []managedFileSnapshot) error {
	var errs []error
	for _, snap := range snapshots {
		if strings.TrimSpace(snap.Path) == "" {
			continue
		}
		if !snap.Existed {
			if err := os.Remove(snap.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		data, err := os.ReadFile(snap.Snapshot)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(snap.Path), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		mode := os.FileMode(snap.Mode)
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(snap.Path, data, mode); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func managedSnapshotsFromMetadata(metadata map[string]any) []managedFileSnapshot {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["managed_snapshots"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []managedFileSnapshot
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func snapshotFile(path, snapshotDir string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Base(path) + "." + time.Now().UTC().Format("20060102T150405Z") + ".bak"
	snapshot := filepath.Join(snapshotDir, name)
	if err := os.WriteFile(snapshot, data, 0o600); err != nil {
		return "", err
	}
	return snapshot, nil
}

func restoreSnapshot(snapshot, dest string) error {
	if snapshot == "" || dest == "" {
		return nil
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(dest); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(dest, data, mode)
}

func runCommand(ctx context.Context, timeout time.Duration, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if _, err := exec.LookPath(args[0]); err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, args[0], args[1:]...) // #nosec G204 -- commands are adapter-defined.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func commandOutput(ctx context.Context, timeout time.Duration, args ...string) string {
	if len(args) == 0 {
		return ""
	}
	if _, err := exec.LookPath(args[0]); err != nil {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, args[0], args[1:]...) // #nosec G204 -- commands are adapter-defined.
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func healthCheck(ctx context.Context, rawURL string) error {
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

func hookPresent(path, line string) bool {
	if path == "" || line == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), line)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func cleanCIDRs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func fileChecksum(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return checksumBytes(data)
}

func checksumBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func policyString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
