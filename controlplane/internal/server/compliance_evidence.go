package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/compliance"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/pdfreport"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ── Evidence collection: POST + GET ──────────────────────────────────────────

func (s *Server) handleComplianceEvidenceCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateComplianceEvidence(w, r)
	case http.MethodGet:
		s.handleListComplianceEvidence(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreateComplianceEvidence(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	tenantIDStr := r.FormValue("tenant_id")
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	evidenceType := strings.TrimSpace(r.FormValue("evidence_type"))
	if evidenceType == "" {
		http.Error(w, "evidence_type is required", http.StatusBadRequest)
		return
	}

	uploaderID := s.userIDForPrincipalCtx(r.Context(), principal)

	ev := &storage.ComplianceEvidence{
		TenantID:     tenantID,
		EvidenceType: evidenceType,
		Title:        title,
		UploadedBy:   uploaderID,
	}

	if fw := strings.TrimSpace(r.FormValue("framework")); fw != "" {
		ev.Framework = &fw
	}
	if cr := strings.TrimSpace(r.FormValue("control_ref")); cr != "" {
		ev.ControlRef = &cr
	}
	if desc := strings.TrimSpace(r.FormValue("description")); desc != "" {
		ev.Description = &desc
	}
	if expStr := strings.TrimSpace(r.FormValue("expires_at")); expStr != "" {
		t, err := time.Parse(time.RFC3339, expStr)
		if err == nil {
			ev.ExpiresAt = &t
		}
	}

	// Handle optional file upload
	file, header, fileErr := r.FormFile("file")
	if fileErr == nil {
		defer func() { _ = file.Close() }()

		evidenceID := uuid.New()
		ev.ID = evidenceID

		dir := filepath.Join(os.TempDir(), "control-one-evidence", tenantID.String(), evidenceID.String())
		if err := os.MkdirAll(dir, 0o750); err != nil {
			http.Error(w, "failed to create storage directory", http.StatusInternalServerError)
			return
		}

		destPath := filepath.Join(dir, header.Filename)
		dest, err := os.Create(destPath)
		if err != nil {
			http.Error(w, "failed to create file", http.StatusInternalServerError)
			return
		}
		defer func() { _ = dest.Close() }()

		hasher := sha256.New()
		written, err := io.Copy(io.MultiWriter(dest, hasher), file)
		if err != nil {
			http.Error(w, "failed to write file", http.StatusInternalServerError)
			return
		}

		checksum := hex.EncodeToString(hasher.Sum(nil))
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		ev.FilePath = &destPath
		ev.FileSizeBytes = &written
		ev.MimeType = &mimeType
		ev.Checksum = &checksum
	}

	created, err := s.store.CreateComplianceEvidence(r.Context(), ev)
	if err != nil {
		s.logger.Sugar().Errorw("create compliance evidence", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, tenantID, "compliance.evidence.upload", "compliance_evidence", created.ID.String(), map[string]any{
		"title":         created.Title,
		"evidence_type": created.EvidenceType,
	})

	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListComplianceEvidence(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	q := r.URL.Query()
	tenantIDStr := q.Get("tenant_id")
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	framework := q.Get("framework")
	evidenceType := q.Get("evidence_type")
	limit := 50
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	items, total, err := s.store.ListComplianceEvidence(r.Context(), tenantID, framework, evidenceType, limit, offset)
	if err != nil {
		s.logger.Sugar().Errorw("list compliance evidence", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"pagination": map[string]any{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// ── Evidence resource: GET (download) + DELETE ────────────────────────────────

func (s *Server) handleComplianceEvidenceResource(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/compliance/evidence/{id}  or  /api/v1/compliance/evidence/{id}/download
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/evidence/"), "/")
	idStr := parts[0]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid evidence id", http.StatusBadRequest)
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "download" && r.Method == http.MethodGet:
		s.handleDownloadComplianceEvidence(w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		s.handleDeleteComplianceEvidence(w, r, id)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDownloadComplianceEvidence(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	tenantID, terr := requiredTenantID(r)
	if terr != nil {
		http.Error(w, terr.Error(), http.StatusBadRequest)
		return
	}

	ev, err := s.store.GetComplianceEvidence(r.Context(), id)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if ev == nil || ev.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	if ev.FilePath == nil {
		http.Error(w, "no file attached to this evidence record", http.StatusNotFound)
		return
	}

	filename := filepath.Base(*ev.FilePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if ev.MimeType != nil {
		w.Header().Set("Content-Type", *ev.MimeType)
	}
	http.ServeFile(w, r, *ev.FilePath)
}

func (s *Server) handleDeleteComplianceEvidence(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	tenantID, terr := requiredTenantID(r)
	if terr != nil {
		http.Error(w, terr.Error(), http.StatusBadRequest)
		return
	}

	ev, err := s.store.GetComplianceEvidence(r.Context(), id)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if ev == nil || ev.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}

	// Remove file from disk if present
	if ev.FilePath != nil {
		dir := filepath.Dir(*ev.FilePath)
		_ = os.RemoveAll(dir)
	}

	if err := s.store.DeleteComplianceEvidence(r.Context(), id); err != nil {
		s.logger.Sugar().Errorw("delete compliance evidence", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, ev.TenantID, "compliance.evidence.delete", "compliance_evidence", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// ── Frameworks catalog ────────────────────────────────────────────────────────

func (s *Server) handleComplianceFrameworks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	type controlOut struct {
		Framework   string `json:"framework"`
		ControlID   string `json:"control_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}

	controls := make(map[string][]controlOut)
	for fw, mappings := range compliance.FrameworkControls {
		out := make([]controlOut, 0, len(mappings))
		for _, m := range mappings {
			out = append(out, controlOut{
				Framework:   m.Framework,
				ControlID:   m.ControlID,
				Title:       m.Title,
				Description: m.Description,
			})
		}
		controls[fw] = out
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"frameworks": compliance.ListFrameworks(),
		"controls":   controls,
	})
}

// ── Reports collection: POST + GET ───────────────────────────────────────────

func (s *Server) handleComplianceReportsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateAuditReport(w, r)
	case http.MethodGet:
		s.handleListAuditReports(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

type createAuditReportRequest struct {
	TenantID    uuid.UUID `json:"tenant_id"`
	Framework   string    `json:"framework"`
	PeriodStart string    `json:"period_start"`
	PeriodEnd   string    `json:"period_end"`
}

func (s *Server) handleCreateAuditReport(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	var req createAuditReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.TenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if req.Framework == "" {
		http.Error(w, "framework is required", http.StatusBadRequest)
		return
	}

	start, err := time.Parse("2006-01-02", req.PeriodStart)
	if err != nil {
		http.Error(w, "invalid period_start (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	end, err := time.Parse("2006-01-02", req.PeriodEnd)
	if err != nil {
		http.Error(w, "invalid period_end (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	generatedBy := s.userIDForPrincipalCtx(r.Context(), principal)
	report := &storage.AuditReport{
		TenantID:    req.TenantID,
		Framework:   req.Framework,
		PeriodStart: start,
		PeriodEnd:   end,
		Status:      "pending",
		GeneratedBy: &generatedBy,
	}

	created, err := s.store.CreateAuditReport(r.Context(), report)
	if err != nil {
		s.logger.Sugar().Errorw("create audit report", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Queue a job to generate the report
	payload, _ := json.Marshal(map[string]any{
		"report_id": created.ID.String(),
		"framework": created.Framework,
	})
	job := storage.Job{
		Type:     "compliance_report_generate",
		Status:   storage.JobStatusQueued,
		TenantID: req.TenantID,
		Payload:  payload,
	}
	_, _ = s.store.CreateJob(r.Context(), &job, nil)

	s.recordAudit(r.Context(), principal, req.TenantID, "compliance.report.create", "audit_report", created.ID.String(), map[string]any{
		"framework": created.Framework,
	})

	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListAuditReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	q := r.URL.Query()
	tenantID, err := uuid.Parse(q.Get("tenant_id"))
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	limit := 50
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	reports, total, err := s.store.ListAuditReports(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Sugar().Errorw("list audit reports", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": reports,
		"pagination": map[string]any{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// ── Reports resource: download ────────────────────────────────────────────────

func (s *Server) handleComplianceReportsResource(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/reports/"), "/")
	idStr := parts[0]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid report id", http.StatusBadRequest)
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "download" && r.Method == http.MethodGet {
		s.handleDownloadAuditReport(w, r, id)
		return
	}

	w.Header().Set("Allow", http.MethodGet)
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func (s *Server) handleDownloadAuditReport(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	report, err := s.store.GetAuditReport(r.Context(), id)
	if err != nil {
		s.logger.Sugar().Errorw("get audit report", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if report == nil {
		http.NotFound(w, r)
		return
	}

	tenantName := report.TenantID.String()
	if tenant, terr := s.store.GetTenant(r.Context(), report.TenantID); terr == nil && tenant != nil {
		tenantName = tenant.Name
	} else if terr != nil {
		s.logger.Sugar().Warnw("get tenant for audit report", "error", terr)
	}

	coverage, cerr := s.store.GetControlCoverage(r.Context(), report.TenantID, report.Framework, report.PeriodStart, report.PeriodEnd)
	if cerr != nil {
		s.logger.Sugar().Warnw("get control coverage", "error", cerr)
	}
	passed, failed, perr := s.store.CountResultsForReport(r.Context(), report.TenantID, report.Framework, report.PeriodStart, report.PeriodEnd)
	if perr != nil {
		s.logger.Sugar().Warnw("count results for report", "error", perr)
	}
	matrix, merr := s.store.GetPerNodeMatrix(r.Context(), report.TenantID, report.Framework, report.PeriodStart, report.PeriodEnd, 500)
	if merr != nil {
		s.logger.Sugar().Warnw("get per-node matrix", "error", merr)
	}

	// Hydrate Title from the Go-side framework catalog so the report shows
	// human-readable names instead of bare control IDs.
	titles := make(map[string]string)
	for _, c := range compliance.FrameworkControls[report.Framework] {
		titles[c.ControlID] = c.Title
	}
	for i := range coverage {
		if t, ok := titles[coverage[i].ControlID]; ok && coverage[i].Title == "" {
			coverage[i].Title = t
		}
	}

	gap := make([]storage.ControlCoverage, 0)
	for _, c := range coverage {
		if c.Status == "NO_COVERAGE" {
			gap = append(gap, c)
		}
	}

	evidence, _, _ := s.store.ListComplianceEvidence(r.Context(), report.TenantID, report.Framework, "", 1000, 0)

	data := pdfreport.ReportData{
		TenantName:     tenantName,
		Framework:      report.Framework,
		PeriodStart:    report.PeriodStart,
		PeriodEnd:      report.PeriodEnd,
		GeneratedAt:    time.Now().UTC(),
		Controls:       compliance.FrameworkControls[report.Framework],
		EvidenceList:   evidence,
		OpenFindings:   failed,
		TotalPassed:    passed,
		TotalFailed:    failed,
		ControlSummary: coverage,
		PerNodeMatrix:  matrix,
		GapAnalysis:    gap,
	}

	textOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("text_only")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("text_only")), "true")

	var (
		body        []byte
		genErr      error
		contentType string
		ext         string
	)
	if textOnly {
		body, genErr = pdfreport.GenerateText(r.Context(), data)
		contentType = "text/plain; charset=utf-8"
		ext = "txt"
	} else {
		body, genErr = pdfreport.GenerateHTML(r.Context(), data)
		contentType = "text/html; charset=utf-8"
		ext = "html"
	}
	if genErr != nil {
		s.logger.Sugar().Errorw("generate audit report", "error", genErr, "text_only", textOnly)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	_ = s.store.UpdateAuditReportStatus(r.Context(), id, "ready", nil, &now)

	filename := fmt.Sprintf("compliance-report-%s-%s.%s", report.Framework, report.PeriodEnd.Format("2006-01-02"), ext)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// ── Reviews collection: GET + POST ───────────────────────────────────────────

func (s *Server) handleComplianceReviewsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListComplianceReviews(w, r)
	case http.MethodPost:
		s.handleCreateComplianceReview(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListComplianceReviews(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	tenantID, err := uuid.Parse(q.Get("tenant_id"))
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	limit, offset := 50, 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	reviews, total, err := s.store.ListComplianceReviews(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Sugar().Errorw("list compliance reviews", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": reviews,
		"pagination": map[string]any{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

type createComplianceReviewRequest struct {
	TenantID     uuid.UUID  `json:"tenant_id"`
	ReviewType   string     `json:"review_type"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	Recurrence   *string    `json:"recurrence,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
}

func (s *Server) handleCreateComplianceReview(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createComplianceReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.TenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if req.ReviewType == "" {
		http.Error(w, "review_type is required", http.StatusBadRequest)
		return
	}

	review := &storage.ComplianceReview{
		TenantID:     req.TenantID,
		ReviewType:   req.ReviewType,
		ScheduledFor: req.ScheduledFor,
		Recurrence:   req.Recurrence,
		Notes:        req.Notes,
		Status:       "pending",
	}

	created, err := s.store.CreateComplianceReview(r.Context(), review)
	if err != nil {
		s.logger.Sugar().Errorw("create compliance review", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, req.TenantID, "compliance.review.created", "compliance_review", created.ID.String(), map[string]any{
		"review_type": req.ReviewType,
	})

	writeJSON(w, http.StatusCreated, created)
}

// ── Reviews resource: GET + POST complete + DELETE ────────────────────────────

func (s *Server) handleComplianceReviewsResource(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/reviews/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid review id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetComplianceReview(w, r, id)
	case http.MethodPost:
		// Check for /complete sub-action
		if len(parts) > 1 && parts[1] == "complete" {
			s.handleCompleteComplianceReview(w, r, id)
			return
		}
		http.Error(w, "invalid action", http.StatusBadRequest)
	case http.MethodDelete:
		s.handleDeleteComplianceReview(w, r, id)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetComplianceReview(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	review, err := s.store.GetComplianceReview(r.Context(), id)
	if err != nil {
		s.logger.Sugar().Errorw("get compliance review", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if review == nil {
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, review)
}

type completeComplianceReviewRequest struct {
	Notes string `json:"notes,omitempty"`
}

func (s *Server) handleCompleteComplianceReview(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req completeComplianceReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	review, err := s.store.GetComplianceReview(r.Context(), id)
	if err != nil {
		s.logger.Sugar().Errorw("get compliance review for complete", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if review == nil {
		http.NotFound(w, r)
		return
	}

	reviewedBy := s.userIDForPrincipalCtx(r.Context(), principal)
	var notesPtr *string
	if req.Notes != "" {
		notesPtr = &req.Notes
	}

	if err := s.store.CompleteComplianceReview(r.Context(), id, reviewedBy, notesPtr); err != nil {
		s.logger.Sugar().Errorw("complete compliance review", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, review.TenantID, "compliance.review.completed", "compliance_review", id.String(), nil)

	// Return updated review
	updated, _ := s.store.GetComplianceReview(r.Context(), id)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteComplianceReview(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	review, err := s.store.GetComplianceReview(r.Context(), id)
	if err != nil {
		s.logger.Sugar().Errorw("get compliance review for delete", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if review == nil {
		http.NotFound(w, r)
		return
	}

	if err := s.store.DeleteComplianceReview(r.Context(), id); err != nil {
		s.logger.Sugar().Errorw("delete compliance review", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, review.TenantID, "compliance.review.deleted", "compliance_review", id.String(), map[string]any{
		"review_type": review.ReviewType,
	})

	w.WriteHeader(http.StatusNoContent)
}
