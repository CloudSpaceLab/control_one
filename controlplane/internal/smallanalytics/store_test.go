package smallanalytics

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
)

func TestStorePersistsConnectionRowsAndTopTalkers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	rows := []map[string]any{
		connRow("tenant-1", "node-1", "conn-1", base, time.Time{}, "outbound", "10.0.0.5", "8.8.8.8", 0, 0, ""),
		connRow("tenant-1", "node-1", "conn-1", base, base.Add(2*time.Minute), "outbound", "10.0.0.5", "8.8.8.8", 100, 250, "abuseipdb"),
		connRow("tenant-1", "node-1", "conn-2", base.Add(time.Minute), base.Add(3*time.Minute), "outbound", "10.0.0.5", "10.10.10.10", 50, 50, ""),
	}
	if err := store.AppendConnectionRows(ctx, rows); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	list, err := store.ListConnectionsForTenant(ctx, "tenant-1", base.Add(-time.Hour), base.Add(time.Hour), 10, true)
	if err != nil {
		t.Fatalf("list tenant: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("external connection rows = %d, want 1: %#v", len(list), list)
	}
	if list[0].ConnID != "conn-1" || list[0].BytesOut != 250 || list[0].ThreatFeed != "abuseipdb" || list[0].EndedAt.IsZero() {
		t.Fatalf("connection row did not merge lifetime fields: %#v", list[0])
	}

	talkers, err := store.TopTalkers(ctx, "tenant-1", base.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("top talkers: %v", err)
	}
	if len(talkers) != 1 || talkers[0].IP != "8.8.8.8" || talkers[0].Connections != 1 || talkers[0].ThreatHits != 1 {
		t.Fatalf("unexpected top talkers: %#v", talkers)
	}

	detail, err := store.ConnectionLifetime(ctx, "tenant-1", "conn-1")
	if err != nil {
		t.Fatalf("connection lifetime: %v", err)
	}
	if detail == nil || detail.ConnID != "conn-1" || detail.BytesIn != 100 {
		t.Fatalf("unexpected connection detail: %#v", detail)
	}
}

func TestStoreProjectsConnectionRowsToEventsAndTimeline(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)
	if err := store.AppendConnectionRows(ctx, []map[string]any{
		connRow("tenant-1", "node-1", "conn-1", base, base.Add(90*time.Second), "outbound", "10.0.0.5", "8.8.8.8", 100, 250, "abuseipdb"),
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}

	events, total, err := store.QueryEvents(ctx, doris.EventQueryParams{
		TenantID:   "tenant-1",
		EventTypes: []string{"conn.open", "conn.close"},
		Since:      base.Add(-time.Minute),
		Until:      base.Add(2 * time.Minute),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if total != 2 || len(events) != 2 {
		t.Fatalf("events total=%d len=%d rows=%+v", total, len(events), events)
	}
	if events[0].EventType != "conn.close" || events[0].ConnID != "conn-1" || events[0].ThreatScore != 100 {
		t.Fatalf("unexpected newest event: %+v", events[0])
	}
	if events[0].Collector != "small-analytics" || events[0].Parser != "process_connections" || events[0].DetailsJSON == "" {
		t.Fatalf("event did not preserve citation details: %+v", events[0])
	}

	directionEvents, total, err := store.QueryEvents(ctx, doris.EventQueryParams{
		TenantID:   "tenant-1",
		EventTypes: []string{"conn.outbound"},
		Since:      base.Add(-time.Minute),
		Until:      base.Add(2 * time.Minute),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("query direction events: %v", err)
	}
	if total != 2 || len(directionEvents) != 2 {
		t.Fatalf("direction alias should match the connection events, total=%d rows=%+v", total, directionEvents)
	}

	timeline, err := store.BuildTimeline(ctx, doris.TimelineBuildParams{
		TenantID:   "tenant-1",
		EntityType: "ip",
		EntityID:   "8.8.8.8",
		Since:      base.Add(-time.Minute),
		Until:      base.Add(2 * time.Minute),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("build timeline: %v", err)
	}
	if len(timeline) != 2 || timeline[0].SourceTable != "process_connections" || timeline[0].EventType != "conn.close" {
		t.Fatalf("unexpected timeline: %+v", timeline)
	}
}

func TestStoreConfiguresPooledConnectionsForBusyTimeout(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, Config{Dir: t.TempDir(), QueryTimeout: 2500 * time.Millisecond})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	openConns := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, conn := range openConns {
			_ = conn.Close()
		}
	}()
	var timeouts [4]int
	for i := range timeouts {
		conn, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		openConns = append(openConns, conn)
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeouts[i]); err != nil {
			t.Fatalf("busy_timeout conn %d: %v", i, err)
		}
		if timeouts[i] < 2500 {
			t.Fatalf("conn %d busy_timeout = %dms, want at least 2500ms", i, timeouts[i])
		}
	}
}

func TestStoreSerializesConcurrentConnectionAppends(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	const writers = 24
	const rowsPerWriter = 20
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < rowsPerWriter; j++ {
				connID := fmt.Sprintf("conn-%02d-%02d", i, j)
				row := connRow("tenant-1", "node-1", connID, base.Add(time.Duration(j)*time.Second), time.Time{}, "outbound", "10.0.0.5", "8.8.8.8", int64(j), int64(i+j), "")
				if err := store.AppendConnectionRows(ctx, []map[string]any{row}); err != nil {
					errs <- fmt.Errorf("writer %d row %d: %w", i, j, err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	list, err := store.ListConnectionsForTenant(ctx, "tenant-1", base.Add(-time.Hour), base.Add(time.Hour), writers*rowsPerWriter, false)
	if err != nil {
		t.Fatalf("list tenant: %v", err)
	}
	if len(list) != writers*rowsPerWriter {
		t.Fatalf("connection rows = %d, want %d", len(list), writers*rowsPerWriter)
	}
}

func connRow(tenantID, nodeID, connID string, startedAt, endedAt time.Time, direction, srcIP, dstIP string, bytesIn, bytesOut int64, threatFeed string) map[string]any {
	row := map[string]any{
		"tenant_id":      tenantID,
		"node_id":        nodeID,
		"conn_id":        connID,
		"correlation_id": connID + "-corr",
		"started_at":     startedAt.UTC().Format("2006-01-02 15:04:05.000"),
		"duration_ms":    int64(120000),
		"direction":      direction,
		"pid":            int64(4242),
		"process_name":   "curl",
		"user_name":      "svc",
		"src_ip":         srcIP,
		"src_port":       51515,
		"dst_ip":         dstIP,
		"dst_port":       443,
		"protocol":       "tcp",
		"bytes_in":       bytesIn,
		"bytes_out":      bytesOut,
		"threat_match":   threatFeed != "",
		"threat_feed":    threatFeed,
	}
	if !endedAt.IsZero() {
		row["ended_at"] = endedAt.UTC().Format("2006-01-02 15:04:05.000")
	}
	return row
}
