package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/threatintel"
)

func TestValidateIngestedEventContractForWebAndRemediationEvents(t *testing.T) {
	t.Parallel()

	valid := &IngestedEvent{
		Type:     "web.request",
		SrcIP:    "203.0.113.10",
		BytesOut: 120,
		Details:  map[string]any{"status_code": float64(401)},
		DedupKey: "web.request:test",
	}
	if err := validateIngestedEventContract(valid); err != nil {
		t.Fatalf("valid web.request rejected: %v", err)
	}
	if valid.CorrelationID != valid.DedupKey {
		t.Fatalf("correlation_id = %q, want dedup fallback", valid.CorrelationID)
	}

	invalidIP := &IngestedEvent{Type: "web.request", SrcIP: "not-an-ip"}
	if err := validateIngestedEventContract(invalidIP); err == nil {
		t.Fatal("invalid web.request source IP accepted")
	}

	invalidStatus := &IngestedEvent{
		Type:    "web.request",
		SrcIP:   "203.0.113.10",
		Details: map[string]any{"status_code": float64(799)},
	}
	if err := validateIngestedEventContract(invalidStatus); err == nil {
		t.Fatal("invalid web.request status accepted")
	}

	remediation := &IngestedEvent{Type: "remediation.webserver_block.applied"}
	if err := validateIngestedEventContract(remediation); err == nil {
		t.Fatal("remediation event without correlation accepted")
	}
}

func TestValidateIngestedEventContractRejectsNormalizedSchemaViolations(t *testing.T) {
	t.Parallel()

	badSource := &IngestedEvent{Type: "conn.open", SrcIP: "not-an-ip"}
	if err := validateIngestedEventContract(badSource); err == nil || !strings.Contains(err.Error(), "source.ip") {
		t.Fatalf("invalid source.ip error = %v", err)
	}

	badParsedFields := &IngestedEvent{
		Type: "log.line",
		Details: map[string]any{
			"fields": map[string]any{
				"event":  map[string]any{"kind": "event"},
				"source": map[string]any{"ip": "10.0.0.5", "port": "443"},
			},
		},
	}
	if err := validateIngestedEventContract(badParsedFields); err == nil || !strings.Contains(err.Error(), "source.port") {
		t.Fatalf("invalid parsed source.port error = %v", err)
	}

	valid := &IngestedEvent{
		Type:     "conn.open",
		SrcIP:    "203.0.113.10",
		SrcPort:  44310,
		DstIP:    "10.0.0.5",
		DstPort:  443,
		Protocol: "tcp",
		Details: map[string]any{
			"vendor_specific_source": "not a normalized source object",
		},
	}
	if err := validateIngestedEventContract(valid); err != nil {
		t.Fatalf("valid normalized event rejected: %v", err)
	}
}

