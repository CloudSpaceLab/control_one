package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	contractsv1 "github.com/CloudSpaceLab/control_one/controlplane/internal/ailogfixercontracts/v1"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	JobTypeAILogFixerPlan     = "ailogfixer.plan"
	JobTypeAILogFixerApply    = "ailogfixer.apply"
	JobTypeAILogFixerRollback = "ailogfixer.rollback"

	aiLogFixerJobContractVersion       = "ailogfixer.jobs.v1"
	agentCapabilityAILogFixerRemediate = "ailogfixer_remediation.v1"
)

type aiLogFixerRunStore interface {
	GetAILogFixerRunByDedupKey(context.Context, uuid.UUID, string) (*storage.AILogFixerRun, error)
	CreateAILogFixerRun(context.Context, storage.CreateAILogFixerRunParams) (*storage.AILogFixerRun, error)
}

type aiLogFixerAPIStore interface {
	ListAILogFixerRuns(context.Context, storage.ListAILogFixerRunsFilter, int, int) ([]storage.AILogFixerRun, int, error)
}

type aiLogFixerActionStore interface {
	ListPendingAILogFixerActions(context.Context, uuid.UUID) ([]storage.AILogFixerAction, error)
	GetAILogFixerActionByJobID(context.Context, uuid.UUID) (*storage.AILogFixerAction, error)
	MarkAILogFixerActionStatus(context.Context, uuid.UUID, string, map[string]any, string) error
	MarkAILogFixerRunStatus(context.Context, uuid.UUID, string, map[string]any, map[string]any) error
}

type aiLogFixerActionCreateStore interface {
	CreateAILogFixerAction(context.Context, storage.CreateAILogFixerActionParams) (*storage.AILogFixerAction, error)
}

type aiLogFixerJobPayload struct {
	ContractVersion      string          `json:"contract_version,omitempty"`
	IdempotencyKey       string          `json:"idempotency_key,omitempty"`
	CorrelationID        string          `json:"correlation_id,omitempty"`
	TenantID             string          `json:"tenant_id"`
	NodeID               string          `json:"node_id"`
	RunID                string          `json:"run_id,omitempty"`
	ServiceKey           string          `json:"service_key"`
	Action               string          `json:"action"`
	Policy               map[string]any  `json:"policy,omitempty"`
	InvestigationRequest json.RawMessage `json:"investigation_request,omitempty"`
	Diagnosis            json.RawMessage `json:"diagnosis,omitempty"`
	RemediationPlan      json.RawMessage `json:"remediation_plan,omitempty"`
}

type aiLogFixerTriggerBundle struct {
	Event                IngestedEvent
	DedupKey             string
	ServiceKey           string
	Severity             string
	Summary              string
	Request              contractsv1.InvestigationRequest
	Diagnosis            contractsv1.DiagnosisResult
	RemediationPlan      contractsv1.RemediationPlan
	Evidence             map[string]any
	ProposalAction       string
	ProposalReason       string
	ProposalApprovalKind string
	ProposalApprovalPath string
}

type aiLogFixerRunResponse struct {
	ID                   string          `json:"id"`
	TenantID             string          `json:"tenant_id"`
	NodeID               string          `json:"node_id,omitempty"`
	InvestigationID      string          `json:"investigation_id,omitempty"`
	ProposalID           string          `json:"proposal_id,omitempty"`
	JobID                string          `json:"job_id,omitempty"`
	ApprovalID           string          `json:"approval_id,omitempty"`
	TriggerEventType     string          `json:"trigger_event_type"`
	TriggerDedupKey      string          `json:"trigger_dedup_key"`
	ServiceKey           string          `json:"service_key"`
	Status               string          `json:"status"`
	RiskLevel            string          `json:"risk_level"`
	InvestigationRequest json.RawMessage `json:"investigation_request,omitempty"`
	Diagnosis            json.RawMessage `json:"diagnosis,omitempty"`
	RemediationPlan      json.RawMessage `json:"remediation_plan,omitempty"`
	Attempt              json.RawMessage `json:"attempt,omitempty"`
	Receipt              json.RawMessage `json:"receipt,omitempty"`
	Evidence             json.RawMessage `json:"evidence,omitempty"`
	CreatedAt            string          `json:"created_at"`
	UpdatedAt            string          `json:"updated_at"`
}

