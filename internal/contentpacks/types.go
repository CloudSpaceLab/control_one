package contentpacks

import "time"

const (
	SchemaVersion = 1

	SchemaOCSF = "ocsf"
	SchemaECS  = "ecs"

	RiskLow      = "low"
	RiskMedium   = "medium"
	RiskHigh     = "high"
	RiskCritical = "critical"

	SensitivityLow        = "low"
	SensitivityModerate   = "moderate"
	SensitivityHigh       = "high"
	SensitivityRestricted = "restricted"

	CollectorNodeFileLog    = "node_filelog"
	CollectorOTelFileLog    = "otel_filelog"
	CollectorSyslog         = "syslog"
	CollectorWindowsEvent   = "windows_eventlog"
	CollectorSplunkHEC      = "splunk_hec"
	CollectorKafka          = "kafka"
	CollectorOTLP           = "otlp"
	CollectorVendorAPI      = "vendor_api"
	CollectorWEF            = "wef"
	CollectorArchive        = "archive"
	CollectorPrometheus     = "prometheus"
	CollectorDatabase       = "database"
	CollectorPrivateAccess  = "private_access"
	CollectorControlOneNode = "controlone_node"

	StageJSON             = "json"
	StageSyslogRFC3164    = "syslog_rfc3164"
	StageSyslogRFC5424    = "syslog_rfc5424"
	StageCEF              = "cef"
	StageLEEF             = "leef"
	StageRegex            = "regex"
	StageGrok             = "grok"
	StageKV               = "kv"
	StageLogfmt           = "logfmt"
	StageXML              = "xml"
	StageWindowsEventData = "windows_eventdata"
	StageTimestamp        = "timestamp"
	StageFieldMap         = "field_map"
	StageRedact           = "redact"
	StageDrop             = "drop"
	StageEnrich           = "enrich"
	StageOCSFMap          = "ocsf_map"
	StageECSAlias         = "ecs_alias"

	DetectionKindSigma      = "sigma"
	DetectionKindControlOne = "controlone"

	OnErrorFail    = "fail"
	OnErrorKeepRaw = "keep_raw"
	OnErrorDrop    = "drop"

	CoverageDiscovered         = "discovered"
	CoverageProposed           = "proposed"
	CoverageApprovalRequired   = "approval_required"
	CoverageApproved           = "approved"
	CoverageConfigRendered     = "config_rendered"
	CoverageDeployed           = "deployed"
	CoverageCollecting         = "collecting"
	CoverageParserHealthy      = "parser_healthy"
	CoverageParserFailed       = "parser_failed"
	CoverageSilent             = "silent"
	CoverageBackpressured      = "backpressured"
	CoverageCollectionConflict = "collection_conflict"
	CoverageUnsupported        = "unsupported"
	CoveragePrivacyBlocked     = "privacy_blocked"
	CoverageStale              = "stale"

	PackStatusAvailable         = "available"
	PackStatusInstalled         = "installed"
	PackStatusEnabled           = "enabled"
	PackStatusDisabled          = "disabled"
	PackStatusQuarantined       = "quarantined"
	PackStatusDeprecated        = "deprecated"
	PackStatusRollbackAvailable = "rollback_available"
)

