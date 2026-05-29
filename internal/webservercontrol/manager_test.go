package webservercontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestPlanRequiresManualHookByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "nginx.conf")
	original := "events {}\nhttp {}\n"
	if err := os.WriteFile(configPath, []byte(original), 0o640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_apply", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir:      filepath.Join(dir, "managed"),
		LogDir:          filepath.Join(dir, "logs"),
		Approved:        true,
		AllowConfigHook: true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.RequiresApproval {
		t.Fatalf("expected missing include hook to require approval by default")
	}
	if plan.ConfigHookStrategy != "manual" {
		t.Fatalf("ConfigHookStrategy = %q, want manual", plan.ConfigHookStrategy)
	}
	if !strings.Contains(plan.Diff, "requires managed include hook") {
		t.Fatalf("diff did not explain manual hook requirement: %q", plan.Diff)
	}

	receipt, err := mgr.Apply(context.Background(), plan)
	if err == nil {
		t.Fatalf("expected apply to be blocked")
	}
	if receipt.ValidationStatus != "blocked" {
		t.Fatalf("ValidationStatus = %q, want blocked", receipt.ValidationStatus)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != original {
		t.Fatalf("config changed despite manual hook requirement:\n%s", string(after))
	}
}

func TestApplyAppendHookOnlyWhenExplicit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "nginx.conf")
	original := "events {}\nhttp {}\n"
	if err := os.WriteFile(configPath, []byte(original), 0o640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	beforeMode := filePerm(t, configPath)

	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_apply", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir:         filepath.Join(dir, "managed"),
		LogDir:             filepath.Join(dir, "logs"),
		Approved:           true,
		AllowConfigHook:    true,
		ConfigHookStrategy: "append_at_eof",
		TrustedProxyCIDRs:  []string{"10.0.0.0/8"},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.RequiresApproval {
		t.Fatalf("expected explicit append strategy with approval to be applicable: %v", plan.Warnings)
	}
	if !strings.Contains(plan.Diff, "append managed include hook") {
		t.Fatalf("diff did not describe append hook: %q", plan.Diff)
	}

	plan.ValidationCommand = nil
	plan.ReloadCommand = nil
	receipt, err := mgr.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if receipt.RollbackRef == "" {
		t.Fatalf("expected rollback snapshot")
	}
	if receipt.ValidationStatus != "passed" {
		t.Fatalf("ValidationStatus = %q, want passed", receipt.ValidationStatus)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(after), plan.ConfigHookLine) {
		t.Fatalf("config missing appended hook %q:\n%s", plan.ConfigHookLine, string(after))
	}
	if mode := filePerm(t, configPath); mode != beforeMode {
		t.Fatalf("config mode = %o, want preserved mode %o", mode, beforeMode)
	}
	if _, err := os.Stat(filepath.Join(plan.ManagedDir, "control-one.conf")); err != nil {
		t.Fatalf("managed capture file missing: %v", err)
	}
}

