package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestAuditReportDownloadRequiresTenantScopeAndPersistsArtifact(t *testing.T) {
	reportsDir := t.TempDir()
	t.Setenv("CONTROL_ONE_REPORTS_DIR", reportsDir)

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	reportID := uuid.New()
	report := &storage.AuditReport{
		ID:          reportID,
		TenantID:    tenantID,
		Framework:   "SOC2",
		PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Status:      "pending",
	}
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}},
		auditReports: map[uuid.UUID]*storage.AuditReport{
			reportID: report,
		},
	}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "report-viewer"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	handler := srv.Handler()

	crossTenantReq := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports/"+reportID.String()+"/download?tenant_id="+otherTenantID.String(), nil)
	crossTenantReq.Header.Set("Authorization", "Bearer report-viewer")
	crossTenantRec := httptest.NewRecorder()
	handler.ServeHTTP(crossTenantRec, crossTenantReq)
	if crossTenantRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-tenant report download to be hidden, got %d body=%s", crossTenantRec.Code, crossTenantRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports/"+reportID.String()+"/download?tenant_id="+tenantID.String(), nil)
	req.Header.Set("Authorization", "Bearer report-viewer")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected report download, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "compliance-report-SOC2-2026-05-31.html") {
		t.Fatalf("unexpected content disposition: %s", rec.Header().Get("Content-Disposition"))
	}

	updated := store.auditReports[reportID]
	if updated == nil || updated.Status != "ready" || updated.PDFPath == nil || updated.GeneratedAt == nil {
		t.Fatalf("expected persisted ready report artifact, got %#v", updated)
	}
	if !strings.HasPrefix(*updated.PDFPath, reportsDir) {
		t.Fatalf("artifact path escaped reports dir: %s", *updated.PDFPath)
	}
	if _, err := os.Stat(*updated.PDFPath); err != nil {
		t.Fatalf("expected artifact file to exist: %v", err)
	}
}

func TestListComplianceEvidenceReturnsFreshnessAndHidesFilePath(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	framework := "SOC2"
	control := "CC6.1"
	filePath := "C:/tmp/control-one/evidence/secrets.txt"
	evidenceID := uuid.New()
	store := &fakeStore{
		complianceEvidence: []storage.ComplianceEvidence{
			{
				ID:           uuid.New(),
				TenantID:     tenantID,
				EvidenceType: "config_snapshot",
				Framework:    &framework,
				ControlRef:   &control,
				Title:        "Expired evidence",
				FilePath:     &filePath,
				UploadedAt:   now.Add(-time.Hour),
				ExpiresAt:    testTimePtr(now.Add(-time.Minute)),
			},
			{
				ID:           evidenceID,
				TenantID:     tenantID,
				EvidenceType: "config_snapshot",
				Framework:    &framework,
				ControlRef:   &control,
				Title:        "Current evidence",
				FilePath:     &filePath,
				UploadedAt:   now.Add(-2 * time.Hour),
			},
		},
	}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "evidence-viewer"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/evidence?tenant_id="+tenantID.String()+"&framework=SOC2&control_ref=CC6.1&evidence_type=config_snapshot", nil)
	req.Header.Set("Authorization", "Bearer evidence-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected evidence list, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "FilePath") || strings.Contains(rec.Body.String(), filePath) {
		t.Fatalf("list response leaked server file path: %s", rec.Body.String())
	}

	var body struct {
		Data []complianceUploadedEvidenceResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != evidenceID.String() {
		t.Fatalf("expected only current evidence, got %#v", body.Data)
	}
	if body.Data[0].TenantID != tenantID.String() || body.Data[0].Freshness != "fresh" || body.Data[0].AgeSeconds <= 0 {
		t.Fatalf("expected freshness metadata, got %#v", body.Data[0])
	}
}

func TestEmptyComplianceReportAndReviewListsReturnArrays(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "list-viewer"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	for _, path := range []string{
		"/api/v1/compliance/reports?tenant_id=" + tenantID.String(),
		"/api/v1/compliance/reviews?tenant_id=" + tenantID.String(),
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer list-viewer")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d body=%s", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s response: %v", path, err)
		}
		if string(body.Data) != "[]" {
			t.Fatalf("%s data must serialize as [], got %s in %s", path, string(body.Data), rec.Body.String())
		}
	}
}

func TestComplianceReportAndReviewResponsesUseAPIFieldNames(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "api-shape-operator"),
	}
	handler := New(zap.NewNop(), cfg, store, &stubQueue{}).Handler()

	reportBody := strings.NewReader(`{"tenant_id":"` + tenantID.String() + `","framework":"SOC2","period_start":"2026-06-01","period_end":"2026-06-07"}`)
	reportReq := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/reports", reportBody)
	reportReq.Header.Set("Authorization", "Bearer api-shape-operator")
	reportReq.Header.Set("Content-Type", "application/json")
	reportRec := httptest.NewRecorder()
	handler.ServeHTTP(reportRec, reportReq)
	if reportRec.Code != http.StatusCreated {
		t.Fatalf("create report returned %d body=%s", reportRec.Code, reportRec.Body.String())
	}
	assertAPIFieldNames(t, "create report", reportRec.Body.String(), []string{`"id"`, `"tenant_id"`, `"period_start"`}, []string{`"ID"`, `"TenantID"`, `"CreatedAt"`})

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports?tenant_id="+tenantID.String(), nil)
	listReq.Header.Set("Authorization", "Bearer api-shape-operator")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list reports returned %d body=%s", listRec.Code, listRec.Body.String())
	}
	assertAPIFieldNames(t, "list reports", listRec.Body.String(), []string{`"data":[{"id"`, `"tenant_id"`, `"period_start"`}, []string{`"ID"`, `"TenantID"`, `"CreatedAt"`})

	reviewBody := strings.NewReader(`{"tenant_id":"` + tenantID.String() + `","review_type":"quarterly"}`)
	reviewReq := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/reviews", reviewBody)
	reviewReq.Header.Set("Authorization", "Bearer api-shape-operator")
	reviewReq.Header.Set("Content-Type", "application/json")
	reviewRec := httptest.NewRecorder()
	handler.ServeHTTP(reviewRec, reviewReq)
	if reviewRec.Code != http.StatusCreated {
		t.Fatalf("create review returned %d body=%s", reviewRec.Code, reviewRec.Body.String())
	}
	assertAPIFieldNames(t, "create review", reviewRec.Body.String(), []string{`"tenant_id"`, `"review_type"`, `"created_at"`}, []string{`"TenantID"`, `"ReviewType"`, `"CreatedAt"`})
}

func assertAPIFieldNames(t *testing.T, label, body string, wants, rejects []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s response missing %s: %s", label, want, body)
		}
	}
	for _, reject := range rejects {
		if strings.Contains(body, reject) {
			t.Fatalf("%s response contains non-API field %s: %s", label, reject, body)
		}
	}
}

func TestCreateComplianceEvidenceRejectsInvalidExpiration(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "evidence-uploader"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	body := strings.NewReader("tenant_id=" + tenantID.String() + "&title=Bad+expiry&evidence_type=config_snapshot&expires_at=tomorrow")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/evidence", body)
	req.Header.Set("Authorization", "Bearer evidence-uploader")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid expiration to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.complianceEvidence) != 0 {
		t.Fatalf("invalid evidence upload was persisted: %#v", store.complianceEvidence)
	}
}
