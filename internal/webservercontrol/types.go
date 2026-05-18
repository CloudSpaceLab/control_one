package webservercontrol

import (
	"context"
	"time"
)

type WebServerAdapter interface {
	Detect(ctx context.Context) ([]WebServerInstance, error)
	Plan(ctx context.Context, instance WebServerInstance, desired WebPolicy) (ConfigPlan, error)
	Validate(ctx context.Context, plan ConfigPlan) error
	Apply(ctx context.Context, plan ConfigPlan) (ConfigReceipt, error)
	Rollback(ctx context.Context, receipt ConfigReceipt) error
}

type WebServerInstance struct {
	Kind          string           `json:"kind"`
	Version       string           `json:"version,omitempty"`
	ServiceName   string           `json:"service_name,omitempty"`
	ConfigPath    string           `json:"config_path,omitempty"`
	AccessLogPath string           `json:"access_log_path,omitempty"`
	ErrorLogPath  string           `json:"error_log_path,omitempty"`
	VHosts        []map[string]any `json:"vhosts,omitempty"`
	Capabilities  map[string]any   `json:"capabilities,omitempty"`
}

type WebPolicy struct {
	Mode                      string         `json:"mode,omitempty"`
	ManagedDir                string         `json:"managed_dir,omitempty"`
	LogDir                    string         `json:"log_dir,omitempty"`
	TrustedProxyCIDRs         []string       `json:"trusted_proxy_cidrs,omitempty"`
	BlockCIDRs                []string       `json:"block_cidrs,omitempty"`
	BlockTTLSeconds           int            `json:"block_ttl_seconds,omitempty"`
	AllowConfigHook           bool           `json:"allow_config_hook,omitempty"`
	ConfigHookStrategy        string         `json:"config_hook_strategy,omitempty"`
	Approved                  bool           `json:"approved,omitempty"`
	MaintenanceWindowApproved bool           `json:"maintenance_window_approved,omitempty"`
	AllowRestart              bool           `json:"allow_restart,omitempty"`
	HealthCheckURL            string         `json:"health_check_url,omitempty"`
	MaxBlockChanges           int            `json:"max_block_changes,omitempty"`
	Reason                    string         `json:"reason,omitempty"`
	Metadata                  map[string]any `json:"metadata,omitempty"`
}

type ManagedFile struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
}

type ConfigPlan struct {
	Action              string            `json:"action"`
	Mode                string            `json:"mode"`
	Instance            WebServerInstance `json:"instance"`
	ManagedDir          string            `json:"managed_dir"`
	LogDir              string            `json:"log_dir,omitempty"`
	Files               []ManagedFile     `json:"files,omitempty"`
	ConfigHookPath      string            `json:"config_hook_path,omitempty"`
	ConfigHookLine      string            `json:"config_hook_line,omitempty"`
	ConfigHookStrategy  string            `json:"config_hook_strategy,omitempty"`
	ValidationCommand   []string          `json:"validation_command,omitempty"`
	ReloadCommand       []string          `json:"reload_command,omitempty"`
	HealthCheckURL      string            `json:"health_check_url,omitempty"`
	RequiresApproval    bool              `json:"requires_approval"`
	RequiresRestart     bool              `json:"requires_restart"`
	Warnings            []string          `json:"warnings,omitempty"`
	Diff                string            `json:"diff,omitempty"`
	ChecksumBefore      string            `json:"checksum_before,omitempty"`
	EstimatedBlockCount int               `json:"estimated_block_count,omitempty"`
}

type ConfigReceipt struct {
	Action           string         `json:"action"`
	ChecksumBefore   string         `json:"checksum_before,omitempty"`
	ChecksumAfter    string         `json:"checksum_after,omitempty"`
	ValidationStatus string         `json:"validation_status"`
	ReloadStatus     string         `json:"reload_status"`
	RollbackRef      string         `json:"rollback_ref,omitempty"`
	Diff             string         `json:"diff,omitempty"`
	ManagedFiles     []string       `json:"managed_files,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}
