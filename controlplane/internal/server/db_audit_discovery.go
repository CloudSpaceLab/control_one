package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type dbAuditDiscoveryResponse struct {
	TenantID      string                     `json:"tenant_id"`
	Since         time.Time                  `json:"since"`
	Until         time.Time                  `json:"until"`
	Source        string                     `json:"source"`
	CapturePolicy dbAuditCapturePolicy       `json:"capture_policy"`
	Summary       dbAuditDiscoverySummary    `json:"summary"`
	Candidates    []dbAuditCandidate         `json:"candidates"`
	Citations     []dbAuditDiscoveryCitation `json:"citations"`
	Guardrails    []string                   `json:"guardrails"`
}

type dbAuditCapturePolicy struct {
	Source             string `json:"source"`
	CaptureDBQueries   bool   `json:"capture_db_queries"`
	DBQueryTextCapture bool   `json:"db_query_text_capture"`
	ForensicMode       bool   `json:"forensic_mode"`
}

type dbAuditDiscoverySummary struct {
	Candidates           int `json:"candidates"`
	ServicesDetected     int `json:"services_detected"`
	QueryEventsSeen      int `json:"query_events_seen"`
	LongRunningSeen      int `json:"long_running_seen"`
	MissingAccessTargets int `json:"missing_access_targets"`
}

type dbAuditCandidate struct {
	ID                     string                        `json:"id"`
	NodeID                 string                        `json:"node_id"`
	Hostname               string                        `json:"hostname,omitempty"`
	Engine                 string                        `json:"engine"`
	CoverageState          string                        `json:"coverage_state"`
	Host                   string                        `json:"host,omitempty"`
	Port                   int                           `json:"port,omitempty"`
	Process                string                        `json:"process,omitempty"`
	ServiceKind            string                        `json:"service_kind,omitempty"`
	Sources                []string                      `json:"sources"`
	CoverageStatus         []string                      `json:"coverage_status"`
	AccessState            string                        `json:"access_state"`
	CaptureDBQueries       bool                          `json:"capture_db_queries"`
	DBQueryTextCapture     bool                          `json:"db_query_text_capture"`
	RecentQueryEvents      int                           `json:"recent_query_events"`
	RecentLongRunning      int                           `json:"recent_long_running_events"`
	CitationIDs            []string                      `json:"citation_ids"`
	LastObservedEventAt    string                        `json:"last_observed_event_at,omitempty"`
	MissingAccessRationale string                        `json:"missing_access_rationale,omitempty"`
	AccessRequest          *dbAuditAccessRequestArtifact `json:"access_request,omitempty"`
	SourceCoverage         []dbAuditSourceCoverage       `json:"source_coverage,omitempty"`
	SideChannelEvidence    []dbAuditSideChannelEvidence  `json:"side_channel_evidence,omitempty"`
}

type dbAuditAccessRequestArtifact struct {
	Engine             string   `json:"engine"`
	LeastPrivilegeRole string   `json:"least_privilege_role"`
	GrantStatements    []string `json:"grant_statements"`
	SetupNotes         []string `json:"setup_notes,omitempty"`
	ExpectedEvidence   []string `json:"expected_evidence"`
	RiskNote           string   `json:"risk_note"`
}

type dbAuditSourceCoverage struct {
	Engine             string   `json:"engine"`
	Source             string   `json:"source"`
	State              string   `json:"state"`
	RequiredPrivileges []string `json:"required_privileges,omitempty"`
	ExpectedEvidence   []string `json:"expected_evidence,omitempty"`
}

type dbAuditSideChannelEvidence struct {
	Kind       string `json:"kind"`
	CitationID string `json:"citation_id"`
	Detail     string `json:"detail"`
}

type dbAuditDiscoveryCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
	NodeID         string `json:"node_id,omitempty"`
}

type dbAuditDiscoveryQuery struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Since    time.Time
	Until    time.Time
	Limit    int
}

type dbAuditEventAggregate struct {
	NodeID           string
	Engine           string
	QueryCount       int
	LongRunningCount int
	LastSeen         time.Time
	CitationID       string
}

