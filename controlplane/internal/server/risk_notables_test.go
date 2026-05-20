package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

func TestRiskNotablesAggregatesExistingRiskEvidence(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	alertID := uuid.New()
	findingID := uuid.New()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "payments-db-01",
			State:     storage.NodeStateActive,
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now.Add(-time.Hour),
		}},
		alerts: []storage.Alert{{
			ID:       alertID,
			TenantID: tenantID,
			NodeID:   uuid.NullUUID{UUID: nodeID, Valid: true},
			Source:   "correlation",
			Severity: "high",
			Title:    "Credential attack against public-facing database",
			Summary:  sql.NullString{String: "auth failure spike on database service", Valid: true},
			State:    "open",
			Context: map[string]any{
				"source_ip": "203.0.113.20",
				"disposition": map[string]any{
					"value":  "accepted_risk",
					"reason": "approved test window",
				},
			},
			OpenedAt: now.Add(-time.Hour),
		}},
		ipBehaviorFindings: []storage.IPBehaviorFinding{{
			ID:          findingID,
			TenantID:    tenantID,
			NodeID:      uuid.NullUUID{UUID: nodeID, Valid: true},
			DedupKey:    "203.0.113.20|credential_attack",
			SourceIP:    sql.NullString{String: "203.0.113.20", Valid: true},
			Category:    "credential_attack",
			Severity:    "critical",
			Score:       97,
			Status:      "open",
			Reason:      "credential behavior exceeded learned baseline",
			Evidence:    map[string]any{"auth_failures": 42},
			FirstSeenAt: now.Add(-2 * time.Hour),
			LastSeenAt:  now.Add(-30 * time.Minute),
			CreatedAt:   now.Add(-2 * time.Hour),
			UpdatedAt:   now.Add(-30 * time.Minute),
		}},
	}
	srv := buildHeartbeatServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risk/notables?tenant_id="+tenantID.String()+"&since=2026-05-18T12:00:00Z&until=2026-05-19T12:00:00Z", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "user",
		Name:  "viewer",
		Roles: []string{roleViewer},
	}))
	rec := httptest.NewRecorder()

	srv.handleRiskNotables(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp riskNotablesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Notables) != 2 {
		t.Fatalf("notables = %d, want 2: %#v", len(resp.Notables), resp.Notables)
	}
	if resp.Summary.Critical != 1 || resp.Summary.High != 0 || resp.Summary.Medium != 1 || resp.Summary.BySource["ip_behavior"] != 1 || resp.Summary.BySource["alert"] != 1 {
		t.Fatalf("unexpected summary: %#v", resp.Summary)
	}
	first := resp.Notables[0]
	if first.SourceType != "ip_behavior" || first.RiskScore != 97 || first.EntityType != "ip" {
		t.Fatalf("expected IP behavior first by risk, got %#v", first)
	}
	if len(first.MITRE) == 0 || first.MITRE[0].Technique != "T1110" {
		t.Fatalf("expected credential MITRE mapping, got %#v", first.MITRE)
	}
	var alertNotable *riskNotable
	for i := range resp.Notables {
		if resp.Notables[i].SourceType == "alert" {
			alertNotable = &resp.Notables[i]
			break
		}
	}
	if alertNotable == nil || alertNotable.Disposition != "accepted_risk" || !containsString(alertNotable.DispositionActions, "alert_disposition") {
		t.Fatalf("expected alert disposition feedback in notable, got %#v", alertNotable)
	}
	if alertNotable.State != "accepted_risk" || alertNotable.RiskScore > 55 || alertNotable.RiskLevel != "medium" {
		t.Fatalf("expected accepted-risk cap on notable risk, got %#v", alertNotable)
	}
	if len(resp.Citations) < 2 {
		t.Fatalf("expected citations for source evidence, got %#v", resp.Citations)
	}
}

func TestRiskNotablesDispositionFeedbackCapsClosedRisk(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	alertID := uuid.New()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		alerts: []storage.Alert{{
			ID:       alertID,
			TenantID: tenantID,
			Source:   "correlation",
			Severity: "critical",
			Title:    "Scanner maintenance window",
			State:    "resolved",
			Context: map[string]any{
				"disposition": map[string]any{
					"value":  "false_positive",
					"reason": "approved scanner maintenance",
				},
			},
			OpenedAt: now.Add(-time.Hour),
		}},
	}
	srv := buildHeartbeatServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risk/notables?tenant_id="+tenantID.String()+"&since=2026-05-18T12:00:00Z&until=2026-05-19T12:00:00Z", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "user",
		Name:  "viewer",
		Roles: []string{roleViewer},
	}))
	rec := httptest.NewRecorder()

	srv.handleRiskNotables(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp riskNotablesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Notables) != 1 {
		t.Fatalf("notables = %d, want 1: %#v", len(resp.Notables), resp.Notables)
	}
	notable := resp.Notables[0]
	if notable.Disposition != "false_positive" || notable.State != "closed" || notable.RiskScore > 5 || notable.RiskLevel != "low" {
		t.Fatalf("expected false-positive risk cap, got %#v", notable)
	}
	if resp.Summary.Critical != 0 || resp.Summary.High != 0 || resp.Summary.Low != 1 {
		t.Fatalf("unexpected summary after disposition feedback: %#v", resp.Summary)
	}
	if !containsString(resp.Guardrails, "disposition_feedback_applied") {
		t.Fatalf("expected disposition feedback guardrail, got %#v", resp.Guardrails)
	}
}

func TestRiskNotablesNodeScopeRejectsInvalidNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	srv := buildHeartbeatServer(t, &fakeStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risk/notables?tenant_id="+tenantID.String()+"&node_id=not-a-uuid", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "user",
		Name:  "viewer",
		Roles: []string{roleViewer},
	}))
	rec := httptest.NewRecorder()

	srv.handleRiskNotables(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRiskNotablesRejectsCrossTenantNodeHTTP(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := buildHeartbeatServer(t, &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risk/notables?tenant_id="+tenantID.String()+"&node_id="+nodeID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "user",
		Name:  "viewer",
		Roles: []string{roleViewer},
	}))
	rec := httptest.NewRecorder()

	srv.handleRiskNotables(rec, req)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRiskNotablesAIToolRejectsCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "risk_notables", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}
