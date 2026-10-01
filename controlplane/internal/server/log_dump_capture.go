package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	maxControlPlaneLogDumpRows     = 25000
	maxControlPlaneLogDumpScanRows = 100000
	controlPlaneLogDumpPageSize    = 2000
)

func (s *Server) captureControlPlaneLogDump(ctx context.Context, dump *storage.LogDump) error {
	if dump == nil {
		return fmt.Errorf("log dump is nil")
	}
	store, ok := s.logDumpStorage()
	if !ok {
		return fmt.Errorf("log dump storage unavailable")
	}

	var (
		rowCount        int64
		scanned         int
		truncated       bool
		sourceAvailable = true
		sourceReason    string
	)
	artifact, err := writeLogDumpArtifactAtomic(dump.TenantID, dump.NodeID, dump.ID, func(dstWriter io.Writer) error {
		encoder := json.NewEncoder(dstWriter)
		for offset := 0; scanned < maxControlPlaneLogDumpScanRows && rowCount < maxControlPlaneLogDumpRows; offset += controlPlaneLogDumpPageSize {
			logs, total, err := s.store.ListTelemetryLogs(ctx, storage.TelemetryLogFilter{
				TenantID: dump.TenantID,
				NodeID:   dump.NodeID,
				Since:    &dump.WindowStart,
				Until:    &dump.WindowEnd,
			}, controlPlaneLogDumpPageSize, offset)
			if err != nil {
				sourceAvailable = false
				sourceReason = "control-plane telemetry source unavailable"
				return fmt.Errorf("query telemetry logs: %w", err)
			}
			if len(logs) == 0 {
				break
			}
			for _, entry := range logs {
				scanned++
				if !logDumpEntityMatches(dump.EntityFilter, entry.Labels) {
					continue
				}
				record := map[string]any{
					"id":         entry.ID.String(),
					"tenant_id":  entry.TenantID.String(),
					"node_id":    entry.NodeID.String(),
					"level":      entry.LogLevel,
					"message":    entry.LogMessage,
					"labels":     entry.Labels,
					"timestamp":  entry.Timestamp.UTC(),
					"created_at": entry.CreatedAt.UTC(),
				}
				if entry.LogSource.Valid {
					record["source"] = entry.LogSource.String
				}
				if entry.LogProgram.Valid {
					record["program"] = entry.LogProgram.String
				}
				if err := encoder.Encode(record); err != nil {
					return err
				}
				rowCount++
				if rowCount >= maxControlPlaneLogDumpRows {
					truncated = offset+len(logs) < total || scanned < total
					break
				}
			}
			if offset+len(logs) >= total {
				break
			}
			if scanned >= maxControlPlaneLogDumpScanRows {
				truncated = true
				break
			}
		}
		return nil
	})
	if err != nil {
		_ = store.FailControlPlaneLogDump(ctx, dump.TenantID, dump.NodeID, dump.ID, err.Error())
		if availabilityStore, ok := s.store.(interface {
			MarkLogDumpSourceAvailability(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string) error
		}); ok {
			_ = availabilityStore.MarkLogDumpSourceAvailability(
				ctx, dump.TenantID, dump.NodeID, dump.ID, sourceAvailable, sourceReason,
			)
		}
		return err
	}
	if scanned >= maxControlPlaneLogDumpScanRows {
		truncated = true
	}
	if err := store.MarkControlPlaneLogDumpCaptured(ctx, dump.TenantID, dump.NodeID, dump.ID,
		artifact.Path, artifact.SHA256, rowCount, artifact.SizeBytes, truncated, s.logDumpNow()); err != nil {
		_ = removeScopedLogDumpFile(artifact.Path)
		return err
	}
	return nil
}

func logDumpEntityMatches(entity map[string]any, labels map[string]string) bool {
	if len(entity) == 0 {
		return true
	}
	for key, raw := range entity {
		key = strings.TrimSpace(key)
		if key == "" {
			return false
		}
		want, ok := raw.(string)
		if !ok {
			return false
		}
		if got, ok := labels[key]; !ok || !strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want)) {
			return false
		}
	}
	return true
}