func (s *Server) handleDBAuditDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}

	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), 100)
	if err != nil {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	nodeID, err := parseOptionalUUID(r.URL.Query().Get("node_id"))
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	scope, guardrails, err := investigationScopeFromRequest(
		r,
		r.URL.Query().Get("tenant_id"),
		r.URL.Query().Get("since"),
		r.URL.Query().Get("until"),
		limit,
		0,
		100,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, scope.TenantID, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
	}
	resp, err := s.buildDBAuditDiscovery(r.Context(), dbAuditDiscoveryQuery{
		TenantID: scope.TenantID,
		NodeID:   nodeID,
		Since:    scope.Since,
		Until:    scope.Until,
		Limit:    scope.Limit,
	}, guardrails)
	if err != nil {
		if writeTenantScopedNodeError(w, err) {
			return
		}
		s.logger.Error("db audit discovery", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) buildDBAuditDiscovery(ctx context.Context, q dbAuditDiscoveryQuery, guardrails []string) (dbAuditDiscoveryResponse, error) {
	if q.Limit <= 0 || q.Limit > maxListLimit {
		q.Limit = 100
	}
	resp := dbAuditDiscoveryResponse{
		TenantID:   q.TenantID.String(),
		Since:      q.Since,
		Until:      q.Until,
		Source:     "controlplane",
		Guardrails: append([]string{"read_only", "no_database_credentials", "no_live_db_connections"}, guardrails...),
	}
	if q.NodeID != uuid.Nil {
		if _, err := s.ensureNodeInTenant(ctx, q.TenantID, q.NodeID); err != nil {
			return resp, err
		}
	}

	filters, err := s.store.GetTenantEventFilters(ctx, q.TenantID)
	if err != nil {
		resp.Guardrails = append(resp.Guardrails, "tenant_event_filters_unavailable")
	} else if filters != nil {
		resp.CapturePolicy = dbAuditCapturePolicy{
			Source:             "tenant_event_filters",
			CaptureDBQueries:   filters.CaptureDBQueries,
			DBQueryTextCapture: filters.DBQueryTextCapture,
			ForensicMode:       filters.ForensicMode,
		}
		resp.Citations = append(resp.Citations, dbAuditDiscoveryCitation{
			ID:             citationID("tenant_event_filters", q.TenantID.String()),
			Kind:           "capture_policy",
			Table:          "tenant_event_filters",
			SourceRecordID: "tenant_event_filters:" + q.TenantID.String(),
		})
	}

	nodes, _, err := s.store.ListNodes(ctx, q.TenantID, "", maxListLimit, 0)
	if err != nil {
		return resp, err
	}
	nodeByID := make(map[uuid.UUID]storage.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}

	services, err := s.store.ListNodeServicesForTenant(ctx, q.TenantID)
	if err != nil {
		return resp, err
	}
	eventAgg, eventCitations, eventGuardrails := s.dbAuditEventAggregates(ctx, q)
	resp.Citations = append(resp.Citations, eventCitations...)
	resp.Guardrails = append(resp.Guardrails, eventGuardrails...)

	candidates := map[string]*dbAuditCandidate{}
	for _, svc := range services {
		if q.NodeID != uuid.Nil && svc.NodeID != q.NodeID {
			continue
		}
		engine := detectDBServiceEngine(svc)
		if engine == "" {
			continue
		}
		key := dbAuditCandidateKey(svc.NodeID.String(), engine, svc.ListenAddr, svc.Port)
		c := candidates[key]
		if c == nil {
			node := nodeByID[svc.NodeID]
			c = &dbAuditCandidate{
				ID:                 key,
				NodeID:             svc.NodeID.String(),
				Hostname:           node.Hostname,
				Engine:             engine,
				Host:               svc.ListenAddr,
				Port:               svc.Port,
				Process:            svc.Process,
				ServiceKind:        svc.ServiceKind,
				Sources:            []string{"node_service"},
				CaptureDBQueries:   resp.CapturePolicy.CaptureDBQueries,
				DBQueryTextCapture: resp.CapturePolicy.DBQueryTextCapture,
				CitationIDs:        []string{},
			}
			candidates[key] = c
			resp.Summary.ServicesDetected++
		}
		citation := dbAuditDiscoveryCitation{
			ID:             citationID("node_services", svc.ID.String()),
			Kind:           "db_service",
			Table:          "node_services",
			SourceRecordID: "node_services:" + svc.ID.String(),
			NodeID:         svc.NodeID.String(),
		}
		resp.Citations = append(resp.Citations, citation)
		c.CitationIDs = appendUniqueString(c.CitationIDs, citation.ID)
		c.SideChannelEvidence = append(c.SideChannelEvidence, dbAuditSideChannelEvidence{
			Kind:       "node_service",
			CitationID: citation.ID,
			Detail:     "database listener/process discovered from node service inventory",
		})
	}

	for _, agg := range eventAgg {
		nodeIDString := agg.NodeID
		if q.NodeID != uuid.Nil && nodeIDString != q.NodeID.String() {
			continue
		}
		engine := strings.TrimSpace(agg.Engine)
		if engine == "" {
			engine = "unknown"
		}
		key := dbAuditCandidateKey(nodeIDString, engine, "", 0)
		var c *dbAuditCandidate
		for _, existing := range candidates {
			if existing.NodeID != nodeIDString {
				continue
			}
			if strings.EqualFold(existing.Engine, engine) {
				c = existing
				break
			}
			if engine == "unknown" && c == nil {
				c = existing
			}
		}
		if c == nil {
			nodeID, _ := uuid.Parse(nodeIDString)
			node := nodeByID[nodeID]
			c = &dbAuditCandidate{
				ID:                 key,
				NodeID:             nodeIDString,
				Hostname:           node.Hostname,
				Engine:             engine,
				Sources:            []string{"db_query_event"},
				CaptureDBQueries:   resp.CapturePolicy.CaptureDBQueries,
				DBQueryTextCapture: resp.CapturePolicy.DBQueryTextCapture,
			}
			candidates[key] = c
		} else {
			c.Sources = appendUniqueString(c.Sources, "db_query_event")
		}
		c.RecentQueryEvents += agg.QueryCount
		c.RecentLongRunning += agg.LongRunningCount
		if !agg.LastSeen.IsZero() {
			c.LastObservedEventAt = agg.LastSeen.UTC().Format(time.RFC3339)
		}
		if agg.CitationID != "" {
			c.CitationIDs = appendUniqueString(c.CitationIDs, agg.CitationID)
			c.SideChannelEvidence = append(c.SideChannelEvidence, dbAuditSideChannelEvidence{
				Kind:       "db_query_event",
				CitationID: agg.CitationID,
				Detail:     "database query telemetry observed in normalized event storage",
			})
		}
	}

	resp.Candidates = make([]dbAuditCandidate, 0, len(candidates))
	for _, c := range candidates {
		finalizeDBAuditCandidate(c)
		resp.Summary.QueryEventsSeen += c.RecentQueryEvents
		resp.Summary.LongRunningSeen += c.RecentLongRunning
		if c.AccessState == "missing_access" {
			resp.Summary.MissingAccessTargets++
		}
		resp.Candidates = append(resp.Candidates, *c)
	}
	sortDBAuditCandidates(resp.Candidates)
	if len(resp.Candidates) > q.Limit {
		resp.Candidates = resp.Candidates[:q.Limit]
		resp.Guardrails = append(resp.Guardrails, "candidate_limit_applied")
	}
	resp.Summary.Candidates = len(resp.Candidates)
	return resp, nil
}

func writeTenantScopedNodeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "outside requested tenant"):
		http.Error(w, msg, http.StatusForbidden)
		return true
	case strings.Contains(msg, "node not found"):
		http.Error(w, msg, http.StatusNotFound)
		return true
	default:
		return false
	}
}

