package server

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type complianceResultResponse struct {
	ID          string         `json:"id"`
	JobID       string         `json:"job_id"`
	TenantID    *string        `json:"tenant_id,omitempty"`
	NodeID      *string        `json:"node_id,omitempty"`
	ScanID      *string        `json:"scan_id,omitempty"`
	RuleID      string         `json:"rule_id"`
	Passed      bool           `json:"passed"`
	Severity    *string        `json:"severity,omitempty"`
	Details     *string        `json:"details,omitempty"`
	Remediation *string        `json:"remediation,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CheckedAt   *string        `json:"checked_at,omitempty"`
	CreatedAt   string         `json:"created_at"`
}

type complianceSummaryResponse struct {
	Total       int            `json:"total"`
	Passed      int            `json:"passed"`
	Failed      int            `json:"failed"`
	BySeverity  map[string]int `json:"by_severity"`
	ByRuleID    map[string]int `json:"by_rule_id,omitempty"`
	LastChecked *string        `json:"last_checked,omitempty"`
}

func (s *Server) handleComplianceResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.ComplianceResultFilter{}

	if jobParam := strings.TrimSpace(r.URL.Query().Get("job_id")); jobParam != "" {
		parsed, err := uuid.Parse(jobParam)
		if err != nil {
			http.Error(w, "invalid job_id", http.StatusBadRequest)
			return
		}
		filter.JobID = parsed
	}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	if scanParam := strings.TrimSpace(r.URL.Query().Get("scan_id")); scanParam != "" {
		filter.ScanID = scanParam
	}

	if ruleParam := strings.TrimSpace(r.URL.Query().Get("rule_id")); ruleParam != "" {
		filter.RuleID = ruleParam
	}

	if passedParam := strings.TrimSpace(r.URL.Query().Get("passed")); passedParam != "" {
		passed := parseBoolQuery(passedParam)
		filter.Passed = &passed
	}

	if severityParam := strings.TrimSpace(r.URL.Query().Get("severity")); severityParam != "" {
		filter.Severity = severityParam
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	results, total, err := s.store.ListComplianceResultsFiltered(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list compliance results", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]complianceResultResponse, 0, len(results))
	for _, result := range results {
		respItems = append(respItems, newComplianceResultResponse(result))
	}

	resp := paginatedResponse[complianceResultResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleComplianceSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	filter := storage.ComplianceResultFilter{}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	agg, err := s.store.GetComplianceAggregation(r.Context(), filter)
	if err != nil {
		s.logger.Error("get compliance aggregation", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	summary := complianceSummaryResponse{
		Total:      agg.Total,
		Passed:     agg.Passed,
		Failed:     agg.Failed,
		BySeverity: agg.BySeverity,
		ByRuleID:   agg.ByRuleID,
	}

	if agg.LastChecked != nil {
		ts := agg.LastChecked.UTC().Format(time.RFC3339)
		summary.LastChecked = &ts
	}

	includeByRuleID := parseBoolQuery(r.URL.Query().Get("include_by_rule_id"))
	if !includeByRuleID {
		summary.ByRuleID = nil
	}

	writeJSON(w, http.StatusOK, summary)
}

func newComplianceResultResponse(result storage.ComplianceResult) complianceResultResponse {
	resp := complianceResultResponse{
		ID:        result.ID.String(),
		JobID:     result.JobID.String(),
		RuleID:    result.RuleID,
		Passed:    result.Passed,
		Metadata:  result.Metadata,
		CreatedAt: formatTime(result.CreatedAt),
	}

	if result.TenantID != uuid.Nil {
		tid := result.TenantID.String()
		resp.TenantID = &tid
	}
	if result.NodeID != uuid.Nil {
		nid := result.NodeID.String()
		resp.NodeID = &nid
	}
	if result.ScanID != nil {
		resp.ScanID = result.ScanID
	}
	if result.Severity != nil {
		resp.Severity = result.Severity
	}
	if result.Details != nil {
		resp.Details = result.Details
	}
	if result.Remediation != nil {
		resp.Remediation = result.Remediation
	}
	if result.CheckedAt != nil {
		ts := result.CheckedAt.UTC().Format(time.RFC3339)
		resp.CheckedAt = &ts
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}

	return resp
}

type complianceTrendsResponse struct {
	Trends []complianceTrendItem `json:"trends"`
}

type complianceTrendItem struct {
	Date     string  `json:"date"`
	Total    int     `json:"total"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	PassRate float64 `json:"pass_rate"`
}

