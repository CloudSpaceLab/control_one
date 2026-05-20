package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config captures control plane service settings.
type Config struct {
	HTTP           HTTPConfig           `mapstructure:"http"`
	TLS            TLSConfig            `mapstructure:"tls"`
	Observability  ObservabilityConfig  `mapstructure:"observability"`
	Database       DatabaseConfig       `mapstructure:"database"`
	Worker         WorkerConfig         `mapstructure:"worker"`
	Jobs           JobsConfig           `mapstructure:"jobs"`
	Auth           AuthConfig           `mapstructure:"auth"`
	Registration   RegistrationConfig   `mapstructure:"registration"`
	Enrollment     EnrollmentConfig     `mapstructure:"enrollment"`
	Agent          AgentConfig          `mapstructure:"agent"`
	Remediation    RemediationConfig    `mapstructure:"remediation"`
	Secrets        SecretsConfig        `mapstructure:"secrets"`
	WebAuthn       WebAuthnConfig       `mapstructure:"webauthn"`
	Doris          DorisConfig          `mapstructure:"doris"`
	Bastion        BastionConfig        `mapstructure:"bastion"`
	LDAP           LDAPConfig           `mapstructure:"ldap"`
	IPIntel        IPIntelConfig        `mapstructure:"ipintel"`
	ThreatIntel    ThreatIntelConfig    `mapstructure:"threat_intel"`
	IPBehavior     IPBehaviorConfig     `mapstructure:"ip_behavior"`
	OfflineContent OfflineContentConfig `mapstructure:"offline_content"`
	Policy         PolicyConfig         `mapstructure:"policy"`
	AML            AMLConfig            `mapstructure:"aml"`
}

// AMLConfig points Control One at the existing CloudSpaceLab/aml-service.
// When BaseURL is empty, AML routes fail closed with 503.
type AMLConfig struct {
	BaseURL            string        `mapstructure:"base_url"`
	APIKey             string        `mapstructure:"api_key"`
	Timeout            time.Duration `mapstructure:"timeout"`
	AllowInsecure      bool          `mapstructure:"allow_insecure"`
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify"`
}

// PolicyConfig points the control plane at the ed25519 keys used to sign and
// verify desired policy state. PublicKeyFile is shipped to enrolling agents;
// SigningKeyPath signs server-issued desired state such as network_policy.
// When no public key is configured, enrolling agents skip signature verification.
type PolicyConfig struct {
	PublicKeyFile  string `mapstructure:"public_key_file"`
	SigningKeyPath string `mapstructure:"signing_key_path"`
}

// IPIntelConfig governs external IP enrichment cache fills. Request-path
// blacklist checks use the local threat-intel snapshot first and read only
// cached enrichment from this service. When IpqueryBaseURL is set the service
// can call a self-hosted akyriako/ipquery instance for combined geo + ASN +
// risk lookups; otherwise it can fall back to AbuseIPDB-only when an API key
// is configured. Results are cached in Postgres (ip_enrichment_cache) for
// CacheTTL; set to 0 to disable caching, default 1h.
type IPIntelConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	IpqueryBaseURL   string        `mapstructure:"ipquery_base_url"`
	AbuseIPDBKey     string        `mapstructure:"abuseipdb_api_key"`
	CacheTTL         time.Duration `mapstructure:"cache_ttl"`
	HTTPTimeout      time.Duration `mapstructure:"http_timeout"`
	AbuseScoreCutoff int           `mapstructure:"abuse_score_cutoff"` // chip emitted when score ≥ this; default 25
}

// ThreatIntelConfig governs locally downloaded blacklist/feed snapshots.
// External feeds are refreshed in the background; request-path checks read the
// local snapshot so IP pivots and scans do not require live API calls.
type ThreatIntelConfig struct {
	SnapshotDir string `mapstructure:"snapshot_dir"`
}

// IPBehaviorConfig controls web request behavioral intelligence. Counters use
// memory by default for dev and airgapped single-node deployments, and Redis
// when multiple control-plane replicas need a shared current window.
type IPBehaviorConfig struct {
	Counters IPBehaviorCountersConfig `mapstructure:"counters"`
}

type IPBehaviorCountersConfig struct {
	Backend       string `mapstructure:"backend"` // memory | redis
	RedisAddress  string `mapstructure:"redis_address"`
	RedisDB       int    `mapstructure:"redis_db"`
	RedisPassword string `mapstructure:"redis_password"`
}

