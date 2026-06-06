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

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestSOCCasesListDetailAndAuditExport(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	evidence := map[string]any{
		"source_tool": "incident_create",
		"citations": []string{
			citationID("node_vulnerability_findings", uuid.NewString()),
			citationID("normalized_events", "doris-row-1"),
		},
		"details": map[string]any{"risk_score": 91},
	}
	rawEvidence, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	store := &fakeStore{
		nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "soc-app"}},
	}
	row, err := store.CreateAIInvestigation(context.Background(), storage.CreateAIInvestigationParams{
		TenantID:         tenantID,
		NodeID:           nodeID,
		TriggerType:      "incident",
		TriggerEventType: "vulnerability.patch",
		TriggerDedupKey:  "soc-case-1",
		Severity:         "critical",
		Summary:          "Critical exploitable package on production node",
		Evidence:         rawEvidence,
		Status:           storage.AIInvestigationStatusOpen,
	})
	if err != nil {
		t.Fatalf("seed ai investigation: %v", err)
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens(roleInvestigator, "investigator-token"),
	}, store, &stubQueue{})
	defer func() { _ = srv.Stop(context.Background()) }()

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/soc/cases?tenant_id="+tenantID.String(), nil)
	listReq.Header.Set("Authorization", "Bearer investigator-token")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var list paginatedResponse[socCaseResponse]
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Pagination.Total != 1 || len(list.Data) != 1 {
		t.Fatalf("unexpected list: %+v", list)
	}
	item := list.Data[0]
	if item.CaseID != row.ID.String() || item.NodeID != nodeID.String() || item.Severity != "critical" {
		t.Fatalf("case summary lost scope/severity: %+v", item)
	}
	if len(item.Citations) != 1 || len(item.EvidenceRefs) < 3 || !contains(item.ExportURL, "tenant_id="+tenantID.String()) {
		t.Fatalf("case missing citations/evidence/export URL: %+v", item)
	}

	notePayload := `{"note":"Confirmed the vulnerable package is reachable through the public app path.","citations":["` + item.EvidenceRefs[0].ID + `"]}`
	noteReq := httptest.NewRequest(http.MethodPost, "/api/v1/soc/cases/"+row.ID.String()+"/notes?tenant_id="+tenantID.String(), bytes.NewBufferString(notePayload))
	noteReq.Header.Set("Authorization", "Bearer investigator-token")
	noteRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(noteRec, noteReq)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("note status=%d body=%s", noteRec.Code, noteRec.Body.String())
	}
	var note socCaseNoteResponse
	if err := json.Unmarshal(noteRec.Body.Bytes(), &note); err != nil {
		t.Fatalf("decode note: %v", err)
	}
	if note.CaseID != row.ID.String() || note.Note == "" || len(note.Citations) != 1 {
		t.Fatalf("unexpected note response: %+v", note)
	}

	listWithNotesReq := httptest.NewRequest(http.MethodGet, "/api/v1/soc/cases?tenant_id="+tenantID.String()+"&include_notes=true", nil)
	listWithNotesReq.Header.Set("Authorization", "Bearer investigator-token")
	listWithNotesRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listWithNotesRec, listWithNotesReq)
	if listWithNotesRec.Code != http.StatusOK {
		t.Fatalf("list with notes status=%d body=%s", listWithNotesRec.Code, listWithNotesRec.Body.String())
	}
	var listWithNotes paginatedResponse[socCaseResponse]
	if err := json.Unmarshal(listWithNotesRec.Body.Bytes(), &listWithNotes); err != nil {
		t.Fatalf("decode list with notes: %v", err)
	}
	if len(listWithNotes.Data) != 1 || len(listWithNotes.Data[0].Notes) != 1 || listWithNotes.Data[0].Notes[0].AuditID != note.AuditID {
		t.Fatalf("list with notes did not hydrate analyst note: %+v", listWithNotes)
	}

	exportReq := httptest.NewRequest(http.MethodGet, item.ExportURL, bytes.NewReader(nil))
	exportReq.Header.Set("Authorization", "Bearer investigator-token")
	exportRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRec.Code, exportRec.Body.String())
	}
	var export socCaseExportResponse
	if err := json.Unmarshal(exportRec.Body.Bytes(), &export); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if export.ExportVersion != "soc-case-export-v1" || export.Case.CaseID != row.ID.String() {
		t.Fatalf("unexpected export envelope: %+v", export)
	}
	if len(export.Evidence) < 3 || !contains(stringsForCaseGuardrails(export.Guardrails), "source_row_citations") {
		t.Fatalf("export missing evidence/guardrails: %+v", export)
	}
	if export.Case.Evidence != nil || contains(exportRec.Body.String(), "risk_score") {
		t.Fatalf("export must carry evidence references without raw evidence details: %s", exportRec.Body.String())
	}
	if len(export.Notes) != 1 || export.Notes[0].AuditID != note.AuditID {
		t.Fatalf("export missing analyst note: %+v", export.Notes)
	}
}

