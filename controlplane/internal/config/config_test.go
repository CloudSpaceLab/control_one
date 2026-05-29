package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateRequiresEssentials(t *testing.T) {
	base := func() *Config {
		return &Config{
			HTTP: HTTPConfig{Address: ":0"},
			TLS:  TLSConfig{},
			Database: DatabaseConfig{
				URL: "postgres://localhost/db",
			},
			Worker: WorkerConfig{
				Concurrency:  1,
				QueueSize:    1,
				MaxAttempts:  1,
				RetryBackoff: time.Second,
			},
			Auth: AuthConfig{
				OIDC: OIDCConfig{},
				RBAC: RBACConfig{DefaultRole: "viewer"},
			},
		}
	}

	t.Run("missing http address", func(t *testing.T) {
		cfg := base()
		cfg.HTTP.Address = ""
		if err := Validate(cfg); err == nil {
			t.Fatalf("expected error for missing http.address")
		}
	})

	t.Run("missing database url", func(t *testing.T) {
		cfg := base()
		cfg.Database.URL = ""
		if err := Validate(cfg); err == nil {
			t.Fatalf("expected error for missing database.url")
		}
	})

	t.Run("tls requires cert and key", func(t *testing.T) {
		cfg := base()
		cfg.TLS.Enabled = true
		cfg.TLS.CertFile = ""
		cfg.TLS.KeyFile = ""
		if err := Validate(cfg); err == nil {
			t.Fatalf("expected error for missing tls cert/key")
		}
	})

	t.Run("asynq requires redis address", func(t *testing.T) {
		cfg := base()
		cfg.Worker.Backend = "asynq"
		cfg.Worker.Asynq.Enabled = true
		cfg.Worker.Asynq.RedisAddress = ""
		if err := Validate(cfg); err == nil {
			t.Fatalf("expected error for missing asynq redis address")
		}
	})

	t.Run("ip behavior redis counters require redis address", func(t *testing.T) {
		cfg := base()
		cfg.IPBehavior.Counters.Backend = "redis"
		cfg.IPBehavior.Counters.RedisAddress = ""
		if err := Validate(cfg); err == nil {
			t.Fatalf("expected error for missing ip behavior redis address")
		}
	})

	t.Run("oidc requires issuer and client id", func(t *testing.T) {
		cfg := base()
		cfg.Auth.OIDC.Enabled = true
		cfg.Auth.OIDC.IssuerURL = ""
		cfg.Auth.OIDC.ClientID = ""
		if err := Validate(cfg); err == nil {
			t.Fatalf("expected error for missing oidc settings")
		}
	})

	t.Run("valid base config passes", func(t *testing.T) {
		cfg := base()
		cfg.Auth.OIDC.CacheTTL = time.Minute
		cfg.Worker.RetryBackoff = time.Second
		if err := Validate(cfg); err != nil {
			t.Fatalf("expected config to validate, got %v", err)
		}
	})
}