// OfflineContentConfig controls signed content bundles for airgapped
// deployments. Bundles can carry geo/IP intelligence, threat feeds, detection
// rules, parser profiles, and webserver adapter templates.
type OfflineContentConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	RootDir        string `mapstructure:"root_dir"`
	PublicKeyFile  string `mapstructure:"public_key_file"`
	MaxBundleBytes int64  `mapstructure:"max_bundle_bytes"`
}

// LDAPConfig mirrors auth.LDAPConfig so viper can unmarshal directly. See
// controlplane/internal/auth/ldap.go for field semantics.
type LDAPConfig struct {
	Enabled      bool              `mapstructure:"enabled"`
	URL          string            `mapstructure:"url"`
	StartTLS     bool              `mapstructure:"start_tls"`
	SkipVerify   bool              `mapstructure:"skip_verify"`
	BindDN       string            `mapstructure:"bind_dn"`
	BindPassword string            `mapstructure:"bind_password"`
	UserBaseDN   string            `mapstructure:"user_base_dn"`
	UserFilter   string            `mapstructure:"user_filter"`
	GroupBaseDN  string            `mapstructure:"group_base_dn"`
	GroupFilter  string            `mapstructure:"group_filter"`
	GroupAttr    string            `mapstructure:"group_attr"`
	EmailAttr    string            `mapstructure:"email_attr"`
	NameAttr     string            `mapstructure:"name_attr"`
	GroupRoleMap map[string]string `mapstructure:"group_role_map"`
	DefaultRole  string            `mapstructure:"default_role"`
}

// BastionConfig drives the optional SSH bastion proxy. When enabled the
// controlplane starts an sshproxy listener that authenticates operators
// via tenant-CA-signed user certs, forwards them to the target node's
// agent (which terminates into local sshd), and emits
// bastion.session.{open,close} events into the eventbus + Doris so the
// forensic timeline cross-links every privileged session to the
// connection rows it produced.
type BastionConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	ListenAddr      string `mapstructure:"listen_addr"`
	HostKeyFile     string `mapstructure:"host_key_file"`
	CAPublicKeyFile string `mapstructure:"ca_public_key_file"`
	NodeDialerAddr  string `mapstructure:"node_dialer_addr"` // host:port of nodeagent SSH tunnel listener
}

// DorisConfig describes the Apache Doris analytic store. When DSN +
// HTTPEndpoint are both set the controlplane writes raw events to Doris in
// addition to its existing Postgres rollups; otherwise event ingest still
// works (rows live in the journal + rollups) and Doris features remain off.
type DorisConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	DSN             string `mapstructure:"dsn"`
	HTTPEndpoint    string `mapstructure:"http_endpoint"`
	Database        string `mapstructure:"database"`
	User            string `mapstructure:"user"`
	Password        string `mapstructure:"password"`
	ApplyMigrations bool   `mapstructure:"apply_migrations"`
}

// WebAuthnConfig configures the relying-party identity advertised to browsers
// during WebAuthn ceremonies. RPID must be the bare hostname users reach the
// console at; RPOrigin must be the full origin (scheme + host + port).
// Leaving both blank disables WebAuthn enrolment + step-up.
type WebAuthnConfig struct {
	RPID     string `mapstructure:"rp_id"`
	RPName   string `mapstructure:"rp_name"`
	RPOrigin string `mapstructure:"rp_origin"`
}

// SecretsConfig controls at-rest encryption of sensitive rows (e.g. provider
// credentials). `EncryptionKey` must be 32 bytes raw or 64 hex chars.
type SecretsConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"`
}