func aiLogFixerRunResponseFromModel(run storage.AILogFixerRun) aiLogFixerRunResponse {
	resp := aiLogFixerRunResponse{
		ID:                   run.ID.String(),
		TenantID:             run.TenantID.String(),
		TriggerEventType:     run.TriggerEventType,
		TriggerDedupKey:      run.TriggerDedupKey,
		ServiceKey:           run.ServiceKey,
		Status:               run.Status,
		RiskLevel:            run.RiskLevel,
		InvestigationRequest: run.InvestigationRequest,
		Diagnosis:            run.Diagnosis,
		RemediationPlan:      run.RemediationPlan,
		Attempt:              run.Attempt,
		Receipt:              run.Receipt,
		Evidence:             run.Evidence,
		CreatedAt:            run.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            run.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if run.NodeID != uuid.Nil {
		resp.NodeID = run.NodeID.String()
	}
	if run.InvestigationID != uuid.Nil {
		resp.InvestigationID = run.InvestigationID.String()
	}
	if run.ProposalID != uuid.Nil {
		resp.ProposalID = run.ProposalID.String()
	}
	if run.JobID != uuid.Nil {
		resp.JobID = run.JobID.String()
	}
	if run.ApprovalID != uuid.Nil {
		resp.ApprovalID = run.ApprovalID.String()
	}
	return resp
}

func (s *Server) aiLogFixerRunBackend() aiLogFixerRunStore {
	if s == nil || s.store == nil {
		return nil
	}
	if backend, ok := s.store.(aiLogFixerRunStore); ok {
		return backend
	}
	return nil
}

func (s *Server) handleAILogFixerRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(aiLogFixerAPIStore)
	if !ok {
		http.Error(w, "ai logfixer run store unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.ListAILogFixerRunsFilter{
		TenantID: tenantID,
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
	}
	if rawNode := strings.TrimSpace(r.URL.Query().Get("node_id")); rawNode != "" {
		nodeID, err := uuid.Parse(rawNode)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = nodeID
	}
	runs, total, err := store.ListAILogFixerRuns(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list ai logfixer runs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]aiLogFixerRunResponse, 0, len(runs))
	for i := range runs {
		resp = append(resp, aiLogFixerRunResponseFromModel(runs[i]))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[aiLogFixerRunResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	})
}

