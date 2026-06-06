package server

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
)

// handleConnectionsList serves
//
//	GET /api/v1/connections?tenant_id=...&ip=...&node_id=...&open_only=true&since=...&until=...&limit=...
//
// Backed by the Doris `process_connections` table. In small analytics mode
// the raw connection row store is intentionally absent, so the endpoint
// returns an empty, successful envelope and lets fleet rollups remain fast.
func (s *Server) handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.usesDorisAnalytics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":       []doris.ConnectionRow{},
			"source":     "small-analytics-pending",
			"guardrails": []string{"raw connection rows require the Redis+SQLite small analytics store or OLAP mode"},
		})
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	externalOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("external_only")), "true")
	since, until := parseTimeWindow(r, 24*time.Hour)
	limit := parseLimitDefault(r, 100, 1000)

	var rows []doris.ConnectionRow
	if nodeID != "" {
		if _, err := uuid.Parse(nodeID); err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		openOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("open_only")), "true")
		rows, err = s.dorisClient.ListConnectionsForNode(r.Context(), tenantID.String(), nodeID, since, until, limit, openOnly, externalOnly)
	} else if ip != "" {
		rows, err = s.dorisClient.ListConnectionsForIP(r.Context(), tenantID.String(), ip, since, until, limit)
	} else {
		rows, err = s.dorisClient.ListConnectionsForTenant(r.Context(), tenantID.String(), since, until, limit, externalOnly)
	}
	if err != nil {
		s.logger.Warn("doris list connections", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	rows = sanitizeConnectionThreatRows(rows)
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// handleConnectionDetail returns the connection-level record + correlated
// timeline.
//
//	GET /api/v1/connections/{conn_id}?tenant_id=...
func (s *Server) handleConnectionDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if !s.usesDorisAnalytics() {
		http.Error(w, "analytic store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	connID := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")
	if connID == "" || strings.Contains(connID, "/") {
		http.Error(w, "conn_id required", http.StatusBadRequest)
		return
	}
	row, err := s.dorisClient.ConnectionLifetime(r.Context(), tenantID.String(), connID)
	if err != nil {
		s.logger.Warn("doris connection lifetime", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if row != nil {
		sanitized := sanitizeConnectionThreatRow(*row)
		row = &sanitized
	}
	resp := map[string]any{"connection": row}
	if row != nil && row.CorrelationID != "" {
		evs, err := s.dorisClient.ListEventsByCorrelation(r.Context(), tenantID.String(), row.CorrelationID)
		if err == nil {
			resp["events"] = evs
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleTopTalkers powers the dashboard "Top Talkers" card.
//
//	GET /api/v1/connections/top-talkers?tenant_id=...&since=...
func (s *Server) handleTopTalkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if !s.usesDorisAnalytics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":       []doris.TopTalker{},
			"source":     "small-analytics-pending",
			"guardrails": []string{"top talkers require the Redis+SQLite small analytics store or OLAP mode"},
		})
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	since, _ := parseTimeWindow(r, 24*time.Hour)
	limit := parseLimitDefault(r, 10, 100)
	rows, err := s.dorisClient.TopTalkers(r.Context(), tenantID.String(), since, limit)
	if err != nil {
		s.logger.Warn("doris top talkers", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// handleFleetHealth powers the dashboard topology grid.
//
//	GET /api/v1/fleet/health?tenant_id=...&since=...
func (s *Server) handleFleetHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	since, _ := parseTimeWindow(r, 24*time.Hour)
	source := "postgres-fallback"
	if effectiveAnalyticsMode(s.cfg) == analyticsModeSmall {
		source = "small-analytics-postgres"
	}
	if s.usesDorisAnalytics() {
		rows, err := s.dorisClient.FleetHealthSnapshot(r.Context(), tenantID.String(), since)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": rows, "source": "doris"})
			return
		}
		s.logger.Warn("doris fleet health degraded", zap.Error(err))
	}
	// Fallback: roll up from Postgres event_rollups_hourly.
	rollups, err := s.store.QueryHourlyRollup(r.Context(), tenantID, since, time.Now().UTC())
	if err != nil {
		s.logger.Error("postgres fleet health", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Aggregate across event_type per node for the topology view.
	type acc struct {
		connOpen  int64
		connClose int64
		bytesIn   int64
		bytesOut  int64
		sevMax    string
		nodeID    string
		lastAt    time.Time
	}
	byNode := map[string]*acc{}
	for _, r := range rollups {
		nID := ""
		if r.NodeID.Valid {
			nID = r.NodeID.UUID.String()
		}
		a, ok := byNode[nID]
		if !ok {
			a = &acc{nodeID: nID}
			byNode[nID] = a
		}
		switch strings.ToLower(strings.TrimSpace(r.EventType)) {
		case "conn.open":
			a.connOpen += r.Count
		case "conn.close":
			a.connClose += r.Count
		}
		a.bytesIn += r.BytesIn
		a.bytesOut += r.BytesOut
		if r.SevMax.Valid {
			a.sevMax = r.SevMax.String
		}
		if r.HourTS.After(a.lastAt) {
			a.lastAt = r.HourTS
		}
	}
	var fallback []doris.FleetSnapshotRow
	for _, a := range byNode {
		activeConns := a.connOpen - a.connClose
		if activeConns < 0 {
			activeConns = 0
		}
		fallback = append(fallback, doris.FleetSnapshotRow{
			NodeID:        a.nodeID,
			ConnsActive:   activeConns,
			BytesIn24h:    a.bytesIn,
			BytesOut24h:   a.bytesOut,
			ThreatHits24h: 0,
			LastEventAt:   a.lastAt,
			SeverityMax:   a.sevMax,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": fallback, "source": source})
}

func parseTimeWindow(r *http.Request, defaultSpan time.Duration) (since, until time.Time) {
	until = time.Now().UTC()
	since = until.Add(-defaultSpan)
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("until")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			until = t
		}
	}
	return since, until
}

func parseLimitDefault(r *http.Request, def, max int) int {
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > max {
				return max
			}
			return n
		}
	}
	return def
}

func sanitizeConnectionThreatRows(rows []doris.ConnectionRow) []doris.ConnectionRow {
	for i := range rows {
		rows[i] = sanitizeConnectionThreatRow(rows[i])
	}
	return rows
}

func sanitizeConnectionThreatRow(row doris.ConnectionRow) doris.ConnectionRow {
	if row.ThreatMatch && !connectionThreatPeerIsPublic(row) {
		row.ThreatMatch = false
		row.ThreatFeed = ""
	}
	return row
}

func connectionThreatPeerIsPublic(row doris.ConnectionRow) bool {
	direction := strings.ToLower(strings.TrimSpace(row.Direction))
	switch direction {
	case "inbound":
		return isPublicRoutableIP(net.ParseIP(strings.TrimSpace(row.SrcIP)))
	case "outbound":
		return isPublicRoutableIP(net.ParseIP(strings.TrimSpace(row.DstIP)))
	default:
		return isPublicRoutableIP(net.ParseIP(strings.TrimSpace(row.SrcIP))) ||
			isPublicRoutableIP(net.ParseIP(strings.TrimSpace(row.DstIP)))
	}
}
