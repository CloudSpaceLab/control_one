package telemetry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"
)

// LogSpoolSnapshot is a bounded, non-destructive view of durable log batches.
type LogSpoolSnapshot struct {
	Data      []byte
	Records   int
	Available bool
	Truncated bool
	Reason    string
}

// SnapshotLogSpool copies durable log records whose spool timestamps intersect
// the requested window. It never deletes or mutates spool records.
func (s *Service) SnapshotLogSpool(since, until time.Time, maxBytes int64) (LogSpoolSnapshot, error) {
	if s == nil || s.logSpool == nil {
		return LogSpoolSnapshot{Available: false, Reason: "durable log spool is not configured"}, nil
	}
	if maxBytes <= 0 {
		return LogSpoolSnapshot{}, errors.New("max snapshot bytes must be positive")
	}
	if !until.After(since) {
		return LogSpoolSnapshot{}, errors.New("snapshot window must be positive")
	}

	s.logSpoolMu.Lock()
	defer s.logSpoolMu.Unlock()

	records, err := s.logSpool.Records()
	if err != nil {
		return LogSpoolSnapshot{Available: false, Reason: "durable log spool is unavailable"}, fmt.Errorf("list log spool: %w", err)
	}
	out := LogSpoolSnapshot{Available: true}
	var buf bytes.Buffer
	for _, record := range records {
		info, statErr := os.Stat(record.Path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return out, fmt.Errorf("stat log spool record: %w", statErr)
		}
		ts := info.ModTime().UTC()
		if ts.Before(since) || ts.After(until) {
			continue
		}
		if record.Size < 0 || int64(buf.Len())+record.Size > maxBytes {
			out.Truncated = true
			break
		}
		body, readErr := s.logSpool.Read(record)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return out, fmt.Errorf("read log spool record: %w", readErr)
		}
		if int64(buf.Len()+len(body)) > maxBytes {
			out.Truncated = true
			break
		}
		if _, err := buf.Write(body); err != nil {
			return out, err
		}
		if len(body) > 0 && body[len(body)-1] != '\n' {
			if int64(buf.Len()+1) > maxBytes {
				out.Truncated = true
				break
			}
			_ = buf.WriteByte('\n')
		}
		out.Records++
	}
	out.Data = append([]byte(nil), buf.Bytes()...)
	return out, nil
}
