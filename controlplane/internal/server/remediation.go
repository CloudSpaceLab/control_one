package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

type remediationScriptResponse struct {
	ID                string         `json:"id"`
	RuleID            string         `json:"rule_id"`
	Platform          string         `json:"platform"`
	ScriptType        string         `json:"script_type"`
	ScriptContent     string         `json:"script_content"`
	Checksum          *string        `json:"checksum,omitempty"`
	Version           int            `json:"version"`
	Enabled           bool           `json:"enabled"`
	ActionType        string         `json:"action_type"`
	SafetyClass       string         `json:"safety_class"`
	RequiresApproval  bool           `json:"requires_approval"`
	AIProposalOnly    bool           `json:"ai_proposal_only"`
	RollbackAvailable bool           `json:"rollback_available"`
	PolicyGates       []string       `json:"policy_gates,omitempty"`
	EvidenceRequired  []string       `json:"evidence_required,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedBy         *string        `json:"created_by,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type remediationActionDescriptor struct {
	ID                string   `json:"id"`
	RuleID            string   `json:"rule_id"`
	Platform          string   `json:"platform"`
	ScriptType        string   `json:"script_type"`
	Version           int      `json:"version"`
	Enabled           bool     `json:"enabled"`
	ActionType        string   `json:"action_type"`
	SafetyClass       string   `json:"safety_class"`
	RequiresApproval  bool     `json:"requires_approval"`
	AIProposalOnly    bool     `json:"ai_proposal_only"`
	RollbackAvailable bool     `json:"rollback_available"`
	ExecutionPolicy   string   `json:"execution_policy"`
	PolicyGates       []string `json:"policy_gates,omitempty"`
	EvidenceRequired  []string `json:"evidence_required,omitempty"`
}

type remediationMatrixResponse struct {
	Data       []remediationActionDescriptor `json:"data"`
	Pagination paginationMeta                `json:"pagination"`
	Legend     map[string]any                `json:"legend"`
}

type createRemediationScriptRequest struct {
	RuleID        string         `json:"rule_id"`
	Platform      string         `json:"platform"`
	ScriptType    string         `json:"script_type"`
	ScriptContent string         `json:"script_content"`
	Version       *int           `json:"version,omitempty"`
	Enabled       *bool          `json:"enabled,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type updateRemediationScriptRequest struct {
	ScriptContent *string         `json:"script_content,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
	Metadata      *map[string]any `json:"metadata,omitempty"`
}

