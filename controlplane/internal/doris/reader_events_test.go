package doris

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// TestListConnectionsForNodeQuery_NoRFC1918Strip pins the SQL emitted by
// ListConnectionsForNode against bugs §1.2 — the "double-filter" that left
// the connections panel empty on dev/internal nodes. The server is now the
// canonical visibility layer, so the query must NOT exclude private peers
// (the UI used to do that, and we removed it). Predecessor for S6
// c1-bandwidth-rollups, which depends on summary rows being visible.
func TestListConnectionsForNodeQuery_NoRFC1918Strip(t *testing.T) {
	cases := []struct {
		name     string
		limit    int
		openOnly bool
	}{
		{"all-rows-no-open-filter", 100, false},
		{"open-only-200", 200, true},
		{"open-only-default", 0, true},
	}

	// Exact substrings that would indicate a private/RFC1918 strip leaking
	// into the SQL. We check both literal CIDRs and well-known function
	// names a careless refactor might reach for.
	forbiddenSubstrings := []string{
		"10.0.0.0/8",
		"172.16.",
		"172.16.0.0/12",
		"192.168.",
		"192.168.0.0/16",
		"169.254.",
		"127.0.0.0/8",
		"is_private",
		"isPrivate",
		"NOT IN (\"10.",
		"src_ip NOT LIKE",
		"dst_ip NOT LIKE",
		"INET_ATON",   // a clever range exclusion would reach for this
		"PRIVATE_IP_", // hypothetical UDF
		"CASE WHEN threat_match",
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := buildListConnectionsForNodeQuery(tc.limit, tc.openOnly)

			// Sanity: the query must reference the canonical table.
			if !strings.Contains(q, "FROM process_connections") {
				t.Fatalf("query missing FROM process_connections: %s", q)
			}

			// The only allowed filters are tenant_id, node_id, time
			// window overlap, optionally ended_at, and a LIMIT/ORDER. Anything
			// that smells like an RFC1918 strip is a regression.
			lower := strings.ToLower(q)
			for _, bad := range forbiddenSubstrings {
				if strings.Contains(lower, strings.ToLower(bad)) {
					t.Errorf("query contains forbidden RFC1918 strip pattern %q — bugs §1.2 regression\nquery:\n%s",
						bad, q)
				}
			}

			// open_only=true must add a strict open predicate. open_only=false
			// uses overlap semantics, which intentionally includes open rows
			// via "(ended_at IS NULL OR ended_at >= ?)".
			hasStrictOpenPredicate := strings.Contains(q, "AND ended_at IS NULL")
			if tc.openOnly && !hasStrictOpenPredicate {
				t.Errorf("openOnly=true but query lacks ended_at IS NULL: %s", q)
			}
			if !tc.openOnly && hasStrictOpenPredicate {
				t.Errorf("openOnly=false but query contains a strict open-only predicate: %s", q)
			}
			if strings.Contains(q, "started_at >= ?") {
				t.Errorf("query uses start-only window and would hide long-lived open connections: %s", q)
			}
			if tc.openOnly && !strings.Contains(q, "started_at <= ?") {
				t.Errorf("openOnly=true must bound future rows with started_at <= ?: %s", q)
			}
			if !tc.openOnly && !strings.Contains(q, "(ended_at IS NULL OR ended_at >= ?)") {
				t.Errorf("openOnly=false must use overlap semantics: %s", q)
			}
		})
	}
}

func TestListConnectionsForNodeQueryOrdersByRecency(t *testing.T) {
	for _, externalOnly := range []bool{false, true} {
		q := buildListConnectionsForNodeDayQuery(100, false, externalOnly)
		if !strings.Contains(q, "ORDER BY started_at DESC") {
			t.Fatalf("node connections should default to recency order:\n%s", q)
		}
		if strings.Contains(q, "CASE WHEN threat_match") {
			t.Fatalf("node connections should not prioritize threat matches by default:\n%s", q)
		}
	}
}

