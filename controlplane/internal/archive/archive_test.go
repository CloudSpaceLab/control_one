package archive

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveAndReadBack(t *testing.T) {
	dir := t.TempDir()
	w := &FilesystemWriter{BaseDir: dir}
	rows := []map[string]any{
		{"id": 1, "message": "hello"},
		{"id": 2, "message": "world"},
	}
	key := KeyForPartition("telemetry_logs", time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC), 1)
	if _, err := Archive(context.Background(), w, key, rows); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(key))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	dec := json.NewDecoder(gz)
	var got []map[string]any
	for dec.More() {
		var r map[string]any
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	if got[0]["message"] != "hello" {
		t.Fatalf("unexpected row 0: %+v", got[0])
	}
}

func TestKeyForPartition(t *testing.T) {
	k := KeyForPartition("x", time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC), 7)
	if k != "x/2026/04/24/part-0007.jsonl.gz" {
		t.Fatalf("unexpected key %s", k)
	}
}
