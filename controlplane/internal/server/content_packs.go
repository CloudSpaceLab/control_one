package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

const controlOneContentPackRuntimeVersion = "1.0.0"

type contentPackRegistrySnapshotStore interface {
	ActiveContentPackRegistrySnapshot(context.Context, uuid.UUID) (*storage.ContentPackRegistrySnapshotRecord, error)
	SaveContentPackRegistrySnapshot(context.Context, storage.SaveContentPackRegistrySnapshotParams) (*storage.ContentPackRegistrySnapshotRecord, error)
}

type contentPackRegistrySnapshotReader interface {
	ActiveContentPackRegistrySnapshot(context.Context, uuid.UUID) (*storage.ContentPackRegistrySnapshotRecord, error)
}

type contentPackCollectorConfigCandidateStore interface {
	contentPackRegistrySnapshotReader
	CreateContentPackCollectorConfigCandidate(context.Context, storage.CreateContentPackCollectorConfigCandidateParams) (*storage.ContentPackCollectorConfigCandidate, error)
	ApproveContentPackCollectorConfigCandidate(context.Context, storage.ApproveContentPackCollectorConfigCandidateParams) (*storage.ContentPackCollectorConfigCandidate, error)
	QueueContentPackCollectorConfigCandidate(context.Context, storage.QueueContentPackCollectorConfigCandidateParams) (*storage.ContentPackCollectorConfigCandidate, error)
	QueueContentPackCollectorConfigRollback(context.Context, storage.QueueContentPackCollectorConfigRollbackParams) (*storage.ContentPackCollectorConfigCandidate, error)
	GetContentPackCollectorConfigCandidate(context.Context, uuid.UUID) (*storage.ContentPackCollectorConfigCandidate, error)
	QueuedContentPackCollectorConfigForCollector(context.Context, uuid.UUID, string) (*storage.ContentPackCollectorConfigCandidate, error)
	RecordContentPackCollectorConfigApplyResult(context.Context, storage.RecordContentPackCollectorConfigApplyResultParams) (*storage.ContentPackCollectorConfigCandidate, error)
	ListContentPackCollectorConfigCandidates(context.Context, uuid.UUID, int, int) ([]storage.ContentPackCollectorConfigCandidate, int, error)
}

type contentPackEdgeCollectorStore interface {
	UpsertContentPackEdgeCollectorRegistration(context.Context, storage.UpsertContentPackEdgeCollectorRegistrationParams) (*storage.ContentPackEdgeCollector, error)
	RecordContentPackEdgeCollectorHeartbeat(context.Context, storage.RecordContentPackEdgeCollectorHeartbeatParams) (*storage.ContentPackEdgeCollector, error)
	RotateContentPackEdgeCollectorToken(context.Context, storage.RotateContentPackEdgeCollectorTokenParams) (*storage.ContentPackEdgeCollectorToken, error)
	ValidateContentPackEdgeCollectorToken(context.Context, uuid.UUID, string, string) (*storage.ContentPackEdgeCollector, error)
	ListContentPackEdgeCollectors(context.Context, uuid.UUID, int, int) ([]storage.ContentPackEdgeCollector, int, error)
}

type contentPackSourceRuntimeStateStore interface {
	UpsertContentPackSourceRuntimeState(context.Context, storage.UpsertContentPackSourceRuntimeStateParams) (*storage.ContentPackSourceRuntimeStateRecord, error)
	ListContentPackSourceRuntimeStates(context.Context, uuid.UUID, int, int) ([]storage.ContentPackSourceRuntimeStateRecord, int, error)
}

type contentPackSourceRuntimeStateSearchStore interface {
	ListContentPackSourceRuntimeStatesFiltered(context.Context, uuid.UUID, storage.ContentPackSourceRuntimeStateFilter, int, int) ([]storage.ContentPackSourceRuntimeStateRecord, int, error)
}

type contentPackSourceRuntimeStateSummaryStore interface {
	ContentPackSourceRuntimeStateSummaryFiltered(context.Context, uuid.UUID, storage.ContentPackSourceRuntimeStateFilter) (storage.ContentPackSourceRuntimeStateSummary, error)
}

type contentPackSourceRuntimeStateLookupStore interface {
	GetContentPackSourceRuntimeState(context.Context, uuid.UUID) (*storage.ContentPackSourceRuntimeStateRecord, error)
}

type contentPackSourceProposalStore interface {
	UpsertContentPackSourceProposals(context.Context, storage.UpsertContentPackSourceProposalsParams) ([]storage.ContentPackSourceProposalRecord, error)
	ListContentPackSourceProposals(context.Context, uuid.UUID, int, int) ([]storage.ContentPackSourceProposalRecord, int, error)
	ApproveContentPackSourceProposal(context.Context, storage.ApproveContentPackSourceProposalParams) (*storage.ContentPackSourceProposalRecord, error)
	RejectContentPackSourceProposal(context.Context, storage.RejectContentPackSourceProposalParams) (*storage.ContentPackSourceProposalRecord, error)
}

type contentPackSourceProposalFilteredStore interface {
	ListContentPackSourceProposalsFiltered(context.Context, uuid.UUID, storage.ContentPackSourceProposalFilter, int, int) ([]storage.ContentPackSourceProposalRecord, int, error)
}

type contentPackSourceProposalSummaryStore interface {
	ContentPackSourceProposalSummaryFiltered(context.Context, uuid.UUID, storage.ContentPackSourceProposalFilter) (storage.ContentPackSourceProposalSummary, error)
}

type contentPackSourceProposalLookupStore interface {
	ListContentPackSourceProposalsByIDs(context.Context, uuid.UUID, []uuid.UUID) ([]storage.ContentPackSourceProposalRecord, error)
}

type contentPackListResponse struct {
	TenantID          string                    `json:"tenant_id"`
	GeneratedAt       string                    `json:"generated_at"`
	Source            string                    `json:"source"`
	SnapshotID        string                    `json:"snapshot_id,omitempty"`
	SnapshotCreatedAt string                    `json:"snapshot_created_at,omitempty"`
	ControlOneVersion string                    `json:"control_one_version,omitempty"`
	Items             []contentPackRecordDTO    `json:"items"`
	Sources           []contentPackSourceDTO    `json:"sources,omitempty"`
	Totals            contentPackRegistryTotals `json:"totals"`
}

type contentPackRegistryTotals struct {
	Packs       int            `json:"packs"`
	Sources     int            `json:"sources"`
	Parsers     int            `json:"parsers"`
	Detections  int            `json:"detections"`
	Samples     int            `json:"samples"`
	ByStatus    map[string]int `json:"by_status"`
	ByRiskClass map[string]int `json:"by_risk_class,omitempty"`
}

type contentPackRecordDTO struct {
	PackID             string            `json:"pack_id"`
	PackVersion        string            `json:"pack_version"`
	DisplayName        string            `json:"display_name,omitempty"`
	Status             string            `json:"status"`
	Compatible         bool              `json:"compatible"`
	CompatibilityError string            `json:"compatibility_error,omitempty"`
	InstalledAt        string            `json:"installed_at,omitempty"`
	EnabledAt          string            `json:"enabled_at,omitempty"`
	DisabledAt         string            `json:"disabled_at,omitempty"`
	QuarantinedAt      string            `json:"quarantined_at,omitempty"`
	QuarantineReason   string            `json:"quarantine_reason,omitempty"`
	SourceCount        int               `json:"source_count"`
	ParserCount        int               `json:"parser_count"`
	DetectionCount     int               `json:"detection_count"`
	SampleCount        int               `json:"sample_count"`
	Labels             map[string]string `json:"labels,omitempty"`
}

type contentPackLifecycleRequest struct {
	PackID             string `json:"pack_id"`
	PackVersion        string `json:"pack_version"`
	Action             string `json:"action"`
	ExpectedSnapshotID string `json:"expected_snapshot_id"`
	Note               string `json:"note,omitempty"`
}

type contentPackLifecycleResponse struct {
	TenantID          string                              `json:"tenant_id"`
	GeneratedAt       string                              `json:"generated_at"`
	Action            string                              `json:"action"`
	SnapshotID        string                              `json:"snapshot_id"`
	SnapshotCreatedAt string                              `json:"snapshot_created_at"`
	Pack              contentPackRecordDTO                `json:"pack"`
	DetectionReplay   *contentpacks.DetectionReplayReport `json:"detection_replay,omitempty"`
}

type contentPackSourceDTO struct {
	PackID            string   `json:"pack_id"`
	PackVersion       string   `json:"pack_version"`
	ContentStatus     string   `json:"content_status"`
	SourceID          string   `json:"source_id"`
	DisplayName       string   `json:"display_name,omitempty"`
	Vendor            string   `json:"vendor,omitempty"`
	Product           string   `json:"product,omitempty"`
	SourceClass       string   `json:"source_class"`
	RiskClass         string   `json:"risk_class"`
	DataSensitivity   string   `json:"data_sensitivity"`
	CollectorModes    []string `json:"collector_modes,omitempty"`
	ApprovalRequired  bool     `json:"approval_required"`
	ParserIDs         []string `json:"parser_ids,omitempty"`
	DetectionIDs      []string `json:"detection_ids,omitempty"`
	SampleIDs         []string `json:"sample_ids,omitempty"`
	OCSFCategory      string   `json:"ocsf_category,omitempty"`
	OCSFClass         string   `json:"ocsf_class,omitempty"`
	RawRetention      string   `json:"raw_retention_default,omitempty"`
	ExpectedEPS       int      `json:"expected_events_per_second,omitempty"`
	ExpectedBytesPS   int64    `json:"expected_bytes_per_second,omitempty"`
	OperationalStatus string   `json:"operational_status"`
}

type contentPackSourceHealthResponse struct {
	TenantID    string                        `json:"tenant_id"`
	GeneratedAt string                        `json:"generated_at"`
	Items       []contentPackSourceHealthDTO  `json:"items"`
	Totals      contentPackSourceHealthTotals `json:"totals"`
	Pagination  *paginationMeta               `json:"pagination,omitempty"`
}

type contentPackSourceHealthTotals struct {
	Sources             int                               `json:"sources"`
	CollectorsReporting int                               `json:"collectors_reporting"`
	ByState             map[string]int                    `json:"by_state"`
	Metrics             contentpacks.SourceRuntimeMetrics `json:"metrics,omitempty"`
}

type contentPackSourceHealthDTO struct {
	RuntimeStateID     string                             `json:"runtime_state_id,omitempty"`
	SourceInstanceID   string                             `json:"source_instance_id,omitempty"`
	CollectorID        string                             `json:"collector_id"`
	SourceID           string                             `json:"source_id"`
	ReceiverID         string                             `json:"receiver_id,omitempty"`
	NodeID             string                             `json:"node_id,omitempty"`
	PackID             string                             `json:"pack_id,omitempty"`
	PackVersion        string                             `json:"pack_version,omitempty"`
	ParserID           string                             `json:"parser_id,omitempty"`
	CollectorMode      string                             `json:"collector_mode,omitempty"`
	ConfigVersion      string                             `json:"config_version,omitempty"`
	ContentVersion     string                             `json:"content_version,omitempty"`
	DisplayName        string                             `json:"display_name,omitempty"`
	CoverageState      string                             `json:"coverage_state"`
	ApprovalRequired   bool                               `json:"approval_required,omitempty"`
	ApprovalID         string                             `json:"approval_id,omitempty"`
	Metrics            contentpacks.SourceRuntimeMetrics  `json:"metrics,omitempty"`
	Labels             map[string]string                  `json:"labels,omitempty"`
	LastEventAt        string                             `json:"last_event_at,omitempty"`
	LastParsedAt       string                             `json:"last_parsed_at,omitempty"`
	LastHealthAt       string                             `json:"last_health_at,omitempty"`
	LastError          string                             `json:"last_error,omitempty"`
	RecommendedActions []contentPackSourceHealthActionDTO `json:"recommended_actions,omitempty"`
}

type contentPackSourceHealthActionDTO struct {
	ID          string `json:"id"`
	Action      string `json:"action"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type contentPackSourceHealthInvestigationRequest struct {
	RuntimeStateID string `json:"runtime_state_id"`
	Note           string `json:"note,omitempty"`
}

type contentPackSourceHealthInvestigationResponse struct {
	CaseID string          `json:"case_id"`
	Case   socCaseResponse `json:"case"`
}

type contentPackSourceProposalDTO struct {
	ID                  string            `json:"id"`
	TenantID            string            `json:"tenant_id"`
	NodeID              string            `json:"node_id"`
	ProposalID          string            `json:"proposal_id"`
	Kind                string            `json:"kind"`
	Program             string            `json:"program"`
	SourceID            string            `json:"source_id,omitempty"`
	CollectorType       string            `json:"collector_type,omitempty"`
	Formatter           string            `json:"formatter,omitempty"`
	Status              string            `json:"status"`
	Confidence          int               `json:"confidence"`
	Risk                string            `json:"risk,omitempty"`
	AutoConnectEligible bool              `json:"auto_connect_eligible"`
	RequiresApproval    bool              `json:"requires_approval"`
	Reason              string            `json:"reason,omitempty"`
	Paths               []string          `json:"paths,omitempty"`
	Evidence            []string          `json:"evidence,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	FirstSeenAt         string            `json:"first_seen_at"`
	LastSeenAt          string            `json:"last_seen_at"`
	ApprovedBySubject   string            `json:"approved_by_subject,omitempty"`
	ApprovedAt          string            `json:"approved_at,omitempty"`
	ApprovalNote        string            `json:"approval_note,omitempty"`
	CollectMode         string            `json:"collect_mode,omitempty"`
	RejectedBySubject   string            `json:"rejected_by_subject,omitempty"`
	RejectedAt          string            `json:"rejected_at,omitempty"`
	RejectionReason     string            `json:"rejection_reason,omitempty"`
	CreatedAt           string            `json:"created_at"`
	UpdatedAt           string            `json:"updated_at"`
}

type contentPackSourceProposalSummaryDTO struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
}

type contentPackSourceProposalListResponse struct {
	Data       []contentPackSourceProposalDTO       `json:"data"`
	Pagination paginationMeta                       `json:"pagination"`
	Summary    *contentPackSourceProposalSummaryDTO `json:"summary,omitempty"`
}

type contentPackSourceProposalApprovalRequest struct {
	Note        string `json:"note,omitempty"`
	CollectMode string `json:"collect_mode,omitempty"`
}

type contentPackSourceProposalRejectRequest struct {
	Reason         string `json:"reason,omitempty"`
	PrivacyBlocked bool   `json:"privacy_blocked,omitempty"`
}

