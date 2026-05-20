package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestIncidentCreateToolPersistsTenantScopedInvestigation(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	srv := &Server{store: store}
	srv.aiClock = func() time.Time { return time.Date(2026, 5, 19, 13, 0, 0, 0, time.UTC) }

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator}},
		tenantID,
		llm.ToolCall{Name: "incident_create", Input: map[string]any{
			"node_id":            nodeID.String(),
			"summary":            "Possible credential theft from bastion timeline",
			"severity":           "high",
			"trigger_event_type": "credential.suspicious_login",
			"dedup_key":          "incident:credential:suspicious-login",
			"citations":          []any{"events:row-1", "timeline:row-2"},
			"evidence":           map[string]any{"risk_score": 91},
		}},
	)
	if err != nil {
		t.Fatalf("execute incident_create: %v", err)
	}
	if len(store.aiInvestigations) != 1 {
		t.Fatalf("expected one persisted investigation, got %+v", store.aiInvestigations)
	}
	row := store.aiInvestigations[0]
	if row.TenantID != tenantID || row.NodeID != nodeID || row.Severity != "high" || row.TriggerType != "incident" {
		t.Fatalf("unexpected investigation row: %+v", row)
	}
	var evidence map[string]any
	if err := json.Unmarshal(row.Evidence, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence["source_tool"] != "incident_create" || !strings.Contains(string(row.Evidence), "events:row-1") {
		t.Fatalf("expected cited incident evidence, got %s", string(row.Evidence))
	}
	resp, ok := exec.Payload.(incidentCreateToolResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.IncidentID != row.ID.String() || len(resp.Citations) != 1 || resp.Citations[0].Table != "ai_investigations" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if exec.Citation.Tool != "incident_create" {
		t.Fatalf("unexpected citation: %#v", exec.Citation)
	}
}

func TestIncidentCreateToolRejectsViewerAndCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "incident_create", Input: map[string]any{"summary": "viewer should not create incidents"}},
	)
	if err == nil || !strings.Contains(err.Error(), "requires role operator") {
		t.Fatalf("expected viewer rejection, got %v", err)
	}

	_, err = srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator}},
		tenantID,
		llm.ToolCall{Name: "incident_create", Input: map[string]any{"summary": "cross tenant", "node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}

func TestCaseNoteAddToolAnnotatesIncidentCreatedByAITool(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	srv := &Server{store: store}
	principal := &auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator, roleInvestigator}}

	created, err := srv.executeAITool(
		context.Background(),
		principal,
		tenantID,
		llm.ToolCall{Name: "incident_create", Input: map[string]any{
			"node_id":   nodeID.String(),
			"summary":   "Suspicious DB query sequence",
			"severity":  "high",
			"citations": []any{"events:row-1"},
		}},
	)
	if err != nil {
		t.Fatalf("execute incident_create: %v", err)
	}
	incident := created.Payload.(incidentCreateToolResponse)

	exec, err := srv.executeAITool(
		context.Background(),
		principal,
		tenantID,
		llm.ToolCall{Name: "case_note_add", Input: map[string]any{
			"case_id":   incident.IncidentID,
			"note":      "Confirmed the timeline supports escalation.",
			"citations": []any{"events:row-1"},
		}},
	)
	if err != nil {
		t.Fatalf("execute case_note_add: %v", err)
	}
	if len(store.auditLogs) != 1 {
		t.Fatalf("expected one SOC note audit row, got %+v", store.auditLogs)
	}
	audit := store.auditLogs[0]
	if audit.Action != "soc.case.note.add" || audit.ResourceID == nil || *audit.ResourceID != incident.IncidentID {
		t.Fatalf("unexpected SOC note audit row: %+v", audit)
	}
	resp, ok := exec.Payload.(caseNoteAddToolResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.CaseID != incident.IncidentID || resp.AuditID != audit.ID.String() {
		t.Fatalf("unexpected note response: %+v", resp)
	}
}

func TestCaseNoteAddToolRejectsUnlinkedIncidentCitation(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	srv := &Server{store: store}
	principal := &auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator, roleInvestigator}}

	created, err := srv.executeAITool(
		context.Background(),
		principal,
		tenantID,
		llm.ToolCall{Name: "incident_create", Input: map[string]any{
			"summary":   "Suspicious timeline",
			"citations": []any{"events:row-1"},
		}},
	)
	if err != nil {
		t.Fatalf("execute incident_create: %v", err)
	}
	incident := created.Payload.(incidentCreateToolResponse)

	_, err = srv.executeAITool(
		context.Background(),
		principal,
		tenantID,
		llm.ToolCall{Name: "case_note_add", Input: map[string]any{
			"case_id":   incident.IncidentID,
			"note":      "forged citation",
			"citations": []any{"events:row-forged"},
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "citation is not linked") {
		t.Fatalf("expected unlinked citation rejection, got %v", err)
	}
	if len(store.auditLogs) != 0 {
		t.Fatalf("unexpected persisted note for rejected citation: %+v", store.auditLogs)
	}
}

func TestHuntSaveToolPersistsSavedSearchWithOwnerAndFilters(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	userID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	store.users = map[string]*storage.User{
		"operator-subject": {ID: userID, ExternalID: "operator-subject"},
	}
	srv := &Server{store: store}
	srv.aiClock = func() time.Time { return time.Date(2026, 5, 19, 13, 30, 0, 0, time.UTC) }

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "operator-subject", Roles: []string{roleOperator}},
		tenantID,
		llm.ToolCall{Name: "hunt_save", Input: map[string]any{
			"name":        "Failed privileged logins",
			"query":       "event_type:auth.failure user:root",
			"entity_type": "user",
			"shared":      true,
			"filters":     map[string]any{"severity": "high"},
			"citations":   []any{"events:auth-row-1"},
			"since":       "2026-05-19T00:00:00Z",
			"until":       "2026-05-19T13:00:00Z",
		}},
	)
	if err != nil {
		t.Fatalf("execute hunt_save: %v", err)
	}
	if len(store.savedSearches) != 1 {
		t.Fatalf("expected one saved hunt, got %+v", store.savedSearches)
	}
	row := store.savedSearches[0]
	if row.TenantID != tenantID || row.OwnerUserID != userID || row.Name != "Failed privileged logins" || !row.Shared {
		t.Fatalf("unexpected saved search row: %+v", row)
	}
	if !strings.Contains(string(row.Filters), "events:auth-row-1") || !strings.Contains(string(row.Filters), "hunt_save") {
		t.Fatalf("expected filter metadata and citations, got %s", string(row.Filters))
	}
	resp, ok := exec.Payload.(huntSaveToolResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.HuntID != row.ID.String() || resp.OwnerID != userID.String() || len(resp.Citations) != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestCaseNoteAddToolWritesAuditAndEnforcesTenant(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	caseID := uuid.New()
	userID := uuid.New()
	store := &fakeStore{
		users: map[string]*storage.User{
			"investigator-subject": {ID: userID, ExternalID: "investigator-subject"},
		},
		misconductCases: map[uuid.UUID]*storage.MisconductCase{
			caseID: {ID: caseID, TenantID: tenantID, Status: "open", Summary: "privileged access review"},
		},
		auditLogs: []storage.AuditLog{},
	}
	srv := &Server{store: store}

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "investigator-subject", Roles: []string{roleInvestigator}},
		tenantID,
		llm.ToolCall{Name: "case_note_add", Input: map[string]any{
			"case_id":   caseID.String(),
			"note":      "Linked auth failure timeline to credential rotation decision.",
			"citations": []any{"timeline:auth:1", "compliance_evidence:e1"},
		}},
	)
	if err != nil {
		t.Fatalf("execute case_note_add: %v", err)
	}
	if len(store.auditLogs) != 1 {
		t.Fatalf("expected one audit note, got %+v", store.auditLogs)
	}
	audit := store.auditLogs[0]
	if audit.Action != "misconduct.case.note.add" || audit.ResourceID == nil || *audit.ResourceID != caseID.String() || audit.ActorID != userID {
		t.Fatalf("unexpected audit row: %+v", audit)
	}
	if audit.Metadata["note"] == "" || !strings.Contains(strings.Join(stringsFromMetadata(audit.Metadata, "citations"), ","), "timeline:auth:1") {
		t.Fatalf("expected note metadata and citations, got %+v", audit.Metadata)
	}
	resp, ok := exec.Payload.(caseNoteAddToolResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.AuditID != audit.ID.String() || len(resp.Citations) != 1 || resp.Citations[0].Table != "audit_logs" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	_, err = srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "investigator-subject", Roles: []string{roleInvestigator}},
		otherTenantID,
		llm.ToolCall{Name: "case_note_add", Input: map[string]any{"case_id": caseID.String(), "note": "cross tenant"}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant case rejection, got %v", err)
	}
}