// RemediationConfig controls runtime caps on auto-remediation.
type RemediationConfig struct {
	// MaxConcurrentPerTenant caps how many remediation leases a tenant can hold
	// at once. A new remediation that would push the tenant past this ceiling
	// is deferred (its trigger call returns nil and the compliance result is
	// left unremediated until the next scan).
	MaxConcurrentPerTenant int `mapstructure:"max_concurrent_per_tenant"`
	// LeaseTTL is how long a per-node remediation lease stays valid. Expired
	// leases are swept by the storage layer on the next acquire attempt so a
	// stuck job never wedges a node forever.
	LeaseTTL time.Duration `mapstructure:"lease_ttl"`
	// MaxBlockChangesPerHour caps IP block proposal churn per tenant. It is
	// checked before proposal creation and before approval so a bad feed or
	// compromised operator token cannot flood enforcement jobs.
	MaxBlockChangesPerHour int `mapstructure:"max_block_changes_per_hour"`
	// MaxBlockChangesPerServerGroupPerHour caps block churn for a named server
	// group inside one tenant, preventing a noisy group from consuming the
	// whole tenant allowance.
	MaxBlockChangesPerServerGroupPerHour int `mapstructure:"max_block_changes_per_server_group_per_hour"`
	// MaxGlobalBlockChangesPerHour caps block proposal churn across all tenants.
	// It is a last-resort guard for shared threat-feed mistakes.
	MaxGlobalBlockChangesPerHour int `mapstructure:"max_global_block_changes_per_hour"`
	// BlockCanaryEnabled makes tenant/fleet scoped block approvals dispatch a
	// canary wave per server group before fleet-wide promotion.
	BlockCanaryEnabled bool `mapstructure:"block_canary_enabled"`
	// BlockCanaryNodesPerServerGroup controls the size of that first wave.
	BlockCanaryNodesPerServerGroup int `mapstructure:"block_canary_nodes_per_server_group"`
	// WebserverFailureCircuitThreshold trips the webserver enforcement circuit
	// after this many failed block/config actions within WebserverFailureCircuitWindow.
	WebserverFailureCircuitThreshold int           `mapstructure:"webserver_failure_circuit_threshold"`
	WebserverFailureCircuitWindow    time.Duration `mapstructure:"webserver_failure_circuit_window"`
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
	Concurrency  int           `mapstructure:"concurrency"`
	QueueSize    int           `mapstructure:"queue_size"`
	Backend      string        `mapstructure:"backend"`
	MaxAttempts  int           `mapstructure:"max_attempts"`
	RetryBackoff time.Duration `mapstructure:"retry_backoff"`
	Asynq        AsynqConfig   `mapstructure:"asynq"`
}

// AsynqConfig captures Redis-backed queue options.
type AsynqConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	RedisAddress  string `mapstructure:"redis_address"`
	RedisDB       int    `mapstructure:"redis_db"`
	RedisPassword string `mapstructure:"redis_password"`
}

// AuthConfig captures identity provider and RBAC settings.
type AuthConfig struct {
	OIDC OIDCConfig `mapstructure:"oidc"`
	RBAC RBACConfig `mapstructure:"rbac"`
}

// OIDCConfig defines OpenID Connect verification options.
type OIDCConfig struct {
	Enabled       bool                             `mapstructure:"enabled"`
	IssuerURL     string                           `mapstructure:"issuer_url"`
	ClientID      string                           `mapstructure:"client_id"`
	Audience      []string                         `mapstructure:"audience"`
	UsernameClaim string                           `mapstructure:"username_claim"`
	GroupsClaim   string                           `mapstructure:"groups_claim"`
	CacheTTL      time.Duration                    `mapstructure:"cache_ttl"`
	StaticTokens  map[string]StaticPrincipalConfig `mapstructure:"static_tokens"`
}

// StaticPrincipalConfig defines mock principals for local development/testing.
type StaticPrincipalConfig struct {
	Subject string   `mapstructure:"subject"`
	Email   string   `mapstructure:"email"`
	Name    string   `mapstructure:"name"`
	Roles   []string `mapstructure:"roles"`
	Groups  []string `mapstructure:"groups"`
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
	APIBaseURL      string          `mapstructure:"api_base_url"`
	Token           string          `mapstructure:"token"`
	Template        string          `mapstructure:"template"`
	Provider        string          `mapstructure:"provider"`
	Baselines       []string        `mapstructure:"baselines"`
	AutoRemediation bool            `mapstructure:"auto_remediation"`
	TLS             ClientTLSConfig `mapstructure:"tls"`
}

// ComplianceJobConfig defines outbound settings for compliance jobs.
type ComplianceJobConfig struct {
	APIBaseURL      string          `mapstructure:"api_base_url"`
	Token           string          `mapstructure:"token"`
	Region          string          `mapstructure:"region"`
	RuleSets        []string        `mapstructure:"rule_sets"`
	Certifications  []string        `mapstructure:"certifications"`
	AutoApply       bool            `mapstructure:"auto_apply"`
	ScheduleEnabled bool            `mapstructure:"schedule_enabled"`
	ScheduleCron    string          `mapstructure:"schedule_cron"`
	TLS             ClientTLSConfig `mapstructure:"tls"`
}

// ClientTLSConfig captures TLS options for outbound clients.
type ClientTLSConfig struct {
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	CACertFile string `mapstructure:"ca_cert_file"`
}

// EnrollmentConfig captures CA settings for single-command node enrollment.
type EnrollmentConfig struct {
	CAKeyFile  string `mapstructure:"ca_key_file"`
	CACertFile string `mapstructure:"ca_cert_file"`
}

