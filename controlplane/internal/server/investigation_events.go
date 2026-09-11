package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
)

const (
	investigationQueryMaxBody = 64 << 10
	investigationMaxWindow    = 30 * 24 * time.Hour
)

var errInvestigationAnalyticsUnavailable = errors.New("analytic store unavailable")

type eventsQueryRequest struct {
	TenantID      string   `json:"tenant_id"`
	NodeID        string   `json:"node_id"`
	CorrelationID string   `json:"correlation_id"`
	ConnID        string   `json:"conn_id"`
	EventID       string   `json:"event_id"`
	RawRef        string   `json:"raw_ref"`
	EventType     string   `json:"event_type"`
	EventTypes    []string `json:"event_types"`
	Severity      string   `json:"severity"`
	ParserStatus  string   `json:"parser_status"`
	Search        string   `json:"search"`
	Since         string   `json:"since"`
	Until         string   `json:"until"`
	Limit         int      `json:"limit"`
	Offset        int      `json:"offset"`
}

type timelineBuildRequest struct {
	TenantID      string             `json:"tenant_id"`
	CorrelationID string             `json:"correlation_id"`
	NodeID        string             `json:"node_id"`
	ConnID        string             `json:"conn_id"`
	EntityType    string             `json:"entity_type"`
	EntityID      string             `json:"entity_id"`
	Entity        *timelineEntityRef `json:"entity"`
	Since         string             `json:"since"`
	Until         string             `json:"until"`
	Limit         int                `json:"limit"`
}

type timelineEntityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type investigationQueryScope struct {
	TenantID uuid.UUID
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

type eventCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
	EventID        string `json:"event_id,omitempty"`
	RawRef         string `json:"raw_ref,omitempty"`
	TenantID       string `json:"tenant_id"`
}

type eventQueryItem struct {
	SourceRecordID string          `json:"source_record_id"`
	CitationIDs    []string        `json:"citation_ids"`
	SchemaVersion  int             `json:"schema_version"`
	EventID        string          `json:"event_id,omitempty"`
	RawRef         string          `json:"raw_ref,omitempty"`
	Collector      string          `json:"collector,omitempty"`
	Parser         string          `json:"parser,omitempty"`
	ParserStatus   string          `json:"parser_status,omitempty"`
	TenantID       string          `json:"tenant_id"`
	TS             time.Time       `json:"ts"`
	NodeID         string          `json:"node_id,omitempty"`
	EventType      string          `json:"event_type"`
	Severity       string          `json:"severity,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	ConnID         string          `json:"conn_id,omitempty"`
	BastionSessID  string          `json:"bastion_session_id,omitempty"`
	PID            int64           `json:"pid,omitempty"`
	ProcessName    string          `json:"process_name,omitempty"`
	UserName       string          `json:"user_name,omitempty"`
	SrcIP          string          `json:"src_ip,omitempty"`
	SrcPort        int             `json:"src_port,omitempty"`
	DstIP          string          `json:"dst_ip,omitempty"`
	DstPort        int             `json:"dst_port,omitempty"`
	Protocol       string          `json:"protocol,omitempty"`
	BytesIn        int64           `json:"bytes_in,omitempty"`
	BytesOut       int64           `json:"bytes_out,omitempty"`
	DurationMS     int64           `json:"duration_ms,omitempty"`
	RuleID         string          `json:"rule_id,omitempty"`
	ThreatFeed     string          `json:"threat_feed,omitempty"`
	ThreatScore    int             `json:"threat_score,omitempty"`
	Message        string          `json:"message,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	DedupKey       string          `json:"dedup_key,omitempty"`
}

type eventsQueryResponse struct {
	Source     string           `json:"source"`
	TenantID   string           `json:"tenant_id"`
	Since      time.Time        `json:"since"`
	Until      time.Time        `json:"until"`
	Data       []eventQueryItem `json:"data"`
	Citations  []eventCitation  `json:"citations"`
	Pagination paginationMeta   `json:"pagination"`
	Guardrails []string         `json:"guardrails,omitempty"`
}