func (s *Server) persistAILogFixerTriggers(ctx context.Context, tenantID, fallbackNodeID uuid.UUID, events []IngestedEvent) {
	operatorBackend := s.aiOperatorBackend()
	runBackend := s.aiLogFixerRunBackend()
	if operatorBackend == nil || runBackend == nil || len(events) == 0 {
		return
	}
	for i := range events {
		ev := events[i]
		bundle, ok := buildAILogFixerTriggerBundle(tenantID, fallbackNodeID, ev)
		if !ok {
			continue
		}
		if existing, err := runBackend.GetAILogFixerRunByDedupKey(ctx, tenantID, bundle.DedupKey); err != nil {
			s.logger.Warn("lookup ai logfixer run", zap.Error(err), zap.String("dedup_key", bundle.DedupKey))
			continue
		} else if existing != nil {
			continue
		}

		evidenceBytes, err := json.Marshal(bundle.Evidence)
		if err != nil {
			s.logger.Warn("marshal ai logfixer evidence", zap.Error(err), zap.String("event_type", ev.Type))
			continue
		}
		investigation, err := operatorBackend.CreateAIInvestigation(ctx, storage.CreateAIInvestigationParams{
			TenantID:         tenantID,
			NodeID:           effectiveEventNodeID(fallbackNodeID, ev),
			TriggerType:      "ai_logfixer",
			TriggerEventType: ev.Type,
			TriggerDedupKey:  bundle.DedupKey,
			Severity:         bundle.Severity,
			Summary:          bundle.Summary,
			Evidence:         evidenceBytes,
			Status:           storage.AIInvestigationStatusOpen,
		})
		if err != nil {
			s.logger.Warn("persist ai logfixer investigation", zap.Error(err), zap.String("event_type", ev.Type), zap.String("dedup_key", bundle.DedupKey))
			continue
		}

		proposalMetadata := map[string]any{
			"source":                "ai_logfixer_event_bridge",
			"trigger_event_type":    ev.Type,
			"trigger_dedup_key":     bundle.DedupKey,
			"service_key":           bundle.ServiceKey,
			"risk_level":            string(bundle.RemediationPlan.RiskLevel),
			"contract_version":      contractsv1.ContractVersion,
			"investigation_request": bundle.Request,
			"diagnosis":             bundle.Diagnosis,
			"remediation_plan":      bundle.RemediationPlan,
			"evidence":              bundle.Evidence,
		}
		proposalBytes, err := json.Marshal(proposalMetadata)
		if err != nil {
			s.logger.Warn("marshal ai logfixer proposal metadata", zap.Error(err), zap.String("dedup_key", bundle.DedupKey))
			continue
		}
		proposal, err := operatorBackend.CreateAIOperatorProposal(ctx, storage.CreateAIOperatorProposalParams{
			TenantID:     tenantID,
			NodeID:       effectiveEventNodeID(fallbackNodeID, ev),
			Action:       bundle.ProposalAction,
			Reason:       bundle.ProposalReason,
			Status:       storage.AIOperatorProposalStatusProposed,
			DryRun:       true,
			ApprovalKind: bundle.ProposalApprovalKind,
			ApprovalPath: bundle.ProposalApprovalPath,
			SourceTool:   "ai_logfixer_event_bridge",
			Metadata:     proposalBytes,
		})
		if err != nil {
			s.logger.Warn("persist ai logfixer proposal", zap.Error(err), zap.String("dedup_key", bundle.DedupKey))
			continue
		}

		run, err := runBackend.CreateAILogFixerRun(ctx, storage.CreateAILogFixerRunParams{
			TenantID:             tenantID,
			NodeID:               effectiveEventNodeID(fallbackNodeID, ev),
			InvestigationID:      investigation.ID,
			ProposalID:           proposal.ID,
			TriggerEventType:     ev.Type,
			TriggerDedupKey:      bundle.DedupKey,
			ServiceKey:           bundle.ServiceKey,
			Status:               string(bundle.RemediationPlan.Status),
			RiskLevel:            string(bundle.RemediationPlan.RiskLevel),
			InvestigationRequest: mustMarshalJSON(bundle.Request),
			Diagnosis:            mustMarshalJSON(bundle.Diagnosis),
			RemediationPlan:      mustMarshalJSON(bundle.RemediationPlan),
			Evidence:             evidenceBytes,
		})
		if err != nil {
			s.logger.Warn("persist ai logfixer run", zap.Error(err), zap.String("dedup_key", bundle.DedupKey))
			continue
		}
		s.recordAudit(ctx, s.systemActor(), tenantID, "ai_logfixer.run.proposed", "ai_logfixer_run", run.ID.String(), map[string]any{
			"node_id":          effectiveEventNodeID(fallbackNodeID, ev).String(),
			"investigation_id": investigation.ID.String(),
			"proposal_id":      proposal.ID.String(),
			"event_type":       ev.Type,
			"service_key":      bundle.ServiceKey,
			"risk_level":       string(bundle.RemediationPlan.RiskLevel),
		})
	}
}

