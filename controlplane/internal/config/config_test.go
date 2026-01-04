package config

import (
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