func (s *Server) dbAuditEventAggregates(ctx context.Context, q dbAuditDiscoveryQuery) (map[string]dbAuditEventAggregate, []dbAuditDiscoveryCitation, []string) {
	out := map[string]dbAuditEventAggregate{}
	var citations []dbAuditDiscoveryCitation
	var guardrails []string
	if s == nil || s.dorisClient == nil {
		return out, citations, []string{"doris_unavailable"}
	}
	nodeID := ""
	if q.NodeID != uuid.Nil {
		nodeID = q.NodeID.String()
	}
	rows, total, err := s.dorisClient.QueryEvents(ctx, doris.EventQueryParams{
		TenantID:   q.TenantID.String(),
		NodeID:     nodeID,
		EventTypes: []string{"db.query", "db.query.long_running"},
		Since:      q.Since,
		Until:      q.Until,
		Limit:      maxListLimit,
	})
	if err != nil {
		return out, citations, []string{"db_event_query_unavailable"}
	}
	if total > len(rows) {
		guardrails = append(guardrails, "db_event_counts_limited")
	}
	out, citations = dbAuditAggregateEventRows(rows)
	return out, citations, guardrails
}

func dbAuditAggregateEventRows(rows []doris.EventRow) (map[string]dbAuditEventAggregate, []dbAuditDiscoveryCitation) {
	out := map[string]dbAuditEventAggregate{}
	var citations []dbAuditDiscoveryCitation
	for _, row := range rows {
		if strings.TrimSpace(row.NodeID) == "" {
			continue
		}
		engine := inferDBAuditEngineFromEvent(row)
		key := dbAuditEventAggregateKey(row.NodeID, engine)
		agg := out[key]
		agg.NodeID = row.NodeID
		agg.Engine = engine
		switch row.EventType {
		case "db.query.long_running":
			agg.LongRunningCount++
		default:
			agg.QueryCount++
		}
		if row.TS.After(agg.LastSeen) {
			agg.LastSeen = row.TS
		}
		if agg.CitationID == "" {
			item, citation := eventRowToResponse(row)
			agg.CitationID = citation.ID
			citations = append(citations, dbAuditDiscoveryCitation{
				ID:             citation.ID,
				Kind:           "db_query_event",
				Table:          citation.Table,
				SourceRecordID: item.SourceRecordID,
				NodeID:         row.NodeID,
			})
		}
		out[key] = agg
	}
	return out, citations
}

