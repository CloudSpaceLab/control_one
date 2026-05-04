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
	APIURL         string `mapstructure:"api_url"`
	BootstrapToken string `mapstructure:"bootstrap_token"`
	NodeName       string `mapstructure:"node_name"`
	StateFile      string `mapstructure:"state_file"`
	PolicyDir      string `mapstructure:"policy_dir"`
	LogDir         string `mapstructure:"log_dir"`
	PluginDir      string `mapstructure:"plugin_dir"`

	Scanner struct {
		Timeout       time.Duration `mapstructure:"timeout"`
		Shell         string        `mapstructure:"shell"`
		MaxConcurrent int           `mapstructure:"max_concurrent"`
		Enabled       bool          `mapstructure:"enabled"`
		Preferred     []string      `mapstructure:"preferred"`
		Fallback      string        `mapstructure:"fallback"`
		UseRealScan   bool          `mapstructure:"use_real_scan"`
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
		CollectLogs      bool               `mapstructure:"collect_logs"`
		LogNamespaces    []string           `mapstructure:"log_namespaces"`
		FileIntegrity    bool               `mapstructure:"file_integrity"`
		MetricsInterval  time.Duration      `mapstructure:"metrics_interval"`
		ActivityInterval time.Duration      `mapstructure:"activity_interval"`
		LogSources       []LogSourceConfig  `mapstructure:"log_sources"`
		Triggers         []LogTriggerConfig `mapstructure:"triggers"`
	} `mapstructure:"telemetry_prefs"`

	Wizard struct {
		Enabled                  bool          `mapstructure:"enabled"`
		AutoGenerateCertificates bool          `mapstructure:"auto_generate_certificates"`
		CertValidity             time.Duration `mapstructure:"cert_validity"`
		Hosts                    []string      `mapstructure:"hosts"`
		Organization             string        `mapstructure:"organization"`
		RunAccessSync            bool          `mapstructure:"run_access_sync"`
		RunSecretsSync           bool          `mapstructure:"run_secrets_sync"`
		RunProvisioning          bool          `mapstructure:"run_provisioning"`
		RunCompliance            bool          `mapstructure:"run_compliance"`
		EmitSummary              bool          `mapstructure:"emit_summary"`
		Timeout                  time.Duration `mapstructure:"timeout"`
	} `mapstructure:"wizard"`

	Hooks HooksConfig `mapstructure:"hooks"`

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
		Template        string            `mapstructure:"template"`
		Provider        string            `mapstructure:"provider"`
		Metadata        map[string]string `mapstructure:"metadata"`
		Baselines       []string          `mapstructure:"baselines"`
		AutoRemediation bool              `mapstructure:"auto_remediation"`
	} `mapstructure:"provisioning"`

	TLS struct {
		CertFile   string `mapstructure:"cert_file"`
		KeyFile    string `mapstructure:"key_file"`
		CACertFile string `mapstructure:"ca_cert_file"`
	} `mapstructure:"tls"`

	// SSHTunnel exposes a local SSH listener that the bastion proxies into.
	// When enabled, the agent emits bastion.session.{open,close} events so
	// privileged sessions show up in the forensic timeline.
	SSHTunnel struct {
		Enabled        bool   `mapstructure:"enabled"`
		ListenAddr     string `mapstructure:"listen_addr"`    // default :2222
		ClientCAFile   string `mapstructure:"client_ca_file"` // bastion's client-cert CA
		ServerCertFile string `mapstructure:"server_cert_file"`
		ServerKeyFile  string `mapstructure:"server_key_file"`
		UpstreamAddr   string `mapstructure:"upstream_addr"` // default 127.0.0.1:22
	} `mapstructure:"ssh_tunnel"`

	Intervals struct {
		Heartbeat    time.Duration `mapstructure:"heartbeat"`
		PolicySync   time.Duration `mapstructure:"policy_sync"`
		Scan         time.Duration `mapstructure:"scan"`
		Telemetry    time.Duration `mapstructure:"telemetry"`
		Provisioning time.Duration `mapstructure:"provisioning"`
		DLP          time.Duration `mapstructure:"dlp"`
	} `mapstructure:"intervals"`

	DLP struct {
		Enabled     bool     `mapstructure:"enabled"`
		ScanPaths   []string `mapstructure:"scan_paths"`
		MaxFileSize int64    `mapstructure:"max_file_size_mb"`
	} `mapstructure:"dlp"`

	SessionRecording struct {
		Enabled          bool          `mapstructure:"enabled"`
		Backend          string        `mapstructure:"backend"`
		StoragePath      string        `mapstructure:"storage_path"`
		RetentionDays    int           `mapstructure:"retention_days"`
		MaxSizeMB        int           `mapstructure:"max_size_mb"`
		Compress         bool          `mapstructure:"compress"`
		UploadInterval   time.Duration `mapstructure:"upload_interval"`
		SessionTypes     []string      `mapstructure:"session_types"`
		RecordSSH        bool          `mapstructure:"record_ssh"`
		RecordTerminal   bool          `mapstructure:"record_terminal"`
		RecordCommands   bool          `mapstructure:"record_commands"`
		TlogPath         string        `mapstructure:"tlog_path"`
		AuditxPath       string        `mapstructure:"auditx_path"`
		OpenReplayAPIKey string        `mapstructure:"openreplay_api_key"`
		OpenReplayURL    string        `mapstructure:"openreplay_url"`
	} `mapstructure:"session_recording"`
}

