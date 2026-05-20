package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type coverageMatrixResponse struct {
	CatalogVersion string                     `json:"catalog_version"`
	Scope          string                     `json:"scope"`
	TenantID       string                     `json:"tenant_id,omitempty"`
	GeneratedAt    string                     `json:"generated_at,omitempty"`
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
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.coverageTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	resp := newCoverageMatrixResponse(tenantID)
	if tenantID != uuid.Nil {
		now := time.Now().UTC()
		var live bool
		resp.Matrix, live = appendTenantCoverageOverlays(r.Context(), s.store, tenantID, resp.Matrix, now)
		if live {
			resp.GeneratedAt = now.Format(time.RFC3339)
		}
	}
	resp.Matrix = filterCoverageMatrixByDomain(resp.Matrix, r.URL.Query().Get("domain"))
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCoverageExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.coverageTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCoverageExplainResponse(tenantID))
}

func (s *Server) coverageTenantFromQuery(w http.ResponseWriter, r *http.Request, principal *auth.Principal) (uuid.UUID, bool) {
	tenantID, ok := parseTenantQuery(w, r)
	if !ok || tenantID == uuid.Nil {
		return tenantID, ok
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
		return uuid.Nil, false
	}
	return tenantID, true
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

type coverageNodeLister interface {
	ListNodes(context.Context, uuid.UUID, string, int, int) ([]storage.Node, int, error)
}

const coverageHeartbeatFreshnessWindow = 5 * time.Minute

func appendTenantCoverageOverlays(ctx context.Context, store any, tenantID uuid.UUID, rows []coverageMatrixRow, now time.Time) ([]coverageMatrixRow, bool) {
	if tenantID == uuid.Nil || store == nil {
		return rows, false
	}
	nodeStore, ok := store.(coverageNodeLister)
	if !ok {
		return rows, false
	}
	nodes, total, err := nodeStore.ListNodes(ctx, tenantID, "", 500, 0)
	if err != nil {
		row := tenantHeartbeatCoverageRow(coverageStateStale, []string{"node heartbeat inventory unavailable"}, []string{err.Error()})
		return append(rows, row), true
	}
	return append(rows, tenantHeartbeatCoverageFromNodes(nodes, total, now)), true
}

func tenantHeartbeatCoverageFromNodes(nodes []storage.Node, total int, now time.Time) coverageMatrixRow {
	if total == 0 {
		return tenantHeartbeatCoverageRow(
			coverageStateNotApplicable,
			[]string{"no enrolled nodes for this tenant"},
			[]string{"enroll at least one node before heartbeat freshness can be assessed"},
		)
	}
	var fresh, stale, missing int
	var newest *time.Time
	for _, node := range nodes {
		if node.LastSeenAt == nil {
			missing++
			continue
		}
		lastSeen := node.LastSeenAt.UTC()
		if newest == nil || lastSeen.After(*newest) {
			copyTime := lastSeen
			newest = &copyTime
		}
		if now.Sub(lastSeen) <= coverageHeartbeatFreshnessWindow {
			fresh++
		} else {
			stale++
		}
	}
	signals := []string{
		fmt.Sprintf("nodes_total=%d", total),
		fmt.Sprintf("nodes_sampled=%d", len(nodes)),
		fmt.Sprintf("fresh=%d", fresh),
		fmt.Sprintf("stale=%d", stale),
		fmt.Sprintf("missing=%d", missing),
	}
	if newest != nil {
		signals = append(signals, "last_seen_at="+newest.UTC().Format(time.RFC3339))
	}
	if total > len(nodes) {
		signals = append(signals, "node_count_truncated=true")
	}
	if stale > 0 || missing > 0 || total > len(nodes) {
		gaps := []string{}
		if stale > 0 {
			gaps = append(gaps, fmt.Sprintf("%d nodes have heartbeats older than %s", stale, coverageHeartbeatFreshnessWindow))
		}
		if missing > 0 {
			gaps = append(gaps, fmt.Sprintf("%d nodes have no heartbeat timestamp", missing))
		}
		if total > len(nodes) {
			gaps = append(gaps, "tenant has more than 500 nodes; heartbeat overlay is sampled until pagination is expanded")
		}
		return tenantHeartbeatCoverageRow(coverageStateStale, signals, gaps)
	}
	return tenantHeartbeatCoverageRow(coverageStateSupported, signals, nil)
}

func tenantHeartbeatCoverageRow(state coverageSupportState, signals []string, gaps []string) coverageMatrixRow {
	return coverageMatrixRow{
		Domain:  "telemetry",
		Title:   "Tenant heartbeat freshness",
		State:   state,
		Quality: []coverageQualityState{coverageQualityProductionTested},
		Signals: append([]string{
			"nodes.last_seen_at",
			"freshness_window=5m",
		}, signals...),
		Evidence: []string{
			"controlplane/internal/storage/nodes.go",
			"controlplane/internal/server/heartbeat.go",
		},
		Gaps: gaps,
	}
}

func filterCoverageMatrixByDomain(rows []coverageMatrixRow, domain string) []coverageMatrixRow {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return rows
	}
	filtered := make([]coverageMatrixRow, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(row.Domain, domain) {
			filtered = append(filtered, row)
		}
	}
	return filtered
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