func dbAuditEventAggregateKey(nodeID, engine string) string {
	engine = strings.TrimSpace(engine)
	if engine == "" {
		engine = "unknown"
	}
	return strings.TrimSpace(nodeID) + "|" + engine
}

func detectDBServiceEngine(svc storage.NodeService) string {
	text := strings.ToLower(strings.Join([]string{svc.ServiceKind, svc.Process, svc.BinaryPath}, " "))
	switch {
	case strings.Contains(text, "postgres") || svc.Port == 5432:
		return "postgres"
	case strings.Contains(text, "mysql") || strings.Contains(text, "mariadb") || strings.Contains(text, "mysqld") || svc.Port == 3306:
		return "mysql"
	case strings.Contains(text, "mongo") || strings.Contains(text, "mongod") || svc.Port == 27017:
		return "mongodb"
	case strings.Contains(text, "mssql") || strings.Contains(text, "sqlservr") || strings.Contains(text, "sql server") || svc.Port == 1433:
		return "mssql"
	case strings.Contains(text, "oracle") || strings.Contains(text, "tnslsnr") || svc.Port == 1521 || svc.Port == 1522:
		return "oracle"
	case strings.Contains(text, "db2") || strings.Contains(text, "db2sysc") || svc.Port == 50000:
		return "db2"
	default:
		return ""
	}
}

func inferDBAuditEngineFromEvent(row doris.EventRow) string {
	if engine := inferDBAuditEngineFromDetails(row.DetailsJSON); engine != "" {
		return engine
	}
	return detectDBAuditEngineFromText(strings.Join([]string{
		row.ProcessName,
		row.Message,
		row.DetailsJSON,
	}, " "))
}

