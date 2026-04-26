package server

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// IngestedEvent is the wire shape every collector publishes. Server only
// validates the discriminator + a few required fields; everything else
// passes through to Doris / rollups / eventbus.
type IngestedEvent struct {
	Type          string         `json:"type"`
	TS            time.Time      `json:"ts"`
	NodeID        string         `json:"node_id,omitempty"`
	TenantID      string         `json:"tenant_id,omitempty"`
	Severity      string         `json:"severity,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	ConnID        string         `json:"conn_id,omitempty"`
	BastionSessID string         `json:"bastion_session_id,omitempty"`
	PID           int64          `json:"pid,omitempty"`
	ProcessName   string         `json:"process_name,omitempty"`
	UserName      string         `json:"user_name,omitempty"`
	SrcIP         string         `json:"src_ip,omitempty"`
	SrcPort       int            `json:"src_port,omitempty"`
	DstIP         string         `json:"dst_ip,omitempty"`
	DstPort       int            `json:"dst_port,omitempty"`
	Protocol      string         `json:"protocol,omitempty"`
	BytesIn       int64          `json:"bytes_in,omitempty"`
	BytesOut      int64          `json:"bytes_out,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	RuleID        string         `json:"rule_id,omitempty"`
	ThreatFeed    string         `json:"threat_feed,omitempty"`
	ThreatScore   int            `json:"threat_score,omitempty"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DedupKey      string         `json:"dedup_key,omitempty"`
}

// allowedEventTypes is the closed-world set of event_type discriminators we
// accept from agents. Add new families here when collectors land.
var allowedEventTypes = map[string]bool{
	// Network connections
	"conn.open":         true,
	"conn.close":        true,
	"conn.state_change": true,
	"conn.summary":      true,
	// Process lifecycle
	"proc.exec":   true,
	"proc.exit":   true,
	"proc.usage":  true,
	// File access
	"file.open":          true,
	"file.read.summary":  true,
	"file.write.summary": true,
	"file.unlink":        true,
	"file.rename":        true,
	// DB queries
	"db.query": true,
	// Bastion sessions
	"bastion.session.open":         true,
	"bastion.session.close":        true,
	"bastion.session.byte_summary": true,
	// Logs / detection
	"log.line":     true,
	"log.spike":    true,
	"rule.trigger": true,
	// Behavioural anomaly detectors (Phase F)
	"anomaly.new_destination":     true,
	"anomaly.long_connection":     true,
	"anomaly.high_bytes_out":      true,
	"anomaly.fast_bulk_transfer":  true,
	"anomaly.packet_scan":         true,
	"anomaly.new_executable":      true,
	"anomaly.executable_dropped":  true,
	"anomaly.new_db_query":        true,
	"anomaly.db_query_high_rows":  true,
	// Long-running DB query (Phase G)
	"db.query.long_running": true,
	// Compatibility shims for older event flavours
	"security.event":    true,
	"health.incident":   true,
	"compliance.result": true,
}

// rateLimiterRegistry keeps a token-bucket per (tenant, node). Idle entries
// expire after the cleanupInterval so a churning fleet doesn't leak memory.
type rateLimiterRegistry struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
	idleTTL  time.Duration
}

type rateLimiterEntry struct {
	limiter *rate.Limiter
	last    time.Time
}

func newRateLimiterRegistry(rps rate.Limit, burst int) *rateLimiterRegistry {
	return &rateLimiterRegistry{
		limiters: make(map[string]*rateLimiterEntry),
		rps:      rps,
		burst:    burst,
		idleTTL:  10 * time.Minute,
	}
}

func (r *rateLimiterRegistry) allow(key string, n int) (bool, time.Duration) {
	r.mu.Lock()
	now := time.Now()
	entry, ok := r.limiters[key]
	if !ok {
		entry = &rateLimiterEntry{limiter: rate.NewLimiter(r.rps, r.burst)}
		r.limiters[key] = entry
	}
	entry.last = now
	r.mu.Unlock()
	res := entry.limiter.ReserveN(now, n)
	if !res.OK() {
		return false, time.Second
	}
	delay := res.DelayFrom(now)
	if delay <= 0 {
		return true, 0
	}
	res.Cancel()
	return false, delay
}

// handleEventsIngest accepts a gzip + ndjson batch from an authenticated
// agent, journals it, then fans out to Doris + rollups + eventbus.
//
//   POST /api/v1/events/ingest
//   Content-Encoding: gzip
//   Content-Type:     application/x-ndjson
//   Body: one IngestedEvent JSON per line
//
// Auth: the request must be the "agent" principal (mTLS). Other roles get a
// 403 — humans don't post events.
//
// Behaviour:
//   - Hard caps: 1 MiB compressed body, 5 MiB decompressed, 5_000 events.
//   - Per-(tenant, node) token bucket; 429 + Retry-After on overflow.
//   - Always journals to event_ingest_batches before fan-out, so a replica
//     restart re-flushes from the journal.
//   - When dorisWriter is unhealthy or unconfigured, ingest still 200s and
//     the journal row stays 'pending_doris' for the drainer to retry.
func (s *Server) handleEventsIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil || principal.Type != "agent" {
		http.Error(w, "agent principal required", http.StatusForbidden)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, nodeID, terr := s.tenantNodeForAgent(r.Context(), principal)
	if terr != nil {
		http.Error(w, terr.Error(), http.StatusUnauthorized)
		return
	}

	// Rate-limit per (tenant, node). 10_000 events/sec default, burst 20_000.
	if s.ingestLimiter == nil {
		s.ingestLimiter = newRateLimiterRegistry(rate.Limit(10_000), 20_000)
	}

	const (
		maxCompressed   = 1 << 20    // 1 MiB
		maxDecompressed = 5 << 20    // 5 MiB
		maxRows         = 5_000
	)

	r.Body = http.MaxBytesReader(w, r.Body, maxCompressed)
	defer func() { _ = r.Body.Close() }()
	rawBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}

	var stream io.Reader
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(strings.NewReader(string(rawBytes)))
		if err != nil {
			http.Error(w, fmt.Sprintf("gzip decode: %v", err), http.StatusBadRequest)
			return
		}
		defer func() { _ = gz.Close() }()
		stream = io.LimitReader(gz, maxDecompressed)
	} else {
		stream = strings.NewReader(string(rawBytes))
	}

	events := make([]IngestedEvent, 0, 128)
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev IngestedEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			http.Error(w, fmt.Sprintf("bad event at row %d: %v", len(events)+1, err), http.StatusBadRequest)
			return
		}
		if !allowedEventTypes[ev.Type] {
			http.Error(w, fmt.Sprintf("event_type %q not allowed", ev.Type), http.StatusBadRequest)
			return
		}
		if ev.TS.IsZero() {
			ev.TS = time.Now().UTC()
		}
		// Reject events too far in the future or past — clock-skew abuse.
		now := time.Now().UTC()
		if ev.TS.After(now.Add(30*time.Minute)) || ev.TS.Before(now.Add(-7*24*time.Hour)) {
			http.Error(w, fmt.Sprintf("event ts %s out of range", ev.TS.Format(time.RFC3339)), http.StatusBadRequest)
			return
		}
		events = append(events, ev)
		if len(events) > maxRows {
			http.Error(w, "batch exceeds 5000 events", http.StatusRequestEntityTooLarge)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	if len(events) == 0 {
		http.Error(w, "empty batch", http.StatusBadRequest)
		return
	}

	// Token-bucket gate.
	limiterKey := tenantID.String() + "/" + nodeID.String()
	if ok, delay := s.ingestLimiter.allow(limiterKey, len(events)); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", delay.Seconds()+0.5))
		http.Error(w, "rate limit", http.StatusTooManyRequests)
		return
	}

	// Journal first.
	tArg := tenantID
	nArg := nodeID
	batchID, err := s.store.RecordEventIngest(r.Context(), storage.CreateEventIngestBatchParams{
		TenantID: &tArg, NodeID: &nArg,
		SizeBytes: int64(len(rawBytes)), Rows: len(events),
		Status:  "received",
		Payload: rawBytes, // gzipped raw body — kept until drained or pruned
	})
	if err != nil {
		s.logger.Error("journal events ingest", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Fan out.
	dorisStatus, fanoutErr := s.fanOutEvents(r.Context(), tenantID, nodeID, events)
	finalStatus := "accepted"
	errMsg := ""
	if fanoutErr != nil {
		finalStatus = "pending_doris"
		errMsg = fanoutErr.Error()
	}
	if uerr := s.store.MarkEventIngestStatus(r.Context(), batchID, finalStatus, dorisStatus, errMsg); uerr != nil {
		s.logger.Warn("mark ingest status", zap.Error(uerr))
	}

	resp := map[string]any{
		"batch_id":      batchID.String(),
		"rows":          len(events),
		"status":        finalStatus,
		"doris_status":  dorisStatus,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// fanOutEvents writes the batch to Doris (when configured), increments the
// Postgres hourly rollup, and publishes each event on the in-memory bus for
// correlation engine consumers. Runs Phase F anomaly detectors first so any
// synthetic anomaly.* events ride the same downstream path.
func (s *Server) fanOutEvents(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
	if anomalies := s.detectAnomalies(ctx, tenantID, nodeID, events); len(anomalies) > 0 {
		// Stamp tenant/node onto synthetic events so eventToDorisRow has
		// what it needs.
		for i := range anomalies {
			if anomalies[i].TenantID == "" {
				anomalies[i].TenantID = tenantID.String()
			}
			if anomalies[i].NodeID == "" && nodeID != uuid.Nil {
				anomalies[i].NodeID = nodeID.String()
			}
			if anomalies[i].TS.IsZero() {
				anomalies[i].TS = time.Now().UTC()
			}
		}
		events = append(events, anomalies...)
	}
	dorisStatus := ""
	var dorisErr error

	if s.dorisWriter != nil {
		dorisRows := make([]map[string]any, 0, len(events))
		connRows := make([]map[string]any, 0)
		lineageRows := make([]map[string]any, 0)
		fileRows := make([]map[string]any, 0)
		dbRows := make([]map[string]any, 0)
		for i := range events {
			ev := &events[i]
			row := eventToDorisRow(tenantID, nodeID, ev)
			dorisRows = append(dorisRows, row)
			switch ev.Type {
			case "conn.open", "conn.close", "conn.state_change", "conn.summary":
				connRows = append(connRows, eventToConnRow(tenantID, nodeID, ev))
			case "proc.exec", "proc.exit":
				lineageRows = append(lineageRows, eventToLineageRow(tenantID, nodeID, ev))
			case "file.open", "file.read.summary", "file.write.summary", "file.unlink", "file.rename":
				fileRows = append(fileRows, eventToFileRow(tenantID, nodeID, ev))
			case "db.query":
				dbRows = append(dbRows, eventToDBQueryRow(tenantID, nodeID, ev))
			}
		}
		if err := s.dorisWriter.EnqueueNonBlocking("events", dorisRows); err != nil {
			dorisErr = err
		}
		if len(connRows) > 0 {
			_ = s.dorisWriter.EnqueueNonBlocking("process_connections", connRows)
		}
		if len(lineageRows) > 0 {
			_ = s.dorisWriter.EnqueueNonBlocking("process_lineage", lineageRows)
		}
		if len(fileRows) > 0 {
			_ = s.dorisWriter.EnqueueNonBlocking("file_accesses", fileRows)
		}
		if len(dbRows) > 0 {
			_ = s.dorisWriter.EnqueueNonBlocking("db_queries", dbRows)
		}
		if dorisErr != nil {
			dorisStatus = "pending"
		} else {
			dorisStatus = "queued"
		}
	} else {
		dorisStatus = "disabled"
	}

	// Postgres hourly rollup.
	rollups := buildRollupBuckets(tenantID, nodeID, events)
	for _, b := range rollups {
		if err := s.store.IncrementHourlyRollup(ctx, b.tenantID, b.nodeID, b.eventType, b.hour, b.cnt, b.bytesIn, b.bytesOut, b.sevMax); err != nil {
			s.logger.Warn("rollup increment", zap.Error(err))
		}
	}

	// Eventbus fan-out for correlation + UI live SSE.
	if s.eventBus != nil {
		for i := range events {
			ev := &events[i]
			payload, _ := json.Marshal(ev)
			topic := eventTopicFor(ev.Type)
			s.eventBus.Publish(eventbus.Event{
				Topic:    topic,
				TenantID: tenantID,
				NodeID:   nodeIDPtr(nodeID),
				Payload:  payload,
			})
		}
	}

	return dorisStatus, dorisErr
}

// eventTopicFor maps the event_type to an eventbus topic that existing
// correlators already subscribe to.
func eventTopicFor(t string) string {
	switch {
	case strings.HasPrefix(t, "conn."):
		return "events.connection"
	case strings.HasPrefix(t, "proc."):
		return "events.process"
	case strings.HasPrefix(t, "file."):
		return "events.file"
	case t == "db.query":
		return "events.db"
	case strings.HasPrefix(t, "bastion."):
		return "events.bastion"
	case strings.HasPrefix(t, "anomaly."):
		return "events.anomaly"
	case t == "log.spike":
		return "events.log_spike"
	case t == "db.query.long_running":
		return "events.db.long_running"
	case t == "rule.trigger":
		return eventbus.TopicRuleTriggered
	case t == "security.event":
		return eventbus.TopicSecurityEvent
	case t == "health.incident":
		return eventbus.TopicHealthIncident
	case t == "compliance.result":
		return eventbus.TopicComplianceFired
	}
	return "events.other"
}

func nodeIDPtr(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	c := u
	return &c
}

// rollupBucket aggregates events by (tenant, node, type, hour) for the
// Postgres rollup table.
type rollupBucket struct {
	tenantID  uuid.UUID
	nodeID    *uuid.UUID
	eventType string
	hour      time.Time
	cnt       int64
	bytesIn   int64
	bytesOut  int64
	sevMax    string
}

func buildRollupBuckets(tenantID, nodeID uuid.UUID, events []IngestedEvent) []rollupBucket {
	type key struct {
		typ  string
		hour time.Time
	}
	var nID *uuid.UUID
	if nodeID != uuid.Nil {
		c := nodeID
		nID = &c
	}
	m := make(map[key]*rollupBucket)
	severityRank := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
	for i := range events {
		ev := &events[i]
		k := key{typ: ev.Type, hour: ev.TS.UTC().Truncate(time.Hour)}
		b, ok := m[k]
		if !ok {
			b = &rollupBucket{tenantID: tenantID, nodeID: nID, eventType: ev.Type, hour: k.hour}
			m[k] = b
		}
		b.cnt++
		b.bytesIn += ev.BytesIn
		b.bytesOut += ev.BytesOut
		if severityRank[strings.ToLower(ev.Severity)] > severityRank[strings.ToLower(b.sevMax)] {
			b.sevMax = ev.Severity
		}
	}
	out := make([]rollupBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	return out
}

// eventToDorisRow converts an IngestedEvent to the column shape of the Doris
// `events` table. Bytes/details are passed through; dates derived from ts.
func eventToDorisRow(tenantID, nodeID uuid.UUID, ev *IngestedEvent) map[string]any {
	row := map[string]any{
		"event_date":         ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":          tenantID.String(),
		"ts":                 ev.TS.UTC().Format("2006-01-02 15:04:05.000"),
		"event_type":         ev.Type,
		"severity":           ev.Severity,
		"correlation_id":     ev.CorrelationID,
		"conn_id":            ev.ConnID,
		"bastion_session_id": ev.BastionSessID,
		"pid":                ev.PID,
		"process_name":       ev.ProcessName,
		"user_name":          ev.UserName,
		"src_ip":             ev.SrcIP,
		"src_port":           ev.SrcPort,
		"dst_ip":             ev.DstIP,
		"dst_port":           ev.DstPort,
		"protocol":           ev.Protocol,
		"bytes_in":           ev.BytesIn,
		"bytes_out":          ev.BytesOut,
		"duration_ms":        ev.DurationMS,
		"rule_id":            ev.RuleID,
		"threat_feed":        ev.ThreatFeed,
		"threat_score":       ev.ThreatScore,
		"message":            ev.Message,
		"dedup_key":          ev.DedupKey,
	}
	if nodeID != uuid.Nil {
		row["node_id"] = nodeID.String()
	}
	if len(ev.Details) > 0 {
		raw, _ := json.Marshal(ev.Details)
		// Hard 1 KiB cap to bound storage. Details survive in the journal.
		if len(raw) > 1024 {
			raw = raw[:1024]
		}
		row["details_json"] = string(raw)
	}
	return row
}

func eventToConnRow(tenantID, nodeID uuid.UUID, ev *IngestedEvent) map[string]any {
	return map[string]any{
		"event_date":         ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":          tenantID.String(),
		"node_id":            nodeID.String(),
		"conn_id":            ev.ConnID,
		"correlation_id":     ev.CorrelationID,
		"bastion_session_id": ev.BastionSessID,
		"started_at":         detailsString(ev.Details, "started_at", ev.TS.UTC().Format("2006-01-02 15:04:05.000")),
		"ended_at":           detailsString(ev.Details, "ended_at", ""),
		"last_data_at":       detailsString(ev.Details, "last_data_at", ""),
		"duration_ms":        ev.DurationMS,
		"direction":          detailsString(ev.Details, "direction", ""),
		"pid":                ev.PID,
		"process_name":       ev.ProcessName,
		"cmdline":            detailsString(ev.Details, "cmdline", ""),
		"user_name":          ev.UserName,
		"uid":                detailsInt(ev.Details, "uid"),
		"gid":                detailsInt(ev.Details, "gid"),
		"exe_hash":           detailsString(ev.Details, "exe_hash", ""),
		"src_ip":             ev.SrcIP,
		"src_port":           ev.SrcPort,
		"dst_ip":             ev.DstIP,
		"dst_port":           ev.DstPort,
		"protocol":           ev.Protocol,
		"bytes_in":           ev.BytesIn,
		"bytes_out":          ev.BytesOut,
		"packets_in":         detailsInt(ev.Details, "packets_in"),
		"packets_out":        detailsInt(ev.Details, "packets_out"),
		"threat_match":       ev.ThreatFeed != "",
		"threat_feed":        ev.ThreatFeed,
		"threat_score":       ev.ThreatScore,
		"closed_reason":      detailsString(ev.Details, "closed_reason", ""),
	}
}

func eventToLineageRow(tenantID, nodeID uuid.UUID, ev *IngestedEvent) map[string]any {
	return map[string]any{
		"event_date":   ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":    tenantID.String(),
		"node_id":      nodeID.String(),
		"observed_at":  ev.TS.UTC().Format("2006-01-02 15:04:05.000"),
		"pid":          ev.PID,
		"ppid":         detailsInt(ev.Details, "ppid"),
		"process_name": ev.ProcessName,
		"cmdline":      detailsString(ev.Details, "cmdline", ""),
		"user_name":    ev.UserName,
		"uid":          detailsInt(ev.Details, "uid"),
		"gid":          detailsInt(ev.Details, "gid"),
		"exe_path":     detailsString(ev.Details, "exe_path", ""),
		"exe_hash":     detailsString(ev.Details, "exe_hash", ""),
		"exited_at":    detailsString(ev.Details, "exited_at", ""),
	}
}

func eventToFileRow(tenantID, nodeID uuid.UUID, ev *IngestedEvent) map[string]any {
	return map[string]any{
		"event_date":     ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":      tenantID.String(),
		"node_id":        nodeID.String(),
		"ts":             ev.TS.UTC().Format("2006-01-02 15:04:05.000"),
		"correlation_id": ev.CorrelationID,
		"conn_id":        ev.ConnID,
		"pid":            ev.PID,
		"process_name":   ev.ProcessName,
		"user_name":      ev.UserName,
		"path":           detailsString(ev.Details, "path", ev.Message),
		"op":             detailsString(ev.Details, "op", strings.TrimPrefix(ev.Type, "file.")),
		"bytes":          ev.BytesIn + ev.BytesOut,
		"op_count":       detailsInt(ev.Details, "op_count"),
		"started_at":     detailsString(ev.Details, "started_at", ""),
		"ended_at":       detailsString(ev.Details, "ended_at", ""),
	}
}

func eventToDBQueryRow(tenantID, nodeID uuid.UUID, ev *IngestedEvent) map[string]any {
	return map[string]any{
		"event_date":     ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":      tenantID.String(),
		"node_id":        nodeID.String(),
		"ts":             ev.TS.UTC().Format("2006-01-02 15:04:05.000"),
		"correlation_id": ev.CorrelationID,
		"conn_id":        ev.ConnID,
		"pid":            ev.PID,
		"engine":         detailsString(ev.Details, "engine", ""),
		"database_name":  detailsString(ev.Details, "database_name", ""),
		"user_name":      ev.UserName,
		"src_ip":         ev.SrcIP,
		"query_hash":     detailsString(ev.Details, "query_hash", ""),
		"query_text":     truncString(ev.Message, 512),
		"rows_affected":  detailsInt(ev.Details, "rows_affected"),
		"exec_time_ms":   ev.DurationMS,
		"started_at":     detailsString(ev.Details, "started_at", ""),
		"ended_at":       detailsString(ev.Details, "ended_at", ""),
		"tables_touched": detailsString(ev.Details, "tables_touched", ""),
	}
}

func detailsString(d map[string]any, key, fallback string) string {
	if v, ok := d[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func detailsInt(d map[string]any, key string) int64 {
	if v, ok := d[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int:
			return int64(n)
		case int64:
			return n
		}
	}
	return 0
}

func truncString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// tenantNodeForAgent looks up the node row keyed by the cert-derived
// principal subject (a UUID stored on the node). Returns the (tenant_id,
// node_id) pair the rest of ingest scopes by.
func (s *Server) tenantNodeForAgent(ctx context.Context, principal *auth.Principal) (uuid.UUID, uuid.UUID, error) {
	subj := principal.Subject
	// The agent enrollment stamps the node id into the cert CN; production
	// deployments use that as the subject. For the static dev path the
	// subject is "node:<uuid>".
	id := strings.TrimPrefix(subj, "node:")
	parts := strings.Split(id, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "CN=") {
			id = strings.TrimPrefix(p, "CN=")
			break
		}
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("agent principal subject is not a node UUID")
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("lookup node: %w", err)
	}
	if node == nil {
		return uuid.Nil, uuid.Nil, errors.New("agent principal not registered")
	}
	return node.TenantID, node.ID, nil
}
