package doris

import "testing"

func TestWithLimit(t *testing.T) {
	got := withLimit("SELECT 1", 25)
	want := "SELECT 1\n\t\tLIMIT 25"
	if got != want {
		t.Fatalf("withLimit() = %q, want %q", got, want)
	}
}

func TestWithLimitOffset(t *testing.T) {
	got := withLimitOffset("SELECT 1", 25, 50)
	want := "SELECT 1 LIMIT 25 OFFSET 50"
	if got != want {
		t.Fatalf("withLimitOffset() = %q, want %q", got, want)
	}
}