func inferDBAuditEngineFromDetails(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return ""
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		return ""
	}
	for _, key := range []string{"engine", "db_engine", "database_engine", "driver", "dbms"} {
		if value, ok := details[key]; ok {
			if engine := normalizeDBAuditEngineName(strings.TrimSpace(strings.ToLower(fmt.Sprint(value)))); engine != "" {
				return engine
			}
		}
	}
	return ""
}

func detectDBAuditEngineFromText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	switch {
	case strings.Contains(text, "postgres") || strings.Contains(text, "postgresql") || strings.Contains(text, "pg_stat"):
		return "postgres"
	case strings.Contains(text, "mysql") || strings.Contains(text, "mariadb") || strings.Contains(text, "mysqld"):
		return "mysql"
	case strings.Contains(text, "mongodb") || strings.Contains(text, "mongod") || strings.Contains(text, "mongo "):
		return "mongodb"
	case strings.Contains(text, "sqlservr") || strings.Contains(text, "sql server") || strings.Contains(text, "mssql"):
		return "mssql"
	case strings.Contains(text, "oracle") || strings.Contains(text, "tnslsnr"):
		return "oracle"
	case strings.Contains(text, "db2sysc") || strings.Contains(text, " db2 ") || strings.HasPrefix(text, "db2 "):
		return "db2"
	default:
		return ""
	}
}

func normalizeDBAuditEngineName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "postgres" || value == "postgresql" || value == "pg":
		return "postgres"
	case value == "mysql" || value == "mariadb" || value == "aurora-mysql":
		return "mysql"
	case value == "mongodb" || value == "mongo":
		return "mongodb"
	case value == "mssql" || value == "sqlserver" || value == "sql_server" || value == "sql server":
		return "mssql"
	case value == "oracle":
		return "oracle"
	case value == "db2" || value == "ibm_db2":
		return "db2"
	default:
		return ""
	}
}

func finalizeDBAuditCandidate(c *dbAuditCandidate) {
	c.Sources = sortedStrings(c.Sources)
	c.CitationIDs = sortedStrings(c.CitationIDs)
	c.SideChannelEvidence = uniqueDBAuditSideChannelEvidence(c.SideChannelEvidence)
	status := []string{"candidate_detected"}
	c.AccessRequest = dbAuditAccessRequestForEngine(c.Engine)
	if c.CaptureDBQueries {
		status = append(status, "query_capture_enabled")
	} else {
		status = append(status, "query_capture_disabled")
	}
	if c.DBQueryTextCapture {
		status = append(status, "query_text_capture_enabled")
	}
	if c.RecentQueryEvents+c.RecentLongRunning > 0 {
		status = append(status, "query_events_seen")
		c.AccessState = "event_seen"
		c.CoverageState = "normalized"
	} else if c.CaptureDBQueries {
		status = append(status, "target_config_missing")
		c.AccessState = "missing_access"
		c.CoverageState = "discoverable_no_access"
		c.MissingAccessRationale = "database service detected but no db.query events were observed in the selected window"
	} else {
		c.AccessState = "capture_disabled"
		c.CoverageState = "raw_only"
		c.MissingAccessRationale = "database service detected while tenant DB query capture is disabled"
	}
	if c.Engine == "" || c.Engine == "unknown" || c.AccessRequest == nil {
		c.CoverageState = "unsupported"
	}
	c.SourceCoverage = dbAuditSourceCoverageForCandidate(c)
	c.CoverageStatus = sortedStrings(status)
}

