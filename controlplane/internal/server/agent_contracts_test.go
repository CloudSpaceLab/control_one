package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	agentpolicy "github.com/CloudSpaceLab/control_one/internal/policy"
	"github.com/CloudSpaceLab/control_one/internal/scanner"
)

func TestAgentPolicySetReturnsNodeScopedCommandPolicies(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	commandPolicyID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "agent-1"}},
		effectivePolicies: []storage.PolicyWithVersion{
			{
				Policy: storage.Policy{
					ID:       uuid.New(),
					TenantID: tenantID,
					Name:     "server evaluated",
					RuleType: "json-dsl",
					Enabled:  true,
					Labels:   map[string]string{"severity": "critical"},
				},
				Version:        1,
				RuleDefinition: `{"severity":"critical","conditions":[{"field":"facts.os","op":"eq","value":"linux"}]}`,
				VersionID:      uuid.New(),
			},
			{
				Policy: storage.Policy{
					ID:       commandPolicyID,
					TenantID: tenantID,
					Name:     "agent command",
					RuleType: "shell",
					Enabled:  true,
					Labels:   map[string]string{"severity": "high"},
				},
				Version:        3,
				RuleDefinition: "echo ok",
				VersionID:      uuid.New(),
			},
		},
	}
	srv := &Server{store: store, logger: zap.NewNop(), cfg: &config.Config{}}
	req := agentRequest(http.MethodGet, "/api/v1/policies?node_id="+nodeID.String(), nodeID)
	rec := httptest.NewRecorder()

	srv.handlePoliciesCollection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp struct {
		Policies []agentpolicy.Rule `json:"policies"`
		Version  string             `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Policies) != 1 {
		t.Fatalf("policies=%#v, want only the explicit agent command policy", resp.Policies)
	}
	if resp.Policies[0].ID != commandPolicyID.String() || resp.Policies[0].Check != "echo ok" || resp.Policies[0].Version != "3" {
		t.Fatalf("unexpected policy rule: %#v", resp.Policies[0])
	}
	if resp.Version == "" {
		t.Fatalf("expected stable policy version")
	}
}

func TestAgentComplianceReportPersistsResultsAndMarksFirstScan(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	checkedAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "agent-1"}}}
	srv := &Server{store: store, logger: zap.NewNop(), cfg: &config.Config{}}
	body, _ := json.Marshal(map[string]any{
		"node_id":   nodeID.String(),
		"timestamp": checkedAt.Format(time.RFC3339Nano),
		"summary":   map[string]any{"total": 1, "non_compliant": 1},
		"results": []scanner.Result{{
			RuleID:    "cis.ssh.root_login",
			Status:    scanner.StatusNonCompliant,
			Details:   "PermitRootLogin yes",
			CheckedAt: checkedAt,
			Metadata:  map[string]string{"severity": "high"},
		}},
	})
	req := agentRequest(http.MethodPost, "/api/v1/compliance/report", nodeID)
	req.Body = ioNopCloser{Reader: bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()

	srv.handleAgentComplianceReport(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if len(store.jobs) != 1 {
		t.Fatalf("jobs=%d, want 1", len(store.jobs))
	}
	var stored []storage.ComplianceResult
	for _, rows := range store.complianceResults {
		stored = append(stored, rows...)
	}
	if len(stored) != 1 {
		t.Fatalf("stored results=%#v, want 1", stored)
	}
	if stored[0].RuleID != "cis.ssh.root_login" || stored[0].Passed {
		t.Fatalf("unexpected stored result: %#v", stored[0])
	}
	if stored[0].Severity == nil || *stored[0].Severity != "high" {
		t.Fatalf("severity=%v, want high", stored[0].Severity)
	}
	node, _ := store.GetNode(context.Background(), nodeID)
	if node == nil || node.FirstScanAt == nil {
		t.Fatalf("first scan was not marked: %#v", node)
	}
}

func TestAgentComplianceEvaluatePersistsServerEvaluatedResults(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	policyID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "agent-1"}},
		effectivePolicies: []storage.PolicyWithVersion{{
			Policy: storage.Policy{
				ID:       policyID,
				TenantID: tenantID,
				Name:     "ssh-root-login-disabled",
				RuleType: "json-dsl",
				Enabled:  true,
				Labels:   map[string]string{"severity": "high"},
			},
			Version: 1,
			RuleDefinition: `{
				"severity":"high",
				"description":"SSH root login disabled",
				"conditions":[{"field":"facts.ssh.root_login_disabled","op":"eq","value":"true"}],
				"remediation":"Disable PermitRootLogin"
			}`,
			VersionID: uuid.New(),
		}},
	}
	srv := &Server{store: store, logger: zap.NewNop(), cfg: &config.Config{}}
	body, _ := json.Marshal(map[string]any{
		"node_id":  nodeID.String(),
		"region":   "local",
		"rulesets": []string{"default"},
		"policies": map[string]string{"ssh.root_login_disabled": "true"},
	})
	req := agentRequest(http.MethodPost, "/api/v1/compliance/evaluate", nodeID)
	req.Body = ioNopCloser{Reader: bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()

	srv.handleComplianceEvaluate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp complianceEvaluateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Results) != 1 || !resp.Results[0].Passed {
		t.Fatalf("results=%#v, want one passing server-evaluated result", resp.Results)
	}
	var stored []storage.ComplianceResult
	for _, rows := range store.complianceResults {
		stored = append(stored, rows...)
	}
	if len(store.jobs) != 1 || len(stored) != 1 {
		t.Fatalf("jobs=%d stored=%#v, want one job and one result", len(store.jobs), stored)
	}
	if stored[0].RuleID != policyID.String() || !stored[0].Passed {
		t.Fatalf("unexpected persisted result: %#v", stored[0])
	}
}

func TestMeshAgentCompatibilityRoutes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "agent-1"}}}
	srv := &Server{store: store, logger: zap.NewNop(), cfg: &config.Config{}}

	peersReq := agentRequest(http.MethodGet, "/api/v1/mesh/peers?namespace=default&node_id="+nodeID.String(), nodeID)
	peersRec := httptest.NewRecorder()
	srv.handleMeshPeers(peersRec, peersReq)
	if peersRec.Code != http.StatusOK {
		t.Fatalf("peers status=%d body=%s, want 200", peersRec.Code, peersRec.Body.String())
	}
	var peersResp struct {
		Peers []meshPeerResponse `json:"peers"`
	}
	if err := json.Unmarshal(peersRec.Body.Bytes(), &peersResp); err != nil {
		t.Fatalf("decode peers response: %v", err)
	}
	if len(peersResp.Peers) != 0 {
		t.Fatalf("peers=%#v, want empty compatibility set", peersResp.Peers)
	}

	rotateBody, _ := json.Marshal(map[string]string{"node_id": nodeID.String(), "namespace": "default"})
	rotateReq := agentRequest(http.MethodPost, "/api/v1/mesh/rotate", nodeID)
	rotateReq.Body = ioNopCloser{Reader: bytes.NewReader(rotateBody)}
	rotateReq.ContentLength = int64(len(rotateBody))
	rotateRec := httptest.NewRecorder()
	srv.handleMeshRotate(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s, want 200", rotateRec.Code, rotateRec.Body.String())
	}
	var rotateResp map[string]any
	if err := json.Unmarshal(rotateRec.Body.Bytes(), &rotateResp); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if rotateResp["rotation_noop"] != true {
		t.Fatalf("rotation_noop=%v, want true", rotateResp["rotation_noop"])
	}
}

func agentRequest(method, target string, nodeID uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:    "agent",
		Name:    nodeID.String(),
		Subject: nodeID.String(),
		Roles:   []string{"agent"},
	}))
}

type ioNopCloser struct {
	*bytes.Reader
}

func (c ioNopCloser) Close() error { return nil }
