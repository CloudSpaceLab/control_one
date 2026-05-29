package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/securityschema"
)

// IngestedEvent is the wire shape every collector publishes. Server only
// validates the discriminator + a few required fields; everything else
// passes through to Doris / rollups / eventbus.
type IngestedEvent struct {
	SchemaVersion int            `json:"schema_version,omitempty"`
	EventID       string         `json:"event_id,omitempty"`
	Type          string         `json:"type"`
	TS            time.Time      `json:"ts"`
	NodeID        string         `json:"node_id,omitempty"`
	TenantID      string         `json:"tenant_id,omitempty"`
	RawRef        string         `json:"raw_ref,omitempty"`
	Collector     string         `json:"collector,omitempty"`
	Parser        string         `json:"parser,omitempty"`
	ParserStatus  string         `json:"parser_status,omitempty"`
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

const (
	maxEventIngestCompressed   = 1 << 20 // 1 MiB
	maxEventIngestDecompressed = 5 << 20 // 5 MiB
	maxEventIngestRows         = 5_000
)

var errEventIngestTooManyRows = errors.New("batch exceeds 5000 events")

// allowedEventTypes is the closed-world set of event_type discriminators we
// accept from agents. Add new families here when collectors land.
var allowedEventTypes = map[string]bool{
	// Network connections
	"conn.open":         true,
	"conn.close":        true,
	"conn.state_change": true,
	"conn.summary":      true,
	// Process lifecycle
	"proc.exec":  true,
	"proc.exit":  true,
	"proc.usage": true,
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
	// Web request intelligence
	"web.request": true,
	"web.error":   true,
	// Behavioural anomaly detectors (Phase F)
	"anomaly.new_destination":    true,
	"anomaly.long_connection":    true,
	"anomaly.high_bytes_out":     true,
	"anomaly.fast_bulk_transfer": true,
	"anomaly.packet_scan":        true,
	"anomaly.new_executable":     true,
	"anomaly.executable_dropped": true,
	"anomaly.new_db_query":       true,
	"anomaly.db_query_high_rows": true,
	"anomaly.ip_behavior":        true,
	// Long-running DB query (Phase G)
	"db.query.long_running": true,
	// Compatibility shims for older event flavours
	"security.event":                      true,
	"health.incident":                     true,
	"compliance.result":                   true,
	"remediation.webserver_block.applied": true,
	"remediation.webserver_block.failed":  true,
	"webserver.config.changed":            true,
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
//	POST /api/v1/events/ingest
//	Content-Encoding: gzip
//	Content-Type:     application/x-ndjson
//	Body: one IngestedEvent JSON per line
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
	replayKey, err := sanitizeReplayKey(r.Header.Get("X-ControlOne-Replay-Key"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Rate-limit per (tenant, node). 10_000 events/sec default, burst 20_000.
	if s.ingestLimiter == nil {
		s.ingestLimiter = newRateLimiterRegistry(rate.Limit(10_000), 20_000)
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxEventIngestCompressed)
	defer func() { _ = r.Body.Close() }()
	rawBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}

	events, err := decodeIngestedEventPayload(rawBytes, r.Header.Get("Content-Encoding"), maxEventIngestDecompressed, maxEventIngestRows)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errEventIngestTooManyRows) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}

	now := time.Now().UTC()
	for i := range events {
		ev := &events[i]
		if ev.TS.IsZero() {
			ev.TS = now
		}
		// Reject events too far in the future or past — clock-skew abuse.
		if ev.TS.After(now.Add(30*time.Minute)) || ev.TS.Before(now.Add(-7*24*time.Hour)) {
			http.Error(w, fmt.Sprintf("event ts %s out of range", ev.TS.Format(time.RFC3339)), http.StatusBadRequest)
			return
		}
		normalizeIngestedEventMetadata(ev, tenantID, nodeID)
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
		Status:    "received",
		ReplayKey: replayKey,
		Payload:   rawBytes, // gzipped raw body — kept until drained or pruned
	})
	if err != nil {
		var duplicate *storage.DuplicateEventIngestReplayError
		if errors.As(err, &duplicate) {
			writeEventIngestReplayReceipt(w, duplicate.Batch)
			return
		}
		s.logger.Error("journal events ingest", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	dorisStatus, finalStatus, fanoutErr := s.eventIngestService().complete(r.Context(), batchID, tenantID, nodeID, events)
	if fanoutErr != nil {
		s.logger.Warn("fanout event ingest", zap.Error(fanoutErr), zap.String("doris_status", dorisStatus))
	}

	resp := map[string]any{
		"batch_id":     batchID.String(),
		"rows":         len(events),
		"status":       finalStatus,
		"doris_status": dorisStatus,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func sanitizeReplayKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", nil
	}
	if len(key) > 200 {
		return "", errors.New("replay key exceeds 200 bytes")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '.', '_', '-', ':', '/':
			continue
		default:
			return "", errors.New("replay key contains unsupported characters")
		}
	}
	return key, nil
}

