package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config captures control plane service settings.
type Config struct {
	HTTP          HTTPConfig          `mapstructure:"http"`
	TLS           TLSConfig           `mapstructure:"tls"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Worker        WorkerConfig        `mapstructure:"worker"`
	Jobs          JobsConfig          `mapstructure:"jobs"`
	Auth          AuthConfig          `mapstructure:"auth"`
	Registration  RegistrationConfig  `mapstructure:"registration"`
}

// HTTPConfig defines HTTP server settings.
type HTTPConfig struct {
	Address      string        `mapstructure:"address"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// TLSConfig defines TLS listener options.
type TLSConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	CertFile         string `mapstructure:"cert_file"`
	KeyFile          string `mapstructure:"key_file"`
	ClientCAFile     string `mapstructure:"client_ca_file"`
	RequireClientTLS bool   `mapstructure:"require_client_tls"`
}

// ObservabilityConfig captures metrics/tracing toggles.
type ObservabilityConfig struct {
	EnableMetrics bool   `mapstructure:"enable_metrics"`
	MetricsPath   string `mapstructure:"metrics_path"`
}

// DatabaseConfig captures Postgres connectivity options.
type DatabaseConfig struct {
	URL             string        `mapstructure:"url"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ApplyMigrations bool          `mapstructure:"apply_migrations"`
}

// WorkerConfig controls background job processing.
type WorkerConfig struct {
	Concurrency int `mapstructure:"concurrency"`
	QueueSize   int `mapstructure:"queue_size"`
}

// AuthConfig captures identity provider and RBAC settings.
type AuthConfig struct {
	OIDC OIDCConfig `mapstructure:"oidc"`
	RBAC RBACConfig `mapstructure:"rbac"`
}

// OIDCConfig defines OpenID Connect verification options.
type OIDCConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	IssuerURL     string        `mapstructure:"issuer_url"`
	ClientID      string        `mapstructure:"client_id"`
	Audience      []string      `mapstructure:"audience"`
	UsernameClaim string        `mapstructure:"username_claim"`
	GroupsClaim   string        `mapstructure:"groups_claim"`
	CacheTTL      time.Duration `mapstructure:"cache_ttl"`
}

// RBACConfig defines role mapping and defaults.
type RBACConfig struct {
	DefaultRole  string              `mapstructure:"default_role"`
	RoleMappings map[string][]string `mapstructure:"role_mappings"`
}

// JobsConfig captures external service integrations for background jobs.
type JobsConfig struct {
	Provisioning ProvisioningJobConfig `mapstructure:"provisioning"`
	Compliance   ComplianceJobConfig   `mapstructure:"compliance"`
}

// ProvisioningJobConfig defines outbound settings for provisioning jobs.
type ProvisioningJobConfig struct {
	APIBaseURL     string          `mapstructure:"api_base_url"`
	Token          string          `mapstructure:"token"`
	Template       string          `mapstructure:"template"`
	Provider       string          `mapstructure:"provider"`
	Baselines      []string        `mapstructure:"baselines"`
	AutoRemediation bool           `mapstructure:"auto_remediation"`
	TLS            ClientTLSConfig `mapstructure:"tls"`
}

// ComplianceJobConfig defines outbound settings for compliance jobs.
type ComplianceJobConfig struct {
	APIBaseURL     string          `mapstructure:"api_base_url"`
	Token          string          `mapstructure:"token"`
	Region         string          `mapstructure:"region"`
	RuleSets       []string        `mapstructure:"rule_sets"`
	Certifications []string        `mapstructure:"certifications"`
	AutoApply      bool            `mapstructure:"auto_apply"`
	TLS            ClientTLSConfig `mapstructure:"tls"`
}

// ClientTLSConfig captures TLS options for outbound clients.
type ClientTLSConfig struct {
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	CACertFile string `mapstructure:"ca_cert_file"`
}

// RegistrationConfig controls node bootstrap handshake behavior.
type RegistrationConfig struct {
	BootstrapTokens []string `mapstructure:"bootstrap_tokens"`
	DefaultTenantID string   `mapstructure:"default_tenant_id"`
}

// Load reads configuration from the provided path.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	applyFallbacks(cfg)

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("http.address", ":8443")
	v.SetDefault("http.read_timeout", 15*time.Second)
	v.SetDefault("http.write_timeout", 15*time.Second)

	v.SetDefault("tls.enabled", false)
	v.SetDefault("tls.require_client_tls", false)

	v.SetDefault("observability.enable_metrics", true)
	v.SetDefault("observability.metrics_path", "/metrics")

	v.SetDefault("database.max_open_conns", 10)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", time.Minute*15)
	v.SetDefault("database.apply_migrations", true)

	v.SetDefault("worker.concurrency", 2)
	v.SetDefault("worker.queue_size", 128)

	v.SetDefault("jobs.provisioning.auto_remediation", true)
	v.SetDefault("jobs.compliance.auto_apply", true)

	v.SetDefault("registration.bootstrap_tokens", []string{})

	v.SetDefault("auth.oidc.enabled", false)
	v.SetDefault("auth.oidc.username_claim", "email")
	v.SetDefault("auth.oidc.groups_claim", "groups")
	v.SetDefault("auth.oidc.cache_ttl", time.Minute*5)
	v.SetDefault("auth.rbac.default_role", "viewer")
}

func applyFallbacks(cfg *Config) {
	if cfg.HTTP.Address == "" {
		cfg.HTTP.Address = ":8443"
	}
	if cfg.HTTP.ReadTimeout == 0 {
		cfg.HTTP.ReadTimeout = 15 * time.Second
	}
	if cfg.HTTP.WriteTimeout == 0 {
		cfg.HTTP.WriteTimeout = 15 * time.Second
	}
	if cfg.Observability.MetricsPath == "" {
		cfg.Observability.MetricsPath = "/metrics"
	}
	if cfg.Database.MaxOpenConns <= 0 {
		cfg.Database.MaxOpenConns = 10
	}
	if cfg.Database.MaxIdleConns < 0 {
		cfg.Database.MaxIdleConns = 5
	}
	if cfg.Database.ConnMaxLifetime == 0 {
		cfg.Database.ConnMaxLifetime = 15 * time.Minute
	}
	if cfg.Worker.Concurrency <= 0 {
		cfg.Worker.Concurrency = 2
	}
	if cfg.Worker.QueueSize <= 0 {
		cfg.Worker.QueueSize = 128
	}
}
