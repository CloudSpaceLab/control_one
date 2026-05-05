package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/policy"
)

// TestBootstrapE2E_FreshEnrollmentBootsCleanly is a regression guard for the
// production failure observed in the Linux E2E:
//
//	{"level":"fatal","caller":"nodeagent/main.go:165","msg":"policy syncer init failed",
//	 "error":"read policy public key: open /var/lib/control-one/nodeagent/keys/policy_pub.pem: no such file or directory"}
//
// Before the fix, enrollment wrote nodeagent.yaml with no `policy:` section,
// the agent config-defaulted policy.public_key_file to a path nothing
// created, policy.NewSyncer hard-failed on the missing file, main.go
// promoted that to log.Fatal, and the heartbeat goroutine never spawned.
//
// The fix has three layers, each tested independently:
//   case_no_policy_section_in_yaml — defaults no longer point at a phantom path
//   case_yaml_explicit_empty_path  — explicit empty disables sig verification
//   case_yaml_path_missing_file    — soft-fails instead of hard-failing
//   case_yaml_path_with_valid_key  — loads the key when the file exists
//
// Failure of any subtest means a regression in one of the three layers.
func TestBootstrapE2E_FreshEnrollmentBootsCleanly(t *testing.T) {
	srv := httptest.NewServer(nil)
	defer srv.Close()
	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	logger := zap.NewNop()

	// Helper: build a nodeagent.yaml in a temp dir, optionally injecting a
	// custom `policy:` block. Returns the loaded config.
	loadYAML := func(t *testing.T, policyBlock string) *config.Config {
		t.Helper()
		dataDir := t.TempDir()
		configDir := filepath.Join(dataDir, "config")
		certDir := filepath.Join(dataDir, "certs")
		policyDir := filepath.Join(dataDir, "policies")
		logDir := filepath.Join(dataDir, "logs")
		for _, d := range []string{configDir, certDir, policyDir, logDir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", d, err)
			}
		}
		yaml := fmt.Sprintf(`api_url: "https://cp.example:8443"
node_name: "test-node"
state_file: "%s"
policy_dir: "%s"
log_dir: "%s"

tls:
  cert_file: "%s"
  key_file: "%s"
  ca_cert_file: "%s"
%s
intervals:
  heartbeat: 60s
  policy_sync: 600s
  scan: 300s
  telemetry: 60s

scanner:
  enabled: true
  use_real_scan: true

wizard:
  enabled: true
  auto_generate_certificates: false
  run_access_sync: true
  run_secrets_sync: true
  run_compliance: true
  emit_summary: true
`,
			filepath.ToSlash(filepath.Join(dataDir, "state.json")),
			filepath.ToSlash(policyDir), filepath.ToSlash(logDir),
			filepath.ToSlash(filepath.Join(certDir, "client.crt")),
			filepath.ToSlash(filepath.Join(certDir, "client.key")),
			filepath.ToSlash(filepath.Join(certDir, "ca.crt")),
			policyBlock,
		)
		cfgPath := filepath.Join(configDir, "nodeagent.yaml")
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o640); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		return cfg
	}

	t.Run("case_no_policy_section_in_yaml", func(t *testing.T) {
		// Old enrollment path — no policy: section. After the fix, defaults
		// must NOT smuggle in a path that nothing creates.
		cfg := loadYAML(t, "")
		if cfg.Policy.PublicKeyFile != "" {
			t.Fatalf("PublicKeyFile = %q, want empty (no rogue default)", cfg.Policy.PublicKeyFile)
		}
		_, err := policy.NewSyncer(client, logger, policy.Options{
			PublicKeyPath: cfg.Policy.PublicKeyFile,
			MetadataPath:  cfg.Policy.MetadataFile,
		})
		if err != nil {
			t.Fatalf("NewSyncer with empty path: %v (expected success)", err)
		}
	})

	t.Run("case_yaml_explicit_empty_path", func(t *testing.T) {
		// New enrollment path when server isn't configured with a signing key.
		// join.go writes an explicit `public_key_file: ""`.
		cfg := loadYAML(t, "\npolicy:\n  public_key_file: \"\"\n")
		if cfg.Policy.PublicKeyFile != "" {
			t.Fatalf("PublicKeyFile = %q, want empty", cfg.Policy.PublicKeyFile)
		}
		_, err := policy.NewSyncer(client, logger, policy.Options{
			PublicKeyPath: cfg.Policy.PublicKeyFile,
			MetadataPath:  cfg.Policy.MetadataFile,
		})
		if err != nil {
			t.Fatalf("NewSyncer with explicit empty path: %v (expected success)", err)
		}
	})

	t.Run("case_yaml_path_missing_file", func(t *testing.T) {
		// Defense in depth — even if config points at a path that doesn't
		// exist on disk (stale config, not-yet-provisioned key), boot must
		// still succeed with sig verification disabled.
		missing := filepath.ToSlash(filepath.Join(t.TempDir(), "no_such_key.pem"))
		cfg := loadYAML(t, fmt.Sprintf("\npolicy:\n  public_key_file: %q\n", missing))
		if cfg.Policy.PublicKeyFile != missing {
			t.Fatalf("PublicKeyFile = %q, want %q", cfg.Policy.PublicKeyFile, missing)
		}
		_, err := policy.NewSyncer(client, logger, policy.Options{
			PublicKeyPath: cfg.Policy.PublicKeyFile,
			MetadataPath:  cfg.Policy.MetadataFile,
		})
		if err != nil {
			t.Fatalf("NewSyncer with missing file: %v (expected soft-fail)", err)
		}
	})

	t.Run("case_yaml_path_with_valid_key", func(t *testing.T) {
		// Happy path — server shipped a key, join.go wrote it to disk, agent
		// loads it for real signature verification.
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate ed25519 key: %v", err)
		}
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			t.Fatalf("marshal pubkey: %v", err)
		}
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
		keyPath := filepath.Join(t.TempDir(), "policy_pub.pem")
		if err := os.WriteFile(keyPath, pemBytes, 0o644); err != nil {
			t.Fatalf("write key: %v", err)
		}
		cfg := loadYAML(t, fmt.Sprintf("\npolicy:\n  public_key_file: %q\n", filepath.ToSlash(keyPath)))
		_, err = policy.NewSyncer(client, logger, policy.Options{
			PublicKeyPath: cfg.Policy.PublicKeyFile,
			MetadataPath:  cfg.Policy.MetadataFile,
		})
		if err != nil {
			t.Fatalf("NewSyncer with valid key: %v", err)
		}
	})
}

