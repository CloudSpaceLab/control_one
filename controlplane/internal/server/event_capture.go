package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	metricnames "github.com/CloudSpaceLab/control_one/internal/metrics"
)

const (
	metricFileSizeBytes     = "file.size.bytes"
	maxLogTailRows          = 20
	maxLogTailMessageBytes  = 2048
	maxLogTailResponseBytes = 16 * 1024
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*=\s*[^,\s;]+`),
	regexp.MustCompile(`(?i)(api[_-]?key|secret|token)\s*=\s*[^,\s;]+`),
	regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]+`),
}

type FlowDeltaRow struct {
	TenantID    uuid.UUID `json:"tenant_id"`
	NodeID      uuid.UUID `json:"node_id"`
	Process     string    `json:"process"`
	Port        int       `json:"port"`
	Direction   string    `json:"direction"`
	PreviousCPS float64   `json:"previous_cps"`
	CurrentCPS  float64   `json:"current_cps"`
	BytesIn     int64     `json:"bytes_in"`
	BytesOut    int64     `json:"bytes_out"`
	Since       time.Time `json:"since"`
	Until       time.Time `json:"until"`
}

type FileGrowthDeltaRow struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	NodeID     uuid.UUID `json:"node_id"`
	Path       string    `json:"path"`
	StartBytes int64     `json:"start_bytes"`
	EndBytes   int64     `json:"end_bytes"`
	Since      time.Time `json:"since"`
	Until      time.Time `json:"until"`
}

type ResourceDeltaRow struct {
	TenantID    uuid.UUID `json:"tenant_id"`
	NodeID      uuid.UUID `json:"node_id"`
	Metric      string    `json:"metric"`
	Previous    float64   `json:"previous"`
	Current     float64   `json:"current"`
	SampleCount int       `json:"sample_count"`
}

