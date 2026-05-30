package doris

import (
	"os"
	"strings"
	"testing"
)

func TestSplitStatementsBasic(t *testing.T) {
	in := "CREATE TABLE x (a INT);\nINSERT INTO x VALUES (1);\n"
	got := splitStatements(in)
	if len(got) != 2 {
		t.Fatalf("want 2 statements, got %d: %#v", len(got), got)
	}
}

func TestSplitStatementsHonoursQuotes(t *testing.T) {
	in := `INSERT INTO x VALUES ('a;b'); SELECT 1;`
	got := splitStatements(in)
	if len(got) != 2 {
		t.Fatalf("quotes: want 2, got %d: %#v", len(got), got)
	}
}

func TestSplitStatementsHonoursLineComments(t *testing.T) {
	in := "-- comment with ; inside\nSELECT 1;"
	got := splitStatements(in)
	if len(got) != 1 {
		t.Fatalf("line-comment: want 1, got %d: %#v", len(got), got)
	}
}

func TestSplitStatementsHonoursBlockComments(t *testing.T) {
	in := "/* block ; comment */ SELECT 1;"
	got := splitStatements(in)
	if len(got) != 1 {
		t.Fatalf("block-comment: want 1, got %d: %#v", len(got), got)
	}
}

func TestParsePositiveInt(t *testing.T) {
	if _, err := parsePositiveInt("0001"); err != nil {
		t.Fatalf("0001 should parse: %v", err)
	}
	if _, err := parsePositiveInt("0"); err == nil {
		t.Fatal("0 should reject")
	}
	if _, err := parsePositiveInt("1a"); err == nil {
		t.Fatal("1a should reject")
	}
}

func TestRenderMigrationSQLOverridesReplicationNum(t *testing.T) {
	in := `PROPERTIES ("replication_num" = "1");`
	got := renderMigrationSQL(in, MigrationOptions{ReplicationNum: 3})
	if got != `PROPERTIES ("replication_num" = "3");` {
		t.Fatalf("rendered SQL = %q", got)
	}
	if got := renderMigrationSQL(in, MigrationOptions{}); got != in {
		t.Fatalf("default render should preserve dev replication, got %q", got)
	}
}

func TestRenderMigrationSQLRewritesBucketsForSingleNode(t *testing.T) {
	in := `DISTRIBUTED BY HASH (tenant_id) BUCKETS 16
PROPERTIES (
  "replication_num" = "1",
  "dynamic_partition.start" = "-90",
  "dynamic_partition.buckets" = "16"
);`
	got := renderMigrationSQL(in, MigrationOptions{})
	if !strings.Contains(got, "BUCKETS 1") {
		t.Fatalf("single-node render should use BUCKETS 1, got:\n%s", got)
	}
	if strings.Contains(got, "BUCKETS 16") {
		t.Fatalf("single-node render retained large bucket count:\n%s", got)
	}
	if !strings.Contains(got, `"dynamic_partition.buckets" = "1"`) {
		t.Fatalf("single-node render should use dynamic_partition.buckets 1, got:\n%s", got)
	}
	if strings.Contains(got, `"dynamic_partition.buckets" = "16"`) {
		t.Fatalf("single-node render retained large dynamic partition bucket count:\n%s", got)
	}
	if !strings.Contains(got, `"dynamic_partition.start" = "-30"`) {
		t.Fatalf("single-node render should cap hot Doris history to 30 days, got:\n%s", got)
	}
	if !strings.Contains(got, `"dynamic_partition.create_history_partition" = "true"`) {
		t.Fatalf("single-node render should create bounded history partitions, got:\n%s", got)
	}
}

func TestRenderMigrationSQLPreservesBucketsForHA(t *testing.T) {
	in := `DISTRIBUTED BY HASH (tenant_id) BUCKETS 16
PROPERTIES (
  "replication_num" = "1",
  "dynamic_partition.start" = "-90",
  "dynamic_partition.buckets" = "16"
);`
	got := renderMigrationSQL(in, MigrationOptions{ReplicationNum: 3})
	if !strings.Contains(got, "BUCKETS 16") {
		t.Fatalf("HA render should preserve production bucket count, got:\n%s", got)
	}
	if !strings.Contains(got, `"dynamic_partition.buckets" = "16"`) {
		t.Fatalf("HA render should preserve dynamic partition bucket count, got:\n%s", got)
	}
	if !strings.Contains(got, `"dynamic_partition.start" = "-90"`) {
		t.Fatalf("HA render should preserve dynamic partition retention, got:\n%s", got)
	}
	if strings.Contains(got, `"dynamic_partition.create_history_partition"`) {
		t.Fatalf("HA render should not add demo history partition settings, got:\n%s", got)
	}
	if !strings.Contains(got, `"replication_num" = "3"`) {
		t.Fatalf("HA render should still rewrite replication, got:\n%s", got)
	}
}

func TestRewriteAddColumnIfNotExists(t *testing.T) {
	table, column, rewritten, ok := rewriteAddColumnIfNotExists(
		"ALTER TABLE events ADD COLUMN IF NOT EXISTS schema_version SMALLINT AFTER ts",
	)
	if !ok {
		t.Fatal("expected migration helper to recognize ADD COLUMN IF NOT EXISTS")
	}
	if table != "events" || column != "schema_version" {
		t.Fatalf("unexpected target: table=%q column=%q", table, column)
	}
	want := "ALTER TABLE events ADD COLUMN schema_version SMALLINT AFTER ts"
	if rewritten != want {
		t.Fatalf("rewritten statement mismatch\nwant: %s\n got: %s", want, rewritten)
	}
}

func TestRewriteAddColumnIfNotExistsIgnoresOtherStatements(t *testing.T) {
	_, _, _, ok := rewriteAddColumnIfNotExists("CREATE TABLE events (tenant_id VARCHAR(36))")
	if ok {
		t.Fatal("unexpected rewrite for non-ALTER statement")
	}
}

func TestEventsPipelineMigrationDoesNotBuildInlineRollup(t *testing.T) {
	raw, err := os.ReadFile("migrations/0001_events_pipeline.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	if strings.Contains(strings.ToUpper(sql), "CREATE MATERIALIZED VIEW") {
		t.Fatalf("bootstrap migration must not build materialized views inline")
	}
	if !strings.Contains(sql, "Do not build events_per_hour_mv in the bootstrap migration") {
		t.Fatalf("migration should document why the inline rollup is omitted")
	}
}

func TestDashboardAnalyticsMigrationCreatesReaderTables(t *testing.T) {
	raw, err := os.ReadFile("migrations/0004_dashboard_analytics_tables.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	for _, table := range []string{
		"telemetry_logs",
		"security_events",
		"rule_trigger_log",
		"telemetry_metrics_1m",
		"unique_counters",
		"threat_observations",
	} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration missing CREATE TABLE for %s", table)
		}
	}
	if strings.Index(sql, "`timestamp`") > strings.Index(sql, "`node_id`") {
		t.Fatalf("telemetry_logs timestamp key column should be declared before node_id")
	}
	securityEvents := sql[strings.Index(sql, "CREATE TABLE IF NOT EXISTS security_events"):]
	if strings.Index(securityEvents, "`fired_at`") > strings.Index(securityEvents, "`node_id`") {
		t.Fatalf("security_events fired_at key column should be declared before node_id")
	}
}
