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
