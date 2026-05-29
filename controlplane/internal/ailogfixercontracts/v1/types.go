package v1

import (
	"errors"
	"strings"
	"time"
)

const (
	ContractVersion               = "v1"
	DiagnosisSchemaURL            = "https://github.com/CloudSpaceLab/ai-logfixer/contracts/v1/schemas/diagnosis-result.schema.json"
	InvestigationRequestSchemaURL = "https://github.com/CloudSpaceLab/ai-logfixer/contracts/v1/schemas/investigation-request.schema.json"
	RemediationPlanSchemaURL      = "https://github.com/CloudSpaceLab/ai-logfixer/contracts/v1/schemas/remediation-plan.schema.json"
)

type DiagnosisStatus string

const (
	DiagnosisStatusNeedsMoreData DiagnosisStatus = "needs_more_data"
)

type EvidenceType string

const (
	EvidenceTypeLog EvidenceType = "log"
	EvidenceTypeDB  EvidenceType = "db"
)

type RedactionState string

const (
	RedactionStateNotNeeded RedactionState = "not_needed"
)

type SafetyClassification string

const (
	SafetyReadOnly SafetyClassification = "read_only"
)

type SourceType string

const (
	SourceTypeIntegration SourceType = "integration"
)

type RemediationStatus string

const (
	RemediationStatusPlanning         RemediationStatus = "planning"
	RemediationStatusAwaitingApproval RemediationStatus = "awaiting_approval"
	RemediationStatusSucceeded        RemediationStatus = "succeeded"
	RemediationStatusFailed           RemediationStatus = "failed"
	RemediationStatusRolledBack       RemediationStatus = "rolled_back"
)

type RollbackType string

const (
	RollbackUnavailable RollbackType = "unavailable"
)

type TimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type SignalFingerprint struct {
	Service   string   `json:"service"`
	Symptom   string   `json:"symptom"`
	ErrorCode string   `json:"error_code"`
	Source    string   `json:"source"`
	Tags      []string `json:"tags"`
}

type ExternalRef struct {
	System   string            `json:"system"`
	Type     string            `json:"type"`
	ID       string            `json:"id"`
	URL      string            `json:"url,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type KnowledgeRef struct {
	GraphID      string  `json:"graph_id,omitempty"`
	NodeID       string  `json:"node_id,omitempty"`
	NodeType     string  `json:"node_type,omitempty"`
	Relationship string  `json:"relationship,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	Source       string  `json:"source,omitempty"`
}

type NextAction struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	ActionType  string `json:"action_type"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type TimelineEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Severity  string    `json:"severity,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type UIHints struct {
	Icon     string   `json:"icon,omitempty"`
	Tone     string   `json:"tone,omitempty"`
	Sections []string `json:"sections,omitempty"`
}

type EvidenceItem struct {
	ID             string         `json:"id"`
	Type           EvidenceType   `json:"type"`
	Source         string         `json:"source"`
	Timestamp      time.Time      `json:"timestamp"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary,omitempty"`
	RawExcerpt     string         `json:"raw_excerpt,omitempty"`
	RedactionState RedactionState `json:"redaction_state"`
	RelatedIDs     []string       `json:"related_ids,omitempty"`
	UIHints        UIHints        `json:"ui_hints,omitempty"`
	ExternalRefs   []ExternalRef  `json:"external_refs,omitempty"`
	KnowledgeRefs  []KnowledgeRef `json:"knowledge_refs,omitempty"`
}

type RunbookRecommendation struct {
	ID                  string               `json:"id"`
	Title               string               `json:"title"`
	Reason              string               `json:"reason"`
	Confidence          float64              `json:"confidence"`
	Steps               []string             `json:"steps,omitempty"`
	RequiredPermissions []string             `json:"required_permissions,omitempty"`
	EstimatedRisk       SafetyClassification `json:"estimated_risk"`
	RequiresApproval    bool                 `json:"requires_approval"`
}

type DiffPreview struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type RollbackPlan struct {
	ID                   string               `json:"id"`
	RollbackType         RollbackType         `json:"rollback_type"`
	SnapshotRefs         []string             `json:"snapshot_refs,omitempty"`
	RestoreSteps         []string             `json:"restore_steps,omitempty"`
	Limitations          []string             `json:"limitations,omitempty"`
	RiskLevel            SafetyClassification `json:"risk_level"`
	RequiresManualReview bool                 `json:"requires_manual_review,omitempty"`
}