func (s *Server) handleRemediationScriptsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListRemediationScripts(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateRemediationScript(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRemediationMatrix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	scripts, total, err := s.store.ListRemediationScripts(r.Context(), ruleID, platform, limit, offset)
	if err != nil {
		s.logger.Error("list remediation matrix", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	descriptors := make([]remediationActionDescriptor, 0, len(scripts))
	for _, script := range scripts {
		descriptors = append(descriptors, remediationDescriptorFromScript(script))
	}
	writeJSON(w, http.StatusOK, remediationMatrixResponse{
		Data:       descriptors,
		Pagination: newPaginationMeta(total, limit, offset, len(descriptors)),
		Legend:     remediationMatrixLegend(),
	})
}

func (s *Server) handleRemediationScriptSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/remediation/scripts/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) == 1 {
		scriptID, err := uuid.Parse(segments[0])
		if err != nil {
			http.Error(w, "invalid script id", http.StatusBadRequest)
			return
		}
		s.handleRemediationScriptResource(w, r, scriptID)
		return
	}

	if len(segments) == 2 && segments[1] == "execute" {
		scriptID, err := uuid.Parse(segments[0])
		if err != nil {
			http.Error(w, "invalid script id", http.StatusBadRequest)
			return
		}
		s.handleExecuteRemediationScript(w, r, scriptID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleListRemediationScripts(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))

	scripts, total, err := s.store.ListRemediationScripts(r.Context(), ruleID, platform, limit, offset)
	if err != nil {
		s.logger.Error("list remediation scripts", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]remediationScriptResponse, 0, len(scripts))
	for _, script := range scripts {
		respItems = append(respItems, newRemediationScriptResponse(script))
	}

	resp := paginatedResponse[remediationScriptResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateRemediationScript(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req createRemediationScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.RuleID) == "" {
		http.Error(w, "rule_id is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ScriptType) == "" {
		http.Error(w, "script_type is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ScriptContent) == "" {
		http.Error(w, "script_content is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Platform) == "" {
		req.Platform = "all"
	}

	var createdBy *uuid.UUID
	if principal.Subject != "" {
		if id, err := uuid.Parse(principal.Subject); err == nil {
			createdBy = &id
		}
	}

	params := storage.CreateRemediationScriptParams{
		RuleID:        req.RuleID,
		Platform:      req.Platform,
		ScriptType:    req.ScriptType,
		ScriptContent: req.ScriptContent,
		Version:       req.Version,
		Enabled:       req.Enabled,
		Metadata:      req.Metadata,
		CreatedBy:     createdBy,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}

	created, err := s.store.CreateRemediationScript(r.Context(), params)
	if err != nil {
		s.logger.Error("create remediation script", zap.Error(err))
		http.Error(w, fmt.Sprintf("create remediation script failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newRemediationScriptResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "remediation_script.created", "remediation_script", created.ID.String(), map[string]any{
		"rule_id":  req.RuleID,
		"platform": req.Platform,
	})
}

func (s *Server) handleRemediationScriptResource(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetRemediationScript(w, r, scriptID)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUpdateRemediationScript(w, r, scriptID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetRemediationScript(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))

	var script *storage.RemediationScript
	var err error

	if ruleID != "" {
		script, err = s.store.GetRemediationScript(r.Context(), ruleID, platform)
	} else {
		script, err = s.store.GetRemediationScriptByID(r.Context(), scriptID)
	}

	if err != nil {
		s.logger.Error("get remediation script", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if script == nil {
		http.NotFound(w, r)
		return
	}

	resp := newRemediationScriptResponse(*script)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateRemediationScript(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req updateRemediationScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	params := storage.UpdateRemediationScriptParams{
		ScriptContent: req.ScriptContent,
		Enabled:       req.Enabled,
		Metadata:      req.Metadata,
	}

	updated, err := s.store.UpdateRemediationScript(r.Context(), scriptID, params)
	if err != nil {
		s.logger.Error("update remediation script", zap.Error(err))
		http.Error(w, fmt.Sprintf("update remediation script failed: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newRemediationScriptResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "remediation_script.updated", "remediation_script", scriptID.String(), map[string]any{})
}

func newRemediationScriptResponse(script storage.RemediationScript) remediationScriptResponse {
	descriptor := remediationDescriptorFromScript(script)
	resp := remediationScriptResponse{
		ID:                script.ID.String(),
		RuleID:            script.RuleID,
		Platform:          script.Platform,
		ScriptType:        script.ScriptType,
		ScriptContent:     script.ScriptContent,
		Version:           script.Version,
		Enabled:           script.Enabled,
		ActionType:        descriptor.ActionType,
		SafetyClass:       descriptor.SafetyClass,
		RequiresApproval:  descriptor.RequiresApproval,
		AIProposalOnly:    descriptor.AIProposalOnly,
		RollbackAvailable: descriptor.RollbackAvailable,
		PolicyGates:       descriptor.PolicyGates,
		EvidenceRequired:  descriptor.EvidenceRequired,
		Metadata:          script.Metadata,
		CreatedAt:         formatTime(script.CreatedAt),
		UpdatedAt:         formatTime(script.UpdatedAt),
	}
	if script.Checksum.Valid {
		checksum := script.Checksum.String
		resp.Checksum = &checksum
	}
	if script.CreatedBy != nil {
		createdBy := script.CreatedBy.String()
		resp.CreatedBy = &createdBy
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	return resp
}

func remediationDescriptorFromScript(script storage.RemediationScript) remediationActionDescriptor {
	actionType := firstNonEmptyString(
		strings.TrimSpace(detailsString(script.Metadata, "action_type", "")),
		strings.TrimSpace(detailsString(script.Metadata, "remediation_type", "")),
		"script",
	)
	declaredSafetyClass := strings.ToLower(strings.TrimSpace(firstNonEmptyString(
		detailsString(script.Metadata, "safety_class", ""),
		detailsString(script.Metadata, "safety", ""),
	)))
	if declaredSafetyClass != "" {
		declaredSafetyClass = normalizeRemediationSafetyClass(declaredSafetyClass)
	}
	inferredSafetyClass := normalizeRemediationSafetyClass(inferRemediationSafetyClass(script))
	safetyClass := declaredSafetyClass
	if safetyClass == "" {
		safetyClass = inferredSafetyClass
	}
	if inferredSafetyClass == "destructive" && safetyClass != "destructive" {
		safetyClass = inferredSafetyClass
	}
	if safetyClass == "read_only" && inferredSafetyClass != "read_only" {
		safetyClass = inferredSafetyClass
	}
	policyGates := stringsFromMetadata(script.Metadata, "policy_gates")
	if len(policyGates) == 0 {
		policyGates = []string{
			"rbac_operator",
			"tenant_node_binding",
			"remediation_lease",
			"manual_or_policy_approval",
			"circuit_breaker",
			"audit_receipt",
		}
	}
	evidenceRequired := stringsFromMetadata(script.Metadata, "evidence_required")
	if len(evidenceRequired) == 0 {
		evidenceRequired = []string{"rule_id", "script_checksum", "node_id", "operator_reason", "completion_receipt"}
	}
	requiresApproval := true
	if safetyClass == "read_only" && !boolFromMap(script.Metadata, "requires_approval") {
		requiresApproval = false
	}
	executionPolicy := "proposal_only_for_ai_operator_policy_gated"
	if !script.Enabled {
		executionPolicy = "disabled"
	}
	return remediationActionDescriptor{
		ID:                script.ID.String(),
		RuleID:            script.RuleID,
		Platform:          script.Platform,
		ScriptType:        script.ScriptType,
		Version:           script.Version,
		Enabled:           script.Enabled,
		ActionType:        actionType,
		SafetyClass:       safetyClass,
		RequiresApproval:  requiresApproval,
		AIProposalOnly:    true,
		RollbackAvailable: script.RollbackContent.Valid && strings.TrimSpace(script.RollbackContent.String) != "",
		ExecutionPolicy:   executionPolicy,
		PolicyGates:       policyGates,
		EvidenceRequired:  evidenceRequired,
	}
}

func remediationMatrixLegend() map[string]any {
	return map[string]any{
		"safety_classes": []map[string]string{
			{"state": "read_only", "meaning": "collects evidence or plans only; no host mutation"},
			{"state": "standard", "meaning": "bounded configuration change with rollback evidence"},
			{"state": "privileged", "meaning": "requires elevated host privileges and explicit approval gates"},
			{"state": "destructive", "meaning": "can remove, reset, or interrupt critical state; highest scrutiny"},
		},
		"execution_policy": []string{
			"AI tools may propose or save workflow records only",
			"operator execution must pass RBAC, tenant binding, lease, approval, circuit breaker, and audit gates",
			"unsupported or unmapped controls must not be marked automatically remediable",
		},
	}
}

func normalizeRemediationSafetyClass(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read_only", "standard", "privileged", "destructive":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "privileged"
	}
}

func inferRemediationSafetyClass(script storage.RemediationScript) string {
	content := strings.ToLower(script.ScriptContent)
	for _, marker := range []string{"rm -rf", "mkfs", "drop database", "truncate table", "userdel", "shutdown", "reboot", "iptables -f", "format-volume"} {
		if strings.Contains(content, marker) {
			return "destructive"
		}
	}
	if strings.Contains(strings.ToLower(script.ScriptType), "plan") {
		return "read_only"
	}
	return "privileged"
}

func remediationScriptArtifactChecksum(script storage.RemediationScript) string {
	if script.Checksum.Valid && strings.TrimSpace(script.Checksum.String) != "" {
		return strings.TrimSpace(script.Checksum.String)
	}
	sum := sha256.Sum256([]byte(script.ScriptContent))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type executeRemediationScriptRequest struct {
	TenantID string            `json:"tenant_id,omitempty"`
	NodeID   string            `json:"node_id"`
	RuleID   string            `json:"rule_id"`
	Reason   string            `json:"reason,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
}

type executeRemediationScriptResponse struct {
	JobID            string   `json:"job_id,omitempty"`
	ApprovalID       string   `json:"approval_id,omitempty"`
	Status           string   `json:"status"`
	Message          string   `json:"message"`
	SafetyClass      string   `json:"safety_class,omitempty"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
	PolicyGates      []string `json:"policy_gates,omitempty"`
}

func (s *Server) handleExecuteRemediationScript(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	if s.worker == nil {
		http.Error(w, "worker queue unavailable", http.StatusServiceUnavailable)
		return
	}

	var req executeRemediationScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.NodeID) == "" {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}

	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get remediation target node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	tenantID := node.TenantID
	if tenantID == uuid.Nil {
		http.Error(w, "target node is not tenant scoped", http.StatusBadRequest)
		return
	}
	if rawTenantID := strings.TrimSpace(req.TenantID); rawTenantID != "" {
		requestedTenantID, err := uuid.Parse(rawTenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		if requestedTenantID != tenantID {
			http.Error(w, "node is outside requested tenant", http.StatusForbidden)
			return
		}
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleAdmin) {
		return
	}

	script, err := s.store.GetRemediationScriptByID(r.Context(), scriptID)
	if err != nil {
		s.logger.Error("get remediation script", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if script == nil {
		http.NotFound(w, r)
		return
	}

	if !script.Enabled {
		http.Error(w, "remediation script is disabled", http.StatusBadRequest)
		return
	}

	ruleID := req.RuleID
	if ruleID == "" {
		ruleID = script.RuleID
	}

	descriptor := remediationDescriptorFromScript(*script)
	if descriptor.RequiresApproval {
		approval, err := s.createManualRemediationApproval(r.Context(), tenantID, nodeID, ruleID, script, descriptor, req)
		if err != nil {
			s.logger.Error("create manual remediation approval",
				zap.String("tenant_id", tenantID.String()),
				zap.String("node_id", nodeID.String()),
				zap.String("rule_id", ruleID),
				zap.Error(err),
			)
			http.Error(w, "remediation approval gate unavailable", http.StatusServiceUnavailable)
			return
		}
		resp := executeRemediationScriptResponse{
			ApprovalID:       approval.ID.String(),
			Status:           "approval_required",
			Message:          "remediation approval required before execution",
			SafetyClass:      descriptor.SafetyClass,
			RequiresApproval: true,
			PolicyGates:      descriptor.PolicyGates,
		}
		writeJSON(w, http.StatusAccepted, resp)
		s.recordAudit(r.Context(), principal, tenantID, "remediation.execute.approval_requested", "remediation_approval", approval.ID.String(), map[string]any{
			"script_id":    scriptID.String(),
			"node_id":      nodeID.String(),
			"rule_id":      ruleID,
			"safety_class": descriptor.SafetyClass,
		})
		return
	}

	if cap := s.remediationTenantCap(); cap > 0 {
		inflight, err := s.store.CountTenantLeases(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("count tenant remediation leases", zap.String("tenant_id", tenantID.String()), zap.Error(err))
			http.Error(w, "remediation concurrency gate unavailable", http.StatusServiceUnavailable)
			return
		}
		if inflight >= cap {
			http.Error(w, "tenant remediation concurrency cap reached", http.StatusConflict)
			return
		}
	}

	jobPayload := map[string]any{
		"script_id":        scriptID.String(),
		"rule_id":          ruleID,
		"tenant_id":        tenantID.String(),
		"node_id":          nodeID.String(),
		"platform":         script.Platform,
		"script_type":      script.ScriptType,
		"script_content":   script.ScriptContent,
		"manual_triggered": true,
	}
	if len(req.Env) > 0 {
		jobPayload["env"] = req.Env
	}

	payloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		s.logger.Error("marshal remediation job payload", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	job := &storage.Job{
		Type:     "remediation.execute",
		TenantID: tenantID,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(r.Context(), job, nil)
	if err != nil {
		s.logger.Error("create remediation job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if _, err := s.store.AcquireRemediationLease(r.Context(), tenantID, nodeID, job.ID, s.remediationLeaseTTL()); err != nil {
		_ = s.store.UpdateJobStatus(r.Context(), job.ID, storage.JobStatusCancelled, "skipped: remediation lease held", nil)
		if errors.Is(err, storage.ErrLeaseHeld) {
			http.Error(w, "remediation lease already held for node", http.StatusConflict)
			return
		}
		s.logger.Error("acquire manual remediation lease", zap.Error(err))
		http.Error(w, "remediation lease unavailable", http.StatusServiceUnavailable)
		return
	}

	task := worker.Task{
		Name:         fmt.Sprintf("remediation-%s", job.ID),
		Job:          s.chainRemediationExecution(job.ID, scriptID, nodeID, ruleID, script, false),
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		s.logger.Error("enqueue remediation job", zap.Error(err))
		_ = s.store.UpdateJobStatus(r.Context(), job.ID, storage.JobStatusFailed, "failed to enqueue job", nil)
		_ = s.store.ReleaseRemediationLease(r.Context(), nodeID)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := executeRemediationScriptResponse{
		JobID:   job.ID.String(),
		Status:  string(job.Status),
		Message: "remediation job created and queued",
	}
	writeJSON(w, http.StatusAccepted, resp)

	s.recordAudit(r.Context(), principal, tenantID, "remediation.execute", "remediation_script", scriptID.String(), map[string]any{
		"job_id":           job.ID.String(),
		"node_id":          nodeID.String(),
		"rule_id":          ruleID,
		"manual_triggered": true,
	})
}

func (s *Server) createManualRemediationApproval(ctx context.Context, tenantID, nodeID uuid.UUID, ruleID string, script *storage.RemediationScript, descriptor remediationActionDescriptor, req executeRemediationScriptRequest) (*storage.RemediationApproval, error) {
	if script == nil {
		return nil, errors.New("remediation script unavailable")
	}
	payload := map[string]any{
		"script_id":          script.ID.String(),
		"script_checksum":    remediationScriptArtifactChecksum(*script),
		"script_version":     script.Version,
		"rule_id":            ruleID,
		"tenant_id":          tenantID.String(),
		"node_id":            nodeID.String(),
		"platform":           script.Platform,
		"script_type":        script.ScriptType,
		"manual_triggered":   true,
		"approval_requested": true,
		"action_type":        descriptor.ActionType,
		"safety_class":       descriptor.SafetyClass,
		"policy_gates":       descriptor.PolicyGates,
	}
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		payload["operator_reason"] = reason
	}
	if len(req.Env) > 0 {
		envKeys := make([]string, 0, len(req.Env))
		for key := range req.Env {
			if strings.TrimSpace(key) != "" {
				envKeys = append(envKeys, strings.TrimSpace(key))
			}
		}
		sort.Strings(envKeys)
		payload["env_keys"] = envKeys
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal approval payload: %w", err)
	}
	return s.store.CreateRemediationApproval(ctx, storage.CreateRemediationApprovalParams{
		TenantID:    tenantID,
		NodeID:      nodeID,
		RuleID:      ruleID,
		ScriptID:    script.ID,
		Severity:    manualRemediationApprovalSeverity(descriptor.SafetyClass),
		TaskPayload: payloadBytes,
		ExpiresAt:   time.Now().UTC().Add(approvalTTL),
	})
}

func manualRemediationApprovalSeverity(safetyClass string) string {
	switch strings.ToLower(strings.TrimSpace(safetyClass)) {
	case "destructive":
		return "critical"
	case "privileged", "standard":
		return "high"
	default:
		return "low"
	}
}

func (s *Server) buildRemediationJobExecution(jobID uuid.UUID, scriptID uuid.UUID, nodeID uuid.UUID, ruleID string, script *storage.RemediationScript) func(context.Context) error {
	return func(ctx context.Context) error {
		principal := s.systemActor()
		job, err := s.store.GetJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("load job: %w", err)
		}
		if job == nil {
			return fmt.Errorf("job %s not found", jobID)
		}

		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "executing remediation script", nil); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}

		remediationEngine := remediation.NewEngine(s.logger, remediation.Options{
			Timeout: 5 * time.Minute,
		})

		remediationScript := remediation.Script{
			RuleID:        ruleID,
			Platform:      script.Platform,
			ScriptType:    script.ScriptType,
			ScriptContent: script.ScriptContent,
		}

		result, err := remediationEngine.Execute(ctx, remediationScript)
		if err != nil {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, fmt.Sprintf("remediation execution failed: %v", err), map[string]any{
				"error": err.Error(),
			})
			return err
		}

		status := storage.JobStatusSucceeded
		message := "remediation script executed successfully"
		if !result.Success {
			status = storage.JobStatusFailed
			message = fmt.Sprintf("remediation script failed: %s", result.Error)
		}

		if err := s.store.UpdateJobStatus(ctx, jobID, status, message, map[string]any{
			"output":      result.Output,
			"error":       result.Error,
			"executed_at": result.ExecutedAt,
			"duration":    result.Duration.String(),
		}); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}

		s.recordAudit(ctx, principal, job.TenantID, "remediation.completed", "job", jobID.String(), map[string]any{
			"success": result.Success,
			"rule_id": ruleID,
			"node_id": nodeID.String(),
		})

		return nil
	}
}