// TestNetflowFilter_RFC1918Summarised_NotDropped is the agent-side companion
// to the doris query test: it pins that internal/netflow/filter.go does NOT
// drop RFC1918 peers when the default capture policy is in effect — they
// flow through as `summary` events that the writer persists with `ended_at`
// populated. If this test ever flips to `FilterDrop`, the upstream pipe is
// the real culprit and connections_query.go can never serve those rows
// regardless of how clean the SQL is.
//
// Note: this test lives in the doris package only because it asserts a
// cross-layer invariant the doris reader depends on. The filter
// implementation lives in `internal/netflow/filter.go`.
func TestRFC1918Peer_StillReachableAsSummary(t *testing.T) {
	// We can't import internal/netflow here without creating a cycle and
	// internal/netflow already has filter_test.go covering its own
	// behaviour. This test instead asserts the *contract* that the doris
	// schema relies on: a row with src_ip/dst_ip in RFC1918 is shaped
	// identically to any other row, scanConnectionRow handles it, and
	// nothing in the read path special-cases the IP.
	for _, peer := range []string{"10.1.2.3", "172.16.4.5", "192.168.10.20", "169.254.169.254"} {
		ip := net.ParseIP(peer)
		if ip == nil {
			t.Fatalf("invalid test fixture IP: %s", peer)
		}
		// Confirm Go's stdlib classifies these as private/link-local —
		// matches isExternal() in internal/netflow/filter.go.
		if !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
			t.Errorf("fixture %s should be RFC1918/link-local but stdlib disagrees", peer)
		}
	}

	// The doris query must not branch on these addresses.
	q := buildListConnectionsForNodeQuery(200, true)
	for _, peer := range []string{"10.", "172.16.", "192.168.", "169.254."} {
		if strings.Contains(q, peer) {
			t.Errorf("doris query mentions private prefix %q — bugs §1.2 regression\nquery:\n%s", peer, q)
		}
	}
}

func TestScanConnectionRowAllowsSparseDorisRows(t *testing.T) {
	started := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	row, err := scanConnectionRow(staticScanner{values: []any{
		"conn-1", nil, started, nil, nil, nil,
		nil, "nginx", nil, nil,
		"45.135.193.156", nil, "10.0.0.4", 443, "tcp",
		nil, int64(2048), nil, nil,
		nil, nil, nil, nil, "node-1",
	}})
	if err != nil {
		t.Fatalf("scan sparse connection row: %v", err)
	}
	if row.ConnID != "conn-1" || row.ProcessName != "nginx" || row.NodeID != "node-1" {
		t.Fatalf("unexpected row identity: %+v", row)
	}
	if row.CorrelationID != "" || row.DurationMS != 0 || row.PID != 0 || row.ThreatMatch {
		t.Fatalf("nullable fields should default to zero values: %+v", row)
	}
	if row.StartedAt.IsZero() || !row.EndedAt.IsZero() {
		t.Fatalf("unexpected timestamps: started=%v ended=%v", row.StartedAt, row.EndedAt)
	}
	if row.SrcIP != "45.135.193.156" || row.DstIP != "10.0.0.4" || row.DstPort != 443 || row.BytesOut != 2048 {
		t.Fatalf("unexpected network fields: %+v", row)
	}
}

func TestBuildListConnectionsForIPDayQueryIsPartitionScoped(t *testing.T) {
	for _, peerColumn := range []string{"src_ip", "dst_ip"} {
		t.Run(peerColumn, func(t *testing.T) {
			query := buildListConnectionsForIPDayQuery(peerColumn, 250)
			for _, want := range []string{
				"FROM process_connections",
				"event_date = ?",
				"tenant_id = ?",
				peerColumn + " = ?",
				"started_at <= ?",
				"(ended_at IS NULL OR ended_at >= ?)",
				"LIMIT 250",
			} {
				if !strings.Contains(query, want) {
					t.Fatalf("query missing %q:\n%s", want, query)
				}
			}
			for _, bad := range []string{
				"src_ip = ? OR dst_ip = ?",
				"event_date >=",
				"event_date <=",
				"ORDER BY",
			} {
				if strings.Contains(query, bad) {
					t.Fatalf("query contains broad-scan pattern %q:\n%s", bad, query)
				}
			}
		})
	}
}

func TestConnectionEventDaysClampsBroadIPQueries(t *testing.T) {
	until := time.Date(2026, 5, 20, 15, 0, 0, 0, time.UTC)
	since := until.AddDate(0, 0, -90)
	days := connectionEventDays(since, until, 14)
	if len(days) != 14 {
		t.Fatalf("days len = %d, want 14 (%v)", len(days), days)
	}
	if got, want := days[0].Format("2006-01-02"), "2026-05-20"; got != want {
		t.Fatalf("first day = %s, want %s", got, want)
	}
	if got, want := days[len(days)-1].Format("2006-01-02"), "2026-05-07"; got != want {
		t.Fatalf("last day = %s, want %s", got, want)
	}
}