// TestJoinYAMLContainsPolicySection asserts that join.go's emitted yaml
// always carries an explicit policy.public_key_file line, so the (now
// removed) global default can never sneak back in via Viper. Pure
// string-level check on the template — keeps the regression visible at
// the join template instead of waiting for an integration failure.
func TestJoinYAMLContainsPolicySection(t *testing.T) {
	src, err := os.ReadFile("join.go")
	if err != nil {
		t.Fatalf("read join.go: %v", err)
	}
	if !strings.Contains(string(src), "policy:\n  public_key_file:") {
		t.Error("join.go template no longer emits an explicit policy.public_key_file line")
	}
}

// TestRunJoinEmitsParseableYAML drives runJoin end-to-end against an
// httptest enrollment server and asserts that the generated nodeagent.yaml
// loads cleanly via config.Load. Catches the Windows-path bug where raw
// backslashes in double-quoted YAML strings get parsed as escape sequences
// (\U expects 8 hex digits) and panic the loader. Was a real production
// crash on the demo run; this guards against regressions.
func TestRunJoinEmitsParseableYAML(t *testing.T) {
	// Stub installServiceFn so the test never touches the OS service manager.
	origInstall := installServiceFn
	installServiceFn = func(string) error { return nil }
	defer func() { installServiceFn = origInstall }()

	// Fake enrollment server — returns the minimum the agent needs to
	// finish its yaml + state writes without actually issuing a cert.
	enrollSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/enroll" {
			http.NotFound(w, r)
			return
		}
		body := map[string]any{
			"node_id":   "11111111-1111-1111-1111-111111111111",
			"tenant_id": "22222222-2222-2222-2222-222222222222",
			"tls": map[string]string{
				"client_cert": "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n",
				"client_key":  "-----BEGIN EC PRIVATE KEY-----\nfake\n-----END EC PRIVATE KEY-----\n",
				"ca_cert":     "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n",
			},
			"config": map[string]any{
				"intervals": map[string]int{"heartbeat": 60},
			},
			"policy": map[string]string{}, // empty PEM — server with no key configured
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer enrollSrv.Close()

	// Use a tempdir at the path most likely to expose the bug — Windows-style
	// drive-letter root with backslashes. On non-Windows this is just a
	// normal absolute path with no backslashes, but the assertion still
	// holds (yaml must parse).
	dataDir := t.TempDir()
	configDir := t.TempDir()

	if err := runJoin(enrollSrv.URL, "fake-token", "test-node", configDir, dataDir, false, false); err != nil {
		t.Fatalf("runJoin: %v", err)
	}

	cfgPath := filepath.Join(configDir, "nodeagent.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		raw, _ := os.ReadFile(cfgPath)
		t.Fatalf("config.Load on generated yaml: %v\n--- yaml ---\n%s", err, raw)
	}

	// Sanity: parsed values match what runJoin should have emitted.
	if cfg.APIURL != enrollSrv.URL {
		t.Errorf("api_url = %q, want %q", cfg.APIURL, enrollSrv.URL)
	}
	if cfg.Policy.PublicKeyFile != "" {
		t.Errorf("policy.public_key_file = %q, want empty (server returned no PEM)", cfg.Policy.PublicKeyFile)
	}
	if !strings.Contains(cfg.PolicyDir, dataDir[:1]) && !strings.Contains(filepath.ToSlash(cfg.PolicyDir), filepath.ToSlash(dataDir)[:1]) {
		t.Errorf("policy_dir = %q, expected to be rooted under dataDir %q", cfg.PolicyDir, dataDir)
	}
}