type timelineItemResponse struct {
	SourceTable    string          `json:"source_table"`
	SourceRecordID string          `json:"source_record_id"`
	CitationIDs    []string        `json:"citation_ids"`
	SchemaVersion  int             `json:"schema_version"`
	EventID        string          `json:"event_id,omitempty"`
	RawRef         string          `json:"raw_ref,omitempty"`
	Collector      string          `json:"collector,omitempty"`
	Parser         string          `json:"parser,omitempty"`
	ParserStatus   string          `json:"parser_status,omitempty"`
	TenantID       string          `json:"tenant_id"`
	TS             time.Time       `json:"ts"`
	NodeID         string          `json:"node_id,omitempty"`
	EventType      string          `json:"event_type"`
	Severity       string          `json:"severity,omitempty"`
	Message        string          `json:"message,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	ConnID         string          `json:"conn_id,omitempty"`
	PID            int64           `json:"pid,omitempty"`
	ProcessName    string          `json:"process_name,omitempty"`
	UserName       string          `json:"user_name,omitempty"`
	SrcIP          string          `json:"src_ip,omitempty"`
	DstIP          string          `json:"dst_ip,omitempty"`
	DstPort        int             `json:"dst_port,omitempty"`
	Path           string          `json:"path,omitempty"`
	BytesIn        int64           `json:"bytes_in,omitempty"`
	BytesOut       int64           `json:"bytes_out,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
}

type timelineBuildResponse struct {
	Source     string                 `json:"source"`
	TenantID   string                 `json:"tenant_id"`
	Since      time.Time              `json:"since"`
	Until      time.Time              `json:"until"`
	Scope      map[string]string      `json:"scope"`
	Items      []timelineItemResponse `json:"items"`
	Citations  []eventCitation        `json:"citations"`
	Guardrails []string               `json:"guardrails,omitempty"`
}