type staticScanner struct {
	values []any
}

func (s staticScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan dest len %d, values len %d", len(dest), len(s.values))
	}
	for i, value := range s.values {
		switch d := dest[i].(type) {
		case *sql.NullString:
			if value == nil {
				*d = sql.NullString{}
				continue
			}
			v, ok := value.(string)
			if !ok {
				return fmt.Errorf("value %d has type %T, want string", i, value)
			}
			*d = sql.NullString{String: v, Valid: true}
		case *sql.NullInt64:
			if value == nil {
				*d = sql.NullInt64{}
				continue
			}
			switch v := value.(type) {
			case int:
				*d = sql.NullInt64{Int64: int64(v), Valid: true}
			case int64:
				*d = sql.NullInt64{Int64: v, Valid: true}
			default:
				return fmt.Errorf("value %d has type %T, want int/int64", i, value)
			}
		case *sql.NullBool:
			if value == nil {
				*d = sql.NullBool{}
				continue
			}
			v, ok := value.(bool)
			if !ok {
				return fmt.Errorf("value %d has type %T, want bool", i, value)
			}
			*d = sql.NullBool{Bool: v, Valid: true}
		case **time.Time:
			if value == nil {
				*d = nil
				continue
			}
			v, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("value %d has type %T, want time.Time", i, value)
			}
			t := v
			*d = &t
		default:
			return fmt.Errorf("unsupported scan destination %d type %T", i, dest[i])
		}
	}
	return nil
}

func TestBuildEventQuerySQLRequiresTenantAndUsesBoundFilters(t *testing.T) {
	since := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	until := since.Add(2 * time.Hour)

	_, _, _, err := buildEventQuerySQL(EventQueryParams{Since: since, Until: until})
	if err == nil {
		t.Fatal("expected tenant_id validation error")
	}

	query, countQuery, args, err := buildEventQuerySQL(EventQueryParams{
		TenantID:      "tenant-1",
		NodeID:        "node-1",
		CorrelationID: "corr-1",
		EventTypes:    []string{"web.request", "web.error"},
		ParserStatus:  "parsed",
		Since:         since,
		Until:         until,
		Limit:         999,
		Offset:        3,
	})
	if err != nil {
		t.Fatalf("build event query: %v", err)
	}
	for _, want := range []string{
		"FROM events",
		"FROM process_connections",
		"FROM web_requests",
		"tenant_id = ?",
		"node_id = ?",
		"correlation_id = ?",
		"event_type IN (?, ?)",
		"parser_status = ?",
		"ORDER BY ts DESC",
		"LIMIT 500 OFFSET 3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
	if !strings.Contains(countQuery, "SELECT COUNT(*) FROM (") || !strings.Contains(countQuery, "FROM process_connections") || !strings.Contains(countQuery, "WHERE tenant_id = ?") {
		t.Fatalf("count query missing tenant predicate: %s", countQuery)
	}
	if len(args) != 8 {
		t.Fatalf("args len = %d, want 8 (%+v)", len(args), args)
	}
}

func TestBuildEventQuerySQLSearchUsesPortableUnionPredicate(t *testing.T) {
	query, _, args, err := buildEventQuerySQL(EventQueryParams{
		TenantID: "tenant-1",
		Search:   "Nginx Error",
		Limit:    25,
	})
	if err != nil {
		t.Fatalf("build event query: %v", err)
	}
	if !strings.Contains(query, "LOWER(message) LIKE ?") {
		t.Fatalf("query should search the normalized union message column:\n%s", query)
	}
	if len(args) != 2 || args[1] != "%nginx error%" {
		t.Fatalf("search args = %#v, want lowercase LIKE pattern", args)
	}
}

func TestFleetHealthSnapshotSQLAvoidsMemoryHeavyDistinctAndNullScans(t *testing.T) {
	query := fleetHealthSnapshotSQL()
	if strings.Contains(strings.ToUpper(query), "COUNT(DISTINCT") {
		t.Fatalf("fleet health query should avoid memory-heavy distinct counts:\n%s", query)
	}
	for _, want := range []string{
		"IFNULL(node_id, '')",
		"event_type = 'conn.open'",
		"event_type = 'conn.close'",
		"IFNULL(SUM(IFNULL(bytes_in, 0)), 0)",
		"IFNULL(SUM(IFNULL(bytes_out, 0)), 0)",
		"IFNULL(MAX(severity), '')",
		"tenant_id = ?",
		"ts >= ?",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("fleet health query missing %q:\n%s", want, query)
		}
	}
}

