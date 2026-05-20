package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	cpCompliance "github.com/CloudSpaceLab/control_one/controlplane/internal/compliance"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

type complianceEvaluateRequest struct {
	NodeID         string            `json:"node_id"`
	Region         string            `json:"region"`
	RuleSets       []string          `json:"rulesets"`
	Certifications []string          `json:"certifications"`
	Policies       map[string]string `json:"policies"`
	AutoApply      bool              `json:"auto_apply"`
	UseRealScan    bool              `json:"use_real_scan"`
}

func (r complianceEvaluateRequest) validate() error {
	if strings.TrimSpace(r.NodeID) == "" {
		return fmt.Errorf("node_id is required")
	}
	if _, err := uuid.Parse(r.NodeID); err != nil {
		return fmt.Errorf("node_id must be a valid UUID")
	}
	if strings.TrimSpace(r.Region) == "" {
		return fmt.Errorf("region is required")
	}
	if len(r.RuleSets) == 0 {
		return fmt.Errorf("at least one ruleset is required")
	}
	for i, rule := range r.RuleSets {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			return fmt.Errorf("rulesets[%d] cannot be empty", i)
		}
		r.RuleSets[i] = rule
	}
	return nil
}

type complianceEvaluateResponse struct {
	Results  []compliance.Result `json:"results"`
	Metadata map[string]any      `json:"metadata,omitempty"`
}

func (s *Server) handleComplianceEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	var req complianceEvaluateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	nodeID, _ := uuid.Parse(req.NodeID)
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get compliance evaluation node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, node.TenantID, roleOperator, roleAdmin) {
		return
	}

	results, err := s.evaluateComplianceReal(r.Context(), req)
	if err != nil {
		s.logger.Error("compliance evaluation", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := complianceEvaluateResponse{Results: results}
	// evaluateComplianceReal returns nil results only when the node has no
	// matching policies; the UI uses no_policies_assigned to render an empty
	// state instead of fabricated zeros.
	if len(results) == 0 {
		resp.Metadata = map[string]any{"no_policies_assigned": true}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode compliance evaluate response", zap.Error(err))
	}
}

// evaluateComplianceReal performs policy-based evaluation when the node's
// tenant has policies assigned. When no policies are matched, it returns nil
// results (no synthetic fallback) — callers infer no_policies_assigned from
// len(results) == 0 and surface that to the user instead of fabricating data.
func (s *Server) evaluateComplianceReal(ctx context.Context, req complianceEvaluateRequest) ([]compliance.Result, error) {
	nodeID, _ := uuid.Parse(req.NodeID)

	if s.store == nil {
		return nil, nil
	}

	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		s.logger.Warn("get node for compliance eval", zap.Error(err))
	}

	var tenantID uuid.UUID
	if node != nil {
		tenantID = node.TenantID
	}
	if tenantID == uuid.Nil {
		return nil, nil
	}

	policies, err := s.store.GetEffectivePolicies(ctx, tenantID, nodeID)
	if err != nil {
		s.logger.Warn("get effective policies", zap.Error(err))
	}
	if len(policies) == 0 {
		return nil, nil
	}

	return s.evaluateWithPolicies(ctx, req, policies, node)
}

