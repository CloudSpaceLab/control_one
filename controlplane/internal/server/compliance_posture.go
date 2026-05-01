package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/compliance"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// controlCoverageDTO is the JSON shape returned to the UI. It mirrors
// storage.ControlCoverage with a hydrated Title from compliance.FrameworkControls.
type controlCoverageDTO struct {
	Framework     string     `json:"framework"`
	ControlID     string     `json:"control_id"`
	Title         string     `json:"title"`
	Applicability string     `json:"applicability,omitempty"`
	Status        string     `json:"status"` // PASS | PARTIAL | FAIL | NO_COVERAGE
	NodesChecked  int        `json:"nodes_checked"`
	NodesPassing  int        `json:"nodes_passing"`
	NodesFailing  int        `json:"nodes_failing"`
	EvidenceCount int        `json:"evidence_count"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
}

type controlPostureResponse struct {
	Framework   string               `json:"framework"`
	TenantID    string               `json:"tenant_id"`
	PeriodStart time.Time            `json:"period_start"`
	PeriodEnd   time.Time            `json:"period_end"`
	GeneratedAt time.Time            `json:"generated_at"`
	Coverage    []controlCoverageDTO `json:"coverage"`
}

// handleComplianceControlPosture serves GET /api/v1/compliance/control-posture.
//
// Query params:
//
//	framework     (required) — e.g. SOC2, HIPAA, PCI-DSS, ISO27001, GDPR
//	tenant_id     (required) — tenant UUID
//	period_start  (optional, RFC3339 or YYYY-MM-DD) — default: now - 30d
//	period_end    (optional, RFC3339 or YYYY-MM-DD) — default: now
//
// Returns one entry per control_id mapped to the framework, including controls
// with no results in the period (Status = "NO_COVERAGE", zero counts).
func (s *Server) handleComplianceControlPosture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}

	q := r.URL.Query()
	framework := strings.TrimSpace(q.Get("framework"))
	if framework == "" {
		http.Error(w, "framework is required", http.StatusBadRequest)
		return
	}
	if _, ok := compliance.FrameworkControls[framework]; !ok {
		http.Error(w, "unknown framework", http.StatusBadRequest)
		return
	}

	tenantIDStr := strings.TrimSpace(q.Get("tenant_id"))
	if tenantIDStr == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		http.Error(w, "tenant_id must be a valid UUID", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	periodEnd := now
	if v := strings.TrimSpace(q.Get("period_end")); v != "" {
		if t, ok := parseDateOrTimestamp(v); ok {
			periodEnd = t
		} else {
			http.Error(w, "period_end must be RFC3339 or YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	periodStart := periodEnd.Add(-30 * 24 * time.Hour)
	if v := strings.TrimSpace(q.Get("period_start")); v != "" {
		if t, ok := parseDateOrTimestamp(v); ok {
			periodStart = t
		} else {
			http.Error(w, "period_start must be RFC3339 or YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	if !periodStart.Before(periodEnd) {
		http.Error(w, "period_start must be before period_end", http.StatusBadRequest)
		return
	}

	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	coverage, err := s.store.GetControlCoverage(r.Context(), tenantID, framework, periodStart, periodEnd)
	if err != nil {
		s.logger.Sugar().Errorw("get control coverage", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := controlPostureResponse{
		Framework:   framework,
		TenantID:    tenantID.String(),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		GeneratedAt: now,
		Coverage:    hydrateCoverage(framework, coverage),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Sugar().Warnw("encode control posture response", "error", err)
	}
}

// hydrateCoverage joins storage rows against the in-memory framework catalog so
// callers see Title and Applicability without an extra DB hit. Catalog entries
// not present in the DB mappings are still emitted with Status="NO_COVERAGE" so
// the UI's gap analysis surface sees them.
func hydrateCoverage(framework string, rows []storage.ControlCoverage) []controlCoverageDTO {
	catalog := compliance.FrameworkControls[framework]
	byID := make(map[string]storage.ControlCoverage, len(rows))
	for _, r := range rows {
		byID[r.ControlID] = r
	}

	out := make([]controlCoverageDTO, 0, len(catalog))
	seen := make(map[string]bool, len(catalog))
	for _, c := range catalog {
		seen[c.ControlID] = true
		row, ok := byID[c.ControlID]
		dto := controlCoverageDTO{
			Framework:     framework,
			ControlID:     c.ControlID,
			Title:         c.Title,
			Applicability: c.Applicability,
		}
		if ok {
			dto.Status = row.Status
			dto.NodesChecked = row.NodesChecked
			dto.NodesPassing = row.NodesPassing
			dto.NodesFailing = row.NodesFailing
			dto.EvidenceCount = row.EvidenceCount
			dto.LastCheckedAt = row.LastCheckedAt
		} else {
			dto.Status = "NO_COVERAGE"
		}
		out = append(out, dto)
	}
	// Append any DB rows whose control_id is unknown to the catalog — surfaces
	// orphan mappings rather than silently dropping them.
	for _, r := range rows {
		if seen[r.ControlID] {
			continue
		}
		out = append(out, controlCoverageDTO{
			Framework:     framework,
			ControlID:     r.ControlID,
			Title:         r.ControlID, // unknown control: title falls back to ID
			Status:        r.Status,
			NodesChecked:  r.NodesChecked,
			NodesPassing:  r.NodesPassing,
			NodesFailing:  r.NodesFailing,
			EvidenceCount: r.EvidenceCount,
			LastCheckedAt: r.LastCheckedAt,
		})
	}
	return out
}

// parseDateOrTimestamp accepts RFC3339 or YYYY-MM-DD (treated as UTC midnight).
func parseDateOrTimestamp(v string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
