// Package archive moves rows past their retention window out of Postgres and
// into cold storage (S3-compatible object store) before DELETE runs. The
// archival writer is pluggable — the default Filesystem writer is useful for
// tests and air-gapped deployments; an S3 writer is provided as a thin wrapper
// around any object-store SDK the caller supplies via PutObject.
package archive

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Writer persists an archive payload. key is a deterministic path like
// "telemetry_logs/2026-04-24/00000001.jsonl.gz". Implementations must treat
// identical keys as idempotent (overwrite ok).
type Writer interface {
	PutObject(ctx context.Context, key string, body io.Reader) error
}

// Archive serializes a slice of rows as newline-delimited JSON, gzips the
// payload, and hands it to the writer. Returns the byte count written.
func Archive(ctx context.Context, w Writer, key string, rows []map[string]any) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("writer required")
	}
	if !strings.HasSuffix(key, ".jsonl.gz") {
		key = strings.TrimSuffix(key, filepath.Ext(key)) + ".jsonl.gz"
	}
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	enc := json.NewEncoder(gz)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return 0, fmt.Errorf("encode row: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return 0, fmt.Errorf("gzip close: %w", err)
	}
	body := bytes.NewReader(raw.Bytes())
	if err := w.PutObject(ctx, key, body); err != nil {
		return 0, fmt.Errorf("put object: %w", err)
	}
	return raw.Len(), nil
}

// FilesystemWriter writes archives under a base directory. Useful for tests
// and on-prem deployments without an object store.
type FilesystemWriter struct {
	BaseDir string
}

func (f *FilesystemWriter) PutObject(_ context.Context, key string, body io.Reader) error {
	path := filepath.Join(f.BaseDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, body)
	return err
}

// ObjectStoreWriter adapts any (key, body) callback into a Writer. Downstream
// callers pass in an S3/MinIO/GCS-flavored function so this package does not
// grow a dependency on any specific cloud SDK.
type ObjectStoreWriter struct {
	Put func(ctx context.Context, key string, body io.Reader) error
}

func (o *ObjectStoreWriter) PutObject(ctx context.Context, key string, body io.Reader) error {
	return o.Put(ctx, key, body)
}

// KeyForPartition returns a time-partitioned key for a table + day, suitable
// for most archive layouts (e.g. "telemetry_logs/2026/04/24/part-0001.jsonl.gz").
func KeyForPartition(table string, day time.Time, part int) string {
	return fmt.Sprintf("%s/%04d/%02d/%02d/part-%04d.jsonl.gz",
		table, day.Year(), int(day.Month()), day.Day(), part)
}
