package server

import (
	"net/http"

	"github.com/google/uuid"
)

type coverageMatrixResponse struct {
	CatalogVersion string                     `json:"catalog_version"`
	Scope          string                     `json:"scope"`
	TenantID       string                     `json:"tenant_id,omitempty"`
	Domains        []coverageDomainDefinition `json:"domains"`
	Legend         coverageLegend             `json:"legend"`
	Matrix         []coverageMatrixRow        `json:"matrix"`
}

type coverageExplainResponse struct {
	CatalogVersion string                     `json:"catalog_version"`
	Scope          string                     `json:"scope"`
	TenantID       string                     `json:"tenant_id,omitempty"`
	Domains        []coverageDomainDefinition `json:"domains"`
	Legend         coverageLegend             `json:"legend"`
	Explanations   []coverageExplanation      `json:"explanations"`
}

func (s *Server) handleCoverageSubroutes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/coverage/matrix":
		s.handleCoverageMatrix(w, r)
	case "/api/v1/coverage/explain":
		s.handleCoverageExplain(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCoverageMatrix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCoverageMatrixResponse(tenantID))
}

func (s *Server) handleCoverageExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCoverageExplainResponse(tenantID))
}

func newCoverageMatrixResponse(tenantID uuid.UUID) coverageMatrixResponse {
	return coverageMatrixResponse{
		CatalogVersion: coverageCatalogVersion,
		Scope:          coverageScope(tenantID),
		TenantID:       coverageTenantIDString(tenantID),
		Domains:        cloneCoverageDomains(),
		Legend:         buildCoverageLegend(),
		Matrix:         cloneCoverageMatrix(),
	}
}

func newCoverageExplainResponse(tenantID uuid.UUID) coverageExplainResponse {
	return coverageExplainResponse{
		CatalogVersion: coverageCatalogVersion,
		Scope:          coverageScope(tenantID),
		TenantID:       coverageTenantIDString(tenantID),
		Domains:        cloneCoverageDomains(),
		Legend:         buildCoverageLegend(),
		Explanations:   cloneCoverageExplanations(),
	}
}

func coverageScope(tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return "global"
	}
	return "tenant"
}

func coverageTenantIDString(tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return ""
	}
	return tenantID.String()
}