func buildAILogFixerTriggerBundle(tenantID, fallbackNodeID uuid.UUID, ev IngestedEvent) (aiLogFixerTriggerBundle, bool) {
	now := ev.TS
	if now.IsZero() {
		now = time.Now().UTC()
	}
	serviceKey := aiLogFixerServiceKey(ev)
	shouldTrigger, symptom, errorCode := aiLogFixerTriggerReason(ev)
	if !shouldTrigger {
		return aiLogFixerTriggerBundle{}, false
	}
	sourceID := firstNonEmpty(ev.EventID, ev.DedupKey, ev.CorrelationID, hashString(ev.Type+":"+ev.Message))
	path := firstNonEmpty(detailsString(ev.Details, "path_template", ""), detailsString(ev.Details, "path", ""))
	dedupParts := []string{
		"c1:ailogfixer:v1",
		tenantID.String(),
		effectiveEventNodeID(fallbackNodeID, ev).String(),
		ev.Type,
		serviceKey,
		path,
		errorCode,
	}
	dedupKey := strings.Join(dedupParts, ":")
	requestID := "ailf_req_" + hashString(dedupKey)[:24]
	diagnosisID := "ailf_diag_" + hashString(dedupKey + ":diagnosis")[:24]
	planID := "ailf_plan_" + hashString(dedupKey + ":plan")[:24]
	externalRefs := []contractsv1.ExternalRef{
		{
			System: "control_one",
			Type:   "event",
			ID:     sourceID,
			Metadata: map[string]string{
				"event_type":     ev.Type,
				"dedup_key":      ev.DedupKey,
				"correlation_id": ev.CorrelationID,
			},
		},
	}
	evidenceType := contractsv1.EvidenceTypeLog
	if strings.HasPrefix(ev.Type, "db.query") {
		evidenceType = contractsv1.EvidenceTypeDB
	}
	evidenceTitle := "Control One " + ev.Type
	if serviceKey != "" {
		evidenceTitle += " for " + serviceKey
	}
	summary := firstNonEmpty(ev.Message, symptom)
	if summary == "" {
		summary = ev.Type
	}
	diagnosis := contractsv1.DiagnosisResult{
		ID:                   diagnosisID,
		ContractVersion:      contractsv1.ContractVersion,
		SchemaURL:            contractsv1.DiagnosisSchemaURL,
		Status:               contractsv1.DiagnosisStatusNeedsMoreData,
		Summary:              "Control One detected " + strings.ToLower(symptom) + ".",
		Confidence:           0.45,
		SuspectedRootCause:   "Requires AI LogFixer dry-run planning on the affected service before a root cause is asserted.",
		AffectedServices:     []string{serviceKey},
		EvidenceItems:        []contractsv1.EvidenceItem{{ID: "evidence_" + hashString(sourceID)[:16], Type: evidenceType, Source: "control_one", Timestamp: now, Title: evidenceTitle, Summary: summary, RawExcerpt: truncString(ev.Message, 512), RedactionState: contractsv1.RedactionStateNotNeeded, RelatedIDs: []string{sourceID}, ExternalRefs: externalRefs}},
		Recommendations:      []contractsv1.RunbookRecommendation{{ID: "rec_run_dry_plan", Title: "Run AI LogFixer dry-run planning", Reason: "The event is actionable, but Control One v1 keeps AI LogFixer remediation proposal-only until an operator approves node-local dry-run planning.", Confidence: 0.7, Steps: []string{"Review the cited Control One event.", "Approve an AI LogFixer dry-run plan for the mapped service.", "Inspect the returned plan and receipts before any apply action."}, RequiredPermissions: []string{"control_one.operator"}, EstimatedRisk: contractsv1.SafetyReadOnly, RequiresApproval: false}},
		SafetyClassification: contractsv1.SafetyReadOnly,
		DisplayStatus:        "Ready for AI LogFixer planning",
		UserMessage:          "Control One found evidence that AI LogFixer can investigate in dry-run mode.",
		NextActions:          []contractsv1.NextAction{{ID: "next_approve_dry_run", Label: "Approve dry-run plan", ActionType: JobTypeAILogFixerPlan, Description: "Dispatch a node-local AI LogFixer dry-run planning action.", Enabled: true}},
		TimelineEvents:       []contractsv1.TimelineEvent{{ID: "tl_detected_" + hashString(sourceID)[:12], Type: "control_one.event_detected", Message: summary, Severity: firstNonEmpty(ev.Severity, "warning"), Timestamp: now}},
		ExternalRefs:         externalRefs,
		CreatedAt:            now,
	}
	plan := contractsv1.RemediationPlan{
		ID:                planID,
		ContractVersion:   contractsv1.ContractVersion,
		SchemaURL:         contractsv1.RemediationPlanSchemaURL,
		DiagnosisResultID: diagnosisID,
		Summary:           "Prepare an AI LogFixer dry-run plan for " + serviceKey + ".",
		FixPreview:        contractsv1.DiffPreview{Before: "No node-local AI LogFixer plan has run.", After: "AI LogFixer returns diagnosis, proposed changes, risk, and evidence receipts without applying changes."},
		RollbackPlan:      contractsv1.RollbackPlan{ID: "rollback_" + hashString(planID)[:16], RollbackType: contractsv1.RollbackUnavailable, Limitations: []string{"No mutation is performed during proposal-only or dry-run planning."}, RiskLevel: contractsv1.SafetyReadOnly},
		RiskLevel:         contractsv1.SafetyReadOnly,
		ApprovalRequired:  false,
		Status:            contractsv1.RemediationStatusAwaitingApproval,
		DisplayStatus:     "Awaiting dry-run approval",
		UserMessage:       "Approve a dry-run plan before Control One dispatches AI LogFixer to the node.",
		NextActions:       []contractsv1.NextAction{{ID: "next_run_plan", Label: "Run dry-run plan", ActionType: JobTypeAILogFixerPlan, Description: "Send AI LogFixer a node-local planning job.", Enabled: true}},
		TimelineEvents:    []contractsv1.TimelineEvent{{ID: "tl_plan_" + hashString(planID)[:12], Type: "remediation.plan_created", Message: "AI LogFixer dry-run planning proposal created.", Severity: "info", Timestamp: now}},
		ExternalRefs:      externalRefs,
		CreatedAt:         now,
	}
	if err := diagnosis.Validate(); err != nil {
		diagnosis.UserMessage += " Contract validation warning: " + err.Error()
	}
	if err := plan.Validate(); err != nil {
		plan.UserMessage += " Contract validation warning: " + err.Error()
	}
	request := contractsv1.InvestigationRequest{
		ID:              requestID,
		ContractVersion: contractsv1.ContractVersion,
		SchemaURL:       contractsv1.InvestigationRequestSchemaURL,
		SourceType:      contractsv1.SourceTypeIntegration,
		SourceName:      "control-one-observability",
		RequestedBy:     "control-one",
		Service:         serviceKey,
		Symptom:         symptom,
		ErrorCode:       errorCode,
		TimeWindow:      contractsv1.TimeWindow{Start: now.Add(-5 * time.Minute), End: now.Add(5 * time.Minute)},
		SignalFingerprint: contractsv1.SignalFingerprint{
			Service:   serviceKey,
			Symptom:   symptom,
			ErrorCode: errorCode,
			Source:    ev.Type,
			Tags:      aiLogFixerEventTags(ev),
		},
		DisplayStatus: "Queued by Control One",
		UserMessage:   "Control One converted observability evidence into an AI LogFixer investigation request.",
		ExternalRefs:  externalRefs,
		CreatedAt:     now,
	}
	if err := request.Validate(); err != nil {
		request.UserMessage += " Contract validation warning: " + err.Error()
	}
	approvalKind, approvalPath := approvalRouteForOperatorAction(JobTypeAILogFixerPlan)
	return aiLogFixerTriggerBundle{
		Event:                ev,
		DedupKey:             dedupKey,
		ServiceKey:           serviceKey,
		Severity:             firstNonEmpty(ev.Severity, "warning"),
		Summary:              "AI LogFixer investigation proposed for " + serviceKey + ": " + symptom,
		Request:              request,
		Diagnosis:            diagnosis,
		RemediationPlan:      plan,
		Evidence:             aiLogFixerEvidenceEnvelope(ev, serviceKey, dedupKey),
		ProposalAction:       JobTypeAILogFixerPlan + ":" + serviceKey,
		ProposalReason:       "Run AI LogFixer dry-run planning for " + serviceKey + " based on " + ev.Type + " evidence.",
		ProposalApprovalKind: approvalKind,
		ProposalApprovalPath: approvalPath,
	}, true
}