// AgentConfig captures agent binary distribution settings.
type AgentConfig struct {
	BinaryDir            string `mapstructure:"binary_dir"`
	SigningKeyPath       string `mapstructure:"signing_key_path"`
	SigningPublicKeyPath string `mapstructure:"signing_public_key_path"`
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
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

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
	v.SetDefault("worker.backend", "memory")
	v.SetDefault("worker.max_attempts", 1)
	v.SetDefault("worker.retry_backoff", time.Second*5)
	v.SetDefault("worker.asynq.enabled", false)
	v.SetDefault("worker.asynq.redis_address", "127.0.0.1:6379")
	v.SetDefault("worker.asynq.redis_db", 0)
	v.SetDefault("worker.asynq.redis_password", "")

	v.SetDefault("jobs.provisioning.auto_remediation", true)
	v.SetDefault("jobs.compliance.auto_apply", true)
	v.SetDefault("jobs.compliance.schedule_enabled", false)
	v.SetDefault("jobs.compliance.schedule_cron", "0 */6 * * *")

	v.SetDefault("remediation.max_concurrent_per_tenant", 10)
	v.SetDefault("remediation.lease_ttl", 10*time.Minute)
	v.SetDefault("remediation.max_block_changes_per_hour", 100)
	v.SetDefault("remediation.max_block_changes_per_server_group_per_hour", 25)
	v.SetDefault("remediation.max_global_block_changes_per_hour", 1000)
	v.SetDefault("remediation.block_canary_enabled", false)
	v.SetDefault("remediation.block_canary_nodes_per_server_group", 1)
	v.SetDefault("remediation.webserver_failure_circuit_threshold", 3)
	v.SetDefault("remediation.webserver_failure_circuit_window", time.Hour)

	v.SetDefault("registration.bootstrap_tokens", []string{})
	v.SetDefault("aml.timeout", 10*time.Second)
	_ = v.BindEnv("aml.base_url", "AML_SERVICE_BASE_URL")
	_ = v.BindEnv("aml.api_key", "AML_SERVICE_API_KEY")

	v.SetDefault("auth.oidc.enabled", false)
	v.SetDefault("auth.oidc.username_claim", "email")
	v.SetDefault("auth.oidc.groups_claim", "groups")
	v.SetDefault("auth.oidc.cache_ttl", time.Minute*5)
	v.SetDefault("auth.rbac.default_role", "viewer")

	// IP intelligence (Investigate)
	v.SetDefault("ipintel.enabled", true)
	v.SetDefault("ipintel.cache_ttl", time.Hour)
	v.SetDefault("ipintel.http_timeout", 5*time.Second)
	v.SetDefault("ipintel.abuse_score_cutoff", 25)
	_ = v.BindEnv("ipintel.ipquery_base_url", "IPQUERY_BASE_URL")
	_ = v.BindEnv("ipintel.abuseipdb_api_key", "ABUSEIPDB_API_KEY")

	v.SetDefault("threat_intel.snapshot_dir", "data/threat-intel")
	_ = v.BindEnv("threat_intel.snapshot_dir", "THREAT_INTEL_SNAPSHOT_DIR")

	v.SetDefault("ip_behavior.counters.backend", "memory")
	v.SetDefault("ip_behavior.counters.redis_address", "")
	v.SetDefault("ip_behavior.counters.redis_db", 0)
	v.SetDefault("ip_behavior.counters.redis_password", "")
	_ = v.BindEnv("ip_behavior.counters.backend", "IP_BEHAVIOR_COUNTERS_BACKEND")
	_ = v.BindEnv("ip_behavior.counters.redis_address", "IP_BEHAVIOR_REDIS_ADDRESS")
	_ = v.BindEnv("ip_behavior.counters.redis_password", "IP_BEHAVIOR_REDIS_PASSWORD")

	v.SetDefault("offline_content.enabled", false)
	v.SetDefault("offline_content.root_dir", "data/offline-content")
	v.SetDefault("offline_content.max_bundle_bytes", int64(256*1024*1024))
	_ = v.BindEnv("offline_content.enabled", "OFFLINE_CONTENT_ENABLED")
	_ = v.BindEnv("offline_content.root_dir", "OFFLINE_CONTENT_ROOT_DIR")
	_ = v.BindEnv("offline_content.public_key_file", "OFFLINE_CONTENT_PUBLIC_KEY_FILE")
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
	if cfg.Remediation.MaxConcurrentPerTenant <= 0 {
		cfg.Remediation.MaxConcurrentPerTenant = 10
	}
	if cfg.Remediation.LeaseTTL <= 0 {
		cfg.Remediation.LeaseTTL = 10 * time.Minute
	}
	if cfg.Remediation.MaxBlockChangesPerHour <= 0 {
		cfg.Remediation.MaxBlockChangesPerHour = 100
	}
	if cfg.Remediation.MaxBlockChangesPerServerGroupPerHour <= 0 {
		cfg.Remediation.MaxBlockChangesPerServerGroupPerHour = 25
	}
	if cfg.Remediation.MaxGlobalBlockChangesPerHour <= 0 {
		cfg.Remediation.MaxGlobalBlockChangesPerHour = 1000
	}
	if cfg.Remediation.BlockCanaryNodesPerServerGroup <= 0 {
		cfg.Remediation.BlockCanaryNodesPerServerGroup = 1
	}
	if cfg.Remediation.WebserverFailureCircuitThreshold <= 0 {
		cfg.Remediation.WebserverFailureCircuitThreshold = 3
	}
	if cfg.Remediation.WebserverFailureCircuitWindow <= 0 {
		cfg.Remediation.WebserverFailureCircuitWindow = time.Hour
	}
	if cfg.AML.Timeout <= 0 {
		cfg.AML.Timeout = 10 * time.Second
	}
	if strings.EqualFold(strings.TrimSpace(cfg.IPBehavior.Counters.Backend), "redis") && strings.TrimSpace(cfg.IPBehavior.Counters.RedisAddress) == "" {
		cfg.IPBehavior.Counters.RedisAddress = cfg.Worker.Asynq.RedisAddress
		cfg.IPBehavior.Counters.RedisDB = cfg.Worker.Asynq.RedisDB
		cfg.IPBehavior.Counters.RedisPassword = cfg.Worker.Asynq.RedisPassword
	}
	if cfg.OfflineContent.RootDir == "" {
		cfg.OfflineContent.RootDir = "data/offline-content"
	}
	if cfg.OfflineContent.MaxBundleBytes <= 0 {
		cfg.OfflineContent.MaxBundleBytes = 256 * 1024 * 1024
	}
}

