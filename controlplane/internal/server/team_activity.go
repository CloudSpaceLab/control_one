package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// roleInvestigator is defined in misconduct.go; team metrics expose SOC
// attribution data so they are gated like the SOC case routes rather than the
// generic dashboard (viewer) surface.

func (s *Server) requireTeamTenantFromQuery(w http.ResponseWriter, r *http.Request, principal *auth.Principal) (uuid.UUID, bool) {
	return s.requireTenantAccessFromQuery(w, r, principal, roleInvestigator, roleOperator, roleAdmin)
}

// parseTeamWindow resolves the activity window from the `days` query param
// (clamped to [1, 365]) plus a granularity hint from `bucket` ("day",
// "week", "month"; defaults are inferred from the window width).
func parseTeamWindow(r *http.Request) (since, until time.Time, days int, granularity string) {
	days = parseDaysQuery(r, 30)
	now := time.Now().UTC()
	since = now.AddDate(0, 0, -days)
	until = now

	granularity = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("bucket")))
	switch granularity {
	case "week", "month":
	default:
		granularity = "day"
	}
	if granularity == "day" {
		switch {
		case days > 200:
			granularity = "month"
		case days > 60:
			granularity = "week"
		}
	}
	return since, until, days, granularity
}

// handleTeamMetrics reports today's/selected-window summary cards and the
// per-analyst breakdown. GET /api/v1/team/metrics?tenant_id&days
func (s *Server) handleTeamMetrics(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	since, until, _, _ := parseTeamWindow(r)

	summary, err := s.store.GetTeamSummary(r.Context(), tenantID, since, until)
	if err != nil {
		s.logger.Warn("team summary", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	analysts, err := s.store.GetTeamAnalystMetrics(r.Context(), tenantID, since, until)
	if err != nil {
		s.logger.Warn("team analyst metrics", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary":  summary,
		"analysts": analysts,
	})
}

// handleTeamTrends reports bucketed activity trends.
// GET /api/v1/team/trends?tenant_id&days&bucket=(day|week|month)
func (s *Server) handleTeamTrends(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	since, until, _, granularity := parseTeamWindow(r)

	points, err := s.store.GetTeamTrends(r.Context(), tenantID, since, until, granularity)
	if err != nil {
		s.logger.Warn("team trends", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"granularity": granularity,
		"points":      points,
	})
}

// handleTeamActivityFeed returns the chronological activity feed, newest
// first. GET /api/v1/team/activity?tenant_id&days&analyst_id&limit&offset
func (s *Server) handleTeamActivityFeed(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	since, until, _, _ := parseTeamWindow(r)

	var analystID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("analyst_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid analyst_id", http.StatusBadRequest)
			return
		}
		analystID = parsed
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	items, total, err := s.store.GetTeamActivityFeed(r.Context(), tenantID, since, until, analystID, limit, offset)
	if err != nil {
		s.logger.Warn("team activity feed", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, paginatedResponse[storage.TeamActivityItem]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

// handleTeamCoverageGaps reports coverage blind spots.
// GET /api/v1/team/gaps?tenant_id
func (s *Server) handleTeamCoverageGaps(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}

	gaps, err := s.store.GetTeamCoverageGaps(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("team coverage gaps", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, gaps)
}