func writeEventIngestReplayReceipt(w http.ResponseWriter, batch storage.EventIngestBatch) {
	status := strings.TrimSpace(batch.Status)
	if status == "" {
		status = "received"
	}
	dorisStatus := ""
	if batch.DorisStatus.Valid {
		dorisStatus = batch.DorisStatus.String
	}
	resp := map[string]any{
		"batch_id":     batch.ID.String(),
		"rows":         batch.Rows,
		"status":       status,
		"doris_status": dorisStatus,
		"duplicate":    true,
	}
	if batch.ReplayKey.Valid && strings.TrimSpace(batch.ReplayKey.String) != "" {
		resp["replay_key"] = strings.TrimSpace(batch.ReplayKey.String)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func decodeIngestedEventPayload(payload []byte, contentEncoding string, maxDecompressed int64, maxRows int) ([]IngestedEvent, error) {
	if maxDecompressed <= 0 {
		maxDecompressed = maxEventIngestDecompressed
	}
	if maxRows <= 0 {
		maxRows = maxEventIngestRows
	}
	stream, closeFn, err := ingestedEventPayloadReader(payload, contentEncoding, maxDecompressed)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	events := make([]IngestedEvent, 0, 128)
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	row := 0
	for scanner.Scan() {
		row++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(events) >= maxRows {
			return nil, errEventIngestTooManyRows
		}
		var ev IngestedEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("bad event at row %d: %w", row, err)
		}
		if !allowedEventTypes[ev.Type] {
			return nil, fmt.Errorf("event_type %q not allowed", ev.Type)
		}
		if err := validateIngestedEventContract(&ev); err != nil {
			return nil, fmt.Errorf("bad event at row %d: %w", row, err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(events) == 0 {
		return nil, errors.New("empty batch")
	}
	return events, nil
}

func encodeIngestedEventPayload(events []IngestedEvent) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range events {
		if err := enc.Encode(events[i]); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func ingestedEventPayloadReader(payload []byte, contentEncoding string, maxDecompressed int64) (io.Reader, func(), error) {
	encoding := strings.ToLower(strings.TrimSpace(contentEncoding))
	gzipPayload := encoding == "gzip" || (encoding == "" && len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b)
	if !gzipPayload {
		return bytes.NewReader(payload), func() {}, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, func() {}, fmt.Errorf("gzip decode: %w", err)
	}
	return io.LimitReader(gz, maxDecompressed), func() { _ = gz.Close() }, nil
}

func validateIngestedEventContract(ev *IngestedEvent) error {
	if ev == nil {
		return errors.New("event required")
	}
	if ev.SchemaVersion < 0 {
		return errors.New("schema_version cannot be negative")
	}
	switch ev.Type {
	case "web.request":
		if net.ParseIP(strings.TrimSpace(ev.SrcIP)) == nil {
			return errors.New("web.request requires valid src_ip")
		}
		if ev.BytesIn < 0 || ev.BytesOut < 0 || ev.DurationMS < 0 {
			return errors.New("web.request counters cannot be negative")
		}
		if ev.Details != nil {
			if status := detailInt(ev.Details, "status_code"); status != 0 && (status < 100 || status > 599) {
				return errors.New("web.request status_code must be a valid HTTP status")
			}
		}
	case "web.error":
		if strings.TrimSpace(ev.Message) == "" && len(ev.Details) == 0 {
			return errors.New("web.error requires message or details")
		}
	case "anomaly.ip_behavior":
		if net.ParseIP(strings.TrimSpace(ev.SrcIP)) == nil {
			return errors.New("anomaly.ip_behavior requires valid src_ip")
		}
		if strings.TrimSpace(ev.Severity) == "" {
			return errors.New("anomaly.ip_behavior requires severity")
		}
		if strings.TrimSpace(ev.DedupKey) == "" {
			return errors.New("anomaly.ip_behavior requires dedup_key")
		}
	case "remediation.webserver_block.applied", "remediation.webserver_block.failed", "webserver.config.changed":
		if strings.TrimSpace(ev.CorrelationID) == "" && strings.TrimSpace(ev.DedupKey) == "" {
			return fmt.Errorf("%s requires correlation_id or dedup_key", ev.Type)
		}
	}
	if strings.TrimSpace(ev.CorrelationID) == "" && strings.TrimSpace(ev.DedupKey) != "" {
		ev.CorrelationID = ev.DedupKey
	}
	if err := validateIngestedEventSchema(ev); err != nil {
		return err
	}
	return nil
}

func validateIngestedEventSchema(ev *IngestedEvent) error {
	fields := normalizedFieldsForIngestedEvent(ev)
	if len(fields) == 0 {
		return nil
	}
	violations := securityschema.Validate(fields)
	if len(violations) == 0 {
		return nil
	}
	parts := make([]string, 0, len(violations))
	for _, violation := range violations {
		parts = append(parts, violation.Field+" "+violation.Message)
	}
	return fmt.Errorf("normalized schema violation: %s", strings.Join(parts, "; "))
}

func normalizedFieldsForIngestedEvent(ev *IngestedEvent) map[string]any {
	if ev == nil {
		return nil
	}
	fields := map[string]any{}
	mergeNormalizedFields(fields, ev.Details)
	if nested, ok := ev.Details["fields"].(map[string]any); ok {
		mergeNormalizedFields(fields, nested)
	}
	if nested, ok := ev.Details["normalized"].(map[string]any); ok {
		mergeNormalizedFields(fields, nested)
	}
	if strings.TrimSpace(ev.UserName) != "" {
		fields["user.name"] = strings.TrimSpace(ev.UserName)
	}
	if strings.TrimSpace(ev.SrcIP) != "" {
		fields["source.ip"] = strings.TrimSpace(ev.SrcIP)
	}
	if ev.SrcPort != 0 {
		fields["source.port"] = ev.SrcPort
	}
	if strings.TrimSpace(ev.DstIP) != "" {
		fields["destination.ip"] = strings.TrimSpace(ev.DstIP)
	}
	if ev.DstPort != 0 {
		fields["destination.port"] = ev.DstPort
	}
	if strings.TrimSpace(ev.Protocol) != "" {
		fields["network.protocol"] = strings.TrimSpace(ev.Protocol)
	}
	if strings.TrimSpace(ev.RuleID) != "" {
		fields["rule.id"] = strings.TrimSpace(ev.RuleID)
	}
	return fields
}

func mergeNormalizedFields(dst map[string]any, src map[string]any) {
	if len(dst) == 0 && dst == nil {
		return
	}
	for key, value := range securityschema.ECSAliases(src) {
		if value != nil {
			dst[key] = value
		}
	}
}

func detailInt(details map[string]any, key string) int {
	if details == nil {
		return 0
	}
	switch v := details[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func normalizeIngestedEventMetadata(ev *IngestedEvent, tenantID, nodeID uuid.UUID) {
	if ev == nil {
		return
	}
	if ev.SchemaVersion <= 0 {
		ev.SchemaVersion = 1
	}
	ev.EventID = strings.TrimSpace(ev.EventID)
	if ev.EventID == "" {
		ev.EventID = deterministicEventID(tenantID, nodeID, ev)
	}
}

func deterministicEventID(tenantID, nodeID uuid.UUID, ev *IngestedEvent) string {
	if ev == nil {
		return ""
	}
	clone := *ev
	clone.EventID = ""
	if clone.SchemaVersion <= 0 {
		clone.SchemaVersion = 1
	}
	material := struct {
		TenantID string        `json:"tenant_id_scope"`
		NodeID   string        `json:"node_id_scope,omitempty"`
		Event    IngestedEvent `json:"event"`
	}{
		TenantID: tenantID.String(),
		Event:    clone,
	}
	if nodeID != uuid.Nil {
		material.NodeID = nodeID.String()
	}
	raw, err := json.Marshal(material)
	if err != nil {
		raw = []byte(fmt.Sprintf("%#v", material))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// fanOutEvents writes the batch to Doris (when configured), increments the
// Postgres hourly rollup, and publishes each event on the in-memory bus for
// correlation engine consumers. Runs Phase F anomaly detectors first so any
// synthetic anomaly.* events ride the same downstream path.
func (s *Server) fanOutEvents(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
	return s.fanOutEventsWithBatch(ctx, uuid.Nil, tenantID, nodeID, events)
}

func (s *Server) fanOutEventsWithBatch(ctx context.Context, batchID, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
	fanoutEvents, anomalies := s.prepareEventFanout(ctx, tenantID, nodeID, events)
	s.applyLocalEventFanout(ctx, tenantID, nodeID, fanoutEvents, anomalies)
	return s.flushDorisEventFanout(ctx, batchID, tenantID, nodeID, fanoutEvents)
}

func (s *Server) prepareEventFanout(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) ([]IngestedEvent, []IngestedEvent) {
	fanoutEvents := append([]IngestedEvent(nil), events...)
	s.enrichConnectionThreatIntel(tenantID, fanoutEvents)
	anomalies := s.detectAnomalies(ctx, tenantID, nodeID, fanoutEvents)
	if len(anomalies) > 0 {
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
		fanoutEvents = append(fanoutEvents, anomalies...)
	}
	for i := range fanoutEvents {
		normalizeIngestedEventMetadata(&fanoutEvents[i], tenantID, nodeID)
	}
	return fanoutEvents, anomalies
}

func (s *Server) applyLocalEventFanout(ctx context.Context, tenantID, nodeID uuid.UUID, events, anomalies []IngestedEvent) {
	if len(anomalies) > 0 {
		s.persistAnomalyInvestigations(ctx, tenantID, nodeID, anomalies)
	}
	s.persistAILogFixerTriggers(ctx, tenantID, nodeID, events)
	s.evaluateContentPackDetections(ctx, tenantID, nodeID, events)

	// Postgres hourly rollup.
	s.recordIPBehaviorRollups(ctx, tenantID, nodeID, events)
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
}

func (s *Server) flushDorisEventFanout(ctx context.Context, batchID, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
	dorisStatus := ""
	var dorisErr error

	dorisRows := buildDorisEventRows(tenantID, nodeID, events)
	syncDoris := batchID != uuid.Nil && s.dorisClient != nil
	if syncDoris {
		if err := s.streamLoadDorisEventRows(ctx, batchID, dorisRows); err != nil {
			dorisStatus = "pending"
			return dorisStatus, err
		}
		dorisStatus = "loaded"
	} else if s.dorisWriter != nil {
		dorisErr = s.enqueueDorisEventRows(dorisRows)
		if dorisErr != nil {
			dorisStatus = "pending"
		} else {
			dorisStatus = "queued"
		}
	} else {
		dorisStatus = "disabled"
	}

	return dorisStatus, dorisErr
}

type dorisEventRows struct {
	events  []map[string]any
	conns   []map[string]any
	lineage []map[string]any
	files   []map[string]any
	db      []map[string]any
	web     []map[string]any
}

func buildDorisEventRows(tenantID, nodeID uuid.UUID, events []IngestedEvent) dorisEventRows {
	rows := dorisEventRows{events: make([]map[string]any, 0, len(events))}
	for i := range events {
		ev := &events[i]
		rows.events = append(rows.events, eventToDorisRow(tenantID, nodeID, ev))
		switch ev.Type {
		case "conn.open", "conn.close", "conn.state_change", "conn.summary":
			rows.conns = append(rows.conns, eventToConnRow(tenantID, nodeID, ev))
		case "proc.exec", "proc.exit":
			rows.lineage = append(rows.lineage, eventToLineageRow(tenantID, nodeID, ev))
		case "file.open", "file.read.summary", "file.write.summary", "file.unlink", "file.rename":
			rows.files = append(rows.files, eventToFileRow(tenantID, nodeID, ev))
		case "db.query", "db.query.long_running":
			rows.db = append(rows.db, eventToDBQueryRow(tenantID, nodeID, ev))
		case "web.request", "web.error":
			rows.web = append(rows.web, eventToWebRequestRow(tenantID, nodeID, ev))
		}
	}
	return rows
}

type dorisEventTableRows struct {
	table string
	rows  []map[string]any
}

func (r dorisEventRows) tables() []dorisEventTableRows {
	return []dorisEventTableRows{
		{table: "events", rows: r.events},
		{table: "process_connections", rows: r.conns},
		{table: "process_lineage", rows: r.lineage},
		{table: "file_accesses", rows: r.files},
		{table: "db_queries", rows: r.db},
		{table: "web_requests", rows: r.web},
	}
}

func (s *Server) streamLoadDorisEventRows(ctx context.Context, batchID uuid.UUID, rows dorisEventRows) error {
	if s == nil || s.dorisClient == nil {
		return nil
	}
	for _, tableRows := range rows.tables() {
		if len(tableRows.rows) == 0 {
			continue
		}
		label := doris.LabelFor(tableRows.table, batchID.String())
		if _, err := s.dorisClient.StreamLoadJSON(ctx, tableRows.table, tableRows.rows, doris.StreamLoadJSONOptions{Label: label}); err != nil {
			return fmt.Errorf("doris stream load %s: %w", tableRows.table, err)
		}
	}
	return nil
}

func (s *Server) enqueueDorisEventRows(rows dorisEventRows) error {
	if s == nil || s.dorisWriter == nil {
		return nil
	}
	if !s.dorisWriter.Healthy() {
		return errors.New("doris writer unhealthy")
	}
	var firstErr error
	for _, tableRows := range rows.tables() {
		if len(tableRows.rows) == 0 {
			continue
		}
		if err := s.dorisWriter.EnqueueNonBlocking(tableRows.table, tableRows.rows); err != nil {
			if tableRows.table == "events" && firstErr == nil {
				firstErr = err
			}
			if tableRows.table != "events" {
				s.logger.Warn("doris enqueue "+tableRows.table,
					zap.Int("rows", len(tableRows.rows)), zap.Error(err))
			}
		}
	}
	return firstErr
}

type eventIngestStatusMarker interface {
	MarkEventIngestStatus(context.Context, uuid.UUID, string, string, string) error
}

type eventFanOutFunc func(context.Context, uuid.UUID, uuid.UUID, []IngestedEvent) (string, error)

// drainEventIngestBatch replays one persisted journal payload through the
// same fan-out path as live ingest. Coordinator wiring point: call
// drainPendingEventIngestBatches from the server startup scheduler after
// store and dorisWriter are initialised, with a short interval and a small
// limit, so stuck 'received'/'pending_doris' rows retry durably.
func (s *Server) drainEventIngestBatch(ctx context.Context, batch storage.EventIngestBatch) error {
	if s == nil || s.store == nil {
		return errors.New("storage unavailable")
	}
	ingest := s.eventIngestService()
	if isDorisOnlyEventIngestStatus(batch.Status) {
		if s.dorisClient != nil {
			return drainEventIngestBatchDorisOnly(ctx, batch, s.store, func(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
				return ingest.flushDoris(ctx, batch.ID, tenantID, nodeID, events)
			})
		}
		if s.dorisWriter != nil {
			return drainEventIngestBatchDorisOnly(ctx, batch, s.store, func(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
				return ingest.flushDoris(ctx, batch.ID, tenantID, nodeID, events)
			})
		}
		err := errors.New("doris writer unavailable for pending event ingest batch")
		_ = s.store.MarkEventIngestStatus(ctx, batch.ID, "pending_doris", "disabled", err.Error())
		return err
	}
	return drainEventIngestBatch(ctx, batch, s.store, func(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
		return ingest.fanoutMarkLocalAndFlush(ctx, batch.ID, tenantID, nodeID, events)
	})
}

func isDorisOnlyEventIngestStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "local_completed", "pending_doris":
		return true
	default:
		return false
	}
}

func (s *Server) drainPendingEventIngestBatches(ctx context.Context, olderThan time.Duration, limit int) (int, int, error) {
	if s == nil || s.store == nil {
		return 0, 0, errors.New("storage unavailable")
	}
	batches, err := s.store.PendingEventIngestBatches(ctx, olderThan, limit)
	if err != nil {
		return 0, 0, err
	}
	drained := 0
	failed := 0
	var joined error
	for _, batch := range batches {
		if err := s.drainEventIngestBatch(ctx, batch); err != nil {
			failed++
			joined = errors.Join(joined, err)
			continue
		}
		drained++
	}
	return drained, failed, joined
}

func drainEventIngestBatch(ctx context.Context, batch storage.EventIngestBatch, marker eventIngestStatusMarker, fanOut eventFanOutFunc) error {
	if marker == nil {
		return errors.New("ingest status marker unavailable")
	}
	if fanOut == nil {
		err := errors.New("event fanout unavailable")
		_ = marker.MarkEventIngestStatus(ctx, batch.ID, "failed", "", err.Error())
		return err
	}
	if !batch.TenantID.Valid || batch.TenantID.UUID == uuid.Nil {
		err := errors.New("pending event ingest batch missing tenant_id")
		_ = marker.MarkEventIngestStatus(ctx, batch.ID, "failed", "", err.Error())
		return err
	}
	tenantID := batch.TenantID.UUID
	nodeID := uuid.Nil
	if batch.NodeID.Valid {
		nodeID = batch.NodeID.UUID
	}
	events, err := decodeIngestedEventPayload(batch.Payload, "", maxEventIngestDecompressed, maxEventIngestRows)
	if err != nil {
		err = fmt.Errorf("decode event ingest batch %s: %w", batch.ID, err)
		_ = marker.MarkEventIngestStatus(ctx, batch.ID, "failed", "", err.Error())
		return err
	}
	now := time.Now().UTC()
	for i := range events {
		if events[i].TS.IsZero() {
			events[i].TS = now
		}
		normalizeIngestedEventMetadata(&events[i], tenantID, nodeID)
	}
	dorisStatus, fanoutErr := fanOut(ctx, tenantID, nodeID, events)
	if fanoutErr != nil {
		errMsg := fanoutErr.Error()
		if err := marker.MarkEventIngestStatus(ctx, batch.ID, "pending_doris", dorisStatus, errMsg); err != nil {
			return errors.Join(fanoutErr, err)
		}
		return fanoutErr
	}
	if err := marker.MarkEventIngestStatus(ctx, batch.ID, "accepted", dorisStatus, ""); err != nil {
		return err
	}
	return nil
}

func drainEventIngestBatchDorisOnly(ctx context.Context, batch storage.EventIngestBatch, marker eventIngestStatusMarker, flush eventFanOutFunc) error {
	if marker == nil {
		return errors.New("ingest status marker unavailable")
	}
	if flush == nil {
		err := errors.New("event doris flush unavailable")
		_ = marker.MarkEventIngestStatus(ctx, batch.ID, "pending_doris", "", err.Error())
		return err
	}
	if !batch.TenantID.Valid || batch.TenantID.UUID == uuid.Nil {
		err := errors.New("pending event ingest batch missing tenant_id")
		_ = marker.MarkEventIngestStatus(ctx, batch.ID, "failed", "", err.Error())
		return err
	}
	tenantID := batch.TenantID.UUID
	nodeID := uuid.Nil
	if batch.NodeID.Valid {
		nodeID = batch.NodeID.UUID
	}
	events, err := decodeIngestedEventPayload(batch.Payload, "", maxEventIngestDecompressed, maxEventIngestRows)
	if err != nil {
		err = fmt.Errorf("decode event ingest batch %s: %w", batch.ID, err)
		_ = marker.MarkEventIngestStatus(ctx, batch.ID, "failed", "", err.Error())
		return err
	}
	now := time.Now().UTC()
	for i := range events {
		if events[i].TS.IsZero() {
			events[i].TS = now
		}
		normalizeIngestedEventMetadata(&events[i], tenantID, nodeID)
	}
	dorisStatus, flushErr := flush(ctx, tenantID, nodeID, events)
	if flushErr != nil {
		errMsg := flushErr.Error()
		if err := marker.MarkEventIngestStatus(ctx, batch.ID, "pending_doris", dorisStatus, errMsg); err != nil {
			return errors.Join(flushErr, err)
		}
		return flushErr
	}
	if err := marker.MarkEventIngestStatus(ctx, batch.ID, "accepted", dorisStatus, ""); err != nil {
		return err
	}
	return nil
}

func (s *Server) enrichConnectionThreatIntel(tenantID uuid.UUID, events []IngestedEvent) {
	if s == nil || s.threatIntel == nil || len(events) == 0 {
		return
	}
	for i := range events {
		ev := &events[i]
		switch ev.Type {
		case "conn.open", "conn.close", "conn.state_change", "conn.summary":
		default:
			continue
		}
		if ev.ThreatFeed != "" && ev.ThreatScore > 0 {
			continue
		}
		feed, score := s.connectionThreatMatch(tenantID, ev)
		if feed == "" || score <= 0 {
			continue
		}
		if ev.ThreatFeed == "" {
			ev.ThreatFeed = feed
		}
		if score > ev.ThreatScore {
			ev.ThreatScore = score
		}
		if sev := severityForScore(score); eventSeverityRank(sev) > eventSeverityRank(ev.Severity) {
			ev.Severity = sev
		}
	}
}

func (s *Server) connectionThreatMatch(tenantID uuid.UUID, ev *IngestedEvent) (string, int) {
	if ev == nil {
		return "", 0
	}
	bestFeed := ""
	bestScore := 0
	for _, raw := range connectionThreatCandidateIPs(ev) {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			continue
		}
		if !isPublicRoutableIP(ip) {
			continue
		}
		for _, ind := range s.threatIntelIPMatches(tenantID, ip) {
			feed := strings.TrimSpace(ind.Feed)
			if feed == "" {
				continue
			}
			if ind.Score > bestScore || (ind.Score == bestScore && bestFeed == "") {
				bestFeed = feed
				bestScore = ind.Score
			}
		}
	}
	return bestFeed, bestScore
}

func connectionThreatCandidateIPs(ev *IngestedEvent) []string {
	if ev == nil {
		return nil
	}
	direction := strings.ToLower(strings.TrimSpace(detailsString(ev.Details, "direction", "")))
	switch direction {
	case "inbound":
		return []string{ev.SrcIP}
	case "outbound":
		return []string{ev.DstIP}
	default:
		return []string{ev.SrcIP, ev.DstIP}
	}
}

func eventSeverityRank(sev string) int {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "warning", "medium":
		return 2
	case "low", "info":
		return 1
	default:
		return 0
	}
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
	case t == "web.request" || t == "web.error":
		return "events.web"
	case strings.HasPrefix(t, "bastion."):
		return "events.bastion"
	case strings.HasPrefix(t, "anomaly."):
		return eventbus.TopicEventsAnomaly
	case t == "log.spike":
		return "events.log_spike"
	case t == "log.line":
		return "events.log_line"
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
	normalizeIngestedEventMetadata(ev, tenantID, nodeID)
	row := map[string]any{
		"event_date":         ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":          tenantID.String(),
		"ts":                 ev.TS.UTC().Format("2006-01-02 15:04:05.000"),
		"schema_version":     ev.SchemaVersion,
		"event_id":           ev.EventID,
		"event_type":         ev.Type,
		"raw_ref":            ev.RawRef,
		"collector":          ev.Collector,
		"parser":             ev.Parser,
		"parser_status":      ev.ParserStatus,
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

func eventToWebRequestRow(tenantID, nodeID uuid.UUID, ev *IngestedEvent) map[string]any {
	normalizeIngestedEventMetadata(ev, tenantID, nodeID)
	row := map[string]any{
		"event_date":       ev.TS.UTC().Format("2006-01-02"),
		"tenant_id":        tenantID.String(),
		"node_id":          nodeID.String(),
		"ts":               ev.TS.UTC().Format("2006-01-02 15:04:05.000"),
		"schema_version":   ev.SchemaVersion,
		"event_id":         ev.EventID,
		"raw_ref":          ev.RawRef,
		"collector":        ev.Collector,
		"parser":           ev.Parser,
		"parser_status":    ev.ParserStatus,
		"correlation_id":   ev.CorrelationID,
		"webserver_kind":   detailsString(ev.Details, "webserver_kind", ev.ProcessName),
		"server_group":     detailsString(ev.Details, "server_group", ""),
		"app":              detailsString(ev.Details, "app", detailsString(ev.Details, "vhost", "")),
		"vhost":            detailsString(ev.Details, "vhost", ""),
		"src_ip":           ev.SrcIP,
		"socket_ip":        detailsString(ev.Details, "socket_ip", ""),
		"xff_chain":        detailsString(ev.Details, "xff_chain", ""),
		"country_code":     detailsString(ev.Details, "country_code", ""),
		"country":          detailsString(ev.Details, "country", ""),
		"asn":              detailsString(ev.Details, "asn", ""),
		"isp":              detailsString(ev.Details, "isp", ""),
		"reputation_score": ev.ThreatScore,
		"method":           detailsString(ev.Details, "method", ""),
		"path_template":    detailsString(ev.Details, "path_template", ""),
		"path_hash":        detailsString(ev.Details, "path_hash", ""),
		"status_code":      detailsInt(ev.Details, "status_code"),
		"status_family":    detailsString(ev.Details, "status_family", ""),
		"bytes_out":        ev.BytesOut,
		"bytes_in":         ev.BytesIn,
		"duration_ms":      ev.DurationMS,
		"upstream_status":  detailsString(ev.Details, "upstream_status", ""),
		"user_agent_hash":  detailsString(ev.Details, "user_agent_hash", ""),
		"referrer_host":    detailsString(ev.Details, "referrer_host", ""),
		"source_file":      detailsString(ev.Details, "source_file", ""),
		"parser_profile":   detailsString(ev.Details, "parser_profile", ""),
		"message":          ev.Message,
	}
	if len(ev.Details) > 0 {
		raw, _ := json.Marshal(ev.Details)
		if len(raw) > 4096 {
			raw = raw[:4096]
		}
		row["details_json"] = string(raw)
	}
	return row
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