// Validate performs basic consistency checks on the loaded configuration.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(cfg.HTTP.Address) == "" {
		return fmt.Errorf("http.address is required")
	}
	if cfg.TLS.Enabled {
		if strings.TrimSpace(cfg.TLS.CertFile) == "" || strings.TrimSpace(cfg.TLS.KeyFile) == "" {
			return fmt.Errorf("tls.enabled requires tls.cert_file and tls.key_file")
		}
		if cfg.TLS.RequireClientTLS && strings.TrimSpace(cfg.TLS.ClientCAFile) == "" {
			return fmt.Errorf("tls.require_client_tls requires tls.client_ca_file")
		}
	}
	if strings.TrimSpace(cfg.Database.URL) == "" {
		return fmt.Errorf("database.url is required")
	}

	if backend := strings.ToLower(strings.TrimSpace(cfg.Worker.Backend)); backend == "asynq" || cfg.Worker.Asynq.Enabled {
		if strings.TrimSpace(cfg.Worker.Asynq.RedisAddress) == "" {
			return fmt.Errorf("worker.asynq.redis_address is required when asynq backend is enabled")
		}
	}
	switch strings.ToLower(strings.TrimSpace(cfg.IPBehavior.Counters.Backend)) {
	case "", "memory", "redis":
	default:
		return fmt.Errorf("ip_behavior.counters.backend must be memory or redis")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.IPBehavior.Counters.Backend), "redis") && strings.TrimSpace(cfg.IPBehavior.Counters.RedisAddress) == "" {
		return fmt.Errorf("ip_behavior.counters.redis_address is required when redis counters are enabled")
	}
	if cfg.OfflineContent.Enabled {
		if strings.TrimSpace(cfg.OfflineContent.RootDir) == "" {
			return fmt.Errorf("offline_content.root_dir is required when offline content is enabled")
		}
		if strings.TrimSpace(cfg.OfflineContent.PublicKeyFile) == "" {
			return fmt.Errorf("offline_content.public_key_file is required when offline content is enabled")
		}
	}

	if cfg.Auth.OIDC.Enabled {
		if strings.TrimSpace(cfg.Auth.OIDC.IssuerURL) == "" {
			return fmt.Errorf("auth.oidc.issuer_url is required when oidc is enabled")
		}
		if strings.TrimSpace(cfg.Auth.OIDC.ClientID) == "" {
			return fmt.Errorf("auth.oidc.client_id is required when oidc is enabled")
		}
	}

	if len(cfg.Registration.BootstrapTokens) > 0 || strings.TrimSpace(cfg.Registration.DefaultTenantID) != "" {
		if len(cfg.Registration.BootstrapTokens) == 0 && strings.TrimSpace(cfg.Registration.DefaultTenantID) == "" {
			return fmt.Errorf("registration requires at least one bootstrap token or a default tenant id")
		}
	}

	return nil
}