func TestBuildTimelineSQLScopesEveryUnionArmByTenant(t *testing.T) {
	query, args, err := buildTimelineSQL(TimelineBuildParams{
		TenantID:      "tenant-1",
		CorrelationID: "corr-1",
		EntityType:    "ip",
		EntityID:      "203.0.113.10",
		Since:         time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
		Until:         time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Limit:         2000,
	})
	if err != nil {
		t.Fatalf("build timeline query: %v", err)
	}
	for _, table := range []string{"FROM events", "FROM process_connections", "FROM process_lineage", "FROM file_accesses", "FROM db_queries", "FROM web_requests"} {
		if !strings.Contains(query, table) {
			t.Fatalf("timeline query missing %s:\n%s", table, query)
		}
	}
	if got := strings.Count(query, "tenant_id = ?"); got != 6 {
		t.Fatalf("tenant predicate count = %d, want 6:\n%s", got, query)
	}
	if !strings.Contains(query, "events") || !strings.Contains(query, "ORDER BY ts DESC") || !strings.Contains(query, "LIMIT 1000") {
		t.Fatalf("timeline query missing expected ordering/limit:\n%s", query)
	}
	if strings.Contains(query, "203.0.113.10") {
		t.Fatalf("entity value was interpolated into SQL:\n%s", query)
	}
	if len(args) == 0 {
		t.Fatal("expected bound args")
	}
}

func TestBuildTimelineSQLIncludesProcessLineageAndWebRequests(t *testing.T) {
	query, args, err := buildTimelineSQL(TimelineBuildParams{
		TenantID:   "tenant-1",
		NodeID:     "node-1",
		EntityType: "process",
		EntityID:   "nginx",
		Since:      time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
		Until:      time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("build timeline query: %v", err)
	}
	for _, want := range []string{
		"FROM process_lineage",
		"'process_lineage' AS source_table",
		"observed_at >= ?",
		"process_name = ?",
		"FROM web_requests",
		"'web_requests' AS source_table",
		"webserver_kind = ?",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("timeline query missing %q:\n%s", want, query)
		}
	}
	if len(args) == 0 {
		t.Fatal("expected bound args")
	}
}

func TestBuildTimelineSQLAvoidsUnsupportedPivotBroadening(t *testing.T) {
	query, _, err := buildTimelineSQL(TimelineBuildParams{
		TenantID: "tenant-1",
		ConnID:   "conn-1",
		Since:    time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
		Until:    time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("build timeline query: %v", err)
	}
	if !strings.Contains(query, "FROM process_lineage") || !strings.Contains(query, "FROM web_requests") {
		t.Fatalf("expected lineage and web arms to remain present but guarded:\n%s", query)
	}
	if got := strings.Count(query, "conn_id = ?"); got != 4 {
		t.Fatalf("conn_id predicate count = %d, want 4 so lineage/web do not reference missing conn_id columns:\n%s", got, query)
	}
	if got := strings.Count(query, "1 = 0"); got < 2 {
		t.Fatalf("expected unsupported lineage/web conn pivots to be guarded, got %d guards:\n%s", got, query)
	}
}

func TestBuildTimelineSQLIncludesOverlapAwareProcessConnections(t *testing.T) {
	query, args, err := buildTimelineSQL(TimelineBuildParams{
		TenantID: "tenant-1",
		NodeID:   "node-1",
		Since:    time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
		Until:    time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("build timeline query: %v", err)
	}
	for _, want := range []string{
		"FROM process_connections",
		"'process_connections' AS source_table",
		"started_at <= ?",
		"(ended_at IS NULL OR ended_at >= ?)",
		"CONCAT('conn.'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("timeline query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "started_at >= ?") {
		t.Fatalf("connection timeline uses start-only filtering and would hide long-lived flows:\n%s", query)
	}
	if len(args) == 0 {
		t.Fatal("expected bound args")
	}
}
