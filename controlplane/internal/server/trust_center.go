package server

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// TrustCenterPublicResponse is the public trust center data exposed without authentication.
type TrustCenterPublicResponse struct {
	TenantSlug     string               `json:"tenant_slug"`
	TenantName     string               `json:"tenant_name"`
	Subprocessors  []TrustSubprocessor  `json:"subprocessors"`
	Certifications []TrustCertification `json:"certifications"`
	FAQ            []TrustFAQItem       `json:"faq"`
	Incidents      []TrustIncident      `json:"incidents"`
	SecurityEmail  string               `json:"security_email,omitempty"`
	TrustPortalURL string               `json:"trust_portal_url,omitempty"`
	LastUpdated    time.Time            `json:"last_updated"`
}

type TrustSubprocessor struct {
	Name       string   `json:"name"`
	Purpose    string   `json:"purpose"`
	Location   string   `json:"location"`
	DataTypes  []string `json:"data_types"`
	DPAInPlace bool     `json:"dpa_in_place"`
	SOC2       bool     `json:"soc2"`
	ISO27001   bool     `json:"iso27001"`
}

type TrustCertification struct {
	Type      string    `json:"type"`
	Scope     string    `json:"scope"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Auditor   string    `json:"auditor"`
	Status    string    `json:"status"`
}

type TrustFAQItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Category string `json:"category"`
}

type TrustIncident struct {
	IncidentID  string     `json:"incident_id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Severity    string     `json:"severity"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	PublishedAt time.Time  `json:"published_at"`
}