// Manifest is the signed content-pack contract. It is intentionally transport
// agnostic: node-agent collectors, OTel edge collectors, and future managed
// adapters can all consume the same source/parser/detection metadata.
type Manifest struct {
	SchemaVersion        int               `json:"schema_version" yaml:"schema_version"`
	PackID               string            `json:"pack_id" yaml:"pack_id"`
	PackVersion          string            `json:"pack_version" yaml:"pack_version"`
	DisplayName          string            `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Description          string            `json:"description,omitempty" yaml:"description,omitempty"`
	MinControlOneVersion string            `json:"min_control_one_version,omitempty" yaml:"min_control_one_version,omitempty"`
	Labels               map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	License              LicenseMetadata   `json:"license" yaml:"license"`
	Provenance           Provenance        `json:"provenance" yaml:"provenance"`
	Sources              []SourceProfile   `json:"sources" yaml:"sources"`
	Parsers              []ParserProfile   `json:"parsers" yaml:"parsers"`
	Detections           []Detection       `json:"detections,omitempty" yaml:"detections,omitempty"`
	Samples              []SampleCase      `json:"samples" yaml:"samples"`
}

type LicenseMetadata struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	SPDX string `json:"spdx,omitempty" yaml:"spdx,omitempty"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

type Provenance struct {
	Author     string     `json:"author,omitempty" yaml:"author,omitempty"`
	Repository string     `json:"repository,omitempty" yaml:"repository,omitempty"`
	Commit     string     `json:"commit,omitempty" yaml:"commit,omitempty"`
	BuiltAt    *time.Time `json:"built_at,omitempty" yaml:"built_at,omitempty"`
	Sources    []string   `json:"sources,omitempty" yaml:"sources,omitempty"`
}

type SourceProfile struct {
	SourceID            string            `json:"source_id" yaml:"source_id"`
	DisplayName         string            `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Vendor              string            `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	Product             string            `json:"product,omitempty" yaml:"product,omitempty"`
	Versions            []string          `json:"versions,omitempty" yaml:"versions,omitempty"`
	SourceClass         string            `json:"source_class" yaml:"source_class"`
	RiskClass           string            `json:"risk_class" yaml:"risk_class"`
	DataSensitivity     string            `json:"data_sensitivity" yaml:"data_sensitivity"`
	CollectorModes      []string          `json:"collector_modes" yaml:"collector_modes"`
	CollectorRecipes    []CollectorRecipe `json:"collector_recipes,omitempty" yaml:"collector_recipes,omitempty"`
	ApprovalRequired    bool              `json:"approval_required" yaml:"approval_required"`
	RequiredPrivileges  []string          `json:"required_privileges,omitempty" yaml:"required_privileges,omitempty"`
	ExpectedVolume      VolumeHint        `json:"expected_volume,omitempty" yaml:"expected_volume,omitempty"`
	RawRetentionDefault string            `json:"raw_retention_default,omitempty" yaml:"raw_retention_default,omitempty"`
	Schemas             SchemaBinding     `json:"schemas" yaml:"schemas"`
	Parsers             []string          `json:"parsers" yaml:"parsers"`
	Detections          []string          `json:"detections,omitempty" yaml:"detections,omitempty"`
	Samples             []string          `json:"samples" yaml:"samples"`
	Labels              map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type VolumeHint struct {
	EventsPerSecond int    `json:"events_per_second,omitempty" yaml:"events_per_second,omitempty"`
	BytesPerSecond  int64  `json:"bytes_per_second,omitempty" yaml:"bytes_per_second,omitempty"`
	Burst           string `json:"burst,omitempty" yaml:"burst,omitempty"`
}

type CollectorRecipe struct {
	Mode     string         `json:"mode" yaml:"mode"`
	Receiver string         `json:"receiver,omitempty" yaml:"receiver,omitempty"`
	Exporter string         `json:"exporter,omitempty" yaml:"exporter,omitempty"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

type SchemaBinding struct {
	Primary       string      `json:"primary" yaml:"primary"`
	ExportAliases []string    `json:"export_aliases,omitempty" yaml:"export_aliases,omitempty"`
	OCSF          OCSFBinding `json:"ocsf,omitempty" yaml:"ocsf,omitempty"`
}

type OCSFBinding struct {
	Category string `json:"category,omitempty" yaml:"category,omitempty"`
	Class    string `json:"class,omitempty" yaml:"class,omitempty"`
	Activity string `json:"activity,omitempty" yaml:"activity,omitempty"`
}

type ParserProfile struct {
	ParserID    string            `json:"parser_id" yaml:"parser_id"`
	DisplayName string            `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Version     string            `json:"version,omitempty" yaml:"version,omitempty"`
	Entrypoint  string            `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Stages      []ParserStage     `json:"stages" yaml:"stages"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type ParserStage struct {
	StageID string         `json:"stage_id,omitempty" yaml:"stage_id,omitempty"`
	Type    string         `json:"type" yaml:"type"`
	OnError string         `json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

type Detection struct {
	DetectionID string             `json:"detection_id" yaml:"detection_id"`
	Title       string             `json:"title,omitempty" yaml:"title,omitempty"`
	Kind        string             `json:"kind" yaml:"kind"`
	Path        string             `json:"path,omitempty" yaml:"path,omitempty"`
	Severity    string             `json:"severity,omitempty" yaml:"severity,omitempty"`
	RiskScore   int                `json:"risk_score,omitempty" yaml:"risk_score,omitempty"`
	Tags        []string           `json:"tags,omitempty" yaml:"tags,omitempty"`
	Temporal    *DetectionTemporal `json:"temporal,omitempty" yaml:"temporal,omitempty"`
}

type DetectionTemporal struct {
	Kind               string                  `json:"kind" yaml:"kind"`
	WindowSeconds      int                     `json:"window_seconds,omitempty" yaml:"window_seconds,omitempty"`
	Threshold          int                     `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	GroupBy            []string                `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	SuppressForSeconds int                     `json:"suppress_for_seconds,omitempty" yaml:"suppress_for_seconds,omitempty"`
	Sequence           []DetectionTemporalStep `json:"sequence,omitempty" yaml:"sequence,omitempty"`
	Join               []DetectionTemporalStep `json:"join,omitempty" yaml:"join,omitempty"`
}

type DetectionTemporalStep struct {
	Field         string `json:"field" yaml:"field"`
	Op            string `json:"op,omitempty" yaml:"op,omitempty"`
	Values        []any  `json:"values,omitempty" yaml:"values,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty" yaml:"case_sensitive,omitempty"`
}

type SampleCase struct {
	CaseID      string `json:"case_id" yaml:"case_id"`
	SourceID    string `json:"source_id" yaml:"source_id"`
	ParserID    string `json:"parser_id" yaml:"parser_id"`
	InputPath   string `json:"input_path" yaml:"input_path"`
	GoldenPath  string `json:"golden_path" yaml:"golden_path"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type ParserStageRuntime interface {
	Type() string
	Compile(ParserStage) (CompiledParserStage, error)
}

type CompiledParserStage interface {
	Type() string
	Apply(*ParserEvent) error
}