// NormalizeLogSourceConfig applies default values and sanitizes the provided log source configuration.
func NormalizeLogSourceConfig(src *LogSourceConfig) {
	if src == nil {
		return
	}
	src.applyDefaults()
}

// NormalizeLogSources returns a new slice where defaults have been applied to each log source config.
func NormalizeLogSources(sources []LogSourceConfig) []LogSourceConfig {
	if len(sources) == 0 {
		return nil
	}
	normalized := make([]LogSourceConfig, len(sources))
	for i := range sources {
		normalized[i] = sources[i]
		normalized[i].applyDefaults()
	}
	return normalized
}

// NormalizeLogTriggers returns a new slice where defaults have been applied to each trigger config.
func NormalizeLogTriggers(triggers []LogTriggerConfig) []LogTriggerConfig {
	if len(triggers) == 0 {
		return nil
	}
	normalized := make([]LogTriggerConfig, len(triggers))
	for i := range triggers {
		normalized[i] = triggers[i]
		normalized[i].applyDefaults()
	}
	return normalized
}

// LogFormatRuleConfig describes a regex-driven formatting rule usable by the
// generic formatter to normalize heterogeneous program logs.
type LogFormatRuleConfig struct {
	Regex           string            `mapstructure:"regex"`
	TimestampField  string            `mapstructure:"timestamp_field"`
	TimestampLayout string            `mapstructure:"timestamp_layout"`
	SeverityField   string            `mapstructure:"severity_field"`
	SeverityMap     map[string]string `mapstructure:"severity_map"`
	ProgramField    string            `mapstructure:"program_field"`
	MessageTemplate string            `mapstructure:"message_template"`
	Fields          map[string]string `mapstructure:"fields"`
	Labels          map[string]string `mapstructure:"labels"`
}

// LogTriggerConfig defines a regex-based trigger that can emit hooks and optionally run a script.
type LogTriggerConfig struct {
	ID             string            `mapstructure:"id"`
	Program        string            `mapstructure:"program"`
	Regex          string            `mapstructure:"regex"`
	Labels         map[string]string `mapstructure:"labels"`
	Script         string            `mapstructure:"script"`
	Timeout        time.Duration     `mapstructure:"timeout"`
	MaxRuns        int               `mapstructure:"max_runs"`
	Cooldown       time.Duration     `mapstructure:"cooldown"`
	HooksEnabled   bool              `mapstructure:"hooks_enabled"`
	ScriptsEnabled bool              `mapstructure:"scripts_enabled"`
}

func (t *LogTriggerConfig) applyDefaults() {
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = "trigger-" + strings.TrimSpace(t.Program)
	}
	if strings.TrimSpace(t.Regex) == "" {
		t.Regex = ".*"
	}
	if t.Timeout == 0 {
		t.Timeout = 10 * time.Second
	}
	if t.MaxRuns == 0 {
		t.MaxRuns = -1 // unlimited unless specified
	}
	if t.Cooldown == 0 {
		t.Cooldown = 5 * time.Second
	}
}

func (r *LogFormatRuleConfig) applyDefaults() {
	if r.SeverityMap == nil {
		r.SeverityMap = map[string]string{}
	}
	if r.Fields == nil {
		r.Fields = map[string]string{}
	}
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	if strings.TrimSpace(r.Regex) == "" {
		r.Regex = ".*"
	}
	if r.TimestampLayout == "" {
		r.TimestampLayout = time.RFC3339Nano
	}
	if r.SeverityMap["default"] == "" {
		r.SeverityMap["default"] = defaultLogDefaultSeverity
	}
}