// handleTrustCenterPublic serves the public trust center page data at GET /api/v1/trust/:name
// This endpoint is intentionally unauthenticated for public transparency.
func (s *Server) handleTrustCenterPublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing tenant name", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	data, err := s.store.GetTrustCenterData(ctx, name)
	if err != nil {
		s.logger.Warn("trust center lookup failed", zap.String("name", name), zap.Error(err))
		http.Error(w, "trust center unavailable", http.StatusInternalServerError)
		return
	}
	if data == nil {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	resp := TrustCenterPublicResponse{
		TenantSlug:  data.TenantSlug,
		TenantName:  data.TenantName,
		LastUpdated: data.LastUpdated,
	}

	// Map subprocessors (sanitize internal IDs)
	for _, sp := range data.Subprocessors {
		resp.Subprocessors = append(resp.Subprocessors, TrustSubprocessor{
			Name:       sp.Name,
			Purpose:    sp.Purpose,
			Location:   sp.Location,
			DataTypes:  sp.DataTypes,
			DPAInPlace: sp.DPAInPlace,
			SOC2:       sp.SOC2,
			ISO27001:   sp.ISO27001,
		})
	}

	// Map certifications
	for _, c := range data.Certifications {
		resp.Certifications = append(resp.Certifications, TrustCertification{
			Type:      c.Type,
			Scope:     c.Scope,
			IssuedAt:  c.IssuedAt,
			ExpiresAt: c.ExpiresAt,
			Auditor:   c.Auditor,
			Status:    c.Status,
		})
	}

	// Map FAQ
	for _, f := range data.FAQItems {
		resp.FAQ = append(resp.FAQ, TrustFAQItem{
			Question: f.Question,
			Answer:   f.Answer,
			Category: f.Category,
		})
	}

	// Map incidents
	for _, i := range data.Incidents {
		resp.Incidents = append(resp.Incidents, TrustIncident{
			IncidentID:  i.IncidentID,
			Title:       i.Title,
			Summary:     i.Summary,
			Severity:    i.Severity,
			Status:      i.Status,
			StartedAt:   i.StartedAt,
			ResolvedAt:  i.ResolvedAt,
			PublishedAt: i.PublishedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// TrustCenterAdmin handlers (authenticated) for managing trust center content

type createSubprocessorRequest struct {
	Name       string   `json:"name"`
	Purpose    string   `json:"purpose"`
	Location   string   `json:"location"`
	DataTypes  []string `json:"data_types"`
	DPAInPlace bool     `json:"dpa_in_place"`
	SOC2       bool     `json:"soc2"`
	ISO27001   bool     `json:"iso27001"`
}

type createCertificationRequest struct {
	Type      string    `json:"type"`
	Scope     string    `json:"scope"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Auditor   string    `json:"auditor"`
	ReportURL *string   `json:"report_url,omitempty"`
}

type createFAQRequest struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Category string `json:"category"`
	OrderIdx int    `json:"order_idx"`
}

type createIncidentRequest struct {
	IncidentID string     `json:"incident_id"`
	Title      string     `json:"title"`
	Summary    string     `json:"summary"`
	Severity   string     `json:"severity"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ReportURL  *string    `json:"report_url,omitempty"`
}

func (s *Server) handleSubprocessorsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listSubprocessors(w, r)
	case http.MethodPost:
		s.createSubprocessor(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listSubprocessors(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	list, err := s.store.ListSubprocessors(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": list})
}

func (s *Server) createSubprocessor(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	var req createSubprocessorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	sp := &storage.Subprocessor{
		TenantID:   tenantID,
		Name:       req.Name,
		Purpose:    req.Purpose,
		Location:   req.Location,
		DataTypes:  req.DataTypes,
		DPAInPlace: req.DPAInPlace,
		SOC2:       req.SOC2,
		ISO27001:   req.ISO27001,
	}

	if err := s.store.CreateSubprocessor(r.Context(), sp); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sp)
}

func (s *Server) handleSubprocessorResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	id := r.PathValue("id")
	if err := s.store.DeleteSubprocessor(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Certifications handlers
func (s *Server) handleCertificationsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listCertifications(w, r)
	case http.MethodPost:
		s.createCertification(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listCertifications(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	list, err := s.store.ListCertifications(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": list})
}

func (s *Server) createCertification(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	var req createCertificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	c := &storage.Certification{
		TenantID:  tenantID,
		Type:      req.Type,
		Scope:     req.Scope,
		IssuedAt:  req.IssuedAt,
		ExpiresAt: req.ExpiresAt,
		Auditor:   req.Auditor,
		ReportURL: req.ReportURL,
		Status:    "active",
	}

	if err := s.store.CreateCertification(r.Context(), c); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

func (s *Server) handleCertificationResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}
	id := r.PathValue("id")
	if err := s.store.DeleteCertification(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// FAQ handlers
func (s *Server) handleFAQCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listFAQ(w, r)
	case http.MethodPost:
		s.createFAQ(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listFAQ(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	list, err := s.store.ListFAQItems(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": list})
}

func (s *Server) createFAQ(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	var req createFAQRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	f := &storage.SecurityFAQItem{
		TenantID: tenantID,
		Question: req.Question,
		Answer:   req.Answer,
		Category: req.Category,
		OrderIdx: req.OrderIdx,
	}

	if err := s.store.CreateFAQItem(r.Context(), f); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(f)
}

func (s *Server) handleFAQResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}
	id := r.PathValue("id")
	if err := s.store.DeleteFAQItem(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Incident handlers
func (s *Server) handleIncidentsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listIncidents(w, r)
	case http.MethodPost:
		s.createIncident(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	list, err := s.store.ListIncidentReports(r.Context(), tenantID, 50)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"data": list})
}

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request) {
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}

	var req createIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id query parameter required", http.StatusBadRequest)
		return
	}

	i := &storage.IncidentReport{
		TenantID:   tenantID,
		IncidentID: req.IncidentID,
		Title:      req.Title,
		Summary:    req.Summary,
		Severity:   req.Severity,
		Status:     req.Status,
		StartedAt:  req.StartedAt,
		ResolvedAt: req.ResolvedAt,
		ReportURL:  req.ReportURL,
	}

	if err := s.store.CreateIncidentReport(r.Context(), i); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(i)
}

func (s *Server) handleIncidentResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	_, authOK := s.authorize(w, r, roleOperator, roleAdmin)
	if !authOK {
		return
	}
	id := r.PathValue("id")
	if err := s.store.DeleteIncidentReport(r.Context(), id); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
