package server

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

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

// pinger is satisfied by storage.Store, used via type assertion to avoid
// adding Ping to the main Store interface.
type pinger interface {
	Ping(context.Context) error
}

// logThroughputProvider is satisfied by storage.Store; used via type
// assertion so the main Store interface stays narrow and existing test
// fakes don't all need to implement it.
type logThroughputProvider interface {
	LogThroughputSeries(context.Context, uuid.UUID, time.Time, time.Time, time.Duration) ([]storage.LogThroughputBucket, error)
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

	status := "ok"

	// Queue depth from worker status provider.
	var queueDepth int
	if wsp, ok := s.worker.(workerStatusProvider); ok {
		st := wsp.Status()
		queueDepth = st.QueueDepth
		if st.LastError != "" {
			status = "degraded"
		}
	}

	// DB round-trip latency as a proxy for db_p95_ms.
	var dbP95Ms float64
	if p, ok := s.store.(pinger); ok {
		start := time.Now()
		if err := p.Ping(r.Context()); err != nil {
			status = "degraded"
			s.logger.Warn("admin health db ping", zap.Error(err))
		}
		dbP95Ms = float64(time.Since(start).Milliseconds())
	} else if s.store == nil {
		status = "degraded"
	}

	writeJSON(w, http.StatusOK, adminSelfHealthResponse{
		APIP95Ms:     0, // requires prometheus middleware instrumentation
		NATSLagMs:    0, // requires NATS probe
		DBP95Ms:      dbP95Ms,
		QueueDepth:   queueDepth,
		Status:       status,
		GoroutineNum: runtime.NumGoroutine(),
		HeapMB:       mem.HeapAlloc / (1024 * 1024),
		GeneratedAt:  formatTime(time.Now().UTC()),
	})
}

// ─── Ingest Throughput ────────────────────────────────────────────────────

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
	intervalStr := strings.TrimSpace(r.URL.Query().Get("interval"))
	if intervalStr == "" {
		intervalStr = "1m"
	}

	// Parse interval to bucket duration.
	bucketDur := time.Minute
	switch intervalStr {
	case "5m":
		bucketDur = 5 * time.Minute
	case "15m":
		bucketDur = 15 * time.Minute
	case "1h":
		bucketDur = time.Hour
	}

	// Period controls the rolling window the series covers. UI sends 1h/24h/7d.
	// Default to 1h to preserve prior behaviour.
	periodStr := strings.TrimSpace(r.URL.Query().Get("period"))
	window := time.Hour
	switch periodStr {
	case "24h":
		window = 24 * time.Hour
	case "7d":
		window = 7 * 24 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
	}

	resp := ingestThroughputResponse{
		Stream:   stream,
		Interval: intervalStr,
		Series:   []ingestThroughputPoint{},
		Totals:   ingestThroughputTotals{},
	}

	now := time.Now().UTC()
	since := now.Add(-window)
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))

	type bucket struct {
		ts     time.Time
		events int64
	}
	var buckets []bucket

	// Prefer Doris when configured; fall back to PostgreSQL telemetry_logs so
	// the dashboard reflects ingest even on deployments without Doris.
	if s.dorisClient != nil {
		dorisBuckets, err := s.dorisClient.LogThroughputSeries(r.Context(), tenantID, since, now, bucketDur)
		if err != nil {
			s.logger.Warn("ingest throughput series (doris)", zap.Error(err))
		} else {
			for _, b := range dorisBuckets {
				buckets = append(buckets, bucket{ts: b.Timestamp, events: b.Events})
			}
		}
	}
	if len(buckets) == 0 && s.store != nil {
		if pg, ok := s.store.(logThroughputProvider); ok {
			var tenantUUID uuid.UUID
			if tenantID != "" {
				if id, err := uuid.Parse(tenantID); err == nil {
					tenantUUID = id
				}
			}
			pgBuckets, err := pg.LogThroughputSeries(r.Context(), tenantUUID, since, now, bucketDur)
			if err != nil {
				s.logger.Warn("ingest throughput series (postgres)", zap.Error(err))
			} else {
				for _, b := range pgBuckets {
					buckets = append(buckets, bucket{ts: b.Timestamp, events: b.Events})
				}
			}
		}
	}

	bucketSec := bucketDur.Seconds()
	for _, b := range buckets {
		eps := float64(b.events) / bucketSec
		resp.Series = append(resp.Series, ingestThroughputPoint{
			Timestamp:    formatTime(b.ts),
			EventsPerSec: eps,
			BytesPerSec:  0, // byte tracking not in telemetry_logs schema
		})
		resp.Totals.Events += b.events
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── Tenants Activity ─────────────────────────────────────────────────────

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
		writeJSON(w, http.StatusOK, tenantsActivityResponse{Period: period, Top: []tenantActivityRow{}})
		return
	}

	now := time.Now().UTC()
	window := 24 * time.Hour
	switch period {
	case "1h":
		window = time.Hour
	case "7d":
		window = 7 * 24 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
	}
	since := now.Add(-window)
	rows := make([]tenantActivityRow, 0, len(tenants))
	activeCount := 0

	for _, t := range tenants {
		row := tenantActivityRow{TenantID: t.ID.String(), Name: t.Name}

		// Node count + last_seen from existing node list.
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

		// Events24h from Doris security_events table.
		if s.dorisClient != nil {
			ec, dorisErr := s.dorisClient.CountSecurityEvents(r.Context(), t.ID.String(), since, now)
			if dorisErr == nil {
				row.Events24h = ec.Total
			}
		}

		// UsersActive: count nodes that checked in during the period as a
		// proxy until a dedicated session table view is available.
		row.UsersActive = 0
		for _, n := range nodes {
			if n.LastSeenAt != nil && n.LastSeenAt.After(since) {
				row.UsersActive++
			}
		}

		rows = append(rows, row)
	}

	writeJSON(w, http.StatusOK, tenantsActivityResponse{
		Period:      period,
		ActiveCount: activeCount,
		TotalCount:  total,
		Top:         rows,
	})
}