func aiLogFixerTriggerReason(ev IngestedEvent) (bool, string, string) {
	switch ev.Type {
	case "web.request":
		statusCode := int(detailsInt(ev.Details, "status_code"))
		if statusCode == 0 {
			statusCode = int(detailsInt(ev.Details, "status"))
		}
		statusFamily := detailsString(ev.Details, "status_family", "")
		if statusCode >= 500 || strings.EqualFold(statusFamily, "5xx") {
			return true, "Repeated HTTP 5xx responses detected", firstNonEmpty(fmt.Sprint(statusCode), statusFamily, "5xx")
		}
	case "web.error":
		return true, "Web application error detected", firstNonEmpty(detailsString(ev.Details, "error_code", ""), "web.error")
	case "log.spike":
		return true, "Log spike detected", firstNonEmpty(detailsString(ev.Details, "signature", ""), "log.spike")
	case "db.query.long_running":
		return true, "Long-running database query detected", firstNonEmpty(detailsString(ev.Details, "query_hash", ""), "db.query.long_running")
	case "log.line":
		severity := strings.ToLower(strings.TrimSpace(ev.Severity))
		message := strings.ToLower(strings.TrimSpace(ev.Message))
		if severity == "error" || severity == "critical" || strings.Contains(message, "panic") || strings.Contains(message, "exception") || strings.Contains(message, "fatal") {
			return true, "Application error log line detected", firstNonEmpty(detailsString(ev.Details, "signature", ""), detailsString(ev.Details, "error_code", ""), "log.line")
		}
	}
	return false, "", ""
}

