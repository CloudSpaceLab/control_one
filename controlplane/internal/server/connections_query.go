package server

import (
	"encoding/json"
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
//	GET /api/v1/connections?tenant_id=...&ip=...&since=...&until=...&limit=...
//
// Backed by the Doris `process_connections` table. When Doris is not
// configured the endpoint returns 503 — the UI degrades to its
// "fast view" sourced from event_rollups_hourly.
func (s *Server) handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.dorisClient == nil {
		http.Error(w, "analytic store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip == "" {
		http.Error(w, "ip is required", http.StatusBadRequest)
		return
	}
	since, until := parseTimeWindow(r, 24*time.Hour)
	limit := parseLimitDefault(r, 100, 1000)

	rows, err := s.dorisClient.ListConnectionsForIP(r.Context(), tenantID.String(), ip, since, until, limit)
	if err != nil {
		s.logger.Warn("doris list connections", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
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
	if s.dorisClient == nil {
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
	if s.dorisClient == nil {
		http.Error(w, "analytic store unavailable", http.StatusServiceUnavailable)
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
	if s.dorisClient != nil {
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
		count    int64
		bytesIn  int64
		bytesOut int64
		sevMax   string
		nodeID   string
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
		a.count += r.Count
		a.bytesIn += r.BytesIn
		a.bytesOut += r.BytesOut
		if r.SevMax.Valid {
			a.sevMax = r.SevMax.String
		}
	}
	var fallback []doris.FleetSnapshotRow
	for _, a := range byNode {
		fallback = append(fallback, doris.FleetSnapshotRow{
			NodeID:        a.nodeID,
			ConnsActive:   a.count,
			BytesIn24h:    a.bytesIn,
			BytesOut24h:   a.bytesOut,
			ThreatHits24h: 0,
			SeverityMax:   a.sevMax,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": fallback, "source": "postgres-fallback"})
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

// uuidPtr is a tiny helper to read query-string UUIDs without panicking on
// bad input. Returns the zero UUID on parse error.
func uuidPtr(s string) uuid.UUID {
	u, _ := uuid.Parse(s)
	return u
}

// jsonOptional avoids exporting "" instead of null in struct literals.
func jsonOptional(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var _ = json.NewEncoder // ensure encoding/json stays imported