func TestExistingHookAllowsManagedFileUpdateWithoutConfigMutation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	managedDir := filepath.Join(dir, "managed")
	logDir := filepath.Join(dir, "logs")
	hookLine := "include " + filepath.ToSlash(filepath.Join(managedDir, "control-one.conf")) + ";"
	configPath := filepath.Join(dir, "nginx.conf")
	original := "events {}\nhttp {\n    " + hookLine + "\n}\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.blocklist_update", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir: managedDir,
		LogDir:     logDir,
		BlockCIDRs: []string{"203.0.113.10", "198.51.100.0/24"},
		Approved:   true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.RequiresApproval {
		t.Fatalf("existing hook should not require config approval: %v", plan.Warnings)
	}
	if strings.Contains(plan.Diff, "append managed include hook") || strings.Contains(plan.Diff, "requires managed include hook") {
		t.Fatalf("diff should not include hook work when hook exists: %q", plan.Diff)
	}
	if len(plan.Files) == 0 || !strings.Contains(plan.Files[0].Content, filepath.ToSlash(filepath.Join(logDir, "nginx-access.json"))) {
		t.Fatalf("capture snippet does not honor custom log dir: %#v", plan.Files)
	}
	if len(plan.Files) == 0 || !strings.Contains(plan.Files[0].Content, filepath.ToSlash(filepath.Join(managedDir, "control-one-blocklist.conf"))) {
		t.Fatalf("capture snippet does not honor custom managed dir: %#v", plan.Files)
	}

	plan.ValidationCommand = nil
	plan.ReloadCommand = nil
	receipt, err := mgr.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if receipt.RollbackRef != "" {
		t.Fatalf("did not expect config snapshot when hook already exists: %q", receipt.RollbackRef)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != original {
		t.Fatalf("config changed despite existing hook:\n%s", string(after))
	}
	blocklist, err := os.ReadFile(filepath.Join(managedDir, "control-one-blocklist.conf"))
	if err != nil {
		t.Fatalf("read blocklist: %v", err)
	}
	if !strings.Contains(string(blocklist), "deny 203.0.113.10;") || !strings.Contains(string(blocklist), "deny 198.51.100.0/24;") {
		t.Fatalf("blocklist missing CIDRs:\n%s", string(blocklist))
	}
}

func TestApplyRestoresManagedFilesWhenValidationFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	managedDir := filepath.Join(dir, "managed")
	logDir := filepath.Join(dir, "logs")
	hookLine := "include " + filepath.ToSlash(filepath.Join(managedDir, "control-one.conf")) + ";"
	configPath := filepath.Join(dir, "nginx.conf")
	originalConfig := "events {}\nhttp {\n    " + hookLine + "\n}\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	capturePath := filepath.Join(managedDir, "control-one.conf")
	blocklistPath := filepath.Join(managedDir, "control-one-blocklist.conf")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	originalCapture := "# previous capture\n"
	originalBlocklist := "# previous blocklist\n"
	if err := os.WriteFile(capturePath, []byte(originalCapture), 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	if err := os.WriteFile(blocklistPath, []byte(originalBlocklist), 0o640); err != nil {
		t.Fatalf("write blocklist: %v", err)
	}
	originalCaptureMode := filePerm(t, capturePath)
	originalBlocklistMode := filePerm(t, blocklistPath)

	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.blocklist_update", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir: managedDir,
		LogDir:     logDir,
		BlockCIDRs: []string{"203.0.113.10"},
		Approved:   true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.RequiresApproval {
		t.Fatalf("existing hook should not require approval: %v", plan.Warnings)
	}
	plan.ValidationCommand = []string{"control-one-definitely-missing-validator"}
	plan.ReloadCommand = nil

	receipt, err := mgr.Apply(context.Background(), plan)
	if err == nil {
		t.Fatalf("expected validation failure")
	}
	if receipt.ValidationStatus != "failed" {
		t.Fatalf("ValidationStatus = %q, want failed", receipt.ValidationStatus)
	}
	assertFileContent(t, configPath, originalConfig)
	assertFileContent(t, capturePath, originalCapture)
	assertFileContent(t, blocklistPath, originalBlocklist)
	if mode := filePerm(t, capturePath); mode != originalCaptureMode {
		t.Fatalf("capture mode = %o, want %o", mode, originalCaptureMode)
	}
	if mode := filePerm(t, blocklistPath); mode != originalBlocklistMode {
		t.Fatalf("blocklist mode = %o, want %o", mode, originalBlocklistMode)
	}
}