type LogTailRow struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	NodeID    uuid.UUID `json:"node_id"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type RootCauseFinding struct {
	TenantID          uuid.UUID `json:"tenant_id"`
	NodeID            uuid.UUID `json:"node_id"`
	Summary           string    `json:"summary"`
	Confidence        string    `json:"confidence"`
	EvidenceIDs       []string  `json:"evidence_ids"`
	RecommendedAction string    `json:"recommended_action,omitempty"`
	ApprovalKind      string    `json:"approval_kind,omitempty"`
	ApprovalPath      string    `json:"approval_path,omitempty"`
}

type EventCaptureFilter struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Since    time.Time
	Until    time.Time
	Search   string
	Limit    int
}

type EventCaptureStore interface {
	ListFlowDeltas(context.Context, EventCaptureFilter) ([]FlowDeltaRow, error)
	ListFileGrowthDeltas(context.Context, EventCaptureFilter) ([]FileGrowthDeltaRow, error)
	ListResourceDeltas(context.Context, EventCaptureFilter) ([]ResourceDeltaRow, error)
	ListLogTail(context.Context, EventCaptureFilter) ([]LogTailRow, error)
	ListRootCauseFindings(context.Context, EventCaptureFilter) ([]RootCauseFinding, error)
}

func (s *Server) eventCaptureStore() EventCaptureStore {
	if s == nil || s.store == nil {
		return nil
	}
	if store, ok := s.store.(EventCaptureStore); ok {
		return store
	}
	return nil
}

func (s *Server) handleNodeEventCapture(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID, kind string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	roles := []string{roleViewer, roleOperator, roleAdmin}
	if kind == "log-tail" {
		roles = []string{roleOperator, roleAdmin}
	}
	if _, ok := s.authorize(w, r, roles...); !ok {
		return
	}
	tenantID, ok := tenantFromQuery(w, r)
	if !ok {
		return
	}
	filter := eventCaptureFilterFromRequest(r, tenantID, nodeID)
	payload, err := s.eventCapturePayload(r.Context(), kind, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func eventCaptureFilterFromRequest(r *http.Request, tenantID, nodeID uuid.UUID) EventCaptureFilter {
	until := time.Now().UTC()
	since := until.Add(-1 * time.Hour)
	if raw := r.URL.Query().Get("until"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			until = parsed.UTC()
		}
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			since = parsed.UTC()
		}
	}
	limit := 20
	return EventCaptureFilter{
		TenantID: tenantID,
		NodeID:   nodeID,
		Since:    since,
		Until:    until,
		Search:   r.URL.Query().Get("q"),
		Limit:    limit,
	}
}

func eventCaptureFilterFromToolInput(tenantID, nodeID uuid.UUID, input map[string]any) EventCaptureFilter {
	until := time.Now().UTC()
	since := until.Add(-1 * time.Hour)
	if raw := stringFromToolInput(input, "until"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			until = parsed.UTC()
		}
	}
	if raw := stringFromToolInput(input, "since"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			since = parsed.UTC()
		}
	}
	return EventCaptureFilter{TenantID: tenantID, NodeID: nodeID, Since: since, Until: until, Search: stringFromToolInput(input, "q"), Limit: 20}
}

func (s *Server) eventCapturePayload(ctx context.Context, kind string, filter EventCaptureFilter) (any, error) {
	store := s.eventCaptureStore()
	if store != nil {
		switch kind {
		case "flow-delta":
			return store.ListFlowDeltas(ctx, filter)
		case "file-growth-delta":
			return store.ListFileGrowthDeltas(ctx, filter)
		case "resource-delta":
			return store.ListResourceDeltas(ctx, filter)
		case "log-tail":
			rows, err := store.ListLogTail(ctx, filter)
			if err != nil {
				return nil, err
			}
			return sanitizeLogTailRows(rows), nil
		case "root-cause-findings":
			rows, err := store.ListRootCauseFindings(ctx, filter)
			if err != nil {
				return nil, err
			}
			return enrichRootCauseFindings(rows), nil
		default:
			return nil, fmt.Errorf("unknown event capture kind %s", kind)
		}
	}
	switch kind {
	case "flow-delta":
		return s.listFlowDeltas(ctx, filter)
	case "file-growth-delta":
		return s.listFileGrowthDeltas(ctx, filter)
	case "resource-delta":
		return s.listResourceDeltas(ctx, filter)
	case "log-tail":
		return s.listLogTail(ctx, filter)
	case "root-cause-findings":
		return s.listRootCauseFindings(ctx, filter)
	default:
		return nil, fmt.Errorf("unknown event capture kind %s", kind)
	}
}

func (s *Server) listFlowDeltas(ctx context.Context, filter EventCaptureFilter) ([]FlowDeltaRow, error) {
	if s == nil || s.dorisClient == nil {
		return []FlowDeltaRow{}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	span := filter.Until.Sub(filter.Since)
	if span <= 0 {
		span = time.Hour
	}
	current, err := s.dorisClient.ListConnectionsForNode(ctx, filter.TenantID.String(), filter.NodeID.String(), filter.Since, filter.Until, limit, false)
	if err != nil {
		return nil, err
	}
	previous, err := s.dorisClient.ListConnectionsForNode(ctx, filter.TenantID.String(), filter.NodeID.String(), filter.Since.Add(-span), filter.Since, limit, false)
	if err != nil {
		return nil, err
	}
	rows := rollupFlowDeltas(filter, current, previous)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CurrentCPS == rows[j].CurrentCPS {
			return rows[i].BytesIn+rows[i].BytesOut > rows[j].BytesIn+rows[j].BytesOut
		}
		return rows[i].CurrentCPS > rows[j].CurrentCPS
	})
	if filter.Limit > 0 && len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return rows, nil
}

func rollupFlowDeltas(filter EventCaptureFilter, current, previous []doris.ConnectionRow) []FlowDeltaRow {
	type acc struct {
		FlowDeltaRow
		count int
	}
	keyFor := func(row doris.ConnectionRow) string {
		port := row.DstPort
		if port == 0 {
			port = row.SrcPort
		}
		return fmt.Sprintf("%s|%d|%s", strings.TrimSpace(row.ProcessName), port, strings.TrimSpace(row.Direction))
	}
	spanSeconds := filter.Until.Sub(filter.Since).Seconds()
	if spanSeconds <= 0 {
		spanSeconds = 1
	}
	byKey := map[string]*acc{}
	for _, row := range current {
		key := keyFor(row)
		a := byKey[key]
		if a == nil {
			port := row.DstPort
			if port == 0 {
				port = row.SrcPort
			}
			a = &acc{FlowDeltaRow: FlowDeltaRow{
				TenantID:  filter.TenantID,
				NodeID:    filter.NodeID,
				Process:   row.ProcessName,
				Port:      port,
				Direction: row.Direction,
				Since:     filter.Since,
				Until:     filter.Until,
			}}
			byKey[key] = a
		}
		a.count++
		a.BytesIn += row.BytesIn
		a.BytesOut += row.BytesOut
	}
	previousCounts := map[string]int{}
	for _, row := range previous {
		previousCounts[keyFor(row)]++
	}
	out := make([]FlowDeltaRow, 0, len(byKey))
	for key, a := range byKey {
		a.CurrentCPS = float64(a.count) / spanSeconds
		a.PreviousCPS = float64(previousCounts[key]) / spanSeconds
		out = append(out, a.FlowDeltaRow)
	}
	return out
}

func (s *Server) listFileGrowthDeltas(ctx context.Context, filter EventCaptureFilter) ([]FileGrowthDeltaRow, error) {
	if s == nil || s.store == nil {
		return []FileGrowthDeltaRow{}, nil
	}
	rows, _, err := s.store.ListTelemetryMetrics(ctx, storage.TelemetryMetricFilter{
		TenantID:   filter.TenantID,
		NodeID:     filter.NodeID,
		MetricName: metricFileSizeBytes,
		Since:      &filter.Since,
		Until:      &filter.Until,
	}, 5000, 0)
	if err != nil {
		return nil, err
	}
	return fileGrowthDeltasFromTelemetryMetrics(filter, rows), nil
}

func fileGrowthDeltasFromTelemetryMetrics(filter EventCaptureFilter, metrics []storage.TelemetryMetric) []FileGrowthDeltaRow {
	type span struct {
		first storage.TelemetryMetric
		last  storage.TelemetryMetric
		seen  bool
	}
	byPath := map[string]*span{}
	for _, metric := range metrics {
		if metric.TenantID != filter.TenantID || metric.NodeID != filter.NodeID || metric.MetricName != metricFileSizeBytes {
			continue
		}
		path := strings.TrimSpace(firstNonEmpty(metric.Labels["path"], metric.Labels["file_path"], metric.Labels["name"]))
		if path == "" {
			continue
		}
		item := byPath[path]
		if item == nil {
			item = &span{first: metric, last: metric, seen: true}
			byPath[path] = item
			continue
		}
		if metric.Timestamp.Before(item.first.Timestamp) {
			item.first = metric
		}
		if metric.Timestamp.After(item.last.Timestamp) {
			item.last = metric
		}
	}
	out := make([]FileGrowthDeltaRow, 0, len(byPath))
	for path, item := range byPath {
		if !item.seen {
			continue
		}
		start := int64(item.first.MetricValue)
		end := int64(item.last.MetricValue)
		if end < start {
			continue
		}
		out = append(out, FileGrowthDeltaRow{
			TenantID:   filter.TenantID,
			NodeID:     filter.NodeID,
			Path:       path,
			StartBytes: start,
			EndBytes:   end,
			Since:      item.first.Timestamp,
			Until:      item.last.Timestamp,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].EndBytes-out[i].StartBytes > out[j].EndBytes-out[j].StartBytes
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out
}

func (s *Server) listResourceDeltas(ctx context.Context, filter EventCaptureFilter) ([]ResourceDeltaRow, error) {
	if s == nil || s.store == nil {
		return []ResourceDeltaRow{}, nil
	}
	metricNames := []string{
		metricnames.MetricCPUUsagePercent,
		metricnames.MetricMemoryUsedPercent,
		metricnames.MetricDiskUsagePercent,
		metricnames.MetricLoad1,
		metricnames.MetricLoad5,
		metricnames.MetricLoad15,
	}
	out := make([]ResourceDeltaRow, 0, len(metricNames))
	for _, name := range metricNames {
		rows, _, err := s.store.ListTelemetryMetrics(ctx, storage.TelemetryMetricFilter{
			TenantID:   filter.TenantID,
			NodeID:     filter.NodeID,
			MetricName: name,
			Since:      &filter.Since,
			Until:      &filter.Until,
		}, 1000, 0)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, ResourceDeltaRow{
			TenantID:    filter.TenantID,
			NodeID:      filter.NodeID,
			Metric:      name,
			Previous:    rows[len(rows)-1].MetricValue,
			Current:     rows[0].MetricValue,
			SampleCount: len(rows),
		})
	}
	return out, nil
}

func (s *Server) listLogTail(ctx context.Context, filter EventCaptureFilter) ([]LogTailRow, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if s != nil && s.dorisClient != nil {
		rows, _, err := s.dorisClient.SearchLogs(ctx, doris.LogSearchParams{
			TenantID: filter.TenantID.String(),
			NodeID:   filter.NodeID.String(),
			Search:   filter.Search,
			Since:    filter.Since,
			Until:    filter.Until,
			Limit:    limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]LogTailRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, LogTailRow{
				TenantID:  filter.TenantID,
				NodeID:    filter.NodeID,
				Source:    firstNonEmpty(row.Source, row.Program, row.Level),
				Message:   row.Message,
				Timestamp: row.Timestamp,
			})
		}
		return sanitizeLogTailRows(out), nil
	}
	if s == nil || s.store == nil {
		return []LogTailRow{}, nil
	}
	rows, _, err := s.store.ListTelemetryLogs(ctx, storage.TelemetryLogFilter{
		TenantID: filter.TenantID,
		NodeID:   filter.NodeID,
		Search:   filter.Search,
		Since:    &filter.Since,
		Until:    &filter.Until,
	}, limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]LogTailRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, LogTailRow{
			TenantID:  row.TenantID,
			NodeID:    row.NodeID,
			Source:    firstNonEmpty(row.LogSource.String, row.LogProgram.String, row.LogLevel),
			Message:   row.LogMessage,
			Timestamp: row.Timestamp,
		})
	}
	return sanitizeLogTailRows(out), nil
}

func (s *Server) listRootCauseFindings(ctx context.Context, filter EventCaptureFilter) ([]RootCauseFinding, error) {
	flows, err := s.listFlowDeltas(ctx, filter)
	if err != nil {
		return nil, err
	}
	files, err := s.listFileGrowthDeltas(ctx, filter)
	if err != nil {
		return nil, err
	}
	resources, err := s.listResourceDeltas(ctx, filter)
	if err != nil {
		return nil, err
	}
	logs, err := s.listLogTail(ctx, filter)
	if err != nil {
		return nil, err
	}
	summary := synthesizeRootCauseSummary(flows, files, resources, logs)
	if summary == "" {
		return []RootCauseFinding{}, nil
	}
	return []RootCauseFinding{{
		TenantID:          filter.TenantID,
		NodeID:            filter.NodeID,
		Summary:           summary,
		Confidence:        rootCauseConfidence(flows, files, resources, logs),
		EvidenceIDs:       rootCauseEvidenceIDs(flows, files, resources, logs),
		RecommendedAction: recommendedRootCauseAction(files, logs),
		ApprovalKind:      "remediation",
		ApprovalPath:      "/api/v1/remediation/approvals",
	}}, nil
}

func synthesizeRootCauseSummary(flows []FlowDeltaRow, files []FileGrowthDeltaRow, resources []ResourceDeltaRow, logs []LogTailRow) string {
	parts := make([]string, 0, 4)
	if len(flows) > 0 {
		top := flows[0]
		parts = append(parts, fmt.Sprintf("%s traffic on port %d rose from %.2f cps to %.2f cps", firstNonEmpty(top.Process, "process"), top.Port, top.PreviousCPS, top.CurrentCPS))
	}
	for _, row := range resources {
		if row.Current-row.Previous >= 20 || row.Current >= 90 {
			parts = append(parts, fmt.Sprintf("%s moved from %.2f to %.2f", row.Metric, row.Previous, row.Current))
			break
		}
	}
	if len(files) > 0 {
		top := files[0]
		parts = append(parts, fmt.Sprintf("%s grew from %d to %d bytes", top.Path, top.StartBytes, top.EndBytes))
	}
	if len(logs) > 0 {
		parts = append(parts, "recent logs include: "+logs[0].Message)
	}
	return strings.Join(parts, "; ")
}

func rootCauseConfidence(flows []FlowDeltaRow, files []FileGrowthDeltaRow, resources []ResourceDeltaRow, logs []LogTailRow) string {
	signals := 0
	if len(flows) > 0 {
		signals++
	}
	if len(files) > 0 {
		signals++
	}
	if len(resources) > 0 {
		signals++
	}
	if len(logs) > 0 {
		signals++
	}
	if signals >= 3 {
		return "high"
	}
	if signals == 2 {
		return "medium"
	}
	return "low"
}

func rootCauseEvidenceIDs(flows []FlowDeltaRow, files []FileGrowthDeltaRow, resources []ResourceDeltaRow, logs []LogTailRow) []string {
	ids := make([]string, 0, 4)
	if len(flows) > 0 {
		ids = append(ids, "flow-delta")
	}
	if len(files) > 0 {
		ids = append(ids, "file-growth-delta")
	}
	if len(resources) > 0 {
		ids = append(ids, "resource-delta")
	}
	if len(logs) > 0 {
		ids = append(ids, "log-tail")
	}
	return ids
}

func enrichRootCauseFindings(findings []RootCauseFinding) []RootCauseFinding {
	out := make([]RootCauseFinding, 0, len(findings))
	for _, finding := range findings {
		finding.EvidenceIDs = canonicalEvidenceIDs(finding.EvidenceIDs)
		if finding.RecommendedAction == "" {
			finding.RecommendedAction = "review and remediate the cited evidence through the approval workflow"
		}
		if finding.ApprovalKind == "" {
			finding.ApprovalKind = "remediation"
		}
		if finding.ApprovalPath == "" {
			finding.ApprovalPath = "/api/v1/remediation/approvals"
		}
		out = append(out, finding)
	}
	return out
}

func canonicalEvidenceIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		canonical := strings.ToLower(strings.TrimSpace(id))
		switch canonical {
		case "flow":
			canonical = "flow-delta"
		case "files", "file", "file-growth":
			canonical = "file-growth-delta"
		case "resources", "resource":
			canonical = "resource-delta"
		case "logs", "log":
			canonical = "log-tail"
		}
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out
}

func recommendedRootCauseAction(files []FileGrowthDeltaRow, logs []LogTailRow) string {
	if len(files) > 0 {
		return "archive and rotate " + files[0].Path + " through remediation approval"
	}
	if len(logs) > 0 {
		return "review noisy log source " + logs[0].Source + " through remediation approval"
	}
	return "review and remediate the cited evidence through the approval workflow"
}

func sanitizeLogTailRows(rows []LogTailRow) []LogTailRow {
	out := make([]LogTailRow, 0, minInt(len(rows), maxLogTailRows))
	totalBytes := 0
	for _, row := range rows {
		if len(out) >= maxLogTailRows || totalBytes >= maxLogTailResponseBytes {
			break
		}
		row.Message = redactLogMessage(row.Message)
		if len(row.Message) > maxLogTailMessageBytes {
			row.Message = row.Message[:maxLogTailMessageBytes] + "...[truncated]"
		}
		totalBytes += len(row.Message)
		out = append(out, row)
	}
	return out
}

func redactLogMessage(message string) string {
	out := message
	for _, pattern := range secretPatterns {
		out = pattern.ReplaceAllStringFunc(out, func(match string) string {
			if idx := strings.Index(match, "="); idx >= 0 {
				return strings.TrimSpace(match[:idx+1]) + "[REDACTED]"
			}
			fields := strings.Fields(match)
			if len(fields) > 0 {
				return fields[0] + " [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
