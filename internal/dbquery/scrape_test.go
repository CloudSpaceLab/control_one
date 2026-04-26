package dbquery

import "testing"

func TestExtractTablesFROM(t *testing.T) {
	tables := extractTables("select * from users where id = 1")
	if len(tables) != 1 || tables[0] != "users" {
		t.Fatalf("want [users], got %v", tables)
	}
}

func TestExtractTablesJoin(t *testing.T) {
	tables := extractTables("SELECT a.* FROM accounts a JOIN orders o ON o.account_id = a.id")
	if len(tables) != 2 {
		t.Fatalf("want 2 tables, got %v", tables)
	}
}

func TestExtractTablesUpdate(t *testing.T) {
	tables := extractTables("UPDATE customers SET active = false WHERE id = 7")
	if len(tables) != 1 || tables[0] != "customers" {
		t.Fatalf("want [customers], got %v", tables)
	}
}

func TestHashQueryStable(t *testing.T) {
	a := hashQueryText("SELECT 1")
	b := hashQueryText("SELECT 1")
	if a != b {
		t.Fatal("hash should be stable")
	}
}