// ─── SLO ─────────────────────────────────────────────────────────────────

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

	// api_availability: if the server is responding, this request window = 100%.
	// A real SLO needs error-rate tracking from a prometheus counter.
	apiAvail := 1.0
	if s.store == nil {
		apiAvail = 0.5 // degraded
	}

	// ingest_durability: ratio of security events written vs expected, proxied
	// by Doris event count over 30 days vs a naive baseline. Without baseline
	// data we signal 1.0 when Doris is healthy, 0.0 when unavailable.
	ingestDurability := 0.0
	if s.dorisClient != nil {
		if err := s.dorisClient.Ping(r.Context()); err == nil {
			ingestDurability = 1.0
		}
	}

	slos := []sloEntry{
		{Name: "api_availability", Target: 0.999, Actual: apiAvail, BurnRate: 1 - apiAvail, Window: "30d"},
		{Name: "ingest_durability", Target: 0.9999, Actual: ingestDurability, BurnRate: 0, Window: "30d"},
		{Name: "alert_delivery_latency_p95_seconds", Target: 60, Actual: 0, BurnRate: 0, Window: "7d"},
	}
	writeJSON(w, http.StatusOK, sloResponse{SLOs: slos})
}

// ─── Capacity ────────────────────────────────────────────────────────────

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

	diskUsed, diskTotal := diskUsage("/var/lib/control-one")

	dorisStatus := "unconfigured"
	if s.dorisClient != nil {
		if err := s.dorisClient.Ping(r.Context()); err != nil {
			dorisStatus = "degraded"
			s.logger.Warn("admin capacity doris ping", zap.Error(err))
		} else {
			dorisStatus = "ok"
		}
	}

	pgStatus := "ok"
	if s.store == nil {
		pgStatus = "down"
	} else if p, ok := s.store.(pinger); ok {
		if err := p.Ping(r.Context()); err != nil {
			pgStatus = "degraded"
		}
	}

	// Minimum configured retention across all policies — represents how
	// far back guaranteed data availability extends.
	retentionDays := 0
	if s.store != nil {
		policies, _, err := s.store.ListRetentionPolicies(r.Context(), uuid.Nil, 100, 0)
		if err == nil && len(policies) > 0 {
			min := policies[0].RetentionDays
			for _, p := range policies[1:] {
				if p.RetentionDays < min {
					min = p.RetentionDays
				}
			}
			retentionDays = min
		}
	}

	writeJSON(w, http.StatusOK, capacityResponse{
		DiskUsed:               diskUsed,
		DiskTotal:              diskTotal,
		DorisStatus:            dorisStatus,
		PostgresStatus:         pgStatus,
		RetentionDaysRemaining: retentionDays,
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────
