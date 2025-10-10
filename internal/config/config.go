package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config captures all node agent settings loaded from YAML.
type Config struct {
	APIURL         string        `mapstructure:"api_url"`
	BootstrapToken string        `mapstructure:"bootstrap_token"`
	NodeName       string        `mapstructure:"node_name"`
	StateFile      string        `mapstructure:"state_file"`
	PolicyDir      string        `mapstructure:"policy_dir"`
	LogDir         string        `mapstructure:"log_dir"`
	PluginDir      string        `mapstructure:"plugin_dir"`

	Scanner struct {
		Timeout       time.Duration `mapstructure:"timeout"`
		Shell         string        `mapstructure:"shell"`
		MaxConcurrent int           `mapstructure:"max_concurrent"`
	} `mapstructure:"scanner"`

	Policy struct {
		PublicKeyFile string `mapstructure:"public_key_file"`
		MetadataFile  string `mapstructure:"metadata_file"`
	} `mapstructure:"policy"`

	Compliance struct {
		Region             string   `mapstructure:"region"`
		RuleSets           []string `mapstructure:"rule_sets"`
		Certifications     []string `mapstructure:"certifications"`
		AutoApplyTemplates bool     `mapstructure:"auto_apply_templates"`
	} `mapstructure:"compliance"`

	Access struct {
		Provider     string        `mapstructure:"provider"`
		SyncInterval time.Duration `mapstructure:"sync_interval"`
		DefaultRole  string        `mapstructure:"default_role"`
		APIEndpoint  string        `mapstructure:"api_endpoint"`
	} `mapstructure:"access"`

	Secrets struct {
		Backend      string        `mapstructure:"backend"`
		Endpoint     string        `mapstructure:"endpoint"`
		Groups       []string      `mapstructure:"groups"`
		SyncInterval time.Duration `mapstructure:"sync_interval"`
	} `mapstructure:"secrets"`

	Hardening struct {
		Profiles       []string `mapstructure:"profiles"`
		AllowOverrides bool     `mapstructure:"allow_overrides"`
	} `mapstructure:"hardening"`

	TelemetryPrefs struct {
		CollectLogs      bool          `mapstructure:"collect_logs"`
		LogNamespaces    []string      `mapstructure:"log_namespaces"`
		FileIntegrity    bool          `mapstructure:"file_integrity"`
		MetricsInterval  time.Duration `mapstructure:"metrics_interval"`
		ActivityInterval time.Duration `mapstructure:"activity_interval"`
	} `mapstructure:"telemetry_prefs"`

	Mesh struct {
		Enabled        bool          `mapstructure:"enabled"`
		CoordinatorURL string        `mapstructure:"coordinator_url"`
		AuthToken      string        `mapstructure:"auth_token"`
		Namespace      string        `mapstructure:"namespace"`
		PrivateCIDR    string        `mapstructure:"private_cidr"`
		RelayNodes     []string      `mapstructure:"relay_nodes"`
		StateFile      string        `mapstructure:"state_file"`
		PollInterval   time.Duration `mapstructure:"poll_interval"`
		KeyRotation    time.Duration `mapstructure:"key_rotation"`
	} `mapstructure:"mesh"`

	Provisioning struct {
		Template        string   `mapstructure:"template"`
		Baselines       []string `mapstructure:"baselines"`
		AutoRemediation bool     `mapstructure:"auto_remediation"`
	} `mapstructure:"provisioning"`

	TLS struct {
		CertFile   string `mapstructure:"cert_file"`
		KeyFile    string `mapstructure:"key_file"`
		CACertFile string `mapstructure:"ca_cert_file"`
	} `mapstructure:"tls"`

	Intervals struct {
		Heartbeat    time.Duration `mapstructure:"heartbeat"`
		PolicySync   time.Duration `mapstructure:"policy_sync"`
		Scan         time.Duration `mapstructure:"scan"`
		Telemetry    time.Duration `mapstructure:"telemetry"`
		Provisioning time.Duration `mapstructure:"provisioning"`
	} `mapstructure:"intervals"`
}