func (s *Server) evaluateWithPolicies(ctx context.Context, req complianceEvaluateRequest, policies []storage.PolicyWithVersion, node *storage.Node) ([]compliance.Result, error) {
	evaluator := cpCompliance.NewJSONDSLEvaluator()

	nodeMeta := map[string]any{}
	if node != nil {
		nodeMeta["id"] = node.ID.String()
		nodeMeta["hostname"] = node.Hostname
		if node.OS.Valid {
			nodeMeta["os"] = node.OS.String
		}
		if node.Arch.Valid {
			nodeMeta["arch"] = node.Arch.String
		}
		if node.PublicIP.Valid {
			nodeMeta["public_ip"] = node.PublicIP.String
		}
	}

	// Pass request policies as facts for backward compat
	facts := map[string]any{}
	for k, v := range req.Policies {
		facts[k] = v
	}

	input := cpCompliance.EvalInput{
		NodeID:   uuid.MustParse(req.NodeID),
		NodeMeta: nodeMeta,
		Facts:    facts,
	}
	if node != nil {
		input.TenantID = node.TenantID
	}

	var results []compliance.Result
	for _, p := range policies {
		ruleDef := cpCompliance.RuleDefinition{
			ID:         p.ID.String(),
			RuleType:   p.RuleType,
			Definition: p.RuleDefinition,
			Severity:   "medium",
			Framework:  "",
		}

		evalResult, err := evaluator.Evaluate(ctx, ruleDef, input)
		if err != nil {
			s.logger.Warn("evaluate policy",
				zap.Error(err),
				zap.String("policy_id", p.ID.String()),
				zap.String("policy_name", p.Name),
			)
			results = append(results, compliance.Result{
				RuleID:    p.ID.String(),
				Passed:    false,
				Severity:  "high",
				Details:   fmt.Sprintf("evaluation error for policy %s: %v", p.Name, err),
				CheckedAt: time.Now().UTC(),
				Evidence: map[string]any{
					"evidence_contract": complianceEvidenceContractVersion,
					"rule_id":           p.ID.String(),
					"policy_name":       p.Name,
					"error":             err.Error(),
				},
			})
			continue
		}

		evidence, _ := normalizeComplianceEvidence(evalResult.Evidence, "")
		evidenceMap, _ := evidence.(map[string]any)
		results = append(results, compliance.Result{
			RuleID:      p.ID.String(),
			Passed:      evalResult.Passed,
			Severity:    evalResult.Severity,
			Details:     evalResult.Details,
			CheckedAt:   evalResult.CheckedAt,
			Remediation: evalResult.Remediation,
			Evidence:    evidenceMap,
		})
	}

	return results, nil
}

// handleFirstScanHook is called from every code path that persists compliance
// results. It idempotently stamps nodes.first_scan_at and — if the node already
// has a heartbeat — flips it from enrollment_pending to active. Safe to call
// repeatedly; MarkNodeFirstScan uses COALESCE so only the first call mutates.
func (s *Server) handleFirstScanHook(ctx context.Context, nodeID uuid.UUID) {
	if s.store == nil || nodeID == uuid.Nil {
		return
	}
	node, err := s.store.MarkNodeFirstScan(ctx, nodeID)
	if err != nil {
		s.logger.Warn("mark first scan", zap.String("node_id", nodeID.String()), zap.Error(err))
		return
	}
	s.maybeActivatePendingNode(ctx, node)
}

type batchScanRequest struct {
	TenantID *string  `json:"tenant_id"`
	NodeIDs  []string `json:"node_ids"`
}

type batchScanResponse struct {
	JobIDs []string `json:"job_ids"`
	Count  int      `json:"count"`
}

func (s *Server) handleComplianceBatchScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	var req batchScanRequest
	if r.ContentLength > 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}

	var tenantID uuid.UUID
	if req.TenantID != nil {
		parsed, err := uuid.Parse(*req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}
	if tenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
		return
	}

	var nodeIDs []uuid.UUID
	for _, nidStr := range req.NodeIDs {
		nid, err := uuid.Parse(nidStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid node_id %q", nidStr), http.StatusBadRequest)
			return
		}
		if _, err := s.ensureNodeInTenant(r.Context(), tenantID, nid); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		nodeIDs = append(nodeIDs, nid)
	}

	if s.complianceScheduler == nil {
		s.complianceScheduler = NewComplianceScheduler(s)
	}

	jobIDs, err := s.complianceScheduler.createScanJobs(r.Context(), tenantID, nodeIDs)
	if err != nil {
		s.logger.Error("batch compliance scan", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := batchScanResponse{Count: len(jobIDs)}
	for _, id := range jobIDs {
		resp.JobIDs = append(resp.JobIDs, id.String())
	}
	if resp.JobIDs == nil {
		resp.JobIDs = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode batch scan response", zap.Error(err))
	}
}