func uniqueDBAuditSideChannelEvidence(in []dbAuditSideChannelEvidence) []dbAuditSideChannelEvidence {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]dbAuditSideChannelEvidence, 0, len(in))
	for _, item := range in {
		key := item.Kind + "|" + item.CitationID + "|" + item.Detail
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func dbAuditSourceCoverageForCandidate(c *dbAuditCandidate) []dbAuditSourceCoverage {
	specs := dbAuditSourceCoverageSpecs(c.Engine)
	if len(specs) == 0 {
		return nil
	}
	out := make([]dbAuditSourceCoverage, 0, len(specs))
	for _, spec := range specs {
		state := c.CoverageState
		if c.CoverageState == "normalized" && !spec.QueryTelemetry {
			state = "discoverable_no_access"
		}
		out = append(out, dbAuditSourceCoverage{
			Engine:             strings.ToLower(strings.TrimSpace(c.Engine)),
			Source:             spec.Source,
			State:              state,
			RequiredPrivileges: append([]string(nil), spec.RequiredPrivileges...),
			ExpectedEvidence:   append([]string(nil), spec.ExpectedEvidence...),
		})
	}
	return out
}

type dbAuditSourceCoverageSpec struct {
	Source             string
	QueryTelemetry     bool
	RequiredPrivileges []string
	ExpectedEvidence   []string
}

func dbAuditSourceCoverageSpecs(engine string) []dbAuditSourceCoverageSpec {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "postgres":
		return []dbAuditSourceCoverageSpec{
			{Source: "pg_stat_activity", QueryTelemetry: true, RequiredPrivileges: []string{"pg_monitor"}, ExpectedEvidence: []string{"active sessions", "query state", "wait events"}},
			{Source: "pg_stat_statements", QueryTelemetry: true, RequiredPrivileges: []string{"pg_monitor", "SELECT on pg_stat_statements"}, ExpectedEvidence: []string{"query hash", "calls", "rows", "total time"}},
			{Source: "pgaudit/postgresql logs", RequiredPrivileges: []string{"read access to exported PostgreSQL audit logs"}, ExpectedEvidence: []string{"statement audit", "authentication failure", "DDL/DCL activity"}},
		}
	case "mysql":
		return []dbAuditSourceCoverageSpec{
			{Source: "performance_schema", QueryTelemetry: true, RequiredPrivileges: []string{"PROCESS", "SELECT on performance_schema.*"}, ExpectedEvidence: []string{"statement summary", "current processlist", "latency counters"}},
			{Source: "audit/general/slow logs", RequiredPrivileges: []string{"read access to exported MySQL or MariaDB audit/general/slow logs"}, ExpectedEvidence: []string{"connection events", "query text or digest", "slow query evidence"}},
		}
	case "mssql":
		return []dbAuditSourceCoverageSpec{
			{Source: "DMVs", QueryTelemetry: true, RequiredPrivileges: []string{"VIEW SERVER STATE"}, ExpectedEvidence: []string{"active requests", "query text", "wait state"}},
			{Source: "SQL Server Audit", RequiredPrivileges: []string{"VIEW SERVER SECURITY AUDIT", "read access to audit target"}, ExpectedEvidence: []string{"server/database audit action", "principal", "object"}},
			{Source: "Extended Events/error logs", RequiredPrivileges: []string{"VIEW SERVER STATE", "read access to exported logs"}, ExpectedEvidence: []string{"session events", "login failures", "agent/error log entries"}},
		}
	case "oracle":
		return []dbAuditSourceCoverageSpec{
			{Source: "session/process views", QueryTelemetry: true, RequiredPrivileges: []string{"CREATE SESSION", "SELECT on GV_$SESSION/GV_$SQL where permitted"}, ExpectedEvidence: []string{"active sessions", "SQL identifiers", "wait events"}},
			{Source: "Unified Audit/FGA", RequiredPrivileges: []string{"SELECT on UNIFIED_AUDIT_TRAIL", "SELECT on DBA_FGA_AUDIT_TRAIL where permitted"}, ExpectedEvidence: []string{"audit action", "database user", "object/schema", "policy name"}},
			{Source: "listener/alert logs", RequiredPrivileges: []string{"read access to exported Oracle listener and alert logs"}, ExpectedEvidence: []string{"connection attempts", "errors", "startup/shutdown events"}},
		}
	case "db2":
		return []dbAuditSourceCoverageSpec{
			{Source: "activity/event monitors", QueryTelemetry: true, RequiredPrivileges: []string{"read access to DB2 activity/event monitor views"}, ExpectedEvidence: []string{"activity statement", "application handle", "rows read/written"}},
			{Source: "audit facility extracts", RequiredPrivileges: []string{"read access to DB2 audit facility extracts"}, ExpectedEvidence: []string{"audit category", "auth ID", "object/action"}},
			{Source: "db2diag.log", RequiredPrivileges: []string{"read access to exported db2diag.log"}, ExpectedEvidence: []string{"diagnostic errors", "auth failures", "service events"}},
		}
	case "mongodb":
		return []dbAuditSourceCoverageSpec{
			{Source: "currentOp/profiler", QueryTelemetry: true, RequiredPrivileges: []string{"clusterMonitor", "read access to system.profile where enabled"}, ExpectedEvidence: []string{"operation", "namespace", "duration", "plan summary"}},
			{Source: "audit logs", RequiredPrivileges: []string{"read access to exported MongoDB audit logs"}, ExpectedEvidence: []string{"authCheck", "authenticate", "create/drop/modify collection", "role changes"}},
		}
	default:
		return nil
	}
}