const (
	defaultConfigPath          = "/etc/control-one/nodeagent.yaml"
	defaultStateFile           = "/var/lib/control-one/nodeagent/state.json"
	defaultPolicyDir           = "/var/lib/control-one/nodeagent/policies"
	defaultLogDir              = "/var/log/control-one/nodeagent"
	defaultPluginDir           = "/opt/control-one/nodeagent/plugins"
	defaultTLSCertFile         = "/var/lib/control-one/nodeagent/certs/nodeagent.crt"
	defaultTLSKeyFile          = "/var/lib/control-one/nodeagent/certs/nodeagent.key"
	defaultTLSCACertFile       = "/var/lib/control-one/nodeagent/certs/ca.crt"
	defaultPolicyPublicKeyFile = "/var/lib/control-one/nodeagent/keys/policy_pub.pem"
	defaultPolicyMetadataFile  = "/var/lib/control-one/nodeagent/policies/policies.meta.json"
	defaultScannerTimeout      = 30 * time.Second
	defaultMeshStateFile       = "/var/lib/control-one/nodeagent/mesh/state.json"
	defaultMeshNamespace       = "default"
	defaultMeshCIDR            = "10.1.0.0/16"
	defaultProvisioningInterval = 30 * time.Minute
	defaultAccessSyncInterval   = 30 * time.Minute
	defaultSecretsSyncInterval  = 15 * time.Minute
	defaultTelemetryMetrics     = time.Minute
	defaultTelemetryActivity    = 5 * time.Minute
)

// Load reads configuration from the provided path. If path is empty it falls back to defaultConfigPath.
func Load(path string) (*Config, error) {
	cfgPath := path
	if strings.TrimSpace(cfgPath) == "" {
		cfgPath = defaultConfigPath
	}

	v := viper.New()
	v.SetConfigFile(cfgPath)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFound) {
			return nil, fmt.Errorf("config file not found: %w", err)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	applyFallbacks(cfg)

	return cfg, nil
}

// EnsureDirectories creates required directories if they are missing.
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		filepath.Dir(c.StateFile),
		c.PolicyDir,
		c.LogDir,
		c.PluginDir,
		filepath.Dir(c.TLS.CertFile),
		filepath.Dir(c.TLS.KeyFile),
		filepath.Dir(c.TLS.CACertFile),
		filepath.Dir(c.Policy.PublicKeyFile),
		filepath.Dir(c.Policy.MetadataFile),
	}

	if c.Mesh.StateFile != "" {
		dirs = append(dirs, filepath.Dir(c.Mesh.StateFile))
	}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("api_url", "https://control-plane.local/api")
	v.SetDefault("node_name", "")
	v.SetDefault("state_file", defaultStateFile)
	v.SetDefault("policy_dir", defaultPolicyDir)
	v.SetDefault("log_dir", defaultLogDir)
	v.SetDefault("plugin_dir", defaultPluginDir)

	v.SetDefault("scanner.timeout", defaultScannerTimeout.String())
	v.SetDefault("scanner.shell", "")
	v.SetDefault("scanner.max_concurrent", 4)

	v.SetDefault("mesh.enabled", true)
	v.SetDefault("mesh.coordinator_url", "")
	v.SetDefault("mesh.auth_token", "")
	v.SetDefault("mesh.namespace", defaultMeshNamespace)
	v.SetDefault("mesh.private_cidr", defaultMeshCIDR)
	v.SetDefault("mesh.relay_nodes", []string{})
	v.SetDefault("mesh.state_file", defaultMeshStateFile)
	v.SetDefault("mesh.poll_interval", "10m")
	v.SetDefault("mesh.key_rotation", "168h")

	v.SetDefault("provisioning.template", "")
	v.SetDefault("provisioning.baselines", []string{})
	v.SetDefault("provisioning.auto_remediation", true)

	v.SetDefault("compliance.region", "global")
	v.SetDefault("compliance.rule_sets", []string{})
	v.SetDefault("compliance.certifications", []string{})
	v.SetDefault("compliance.auto_apply_templates", true)

	v.SetDefault("access.provider", "local")
	v.SetDefault("access.sync_interval", defaultAccessSyncInterval.String())
	v.SetDefault("access.default_role", "viewer")
	v.SetDefault("access.api_endpoint", "")

	v.SetDefault("secrets.backend", "memory")
	v.SetDefault("secrets.endpoint", "")
	v.SetDefault("secrets.groups", []string{})
	v.SetDefault("secrets.sync_interval", defaultSecretsSyncInterval.String())

	v.SetDefault("hardening.profiles", []string{"baseline"})
	v.SetDefault("hardening.allow_overrides", false)

	v.SetDefault("telemetry_prefs.collect_logs", true)
	v.SetDefault("telemetry_prefs.log_namespaces", []string{"system", "application"})
	v.SetDefault("telemetry_prefs.file_integrity", false)
	v.SetDefault("telemetry_prefs.metrics_interval", defaultTelemetryMetrics.String())
	v.SetDefault("telemetry_prefs.activity_interval", defaultTelemetryActivity.String())

	v.SetDefault("policy.public_key_file", defaultPolicyPublicKeyFile)
	v.SetDefault("policy.metadata_file", defaultPolicyMetadataFile)

	v.SetDefault("tls.cert_file", defaultTLSCertFile)
	v.SetDefault("tls.key_file", defaultTLSKeyFile)
	v.SetDefault("tls.ca_cert_file", defaultTLSCACertFile)

	v.SetDefault("intervals.heartbeat", "60s")
	v.SetDefault("intervals.policy_sync", "30m")
	v.SetDefault("intervals.scan", "15m")
	v.SetDefault("intervals.telemetry", "1m")
	v.SetDefault("intervals.provisioning", defaultProvisioningInterval.String())
}

