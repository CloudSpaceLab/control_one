package server

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// handleReportsCollection returns a descriptor of the reports the UI can
// request. Keeping this static for the go-live MVP; scheduled delivery and
// PDF output will come after the core pipeline works end-to-end.
func (s *Server) handleReportsCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	type reportDesc struct {
		Slug         string   `json:"slug"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		DefaultRange string   `json:"default_range"`
		Formats      []string `json:"formats"`
	}
	reports := []reportDesc{
		{Slug: "compliance", Title: "Compliance posture", Description: "Pass/fail per rule + severity rollup.", DefaultRange: "30d", Formats: []string{"csv"}},
		{Slug: "audit", Title: "Audit log", Description: "All control-plane actions for the window.", DefaultRange: "7d", Formats: []string{"csv"}},
		{Slug: "alerts", Title: "Alerts summary", Description: "Open + resolved alerts over the window.", DefaultRange: "7d", Formats: []string{"csv"}},
		{Slug: "access", Title: "Access requests", Description: "JIT requests and decisions.", DefaultRange: "30d", Formats: []string{"csv"}},
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": reports})
}

// handleReportExport streams a CSV-formatted report for the given slug. The
// window is controlled by ?since=RFC3339; tenants by ?tenant_id=. CSV is the
// only format in this build — the UI maps formats[] to available buttons.
func (s *Server) handleReportExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/reports/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(w, r)
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	since := parseSinceOrDefault(r, 30*24*time.Hour)

	var rows [][]string
	switch slug {
	case "compliance":
		rows = s.buildComplianceReport(r, tenantID, since)
	case "audit":
		rows = s.buildAuditReport(r, tenantID, since)
	case "alerts":
		rows = s.buildAlertsReport(r, tenantID, since)
	case "access":
		rows = s.buildAccessReport(r, tenantID, since)
	default:
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%s-%s.csv"`, slug, time.Now().UTC().Format("20060102T150405Z")))

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			s.logger.Warn("csv write", zap.Error(err))
			break
		}
	}
	writer.Flush()
	_, _ = w.Write(buf.Bytes())
}

func parseSinceOrDefault(r *http.Request, fallback time.Duration) time.Time {
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Now().UTC().Add(-fallback)
}

func (s *Server) buildComplianceReport(r *http.Request, tenantID uuid.UUID, since time.Time) [][]string {
	rows := [][]string{{"rule_id", "node_id", "passed", "severity", "checked_at"}}
	results, _, err := s.store.ListComplianceResultsFiltered(r.Context(), storage.ComplianceResultFilter{TenantID: tenantID, Since: &since}, 5000, 0)
	if err != nil {
		s.logger.Warn("report compliance", zap.Error(err))
		return rows
	}
	for _, c := range results {
		sev := ""
		if c.Severity != nil {
			sev = *c.Severity
		}
		checked := ""
		if c.CheckedAt != nil {
			checked = formatTime(*c.CheckedAt)
		}
		rows = append(rows, []string{c.RuleID, c.NodeID.String(), strconv.FormatBool(c.Passed), sev, checked})
	}
	return rows
}

func (s *Server) buildAuditReport(r *http.Request, tenantID uuid.UUID, since time.Time) [][]string {
	rows := [][]string{{"occurred_at", "actor_id", "action", "resource_type", "resource_id"}}
	filter := storage.AuditLogFilter{Since: &since, TenantID: tenantID}
	logs, _, err := s.store.ListAuditLogs(r.Context(), filter, 5000, 0)
	if err != nil {
		s.logger.Warn("report audit", zap.Error(err))
		return rows
	}
	for _, l := range logs {
		resID := ""
		if l.ResourceID != nil {
			resID = *l.ResourceID
		}
		rows = append(rows, []string{formatTime(l.CreatedAt), l.ActorID.String(), l.Action, l.ResourceType, resID})
	}
	return rows
}

func (s *Server) buildAlertsReport(r *http.Request, tenantID uuid.UUID, since time.Time) [][]string {
	rows := [][]string{{"opened_at", "severity", "state", "source", "title", "node_id"}}
	alerts, _, err := s.store.ListAlerts(r.Context(), storage.AlertFilter{TenantID: tenantID, Since: &since}, 5000, 0)
	if err != nil {
		s.logger.Warn("report alerts", zap.Error(err))
		return rows
	}
	for _, a := range alerts {
		nodeID := ""
		if a.NodeID.Valid {
			nodeID = a.NodeID.UUID.String()
		}
		rows = append(rows, []string{formatTime(a.OpenedAt), a.Severity, a.State, a.Source, a.Title, nodeID})
	}
	return rows
}

func (s *Server) buildAccessReport(r *http.Request, tenantID uuid.UUID, since time.Time) [][]string {
	rows := [][]string{{"requested_at", "user_id", "resource_type", "requested_access", "status", "decided_at", "decided_by", "ttl_seconds"}}
	reqs, _, err := s.store.ListAccessRequests(r.Context(), storage.AccessRequestFilter{TenantID: tenantID}, 5000, 0)
	if err != nil {
		s.logger.Warn("report access", zap.Error(err))
		return rows
	}
	for _, a := range reqs {
		if a.RequestedAt.Before(since) {
			continue
		}
		uid := ""
		if a.UserID.Valid {
			uid = a.UserID.UUID.String()
		}
		decidedAt := ""
		if a.DecidedAt.Valid {
			decidedAt = formatTime(a.DecidedAt.Time)
		}
		decidedBy := ""
		if a.DecidedBy.Valid {
			decidedBy = a.DecidedBy.UUID.String()
		}
		rows = append(rows, []string{formatTime(a.RequestedAt), uid, a.TargetResourceType, a.RequestedAccess, a.Status, decidedAt, decidedBy, strconv.Itoa(a.TTLSeconds)})
	}
	return rows
}