func (s *Server) handleEventsQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	var req eventsQueryRequest
	if err := decodeInvestigationJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scope, guardrails, err := investigationScopeFromRequest(r, req.TenantID, req.Since, req.Until, req.Limit, req.Offset, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, scope.TenantID, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
	}
	eventTypes := append([]string(nil), req.EventTypes...)
	if strings.TrimSpace(req.EventType) != "" {
		eventTypes = append(eventTypes, req.EventType)
	}
	rows, total, source, backendGuardrails, err := s.queryInvestigationEvents(r.Context(), doris.EventQueryParams{
		TenantID:      scope.TenantID.String(),
		NodeID:        strings.TrimSpace(req.NodeID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		ConnID:        strings.TrimSpace(req.ConnID),
		EventID:       strings.TrimSpace(req.EventID),
		RawRef:        strings.TrimSpace(req.RawRef),
		EventTypes:    eventTypes,
		Severity:      strings.TrimSpace(req.Severity),
		ParserStatus:  strings.TrimSpace(req.ParserStatus),
		Search:        strings.TrimSpace(req.Search),
		Since:         scope.Since,
		Until:         scope.Until,
		Limit:         scope.Limit,
		Offset:        scope.Offset,
	})
	if err != nil {
		if errors.Is(err, errInvestigationAnalyticsUnavailable) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	guardrails = append(guardrails, backendGuardrails...)
	items := make([]eventQueryItem, 0, len(rows))
	citations := make([]eventCitation, 0, len(rows))
	for _, row := range rows {
		item, citation := eventRowToResponse(row)
		items = append(items, item)
		citations = append(citations, citation)
	}
	if !s.dbQueryTextCaptureAllowed(r.Context(), scope.TenantID) {
		redacted := redactDBQueryTextItems(items)
		if redacted > 0 {
			guardrails = append(guardrails, "db query text redacted by tenant capture policy")
		}
	}
	writeJSON(w, http.StatusOK, eventsQueryResponse{
		Source:     source,
		TenantID:   scope.TenantID.String(),
		Since:      scope.Since,
		Until:      scope.Until,
		Data:       items,
		Citations:  citations,
		Pagination: newPaginationMeta(total, scope.Limit, scope.Offset, len(items)),
		Guardrails: guardrails,
	})
}

func (s *Server) handleTimelineBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	var req timelineBuildRequest
	if err := decodeInvestigationJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Entity != nil {
		if req.EntityType == "" {
			req.EntityType = req.Entity.Type
		}
		if req.EntityID == "" {
			req.EntityID = req.Entity.ID
		}
	}
	if strings.TrimSpace(req.CorrelationID) == "" &&
		strings.TrimSpace(req.ConnID) == "" &&
		strings.TrimSpace(req.NodeID) == "" &&
		(strings.TrimSpace(req.EntityType) == "" || strings.TrimSpace(req.EntityID) == "") {
		http.Error(w, "timeline requires correlation_id, conn_id, node_id, or entity", http.StatusBadRequest)
		return
	}
	scope, guardrails, err := investigationScopeFromRequest(r, req.TenantID, req.Since, req.Until, req.Limit, 0, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, scope.TenantID, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
	}
	entityType, entityID, err := normalizeTimelineEntityScope(scope.TenantID, req.EntityType, req.EntityID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, source, backendGuardrails, err := s.buildInvestigationTimeline(r.Context(), doris.TimelineBuildParams{
		TenantID:      scope.TenantID.String(),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		NodeID:        strings.TrimSpace(req.NodeID),
		ConnID:        strings.TrimSpace(req.ConnID),
		EntityType:    entityType,
		EntityID:      entityID,
		Since:         scope.Since,
		Until:         scope.Until,
		Limit:         scope.Limit,
	})
	if err != nil {
		if errors.Is(err, errInvestigationAnalyticsUnavailable) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	guardrails = append(guardrails, backendGuardrails...)
	items := make([]timelineItemResponse, 0, len(rows))
	citations := make([]eventCitation, 0, len(rows))
	for _, row := range rows {
		item, citation := timelineRowToResponse(row)
		items = append(items, item)
		citations = append(citations, citation)
	}
	if !s.dbQueryTextCaptureAllowed(r.Context(), scope.TenantID) {
		redacted := redactTimelineDBQueryTextItems(items)
		if redacted > 0 {
			guardrails = append(guardrails, "db query text redacted by tenant capture policy")
		}
	}
	responseScope := map[string]string{}
	for key, value := range map[string]string{
		"correlation_id": strings.TrimSpace(req.CorrelationID),
		"conn_id":        strings.TrimSpace(req.ConnID),
		"node_id":        strings.TrimSpace(req.NodeID),
		"entity_type":    entityType,
		"entity_id":      entityID,
	} {
		if value != "" {
			responseScope[key] = value
		}
	}
	writeJSON(w, http.StatusOK, timelineBuildResponse{
		Source:     source,
		TenantID:   scope.TenantID.String(),
		Since:      scope.Since,
		Until:      scope.Until,
		Scope:      responseScope,
		Items:      items,
		Citations:  citations,
		Guardrails: guardrails,
	})
}

func normalizeTimelineEntityScope(tenantID uuid.UUID, entityType, entityID string) (string, string, error) {
	cleanType := strings.ToLower(strings.TrimSpace(entityType))
	cleanID := strings.TrimSpace(entityID)
	if cleanType == "tenant_id" {
		cleanType = "tenant"
	}
	if cleanType != "tenant" {
		return strings.TrimSpace(entityType), cleanID, nil
	}
	expected := tenantID.String()
	if cleanID == "" {
		cleanID = expected
	}
	if cleanID != expected {
		return "", "", fmt.Errorf("tenant entity_id must match tenant_id")
	}
	return "tenant", cleanID, nil
}

func (s *Server) queryInvestigationEvents(ctx context.Context, p doris.EventQueryParams) ([]doris.EventRow, int, string, []string, error) {
	if s != nil && s.usesDorisAnalytics() {
		rows, total, err := s.dorisClient.QueryEvents(ctx, p)
		return rows, total, "doris", nil, err
	}
	if s != nil && effectiveAnalyticsMode(s.cfg) == analyticsModeOLAP {
		return nil, 0, "doris", nil, errInvestigationAnalyticsUnavailable
	}
	if s != nil && s.localAnalytics != nil {
		rows, total, err := s.localAnalytics.QueryEvents(ctx, p)
		return rows, total, "small-analytics", []string{"Recent evidence projection currently includes connection facts; full generic, file, database, and web event search requires the OLAP profile."}, err
	}
	return nil, 0, "small-analytics-pending", []string{"Recent evidence projection is not ready yet; fleet health and durable ingest remain available while projection catches up."}, nil
}

func (s *Server) buildInvestigationTimeline(ctx context.Context, p doris.TimelineBuildParams) ([]doris.TimelineItem, string, []string, error) {
	if s != nil && s.usesDorisAnalytics() {
		rows, err := s.dorisClient.BuildTimeline(ctx, p)
		return rows, "doris", nil, err
	}
	if s != nil && effectiveAnalyticsMode(s.cfg) == analyticsModeOLAP {
		return nil, "doris", nil, errInvestigationAnalyticsUnavailable
	}
	if s != nil && s.localAnalytics != nil {
		rows, err := s.localAnalytics.BuildTimeline(ctx, p)
		return rows, "small-analytics", []string{"Recent timelines currently include connection facts; full generic, file, database, and web timelines require the OLAP profile."}, err
	}
	return nil, "small-analytics-pending", []string{"Recent timeline projection is not ready yet; fleet health and durable ingest remain available while projection catches up."}, nil
}

func (s *Server) dbQueryTextCaptureAllowed(ctx context.Context, tenantID uuid.UUID) bool {
	if s == nil || s.store == nil || tenantID == uuid.Nil {
		return false
	}
	filters, err := s.store.GetTenantEventFilters(ctx, tenantID)
	if err != nil || filters == nil {
		return false
	}
	return filters.DBQueryTextCapture || filters.ForensicMode
}

func redactDBQueryTextItems(items []eventQueryItem) int {
	redacted := 0
	for i := range items {
		if !isDBQueryEvent(items[i].EventType, "") {
			continue
		}
		items[i].Message = "[redacted: db_query_text_capture_disabled]"
		items[i].Details = json.RawMessage(`{"redacted":"db_query_text_capture_disabled"}`)
		redacted++
	}
	return redacted
}

func redactTimelineDBQueryTextItems(items []timelineItemResponse) int {
	redacted := 0
	for i := range items {
		if !isDBQueryEvent(items[i].EventType, items[i].SourceTable) {
			continue
		}
		items[i].Message = "[redacted: db_query_text_capture_disabled]"
		items[i].Details = json.RawMessage(`{"redacted":"db_query_text_capture_disabled"}`)
		redacted++
	}
	return redacted
}

func isDBQueryEvent(eventType, sourceTable string) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	sourceTable = strings.ToLower(strings.TrimSpace(sourceTable))
	return sourceTable == "db_queries" ||
		strings.Contains(eventType, "db.query") ||
		strings.Contains(eventType, "database.query") ||
		strings.Contains(eventType, "sql.query")
}

func decodeInvestigationJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, investigationQueryMaxBody)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}
	return nil
}