func applyFallbacks(cfg *Config) {
	if cfg.StateFile == "" {
		cfg.StateFile = defaultStateFile
	}
	if cfg.PolicyDir == "" {
		cfg.PolicyDir = defaultPolicyDir
	}
	if cfg.LogDir == "" {
		cfg.LogDir = defaultLogDir
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = defaultPluginDir
	}
	if cfg.Scanner.Timeout == 0 {
		cfg.Scanner.Timeout = defaultScannerTimeout
	}
	if cfg.Scanner.MaxConcurrent <= 0 {
		cfg.Scanner.MaxConcurrent = 4
	}
	if cfg.Policy.PublicKeyFile == "" {
		cfg.Policy.PublicKeyFile = defaultPolicyPublicKeyFile
	}
	if cfg.Policy.MetadataFile == "" {
		cfg.Policy.MetadataFile = defaultPolicyMetadataFile
	}
	if cfg.Mesh.StateFile == "" {
		cfg.Mesh.StateFile = defaultMeshStateFile
	}
	if cfg.Mesh.PollInterval == 0 {
		cfg.Mesh.PollInterval = 10 * time.Minute
	}
	if cfg.Mesh.KeyRotation == 0 {
		cfg.Mesh.KeyRotation = 7 * 24 * time.Hour
	}
	if cfg.Mesh.Namespace == "" {
		cfg.Mesh.Namespace = defaultMeshNamespace
	}
	if cfg.Mesh.PrivateCIDR == "" {
		cfg.Mesh.PrivateCIDR = defaultMeshCIDR
	}

	if cfg.Intervals.Provisioning == 0 {
		cfg.Intervals.Provisioning = defaultProvisioningInterval
	}

	if cfg.Access.SyncInterval == 0 {
		cfg.Access.SyncInterval = defaultAccessSyncInterval
	}
	if cfg.Secrets.SyncInterval == 0 {
		cfg.Secrets.SyncInterval = defaultSecretsSyncInterval
	}
	if cfg.TelemetryPrefs.MetricsInterval == 0 {
		cfg.TelemetryPrefs.MetricsInterval = defaultTelemetryMetrics
	}
	if cfg.TelemetryPrefs.ActivityInterval == 0 {
		cfg.TelemetryPrefs.ActivityInterval = defaultTelemetryActivity
	}

	if cfg.TLS.CertFile == "" {
		cfg.TLS.CertFile = defaultTLSCertFile
	}
	if cfg.TLS.KeyFile == "" {
		cfg.TLS.KeyFile = defaultTLSKeyFile
	}
	if cfg.TLS.CACertFile == "" {
		cfg.TLS.CACertFile = defaultTLSCACertFile
	}

	if cfg.Intervals.Heartbeat == 0 {
		cfg.Intervals.Heartbeat = 60 * time.Second
	}
	if cfg.Intervals.PolicySync == 0 {
		cfg.Intervals.PolicySync = 30 * time.Minute
	}
	if cfg.Intervals.Scan == 0 {
		cfg.Intervals.Scan = 15 * time.Minute
	}
	if cfg.Intervals.Telemetry == 0 {
		cfg.Intervals.Telemetry = time.Minute
	}
}