func aiLogFixerServiceKey(ev IngestedEvent) string {
	candidates := []string{
		detailsString(ev.Details, "service", ""),
		detailsString(ev.Details, "service_name", ""),
		detailsString(ev.Details, "app", ""),
		detailsString(ev.Details, "application", ""),
		detailsString(ev.Details, "vhost", ""),
		detailsString(ev.Details, "target", ""),
		detailsString(ev.Details, "database_name", ""),
		ev.ProcessName,
		"unknown-service",
	}
	base := firstNonEmpty(candidates...)
	env := firstNonEmpty(detailsString(ev.Details, "environment", ""), detailsString(ev.Details, "env", ""))
	serverGroup := detailsString(ev.Details, "server_group", "")
	parts := []string{base}
	if env != "" {
		parts = append(parts, env)
	}
	if serverGroup != "" {
		parts = append(parts, serverGroup)
	}
	return sanitizeServiceKey(strings.Join(parts, "/"))
}

func sanitizeServiceKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown-service"
	}
	replacer := strings.NewReplacer(" ", "-", "\\", "-", "/", "-", "|", "-", ":", "-", ",", "-")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "unknown-service"
	}
	if len(value) > 160 {
		return value[:160]
	}
	return value
}

func aiLogFixerEventTags(ev IngestedEvent) []string {
	keys := []string{"environment", "app", "vhost", "server_group", "path_template", "collector", "parser"}
	out := make([]string, 0, len(keys)+2)
	out = append(out, ev.Type)
	for _, key := range keys {
		if value := strings.TrimSpace(detailsString(ev.Details, key, "")); value != "" {
			out = append(out, key+":"+value)
		}
	}
	return sanitizeStringSlice(out, 12)
}

func aiLogFixerEvidenceEnvelope(ev IngestedEvent, serviceKey, dedupKey string) map[string]any {
	return map[string]any{
		"source":            "control_one",
		"service_key":       serviceKey,
		"trigger_dedup_key": dedupKey,
		"event":             ev,
		"citations": []map[string]any{
			{
				"system":         "control_one",
				"type":           "observability_event",
				"event_id":       firstNonEmpty(ev.EventID, ev.DedupKey, ev.CorrelationID),
				"event_type":     ev.Type,
				"correlation_id": ev.CorrelationID,
				"timestamp":      ev.TS.UTC().Format(time.RFC3339),
			},
		},
		"service_mapping": map[string]any{
			"service":       detailsString(ev.Details, "service", ""),
			"environment":   firstNonEmpty(detailsString(ev.Details, "environment", ""), detailsString(ev.Details, "env", "")),
			"app":           detailsString(ev.Details, "app", ""),
			"vhost":         detailsString(ev.Details, "vhost", ""),
			"server_group":  detailsString(ev.Details, "server_group", ""),
			"path_template": detailsString(ev.Details, "path_template", ""),
			"target":        detailsString(ev.Details, "target", ""),
		},
	}
}

func effectiveEventNodeID(fallbackNodeID uuid.UUID, ev IngestedEvent) uuid.UUID {
	if parsed, err := uuid.Parse(strings.TrimSpace(ev.NodeID)); err == nil {
		return parsed
	}
	return fallbackNodeID
}

func mustMarshalJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func decodeAILogFixerPayload(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("ai logfixer payload required")
	}
	var p aiLogFixerJobPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid ai logfixer payload: %w", err)
	}
	if _, err := uuid.Parse(strings.TrimSpace(p.TenantID)); err != nil {
		return nil, fmt.Errorf("tenant_id must be UUID: %w", err)
	}
	if _, err := uuid.Parse(strings.TrimSpace(p.NodeID)); err != nil {
		return nil, fmt.Errorf("node_id must be UUID: %w", err)
	}
	if strings.TrimSpace(p.RunID) != "" {
		if _, err := uuid.Parse(strings.TrimSpace(p.RunID)); err != nil {
			return nil, fmt.Errorf("run_id must be UUID: %w", err)
		}
	}
	if p.Action == "" {
		return nil, errors.New("action required")
	}
	switch p.Action {
	case JobTypeAILogFixerPlan:
	case JobTypeAILogFixerApply, JobTypeAILogFixerRollback:
		if !policyBool(p.Policy, "approved") {
			return nil, fmt.Errorf("%s requires approved=true policy", p.Action)
		}
	default:
		return nil, fmt.Errorf("unsupported ai logfixer action %q", p.Action)
	}
	if strings.TrimSpace(p.ContractVersion) != "" && strings.TrimSpace(p.ContractVersion) != aiLogFixerJobContractVersion {
		return nil, fmt.Errorf("unsupported ai logfixer contract_version %q", p.ContractVersion)
	}
	if strings.TrimSpace(p.ServiceKey) == "" {
		p.ServiceKey = "unknown-service"
	}
	return p, nil
}

func (s *Server) handleAILogFixerHeartbeatJob(_ context.Context, job *storage.Job) error {
	if job == nil {
		return errors.New("nil job")
	}
	return nil
}

func (s *Server) createAILogFixerActionForJob(ctx context.Context, job *storage.Job, payloadDetails any) error {
	if s == nil || s.store == nil || job == nil {
		return nil
	}
	payload, ok := payloadDetails.(aiLogFixerJobPayload)
	if !ok {
		return nil
	}
	store, ok := s.store.(aiLogFixerActionCreateStore)
	if !ok {
		return errors.New("ai logfixer action store unavailable")
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(payload.TenantID))
	if err != nil {
		return err
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(payload.NodeID))
	if err != nil {
		return err
	}
	var runID *uuid.UUID
	if raw := strings.TrimSpace(payload.RunID); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return err
		}
		runID = &parsed
	}
	payload.Policy = s.attachAILogFixerActionPlan(ctx, tenantID, nodeID, runID, job.ID, payload)
	_, err = store.CreateAILogFixerAction(ctx, storage.CreateAILogFixerActionParams{
		TenantID: tenantID,
		NodeID:   nodeID,
		RunID:    runID,
		JobID:    job.ID,
		Action:   payload.Action,
		Policy:   payload.Policy,
	})
	return err
}

func (s *Server) appendPendingAILogFixerActions(ctx context.Context, nodeID uuid.UUID, node *storage.Node, resp *heartbeatResponse) {
	if s == nil || s.store == nil || resp == nil {
		return
	}
	store, ok := s.store.(aiLogFixerActionStore)
	if !ok {
		return
	}
	pending, err := store.ListPendingAILogFixerActions(ctx, nodeID)
	if err != nil {
		s.logger.Warn("list pending ai logfixer actions", zap.Error(err))
		return
	}
	for _, action := range pending {
		actionType := strings.TrimSpace(action.Action)
		if actionType == "" {
			actionType = JobTypeAILogFixerPlan
		}
		if !nodeAdvertisesCapability(node, agentCapabilityAILogFixerRemediate) {
			errMsg := "agent does not advertise ailogfixer_remediation.v1 capability"
			if err := store.MarkAILogFixerActionStatus(ctx, action.JobID, "failed", nil, errMsg); err != nil {
				s.logger.Warn("mark unsupported ai logfixer action failed", zap.String("job_id", action.JobID.String()), zap.Error(err))
			}
			if err := s.store.UpdateJobStatus(ctx, action.JobID, storage.JobStatusFailed, errMsg, map[string]any{"unsupported_capability": agentCapabilityAILogFixerRemediate}); err != nil {
				s.logger.Warn("mark unsupported ai logfixer job failed", zap.String("job_id", action.JobID.String()), zap.Error(err))
			}
			actionCopy := action
			s.recordAILogFixerActionReceipt(ctx, &actionCopy, action.JobID, "failed", errMsg, map[string]any{"unsupported_capability": agentCapabilityAILogFixerRemediate})
			continue
		}
		resp.PendingActions = append(resp.PendingActions, actionType+":"+action.JobID.String())
		if err := store.MarkAILogFixerActionStatus(ctx, action.JobID, "running", nil, ""); err != nil {
			s.logger.Warn("mark ai logfixer action running", zap.String("job_id", action.JobID.String()), zap.Error(err))
		}
		if err := s.store.UpdateJobStatus(ctx, action.JobID, storage.JobStatusRunning, "agent notified via heartbeat", nil); err != nil {
			s.logger.Warn("mark ai logfixer job running", zap.String("job_id", action.JobID.String()), zap.Error(err))
		}
	}
}

