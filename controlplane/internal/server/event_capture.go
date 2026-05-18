package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	metricnames "github.com/CloudSpaceLab/control_one/internal/metrics"
)

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
	TenantID    uuid.UUID `json:"tenant_id"`
	NodeID      uuid.UUID `json:"node_id"`
	Summary     string    `json:"summary"`
	Confidence  string    `json:"confidence"`
	EvidenceIDs []string  `json:"evidence_ids"`
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
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
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
			return store.ListLogTail(ctx, filter)
		case "root-cause-findings":
			return store.ListRootCauseFindings(ctx, filter)
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

func (s *Server) listFileGrowthDeltas(context.Context, EventCaptureFilter) ([]FileGrowthDeltaRow, error) {
	return []FileGrowthDeltaRow{}, nil
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
		return out, nil
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
	return out, nil
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
		TenantID:    filter.TenantID,
		NodeID:      filter.NodeID,
		Summary:     summary,
		Confidence:  rootCauseConfidence(flows, files, resources, logs),
		EvidenceIDs: rootCauseEvidenceIDs(flows, files, resources, logs),
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