func TestApplyFallbacksSetsEnforcementSafetyDefaults(t *testing.T) {
	cfg := &Config{}
	applyFallbacks(cfg)
	if cfg.Remediation.MaxBlockChangesPerHour != 100 {
		t.Fatalf("MaxBlockChangesPerHour = %d, want 100", cfg.Remediation.MaxBlockChangesPerHour)
	}
	if cfg.Remediation.MaxBlockChangesPerServerGroupPerHour != 25 {
		t.Fatalf("MaxBlockChangesPerServerGroupPerHour = %d, want 25", cfg.Remediation.MaxBlockChangesPerServerGroupPerHour)
	}
	if cfg.Remediation.MaxGlobalBlockChangesPerHour != 1000 {
		t.Fatalf("MaxGlobalBlockChangesPerHour = %d, want 1000", cfg.Remediation.MaxGlobalBlockChangesPerHour)
	}
	if cfg.Remediation.BlockCanaryNodesPerServerGroup != 1 {
		t.Fatalf("BlockCanaryNodesPerServerGroup = %d, want 1", cfg.Remediation.BlockCanaryNodesPerServerGroup)
	}
	if cfg.Remediation.WebserverFailureCircuitThreshold != 3 {
		t.Fatalf("WebserverFailureCircuitThreshold = %d, want 3", cfg.Remediation.WebserverFailureCircuitThreshold)
	}
	if cfg.Remediation.WebserverFailureCircuitWindow != time.Hour {
		t.Fatalf("WebserverFailureCircuitWindow = %s, want 1h", cfg.Remediation.WebserverFailureCircuitWindow)
	}
	if cfg.OfflineContent.RootDir != "data/offline-content" {
		t.Fatalf("OfflineContent.RootDir = %q, want default data/offline-content", cfg.OfflineContent.RootDir)
	}
	if cfg.OfflineContent.MaxBundleBytes != 256*1024*1024 {
		t.Fatalf("OfflineContent.MaxBundleBytes = %d, want 256MiB", cfg.OfflineContent.MaxBundleBytes)
	}
	if cfg.SIEMForwarding.Interval != 30*time.Second {
		t.Fatalf("SIEMForwarding.Interval = %s, want 30s", cfg.SIEMForwarding.Interval)
	}
	if cfg.SIEMForwarding.RunTimeout != 2*time.Minute {
		t.Fatalf("SIEMForwarding.RunTimeout = %s, want 2m", cfg.SIEMForwarding.RunTimeout)
	}
	if cfg.SIEMForwarding.InitialLookback != 15*time.Minute {
		t.Fatalf("SIEMForwarding.InitialLookback = %s, want 15m", cfg.SIEMForwarding.InitialLookback)
	}
	if cfg.SIEMForwarding.MaxBatchSize != 500 {
		t.Fatalf("SIEMForwarding.MaxBatchSize = %d, want 500", cfg.SIEMForwarding.MaxBatchSize)
	}
	if cfg.PrivateAccess.ImportSchedulerInterval != 5*time.Minute {
		t.Fatalf("PrivateAccess.ImportSchedulerInterval = %s, want 5m", cfg.PrivateAccess.ImportSchedulerInterval)
	}
	if cfg.PrivateAccess.ImportSchedulerLimit != 50 {
		t.Fatalf("PrivateAccess.ImportSchedulerLimit = %d, want 50", cfg.PrivateAccess.ImportSchedulerLimit)
	}
	if cfg.Doris.ReplicationNum != 1 {
		t.Fatalf("Doris.ReplicationNum = %d, want dev default 1", cfg.Doris.ReplicationNum)
	}
	if cfg.Vault.Timeout != 30*time.Second {
		t.Fatalf("Vault.Timeout = %s, want 30s", cfg.Vault.Timeout)
	}
}

func TestLoadPreservesStaticTokenCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controlplane.yaml")
	token := "AdminToken-With_MixedCASE123"
	config := `http:
  address: ":0"
database:
  url: "postgres://localhost/db"
auth:
  oidc:
    enabled: false
    static_tokens:
      "` + token + `":
        subject: "static-admin"
        email: "admin@example.com"
        name: "Bootstrap Admin"
        roles: ["admin"]
        groups: ["admins"]
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, ok := cfg.Auth.OIDC.StaticTokens[token]; !ok {
		t.Fatalf("expected exact-case static token key to be preserved")
	}
	if _, ok := cfg.Auth.OIDC.StaticTokens[strings.ToLower(token)]; ok {
		t.Fatalf("did not expect lower-cased static token key")
	}
}

func TestValidateOfflineContentRequiresSigningKey(t *testing.T) {
	cfg := &Config{
		HTTP: HTTPConfig{Address: ":0"},
		Database: DatabaseConfig{
			URL: "postgres://localhost/db",
		},
		Worker: WorkerConfig{
			Concurrency:  1,
			QueueSize:    1,
			MaxAttempts:  1,
			RetryBackoff: time.Second,
		},
		Auth: AuthConfig{RBAC: RBACConfig{DefaultRole: "viewer"}},
		OfflineContent: OfflineContentConfig{
			Enabled: true,
			RootDir: "data/offline-content",
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatalf("expected offline_content.public_key_file validation error")
	}
	cfg.OfflineContent.PublicKeyFile = "offline-content.pub"
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected offline content config to validate with key path, got %v", err)
	}
}