func TestEnrichConnectionThreatIntelUsesLocalSnapshot(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	mgr := threatintel.New(threatintel.Config{
		RefreshInterval: time.Hour,
		HTTPTimeout:     time.Second,
		Sources: []threatintel.Source{staticThreatSource{indicators: []threatintel.Indicator{{
			TenantID:  tenantID.String(),
			IP:        "204.10.162.167",
			Feed:      "abuseipdb",
			Category:  "abuse",
			Score:     95,
			FirstSeen: time.Date(2026, 5, 19, 9, 0, 0, 0, time.UTC),
		}}}},
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)
	waitThreatIntelCurrent(t, mgr)

	srv := &Server{threatIntel: mgr}
	events := []IngestedEvent{{
		Type:     "conn.open",
		Severity: "info",
		SrcIP:    "204.10.162.167",
		SrcPort:  44310,
		DstIP:    "10.0.0.5",
		DstPort:  25,
		Protocol: "tcp",
	}}

	srv.enrichConnectionThreatIntel(tenantID, events)

	if events[0].ThreatFeed != "abuseipdb" || events[0].ThreatScore != 95 {
		t.Fatalf("connection threat enrichment missing: %+v", events[0])
	}
	if events[0].Severity != "critical" {
		t.Fatalf("severity = %q, want critical", events[0].Severity)
	}
	row := eventToConnRow(tenantID, nodeID, &events[0])
	if row["threat_match"] != true || row["threat_feed"] != "abuseipdb" {
		t.Fatalf("conn row did not preserve threat match: %+v", row)
	}
}

func TestEnrichConnectionThreatIntelSkipsNonPublicBogonHits(t *testing.T) {
	tenantID := uuid.New()
	mgr := threatintel.New(threatintel.Config{
		RefreshInterval: time.Hour,
		HTTPTimeout:     time.Second,
		Sources: []threatintel.Source{staticThreatSource{indicators: []threatintel.Indicator{
			{CIDR: "127.0.0.0/8", Feed: "firehol-level1", Score: 90},
			{CIDR: "172.16.0.0/12", Feed: "firehol-level1", Score: 90},
			{IP: "204.10.162.167", Feed: "abuseipdb", Score: 95},
		}}},
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)
	waitThreatIntelCurrent(t, mgr)

	srv := &Server{threatIntel: mgr}
	events := []IngestedEvent{{
		Type:     "conn.open",
		Severity: "info",
		SrcIP:    "127.0.0.1",
		DstIP:    "172.18.0.9",
		Details:  map[string]any{"direction": "outbound"},
	}}

	srv.enrichConnectionThreatIntel(tenantID, events)

	if events[0].ThreatFeed != "" || events[0].ThreatScore != 0 {
		t.Fatalf("non-public connection should not be threat-enriched: %+v", events[0])
	}

	events = []IngestedEvent{{
		Type:     "conn.open",
		Severity: "info",
		SrcIP:    "10.0.0.5",
		DstIP:    "204.10.162.167",
		Details:  map[string]any{"direction": "outbound"},
	}}
	srv.enrichConnectionThreatIntel(tenantID, events)
	if events[0].ThreatFeed != "abuseipdb" || events[0].ThreatScore != 95 {
		t.Fatalf("public connection threat enrichment missing: %+v", events[0])
	}
}

func TestIngestedEventContractV1DefaultsEventMetadata(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	ts := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	ev := IngestedEvent{
		Type:     "web.request",
		TS:       ts,
		SrcIP:    "203.0.113.10",
		BytesOut: 120,
		Details:  map[string]any{"status_code": float64(200), "path_template": "/login"},
	}
	if err := validateIngestedEventContract(&ev); err != nil {
		t.Fatalf("v1 event rejected: %v", err)
	}

	normalizeIngestedEventMetadata(&ev, tenantID, nodeID)
	if ev.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want v1 default", ev.SchemaVersion)
	}
	if ev.EventID == "" {
		t.Fatal("event_id was not generated")
	}

	ev2 := IngestedEvent{
		Type:     "web.request",
		TS:       ts,
		SrcIP:    "203.0.113.10",
		BytesOut: 120,
		Details:  map[string]any{"path_template": "/login", "status_code": float64(200)},
	}
	normalizeIngestedEventMetadata(&ev2, tenantID, nodeID)
	if ev2.EventID != ev.EventID {
		t.Fatalf("event_id not deterministic: %q != %q", ev2.EventID, ev.EventID)
	}

	row := eventToDorisRow(tenantID, nodeID, &ev)
	if row["schema_version"] != 1 || row["event_id"] != ev.EventID {
		t.Fatalf("events row missing v1 metadata: %+v", row)
	}
	webRow := eventToWebRequestRow(tenantID, nodeID, &ev)
	if webRow["schema_version"] != 1 || webRow["event_id"] != ev.EventID {
		t.Fatalf("web_requests row missing v1 metadata: %+v", webRow)
	}
}

func TestIngestedEventContractV2MetadataPreserved(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	ev := IngestedEvent{
		SchemaVersion: 2,
		EventID:       "evt_01JZV2metadata",
		Type:          "web.request",
		TS:            time.Date(2026, 5, 19, 12, 1, 0, 0, time.UTC),
		SrcIP:         "203.0.113.11",
		RawRef:        "journal://batch/row/2",
		Collector:     "nginx-access",
		Parser:        "nginx-combined",
		ParserStatus:  "parsed",
		Details:       map[string]any{"status_code": float64(204)},
	}
	normalizeIngestedEventMetadata(&ev, tenantID, nodeID)
	if ev.SchemaVersion != 2 || ev.EventID != "evt_01JZV2metadata" {
		t.Fatalf("v2 metadata was changed: %+v", ev)
	}

	row := eventToDorisRow(tenantID, nodeID, &ev)
	webRow := eventToWebRequestRow(tenantID, nodeID, &ev)
	for _, got := range []map[string]any{row, webRow} {
		if got["schema_version"] != 2 ||
			got["event_id"] != ev.EventID ||
			got["raw_ref"] != ev.RawRef ||
			got["collector"] != ev.Collector ||
			got["parser"] != ev.Parser ||
			got["parser_status"] != ev.ParserStatus {
			t.Fatalf("Doris row did not preserve v2 metadata: %+v", got)
		}
	}
}

func TestBuildDorisEventRowsKeepsTypedFactsOutOfGenericEvents(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	rows := buildDorisEventRows(tenantID, nodeID, []IngestedEvent{
		{
			Type:     "conn.summary",
			TS:       time.Date(2026, 5, 30, 7, 0, 0, 0, time.UTC),
			ConnID:   "conn-1",
			DedupKey: "conn.summary:conn-1",
		},
		{
			Type:     "web.request",
			TS:       time.Date(2026, 5, 30, 7, 1, 0, 0, time.UTC),
			SrcIP:    "203.0.113.20",
			DedupKey: "web.request:1",
			Details:  map[string]any{"status_code": float64(200)},
		},
		{
			Type:     "rule.trigger",
			TS:       time.Date(2026, 5, 30, 7, 2, 0, 0, time.UTC),
			Severity: "high",
			Message:  "suspicious activity",
		},
	})

	if len(rows.events) != 1 {
		t.Fatalf("generic events rows = %d, want only high-signal row", len(rows.events))
	}
	if got := rows.events[0]["event_type"]; got != "rule.trigger" {
		t.Fatalf("generic event type = %v, want rule.trigger", got)
	}
	if len(rows.conns) != 1 {
		t.Fatalf("connection fact rows = %d, want 1", len(rows.conns))
	}
	if len(rows.web) != 1 {
		t.Fatalf("web fact rows = %d, want 1", len(rows.web))
	}
}

func TestCoalesceDorisHotEventsGroupsRepeatedLogLinesInTwentyMinuteBucket(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	start := time.Date(2026, 5, 30, 9, 0, 0, 0, time.UTC)
	events := make([]IngestedEvent, 0, 1200)
	for i := 0; i < 1200; i++ {
		ts := start.Add(time.Duration(i) * time.Second)
		events = append(events, IngestedEvent{
			Type:     "log.line",
			TS:       ts,
			TenantID: tenantID.String(),
			NodeID:   nodeID.String(),
			Severity: "info",
			Message:  "same exact message",
			Details: map[string]any{
				"program": "nginx",
				"source":  "/var/log/nginx/access.log",
			},
			DedupKey: fmt.Sprintf("log.line:%d", i),
		})
	}

	got := coalesceDorisHotEvents(tenantID, nodeID, events)
	if len(got) != 1 {
		t.Fatalf("coalesced events = %d, want 1", len(got))
	}
	details := got[0].Details
	if details["coalesced"] != true {
		t.Fatalf("coalesced marker missing: %#v", details)
	}
	if details["coalesced_count"] != int64(1200) {
		t.Fatalf("coalesced_count = %#v, want 1200", details["coalesced_count"])
	}
	if got[0].TS != start.Add(1199*time.Second) {
		t.Fatalf("coalesced timestamp = %s, want last seen", got[0].TS)
	}
	if !strings.HasPrefix(got[0].DedupKey, "hot.coalesced:log_line:") || got[0].CorrelationID != got[0].DedupKey {
		t.Fatalf("coalesced keys not deterministic: %+v", got[0])
	}
	samples, ok := details["sample_timestamps"].([]string)
	if !ok || len(samples) != dorisHotCoalesceSampleLimit {
		t.Fatalf("sample_timestamps = %#v, want capped samples", details["sample_timestamps"])
	}
}

func TestCoalesceDorisHotEventsKeepsDifferentBucketsSeparate(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	start := time.Date(2026, 5, 30, 9, 19, 59, 0, time.UTC)
	events := []IngestedEvent{
		{Type: "log.line", TS: start, NodeID: nodeID.String(), Severity: "info", Message: "same exact message", Details: map[string]any{"program": "nginx"}},
		{Type: "log.line", TS: start.Add(2 * time.Second), NodeID: nodeID.String(), Severity: "info", Message: "same exact message", Details: map[string]any{"program": "nginx"}},
	}

	got := coalesceDorisHotEvents(uuid.New(), nodeID, events)
	if len(got) != 2 {
		t.Fatalf("coalesced events = %d, want bucket boundary to keep 2", len(got))
	}
}

func TestHandleEventsIngestReturnsReplayReceiptForDuplicateKey(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: tenantID}}}
	srv := &Server{store: store, logger: zap.NewNop()}
	body := gzipNDJSON(t, IngestedEvent{
		Type:    "web.error",
		TS:      time.Now().UTC().Add(-time.Minute),
		Message: "parser miss",
	})

	post := func() map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/events/ingest", bytes.NewReader(body))
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Content-Type", "application/x-ndjson")
		req.Header.Set("X-ControlOne-Replay-Key", "eventstream:test-replay-key")
		req = withPrincipal(req, &auth.Principal{
			Type:    "agent",
			Name:    nodeID.String(),
			Subject: nodeID.String(),
			Roles:   []string{"agent"},
		})
		rec := httptest.NewRecorder()
		srv.handleEventsIngest(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("event ingest status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	first := post()
	second := post()
	if first["batch_id"] == "" || second["batch_id"] != first["batch_id"] {
		t.Fatalf("duplicate did not return original batch id: first=%v second=%v", first, second)
	}
	if second["duplicate"] != true {
		t.Fatalf("duplicate response missing receipt marker: %v", second)
	}
	if len(store.eventIngestReplayByKey) != 1 {
		t.Fatalf("event ingest replay records = %d, want 1", len(store.eventIngestReplayByKey))
	}
}

func TestDrainEventIngestBatchReplaysGzipNDJSON(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	batchID := uuid.New()
	payload := gzipNDJSON(t,
		IngestedEvent{
			Type:    "web.request",
			TS:      time.Date(2026, 5, 19, 12, 2, 0, 0, time.UTC),
			SrcIP:   "203.0.113.12",
			Details: map[string]any{"status_code": float64(200)},
		},
		IngestedEvent{
			SchemaVersion: 2,
			EventID:       "evt-replay-preserved",
			Type:          "web.error",
			TS:            time.Date(2026, 5, 19, 12, 3, 0, 0, time.UTC),
			Message:       "parser miss",
			RawRef:        "journal://batch/row/2",
			Collector:     "nginx-access",
			Parser:        "nginx-combined",
			ParserStatus:  "error",
		},
	)

	marker := &replayMarker{}
	var replayed []IngestedEvent
	err := drainEventIngestBatch(context.Background(), storage.EventIngestBatch{
		ID:       batchID,
		TenantID: uuid.NullUUID{UUID: tenantID, Valid: true},
		NodeID:   uuid.NullUUID{UUID: nodeID, Valid: true},
		Payload:  payload,
	}, marker, func(_ context.Context, gotTenantID, gotNodeID uuid.UUID, events []IngestedEvent) (string, error) {
		if gotTenantID != tenantID || gotNodeID != nodeID {
			t.Fatalf("fanout scope = %s/%s, want %s/%s", gotTenantID, gotNodeID, tenantID, nodeID)
		}
		replayed = append([]IngestedEvent(nil), events...)
		return "queued", nil
	})
	if err != nil {
		t.Fatalf("drain failed: %v", err)
	}
	if len(replayed) != 2 {
		t.Fatalf("replayed %d events, want 2", len(replayed))
	}
	if replayed[0].SchemaVersion != 1 || replayed[0].EventID == "" {
		t.Fatalf("v1 replay metadata missing: %+v", replayed[0])
	}
	if replayed[1].SchemaVersion != 2 || replayed[1].EventID != "evt-replay-preserved" || replayed[1].ParserStatus != "error" {
		t.Fatalf("v2 replay metadata not preserved: %+v", replayed[1])
	}
	if len(marker.calls) != 1 || marker.calls[0].status != "accepted" || marker.calls[0].dorisStatus != "queued" || marker.calls[0].errMsg != "" {
		t.Fatalf("unexpected mark calls: %+v", marker.calls)
	}
}

func TestDrainEventIngestBatchMarksDecodeFailure(t *testing.T) {
	t.Parallel()

	marker := &replayMarker{}
	fanoutCalled := false
	err := drainEventIngestBatch(context.Background(), storage.EventIngestBatch{
		ID:       uuid.New(),
		TenantID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		Payload:  gzipRaw(t, []byte(`{"type":"not.allowed"}`+"\n")),
	}, marker, func(context.Context, uuid.UUID, uuid.UUID, []IngestedEvent) (string, error) {
		fanoutCalled = true
		return "", errors.New("should not fan out")
	})
	if err == nil {
		t.Fatal("decode failure returned nil")
	}
	if fanoutCalled {
		t.Fatal("fanout called after decode failure")
	}
	if len(marker.calls) != 1 || marker.calls[0].status != "failed" || !strings.Contains(marker.calls[0].errMsg, "not.allowed") {
		t.Fatalf("unexpected mark calls: %+v", marker.calls)
	}
}

func TestDrainEventIngestBatchDorisOnlyDoesNotReplayLocalFanout(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	batchID := uuid.New()
	payload, err := encodeIngestedEventPayload([]IngestedEvent{{
		SchemaVersion: 2,
		EventID:       "evt-local-complete",
		Type:          "web.request",
		TS:            time.Date(2026, 5, 19, 12, 4, 0, 0, time.UTC),
		SrcIP:         "203.0.113.20",
		Details:       map[string]any{"status_code": float64(200)},
	}})
	if err != nil {
		t.Fatalf("encode normalized payload: %v", err)
	}

	marker := &replayMarker{}
	var flushed []IngestedEvent
	err = drainEventIngestBatchDorisOnly(context.Background(), storage.EventIngestBatch{
		ID:       batchID,
		TenantID: uuid.NullUUID{UUID: tenantID, Valid: true},
		NodeID:   uuid.NullUUID{UUID: nodeID, Valid: true},
		Status:   "pending_doris",
		Payload:  payload,
	}, marker, func(_ context.Context, gotTenantID, gotNodeID uuid.UUID, events []IngestedEvent) (string, error) {
		if gotTenantID != tenantID || gotNodeID != nodeID {
			t.Fatalf("flush scope = %s/%s, want %s/%s", gotTenantID, gotNodeID, tenantID, nodeID)
		}
		flushed = append([]IngestedEvent(nil), events...)
		return "loaded", nil
	})
	if err != nil {
		t.Fatalf("doris-only drain failed: %v", err)
	}
	if len(flushed) != 1 || flushed[0].EventID != "evt-local-complete" {
		t.Fatalf("unexpected flushed events: %+v", flushed)
	}
	if len(marker.calls) != 1 || marker.calls[0].status != "accepted" || marker.calls[0].dorisStatus != "loaded" {
		t.Fatalf("unexpected mark calls: %+v", marker.calls)
	}
}

type replayMarkCall struct {
	id          uuid.UUID
	status      string
	dorisStatus string
	errMsg      string
}

type replayMarker struct {
	calls []replayMarkCall
}

func (m *replayMarker) MarkEventIngestStatus(_ context.Context, id uuid.UUID, status, dorisStatus, errMsg string) error {
	m.calls = append(m.calls, replayMarkCall{id: id, status: status, dorisStatus: dorisStatus, errMsg: errMsg})
	return nil
}

func gzipNDJSON(t *testing.T, events ...IngestedEvent) []byte {
	t.Helper()

	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
	return gzipRaw(t, raw.Bytes())
}

func gzipRaw(t *testing.T, raw []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}