func TestSOCCaseDerivesEvidenceAndTimelineFromAnomalyRows(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	evidence := map[string]any{
		"event_id":       "event-7",
		"type":           "conn.open",
		"ts":             "2026-05-21T07:16:00Z",
		"node_id":        nodeID.String(),
		"conn_id":        "conn-7",
		"src_ip":         "102.89.68.242",
		"process_name":   "nginx",
		"message":        "first connection to 102.89.68.242 by nginx",
		"collector":      "node-agent",
		"correlation_id": "corr-7",
		"details": map[string]any{
			"source_file": "/var/log/nginx/access.log",
			"path":        "/login",
			"status_code": 401,
			"request_id":  "req-7",
		},
	}
	rawEvidence, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	row := storage.AIInvestigation{
		ID:               uuid.New(),
		TenantID:         tenantID,
		NodeID:           nodeID,
		TriggerType:      "anomaly",
		TriggerEventType: "conn.open",
		TriggerDedupKey:  "first-connection:102.89.68.242",
		Severity:         "low",
		Summary:          "first connection to 102.89.68.242 by nginx",
		Evidence:         rawEvidence,
		Status:           storage.AIInvestigationStatusOpen,
		CreatedAt:        mustParseTime(t, "2026-05-21T07:18:00Z"),
		UpdatedAt:        mustParseTime(t, "2026-05-21T07:18:00Z"),
	}

	resp := newSOCCaseResponse(row)
	refs := stringsForCaseRefs(resp.EvidenceRefs)
	for _, want := range []string{
		"ai_investigations:" + row.ID.String(),
		"nodes:" + nodeID.String(),
		"events:event-7",
		"connections:conn-7",
		"files:/var/log/nginx/access.log",
		"requests:req-7",
	} {
		if !contains(refs, want) {
			t.Fatalf("derived refs missing %q: %s", want, refs)
		}
	}
	timeline := stringsForCaseTimeline(resp.Timeline)
	for _, want := range []string{"signal.observed", "case.created", "node.scoped", "evidence.linked"} {
		if !contains(timeline, want) {
			t.Fatalf("timeline missing %q: %s", want, timeline)
		}
	}
	if resp.CoverageBadges[1].Tone != "healthy" {
		t.Fatalf("expected evidence-linked badge to be healthy: %+v", resp.CoverageBadges)
	}
}