func TestApplyRestoresManagedFilesWhenReloadFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	managedDir := filepath.Join(dir, "managed")
	logDir := filepath.Join(dir, "logs")
	hookLine := "include " + filepath.ToSlash(filepath.Join(managedDir, "control-one.conf")) + ";"
	configPath := filepath.Join(dir, "nginx.conf")
	originalConfig := "events {}\nhttp {\n    " + hookLine + "\n}\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	capturePath := filepath.Join(managedDir, "control-one.conf")
	blocklistPath := filepath.Join(managedDir, "control-one-blocklist.conf")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	originalCapture := "# previous capture\n"
	originalBlocklist := "# previous blocklist\n"
	if err := os.WriteFile(capturePath, []byte(originalCapture), 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	if err := os.WriteFile(blocklistPath, []byte(originalBlocklist), 0o640); err != nil {
		t.Fatalf("write blocklist: %v", err)
	}

	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.blocklist_update", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir: managedDir,
		LogDir:     logDir,
		BlockCIDRs: []string{"203.0.113.10"},
		Approved:   true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	plan.ValidationCommand = nil
	plan.ReloadCommand = []string{"control-one-definitely-missing-reloader"}

	receipt, err := mgr.Apply(context.Background(), plan)
	if err == nil {
		t.Fatalf("expected reload failure")
	}
	if receipt.ReloadStatus != "failed" {
		t.Fatalf("ReloadStatus = %q, want failed", receipt.ReloadStatus)
	}
	assertFileContent(t, configPath, originalConfig)
	assertFileContent(t, capturePath, originalCapture)
	assertFileContent(t, blocklistPath, originalBlocklist)
}

func TestApplyRestoresManagedFilesWhenHealthCheckFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	managedDir := filepath.Join(dir, "managed")
	logDir := filepath.Join(dir, "logs")
	hookLine := "include " + filepath.ToSlash(filepath.Join(managedDir, "control-one.conf")) + ";"
	configPath := filepath.Join(dir, "nginx.conf")
	originalConfig := "events {}\nhttp {\n    " + hookLine + "\n}\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	capturePath := filepath.Join(managedDir, "control-one.conf")
	blocklistPath := filepath.Join(managedDir, "control-one-blocklist.conf")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	originalCapture := "# previous capture\n"
	originalBlocklist := "# previous blocklist\n"
	if err := os.WriteFile(capturePath, []byte(originalCapture), 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	if err := os.WriteFile(blocklistPath, []byte(originalBlocklist), 0o640); err != nil {
		t.Fatalf("write blocklist: %v", err)
	}
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	}))
	defer health.Close()

	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.blocklist_update", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir:     managedDir,
		LogDir:         logDir,
		BlockCIDRs:     []string{"203.0.113.10"},
		Approved:       true,
		HealthCheckURL: health.URL,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	plan.ValidationCommand = nil
	plan.ReloadCommand = nil

	receipt, err := mgr.Apply(context.Background(), plan)
	if err == nil {
		t.Fatalf("expected health check failure")
	}
	if receipt.ReloadStatus != "health_check_failed" {
		t.Fatalf("ReloadStatus = %q, want health_check_failed", receipt.ReloadStatus)
	}
	assertFileContent(t, configPath, originalConfig)
	assertFileContent(t, capturePath, originalCapture)
	assertFileContent(t, blocklistPath, originalBlocklist)
}

func TestPlanHAProxyCaptureIncludesLifecycleHeaders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "haproxy.cfg")
	hookLine := "include " + filepath.ToSlash(filepath.Join(dir, "managed", "control-one.cfg"))
	if err := os.WriteFile(configPath, []byte("global\n\ndefaults\n    mode http\n\nfrontend web\n    bind :443\n    "+hookLine+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:       "haproxy",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir: filepath.Join(dir, "managed"),
		LogDir:     filepath.Join(dir, "logs"),
		Approved:   true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.RequiresApproval {
		t.Fatalf("existing HAProxy hook should not require approval: %v", plan.Warnings)
	}
	if len(plan.Files) == 0 || !strings.Contains(plan.Files[0].Content, "http-response capture res.hdr(X-Request-ID)") {
		t.Fatalf("HAProxy capture snippet missing response header capture:\n%v", plan.Files)
	}
	if !strings.Contains(plan.Files[0].Content, `"captured_response_headers"`) {
		t.Fatalf("HAProxy log format missing response header evidence:\n%s", plan.Files[0].Content)
	}
}

func TestV2CaddyPlanUsesManagedIncludeAndBlocklist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "Caddyfile")
	hookLine := "import " + filepath.ToSlash(filepath.Join(dir, "managed", "control-one.caddy"))
	if err := os.WriteFile(configPath, []byte("example.bank {\n    "+hookLine+"\n}\n"), 0o644); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}
	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:       "caddy",
		ConfigPath: configPath,
	}, WebPolicy{
		ManagedDir: filepath.Join(dir, "managed"),
		LogDir:     filepath.Join(dir, "logs"),
		BlockCIDRs: []string{"203.0.113.10/32"},
		Approved:   true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.RequiresApproval {
		t.Fatalf("existing Caddy import should not require approval: %v", plan.Warnings)
	}
	if len(plan.Files) != 2 || !strings.Contains(plan.Files[0].Content, "format json") || !strings.Contains(plan.Files[1].Content, "remote_ip 203.0.113.10/32") {
		t.Fatalf("unexpected Caddy managed files: %#v", plan.Files)
	}
}