const (
	defaultConfigPath           = "/etc/control-one/nodeagent.yaml"
	defaultStateFile            = "/var/lib/control-one/nodeagent/state.json"
	defaultPolicyDir            = "/var/lib/control-one/nodeagent/policies"
	defaultLogDir               = "/var/log/control-one/nodeagent"
	defaultPluginDir            = "/opt/control-one/nodeagent/plugins"
	defaultTLSCertFile          = "/var/lib/control-one/nodeagent/certs/nodeagent.crt"
	defaultTLSKeyFile           = "/var/lib/control-one/nodeagent/certs/nodeagent.key"
	defaultTLSCACertFile        = "/var/lib/control-one/nodeagent/certs/ca.crt"
	defaultPolicyPublicKeyFile  = "/var/lib/control-one/nodeagent/keys/policy_pub.pem"
	defaultPolicyMetadataFile   = "/var/lib/control-one/nodeagent/policies/policies.meta.json"
	defaultScannerTimeout       = 30 * time.Second
	defaultMeshStateFile        = "/var/lib/control-one/nodeagent/mesh/state.json"
	defaultMeshNamespace        = "default"
	defaultMeshCIDR             = "10.1.0.0/16"
	defaultProvisioningInterval = 30 * time.Minute
	defaultAccessSyncInterval   = 30 * time.Minute
	defaultSecretsSyncInterval  = 15 * time.Minute
	defaultTelemetryMetrics     = time.Minute
	defaultTelemetryActivity    = 5 * time.Minute
	defaultWizardCertValidity   = 365 * 24 * time.Hour
	defaultHooksMaxQueue        = 1024
	defaultHooksMaxConcurrency  = 4
	defaultLogBatchSize         = 200
	defaultLogBufferSize        = 512
	defaultLogFlushInterval     = 5 * time.Second
	defaultLogPollInterval      = time.Second
	defaultLogDefaultSeverity   = "info"
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
	if c.SessionRecording.Enabled && c.SessionRecording.StoragePath != "" {
		dirs = append(dirs, c.SessionRecording.StoragePath)
	}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			// Diagnose the common case where a path component is a regular
			// file (e.g. an old binary left at /opt/control-one/nodeagent
			// blocks creation of /opt/control-one/nodeagent/plugins).
			// Re-running the install/repair script fixes this automatically.
			for p := dir; p != "/" && p != "."; p = filepath.Dir(p) {
				if fi, statErr := os.Stat(p); statErr == nil && !fi.IsDir() {
					return fmt.Errorf(
						"create dir %s: path component %q is a file, not a directory — "+
							"re-run the install/repair script to fix this automatically: %w",
						dir, p, err,
					)
				}
			}
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
	v.SetDefault("scanner.enabled", true)
	v.SetDefault("scanner.preferred", []string{"openscap", "inspec", "ansible", "trivy"})
	v.SetDefault("scanner.fallback", "builtin")
	v.SetDefault("scanner.use_real_scan", true)

	v.SetDefault("session_recording.enabled", false)
	v.SetDefault("session_recording.backend", "tlog")
	v.SetDefault("session_recording.storage_path", "/var/lib/control-one/nodeagent/sessions")
	v.SetDefault("session_recording.retention_days", 90)
	v.SetDefault("session_recording.max_size_mb", 1024)
	v.SetDefault("session_recording.compress", true)
	v.SetDefault("session_recording.upload_interval", "5m")
	v.SetDefault("session_recording.session_types", []string{"terminal", "ssh"})
	v.SetDefault("session_recording.record_ssh", true)
	v.SetDefault("session_recording.record_terminal", true)
	v.SetDefault("session_recording.record_commands", true)
	v.SetDefault("session_recording.tlog_path", "/usr/bin/tlog-rec")
	v.SetDefault("session_recording.auditx_path", "/usr/bin/auditx")
	v.SetDefault("session_recording.openreplay_api_key", "")
	v.SetDefault("session_recording.openreplay_url", "")

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
	v.SetDefault("provisioning.provider", "")
	v.SetDefault("provisioning.metadata", map[string]any{})
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
	v.SetDefault("telemetry_prefs.log_sources", []map[string]any{})
	v.SetDefault("telemetry_prefs.triggers", []map[string]any{})

	v.SetDefault("wizard.enabled", true)
	v.SetDefault("wizard.auto_generate_certificates", true)
	v.SetDefault("wizard.cert_validity", defaultWizardCertValidity.String())
	v.SetDefault("wizard.hosts", []string{})
	v.SetDefault("wizard.organization", "Control One")
	v.SetDefault("wizard.run_access_sync", true)
	v.SetDefault("wizard.run_secrets_sync", true)
	v.SetDefault("wizard.run_provisioning", false)
	v.SetDefault("wizard.run_compliance", false)
	v.SetDefault("wizard.emit_summary", true)
	v.SetDefault("wizard.timeout", "45s")

	v.SetDefault("hooks.enabled", true)
	v.SetDefault("hooks.auto_run_default", true)
	v.SetDefault("hooks.max_queue_size", defaultHooksMaxQueue)
	v.SetDefault("hooks.max_concurrency", defaultHooksMaxConcurrency)
	v.SetDefault("hooks.bootstrap_subscriptions", []map[string]any{})

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
	v.SetDefault("intervals.dlp", "24h")

	v.SetDefault("dlp.enabled", false)
	v.SetDefault("dlp.scan_paths", []string{"/var/log", "/etc", "/home"})
	v.SetDefault("dlp.max_file_size_mb", 100)

	v.SetDefault("scanner.enabled", true)
	v.SetDefault("scanner.preferred", []string{"openscap", "inspec", "ansible", "trivy"})
	v.SetDefault("scanner.fallback", "builtin")
	v.SetDefault("scanner.use_real_scan", true)

	v.SetDefault("session_recording.enabled", false)
	v.SetDefault("session_recording.backend", "tlog")
	v.SetDefault("session_recording.storage_path", "/var/lib/control-one/nodeagent/sessions")
	v.SetDefault("session_recording.retention_days", 90)
	v.SetDefault("session_recording.max_size_mb", 1024)
	v.SetDefault("session_recording.compress", true)
	v.SetDefault("session_recording.upload_interval", "5m")
	v.SetDefault("session_recording.session_types", []string{"terminal", "ssh"})
	v.SetDefault("session_recording.record_ssh", true)
	v.SetDefault("session_recording.record_terminal", true)
	v.SetDefault("session_recording.record_commands", true)
	v.SetDefault("session_recording.tlog_path", "/usr/bin/tlog-rec")
	v.SetDefault("session_recording.auditx_path", "/usr/bin/auditx")
	v.SetDefault("session_recording.openreplay_api_key", "")
	v.SetDefault("session_recording.openreplay_url", "")
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
	if cfg.Scanner.Fallback == "" {
		cfg.Scanner.Fallback = "builtin"
	}
	if len(cfg.Scanner.Preferred) == 0 {
		cfg.Scanner.Preferred = []string{"openscap", "inspec", "ansible", "trivy"}
	}
	if cfg.SessionRecording.StoragePath == "" {
		cfg.SessionRecording.StoragePath = "/var/lib/control-one/nodeagent/sessions"
	}
	if cfg.SessionRecording.RetentionDays <= 0 {
		cfg.SessionRecording.RetentionDays = 90
	}
	if cfg.SessionRecording.MaxSizeMB <= 0 {
		cfg.SessionRecording.MaxSizeMB = 1024
	}
	if cfg.SessionRecording.UploadInterval == 0 {
		cfg.SessionRecording.UploadInterval = 5 * time.Minute
	}
	if len(cfg.SessionRecording.SessionTypes) == 0 {
		cfg.SessionRecording.SessionTypes = []string{"terminal", "ssh"}
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

	// Provisioning: ensure maps and normalize provider
	if cfg.Provisioning.Metadata == nil {
		cfg.Provisioning.Metadata = map[string]string{}
	}
	cfg.Provisioning.Provider = strings.ToLower(strings.TrimSpace(cfg.Provisioning.Provider))

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
	if cfg.Wizard.CertValidity == 0 {
		cfg.Wizard.CertValidity = defaultWizardCertValidity
	}
	if cfg.Hooks.MaxQueueSize <= 0 {
		cfg.Hooks.MaxQueueSize = defaultHooksMaxQueue
	}
	if cfg.Hooks.MaxConcurrency <= 0 {
		cfg.Hooks.MaxConcurrency = defaultHooksMaxConcurrency
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

	for i := range cfg.TelemetryPrefs.LogSources {
		cfg.TelemetryPrefs.LogSources[i].applyDefaults()
	}
	for i := range cfg.TelemetryPrefs.Triggers {
		cfg.TelemetryPrefs.Triggers[i].applyDefaults()
	}
}

// HooksConfig captures configuration for the event hook system.
type HooksConfig struct {
	Enabled                bool                     `mapstructure:"enabled"`
	AutoRunDefault         bool                     `mapstructure:"auto_run_default"`
	MaxQueueSize           int                      `mapstructure:"max_queue_size"`
	MaxConcurrency         int                      `mapstructure:"max_concurrency"`
	BootstrapSubscriptions []HookSubscriptionConfig `mapstructure:"bootstrap_subscriptions"`
}

// LogSourceConfig describes how the telemetry service should collect and parse logs
// for a specific program or subsystem across supported operating systems.
type LogSourceConfig struct {
	Program       string                `mapstructure:"program"`
	Type          string                `mapstructure:"type"`
	Paths         []string              `mapstructure:"paths"`
	JournalUnits  []string              `mapstructure:"journal_units"`
	EventChannels []string              `mapstructure:"event_channels"`
	Formatter     string                `mapstructure:"formatter"`
	SeverityMap   map[string]string     `mapstructure:"severity_map"`
	BatchSize     int                   `mapstructure:"batch_size"`
	BufferSize    int                   `mapstructure:"buffer_size"`
	FlushInterval time.Duration         `mapstructure:"flush_interval"`
	PollInterval  time.Duration         `mapstructure:"poll_interval"`
	Labels        map[string]string     `mapstructure:"labels"`
	Disabled      bool                  `mapstructure:"disabled"`
	FormatRules   []LogFormatRuleConfig `mapstructure:"format_rules"`
}

func (l *LogSourceConfig) applyDefaults() {
	if l.BatchSize <= 0 {
		l.BatchSize = defaultLogBatchSize
	}
	if l.BufferSize <= 0 {
		l.BufferSize = defaultLogBufferSize
	}
	if l.FlushInterval <= 0 {
		l.FlushInterval = defaultLogFlushInterval
	}
	if l.PollInterval <= 0 {
		l.PollInterval = defaultLogPollInterval
	}
	if strings.TrimSpace(l.Type) == "" {
		l.Type = "auto"
	}
	if l.SeverityMap == nil {
		l.SeverityMap = map[string]string{}
	}
	if l.Labels == nil {
		l.Labels = map[string]string{}
	}
	if l.SeverityMap["default"] == "" {
		l.SeverityMap["default"] = defaultLogDefaultSeverity
	}
	if strings.TrimSpace(l.Program) == "" {
		if len(l.Paths) > 0 {
			l.Program = filepath.Base(l.Paths[0])
		} else if len(l.JournalUnits) > 0 {
			l.Program = l.JournalUnits[0]
		} else if len(l.EventChannels) > 0 {
			l.Program = l.EventChannels[0]
		} else {
			l.Program = "generic"
		}
	}
	for i := range l.FormatRules {
		l.FormatRules[i].applyDefaults()
	}
	if len(l.FormatRules) > 0 && strings.TrimSpace(l.Formatter) == "" {
		l.Formatter = "generic"
	}
}

// HookSubscriptionConfig defines bootstrap subscriptions loaded from config.
type HookSubscriptionConfig struct {
	ID        string        `mapstructure:"id"`
	EventID   string        `mapstructure:"event_id"`
	Filter    string        `mapstructure:"filter"`
	Mode      string        `mapstructure:"mode"`
	Handler   HandlerConfig `mapstructure:"handler"`
	RunPolicy struct {
		Timeout     time.Duration `mapstructure:"timeout"`
		MemoryMB    int           `mapstructure:"memory_mb"`
		Concurrency int           `mapstructure:"concurrency"`
		MaxRetries  int           `mapstructure:"max_retries"`
	} `mapstructure:"run_policy"`
	RemediateAllowed bool     `mapstructure:"remediate_allowed"`
	RBACRoles        []string `mapstructure:"rbac_roles"`
}

// HandlerConfig describes handler bootstrap configuration.
type HandlerConfig struct {
	Type     string `mapstructure:"type"`
	Language string `mapstructure:"language"`
	Inline   string `mapstructure:"inline"`
	Source   string `mapstructure:"source"`
}