func (s *Server) processAILogFixerCompletedAction(ctx context.Context, jobID uuid.UUID, c heartbeatCompletedAction) {
	store, ok := s.store.(aiLogFixerActionStore)
	if !ok {
		return
	}
	action, err := store.GetAILogFixerActionByJobID(ctx, jobID)
	if err != nil {
		s.logger.Warn("get ai logfixer action by job id", zap.String("job_id", jobID.String()), zap.Error(err))
		return
	}
	if action == nil {
		return
	}
	status := "succeeded"
	jobStatus := storage.JobStatusSucceeded
	message := "AI LogFixer action completed"
	errMsg := ""
	if c.Status != "succeeded" {
		status = "failed"
		jobStatus = storage.JobStatusFailed
		errMsg = strings.TrimSpace(c.Error)
		if errMsg == "" {
			errMsg = "agent reported AI LogFixer failure"
		}
		message = errMsg
	}
	if status == "succeeded" && aiLogFixerActionRequiresReceipt(action.Action) && len(metadataMap(c.Metadata["receipt"])) == 0 {
		status = "failed"
		jobStatus = storage.JobStatusFailed
		errMsg = "required AI LogFixer receipt missing"
		message = errMsg
	}
	if err := store.MarkAILogFixerActionStatus(ctx, jobID, status, c.Metadata, errMsg); err != nil {
		s.logger.Warn("mark ai logfixer action status", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	if action.RunID.Valid {
		runStatus := aiLogFixerRunStatusForAction(action.Action, status)
		if err := store.MarkAILogFixerRunStatus(ctx, action.RunID.UUID, runStatus, metadataMap(c.Metadata["attempt"]), metadataMap(c.Metadata["receipt"])); err != nil {
			s.logger.Warn("mark ai logfixer run status", zap.String("run_id", action.RunID.UUID.String()), zap.Error(err))
		}
	}
	fields := map[string]any{}
	for k, v := range c.Metadata {
		fields[k] = v
	}
	if errMsg != "" {
		fields["error"] = errMsg
	}
	if err := s.store.UpdateJobStatus(ctx, jobID, jobStatus, message, fields); err != nil {
		s.logger.Warn("ai logfixer job mark complete", zap.String("job_id", jobID.String()), zap.Error(err))
	}
	s.recordAILogFixerActionReceipt(ctx, action, jobID, status, errMsg, c.Metadata)
	s.recordAudit(ctx, s.systemActor(), action.TenantID, "ai_logfixer.action."+status, "ai_logfixer_action", action.ID.String(), map[string]any{
		"job_id":  jobID.String(),
		"node_id": action.NodeID.String(),
		"run_id":  action.RunID.UUID.String(),
		"action":  action.Action,
		"error":   errMsg,
	})
}

func aiLogFixerActionRequiresReceipt(action string) bool {
	switch strings.TrimSpace(action) {
	case JobTypeAILogFixerApply, JobTypeAILogFixerRollback:
		return true
	default:
		return false
	}
}

func aiLogFixerRunStatusForAction(action, actionStatus string) string {
	if actionStatus == "failed" {
		return string(contractsv1.RemediationStatusFailed)
	}
	switch strings.TrimSpace(action) {
	case JobTypeAILogFixerRollback:
		return string(contractsv1.RemediationStatusRolledBack)
	case JobTypeAILogFixerApply:
		return string(contractsv1.RemediationStatusSucceeded)
	default:
		return string(contractsv1.RemediationStatusPlanning)
	}
}

func init() {
	for _, jobType := range []string{JobTypeAILogFixerPlan, JobTypeAILogFixerApply, JobTypeAILogFixerRollback} {
		registerJobDefinition(jobType, jobDefinition{
			RequiresTenant: true,
			Validate: func(payload json.RawMessage) (any, error) {
				return decodeAILogFixerPayload(payload)
			},
		})
	}
}
