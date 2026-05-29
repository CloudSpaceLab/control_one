package doris

import "testing"

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