func investigationScopeFromRequest(r *http.Request, tenantID, sinceValue, untilValue string, limit, offset, defaultLimit int) (investigationQueryScope, []string, error) {
	if tenantID == "" && r != nil {
		tenantID = r.URL.Query().Get("tenant_id")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return investigationQueryScope{}, nil, fmt.Errorf("tenant_id is required")
	}
	parsedTenantID, err := uuid.Parse(tenantID)
	if err != nil || parsedTenantID == uuid.Nil {
		return investigationQueryScope{}, nil, fmt.Errorf("invalid tenant_id")
	}
	now := time.Now().UTC()
	until := now
	if untilValue = strings.TrimSpace(untilValue); untilValue != "" {
		parsed, err := time.Parse(time.RFC3339, untilValue)
		if err != nil {
			return investigationQueryScope{}, nil, fmt.Errorf("invalid until")
		}
		until = parsed.UTC()
	}
	since := until.Add(-24 * time.Hour)
	if sinceValue = strings.TrimSpace(sinceValue); sinceValue != "" {
		parsed, err := time.Parse(time.RFC3339, sinceValue)
		if err != nil {
			return investigationQueryScope{}, nil, fmt.Errorf("invalid since")
		}
		since = parsed.UTC()
	}
	if until.Before(since) {
		return investigationQueryScope{}, nil, fmt.Errorf("until must be after since")
	}
	if until.Sub(since) > investigationMaxWindow {
		return investigationQueryScope{}, nil, fmt.Errorf("query window exceeds 30 days")
	}
	guardrails := []string{"tenant_id required", "window <= 30d"}
	if until.After(now.Add(5 * time.Minute)) {
		return investigationQueryScope{}, nil, fmt.Errorf("until is too far in the future")
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
		guardrails = append(guardrails, "limit clamped to 500")
	}
	if offset < 0 {
		return investigationQueryScope{}, nil, fmt.Errorf("invalid offset")
	}
	return investigationQueryScope{
		TenantID: parsedTenantID,
		Since:    since,
		Until:    until,
		Limit:    limit,
		Offset:   offset,
	}, guardrails, nil
}