type contentPackOTelConfigRenderRequest struct {
	Endpoint                    string                                 `json:"endpoint"`
	CollectorID                 string                                 `json:"collector_id,omitempty"`
	SourceIDs                   []string                               `json:"source_ids,omitempty"`
	SourceProposalIDs           []string                               `json:"source_proposal_ids,omitempty"`
	Sources                     []contentPackOTelConfigSourceSelection `json:"sources,omitempty"`
	Headers                     map[string]string                      `json:"headers,omitempty"`
	InsecureTLS                 bool                                   `json:"insecure_tls,omitempty"`
	Compression                 string                                 `json:"compression,omitempty"`
	Timeout                     string                                 `json:"timeout,omitempty"`
	MemoryLimitMiB              int                                    `json:"memory_limit_mib,omitempty"`
	MemorySpikeLimitMiB         int                                    `json:"memory_spike_limit_mib,omitempty"`
	BatchTimeout                string                                 `json:"batch_timeout,omitempty"`
	BatchSendBatchSize          int                                    `json:"batch_send_batch_size,omitempty"`
	DisablePersistentStorage    bool                                   `json:"disable_persistent_storage,omitempty"`
	StorageExtensionID          string                                 `json:"storage_extension_id,omitempty"`
	StorageDirectory            string                                 `json:"storage_directory,omitempty"`
	ExporterQueueSize           int                                    `json:"exporter_queue_size,omitempty"`
	ExporterQueueConsumers      int                                    `json:"exporter_queue_consumers,omitempty"`
	ExporterRetryMaxElapsedTime string                                 `json:"exporter_retry_max_elapsed_time,omitempty"`
	SourceApprovalRefs          map[string]string                      `json:"source_approval_refs,omitempty"`
	AdditionalAttributes        map[string]string                      `json:"additional_attributes,omitempty"`
}

type contentPackOTelConfigSourceSelection struct {
	SourceID    string `json:"source_id"`
	Mode        string `json:"mode,omitempty"`
	CollectMode string `json:"collect_mode,omitempty"`
	ApprovalRef string `json:"approval_ref,omitempty"`
}

type contentPackOTelConfigRenderResponse struct {
	TenantID          string                                 `json:"tenant_id"`
	GeneratedAt       string                                 `json:"generated_at"`
	SnapshotID        string                                 `json:"snapshot_id"`
	SnapshotCreatedAt string                                 `json:"snapshot_created_at"`
	ConfigVersion     string                                 `json:"config_version"`
	CandidateID       string                                 `json:"candidate_id,omitempty"`
	CandidateStatus   string                                 `json:"candidate_status,omitempty"`
	Sources           []contentpacks.OTelCollectorSourcePlan `json:"sources"`
	Warnings          []string                               `json:"warnings,omitempty"`
	Config            contentpacks.OTelCollectorConfig       `json:"config"`
	YAML              string                                 `json:"yaml"`
}

type contentPackOTelConfigCandidateDTO struct {
	ID                    string   `json:"id"`
	TenantID              string   `json:"tenant_id"`
	RegistrySnapshotID    string   `json:"registry_snapshot_id,omitempty"`
	Status                string   `json:"status"`
	ConfigVersion         string   `json:"config_version"`
	CollectorID           string   `json:"collector_id,omitempty"`
	Endpoint              string   `json:"endpoint,omitempty"`
	SourceIDs             []string `json:"source_ids,omitempty"`
	CreatedBySubject      string   `json:"created_by_subject,omitempty"`
	ApprovedBySubject     string   `json:"approved_by_subject,omitempty"`
	ApprovalNote          string   `json:"approval_note,omitempty"`
	ReviewedConfigVersion string   `json:"reviewed_config_version,omitempty"`
	ReviewedYAMLSHA256    string   `json:"reviewed_yaml_sha256,omitempty"`
	ApprovedAt            string   `json:"approved_at,omitempty"`
	QueuedBySubject       string   `json:"queued_by_subject,omitempty"`
	QueueNote             string   `json:"queue_note,omitempty"`
	TargetCollectorID     string   `json:"target_collector_id,omitempty"`
	QueuedAt              string   `json:"queued_at,omitempty"`
	DeployedAt            string   `json:"deployed_at,omitempty"`
	FailedAt              string   `json:"failed_at,omitempty"`
	DeploymentError       string   `json:"deployment_error,omitempty"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
}

type contentPackOTelConfigCandidateDetailDTO struct {
	contentPackOTelConfigCandidateDTO
	Sources  []contentpacks.OTelCollectorSourcePlan `json:"sources"`
	Warnings []string                               `json:"warnings,omitempty"`
	YAML     string                                 `json:"yaml"`
}

type contentPackOTelConfigCandidateApprovalRequest struct {
	Note                  string `json:"note,omitempty"`
	ReviewedConfigVersion string `json:"reviewed_config_version,omitempty"`
}

type contentPackOTelConfigCandidateQueueRequest struct {
	CollectorID           string `json:"collector_id,omitempty"`
	Note                  string `json:"note,omitempty"`
	ExpectedConfigVersion string `json:"expected_config_version,omitempty"`
}

type contentPackEdgeCollectorRegistrationRequest struct {
	CollectorID          string `json:"collector_id"`
	Kind                 string `json:"kind,omitempty"`
	DisplayName          string `json:"display_name,omitempty"`
	Endpoint             string `json:"endpoint,omitempty"`
	Version              string `json:"version,omitempty"`
	DesiredConfigVersion string `json:"desired_config_version,omitempty"`
}

type contentPackEdgeCollectorHeartbeatRequest struct {
	Kind                 string         `json:"kind,omitempty"`
	Version              string         `json:"version,omitempty"`
	Status               string         `json:"status,omitempty"`
	DesiredConfigVersion string         `json:"desired_config_version,omitempty"`
	RunningConfigVersion string         `json:"running_config_version,omitempty"`
	Health               map[string]any `json:"health,omitempty"`
	LastError            string         `json:"last_error,omitempty"`
}

type contentPackEdgeCollectorApplyResultRequest struct {
	ConfigVersion string `json:"config_version"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}

type contentPackEdgeCollectorRollbackRequest struct {
	CandidateID   string `json:"candidate_id,omitempty"`
	ConfigVersion string `json:"config_version,omitempty"`
	Note          string `json:"note,omitempty"`
}

type contentPackEdgeCollectorDesiredConfigResponse struct {
	TenantID      string                                 `json:"tenant_id"`
	CollectorID   string                                 `json:"collector_id"`
	CandidateID   string                                 `json:"candidate_id"`
	ConfigVersion string                                 `json:"config_version"`
	GeneratedAt   string                                 `json:"generated_at"`
	QueuedAt      string                                 `json:"queued_at,omitempty"`
	Sources       []contentpacks.OTelCollectorSourcePlan `json:"sources"`
	Warnings      []string                               `json:"warnings,omitempty"`
	YAML          string                                 `json:"yaml"`
}

type contentPackEdgeCollectorTokenResponse struct {
	Collector contentPackEdgeCollectorDTO `json:"collector"`
	Token     string                      `json:"token"`
}

type contentPackEdgeCollectorDTO struct {
	ID                   string         `json:"id"`
	TenantID             string         `json:"tenant_id"`
	CollectorID          string         `json:"collector_id"`
	Kind                 string         `json:"kind"`
	DisplayName          string         `json:"display_name,omitempty"`
	Endpoint             string         `json:"endpoint,omitempty"`
	Version              string         `json:"version,omitempty"`
	Status               string         `json:"status"`
	DesiredConfigVersion string         `json:"desired_config_version,omitempty"`
	RunningConfigVersion string         `json:"running_config_version,omitempty"`
	TokenLastFour        string         `json:"token_last_four,omitempty"`
	TokenIssuedAt        string         `json:"token_issued_at,omitempty"`
	Health               map[string]any `json:"health,omitempty"`
	LastError            string         `json:"last_error,omitempty"`
	LastHeartbeatAt      string         `json:"last_heartbeat_at,omitempty"`
	CreatedAt            string         `json:"created_at"`
	UpdatedAt            string         `json:"updated_at"`
}

