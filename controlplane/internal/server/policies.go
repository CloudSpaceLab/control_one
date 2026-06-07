package server

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	agentpolicy "github.com/CloudSpaceLab/control_one/internal/policy"
)

// emitPolicyUpdated publishes a policy.updated event so subscribed agents
// (and the UI) can refresh without waiting for the pull cron.
func (s *Server) emitPolicyUpdated(tenantID, policyID uuid.UUID, action, name string, version int) {
	s.emitPolicyUpdatedTargeted(tenantID, policyID, action, nil)
	_ = name
	_ = version
}

// emitPolicyUpdatedTargeted narrows fan-out to a specific node for assignments
// that target one node; nil means tenant-wide.
func (s *Server) emitPolicyUpdatedTargeted(tenantID, policyID uuid.UUID, action string, nodeID *uuid.UUID) {
	if s == nil || s.eventBus == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"policy_id": policyID.String(),
		"action":    action,
	})
	s.eventBus.Publish(eventbus.Event{
		Topic:    eventbus.TopicPolicyUpdated,
		TenantID: tenantID,
		NodeID:   nodeID,
		Payload:  payload,
	})
}

type policyResponse struct {
	ID          string            `json:"id"`
	TenantID    *string           `json:"tenant_id,omitempty"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	RuleType    string            `json:"rule_type"`
	Enabled     bool              `json:"enabled"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	ArchivedAt  *string           `json:"archived_at,omitempty"`
}

type policyVersionResponse struct {
	ID             string         `json:"id"`
	Version        int            `json:"version"`
	RuleDefinition string         `json:"rule_definition"`
	Checksum       *string        `json:"checksum,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedBy      *string        `json:"created_by,omitempty"`
	CreatedAt      string         `json:"created_at"`
	PromotedAt     *string        `json:"promoted_at,omitempty"`
}

type createPolicyRequest struct {
	TenantID    *string           `json:"tenant_id"`
	Name        string            `json:"name"`
	Description *string           `json:"description"`
	RuleType    string            `json:"rule_type"`
	Enabled     bool              `json:"enabled"`
	Labels      map[string]string `json:"labels"`
}

type effectivePolicyResponse struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id"`
	Name           string            `json:"name"`
	Description    *string           `json:"description,omitempty"`
	RuleType       string            `json:"rule_type"`
	Enabled        bool              `json:"enabled"`
	Labels         map[string]string `json:"labels"`
	Version        int               `json:"version"`
	VersionID      string            `json:"version_id"`
	RuleDefinition string            `json:"rule_definition"`
	UpdatedAt      string            `json:"updated_at"`
}

