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

func TestFinalPlanAcceptanceNginxCaptureRollbackOnHealthFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "nginx.conf")
	original := "events {}\nhttp {}\n"
	if err := os.WriteFile(configPath, []byte(original), 0o640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unhealthy", http.StatusInternalServerError)
	}))
	defer health.Close()

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
		HealthCheckURL:     health.URL,
		TrustedProxyCIDRs:  []string{"10.0.0.0/8"},
	})
	if err != nil {
		t.Fatalf("plan nginx capture: %v", err)
	}
	plan.ValidationCommand = nil
	plan.ReloadCommand = nil
	receipt, err := mgr.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected failed health check to fail apply")
	}
	if receipt.ValidationStatus != "passed" || receipt.ReloadStatus != "health_check_failed" {
		t.Fatalf("receipt status = %#v, want passed/health_check_failed", receipt)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != original {
		t.Fatalf("nginx config not rolled back:\n%s", string(after))
	}
}

func TestFinalPlanAcceptanceApacheCaptureReceipt(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "apache2.conf")
	if err := os.WriteFile(configPath, []byte("ServerRoot \"/tmp\"\n"), 0o640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	mgr := NewManager(zap.NewNop())
	plan, err := mgr.Plan(context.Background(), "webserver.config_apply", WebServerInstance{
		Kind:       "apache",
		ConfigPath: configPath,
		Capabilities: map[string]any{
			"loaded_modules": []string{"remoteip_module"},
		},
	}, WebPolicy{
		ManagedDir:         filepath.Join(dir, "managed"),
		LogDir:             filepath.Join(dir, "logs"),
		Approved:           true,
		AllowConfigHook:    true,
		ConfigHookStrategy: "append_at_eof",
		TrustedProxyCIDRs:  []string{"10.0.0.0/8"},
	})
	if err != nil {
		t.Fatalf("plan apache capture: %v", err)
	}
	plan.ValidationCommand = nil
	plan.ReloadCommand = nil
	receipt, err := mgr.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply apache capture: %v", err)
	}
	if receipt.ValidationStatus != "passed" || receipt.RollbackRef == "" || len(receipt.ManagedFiles) == 0 {
		t.Fatalf("apache receipt incomplete: %#v", receipt)
	}
	capture, err := os.ReadFile(filepath.Join(plan.ManagedDir, "control-one.conf"))
	if err != nil {
		t.Fatalf("read apache capture snippet: %v", err)
	}
	if !strings.Contains(string(capture), "LogFormat") || !strings.Contains(string(capture), "RemoteIPTrustedProxy 10.0.0.0/8") {
		t.Fatalf("apache capture missing expected fields:\n%s", string(capture))
	}
}