func (s *Server) handleContentPacksCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	resp, ok := s.activeContentPackRegistryResponse(w, r, tenantID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContentPackSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasPrefix(path, "/api/v1/content-packs/source-proposals/") {
		s.handleContentPackSourceProposalSubroute(w, r, path)
		return
	}
	if strings.HasPrefix(path, "/api/v1/content-packs/source-health/") {
		s.handleContentPackSourceHealthSubroute(w, r, path)
		return
	}
	if strings.HasPrefix(path, "/api/v1/content-packs/otel-config/candidates/") {
		s.handleContentPackOTelConfigCandidateSubroute(w, r, path)
		return
	}
	if strings.HasPrefix(path, "/api/v1/content-packs/collectors/") {
		s.handleContentPackEdgeCollectorSubroute(w, r, path)
		return
	}
	switch path {
	case "/api/v1/content-packs/sources":
		s.handleContentPackSources(w, r)
	case "/api/v1/content-packs/detections":
		s.handleContentPackDetections(w, r)
	case "/api/v1/content-packs/detections/replay":
		s.handleContentPackDetectionReplay(w, r)
	case "/api/v1/content-packs/detections/overrides":
		s.handleContentPackDetectionOverrides(w, r)
	case "/api/v1/content-packs/lifecycle":
		s.handleContentPackLifecycle(w, r)
	case "/api/v1/content-packs/source-health":
		s.handleContentPackSourceHealth(w, r)
	case "/api/v1/content-packs/source-proposals":
		s.handleContentPackSourceProposals(w, r)
	case "/api/v1/content-packs/otel-config":
		s.handleContentPackOTelConfig(w, r)
	case "/api/v1/content-packs/otel-config/candidates":
		s.handleContentPackOTelConfigCandidates(w, r)
	case "/api/v1/content-packs/collectors":
		s.handleContentPackEdgeCollectors(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleContentPackSourceProposalSubroute(w http.ResponseWriter, r *http.Request, path string) {
	rest := strings.TrimPrefix(path, "/api/v1/content-packs/source-proposals/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "approve" {
		s.handleApproveContentPackSourceProposal(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "reject" {
		s.handleRejectContentPackSourceProposal(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleContentPackSourceHealthSubroute(w http.ResponseWriter, r *http.Request, path string) {
	rest := strings.TrimPrefix(path, "/api/v1/content-packs/source-health/")
	parts := strings.Split(rest, "/")
	if len(parts) == 1 && parts[0] == "investigate" {
		s.handleCreateContentPackSourceHealthInvestigation(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleContentPackOTelConfigCandidateSubroute(w http.ResponseWriter, r *http.Request, path string) {
	rest := strings.TrimPrefix(path, "/api/v1/content-packs/otel-config/candidates/")
	parts := strings.Split(rest, "/")
	if len(parts) == 1 && parts[0] != "" {
		s.handleGetContentPackOTelConfigCandidate(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "approve" {
		s.handleApproveContentPackOTelConfigCandidate(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "queue" {
		s.handleQueueContentPackOTelConfigCandidate(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleContentPackEdgeCollectorSubroute(w http.ResponseWriter, r *http.Request, path string) {
	rest := strings.TrimPrefix(path, "/api/v1/content-packs/collectors/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "heartbeat" {
		s.handleContentPackEdgeCollectorHeartbeat(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "desired-config" {
		s.handleContentPackEdgeCollectorDesiredConfig(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "apply-result" {
		s.handleContentPackEdgeCollectorApplyResult(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "token" {
		s.handleRotateContentPackEdgeCollectorToken(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "rollback" {
		s.handleContentPackEdgeCollectorRollback(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleContentPackSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	resp, ok := s.activeContentPackRegistryResponse(w, r, tenantID)
	if !ok {
		return
	}
	resp.Items = []contentPackRecordDTO{}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContentPackLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	var req contentPackLifecycleRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	packID := strings.TrimSpace(req.PackID)
	packVersion := strings.TrimSpace(req.PackVersion)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if packID == "" || packVersion == "" {
		http.Error(w, "pack_id and pack_version are required", http.StatusBadRequest)
		return
	}
	if action != "enable" && action != "disable" {
		http.Error(w, "action must be enable or disable", http.StatusBadRequest)
		return
	}
	expectedSnapshotID, err := uuid.Parse(strings.TrimSpace(req.ExpectedSnapshotID))
	if err != nil || expectedSnapshotID == uuid.Nil {
		http.Error(w, "expected_snapshot_id is required", http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackRegistrySnapshotStore)
	if !ok || store == nil {
		http.Error(w, "content pack registry snapshot store unavailable", http.StatusServiceUnavailable)
		return
	}
	active, err := store.ActiveContentPackRegistrySnapshot(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("load active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if active == nil {
		http.Error(w, "no active content pack registry snapshot", http.StatusNotFound)
		return
	}
	if active.ID != expectedSnapshotID {
		http.Error(w, "active content pack registry snapshot changed", http.StatusConflict)
		return
	}
	registry, err := contentpacks.NewRegistryFromSnapshot(active.Snapshot, controlOneContentPackRuntimeVersion)
	if err != nil {
		s.logger.Warn("restore active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	var detectionReplay *contentpacks.DetectionReplayReport
	if action == "enable" {
		report, err := s.replayActiveContentPackDetectionsForLifecycle(r.Context(), packID, packVersion)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if !report.Passed() {
			http.Error(w, fmt.Sprintf("detection replay failed with %d failures", len(report.Failures)), http.StatusConflict)
			return
		}
		detectionReplay = &report
	}
	now := time.Now().UTC()
	var updated *contentpacks.PackRecord
	switch action {
	case "enable":
		updated, err = registry.Enable(packID, packVersion, now)
	case "disable":
		updated, err = registry.Disable(packID, packVersion, now)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	snapshot := registry.Snapshot(now)
	saved, err := store.SaveContentPackRegistrySnapshot(r.Context(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   fmt.Sprintf("operator:%s:%s@%s", action, packID, packVersion),
		Snapshot: snapshot,
	})
	if err != nil {
		s.logger.Warn("save content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.persistActiveContentPackDetectionArtifactsForSnapshot(r.Context(), tenantID, saved.ID, saved.Snapshot)
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.pack."+action, "content_pack", packID+"@"+packVersion, map[string]any{
		"pack_id":               packID,
		"pack_version":          packVersion,
		"previous_snapshot_id":  active.ID.String(),
		"new_snapshot_id":       saved.ID.String(),
		"expected_snapshot_id":  expectedSnapshotID.String(),
		"note":                  strings.TrimSpace(req.Note),
		"detection_replay_pass": detectionReplay != nil && detectionReplay.Passed(),
	})
	writeJSON(w, http.StatusOK, contentPackLifecycleResponse{
		TenantID:          tenantID.String(),
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		Action:            action,
		SnapshotID:        saved.ID.String(),
		SnapshotCreatedAt: saved.CreatedAt.UTC().Format(time.RFC3339),
		Pack:              newContentPackRecordDTO(*updated),
		DetectionReplay:   detectionReplay,
	})
}

func (s *Server) handleContentPackSourceHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	generatedAt := time.Now().UTC()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	stateFilter, err := parseContentPackSourceHealthStateFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hasFilters := query != "" || len(stateFilter) > 0
	if sourceStore, ok := s.store.(contentPackSourceRuntimeStateStore); ok && sourceStore != nil {
		var rows []storage.ContentPackSourceRuntimeStateRecord
		var total int
		var err error
		sourceFilter := storage.ContentPackSourceRuntimeStateFilter{}
		if searchStore, ok := s.store.(contentPackSourceRuntimeStateSearchStore); ok && searchStore != nil {
			staleBefore := generatedAt.Add(-coverageHeartbeatFreshnessWindow)
			sourceFilter = storage.ContentPackSourceRuntimeStateFilter{
				Query:          query,
				CoverageStates: stateFilter,
				StaleBefore:    &staleBefore,
			}
			rows, total, err = searchStore.ListContentPackSourceRuntimeStatesFiltered(r.Context(), tenantID, sourceFilter, limit, offset)
		} else {
			rows, total, err = sourceStore.ListContentPackSourceRuntimeStates(r.Context(), tenantID, limit, offset)
		}
		if err != nil {
			s.logger.Warn("list content pack source runtime states", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if len(rows) > 0 || total > 0 || hasFilters {
			var summary *storage.ContentPackSourceRuntimeStateSummary
			if summaryStore, ok := s.store.(contentPackSourceRuntimeStateSummaryStore); ok && summaryStore != nil {
				sourceSummary, err := summaryStore.ContentPackSourceRuntimeStateSummaryFiltered(r.Context(), tenantID, sourceFilter)
				if err != nil {
					s.logger.Warn("summarize content pack source runtime states", zap.Error(err))
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				summary = &sourceSummary
			}
			writeJSON(w, http.StatusOK, contentPackSourceHealthResponseFromRuntimeStates(tenantID, generatedAt, rows, total, limit, offset, summary))
			return
		}
	}
	store, ok := s.store.(coverageEdgeCollectorLister)
	if !ok || store == nil {
		http.Error(w, "content pack edge collector store unavailable", http.StatusServiceUnavailable)
		return
	}
	collectors, _, err := store.ListContentPackEdgeCollectors(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Warn("list content pack edge collectors for source health", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	evidence := sourceHealthEvidenceFromCollectors(collectors)
	if hasFilters {
		evidence = filterSourceHealthEvidence(evidence, query, stateFilter, generatedAt)
	}
	writeJSON(w, http.StatusOK, contentPackSourceHealthResponseFromEvidence(tenantID, generatedAt, evidence, nil))
}

func (s *Server) handleCreateContentPackSourceHealthInvestigation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	var req contentPackSourceHealthInvestigationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	runtimeStateID, err := uuid.Parse(strings.TrimSpace(req.RuntimeStateID))
	if err != nil {
		http.Error(w, "runtime_state_id must be a UUID", http.StatusBadRequest)
		return
	}
	lookup, ok := s.store.(contentPackSourceRuntimeStateLookupStore)
	if !ok || lookup == nil {
		http.Error(w, "content pack source runtime state store unavailable", http.StatusServiceUnavailable)
		return
	}
	row, err := lookup.GetContentPackSourceRuntimeState(r.Context(), runtimeStateID)
	if err != nil {
		s.logger.Warn("get content pack source runtime state", zap.Error(err), zap.String("runtime_state_id", runtimeStateID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if row == nil || row.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	effective := sourceHealthRuntimeStateApplyFreshness(row.State, time.Now().UTC())
	if !contentPackSourceHealthInvestigationAllowed(effective.CoverageState) {
		http.Error(w, "source health state does not require an investigation", http.StatusConflict)
		return
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "case store unavailable", http.StatusServiceUnavailable)
		return
	}
	evidence, err := json.Marshal(contentPackSourceHealthInvestigationEvidence(*row, effective, req.Note))
	if err != nil {
		s.logger.Warn("marshal content pack source health investigation evidence", zap.Error(err), zap.String("runtime_state_id", runtimeStateID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	caseRow, err := backend.CreateAIInvestigation(r.Context(), storage.CreateAIInvestigationParams{
		TenantID:         tenantID,
		NodeID:           contentPackSourceHealthInvestigationNodeID(effective),
		TriggerType:      "siem_source_health",
		TriggerEventType: "content_pack.source_health." + string(effective.CoverageState),
		TriggerDedupKey:  contentPackSourceHealthInvestigationDedupKey(tenantID, effective),
		Severity:         contentPackSourceHealthInvestigationSeverity(effective.CoverageState),
		Summary:          contentPackSourceHealthInvestigationSummary(effective),
		Evidence:         evidence,
		Status:           storage.AIInvestigationStatusOpen,
	})
	if err != nil {
		s.logger.Warn("create content pack source health investigation", zap.Error(err), zap.String("runtime_state_id", runtimeStateID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.source_health.investigation_opened", "ai_investigation", caseRow.ID.String(), map[string]any{
		"runtime_state_id":   runtimeStateID.String(),
		"source_instance_id": effective.SourceInstanceID,
		"source_id":          effective.SourceID,
		"collector_id":       effective.CollectorID,
		"coverage_state":     string(effective.CoverageState),
	})
	writeJSON(w, http.StatusCreated, contentPackSourceHealthInvestigationResponse{
		CaseID: caseRow.ID.String(),
		Case:   newSOCCaseResponse(*caseRow),
	})
}

func contentPackSourceHealthRecommendedActions(item tenantSourceHealthEvidence) []contentPackSourceHealthActionDTO {
	if !contentPackSourceHealthInvestigationAllowed(item.State) {
		return nil
	}
	action := contentPackSourceHealthActionDTO{
		ID:      "open_source_health_investigation",
		Action:  "source_health.investigate",
		Enabled: strings.TrimSpace(item.RuntimeStateID) != "",
	}
	switch item.State {
	case contentpacks.CoverageState(contentpacks.CoverageCollectionConflict):
		action.Label = "Open collection conflict investigation"
		action.Description = "The same source appears active through multiple collection owners without approved migration dual-write."
	case contentpacks.CoverageState(contentpacks.CoverageParserFailed):
		action.Label = "Open parser investigation"
		action.Description = "Parser failures are present for this source; open a cited SOC case before changing parser content or collector config."
	case contentpacks.CoverageState(contentpacks.CoverageSilent):
		action.Label = "Open silent source investigation"
		action.Description = "The source is deployed but not sending events inside the freshness window."
	case contentpacks.CoverageState(contentpacks.CoverageBackpressured):
		action.Label = "Open backpressure investigation"
		action.Description = "Queue, retry, lag, or drop metrics show collection pressure for this source."
	case contentpacks.CoverageState(contentpacks.CoverageStale):
		action.Label = "Open stale source investigation"
		action.Description = "Runtime proof is stale; verify collector heartbeat, config version, and event flow."
	default:
		action.Label = "Open source health investigation"
		action.Description = "Open a cited SOC case from this source-health evidence."
	}
	return []contentPackSourceHealthActionDTO{action}
}

func contentPackSourceHealthInvestigationAllowed(state contentpacks.CoverageState) bool {
	switch state {
	case contentpacks.CoverageState(contentpacks.CoverageParserFailed),
		contentpacks.CoverageState(contentpacks.CoverageCollectionConflict),
		contentpacks.CoverageState(contentpacks.CoverageSilent),
		contentpacks.CoverageState(contentpacks.CoverageBackpressured),
		contentpacks.CoverageState(contentpacks.CoverageStale):
		return true
	default:
		return false
	}
}

func sourceHealthRuntimeStateApplyFreshness(state contentpacks.SourceRuntimeState, now time.Time) contentpacks.SourceRuntimeState {
	item := tenantSourceHealthEvidenceFromRuntimeState(state)
	item = sourceHealthEvidenceApplyFreshness(item, now)
	state.CoverageState = item.State
	return state
}

func contentPackSourceHealthInvestigationNodeID(state contentpacks.SourceRuntimeState) uuid.UUID {
	nodeID, err := uuid.Parse(strings.TrimSpace(state.NodeID))
	if err != nil {
		return uuid.Nil
	}
	return nodeID
}

func contentPackSourceHealthInvestigationDedupKey(tenantID uuid.UUID, state contentpacks.SourceRuntimeState) string {
	parts := []string{
		"c1:siem-source-health:v1",
		tenantID.String(),
		firstNonEmptyString(strings.TrimSpace(state.SourceInstanceID), strings.TrimSpace(state.SourceID), "unknown-source"),
		string(state.CoverageState),
	}
	return strings.Join(parts, ":")
}

func contentPackSourceHealthInvestigationSeverity(state contentpacks.CoverageState) string {
	switch state {
	case contentpacks.CoverageState(contentpacks.CoverageCollectionConflict):
		return "high"
	case contentpacks.CoverageState(contentpacks.CoverageParserFailed):
		return "high"
	case contentpacks.CoverageState(contentpacks.CoverageSilent),
		contentpacks.CoverageState(contentpacks.CoverageBackpressured):
		return "medium"
	case contentpacks.CoverageState(contentpacks.CoverageStale):
		return "warning"
	default:
		return "info"
	}
}

func contentPackSourceHealthInvestigationSummary(state contentpacks.SourceRuntimeState) string {
	source := firstNonEmptyString(strings.TrimSpace(state.DisplayName), strings.TrimSpace(state.SourceID), strings.TrimSpace(state.SourceInstanceID), "SIEM source")
	switch state.CoverageState {
	case contentpacks.CoverageState(contentpacks.CoverageCollectionConflict):
		return "Collection conflict investigation opened for " + source
	case contentpacks.CoverageState(contentpacks.CoverageParserFailed):
		return "Parser failure investigation opened for " + source
	case contentpacks.CoverageState(contentpacks.CoverageSilent):
		return "Silent source investigation opened for " + source
	case contentpacks.CoverageState(contentpacks.CoverageBackpressured):
		return "Collection backpressure investigation opened for " + source
	case contentpacks.CoverageState(contentpacks.CoverageStale):
		return "Stale source investigation opened for " + source
	default:
		return "Source health investigation opened for " + source
	}
}

func contentPackSourceHealthInvestigationEvidence(row storage.ContentPackSourceRuntimeStateRecord, state contentpacks.SourceRuntimeState, note string) map[string]any {
	return map[string]any{
		"source":             "content_pack_source_runtime_state",
		"runtime_state_id":   row.ID.String(),
		"source_instance_id": state.SourceInstanceID,
		"source_id":          state.SourceID,
		"display_name":       state.DisplayName,
		"node_id":            state.NodeID,
		"collector_id":       state.CollectorID,
		"collector_mode":     state.CollectorMode,
		"parser_id":          state.ParserID,
		"pack_id":            state.PackID,
		"pack_version":       state.PackVersion,
		"coverage_state":     string(state.CoverageState),
		"approval_required":  state.ApprovalRequired,
		"approval_id":        state.ApprovalID,
		"config_version":     state.ConfigVersion,
		"content_version":    state.ContentVersion,
		"last_error":         state.LastError,
		"last_event_at":      formatContentPackTimePtr(state.LastEventAt),
		"last_parsed_at":     formatContentPackTimePtr(state.LastParsedAt),
		"last_health_at":     formatContentPackTimePtr(state.LastHealthAt),
		"metrics":            state.Metrics,
		"labels":             cloneStringMapContentPack(state.Labels),
		"operator_note":      strings.TrimSpace(note),
		"citations": []map[string]any{{
			"system":           "control_one",
			"type":             "content_pack_source_runtime_state",
			"runtime_state_id": row.ID.String(),
			"updated_at":       formatContentPackTime(row.UpdatedAt),
		}},
	}
}

func (s *Server) handleContentPackSourceProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	store, ok := s.store.(contentPackSourceProposalStore)
	if !ok || store == nil {
		http.Error(w, "content pack source proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	statuses, err := parseContentPackSourceProposalStatusFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.ContentPackSourceProposalFilter{
		Query:    query,
		Statuses: statuses,
	}
	var rows []storage.ContentPackSourceProposalRecord
	var total int
	if filteredStore, ok := s.store.(contentPackSourceProposalFilteredStore); ok && filteredStore != nil {
		rows, total, err = filteredStore.ListContentPackSourceProposalsFiltered(r.Context(), tenantID, filter, limit, offset)
	} else {
		rows, total, err = store.ListContentPackSourceProposals(r.Context(), tenantID, limit, offset)
	}
	if err != nil {
		s.logger.Warn("list content pack source proposals", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]contentPackSourceProposalDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, newContentPackSourceProposalDTO(row))
	}
	var summary *contentPackSourceProposalSummaryDTO
	if summaryStore, ok := s.store.(contentPackSourceProposalSummaryStore); ok && summaryStore != nil {
		sourceSummary, err := summaryStore.ContentPackSourceProposalSummaryFiltered(r.Context(), tenantID, filter)
		if err != nil {
			s.logger.Warn("summarize content pack source proposals", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		summary = &contentPackSourceProposalSummaryDTO{
			Total:    sourceSummary.Total,
			ByStatus: sourceSummary.ByStatus,
		}
	}
	writeJSON(w, http.StatusOK, contentPackSourceProposalListResponse{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
		Summary:    summary,
	})
}

func (s *Server) handleApproveContentPackSourceProposal(w http.ResponseWriter, r *http.Request, proposalIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	proposalID, err := uuid.Parse(strings.TrimSpace(proposalIDRaw))
	if err != nil {
		http.Error(w, "invalid source proposal id", http.StatusBadRequest)
		return
	}
	var req contentPackSourceProposalApprovalRequest
	if r.ContentLength != 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	store, ok := s.store.(contentPackSourceProposalStore)
	if !ok || store == nil {
		http.Error(w, "content pack source proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	proposal, err := store.ApproveContentPackSourceProposal(r.Context(), storage.ApproveContentPackSourceProposalParams{
		TenantID:          tenantID,
		ProposalID:        proposalID,
		ApprovedBySubject: strings.TrimSpace(principal.Subject),
		ApprovalNote:      req.Note,
		CollectMode:       req.CollectMode,
	})
	if err != nil {
		s.logger.Warn("approve content pack source proposal", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.persistContentPackSourceRuntimeStatesFromProposals(r.Context(), []storage.ContentPackSourceProposalRecord{*proposal})
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.source_proposal.approved", "content_pack_source_proposal", proposal.ID.String(), map[string]any{
		"proposal_id":  proposal.ProposalID,
		"node_id":      proposal.NodeID.String(),
		"program":      proposal.Program,
		"source_id":    proposal.SourceID,
		"collect_mode": proposal.CollectMode,
		"note":         proposal.ApprovalNote,
	})
	writeJSON(w, http.StatusOK, newContentPackSourceProposalDTO(*proposal))
}

func (s *Server) handleRejectContentPackSourceProposal(w http.ResponseWriter, r *http.Request, proposalIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	proposalID, err := uuid.Parse(strings.TrimSpace(proposalIDRaw))
	if err != nil {
		http.Error(w, "invalid source proposal id", http.StatusBadRequest)
		return
	}
	var req contentPackSourceProposalRejectRequest
	if r.ContentLength != 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	store, ok := s.store.(contentPackSourceProposalStore)
	if !ok || store == nil {
		http.Error(w, "content pack source proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	proposal, err := store.RejectContentPackSourceProposal(r.Context(), storage.RejectContentPackSourceProposalParams{
		TenantID:          tenantID,
		ProposalID:        proposalID,
		RejectedBySubject: strings.TrimSpace(principal.Subject),
		RejectionReason:   req.Reason,
		PrivacyBlocked:    req.PrivacyBlocked,
	})
	if err != nil {
		s.logger.Warn("reject content pack source proposal", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.persistContentPackSourceRuntimeStatesFromProposals(r.Context(), []storage.ContentPackSourceProposalRecord{*proposal})
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.source_proposal.rejected", "content_pack_source_proposal", proposal.ID.String(), map[string]any{
		"proposal_id":     proposal.ProposalID,
		"node_id":         proposal.NodeID.String(),
		"program":         proposal.Program,
		"source_id":       proposal.SourceID,
		"privacy_blocked": proposal.Status == storage.ContentPackSourceProposalStatusPrivacyBlocked,
		"reason":          proposal.RejectionReason,
	})
	writeJSON(w, http.StatusOK, newContentPackSourceProposalDTO(*proposal))
}

func contentPackSourceHealthResponseFromRuntimeStates(tenantID uuid.UUID, generatedAt time.Time, rows []storage.ContentPackSourceRuntimeStateRecord, total, limit, offset int, summary *storage.ContentPackSourceRuntimeStateSummary) contentPackSourceHealthResponse {
	evidence := make(map[string]tenantSourceHealthEvidence, len(rows))
	for _, row := range rows {
		item := tenantSourceHealthEvidenceFromRuntimeState(row.State)
		if item.SourceID == "" {
			continue
		}
		item.RuntimeStateID = row.ID.String()
		key := item.CollectorID + "/" + item.SourceID
		if key == "/" {
			key = item.SourceID
		}
		evidence[key] = item
	}
	pagination := newPaginationMeta(total, limit, offset, len(rows))
	resp := contentPackSourceHealthResponseFromEvidence(tenantID, generatedAt, evidence, &pagination)
	if summary != nil && resp.Totals.ByState[string(contentpacks.CoverageState(contentpacks.CoverageCollectionConflict))] == 0 {
		resp.Totals = contentPackSourceHealthTotals{
			Sources:             summary.Total,
			CollectorsReporting: summary.CollectorsReporting,
			ByState:             summary.ByState,
			Metrics:             summary.Metrics,
		}
	}
	return resp
}

func contentPackSourceHealthResponseFromEvidence(tenantID uuid.UUID, generatedAt time.Time, evidence map[string]tenantSourceHealthEvidence, pagination *paginationMeta) contentPackSourceHealthResponse {
	evidence = sourceHealthEvidenceWithCollectionConflicts(evidence)
	keys := make([]string, 0, len(evidence))
	for key := range evidence {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]contentPackSourceHealthDTO, 0, len(keys))
	byState := map[string]int{}
	for _, key := range keys {
		item := sourceHealthEvidenceApplyFreshness(evidence[key], generatedAt)
		dto := newContentPackSourceHealthDTO(item)
		items = append(items, dto)
		byState[dto.CoverageState]++
	}
	return contentPackSourceHealthResponse{
		TenantID:    tenantID.String(),
		GeneratedAt: generatedAt.Format(time.RFC3339),
		Items:       items,
		Totals: contentPackSourceHealthTotals{
			Sources:             len(items),
			CollectorsReporting: len(sourceHealthReportingCollectors(evidence)),
			ByState:             byState,
		},
		Pagination: pagination,
	}
}

func parseContentPackSourceHealthStateFilter(values map[string][]string) ([]contentpacks.CoverageState, error) {
	rawValues := make([]string, 0, len(values["state"])+len(values["states"])+len(values["coverage_state"]))
	rawValues = append(rawValues, values["state"]...)
	rawValues = append(rawValues, values["states"]...)
	rawValues = append(rawValues, values["coverage_state"]...)
	if len(rawValues) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := []contentpacks.CoverageState{}
	for _, rawValue := range rawValues {
		for _, part := range strings.Split(rawValue, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" || strings.EqualFold(trimmed, "all") {
				continue
			}
			normalized := contentpacks.NormalizeCoverageState(trimmed)
			value := string(normalized)
			if !contentpacks.ValidCoverageState(value) {
				return nil, fmt.Errorf("invalid source health state %q", trimmed)
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out, nil
}

func parseContentPackSourceProposalStatusFilter(values map[string][]string) ([]string, error) {
	rawValues := make([]string, 0, len(values["status"])+len(values["statuses"]))
	rawValues = append(rawValues, values["status"]...)
	rawValues = append(rawValues, values["statuses"]...)
	if len(rawValues) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, rawValue := range rawValues {
		for _, part := range strings.Split(rawValue, ",") {
			status := strings.ToLower(strings.TrimSpace(part))
			if status == "" || status == "all" {
				continue
			}
			if !validContentPackSourceProposalStatus(status) {
				return nil, fmt.Errorf("invalid source proposal status %q", strings.TrimSpace(part))
			}
			if _, ok := seen[status]; ok {
				continue
			}
			seen[status] = struct{}{}
			out = append(out, status)
		}
	}
	return out, nil
}

func validContentPackSourceProposalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case storage.ContentPackSourceProposalStatusProposed,
		storage.ContentPackSourceProposalStatusAutoEligible,
		storage.ContentPackSourceProposalStatusApprovalRequired,
		storage.ContentPackSourceProposalStatusApproved,
		storage.ContentPackSourceProposalStatusRejected,
		storage.ContentPackSourceProposalStatusPrivacyBlocked,
		storage.ContentPackSourceProposalStatusStale:
		return true
	default:
		return false
	}
}

func filterSourceHealthEvidence(evidence map[string]tenantSourceHealthEvidence, query string, states []contentpacks.CoverageState, generatedAt time.Time) map[string]tenantSourceHealthEvidence {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" && len(states) == 0 {
		return evidence
	}
	if len(evidence) == 0 {
		return evidence
	}
	stateSet := map[contentpacks.CoverageState]struct{}{}
	for _, state := range states {
		normalized := contentpacks.NormalizeCoverageState(string(state))
		if normalized != "" {
			stateSet[normalized] = struct{}{}
		}
	}
	filtered := make(map[string]tenantSourceHealthEvidence, len(evidence))
	for key, item := range evidence {
		if needle != "" && !sourceHealthEvidenceMatchesQuery(item, needle) {
			continue
		}
		if len(stateSet) > 0 {
			effective := sourceHealthEvidenceApplyFreshness(item, generatedAt)
			if _, ok := stateSet[contentpacks.NormalizeCoverageState(string(effective.State))]; !ok {
				continue
			}
		}
		filtered[key] = item
	}
	return filtered
}

func sourceHealthEvidenceMatchesQuery(item tenantSourceHealthEvidence, needle string) bool {
	values := []string{
		item.SourceInstanceID,
		item.SourceID,
		item.DisplayName,
		item.NodeID,
		item.CollectorID,
		item.ReceiverID,
		item.ParserID,
		item.CollectorMode,
		item.ApprovalID,
		item.ConfigVersion,
		item.ContentVersion,
		item.LastError,
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), needle) {
			return true
		}
	}
	for key, value := range item.Labels {
		if strings.Contains(strings.ToLower(strings.TrimSpace(key)), needle) ||
			strings.Contains(strings.ToLower(strings.TrimSpace(value)), needle) {
			return true
		}
	}
	return false
}

func (s *Server) handleContentPackOTelConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	var req contentPackOTelConfigRenderRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Endpoint) == "" {
		http.Error(w, "endpoint is required", http.StatusBadRequest)
		return
	}
	if !contentPackOTelRequestHasSources(req) {
		http.Error(w, "at least one source_id or source_proposal_id is required", http.StatusBadRequest)
		return
	}

	store, ok := s.store.(contentPackRegistrySnapshotReader)
	if !ok || store == nil {
		http.Error(w, "content pack registry snapshot store unavailable", http.StatusServiceUnavailable)
		return
	}
	record, err := store.ActiveContentPackRegistrySnapshot(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("load active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if record == nil {
		http.Error(w, "no active content pack registry snapshot", http.StatusNotFound)
		return
	}
	registry, err := contentpacks.NewRegistryFromSnapshot(record.Snapshot, controlOneContentPackRuntimeVersion)
	if err != nil {
		s.logger.Warn("restore active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	renderSources, err := s.contentPackOTelRenderSources(r.Context(), tenantID, registry, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	plan, err := contentpacks.BuildOTelCollectorConfig(renderSources, contentpacks.OTelCollectorConfigOptions{
		Endpoint:                    req.Endpoint,
		TenantID:                    tenantID.String(),
		CollectorID:                 req.CollectorID,
		Headers:                     req.Headers,
		InsecureTLS:                 req.InsecureTLS,
		Compression:                 req.Compression,
		Timeout:                     req.Timeout,
		MemoryLimitMiB:              req.MemoryLimitMiB,
		MemorySpikeLimitMiB:         req.MemorySpikeLimitMiB,
		BatchTimeout:                req.BatchTimeout,
		BatchSendBatchSize:          req.BatchSendBatchSize,
		DisablePersistentStorage:    req.DisablePersistentStorage,
		StorageExtensionID:          req.StorageExtensionID,
		StorageDirectory:            req.StorageDirectory,
		ExporterQueueSize:           req.ExporterQueueSize,
		ExporterQueueConsumers:      req.ExporterQueueConsumers,
		ExporterRetryMaxElapsedTime: req.ExporterRetryMaxElapsedTime,
		SourceApprovalRefs:          req.SourceApprovalRefs,
		AdditionalAttributes:        req.AdditionalAttributes,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	renderedYAML, err := contentpacks.RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		s.logger.Warn("render OTel content pack collector config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, contentPackOTelConfigRenderResponse{
		TenantID:          tenantID.String(),
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		SnapshotID:        record.ID.String(),
		SnapshotCreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
		ConfigVersion:     contentpacks.OTelCollectorConfigVersion(renderedYAML),
		Sources:           plan.Sources,
		Warnings:          plan.Warnings,
		Config:            plan.Config,
		YAML:              string(renderedYAML),
	})
}

func (s *Server) handleContentPackOTelConfigCandidates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListContentPackOTelConfigCandidates(w, r)
	case http.MethodPost:
		s.handleCreateContentPackOTelConfigCandidate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListContentPackOTelConfigCandidates(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	rows, total, err := store.ListContentPackCollectorConfigCandidates(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Warn("list content pack collector config candidates", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]contentPackOTelConfigCandidateDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, newContentPackOTelConfigCandidateDTO(row))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[contentPackOTelConfigCandidateDTO]{
		Data:       out,
		Pagination: newPaginationMeta(total, limit, offset, len(out)),
	})
}

func (s *Server) handleGetContentPackOTelConfigCandidate(w http.ResponseWriter, r *http.Request, candidateIDRaw string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	candidateID, err := uuid.Parse(strings.TrimSpace(candidateIDRaw))
	if err != nil {
		http.Error(w, "invalid candidate id", http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	candidate, err := store.GetContentPackCollectorConfigCandidate(r.Context(), candidateID)
	if err != nil {
		s.logger.Warn("get content pack collector config candidate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if candidate == nil || candidate.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, newContentPackOTelConfigCandidateDetailDTO(*candidate))
}

func (s *Server) handleCreateContentPackOTelConfigCandidate(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req contentPackOTelConfigRenderRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	record, plan, renderedYAML, ok := s.renderContentPackOTelConfig(w, r, tenantID, req, store)
	if !ok {
		return
	}
	configVersion := contentpacks.OTelCollectorConfigVersion(renderedYAML)
	candidate, err := store.CreateContentPackCollectorConfigCandidate(r.Context(), storage.CreateContentPackCollectorConfigCandidateParams{
		TenantID:           tenantID,
		RegistrySnapshotID: record.ID,
		ConfigVersion:      configVersion,
		CollectorID:        req.CollectorID,
		Endpoint:           req.Endpoint,
		SourceIDs:          contentPackOTelPlanSourceIDs(plan),
		Plan:               plan,
		RenderedYAML:       string(renderedYAML),
		CreatedBySubject:   strings.TrimSpace(principal.Subject),
	})
	if err != nil {
		s.logger.Warn("create content pack collector config candidate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.persistContentPackSourceRuntimeStatesFromOTelPlan(r.Context(), tenantID, req.CollectorID, configVersion, candidate.ID.String(), candidate.Status, contentpacks.CoverageState(contentpacks.CoverageConfigRendered), "", plan)
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.otel_config_candidate.created", "content_pack_collector_config_candidate", candidate.ID.String(), map[string]any{
		"config_version":       candidate.ConfigVersion,
		"registry_snapshot_id": candidate.RegistrySnapshotID.String(),
		"collector_id":         candidate.CollectorID,
		"source_ids":           candidate.SourceIDs,
	})
	writeJSON(w, http.StatusCreated, contentPackOTelConfigRenderResponse{
		TenantID:          tenantID.String(),
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		SnapshotID:        record.ID.String(),
		SnapshotCreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
		ConfigVersion:     configVersion,
		CandidateID:       candidate.ID.String(),
		CandidateStatus:   candidate.Status,
		Sources:           plan.Sources,
		Warnings:          plan.Warnings,
		Config:            plan.Config,
		YAML:              string(renderedYAML),
	})
}

func (s *Server) handleApproveContentPackOTelConfigCandidate(w http.ResponseWriter, r *http.Request, candidateIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	candidateID, err := uuid.Parse(strings.TrimSpace(candidateIDRaw))
	if err != nil {
		http.Error(w, "invalid candidate id", http.StatusBadRequest)
		return
	}
	var req contentPackOTelConfigCandidateApprovalRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	req.ReviewedConfigVersion = strings.TrimSpace(req.ReviewedConfigVersion)
	if req.ReviewedConfigVersion == "" {
		http.Error(w, "reviewed_config_version is required", http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	candidate, err := store.ApproveContentPackCollectorConfigCandidate(r.Context(), storage.ApproveContentPackCollectorConfigCandidateParams{
		TenantID:              tenantID,
		CandidateID:           candidateID,
		ApprovedBySubject:     strings.TrimSpace(principal.Subject),
		ApprovalNote:          req.Note,
		ReviewedConfigVersion: req.ReviewedConfigVersion,
	})
	if err != nil {
		s.logger.Warn("approve content pack collector config candidate", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.otel_config_candidate.approved", "content_pack_collector_config_candidate", candidate.ID.String(), map[string]any{
		"config_version":          candidate.ConfigVersion,
		"registry_snapshot_id":    candidate.RegistrySnapshotID.String(),
		"collector_id":            candidate.CollectorID,
		"source_ids":              candidate.SourceIDs,
		"approval_note":           candidate.ApprovalNote,
		"reviewed_config_version": candidate.ReviewedConfigVersion,
		"reviewed_yaml_sha256":    candidate.ReviewedYAMLSHA256,
	})
	writeJSON(w, http.StatusOK, newContentPackOTelConfigCandidateDTO(*candidate))
}

func (s *Server) handleQueueContentPackOTelConfigCandidate(w http.ResponseWriter, r *http.Request, candidateIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	candidateID, err := uuid.Parse(strings.TrimSpace(candidateIDRaw))
	if err != nil {
		http.Error(w, "invalid candidate id", http.StatusBadRequest)
		return
	}
	var req contentPackOTelConfigCandidateQueueRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	req.ExpectedConfigVersion = strings.TrimSpace(req.ExpectedConfigVersion)
	if req.ExpectedConfigVersion == "" {
		http.Error(w, "expected_config_version is required", http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	candidate, err := store.QueueContentPackCollectorConfigCandidate(r.Context(), storage.QueueContentPackCollectorConfigCandidateParams{
		TenantID:              tenantID,
		CandidateID:           candidateID,
		TargetCollectorID:     req.CollectorID,
		QueuedBySubject:       strings.TrimSpace(principal.Subject),
		QueueNote:             req.Note,
		ExpectedConfigVersion: req.ExpectedConfigVersion,
	})
	if err != nil {
		s.logger.Warn("queue content pack collector config candidate", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.persistContentPackSourceRuntimeStatesFromOTelPlan(r.Context(), tenantID, candidate.TargetCollectorID, candidate.ConfigVersion, candidate.ID.String(), candidate.Status, contentpacks.CoverageState(contentpacks.CoverageConfigRendered), "", candidate.Plan)
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.otel_config_candidate.queued", "content_pack_collector_config_candidate", candidate.ID.String(), map[string]any{
		"config_version":          candidate.ConfigVersion,
		"target_collector_id":     candidate.TargetCollectorID,
		"source_ids":              candidate.SourceIDs,
		"queue_note":              candidate.QueueNote,
		"expected_config_version": req.ExpectedConfigVersion,
		"reviewed_config_version": candidate.ReviewedConfigVersion,
	})
	writeJSON(w, http.StatusOK, newContentPackOTelConfigCandidateDTO(*candidate))
}

func (s *Server) handleContentPackEdgeCollectors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListContentPackEdgeCollectors(w, r)
	case http.MethodPost:
		s.handleRegisterContentPackEdgeCollector(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListContentPackEdgeCollectors(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackEdgeCollectorStore)
	if !ok || store == nil {
		http.Error(w, "content pack edge collector store unavailable", http.StatusServiceUnavailable)
		return
	}
	rows, total, err := store.ListContentPackEdgeCollectors(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Warn("list content pack edge collectors", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]contentPackEdgeCollectorDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, newContentPackEdgeCollectorDTO(row))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[contentPackEdgeCollectorDTO]{
		Data:       out,
		Pagination: newPaginationMeta(total, limit, offset, len(out)),
	})
}

func (s *Server) handleRegisterContentPackEdgeCollector(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	var req contentPackEdgeCollectorRegistrationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.CollectorID) == "" {
		http.Error(w, "collector_id is required", http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackEdgeCollectorStore)
	if !ok || store == nil {
		http.Error(w, "content pack edge collector store unavailable", http.StatusServiceUnavailable)
		return
	}
	collector, err := store.UpsertContentPackEdgeCollectorRegistration(r.Context(), storage.UpsertContentPackEdgeCollectorRegistrationParams{
		TenantID:             tenantID,
		CollectorID:          req.CollectorID,
		Kind:                 req.Kind,
		DisplayName:          req.DisplayName,
		Endpoint:             req.Endpoint,
		Version:              req.Version,
		DesiredConfigVersion: req.DesiredConfigVersion,
	})
	if err != nil {
		s.logger.Warn("register content pack edge collector", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.edge_collector.registered", "content_pack_edge_collector", collector.ID.String(), map[string]any{
		"collector_id":              collector.CollectorID,
		"kind":                      collector.Kind,
		"desired_config_version":    collector.DesiredConfigVersion,
		"running_config_version":    collector.RunningConfigVersion,
		"collector_status":          collector.Status,
		"collector_display_name":    collector.DisplayName,
		"collector_endpoint":        collector.Endpoint,
		"collector_runtime_version": collector.Version,
	})
	writeJSON(w, http.StatusCreated, newContentPackEdgeCollectorDTO(*collector))
}

func (s *Server) handleRotateContentPackEdgeCollectorToken(w http.ResponseWriter, r *http.Request, collectorIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	collectorID := strings.TrimSpace(collectorIDRaw)
	if collectorID == "" {
		http.Error(w, "collector id is required", http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackEdgeCollectorStore)
	if !ok || store == nil {
		http.Error(w, "content pack edge collector store unavailable", http.StatusServiceUnavailable)
		return
	}
	issued, err := store.RotateContentPackEdgeCollectorToken(r.Context(), storage.RotateContentPackEdgeCollectorTokenParams{
		TenantID:    tenantID,
		CollectorID: collectorID,
	})
	if err != nil {
		s.logger.Warn("rotate content pack edge collector token", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.edge_collector.token_rotated", "content_pack_edge_collector", issued.Collector.ID.String(), map[string]any{
		"collector_id":    issued.Collector.CollectorID,
		"token_last_four": issued.Collector.TokenLastFour,
		"token_issued_at": formatContentPackTimePtr(issued.Collector.TokenIssuedAt),
	})
	writeJSON(w, http.StatusOK, contentPackEdgeCollectorTokenResponse{
		Collector: newContentPackEdgeCollectorDTO(issued.Collector),
		Token:     issued.Token,
	})
}

func (s *Server) handleContentPackEdgeCollectorHeartbeat(w http.ResponseWriter, r *http.Request, collectorIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	collectorID := strings.TrimSpace(collectorIDRaw)
	if collectorID == "" {
		http.Error(w, "collector id is required", http.StatusBadRequest)
		return
	}
	tenantID, _, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collectorID, roleOperator, roleAdmin)
	if !ok {
		return
	}
	var req contentPackEdgeCollectorHeartbeatRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackEdgeCollectorStore)
	if !ok || store == nil {
		http.Error(w, "content pack edge collector store unavailable", http.StatusServiceUnavailable)
		return
	}
	collector, err := store.RecordContentPackEdgeCollectorHeartbeat(r.Context(), storage.RecordContentPackEdgeCollectorHeartbeatParams{
		TenantID:             tenantID,
		CollectorID:          collectorID,
		Kind:                 req.Kind,
		Version:              req.Version,
		Status:               req.Status,
		DesiredConfigVersion: req.DesiredConfigVersion,
		RunningConfigVersion: req.RunningConfigVersion,
		Health:               req.Health,
		LastError:            req.LastError,
	})
	if err != nil {
		s.logger.Warn("record content pack edge collector heartbeat", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.persistContentPackSourceRuntimeStates(r.Context(), tenantID, *collector)
	writeJSON(w, http.StatusOK, newContentPackEdgeCollectorDTO(*collector))
}

func (s *Server) handleContentPackEdgeCollectorDesiredConfig(w http.ResponseWriter, r *http.Request, collectorIDRaw string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	collectorID := strings.TrimSpace(collectorIDRaw)
	if collectorID == "" {
		http.Error(w, "collector id is required", http.StatusBadRequest)
		return
	}
	tenantID, _, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collectorID, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	candidate, err := store.QueuedContentPackCollectorConfigForCollector(r.Context(), tenantID, collectorID)
	if err != nil {
		s.logger.Warn("load queued content pack collector config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if candidate == nil {
		http.Error(w, "no queued collector config", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, contentPackEdgeCollectorDesiredConfigResponse{
		TenantID:      tenantID.String(),
		CollectorID:   collectorID,
		CandidateID:   candidate.ID.String(),
		ConfigVersion: candidate.ConfigVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		QueuedAt:      formatContentPackTimePtr(candidate.QueuedAt),
		Sources:       candidate.Plan.Sources,
		Warnings:      candidate.Plan.Warnings,
		YAML:          candidate.RenderedYAML,
	})
}

func (s *Server) handleContentPackEdgeCollectorApplyResult(w http.ResponseWriter, r *http.Request, collectorIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	collectorID := strings.TrimSpace(collectorIDRaw)
	if collectorID == "" {
		http.Error(w, "collector id is required", http.StatusBadRequest)
		return
	}
	tenantID, principal, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collectorID, roleOperator, roleAdmin)
	if !ok {
		return
	}
	var req contentPackEdgeCollectorApplyResultRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	candidate, err := store.RecordContentPackCollectorConfigApplyResult(r.Context(), storage.RecordContentPackCollectorConfigApplyResultParams{
		TenantID:      tenantID,
		CollectorID:   collectorID,
		ConfigVersion: req.ConfigVersion,
		Status:        req.Status,
		ErrorMessage:  req.Error,
	})
	if err != nil {
		s.logger.Warn("record content pack collector config apply result", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	coverageState := contentpacks.CoverageState(contentpacks.CoverageConfigRendered)
	lastError := candidate.DeploymentError
	if candidate.Status == storage.ContentPackCollectorConfigStatusDeployed {
		coverageState = contentpacks.CoverageState(contentpacks.CoverageDeployed)
		lastError = ""
	}
	s.persistContentPackSourceRuntimeStatesFromOTelPlan(r.Context(), tenantID, candidate.TargetCollectorID, candidate.ConfigVersion, candidate.ID.String(), candidate.Status, coverageState, lastError, candidate.Plan)
	auditMetadata := map[string]any{
		"config_version":      candidate.ConfigVersion,
		"target_collector_id": candidate.TargetCollectorID,
		"deployment_error":    candidate.DeploymentError,
	}
	if principal == nil {
		auditMetadata["collector_auth"] = true
	}
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.otel_config_candidate."+candidate.Status, "content_pack_collector_config_candidate", candidate.ID.String(), auditMetadata)
	writeJSON(w, http.StatusOK, newContentPackOTelConfigCandidateDTO(*candidate))
}

func (s *Server) handleContentPackEdgeCollectorRollback(w http.ResponseWriter, r *http.Request, collectorIDRaw string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	collectorID := strings.TrimSpace(collectorIDRaw)
	if collectorID == "" {
		http.Error(w, "collector id is required", http.StatusBadRequest)
		return
	}
	var req contentPackEdgeCollectorRollbackRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	var candidateID uuid.UUID
	if strings.TrimSpace(req.CandidateID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(req.CandidateID))
		if err != nil {
			http.Error(w, "invalid candidate_id", http.StatusBadRequest)
			return
		}
		candidateID = parsed
	}
	store, ok := s.store.(contentPackCollectorConfigCandidateStore)
	if !ok || store == nil {
		http.Error(w, "content pack collector config candidate store unavailable", http.StatusServiceUnavailable)
		return
	}
	candidate, err := store.QueueContentPackCollectorConfigRollback(r.Context(), storage.QueueContentPackCollectorConfigRollbackParams{
		TenantID:        tenantID,
		CollectorID:     collectorID,
		CandidateID:     candidateID,
		ConfigVersion:   req.ConfigVersion,
		QueuedBySubject: strings.TrimSpace(principal.Subject),
		QueueNote:       req.Note,
	})
	if err != nil {
		s.logger.Warn("queue content pack collector config rollback", zap.Error(err))
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.persistContentPackSourceRuntimeStatesFromOTelPlan(r.Context(), tenantID, candidate.TargetCollectorID, candidate.ConfigVersion, candidate.ID.String(), candidate.Status, contentpacks.CoverageState(contentpacks.CoverageConfigRendered), "", candidate.Plan)
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.otel_config_candidate.rollback_queued", "content_pack_collector_config_candidate", candidate.ID.String(), map[string]any{
		"config_version":      candidate.ConfigVersion,
		"target_collector_id": candidate.TargetCollectorID,
		"queue_note":          candidate.QueueNote,
	})
	writeJSON(w, http.StatusOK, newContentPackOTelConfigCandidateDTO(*candidate))
}

func (s *Server) renderContentPackOTelConfig(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, req contentPackOTelConfigRenderRequest, store contentPackRegistrySnapshotReader) (*storage.ContentPackRegistrySnapshotRecord, contentpacks.OTelCollectorConfigPlan, []byte, bool) {
	if strings.TrimSpace(req.Endpoint) == "" {
		http.Error(w, "endpoint is required", http.StatusBadRequest)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	if !contentPackOTelRequestHasSources(req) {
		http.Error(w, "at least one source_id or source_proposal_id is required", http.StatusBadRequest)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	if store == nil {
		http.Error(w, "content pack registry snapshot store unavailable", http.StatusServiceUnavailable)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	record, err := store.ActiveContentPackRegistrySnapshot(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("load active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	if record == nil {
		http.Error(w, "no active content pack registry snapshot", http.StatusNotFound)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	registry, err := contentpacks.NewRegistryFromSnapshot(record.Snapshot, controlOneContentPackRuntimeVersion)
	if err != nil {
		s.logger.Warn("restore active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	renderSources, err := s.contentPackOTelRenderSources(r.Context(), tenantID, registry, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	plan, err := contentpacks.BuildOTelCollectorConfig(renderSources, contentpacks.OTelCollectorConfigOptions{
		Endpoint:                    req.Endpoint,
		TenantID:                    tenantID.String(),
		CollectorID:                 req.CollectorID,
		Headers:                     req.Headers,
		InsecureTLS:                 req.InsecureTLS,
		Compression:                 req.Compression,
		Timeout:                     req.Timeout,
		MemoryLimitMiB:              req.MemoryLimitMiB,
		MemorySpikeLimitMiB:         req.MemorySpikeLimitMiB,
		BatchTimeout:                req.BatchTimeout,
		BatchSendBatchSize:          req.BatchSendBatchSize,
		DisablePersistentStorage:    req.DisablePersistentStorage,
		StorageExtensionID:          req.StorageExtensionID,
		StorageDirectory:            req.StorageDirectory,
		ExporterQueueSize:           req.ExporterQueueSize,
		ExporterQueueConsumers:      req.ExporterQueueConsumers,
		ExporterRetryMaxElapsedTime: req.ExporterRetryMaxElapsedTime,
		SourceApprovalRefs:          req.SourceApprovalRefs,
		AdditionalAttributes:        req.AdditionalAttributes,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	renderedYAML, err := contentpacks.RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		s.logger.Warn("render OTel content pack collector config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, contentpacks.OTelCollectorConfigPlan{}, nil, false
	}
	return record, plan, renderedYAML, true
}

func (s *Server) contentPackTenantFromQuery(w http.ResponseWriter, r *http.Request, principal *auth.Principal) (uuid.UUID, bool) {
	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return uuid.Nil, false
	}
	if tenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return uuid.Nil, false
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
		return uuid.Nil, false
	}
	return tenantID, true
}

func (s *Server) authorizeContentPackEdgeCollectorCall(w http.ResponseWriter, r *http.Request, collectorID string, allowedRoles ...string) (uuid.UUID, *auth.Principal, bool) {
	if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
		if _, ok := s.authorize(w, r, allowedRoles...); !ok {
			return uuid.Nil, nil, false
		}
		tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
		return tenantID, principal, ok
	}

	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return uuid.Nil, nil, false
	}
	if tenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return uuid.Nil, nil, false
	}

	token := contentPackEdgeCollectorTokenFromRequest(r)
	if token == "" {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return uuid.Nil, nil, false
	}
	store, ok := s.store.(contentPackEdgeCollectorStore)
	if !ok || store == nil {
		http.Error(w, "content pack edge collector store unavailable", http.StatusServiceUnavailable)
		return uuid.Nil, nil, false
	}
	collector, err := store.ValidateContentPackEdgeCollectorToken(r.Context(), tenantID, collectorID, token)
	if err != nil {
		s.logger.Warn("validate content pack edge collector token", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return uuid.Nil, nil, false
	}
	if collector == nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return uuid.Nil, nil, false
	}
	return tenantID, nil, true
}

func (s *Server) persistContentPackSourceRuntimeStates(ctx context.Context, tenantID uuid.UUID, collector storage.ContentPackEdgeCollector) {
	store, ok := s.store.(contentPackSourceRuntimeStateStore)
	if !ok || store == nil {
		return
	}
	evidence := sourceHealthEvidenceFromCollectors([]storage.ContentPackEdgeCollector{collector})
	for _, item := range evidence {
		state, ok := contentPackSourceRuntimeStateFromEvidence(item)
		if !ok {
			continue
		}
		if _, err := store.UpsertContentPackSourceRuntimeState(ctx, storage.UpsertContentPackSourceRuntimeStateParams{
			TenantID: tenantID,
			State:    state,
		}); err != nil {
			s.logger.Warn("persist content pack source runtime state",
				zap.Error(err),
				zap.String("collector_id", item.CollectorID),
				zap.String("source_id", item.SourceID),
			)
		}
	}
}

func (s *Server) persistContentPackSourceRuntimeStatesFromProposals(ctx context.Context, proposals []storage.ContentPackSourceProposalRecord) {
	store, ok := s.store.(contentPackSourceRuntimeStateStore)
	if !ok || store == nil || len(proposals) == 0 {
		return
	}
	for _, proposal := range proposals {
		state, ok := contentPackSourceRuntimeStateFromProposal(proposal)
		if !ok {
			continue
		}
		if _, err := store.UpsertContentPackSourceRuntimeState(ctx, storage.UpsertContentPackSourceRuntimeStateParams{
			TenantID: proposal.TenantID,
			State:    state,
		}); err != nil {
			s.logger.Warn("persist source runtime state from connector proposal",
				zap.Error(err),
				zap.String("proposal_id", proposal.ID.String()),
				zap.String("source_id", proposal.SourceID),
			)
		}
	}
}

func (s *Server) persistContentPackSourceRuntimeStatesFromOTelPlan(ctx context.Context, tenantID uuid.UUID, collectorID, configVersion, candidateID, candidateStatus string, coverageState contentpacks.CoverageState, lastError string, plan contentpacks.OTelCollectorConfigPlan) {
	store, ok := s.store.(contentPackSourceRuntimeStateStore)
	if !ok || store == nil || tenantID == uuid.Nil || len(plan.Sources) == 0 {
		return
	}
	if !contentpacks.ValidCoverageState(string(coverageState)) {
		coverageState = contentpacks.CoverageState(contentpacks.CoverageConfigRendered)
	}
	now := time.Now().UTC()
	for _, source := range plan.Sources {
		sourceID := strings.TrimSpace(source.SourceID)
		if sourceID == "" {
			continue
		}
		state := contentpacks.SourceRuntimeState{
			SourceInstanceID: contentPackSourceInstanceID(collectorID, sourceID),
			PackID:           strings.TrimSpace(source.PackID),
			PackVersion:      strings.TrimSpace(source.PackVersion),
			SourceID:         sourceID,
			CollectorID:      strings.TrimSpace(collectorID),
			CollectorMode:    strings.TrimSpace(source.Mode),
			CoverageState:    coverageState,
			ApprovalRequired: strings.TrimSpace(source.ApprovalRef) != "",
			ApprovalID:       strings.TrimSpace(source.ApprovalRef),
			ConfigVersion:    strings.TrimSpace(configVersion),
			LastHealthAt:     &now,
			LastError:        strings.TrimSpace(lastError),
			Labels: map[string]string{
				"candidate_id":                        strings.TrimSpace(candidateID),
				"candidate_status":                    strings.TrimSpace(candidateStatus),
				"pipeline_id":                         strings.TrimSpace(source.PipelineID),
				"receiver":                            strings.TrimSpace(source.Receiver),
				"approval_ref":                        strings.TrimSpace(source.ApprovalRef),
				contentPackCollectionOwnerLabel:       contentPackCollectionOwnerOTelEdge,
				contentPackCollectionIdentityLabel:    contentPackOTelPlanCollectionIdentity(collectorID, source),
				"control_one.receiver_identity":       strings.Join(source.ReceiverIDs, ","),
				"control_one.collection_mode":         strings.TrimSpace(source.Mode),
				"control_one.collection_config_owner": "otel_candidate",
			},
			UpdatedAt: now,
		}
		if _, err := store.UpsertContentPackSourceRuntimeState(ctx, storage.UpsertContentPackSourceRuntimeStateParams{
			TenantID: tenantID,
			State:    state,
		}); err != nil {
			s.logger.Warn("persist source runtime state from OTel config candidate",
				zap.Error(err),
				zap.String("source_id", sourceID),
				zap.String("collector_id", collectorID),
				zap.String("config_version", configVersion),
			)
		}
	}
}

func contentPackSourceRuntimeStateFromProposal(proposal storage.ContentPackSourceProposalRecord) (contentpacks.SourceRuntimeState, bool) {
	sourceID := firstNonEmptyContentPack(proposal.SourceID, proposal.Program)
	if proposal.TenantID == uuid.Nil || proposal.NodeID == uuid.Nil || strings.TrimSpace(sourceID) == "" {
		return contentpacks.SourceRuntimeState{}, false
	}
	coverageState := contentPackProposalCoverageState(proposal.Status)
	if !contentpacks.ValidCoverageState(string(coverageState)) {
		return contentpacks.SourceRuntimeState{}, false
	}
	lastHealthAt := proposal.LastSeenAt.UTC()
	if lastHealthAt.IsZero() {
		lastHealthAt = time.Now().UTC()
	}
	labels := map[string]string{
		"proposal_id":                    proposal.ID.String(),
		"proposal_external_id":           strings.TrimSpace(proposal.ProposalID),
		"proposal_status":                strings.TrimSpace(proposal.Status),
		"proposal_kind":                  strings.TrimSpace(proposal.Kind),
		"program":                        strings.TrimSpace(proposal.Program),
		"collector_type":                 strings.TrimSpace(proposal.CollectorType),
		"formatter":                      strings.TrimSpace(proposal.Formatter),
		"control_one.source_proposal_id": proposal.ID.String(),
		"control_one.source_proposal_external_id": strings.TrimSpace(proposal.ProposalID),
		"control_one.connector_decision":          strings.TrimSpace(proposal.Status),
		contentPackCollectionOwnerLabel:           contentPackCollectionOwnerProposal,
		contentPackCollectionIdentityLabel:        contentPackProposalCollectionIdentity(proposal, sourceID),
	}
	applyContentPackProposalCollectModeLabels(labels, proposal)
	return contentpacks.SourceRuntimeState{
		SourceInstanceID: contentPackProposalSourceInstanceID(proposal.NodeID, sourceID),
		SourceID:         strings.TrimSpace(sourceID),
		DisplayName:      strings.TrimSpace(firstNonEmptyContentPack(proposal.Program, proposal.ProposalID)),
		NodeID:           proposal.NodeID.String(),
		CollectorMode:    contentPackProposalCollectorMode(proposal),
		ParserID:         strings.TrimSpace(sourceID),
		CoverageState:    coverageState,
		ApprovalRequired: proposal.RequiresApproval,
		ApprovalID:       contentPackProposalApprovalID(proposal),
		LastHealthAt:     &lastHealthAt,
		LastError:        contentPackProposalLastError(proposal),
		Labels:           labels,
		UpdatedAt:        time.Now().UTC(),
	}, true
}

func applyContentPackProposalCollectModeLabels(labels map[string]string, proposal storage.ContentPackSourceProposalRecord) {
	if labels == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(proposal.CollectMode))
	if mode == "" && proposal.Status == storage.ContentPackSourceProposalStatusApproved {
		mode = storage.ContentPackSourceProposalCollectModeCollectRaw
	}
	if mode == "" {
		return
	}
	labels["collect_mode"] = mode
	labels["control_one.collect_mode"] = mode
	switch mode {
	case storage.ContentPackSourceProposalCollectModeMetadataOnly:
		labels["runtime_evidence"] = "metadata_observed"
		labels["metadata_observed"] = "true"
		labels["log_collection_started"] = "false"
		labels["collection_disabled_reason"] = storage.ContentPackSourceProposalCollectModeMetadataOnly
		labels["raw_message_retained"] = "false"
		labels["control_one.raw_message_retained"] = "false"
	case storage.ContentPackSourceProposalCollectModeObserveOnly:
		labels["runtime_evidence"] = "proposal_observed"
		labels["observe_only"] = "true"
		labels["log_collection_started"] = "false"
		labels["collection_disabled_reason"] = storage.ContentPackSourceProposalCollectModeObserveOnly
		labels["raw_message_retained"] = "false"
		labels["control_one.raw_message_retained"] = "false"
	case storage.ContentPackSourceProposalCollectModeDisabled:
		labels["runtime_evidence"] = "collection_disabled"
		labels["log_collection_started"] = "false"
		labels["collection_disabled_reason"] = storage.ContentPackSourceProposalCollectModeDisabled
		labels["raw_message_retained"] = "false"
		labels["control_one.raw_message_retained"] = "false"
	case storage.ContentPackSourceProposalCollectModeCollectParsed:
		labels["raw_message_retained"] = "false"
		labels["control_one.raw_message_retained"] = "false"
	}
}

func contentPackSourceRuntimeStateFromEvidence(item tenantSourceHealthEvidence) (contentpacks.SourceRuntimeState, bool) {
	sourceID := strings.TrimSpace(item.SourceID)
	if sourceID == "" {
		return contentpacks.SourceRuntimeState{}, false
	}
	state := contentpacks.NormalizeCoverageState(string(item.State))
	if !contentpacks.ValidCoverageState(string(state)) {
		state = contentpacks.CoverageState(contentpacks.CoverageDeployed)
	}
	sourceInstanceID := strings.TrimSpace(item.SourceInstanceID)
	if sourceInstanceID == "" {
		sourceInstanceID = contentPackSourceInstanceID(item.CollectorID, sourceID)
	}
	labels := mergeHealthLabels(item.Labels, map[string]string{
		"receiver_id": strings.TrimSpace(item.ReceiverID),
	})
	if labels == nil {
		labels = map[string]string{}
	}
	if strings.TrimSpace(labels[contentPackCollectionOwnerLabel]) == "" {
		labels[contentPackCollectionOwnerLabel] = contentPackSourceCollectionOwner(item)
	}
	if strings.TrimSpace(labels[contentPackCollectionIdentityLabel]) == "" {
		labels[contentPackCollectionIdentityLabel] = contentPackSourceCollectionIdentity(item)
	}
	return contentpacks.SourceRuntimeState{
		SourceInstanceID: sourceInstanceID,
		PackID:           strings.TrimSpace(item.PackID),
		PackVersion:      strings.TrimSpace(item.PackVersion),
		SourceID:         sourceID,
		DisplayName:      strings.TrimSpace(item.DisplayName),
		NodeID:           strings.TrimSpace(item.NodeID),
		CollectorID:      strings.TrimSpace(item.CollectorID),
		CollectorMode:    strings.TrimSpace(item.CollectorMode),
		ParserID:         strings.TrimSpace(item.ParserID),
		CoverageState:    state,
		ApprovalRequired: item.ApprovalRequired,
		ApprovalID:       strings.TrimSpace(item.ApprovalID),
		ConfigVersion:    strings.TrimSpace(item.ConfigVersion),
		ContentVersion:   strings.TrimSpace(item.ContentVersion),
		LastEventAt:      item.LastEventAt,
		LastParsedAt:     item.LastParsedAt,
		LastHealthAt:     item.LastHealthAt,
		LastError:        strings.TrimSpace(item.LastError),
		Metrics:          item.Metrics,
		Labels:           labels,
		UpdatedAt:        time.Now().UTC(),
	}, true
}

func contentPackProposalCoverageState(status string) contentpacks.CoverageState {
	switch strings.TrimSpace(status) {
	case storage.ContentPackSourceProposalStatusAutoEligible, storage.ContentPackSourceProposalStatusProposed:
		return contentpacks.CoverageState(contentpacks.CoverageProposed)
	case storage.ContentPackSourceProposalStatusApprovalRequired:
		return contentpacks.CoverageState(contentpacks.CoverageApprovalRequired)
	case storage.ContentPackSourceProposalStatusApproved:
		return contentpacks.CoverageState(contentpacks.CoverageApproved)
	case storage.ContentPackSourceProposalStatusPrivacyBlocked:
		return contentpacks.CoverageState(contentpacks.CoveragePrivacyBlocked)
	case storage.ContentPackSourceProposalStatusRejected:
		return contentpacks.CoverageState(contentpacks.CoverageUnsupported)
	case storage.ContentPackSourceProposalStatusStale:
		return contentpacks.CoverageState(contentpacks.CoverageStale)
	default:
		return ""
	}
}

func contentPackProposalSourceInstanceID(nodeID uuid.UUID, sourceID string) string {
	return "node:" + nodeID.String() + "/" + strings.TrimSpace(sourceID)
}

func contentPackProposalCollectionIdentity(proposal storage.ContentPackSourceProposalRecord, sourceID string) string {
	if proposal.ID != uuid.Nil {
		return "proposal:" + proposal.ID.String()
	}
	return contentPackProposalSourceInstanceID(proposal.NodeID, sourceID)
}

func contentPackAgentLogCollectionIdentity(nodeID uuid.UUID, sourceID string, labels map[string]string) string {
	if proposalID := strings.TrimSpace(labels["control_one.source_proposal_id"]); proposalID != "" {
		return "proposal:" + proposalID
	}
	return contentPackProposalSourceInstanceID(nodeID, sourceID)
}

func contentPackOTelPlanCollectionIdentity(collectorID string, source contentpacks.OTelCollectorSourcePlan) string {
	if approvalRef := strings.TrimSpace(source.ApprovalRef); approvalRef != "" {
		return "proposal:" + approvalRef
	}
	collectorID = strings.TrimSpace(collectorID)
	sourceID := strings.TrimSpace(source.SourceID)
	if collectorID == "" {
		return contentPackSourceInstanceID("", sourceID)
	}
	return "collector:" + contentPackSourceInstanceID(collectorID, sourceID)
}

func contentPackProposalCollectorMode(proposal storage.ContentPackSourceProposalRecord) string {
	if strings.EqualFold(strings.TrimSpace(proposal.Kind), "local_log") && strings.EqualFold(strings.TrimSpace(proposal.CollectorType), "file") {
		return contentpacks.CollectorNodeFileLog
	}
	return strings.TrimSpace(proposal.CollectorType)
}

func contentPackProposalApprovalID(proposal storage.ContentPackSourceProposalRecord) string {
	if proposal.Status != storage.ContentPackSourceProposalStatusApproved {
		return ""
	}
	return proposal.ID.String()
}

func contentPackProposalLastError(proposal storage.ContentPackSourceProposalRecord) string {
	switch proposal.Status {
	case storage.ContentPackSourceProposalStatusRejected:
		return firstNonEmptyContentPack(proposal.RejectionReason, "source proposal rejected")
	case storage.ContentPackSourceProposalStatusPrivacyBlocked:
		return firstNonEmptyContentPack(proposal.RejectionReason, "source proposal privacy-blocked")
	default:
		return ""
	}
}

func contentPackSourceInstanceID(collectorID, sourceID string) string {
	collectorID = strings.TrimSpace(collectorID)
	sourceID = strings.TrimSpace(sourceID)
	if collectorID == "" {
		return sourceID
	}
	return collectorID + "/" + sourceID
}

func contentPackEdgeCollectorTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token := strings.TrimSpace(r.Header.Get("X-ControlOne-Collector-Token")); token != "" {
		return token
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return ""
	}
	token := strings.TrimSpace(authz[7:])
	if !strings.HasPrefix(token, storage.ContentPackEdgeCollectorTokenPrefix) {
		return ""
	}
	return token
}

func (s *Server) activeContentPackRegistryResponse(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) (contentPackListResponse, bool) {
	resp := contentPackListResponse{
		TenantID:    tenantID.String(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      "none",
		Items:       []contentPackRecordDTO{},
		Sources:     []contentPackSourceDTO{},
		Totals: contentPackRegistryTotals{
			ByStatus:    map[string]int{},
			ByRiskClass: map[string]int{},
		},
	}
	store, ok := s.store.(contentPackRegistrySnapshotReader)
	if !ok || store == nil {
		writeJSON(w, http.StatusOK, resp)
		return resp, false
	}
	record, err := store.ActiveContentPackRegistrySnapshot(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("load active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return resp, false
	}
	if record == nil {
		writeJSON(w, http.StatusOK, resp)
		return resp, false
	}
	return newContentPackListResponse(tenantID, *record), true
}

func newContentPackListResponse(tenantID uuid.UUID, record storage.ContentPackRegistrySnapshotRecord) contentPackListResponse {
	resp := contentPackListResponse{
		TenantID:          tenantID.String(),
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		Source:            firstNonEmptyContentPack(record.Source, "database"),
		SnapshotID:        record.ID.String(),
		SnapshotCreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
		ControlOneVersion: record.Snapshot.ControlOneVersion,
		Items:             make([]contentPackRecordDTO, 0, len(record.Snapshot.Packs)),
		Sources:           []contentPackSourceDTO{},
		Totals: contentPackRegistryTotals{
			ByStatus:    map[string]int{},
			ByRiskClass: map[string]int{},
		},
	}
	for _, pack := range record.Snapshot.Packs {
		resp.Items = append(resp.Items, newContentPackRecordDTO(pack))
		resp.Totals.Packs++
		resp.Totals.Parsers += pack.ParserCount
		resp.Totals.Detections += pack.DetectionCount
		resp.Totals.Samples += pack.SampleCount
		resp.Totals.ByStatus[string(pack.Status)]++
		for _, source := range pack.Manifest.Sources {
			dto := newContentPackSourceDTO(pack, source)
			resp.Sources = append(resp.Sources, dto)
			resp.Totals.Sources++
			if dto.RiskClass != "" {
				resp.Totals.ByRiskClass[dto.RiskClass]++
			}
		}
	}
	return resp
}

func newContentPackRecordDTO(record contentpacks.PackRecord) contentPackRecordDTO {
	return contentPackRecordDTO{
		PackID:             record.PackID,
		PackVersion:        record.PackVersion,
		DisplayName:        record.DisplayName,
		Status:             string(record.Status),
		Compatible:         record.Compatible,
		CompatibilityError: record.CompatibilityError,
		InstalledAt:        formatContentPackTime(record.InstalledAt),
		EnabledAt:          formatContentPackTimePtr(record.EnabledAt),
		DisabledAt:         formatContentPackTimePtr(record.DisabledAt),
		QuarantinedAt:      formatContentPackTimePtr(record.QuarantinedAt),
		QuarantineReason:   record.QuarantineReason,
		SourceCount:        record.SourceCount,
		ParserCount:        record.ParserCount,
		DetectionCount:     record.DetectionCount,
		SampleCount:        record.SampleCount,
		Labels:             record.Labels,
	}
}

func newContentPackSourceDTO(record contentpacks.PackRecord, source contentpacks.SourceProfile) contentPackSourceDTO {
	return contentPackSourceDTO{
		PackID:            record.PackID,
		PackVersion:       record.PackVersion,
		ContentStatus:     string(record.Status),
		SourceID:          source.SourceID,
		DisplayName:       source.DisplayName,
		Vendor:            source.Vendor,
		Product:           source.Product,
		SourceClass:       source.SourceClass,
		RiskClass:         source.RiskClass,
		DataSensitivity:   source.DataSensitivity,
		CollectorModes:    append([]string(nil), source.CollectorModes...),
		ApprovalRequired:  source.ApprovalRequired,
		ParserIDs:         append([]string(nil), source.Parsers...),
		DetectionIDs:      append([]string(nil), source.Detections...),
		SampleIDs:         append([]string(nil), source.Samples...),
		OCSFCategory:      source.Schemas.OCSF.Category,
		OCSFClass:         source.Schemas.OCSF.Class,
		RawRetention:      source.RawRetentionDefault,
		ExpectedEPS:       source.ExpectedVolume.EventsPerSecond,
		ExpectedBytesPS:   source.ExpectedVolume.BytesPerSecond,
		OperationalStatus: contentPackSourceOperationalStatus(record.Status, source),
	}
}

func newContentPackSourceHealthDTO(item tenantSourceHealthEvidence) contentPackSourceHealthDTO {
	return contentPackSourceHealthDTO{
		RuntimeStateID:     item.RuntimeStateID,
		SourceInstanceID:   item.SourceInstanceID,
		CollectorID:        item.CollectorID,
		SourceID:           item.SourceID,
		ReceiverID:         item.ReceiverID,
		NodeID:             item.NodeID,
		PackID:             item.PackID,
		PackVersion:        item.PackVersion,
		ParserID:           item.ParserID,
		CollectorMode:      item.CollectorMode,
		ConfigVersion:      item.ConfigVersion,
		ContentVersion:     item.ContentVersion,
		DisplayName:        item.DisplayName,
		CoverageState:      string(item.State),
		ApprovalRequired:   item.ApprovalRequired,
		ApprovalID:         item.ApprovalID,
		Metrics:            item.Metrics,
		Labels:             cloneStringMapContentPack(item.Labels),
		LastEventAt:        formatContentPackTimePtr(item.LastEventAt),
		LastParsedAt:       formatContentPackTimePtr(item.LastParsedAt),
		LastHealthAt:       formatContentPackTimePtr(item.LastHealthAt),
		LastError:          item.LastError,
		RecommendedActions: contentPackSourceHealthRecommendedActions(item),
	}
}

func newContentPackSourceProposalDTO(row storage.ContentPackSourceProposalRecord) contentPackSourceProposalDTO {
	return contentPackSourceProposalDTO{
		ID:                  row.ID.String(),
		TenantID:            row.TenantID.String(),
		NodeID:              row.NodeID.String(),
		ProposalID:          row.ProposalID,
		Kind:                row.Kind,
		Program:             row.Program,
		SourceID:            row.SourceID,
		CollectorType:       row.CollectorType,
		Formatter:           row.Formatter,
		Status:              row.Status,
		Confidence:          row.Confidence,
		Risk:                row.Risk,
		AutoConnectEligible: row.AutoConnectEligible,
		RequiresApproval:    row.RequiresApproval,
		Reason:              row.Reason,
		Paths:               append([]string(nil), row.Paths...),
		Evidence:            append([]string(nil), row.Evidence...),
		Labels:              cloneStringMapContentPack(row.Labels),
		FirstSeenAt:         formatContentPackTime(row.FirstSeenAt),
		LastSeenAt:          formatContentPackTime(row.LastSeenAt),
		ApprovedBySubject:   row.ApprovedBySubject,
		ApprovedAt:          formatContentPackTimePtr(row.ApprovedAt),
		ApprovalNote:        row.ApprovalNote,
		CollectMode:         row.CollectMode,
		RejectedBySubject:   row.RejectedBySubject,
		RejectedAt:          formatContentPackTimePtr(row.RejectedAt),
		RejectionReason:     row.RejectionReason,
		CreatedAt:           formatContentPackTime(row.CreatedAt),
		UpdatedAt:           formatContentPackTime(row.UpdatedAt),
	}
}

func tenantSourceHealthEvidenceFromRuntimeState(state contentpacks.SourceRuntimeState) tenantSourceHealthEvidence {
	return tenantSourceHealthEvidence{
		SourceInstanceID: state.SourceInstanceID,
		CollectorID:      state.CollectorID,
		SourceID:         state.SourceID,
		NodeID:           state.NodeID,
		PackID:           state.PackID,
		PackVersion:      state.PackVersion,
		ParserID:         state.ParserID,
		CollectorMode:    state.CollectorMode,
		ConfigVersion:    state.ConfigVersion,
		ContentVersion:   state.ContentVersion,
		DisplayName:      state.DisplayName,
		State:            state.CoverageState,
		ApprovalRequired: state.ApprovalRequired,
		ApprovalID:       state.ApprovalID,
		Metrics:          state.Metrics,
		LastEventAt:      state.LastEventAt,
		LastParsedAt:     state.LastParsedAt,
		LastHealthAt:     state.LastHealthAt,
		LastError:        state.LastError,
		Labels:           cloneStringMapContentPack(state.Labels),
	}
}

func contentPackSourceOperationalStatus(status contentpacks.PackStatus, source contentpacks.SourceProfile) string {
	switch status {
	case contentpacks.PackStatusEnabled:
		if source.ApprovalRequired {
			return contentpacks.CoverageApprovalRequired
		}
		return contentpacks.CoverageProposed
	case contentpacks.PackStatusQuarantined:
		return contentpacks.CoverageUnsupported
	case contentpacks.PackStatusDisabled, contentpacks.PackStatusDeprecated:
		return contentpacks.CoverageUnsupported
	default:
		return contentpacks.CoverageDiscovered
	}
}

func contentPackOTelRequestHasSources(req contentPackOTelConfigRenderRequest) bool {
	return len(req.SourceIDs) > 0 || len(req.Sources) > 0 || len(req.SourceProposalIDs) > 0
}

func (s *Server) contentPackOTelRenderSources(ctx context.Context, tenantID uuid.UUID, registry *contentpacks.Registry, req contentPackOTelConfigRenderRequest) ([]contentpacks.OTelCollectorConfigSource, error) {
	if registry == nil {
		return nil, fmt.Errorf("content pack registry is unavailable")
	}
	proposalSelections, proposalApprovalRefs, err := s.contentPackOTelSelectionsFromApprovedSourceProposals(ctx, tenantID, registry, req.SourceProposalIDs)
	if err != nil {
		return nil, err
	}
	selections := make([]contentPackOTelConfigSourceSelection, 0, len(req.SourceIDs)+len(req.Sources)+len(proposalSelections))
	selections = append(selections, proposalSelections...)
	for _, sourceID := range req.SourceIDs {
		selections = append(selections, contentPackOTelConfigSourceSelection{SourceID: sourceID})
	}
	selections = append(selections, req.Sources...)
	out := make([]contentpacks.OTelCollectorConfigSource, 0, len(selections))
	seen := map[string]struct{}{}
	for _, selection := range selections {
		sourceID := strings.TrimSpace(selection.SourceID)
		if sourceID == "" {
			return nil, fmt.Errorf("source_id is required")
		}
		mode := strings.TrimSpace(selection.Mode)
		collectMode := strings.TrimSpace(selection.CollectMode)
		key := sourceID + "\x00" + mode + "\x00" + collectMode
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		resolved, ok := registry.ResolveSource(sourceID)
		if !ok {
			return nil, fmt.Errorf("source %s is not enabled in the active content-pack registry", sourceID)
		}
		out = append(out, contentpacks.OTelCollectorConfigSource{
			Source:      resolved,
			Mode:        mode,
			CollectMode: collectMode,
			ApprovalRef: firstNonEmptyContentPack(strings.TrimSpace(selection.ApprovalRef), proposalApprovalRefs[sourceID]),
		})
	}
	return out, nil
}

func (s *Server) contentPackOTelSelectionsFromApprovedSourceProposals(ctx context.Context, tenantID uuid.UUID, registry *contentpacks.Registry, rawProposalIDs []string) ([]contentPackOTelConfigSourceSelection, map[string]string, error) {
	ids, err := parseContentPackSourceProposalIDs(rawProposalIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, map[string]string{}, nil
	}
	store, ok := s.store.(contentPackSourceProposalLookupStore)
	if !ok || store == nil {
		return nil, nil, fmt.Errorf("content pack source proposal lookup store unavailable")
	}
	proposals, err := store.ListContentPackSourceProposalsByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[uuid.UUID]storage.ContentPackSourceProposalRecord, len(proposals))
	for _, proposal := range proposals {
		byID[proposal.ID] = proposal
	}
	selections := make([]contentPackOTelConfigSourceSelection, 0, len(ids))
	approvalRefs := map[string]string{}
	for _, id := range ids {
		proposal, ok := byID[id]
		if !ok {
			return nil, nil, fmt.Errorf("source proposal %s is not available for this tenant", id)
		}
		if proposal.Status != storage.ContentPackSourceProposalStatusApproved {
			return nil, nil, fmt.Errorf("source proposal %s must be approved before OTel config rendering", id)
		}
		if !storage.ContentPackSourceProposalCollectModeDeploysOTel(proposal.CollectMode) {
			return nil, nil, fmt.Errorf("source proposal %s is approved with collect_mode %q; only collect_raw or collect_parsed approvals can be rendered into collector configs in this build", id, strings.TrimSpace(proposal.CollectMode))
		}
		sourceID, err := contentPackSourceIDForProposal(registry, proposal)
		if err != nil {
			return nil, nil, err
		}
		approvalRef := contentPackProposalApprovalID(proposal)
		selections = append(selections, contentPackOTelConfigSourceSelection{
			SourceID:    sourceID,
			Mode:        contentPackProposalOTelCollectorMode(proposal),
			CollectMode: contentPackProposalOTelCollectMode(proposal),
			ApprovalRef: approvalRef,
		})
		if approvalRef != "" {
			approvalRefs[sourceID] = approvalRef
		}
	}
	return selections, approvalRefs, nil
}

func contentPackSourceIDForProposal(registry *contentpacks.Registry, proposal storage.ContentPackSourceProposalRecord) (string, error) {
	candidates := contentPackSourceProposalCandidates(proposal)
	if len(candidates) == 0 {
		return "", fmt.Errorf("source proposal %s has no content-pack source_id or parser hint", proposal.ID)
	}
	if registry == nil {
		return "", fmt.Errorf("content pack registry is unavailable")
	}
	for _, candidate := range candidates {
		if resolved, ok := registry.ResolveSource(candidate); ok {
			return resolved.Source.SourceID, nil
		}
	}
	matches := contentPackRegistrySourceHintMatches(registry, candidates)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("source proposal %s has no enabled content-pack source match for %s", proposal.ID, strings.Join(candidates, ", "))
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("source proposal %s matches multiple enabled content-pack sources (%s); set content_pack_source_id explicitly", proposal.ID, strings.Join(matches, ", "))
	}
}

func contentPackSourceProposalCandidates(proposal storage.ContentPackSourceProposalRecord) []string {
	labels := proposal.Labels
	values := []string{
		proposal.SourceID,
		labels["content_pack_source_id"],
		labels["control_one.content_pack_source_id"],
		labels["source_id"],
		labels["parser_profile"],
		labels["program"],
		proposal.Program,
	}
	for _, key := range []string{"content_pack_source_aliases", "source_aliases", "parser_profile_aliases"} {
		values = append(values, splitContentPackCSV(labels[key])...)
	}
	return dedupeContentPackStrings(values)
}

func contentPackRegistrySourceHintMatches(registry *contentpacks.Registry, candidates []string) []string {
	candidateSet := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = normalizeContentPackSourceHint(candidate)
		if candidate != "" {
			candidateSet[candidate] = struct{}{}
		}
	}
	if registry == nil || len(candidateSet) == 0 {
		return nil
	}
	matches := map[string]struct{}{}
	for _, record := range registry.List() {
		if record.Status != contentpacks.PackStatus(contentpacks.PackStatusEnabled) {
			continue
		}
		for _, source := range record.Manifest.Sources {
			if contentPackSourceMatchesHints(source, candidateSet) {
				sourceID := strings.TrimSpace(source.SourceID)
				if sourceID != "" {
					matches[sourceID] = struct{}{}
				}
			}
		}
	}
	return sortedContentPackKeys(matches)
}

func contentPackSourceMatchesHints(source contentpacks.SourceProfile, candidates map[string]struct{}) bool {
	sourceID := normalizeContentPackSourceHint(source.SourceID)
	for candidate := range candidates {
		if sourceID == candidate || strings.HasPrefix(sourceID, candidate+".") || strings.HasPrefix(sourceID, candidate+"_") {
			return true
		}
	}
	for _, value := range []string{source.Product, source.Vendor, source.DisplayName} {
		if _, ok := candidates[normalizeContentPackSourceHint(value)]; ok {
			return true
		}
	}
	for _, parserID := range source.Parsers {
		parserID = normalizeContentPackSourceHint(parserID)
		for candidate := range candidates {
			if parserID == candidate || strings.HasPrefix(parserID, candidate+".") || strings.HasPrefix(parserID, candidate+"_") {
				return true
			}
		}
	}
	for _, labels := range []map[string]string{source.Labels, source.Metadata} {
		for _, key := range []string{"program", "parser_profile", "content_pack_source_id", "source_id", "appcatalog_program", "app_catalog_program"} {
			if _, ok := candidates[normalizeContentPackSourceHint(labels[key])]; ok {
				return true
			}
		}
		for _, key := range []string{"aliases", "source_aliases", "parser_profile_aliases", "program_aliases"} {
			for _, alias := range splitContentPackCSV(labels[key]) {
				if _, ok := candidates[normalizeContentPackSourceHint(alias)]; ok {
					return true
				}
			}
		}
	}
	return false
}

func parseContentPackSourceProposalIDs(raw []string) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]struct{}{}
	out := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("invalid source_proposal_id %q", value)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func contentPackProposalOTelCollectorMode(proposal storage.ContentPackSourceProposalRecord) string {
	switch strings.ToLower(strings.TrimSpace(proposal.CollectorType)) {
	case "file", "filelog", "local_file", "local_filelog":
		return contentpacks.CollectorOTelFileLog
	case contentpacks.CollectorSyslog, "syslog_tls":
		return contentpacks.CollectorSyslog
	case contentpacks.CollectorWindowsEvent, "windows_event_log", "eventlog", "wineventlog":
		return contentpacks.CollectorWindowsEvent
	case contentpacks.CollectorOTLP:
		return contentpacks.CollectorOTLP
	case contentpacks.CollectorSplunkHEC:
		return contentpacks.CollectorSplunkHEC
	case contentpacks.CollectorKafka:
		return contentpacks.CollectorKafka
	case contentpacks.CollectorPrometheus:
		return contentpacks.CollectorPrometheus
	default:
		return ""
	}
}

func contentPackProposalOTelCollectMode(proposal storage.ContentPackSourceProposalRecord) string {
	switch strings.ToLower(strings.TrimSpace(proposal.CollectMode)) {
	case "", storage.ContentPackSourceProposalCollectModeCollectRaw:
		return contentpacks.OTelCollectModeCollectRaw
	case storage.ContentPackSourceProposalCollectModeCollectParsed:
		return contentpacks.OTelCollectModeCollectParsed
	default:
		return strings.ToLower(strings.TrimSpace(proposal.CollectMode))
	}
}

func contentPackOTelPlanSourceIDs(plan contentpacks.OTelCollectorConfigPlan) []string {
	seen := map[string]struct{}{}
	for _, source := range plan.Sources {
		sourceID := strings.TrimSpace(source.SourceID)
		if sourceID != "" {
			seen[sourceID] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for sourceID := range seen {
		out = append(out, sourceID)
	}
	sort.Strings(out)
	return out
}

func newContentPackOTelConfigCandidateDTO(row storage.ContentPackCollectorConfigCandidate) contentPackOTelConfigCandidateDTO {
	dto := contentPackOTelConfigCandidateDTO{
		ID:                    row.ID.String(),
		TenantID:              row.TenantID.String(),
		Status:                row.Status,
		ConfigVersion:         row.ConfigVersion,
		CollectorID:           row.CollectorID,
		Endpoint:              row.Endpoint,
		SourceIDs:             append([]string(nil), row.SourceIDs...),
		CreatedBySubject:      row.CreatedBySubject,
		ApprovedBySubject:     row.ApprovedBySubject,
		ApprovalNote:          row.ApprovalNote,
		ReviewedConfigVersion: row.ReviewedConfigVersion,
		ReviewedYAMLSHA256:    row.ReviewedYAMLSHA256,
		ApprovedAt:            formatContentPackTimePtr(row.ApprovedAt),
		QueuedBySubject:       row.QueuedBySubject,
		QueueNote:             row.QueueNote,
		TargetCollectorID:     row.TargetCollectorID,
		QueuedAt:              formatContentPackTimePtr(row.QueuedAt),
		DeployedAt:            formatContentPackTimePtr(row.DeployedAt),
		FailedAt:              formatContentPackTimePtr(row.FailedAt),
		DeploymentError:       row.DeploymentError,
		CreatedAt:             formatContentPackTime(row.CreatedAt),
		UpdatedAt:             formatContentPackTime(row.UpdatedAt),
	}
	if row.RegistrySnapshotID != uuid.Nil {
		dto.RegistrySnapshotID = row.RegistrySnapshotID.String()
	}
	return dto
}

func newContentPackOTelConfigCandidateDetailDTO(row storage.ContentPackCollectorConfigCandidate) contentPackOTelConfigCandidateDetailDTO {
	return contentPackOTelConfigCandidateDetailDTO{
		contentPackOTelConfigCandidateDTO: newContentPackOTelConfigCandidateDTO(row),
		Sources:                           append([]contentpacks.OTelCollectorSourcePlan(nil), row.Plan.Sources...),
		Warnings:                          append([]string(nil), row.Plan.Warnings...),
		YAML:                              row.RenderedYAML,
	}
}

func newContentPackEdgeCollectorDTO(row storage.ContentPackEdgeCollector) contentPackEdgeCollectorDTO {
	return contentPackEdgeCollectorDTO{
		ID:                   row.ID.String(),
		TenantID:             row.TenantID.String(),
		CollectorID:          row.CollectorID,
		Kind:                 row.Kind,
		DisplayName:          row.DisplayName,
		Endpoint:             row.Endpoint,
		Version:              row.Version,
		Status:               row.Status,
		DesiredConfigVersion: row.DesiredConfigVersion,
		RunningConfigVersion: row.RunningConfigVersion,
		TokenLastFour:        row.TokenLastFour,
		TokenIssuedAt:        formatContentPackTimePtr(row.TokenIssuedAt),
		Health:               row.Health,
		LastError:            row.LastError,
		LastHeartbeatAt:      formatContentPackTimePtr(row.LastHeartbeatAt),
		CreatedAt:            formatContentPackTime(row.CreatedAt),
		UpdatedAt:            formatContentPackTime(row.UpdatedAt),
	}
}

func formatContentPackTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatContentPackTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatContentPackTime(*t)
}

func firstNonEmptyContentPack(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func dedupeContentPackStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func splitContentPackCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeContentPackSourceHint(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortedContentPackKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func cloneStringMapContentPack(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func (s *Server) syncOfflineContentPacks(ctx context.Context, tenantID uuid.UUID, receipt *offlinebundle.Receipt) {
	if receipt == nil || !offlineBundleHasSIEMContentPack(receipt.Contents) {
		return
	}
	root := s.offlineContentRootDir()
	if root == "" {
		s.logger.Warn("sync offline content packs skipped: offline content root unavailable")
		return
	}
	store, ok := s.store.(contentPackRegistrySnapshotStore)
	if !ok || store == nil {
		s.logger.Warn("sync offline content packs skipped: snapshot store unavailable")
		return
	}

	registry := contentpacks.NewRegistry(controlOneContentPackRuntimeVersion)
	active, err := store.ActiveContentPackRegistrySnapshot(ctx, tenantID)
	if err != nil {
		s.logger.Warn("load active content pack registry snapshot", zap.Error(err))
		return
	}
	if active != nil {
		restored, err := contentpacks.NewRegistryFromSnapshot(active.Snapshot, controlOneContentPackRuntimeVersion)
		if err != nil {
			s.logger.Warn("restore active content pack registry snapshot", zap.Error(err))
			return
		}
		registry = restored
	}

	results, err := offlinebundle.SyncActiveContentPacksToRegistry(ctx, root, registry, contentpacks.SampleReplayOptions{}, receipt.ImportedAt)
	if err != nil {
		s.logger.Warn("sync active offline content packs to registry", zap.Error(err))
		return
	}
	if len(results) == 0 {
		return
	}
	snapshot := registry.Snapshot(receipt.ImportedAt)
	saved, err := store.SaveContentPackRegistrySnapshot(ctx, storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   fmt.Sprintf("offline_bundle:%s:%d", strings.TrimSpace(receipt.BundleID), receipt.Sequence),
		Snapshot: snapshot,
	})
	if err != nil {
		s.logger.Warn("save content pack registry snapshot", zap.Error(err))
		return
	}
	s.persistActiveContentPackDetectionArtifactsForSnapshot(ctx, tenantID, saved.ID, saved.Snapshot)
	s.logger.Info("synced offline content packs",
		zap.String("bundle_id", receipt.BundleID),
		zap.Int64("bundle_sequence", receipt.Sequence),
		zap.Int("packs", len(results)),
		zap.Int("registry_packs", len(snapshot.Packs)),
	)
}

func offlineBundleHasSIEMContentPack(contents []offlinebundle.ContentReceipt) bool {
	for _, content := range contents {
		if strings.EqualFold(strings.TrimSpace(content.Type), offlinebundle.ContentTypeSIEMContentPack) {
			return true
		}
	}
	return false
}
