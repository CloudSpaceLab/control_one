package server

import (
	"net/http"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

// adminSelfHealthResponse summarises the running control-plane's own health.
//
// TODO: api_p95_ms / nats_lag_ms / db_p95_ms / queue_depth currently come from
// runtime stubs. Wire these to the prometheus registry, NATS lag probe, and
// queue length once those probes are exposed in-process.
type adminSelfHealthResponse struct {
	APIP95Ms     float64 `json:"api_p95_ms"`
	NATSLagMs    float64 `json:"nats_lag_ms"`
	DBP95Ms      float64 `json:"db_p95_ms"`
	QueueDepth   int     `json:"queue_depth"`
	Status       string  `json:"status"` // "ok" | "degraded" | "down"
	GoroutineNum int     `json:"goroutine_num"`
	HeapMB       uint64  `json:"heap_mb"`
	GeneratedAt  string  `json:"generated_at"`
}

func (s *Server) handleAdminSelfHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	resp := adminSelfHealthResponse{
		// TODO: replace stubs with prometheus histogram p95s + NATS lag probe.
		APIP95Ms:     0,
		NATSLagMs:    0,
		DBP95Ms:      0,
		QueueDepth:   0,
		Status:       "ok",
		GoroutineNum: runtime.NumGoroutine(),
		HeapMB:       mem.HeapAlloc / (1024 * 1024),
		GeneratedAt:  formatTime(time.Now().UTC()),
	}

	if s.store == nil {
		resp.Status = "degraded"
	}
	writeJSON(w, http.StatusOK, resp)
}

// ingestThroughputPoint is a single bucket on the throughput series.
type ingestThroughputPoint struct {
	Timestamp    string  `json:"ts"`
	EventsPerSec float64 `json:"events_per_sec"`
	BytesPerSec  float64 `json:"bytes_per_sec"`
}

type ingestThroughputTotals struct {
	Events int64 `json:"events"`
	Bytes  int64 `json:"bytes"`
}

type ingestThroughputResponse struct {
	Stream   string                  `json:"stream,omitempty"`
	Interval string                  `json:"interval,omitempty"`
	Series   []ingestThroughputPoint `json:"series"`
	Totals   ingestThroughputTotals  `json:"totals"`
}

func (s *Server) handleAdminIngestThroughput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	stream := strings.TrimSpace(r.URL.Query().Get("stream"))
	interval := strings.TrimSpace(r.URL.Query().Get("interval"))
	if interval == "" {
		interval = "1m"
	}

	// TODO: hook this up to the Doris ingest journal / hourly_rollup tables
	// once stream throughput aggregates are wired through. For now expose a
	// non-error empty series so the UI can render the panel.
	resp := ingestThroughputResponse{
		Stream:   stream,
		Interval: interval,
		Series:   []ingestThroughputPoint{},
		Totals:   ingestThroughputTotals{},
	}
	writeJSON(w, http.StatusOK, resp)
}

type tenantActivityRow struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Events24h   int64  `json:"events_24h"`
	Nodes       int    `json:"nodes"`
	UsersActive int    `json:"users_active"`
	LastSeen    string `json:"last_seen,omitempty"`
}

type tenantsActivityResponse struct {
	Period      string              `json:"period"`
	ActiveCount int                 `json:"active_count"`
	TotalCount  int                 `json:"total_count"`
	Top         []tenantActivityRow `json:"top"`
}

func (s *Server) handleAdminTenantsActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = "24h"
	}

	tenants, total, err := s.store.ListTenants(r.Context(), "", maxListLimit, 0)
	if err != nil {
		s.logger.Warn("tenants activity list", zap.Error(err))
		writeJSON(w, http.StatusOK, tenantsActivityResponse{
			Period: period,
			Top:    []tenantActivityRow{},
		})
		return
	}

	since := time.Now().UTC().Add(-24 * time.Hour)
	rows := make([]tenantActivityRow, 0, len(tenants))
	activeCount := 0
	for _, t := range tenants {
		row := tenantActivityRow{
			TenantID: t.ID.String(),
			Name:     t.Name,
		}

		// Node count + last_seen approximation via the existing node list.
		nodes, totalNodes, listErr := s.store.ListNodes(r.Context(), t.ID, "", maxListLimit, 0)
		if listErr == nil {
			row.Nodes = totalNodes
			var latest *time.Time
			for _, n := range nodes {
				if n.LastSeenAt == nil {
					continue
				}
				if latest == nil || n.LastSeenAt.After(*latest) {
					seen := *n.LastSeenAt
					latest = &seen
				}
			}
			if latest != nil {
				row.LastSeen = formatTime(*latest)
				if latest.After(since) {
					activeCount++
				}
			}
		}

		// TODO: events_24h should come from the Doris ingest hourly rollup
		// once tenant-scoped aggregates are queryable. Leaving as zero so the
		// UI can still render rows without fabricated counts.
		row.Events24h = 0
		// TODO: users_active needs a session-table lookup once that view is
		// wired through.
		row.UsersActive = 0

		rows = append(rows, row)
	}

	resp := tenantsActivityResponse{
		Period:      period,
		ActiveCount: activeCount,
		TotalCount:  total,
		Top:         rows,
	}
	writeJSON(w, http.StatusOK, resp)
}

type sloEntry struct {
	Name     string  `json:"name"`
	Target   float64 `json:"target"`
	Actual   float64 `json:"actual"`
	BurnRate float64 `json:"burn_rate"`
	Window   string  `json:"window"`
}

type sloResponse struct {
	SLOs []sloEntry `json:"slos"`
}

func (s *Server) handleAdminSLO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	// TODO: SLO computation belongs on top of the Doris-backed ingest journal
	// and prometheus error-rate metrics; for now we expose the canonical
	// definitions with zero `actual` so the UI can render the table.
	resp := sloResponse{
		SLOs: []sloEntry{
			{Name: "api_availability", Target: 0.999, Actual: 0, BurnRate: 0, Window: "30d"},
			{Name: "ingest_durability", Target: 0.9999, Actual: 0, BurnRate: 0, Window: "30d"},
			{Name: "alert_delivery_latency_p95_seconds", Target: 60, Actual: 0, BurnRate: 0, Window: "7d"},
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

type capacityResponse struct {
	DiskUsed               int64  `json:"disk_used"`
	DiskTotal              int64  `json:"disk_total"`
	DorisStatus            string `json:"doris_status"`
	PostgresStatus         string `json:"postgres_status"`
	RetentionDaysRemaining int    `json:"retention_days_remaining"`
}

func (s *Server) handleAdminCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	resp := capacityResponse{
		// TODO: disk_used / disk_total should come from a node_exporter scrape
		// or local fs probe; placeholder zero values until that is wired.
		DiskUsed:               0,
		DiskTotal:              0,
		DorisStatus:            "unknown", // TODO: wire to Doris frontend ping.
		PostgresStatus:         "ok",
		RetentionDaysRemaining: 0, // TODO: derive from active retention policies.
	}
	if s.store == nil {
		resp.PostgresStatus = "down"
	}
	writeJSON(w, http.StatusOK, resp)
}
