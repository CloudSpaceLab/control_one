package durablespool

import (
	"encoding/json"
	"testing"
)

func TestSpoolAppendsListsReadsAndDeletesRecords(t *testing.T) {
	t.Parallel()

	spool, err := New(Options{Dir: t.TempDir(), Prefix: "test", MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := spool.AppendJSON(map[string]string{"id": "one"}); err != nil {
		t.Fatalf("AppendJSON() error = %v", err)
	}
	records, err := spool.Records()
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	stats, err := spool.Stats()
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Records != 1 || stats.Bytes <= 0 || stats.MaxBytes != 1<<20 {
		t.Fatalf("stats = %#v", stats)
	}
	data, err := spool.Read(records[0])
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload["id"] != "one" {
		t.Fatalf("payload = %#v", payload)
	}
	if err := spool.Delete(records[0]); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	records, err = spool.Records()
	if err != nil {
		t.Fatalf("Records(after delete) error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records after delete = %d, want 0", len(records))
	}
}

func TestSpoolDropsOldestWhenBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	spool, err := New(Options{Dir: t.TempDir(), Prefix: "test", MaxBytes: 80})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := spool.AppendJSON(map[string]string{"id": "one", "payload": "aaaaaaaaaa"}); err != nil {
		t.Fatalf("AppendJSON(one) error = %v", err)
	}
	if _, err := spool.AppendJSON(map[string]string{"id": "two", "payload": "bbbbbbbbbb"}); err != nil {
		t.Fatalf("AppendJSON(two) error = %v", err)
	}
	records, err := spool.Records()
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want oldest dropped and one retained", len(records))
	}
	stats, err := spool.Stats()
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.DroppedRecords != 1 {
		t.Fatalf("DroppedRecords = %d, want 1", stats.DroppedRecords)
	}
	data, err := spool.Read(records[0])
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload["id"] != "two" {
		t.Fatalf("retained payload = %#v, want id two", payload)
	}
}