type DiagnosisResult struct {
	ID                   string                  `json:"id"`
	ContractVersion      string                  `json:"contract_version"`
	SchemaURL            string                  `json:"schema_url"`
	Status               DiagnosisStatus         `json:"status"`
	Summary              string                  `json:"summary"`
	Confidence           float64                 `json:"confidence"`
	SuspectedRootCause   string                  `json:"suspected_root_cause,omitempty"`
	AffectedServices     []string                `json:"affected_services,omitempty"`
	EvidenceItems        []EvidenceItem          `json:"evidence_items,omitempty"`
	Recommendations      []RunbookRecommendation `json:"recommendations,omitempty"`
	PatchPlan            *PatchPlan              `json:"patch_plan,omitempty"`
	RollbackPlan         *RollbackPlan           `json:"rollback_plan,omitempty"`
	SafetyClassification SafetyClassification    `json:"safety_classification"`
	DisplayStatus        string                  `json:"display_status,omitempty"`
	UserMessage          string                  `json:"user_message,omitempty"`
	NextActions          []NextAction            `json:"next_actions,omitempty"`
	TimelineEvents       []TimelineEvent         `json:"timeline_events,omitempty"`
	ExternalRefs         []ExternalRef           `json:"external_refs,omitempty"`
	KnowledgeRefs        []KnowledgeRef          `json:"knowledge_refs,omitempty"`
	CreatedAt            time.Time               `json:"created_at"`
}

type PatchPlan struct {
	ID               string               `json:"id"`
	TargetType       string               `json:"target_type,omitempty"`
	TargetRefs       []string             `json:"target_refs,omitempty"`
	DiffPreview      DiffPreview          `json:"diff_preview,omitempty"`
	RiskLevel        SafetyClassification `json:"risk_level,omitempty"`
	RequiresApproval bool                 `json:"requires_approval,omitempty"`
	BlockedReasons   []string             `json:"blocked_reasons,omitempty"`
}

type RemediationPlan struct {
	ID                string               `json:"id"`
	ContractVersion   string               `json:"contract_version"`
	SchemaURL         string               `json:"schema_url"`
	DiagnosisResultID string               `json:"diagnosis_result_id"`
	Summary           string               `json:"summary"`
	FixPreview        DiffPreview          `json:"fix_preview"`
	RollbackPlan      RollbackPlan         `json:"rollback_plan"`
	RiskLevel         SafetyClassification `json:"risk_level"`
	ApprovalRequired  bool                 `json:"approval_required"`
	Status            RemediationStatus    `json:"status"`
	DisplayStatus     string               `json:"display_status,omitempty"`
	UserMessage       string               `json:"user_message,omitempty"`
	NextActions       []NextAction         `json:"next_actions,omitempty"`
	TimelineEvents    []TimelineEvent      `json:"timeline_events,omitempty"`
	ExternalRefs      []ExternalRef        `json:"external_refs,omitempty"`
	KnowledgeRefs     []KnowledgeRef       `json:"knowledge_refs,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
}

type InvestigationRequest struct {
	ID                string            `json:"id"`
	ContractVersion   string            `json:"contract_version"`
	SchemaURL         string            `json:"schema_url"`
	SourceType        SourceType        `json:"source_type"`
	SourceName        string            `json:"source_name"`
	RequestedBy       string            `json:"requested_by"`
	Service           string            `json:"service"`
	Symptom           string            `json:"symptom"`
	ErrorCode         string            `json:"error_code,omitempty"`
	TimeWindow        TimeWindow        `json:"time_window"`
	SignalFingerprint SignalFingerprint `json:"signal_fingerprint"`
	DisplayStatus     string            `json:"display_status,omitempty"`
	UserMessage       string            `json:"user_message,omitempty"`
	ExternalRefs      []ExternalRef     `json:"external_refs,omitempty"`
	KnowledgeRefs     []KnowledgeRef    `json:"knowledge_refs,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
}

func (d DiagnosisResult) Validate() error {
	return validateContract(d.ID, d.ContractVersion, d.SchemaURL)
}

func (p RemediationPlan) Validate() error {
	return validateContract(p.ID, p.ContractVersion, p.SchemaURL)
}

func (r InvestigationRequest) Validate() error {
	return validateContract(r.ID, r.ContractVersion, r.SchemaURL)
}

func validateContract(id, version, schemaURL string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(version) != ContractVersion {
		return errors.New("unsupported contract_version")
	}
	if strings.TrimSpace(schemaURL) == "" {
		return errors.New("schema_url is required")
	}
	return nil
}