func dbAuditAccessRequestForEngine(engine string) *dbAuditAccessRequestArtifact {
	engine = strings.ToLower(strings.TrimSpace(engine))
	switch engine {
	case "postgres":
		return &dbAuditAccessRequestArtifact{
			Engine:             "postgres",
			LeastPrivilegeRole: "controlone_audit_reader",
			GrantStatements: []string{
				"CREATE ROLE controlone_audit_reader LOGIN PASSWORD '<managed-secret>';",
				"GRANT pg_monitor TO controlone_audit_reader;",
				"GRANT SELECT ON pg_stat_statements TO controlone_audit_reader;",
			},
			SetupNotes: []string{
				"Enable pg_stat_statements where query digest coverage is required.",
				"Configure pgaudit/log_line_prefix and grant read access to exported PostgreSQL audit logs where database-level views are insufficient.",
			},
			ExpectedEvidence: []string{"pg_stat_activity snapshots", "pg_stat_statements query hashes", "pgaudit or PostgreSQL log entries", "authentication failure events"},
			RiskNote:         "Read-only monitoring role; do not grant table data access beyond PostgreSQL statistics/audit views.",
		}
	case "mysql":
		return &dbAuditAccessRequestArtifact{
			Engine:             "mysql",
			LeastPrivilegeRole: "controlone_audit_reader",
			GrantStatements: []string{
				"CREATE USER 'controlone_audit_reader'@'%' IDENTIFIED BY '<managed-secret>';",
				"GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'controlone_audit_reader'@'%';",
				"GRANT SELECT ON performance_schema.* TO 'controlone_audit_reader'@'%';",
			},
			SetupNotes: []string{
				"Grant filesystem or object-store read access to MySQL Enterprise/MariaDB audit, general, and slow-query log exports when enabled.",
			},
			ExpectedEvidence: []string{"performance_schema statement summaries", "processlist snapshots", "audit plugin records", "general/slow query log entries", "authentication failure events"},
			RiskNote:         "Avoid broad SELECT on application schemas; performance_schema and exported audit logs are sufficient for discovery.",
		}
	case "mssql":
		return &dbAuditAccessRequestArtifact{
			Engine:             "mssql",
			LeastPrivilegeRole: "controlone_audit_reader",
			GrantStatements: []string{
				"CREATE LOGIN controlone_audit_reader WITH PASSWORD = '<managed-secret>';",
				"GRANT VIEW SERVER STATE TO controlone_audit_reader;",
				"GRANT VIEW SERVER SECURITY AUDIT TO controlone_audit_reader;",
			},
			SetupNotes: []string{
				"Grant read access to SQL Server Audit, Extended Events, SQL Agent/error logs, and default trace exports where configured.",
			},
			ExpectedEvidence: []string{"sys.dm_exec_requests snapshots", "SQL Server Audit records", "Extended Events sessions", "SQL Agent/error log entries", "login failure events"},
			RiskNote:         "Monitoring grants expose metadata and query text; restrict to audit servers and rotate credentials.",
		}
	case "oracle":
		return &dbAuditAccessRequestArtifact{
			Engine:             "oracle",
			LeastPrivilegeRole: "CONTROLONE_AUDIT_READER",
			GrantStatements: []string{
				"CREATE USER CONTROLONE_AUDIT_READER IDENTIFIED BY '<managed-secret>';",
				"GRANT CREATE SESSION TO CONTROLONE_AUDIT_READER;",
				"GRANT SELECT ON SYS.UNIFIED_AUDIT_TRAIL TO CONTROLONE_AUDIT_READER;",
				"GRANT SELECT ON SYS.DBA_FGA_AUDIT_TRAIL TO CONTROLONE_AUDIT_READER;",
				"GRANT SELECT ON SYS.GV_$SESSION TO CONTROLONE_AUDIT_READER;",
				"GRANT SELECT ON SYS.GV_$SQL TO CONTROLONE_AUDIT_READER;",
			},
			SetupNotes: []string{
				"Provide exported listener and alert logs when catalog audit views are restricted.",
			},
			ExpectedEvidence: []string{"Unified Audit records", "Fine-Grained Auditing records", "listener log entries", "alert log entries", "session/process snapshots"},
			RiskNote:         "Prefer scoped audit views or exported logs over broad catalog grants in highly regulated databases.",
		}
	case "db2":
		return &dbAuditAccessRequestArtifact{
			Engine:             "db2",
			LeastPrivilegeRole: "CONTROLONE_AUDIT_READER",
			GrantStatements: []string{
				"GRANT SELECT ON TABLE SYSIBMADM.MON_CURRENT_SQL TO USER CONTROLONE_AUDIT_READER;",
				"GRANT SELECT ON TABLE SYSIBMADM.MON_CONNECTION_SUMMARY TO USER CONTROLONE_AUDIT_READER;",
			},
			SetupNotes: []string{
				"Create CONTROLONE_AUDIT_READER through the platform identity mechanism used by this DB2 deployment.",
				"Grant read access to DB2 audit facility extracts and db2diag.log exports.",
			},
			ExpectedEvidence: []string{"DB2 audit facility extracts", "db2diag.log entries", "activity/event monitor rows", "authentication failure events"},
			RiskNote:         "DB2 grant syntax varies by platform; prefer exported audit facility files with read-only filesystem access.",
		}
	case "mongodb":
		return &dbAuditAccessRequestArtifact{
			Engine:             "mongodb",
			LeastPrivilegeRole: "controlone_audit_reader",
			GrantStatements: []string{
				"db.createUser({user: 'controlone_audit_reader', pwd: '<managed-secret>', roles: [{role: 'clusterMonitor', db: 'admin'}]});",
			},
			SetupNotes: []string{
				"Enable MongoDB audit logging and grant read access to exported audit/profiler records where available.",
				"Use profiler level and audit filters approved by the DBA/security owner; avoid collection data reads.",
			},
			ExpectedEvidence: []string{"MongoDB audit log records", "system.profile entries", "currentOp snapshots", "authentication failure events"},
			RiskNote:         "clusterMonitor exposes operational metadata; do not grant readWrite/readAnyDatabase for audit discovery.",
		}
	default:
		return nil
	}
}

func sortDBAuditCandidates(candidates []dbAuditCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Hostname != candidates[j].Hostname {
			return candidates[i].Hostname < candidates[j].Hostname
		}
		if candidates[i].Engine != candidates[j].Engine {
			return candidates[i].Engine < candidates[j].Engine
		}
		return candidates[i].Port < candidates[j].Port
	})
}

func dbAuditCandidateKey(nodeID, engine, host string, port int) string {
	parts := []string{strings.TrimSpace(nodeID), strings.TrimSpace(engine), strings.TrimSpace(host), strconv.Itoa(port)}
	return strings.Join(parts, "|")
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func parseOptionalInt(raw string, defaultValue int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, errors.New("must be non-negative")
	}
	return value, nil
}

func parseOptionalUUID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(raw)
}