func newEffectivePolicyResponse(p storage.PolicyWithVersion) effectivePolicyResponse {
	out := effectivePolicyResponse{
		ID:             p.ID.String(),
		TenantID:       p.TenantID.String(),
		Name:           p.Name,
		RuleType:       p.RuleType,
		Enabled:        p.Enabled,
		Labels:         p.Labels,
		Version:        p.Version,
		VersionID:      p.VersionID.String(),
		RuleDefinition: p.RuleDefinition,
		UpdatedAt:      formatTime(p.UpdatedAt),
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	if p.Description.Valid {
		desc := p.Description.String
		out.Description = &desc
	}
	return out
}

type updatePolicyRequest struct {
	Name        *string            `json:"name"`
	Description *string            `json:"description"`
	RuleType    *string            `json:"rule_type"`
	Enabled     *bool              `json:"enabled"`
	Labels      *map[string]string `json:"labels"`
	Archived    *bool              `json:"archived"`
}

type createPolicyVersionRequest struct {
	RuleDefinition string         `json:"rule_definition"`
	Checksum       *string        `json:"checksum"`
	Metadata       map[string]any `json:"metadata"`
}

func (s *Server) handlePoliciesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if principal.Type == "agent" {
			s.handleAgentPolicySet(w, r, principal)
			return
		}
		if _, ok := s.authorizePrincipal(w, principal, roleViewer); !ok {
			return
		}
		s.handleListPolicies(w, r, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreatePolicy(w, r, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) authorizePrincipal(w http.ResponseWriter, principal *auth.Principal, allowedRoles ...string) (*auth.Principal, bool) {
	if principal == nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return nil, false
	}
	if len(allowedRoles) == 0 {
		return principal, true
	}
	for _, role := range principal.Roles {
		for _, allowed := range allowedRoles {
			if strings.EqualFold(strings.TrimSpace(role), strings.TrimSpace(allowed)) {
				return principal, true
			}
		}
	}
	for _, role := range principal.Roles {
		if strings.EqualFold(strings.TrimSpace(role), roleAdmin) {
			return principal, true
		}
	}
	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	return nil, false
}

func (s *Server) handleAgentPolicySet(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, nodeID, err := s.tenantNodeForAgent(r.Context(), principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requested := strings.TrimSpace(r.URL.Query().Get("node_id")); requested != "" {
		parsed, err := uuid.Parse(requested)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if parsed != nodeID {
			http.Error(w, "agent cannot fetch policies for another node", http.StatusForbidden)
			return
		}
	}

	effective, err := s.store.GetEffectivePolicies(r.Context(), tenantID, nodeID)
	if err != nil {
		s.logger.Error("list agent effective policies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	rules := make([]agentpolicy.Rule, 0, len(effective))
	for _, p := range effective {
		if rule, ok := agentRuleFromEffectivePolicy(p); ok {
			rules = append(rules, rule)
		}
	}
	version := agentPolicyVersion(rules)
	resp := struct {
		Policies  []agentpolicy.Rule `json:"policies"`
		Signature string             `json:"signature,omitempty"`
		Version   string             `json:"version,omitempty"`
	}{
		Policies: rules,
		Version:  version,
	}
	resp.Signature = s.signAgentPolicySet(rules, version)
	writeJSON(w, http.StatusOK, resp)
}

func agentRuleFromEffectivePolicy(p storage.PolicyWithVersion) (agentpolicy.Rule, bool) {
	check, remediation, severity := agentPolicyCommand(p)
	if strings.TrimSpace(check) == "" {
		return agentpolicy.Rule{}, false
	}
	if strings.TrimSpace(severity) == "" {
		severity = strings.TrimSpace(p.Labels["severity"])
	}
	if strings.TrimSpace(severity) == "" {
		severity = "medium"
	}
	return agentpolicy.Rule{
		ID:          p.ID.String(),
		Version:     strconv.Itoa(p.Version),
		Severity:    severity,
		Check:       check,
		Remediation: remediation,
	}, true
}

func agentPolicyCommand(p storage.PolicyWithVersion) (check, remediation, severity string) {
	definition := strings.TrimSpace(p.RuleDefinition)
	if definition == "" {
		return "", "", ""
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(definition), &body); err == nil {
		for _, key := range []string{"agent_check", "check", "command", "script"} {
			if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
				check = strings.TrimSpace(value)
				break
			}
		}
		if value, ok := body["remediation"].(string); ok {
			remediation = strings.TrimSpace(value)
		}
		if value, ok := body["severity"].(string); ok {
			severity = strings.TrimSpace(value)
		}
		return check, remediation, severity
	}
	switch strings.ToLower(strings.TrimSpace(p.RuleType)) {
	case "shell", "command", "script", "agent-command":
		return definition, "", ""
	default:
		return "", "", ""
	}
}

func agentPolicyVersion(rules []agentpolicy.Rule) string {
	payload := struct {
		Policies []agentpolicy.Rule `json:"policies"`
	}{Policies: rules}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Server) signAgentPolicySet(rules []agentpolicy.Rule, version string) string {
	mat := s.policySigningMaterial()
	if mat == nil || !mat.haveSigningKey || len(mat.privateKey) != ed25519.PrivateKeySize {
		return ""
	}
	payload := struct {
		Policies []agentpolicy.Rule `json:"policies"`
		Version  string             `json:"version,omitempty"`
	}{Policies: rules, Version: version}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(mat.privateKey, data))
}

func (s *Server) handlePolicySubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")

	policyID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid policy id", http.StatusBadRequest)
		return
	}

	switch len(segments) {
	case 1:
		s.handlePolicyResource(w, r, policyID)
	case 2:
		switch segments[1] {
		case "versions":
			s.handlePolicyVersions(w, r, policyID)
		case "assignments":
			s.handlePolicyAssignmentsCollection(w, r, policyID)
		default:
			http.NotFound(w, r)
		}
	case 3:
		if segments[1] == "assignments" {
			assignmentID, aidErr := uuid.Parse(segments[2])
			if aidErr != nil {
				http.Error(w, "invalid assignment id", http.StatusBadRequest)
				return
			}
			s.handleDeletePolicyAssignment(w, r, policyID, assignmentID)
			return
		}
		http.NotFound(w, r)
	case 4:
		if segments[1] == "versions" && segments[3] == "promote" {
			versionNumber, verErr := strconv.Atoi(segments[2])
			if verErr != nil || versionNumber <= 0 {
				http.Error(w, "invalid version number", http.StatusBadRequest)
				return
			}
			s.handlePromotePolicyVersion(w, r, policyID, versionNumber)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handlePolicyResource(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleGetPolicy(w, r, policyID, principal)
	case http.MethodPatch:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleUpdatePolicy(w, r, policyID, principal)
	case http.MethodDelete:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleDeletePolicy(w, r, policyID, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePolicyVersions(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, ok := s.requirePolicyAccess(w, r, principal, policyID, roleViewer, roleOperator, roleAdmin); !ok {
			return
		}

		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		versions, total, err := s.store.ListPolicyVersions(r.Context(), policyID, limit, offset)
		if err != nil {
			s.logger.Error("list policy versions", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		items := make([]policyVersionResponse, 0, len(versions))
		for i := range versions {
			items = append(items, newPolicyVersionResponse(&versions[i]))
		}

		resp := paginatedResponse[policyVersionResponse]{
			Data:       items,
			Pagination: newPaginationMeta(total, limit, offset, len(items)),
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreatePolicyVersion(w, r, policyID, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePromotePolicyVersion(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, versionNumber int) {
	switch r.Method {
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		policy, ok := s.requirePolicyAccess(w, r, principal, policyID, roleAdmin)
		if !ok {
			return
		}
		version, err := s.store.PromotePolicyVersion(r.Context(), policyID, versionNumber)
		if err != nil {
			http.Error(w, fmt.Sprintf("promote policy version: %v", err), http.StatusBadRequest)
			return
		}
		resp := newPolicyVersionResponse(version)
		writeJSON(w, http.StatusOK, resp)
		s.emitPolicyUpdated(policy.TenantID, policyID, "promote", policy.Name, versionNumber)
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.PolicyFilter{
		NamePrefix:      strings.TrimSpace(r.URL.Query().Get("name_prefix")),
		RuleType:        strings.TrimSpace(r.URL.Query().Get("rule_type")),
		IncludeArchived: parseBoolQuery(r.URL.Query().Get("include_archived")),
	}

	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id"))
	var parsedTenantID uuid.UUID
	if tenantParam != "" {
		parsedTenantID, err = uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		if !s.requireTenantAccess(w, r, principal, parsedTenantID, roleViewer, roleOperator, roleAdmin) {
			return
		}
		filter.TenantID = parsedTenantID
	} else if nodeParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}

	if enabledParam := strings.TrimSpace(r.URL.Query().Get("enabled")); enabledParam != "" {
		enabled := parseBoolQuery(enabledParam)
		filter.Enabled = &enabled
	}

	if nodeParam != "" {
		nodeID, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if filter.TenantID == uuid.Nil {
			node, err := s.store.GetNode(r.Context(), nodeID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					http.Error(w, "node not found", http.StatusNotFound)
					return
				}
				s.logger.Error("lookup node for effective policies", zap.Error(err))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if node == nil {
				http.Error(w, "node not found", http.StatusNotFound)
				return
			}
			filter.TenantID = node.TenantID
		} else if _, err := s.ensureNodeInTenant(r.Context(), filter.TenantID, nodeID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if !s.requireTenantAccess(w, r, principal, filter.TenantID, roleViewer, roleOperator, roleAdmin) {
			return
		}
		effective, err := s.store.GetEffectivePolicies(r.Context(), filter.TenantID, nodeID)
		if err != nil {
			s.logger.Error("list effective policies", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		respItems := make([]effectivePolicyResponse, 0, len(effective))
		for _, p := range effective {
			respItems = append(respItems, newEffectivePolicyResponse(p))
		}
		resp := paginatedResponse[effectivePolicyResponse]{
			Data:       respItems,
			Pagination: newPaginationMeta(len(respItems), len(respItems), 0, len(respItems)),
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	policies, total, err := s.store.ListPolicies(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list policies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]policyResponse, 0, len(policies))
	for _, p := range policies {
		respItems = append(respItems, newPolicyResponse(p))
	}

	resp := paginatedResponse[policyResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	req.RuleType = strings.TrimSpace(req.RuleType)
	if req.RuleType == "" {
		http.Error(w, "rule_type is required", http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if req.TenantID == nil || strings.TrimSpace(*req.TenantID) == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*req.TenantID))
	if err != nil || parsed == uuid.Nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	tenantID = parsed
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
		return
	}

	params := storage.CreatePolicyParams{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		RuleType:    req.RuleType,
		Enabled:     req.Enabled,
		Labels:      sanitizeLabels(req.Labels),
	}

	created, err := s.store.CreatePolicy(r.Context(), params)
	if err != nil {
		http.Error(w, fmt.Sprintf("create policy failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newPolicyResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, created.TenantID, "policy.create", "policy", created.ID.String(), map[string]any{
		"name":      created.Name,
		"rule_type": created.RuleType,
	})
	s.emitPolicyUpdated(created.TenantID, created.ID, "create", created.Name, 0)
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	policy, ok := s.requirePolicyAccess(w, r, principal, policyID, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}

	resp := newPolicyResponse(*policy)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.requirePolicyAccess(w, r, principal, policyID, roleAdmin); !ok {
		return
	}

	var req updatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	params := storage.UpdatePolicyParams{}
	var hasUpdate bool

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			http.Error(w, "name cannot be empty", http.StatusBadRequest)
			return
		}
		req.Name = &name
		params.Name = req.Name
		hasUpdate = true
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		req.Description = &desc
		params.Description = req.Description
		hasUpdate = true
	}
	if req.RuleType != nil {
		ruleType := strings.TrimSpace(*req.RuleType)
		if ruleType == "" {
			http.Error(w, "rule_type cannot be empty", http.StatusBadRequest)
			return
		}
		req.RuleType = &ruleType
		params.RuleType = req.RuleType
		hasUpdate = true
	}
	if req.Enabled != nil {
		params.Enabled = req.Enabled
		hasUpdate = true
	}
	if req.Labels != nil {
		sanitized := sanitizeLabels(*req.Labels)
		params.Labels = &sanitized
		hasUpdate = true
	}
	if req.Archived != nil {
		params.Archived = req.Archived
		hasUpdate = true
	}

	if !hasUpdate {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	updated, err := s.store.UpdatePolicy(r.Context(), policyID, params)
	if err != nil {
		http.Error(w, fmt.Sprintf("update policy: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newPolicyResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, updated.TenantID, "policy.update", "policy", policyID.String(), map[string]any{
		"name": updated.Name,
	})
	s.emitPolicyUpdated(updated.TenantID, updated.ID, "update", updated.Name, 0)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	policy, ok := s.requirePolicyAccess(w, r, principal, policyID, roleAdmin)
	if !ok {
		return
	}

	if err := s.store.DeletePolicy(r.Context(), policyID); err != nil {
		s.logger.Error("delete policy", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	s.recordAudit(r.Context(), principal, policy.TenantID, "policy.delete", "policy", policyID.String(), map[string]any{
		"name": policy.Name,
	})
	s.emitPolicyUpdated(policy.TenantID, policy.ID, "delete", policy.Name, 0)
}

func (s *Server) handleCreatePolicyVersion(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.requirePolicyAccess(w, r, principal, policyID, roleAdmin); !ok {
		return
	}

	var req createPolicyVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	req.RuleDefinition = strings.TrimSpace(req.RuleDefinition)
	if req.RuleDefinition == "" {
		http.Error(w, "rule_definition is required", http.StatusBadRequest)
		return
	}

	params := storage.CreatePolicyVersionParams{
		PolicyID:       policyID,
		RuleDefinition: req.RuleDefinition,
		Checksum:       req.Checksum,
		Metadata:       req.Metadata,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}
	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			params.CreatedBy = &user.ID
		}
	}

	version, err := s.store.CreatePolicyVersion(r.Context(), params)
	if err != nil {
		http.Error(w, fmt.Sprintf("create policy version failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newPolicyVersionResponse(version)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) requirePolicyAccess(w http.ResponseWriter, r *http.Request, principal *auth.Principal, policyID uuid.UUID, roles ...string) (*storage.Policy, bool) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	policy, err := s.store.GetPolicy(r.Context(), policyID)
	if err != nil {
		s.logger.Error("get policy for access check", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, false
	}
	if policy == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if !s.requireTenantAccess(w, r, principal, policy.TenantID, roles...) {
		return nil, false
	}
	return policy, true
}

func newPolicyResponse(p storage.Policy) policyResponse {
	resp := policyResponse{
		ID:        p.ID.String(),
		Name:      p.Name,
		RuleType:  p.RuleType,
		Enabled:   p.Enabled,
		Labels:    p.Labels,
		CreatedAt: formatTime(p.CreatedAt),
		UpdatedAt: formatTime(p.UpdatedAt),
	}
	if resp.Labels == nil {
		resp.Labels = map[string]string{}
	}
	if p.TenantID != uuid.Nil {
		tid := p.TenantID.String()
		resp.TenantID = &tid
	}
	if p.Description.Valid {
		desc := p.Description.String
		resp.Description = &desc
	}
	if p.ArchivedAt.Valid {
		resp.ArchivedAt = formatNullTime(p.ArchivedAt)
	}
	return resp
}

// --- Policy Assignment Handlers ---

type createPolicyAssignmentRequest struct {
	TenantID  string         `json:"tenant_id"`
	NodeID    *string        `json:"node_id"`
	ScopeType *string        `json:"scope_type"`
	ScopeID   *string        `json:"scope_id"`
	Selector  map[string]any `json:"selector"`
	ExpiresAt *string        `json:"expires_at"`
}

type policyAssignmentResponse struct {
	ID         string         `json:"id"`
	PolicyID   string         `json:"policy_id"`
	TenantID   string         `json:"tenant_id"`
	NodeID     *string        `json:"node_id,omitempty"`
	ScopeType  string         `json:"scope_type"`
	ScopeID    *string        `json:"scope_id,omitempty"`
	Selector   map[string]any `json:"selector"`
	AssignedAt string         `json:"assigned_at"`
	AssignedBy *string        `json:"assigned_by,omitempty"`
	ExpiresAt  *string        `json:"expires_at,omitempty"`
}

func (s *Server) handlePolicyAssignmentsCollection(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleListPolicyAssignments(w, r, policyID, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreatePolicyAssignment(w, r, policyID, principal)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreatePolicyAssignment(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createPolicyAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil || tenantID == uuid.Nil {
		http.Error(w, "valid tenant_id is required", http.StatusBadRequest)
		return
	}
	policy, ok := s.requirePolicyAccess(w, r, principal, policyID, roleAdmin)
	if !ok {
		return
	}
	if policy.TenantID != tenantID {
		http.Error(w, "assignment tenant_id must match policy tenant_id", http.StatusBadRequest)
		return
	}

	params := storage.CreatePolicyAssignmentParams{
		PolicyID: policyID,
		TenantID: tenantID,
		Selector: req.Selector,
	}

	if req.ScopeType != nil {
		params.ScopeType = strings.TrimSpace(*req.ScopeType)
	}
	if req.ScopeID != nil {
		scopeID, err := uuid.Parse(strings.TrimSpace(*req.ScopeID))
		if err != nil || scopeID == uuid.Nil {
			http.Error(w, "invalid scope_id", http.StatusBadRequest)
			return
		}
		params.ScopeID = scopeID
	}
	if req.NodeID != nil {
		nodeID, err := uuid.Parse(strings.TrimSpace(*req.NodeID))
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if _, err := s.ensureNodeInTenant(r.Context(), tenantID, nodeID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		params.NodeID = nodeID
	}
	if params.ScopeType == "" && params.NodeID != uuid.Nil {
		params.ScopeType = storage.AssignmentScopeNode
		params.ScopeID = params.NodeID
	}
	if params.ScopeType == "" {
		params.ScopeType = storage.AssignmentScopeTenant
	}
	if params.ScopeType == storage.AssignmentScopeNode && params.ScopeID == uuid.Nil {
		params.ScopeID = params.NodeID
	}
	if err := s.validatePolicyAssignmentScope(r.Context(), tenantID, params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			http.Error(w, "expires_at must be RFC3339", http.StatusBadRequest)
			return
		}
		params.ExpiresAt = &t
	}

	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			params.AssignedBy = &user.ID
		}
	}

	assignment, err := s.store.CreatePolicyAssignment(r.Context(), params)
	if err != nil {
		s.logger.Error("create policy assignment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, newPolicyAssignmentResponse(assignment))

	var nodeFilter *uuid.UUID
	if assignment.NodeID != uuid.Nil {
		n := assignment.NodeID
		nodeFilter = &n
	}
	s.emitPolicyUpdatedTargeted(assignment.TenantID, policyID, "assign", nodeFilter)
}

func (s *Server) validatePolicyAssignmentScope(ctx context.Context, tenantID uuid.UUID, params storage.CreatePolicyAssignmentParams) error {
	scopeType := strings.ToLower(strings.TrimSpace(params.ScopeType))
	switch scopeType {
	case storage.AssignmentScopeTenant:
		if params.ScopeID != uuid.Nil || params.NodeID != uuid.Nil || len(params.Selector) > 0 {
			return errors.New("tenant scope cannot include node_id, scope_id, or selector")
		}
	case storage.AssignmentScopeNode:
		scopeID := params.ScopeID
		if scopeID == uuid.Nil {
			scopeID = params.NodeID
		}
		if scopeID == uuid.Nil {
			return errors.New("node scope requires scope_id")
		}
		if _, err := s.ensureNodeInTenant(ctx, tenantID, scopeID); err != nil {
			return err
		}
	case storage.AssignmentScopeCluster:
		if params.ScopeID == uuid.Nil {
			return errors.New("cluster scope requires scope_id")
		}
		cluster, err := s.store.GetClusterByID(ctx, params.ScopeID)
		if err != nil {
			return fmt.Errorf("lookup cluster scope: %w", err)
		}
		if cluster == nil || cluster.TenantID != tenantID {
			return errors.New("cluster scope is outside assignment tenant")
		}
	case storage.AssignmentScopeHypervisorHost:
		if params.ScopeID == uuid.Nil {
			return errors.New("hypervisor_host scope requires scope_id")
		}
		host, err := s.store.GetHypervisorHost(ctx, params.ScopeID)
		if err != nil {
			return fmt.Errorf("lookup hypervisor scope: %w", err)
		}
		if host == nil || host.TenantID != tenantID {
			return errors.New("hypervisor_host scope is outside assignment tenant")
		}
	case storage.AssignmentScopeEnrollmentToken:
		if params.ScopeID == uuid.Nil {
			return errors.New("enrollment_token scope requires scope_id")
		}
		token, err := s.store.GetEnrollmentToken(ctx, params.ScopeID)
		if err != nil {
			return fmt.Errorf("lookup enrollment token scope: %w", err)
		}
		if token == nil || token.TenantID != tenantID {
			return errors.New("enrollment_token scope is outside assignment tenant")
		}
	case storage.AssignmentScopeLabelSelector:
		if params.ScopeID != uuid.Nil || params.NodeID != uuid.Nil {
			return errors.New("label_selector scope cannot include node_id or scope_id")
		}
		if len(params.Selector) == 0 {
			return errors.New("label_selector scope requires selector")
		}
	default:
		return fmt.Errorf("unsupported scope_type %q", params.ScopeType)
	}
	return nil
}

func (s *Server) handleListPolicyAssignments(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.requirePolicyAccess(w, r, principal, policyID, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	assignments, total, err := s.store.ListPolicyAssignments(r.Context(), policyID, limit, offset)
	if err != nil {
		s.logger.Error("list policy assignments", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	items := make([]policyAssignmentResponse, 0, len(assignments))
	for _, a := range assignments {
		items = append(items, newPolicyAssignmentResponse(&a))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

func (s *Server) handleDeletePolicyAssignment(w http.ResponseWriter, r *http.Request, policyID, assignmentID uuid.UUID) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	assignment, err := s.store.GetPolicyAssignment(r.Context(), assignmentID)
	if err != nil {
		s.logger.Error("get policy assignment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if assignment == nil || assignment.PolicyID != policyID {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, assignment.TenantID, roleAdmin) {
		return
	}

	if err := s.store.DeletePolicyAssignment(r.Context(), assignmentID); err != nil {
		s.logger.Error("delete policy assignment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	var nodeFilter *uuid.UUID
	if assignment.NodeID != uuid.Nil {
		n := assignment.NodeID
		nodeFilter = &n
	}
	s.emitPolicyUpdatedTargeted(assignment.TenantID, policyID, "unassign", nodeFilter)
}

func newPolicyAssignmentResponse(a *storage.PolicyAssignment) policyAssignmentResponse {
	resp := policyAssignmentResponse{
		ID:         a.ID.String(),
		PolicyID:   a.PolicyID.String(),
		TenantID:   a.TenantID.String(),
		ScopeType:  a.ScopeType,
		Selector:   a.Selector,
		AssignedAt: formatTime(a.AssignedAt),
	}
	if resp.ScopeType == "" {
		if a.NodeID != uuid.Nil {
			resp.ScopeType = storage.AssignmentScopeNode
		} else {
			resp.ScopeType = storage.AssignmentScopeTenant
		}
	}
	if resp.Selector == nil {
		resp.Selector = map[string]any{}
	}
	if a.NodeID != uuid.Nil {
		nid := a.NodeID.String()
		resp.NodeID = &nid
	}
	if a.ScopeID != uuid.Nil {
		sid := a.ScopeID.String()
		resp.ScopeID = &sid
	}
	if a.AssignedBy != nil {
		aid := a.AssignedBy.String()
		resp.AssignedBy = &aid
	}
	if a.ExpiresAt.Valid {
		resp.ExpiresAt = formatNullTime(a.ExpiresAt)
	}
	return resp
}

func newPolicyVersionResponse(v *storage.PolicyVersion) policyVersionResponse {
	resp := policyVersionResponse{
		ID:             v.ID.String(),
		Version:        v.Version,
		RuleDefinition: v.RuleDefinition,
		Metadata:       v.Metadata,
		CreatedAt:      formatTime(v.CreatedAt),
	}
	if v.Checksum.Valid {
		val := v.Checksum.String
		resp.Checksum = &val
	}
	if v.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	if v.CreatedBy != nil {
		id := v.CreatedBy.String()
		resp.CreatedBy = &id
	}
	if v.PromotedAt.Valid {
		resp.PromotedAt = formatNullTime(v.PromotedAt)
	}
	return resp
}