func TestSOCCaseResponseCleansDanglingFirstConnectionProcess(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	evidence := map[string]any{
		"event_id": "event-8",
		"ts":       "2026-06-06T16:05:26Z",
		"node_id":  nodeID.String(),
		"message":  "first connection to 20.169.85.72 by",
	}
	rawEvidence, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	row := storage.AIInvestigation{
		ID:               uuid.New(),
		TenantID:         tenantID,
		NodeID:           nodeID,
		TriggerType:      "anomaly",
		TriggerEventType: "anomaly.new_destination",
		TriggerDedupKey:  "anomaly.new_dst:" + tenantID.String() + ":20.169.85.72",
		Severity:         "low",
		Summary:          "first connection to 20.169.85.72 by",
		Evidence:         rawEvidence,
		Status:           storage.AIInvestigationStatusOpen,
		CreatedAt:        mustParseTime(t, "2026-06-06T16:05:27Z"),
		UpdatedAt:        mustParseTime(t, "2026-06-06T16:05:27Z"),
	}

	resp := newSOCCaseResponse(row)
	if resp.Title != "first connection to 20.169.85.72" || resp.Summary != "first connection to 20.169.85.72" {
		t.Fatalf("case response kept dangling process suffix: title=%q summary=%q", resp.Title, resp.Summary)
	}
	timeline := stringsForCaseTimeline(resp.Timeline)
	if contains(timeline, "first connection to 20.169.85.72 by") {
		t.Fatalf("timeline kept dangling process suffix: %s", timeline)
	}
}

func TestSOCCaseNoteRejectsUnlinkedCitation(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	evidence := map[string]any{
		"citations": []string{citationID("normalized_events", "doris-row-1")},
	}
	rawEvidence, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	store := &fakeStore{}
	row, err := store.CreateAIInvestigation(context.Background(), storage.CreateAIInvestigationParams{
		TenantID:         tenantID,
		TriggerType:      "incident",
		TriggerEventType: "timeline",
		TriggerDedupKey:  "soc-case-citation",
		Severity:         "high",
		Summary:          "Case with a single event citation",
		Evidence:         rawEvidence,
		Status:           storage.AIInvestigationStatusOpen,
	})
	if err != nil {
		t.Fatalf("seed ai investigation: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens(roleInvestigator, "investigator-token"),
	}, store, &stubQueue{})
	defer func() { _ = srv.Stop(context.Background()) }()

	noteReq := httptest.NewRequest(http.MethodPost, "/api/v1/soc/cases/"+row.ID.String()+"/notes?tenant_id="+tenantID.String(), bytes.NewBufferString(`{"note":"bad citation","citations":["node_vulnerability_findings:forged"]}`))
	noteReq.Header.Set("Authorization", "Bearer investigator-token")
	noteRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(noteRec, noteReq)
	if noteRec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", noteRec.Code, noteRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/soc/cases/"+row.ID.String()+"/notes?tenant_id="+tenantID.String(), nil)
	listReq.Header.Set("Authorization", "Bearer investigator-token")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var list paginatedResponse[socCaseNoteResponse]
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Pagination.Total != 0 || len(list.Data) != 0 {
		t.Fatalf("unexpected persisted notes after rejected citation: %+v", list)
	}
}

func TestSOCCaseExportDoesNotLeakCrossTenantCase(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	store := &fakeStore{}
	row, err := store.CreateAIInvestigation(context.Background(), storage.CreateAIInvestigationParams{
		TenantID:         otherTenantID,
		TriggerType:      "incident",
		TriggerEventType: "db.audit",
		TriggerDedupKey:  "cross-tenant",
		Severity:         "high",
		Summary:          "Other tenant case",
		Status:           storage.AIInvestigationStatusOpen,
	})
	if err != nil {
		t.Fatalf("seed ai investigation: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens(roleInvestigator, "investigator-token"),
	}, store, &stubQueue{})
	defer func() { _ = srv.Stop(context.Background()) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/soc/cases/"+row.ID.String()+"/export?tenant_id="+tenantID.String(), nil)
	req.Header.Set("Authorization", "Bearer investigator-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func stringsForCaseGuardrails(values []string) string {
	out, _ := json.Marshal(values)
	return string(out)
}

func stringsForCaseRefs(values []socCaseEvidenceRef) string {
	out, _ := json.Marshal(values)
	return string(out)
}

func stringsForCaseTimeline(values []socCaseTimelineItem) string {
	out, _ := json.Marshal(values)
	return string(out)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}