func eventRowToResponse(row doris.EventRow) (eventQueryItem, eventCitation) {
	sourceID := eventSourceRecordID(row.EventID, row.RawRef, "events", row.TS, row.EventType, row.ConnID, row.DedupKey)
	citation := eventCitation{
		ID:             citationID("events", sourceID),
		Kind:           "normalized_event",
		Table:          "events",
		SourceRecordID: sourceID,
		EventID:        row.EventID,
		RawRef:         row.RawRef,
		TenantID:       row.TenantID,
	}
	item := eventQueryItem{
		SourceRecordID: sourceID,
		CitationIDs:    []string{citation.ID},
		SchemaVersion:  row.SchemaVersion,
		EventID:        row.EventID,
		RawRef:         row.RawRef,
		Collector:      row.Collector,
		Parser:         row.Parser,
		ParserStatus:   row.ParserStatus,
		TenantID:       row.TenantID,
		TS:             row.TS,
		NodeID:         row.NodeID,
		EventType:      row.EventType,
		Severity:       row.Severity,
		CorrelationID:  row.CorrelationID,
		ConnID:         row.ConnID,
		BastionSessID:  row.BastionSessID,
		PID:            row.PID,
		ProcessName:    row.ProcessName,
		UserName:       row.UserName,
		SrcIP:          row.SrcIP,
		SrcPort:        row.SrcPort,
		DstIP:          row.DstIP,
		DstPort:        row.DstPort,
		Protocol:       row.Protocol,
		BytesIn:        row.BytesIn,
		BytesOut:       row.BytesOut,
		DurationMS:     row.DurationMS,
		RuleID:         row.RuleID,
		ThreatFeed:     row.ThreatFeed,
		ThreatScore:    row.ThreatScore,
		Message:        row.Message,
		DedupKey:       row.DedupKey,
	}
	item.Details = rawJSONOrNil(row.DetailsJSON)
	return item, citation
}

func timelineRowToResponse(row doris.TimelineItem) (timelineItemResponse, eventCitation) {
	sourceID := eventSourceRecordID(row.EventID, row.RawRef, row.SourceTable, row.TS, row.EventType, row.ConnID, row.Path)
	citation := eventCitation{
		ID:             citationID(row.SourceTable, sourceID),
		Kind:           "timeline_row",
		Table:          row.SourceTable,
		SourceRecordID: sourceID,
		EventID:        row.EventID,
		RawRef:         row.RawRef,
		TenantID:       row.TenantID,
	}
	item := timelineItemResponse{
		SourceTable:    row.SourceTable,
		SourceRecordID: sourceID,
		CitationIDs:    []string{citation.ID},
		SchemaVersion:  row.SchemaVersion,
		EventID:        row.EventID,
		RawRef:         row.RawRef,
		Collector:      row.Collector,
		Parser:         row.Parser,
		ParserStatus:   row.ParserStatus,
		TenantID:       row.TenantID,
		TS:             row.TS,
		NodeID:         row.NodeID,
		EventType:      row.EventType,
		Severity:       row.Severity,
		Message:        row.Message,
		CorrelationID:  row.CorrelationID,
		ConnID:         row.ConnID,
		PID:            row.PID,
		ProcessName:    row.ProcessName,
		UserName:       row.UserName,
		SrcIP:          row.SrcIP,
		DstIP:          row.DstIP,
		DstPort:        row.DstPort,
		Path:           row.Path,
		BytesIn:        row.BytesIn,
		BytesOut:       row.BytesOut,
	}
	item.Details = rawJSONOrNil(row.DetailsJSON)
	return item, citation
}

func eventSourceRecordID(eventID, rawRef, table string, ts time.Time, eventType, connID, fallback string) string {
	if strings.TrimSpace(eventID) != "" {
		return strings.TrimSpace(eventID)
	}
	if strings.TrimSpace(rawRef) != "" {
		return strings.TrimSpace(rawRef)
	}
	parts := []string{strings.TrimSpace(table), ts.UTC().Format(time.RFC3339Nano), strings.TrimSpace(eventType), strings.TrimSpace(connID), strings.TrimSpace(fallback)}
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], ":", "_")
	}
	return strings.Join(parts, ":")
}

func citationID(table, sourceID string) string {
	return strings.TrimSpace(table) + ":" + strings.TrimSpace(sourceID)
}

func rawJSONOrNil(value string) json.RawMessage {
	value = strings.TrimSpace(value)
	if value == "" || !json.Valid([]byte(value)) {
		return nil
	}
	return json.RawMessage(value)
}
