package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	if len(item.Citations) != 1 || len(item.EvidenceRefs) != 2 || !contains(item.ExportURL, "tenant_id="+tenantID.String()) {
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
	if len(export.Evidence) != 2 || !contains(stringsForCaseGuardrails(export.Guardrails), "source_row_citations") {
		t.Fatalf("export missing evidence/guardrails: %+v", export)
	}
	if export.Case.Evidence != nil || contains(exportRec.Body.String(), "risk_score") {
		t.Fatalf("export must carry evidence references without raw evidence details: %s", exportRec.Body.String())
	}
	if len(export.Notes) != 1 || export.Notes[0].AuditID != note.AuditID {
		t.Fatalf("export missing analyst note: %+v", export.Notes)
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