func TestV2ManualEdgeProxyPlanIsApprovalFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "envoy.yaml")
	if err := os.WriteFile(configPath, []byte("static_resources: {}\n"), 0o644); err != nil {
		t.Fatalf("write envoy config: %v", err)
	}
	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:       "envoy",
		ConfigPath: configPath,
	}, WebPolicy{ManagedDir: filepath.Join(dir, "managed"), LogDir: filepath.Join(dir, "logs"), Approved: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.RequiresApproval {
		t.Fatalf("Envoy manual merge must require approval")
	}
	if len(plan.Files) != 1 || !strings.Contains(plan.Files[0].Content, "envoy.access_loggers.file") {
		t.Fatalf("Envoy capture fragment missing: %#v", plan.Files)
	}
	if !strings.Contains(strings.Join(plan.Warnings, " "), "manual merge") {
		t.Fatalf("Envoy warning should explain manual merge: %#v", plan.Warnings)
	}
}

func TestV2EnterpriseAppServerPlanRequiresMaintenanceApproval(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:       "weblogic",
		ConfigPath: filepath.Join(dir, "config.xml"),
	}, WebPolicy{ManagedDir: filepath.Join(dir, "managed"), LogDir: filepath.Join(dir, "logs"), Approved: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.RequiresRestart || !plan.RequiresApproval {
		t.Fatalf("WebLogic plan should require restart and approval: %#v", plan)
	}
	if len(plan.Files) != 1 || !strings.Contains(plan.Files[0].Content, "WebLogic capture reference") {
		t.Fatalf("WebLogic capture reference missing: %#v", plan.Files)
	}

	approved, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:       "weblogic",
		ConfigPath: filepath.Join(dir, "config.xml"),
	}, WebPolicy{
		ManagedDir:                filepath.Join(dir, "managed"),
		LogDir:                    filepath.Join(dir, "logs"),
		Approved:                  true,
		MaintenanceWindowApproved: true,
		AllowRestart:              true,
	})
	if err != nil {
		t.Fatalf("approved plan: %v", err)
	}
	if !approved.RequiresRestart || approved.RequiresApproval {
		t.Fatalf("approved WebLogic plan should keep restart flag but clear approval: %#v", approved)
	}
}

func TestApacheRemoteIPRequiresLoadedModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mgr := NewManager(zap.NewNop())
	withoutModule, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:         "apache",
		ConfigPath:   filepath.Join(dir, "missing.conf"),
		Capabilities: map[string]any{"loaded_modules": []string{"rewrite_module"}},
	}, WebPolicy{
		ManagedDir:        filepath.Join(dir, "managed-a"),
		LogDir:            filepath.Join(dir, "logs"),
		TrustedProxyCIDRs: []string{"10.0.0.0/8"},
		Approved:          true,
	})
	if err != nil {
		t.Fatalf("plan without module: %v", err)
	}
	if strings.Contains(withoutModule.Files[0].Content, "RemoteIPTrustedProxy") {
		t.Fatalf("remoteip directives should be skipped when module is absent:\n%s", withoutModule.Files[0].Content)
	}

	withModule, err := mgr.Plan(context.Background(), "webserver.config_plan", WebServerInstance{
		Kind:         "apache",
		ConfigPath:   filepath.Join(dir, "missing.conf"),
		Capabilities: map[string]any{"loaded_modules": []string{"remoteip_module"}},
	}, WebPolicy{
		ManagedDir:        filepath.Join(dir, "managed-b"),
		LogDir:            filepath.Join(dir, "logs"),
		TrustedProxyCIDRs: []string{"10.0.0.0/8"},
		Approved:          true,
	})
	if err != nil {
		t.Fatalf("plan with module: %v", err)
	}
	if !strings.Contains(withModule.Files[0].Content, "RemoteIPTrustedProxy 10.0.0.0/8") {
		t.Fatalf("remoteip directives missing when module is present:\n%s", withModule.Files[0].Content)
	}
}

func TestApplicationRootDiscoveryClassifiesConfiguredApps(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "core-api")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "package.json"), []byte(`{"name":"core-api"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	configPath := filepath.Join(dir, "nginx.conf")
	config := "events {}\nhttp {\n server { server_name core.example; root " + filepath.ToSlash(appDir) + "; }\n}\n"
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	inst := WebServerInstance{Kind: "nginx", ConfigPath: configPath, Capabilities: map[string]any{}}
	enrichDetectedInstance(&inst)
	apps, ok := inst.Capabilities["application_roots"].([]map[string]any)
	if !ok || len(apps) == 0 {
		t.Fatalf("application roots not discovered: %#v", inst.Capabilities["application_roots"])
	}
	var configuredApp map[string]any
	for _, app := range apps {
		if app["path"] == appDir {
			configuredApp = app
			break
		}
	}
	if configuredApp == nil {
		t.Fatalf("configured application root not discovered: %#v", apps)
	}
	if configuredApp["application_type"] != "nodejs" {
		t.Fatalf("application_type = %#v, want nodejs", configuredApp["application_type"])
	}
	purposes, ok := inst.Capabilities["server_purposes"].([]string)
	if !ok || len(purposes) == 0 {
		t.Fatalf("server purposes missing: %#v", inst.Capabilities["server_purposes"])
	}
	var foundVHost bool
	for _, vhost := range inst.VHosts {
		if vhost["document_root"] == appDir {
			foundVHost = true
			break
		}
	}
	if !foundVHost {
		t.Fatalf("vhost app metadata missing: %#v", inst.VHosts)
	}
}

func TestAuditModeReportsManagedDriftWithoutMutation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	managedDir := filepath.Join(dir, "managed")
	logDir := filepath.Join(dir, "logs")
	hookLine := "include " + filepath.ToSlash(filepath.Join(managedDir, "control-one.conf")) + ";"
	configPath := filepath.Join(dir, "nginx.conf")
	originalConfig := "events {}\nhttp {\n    " + hookLine + "\n}\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	capturePath := filepath.Join(managedDir, "control-one.conf")
	if err := os.WriteFile(capturePath, []byte("# unmanaged edit\n"), 0o644); err != nil {
		t.Fatalf("write drifted capture: %v", err)
	}

	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_apply", WebServerInstance{
		Kind:       "nginx",
		ConfigPath: configPath,
	}, WebPolicy{
		Mode:       "audit",
		ManagedDir: managedDir,
		LogDir:     logDir,
		Approved:   true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	receipt, err := mgr.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("audit apply: %v", err)
	}
	if receipt.ReloadStatus != "not_required" {
		t.Fatalf("audit reload status = %q", receipt.ReloadStatus)
	}
	if detected, _ := receipt.Metadata["drift_detected"].(bool); !detected {
		t.Fatalf("expected drift detection in receipt: %#v", receipt.Metadata)
	}
	assertFileContent(t, configPath, originalConfig)
	assertFileContent(t, capturePath, "# unmanaged edit\n")
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s content changed:\n%s", path, string(data))
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return st.Mode().Perm()
}