func (s *Server) handleComplianceTrends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	filter := storage.ComplianceResultFilter{}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	intervalDays := 1
	if intervalParam := strings.TrimSpace(r.URL.Query().Get("interval_days")); intervalParam != "" {
		parsed, err := strconv.Atoi(intervalParam)
		if err == nil && parsed > 0 && parsed <= 90 {
			intervalDays = parsed
		}
	}

	trends, err := s.store.GetComplianceTrends(r.Context(), filter, intervalDays)
	if err != nil {
		s.logger.Error("get compliance trends", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	items := make([]complianceTrendItem, 0, len(trends))
	for _, trend := range trends {
		items = append(items, complianceTrendItem{
			Date:     trend.Date.UTC().Format(time.RFC3339),
			Total:    trend.Total,
			Passed:   trend.Passed,
			Failed:   trend.Failed,
			PassRate: trend.PassRate,
		})
	}

	resp := complianceTrendsResponse{Trends: items}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleComplianceExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		http.Error(w, "format must be 'json' or 'csv'", http.StatusBadRequest)
		return
	}

	filter := storage.ComplianceResultFilter{}

	if jobParam := strings.TrimSpace(r.URL.Query().Get("job_id")); jobParam != "" {
		parsed, err := uuid.Parse(jobParam)
		if err != nil {
			http.Error(w, "invalid job_id", http.StatusBadRequest)
			return
		}
		filter.JobID = parsed
	}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	if scanParam := strings.TrimSpace(r.URL.Query().Get("scan_id")); scanParam != "" {
		filter.ScanID = scanParam
	}

	if ruleParam := strings.TrimSpace(r.URL.Query().Get("rule_id")); ruleParam != "" {
		filter.RuleID = ruleParam
	}

	if passedParam := strings.TrimSpace(r.URL.Query().Get("passed")); passedParam != "" {
		passed := parseBoolQuery(passedParam)
		filter.Passed = &passed
	}

	if severityParam := strings.TrimSpace(r.URL.Query().Get("severity")); severityParam != "" {
		filter.Severity = severityParam
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	results, _, err := s.store.ListComplianceResultsFiltered(r.Context(), filter, 0, 0)
	if err != nil {
		s.logger.Error("list compliance results for export", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="compliance-export-`+time.Now().UTC().Format("2006-01-02")+`.csv"`)
		csvWriter := csv.NewWriter(w)
		defer csvWriter.Flush()

		headers := []string{"ID", "Job ID", "Tenant ID", "Node ID", "Scan ID", "Rule ID", "Passed", "Severity", "Details", "Remediation", "Checked At", "Created At"}
		if err := csvWriter.Write(headers); err != nil {
			s.logger.Error("write CSV header", zap.Error(err))
			return
		}

		for _, result := range results {
			row := []string{
				result.ID.String(),
				result.JobID.String(),
			}
			if result.TenantID != uuid.Nil {
				row = append(row, result.TenantID.String())
			} else {
				row = append(row, "")
			}
			if result.NodeID != uuid.Nil {
				row = append(row, result.NodeID.String())
			} else {
				row = append(row, "")
			}
			if result.ScanID != nil {
				row = append(row, *result.ScanID)
			} else {
				row = append(row, "")
			}
			row = append(row, result.RuleID)
			if result.Passed {
				row = append(row, "true")
			} else {
				row = append(row, "false")
			}
			if result.Severity != nil {
				row = append(row, *result.Severity)
			} else {
				row = append(row, "")
			}
			if result.Details != nil {
				row = append(row, *result.Details)
			} else {
				row = append(row, "")
			}
			if result.Remediation != nil {
				row = append(row, *result.Remediation)
			} else {
				row = append(row, "")
			}
			if result.CheckedAt != nil {
				row = append(row, result.CheckedAt.UTC().Format(time.RFC3339))
			} else {
				row = append(row, "")
			}
			row = append(row, result.CreatedAt.UTC().Format(time.RFC3339))

			if err := csvWriter.Write(row); err != nil {
				s.logger.Error("write CSV row", zap.Error(err))
				return
			}
		}
	} else {
		respItems := make([]complianceResultResponse, 0, len(results))
		for _, result := range results {
			respItems = append(respItems, newComplianceResultResponse(result))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="compliance-export-`+time.Now().UTC().Format("2006-01-02")+`.json"`)
		if err := json.NewEncoder(w).Encode(respItems); err != nil {
			s.logger.Error("encode JSON export", zap.Error(err))
		}
	}
}

func (s *Server) handleComplianceNodeHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/compliance/nodes/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) < 2 || segments[1] != "history" {
		http.NotFound(w, r)
		return
	}

	nodeID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.ComplianceResultFilter{
		NodeID: nodeID,
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	if ruleParam := strings.TrimSpace(r.URL.Query().Get("rule_id")); ruleParam != "" {
		filter.RuleID = ruleParam
	}

	if passedParam := strings.TrimSpace(r.URL.Query().Get("passed")); passedParam != "" {
		passed := parseBoolQuery(passedParam)
		filter.Passed = &passed
	}

	if severityParam := strings.TrimSpace(r.URL.Query().Get("severity")); severityParam != "" {
		filter.Severity = severityParam
	}

	results, total, err := s.store.ListComplianceResultsFiltered(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list node compliance history", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]complianceResultResponse, 0, len(results))
	for _, result := range results {
		respItems = append(respItems, newComplianceResultResponse(result))
	}

	resp := paginatedResponse[complianceResultResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

