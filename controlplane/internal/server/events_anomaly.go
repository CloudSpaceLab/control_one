package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// detectAnomalies runs the five Phase F detectors over an ingest batch and
// returns synthetic anomaly.* events to append to the same batch. Each
// detector is a single index lookup against Postgres so the latency impact
// is bounded — the typical batch (≤256 KiB / ≤5000 rows) adds <50 ms.
//
// We intentionally fold anomalies into the same `events` slice rather than
// publishing them sideways: that way they ride the same Doris fan-out,
// hourly rollup, and eventbus delivery as the originating row, and the
// UI's correlation_id linkage just works.
func (s *Server) detectAnomalies(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) []IngestedEvent {
	if s == nil || s.store == nil {
		return nil
	}
	out := make([]IngestedEvent, 0, 4)
	for i := range events {
		ev := &events[i]
		switch ev.Type {
		case "conn.open":
			if a := s.detectFirstSeenDestination(ctx, tenantID, nodeID, ev); a != nil {
				out = append(out, *a)
			}
		case "conn.close":
			if a := s.detectLongConnection(ctx, tenantID, nodeID, ev); a != nil {
				out = append(out, *a)
			}
			out = append(out, s.detectExcessiveBytes(ctx, tenantID, nodeID, ev)...)
		case "proc.exec":
			if a := s.detectNewExecutable(ctx, tenantID, nodeID, ev); a != nil {
				out = append(out, *a)
			}
		case "file.write.summary":
			if a := s.detectExecutableDropped(ctx, tenantID, nodeID, ev); a != nil {
				out = append(out, *a)
			}
		case "db.query":
			out = append(out, s.detectDBQueryAnomalies(ctx, tenantID, nodeID, ev)...)
		}
	}
	out = append(out, s.detectIPBehaviorBatch(ctx, tenantID, nodeID, events)...)
	return out
}

// F.1: first-seen destination. Severity escalates to high if the conn
// also matched a threat feed.
func (s *Server) detectFirstSeenDestination(ctx context.Context, tenantID, nodeID uuid.UUID, ev *IngestedEvent) *IngestedEvent {
	if ev.DstIP == "" {
		return nil
	}
	res, err := s.store.UpsertKnownDestination(ctx, tenantID, ev.DstIP)
	if err != nil {
		s.logger.Debug("anomaly: upsert known destination", zap.Error(err))
		return nil
	}
	if !res.FirstSighting {
		return nil
	}
	sev := "low"
	if ev.ThreatScore > 0 || ev.ThreatFeed != "" {
		sev = "high"
	}
	return &IngestedEvent{
		Type:          "anomaly.new_destination",
		TS:            ev.TS,
		NodeID:        ev.NodeID,
		TenantID:      ev.TenantID,
		Severity:      sev,
		CorrelationID: ev.CorrelationID,
		ConnID:        ev.ConnID,
		PID:           ev.PID,
		ProcessName:   ev.ProcessName,
		UserName:      ev.UserName,
		SrcIP:         ev.SrcIP,
		SrcPort:       ev.SrcPort,
		DstIP:         ev.DstIP,
		DstPort:       ev.DstPort,
		Protocol:      ev.Protocol,
		ThreatFeed:    ev.ThreatFeed,
		ThreatScore:   ev.ThreatScore,
		Message:       fmt.Sprintf("first connection to %s by %s", ev.DstIP, ev.ProcessName),
		Details: map[string]any{
			"first_seen": true,
		},
		DedupKey: fmt.Sprintf("anomaly.new_dst:%s:%s", tenantID, ev.DstIP),
	}
}

// F.2: long connection — duration > 3× p95 from the rolling baseline.
func (s *Server) detectLongConnection(ctx context.Context, tenantID, nodeID uuid.UUID, ev *IngestedEvent) *IngestedEvent {
	if ev.DurationMS == 0 || ev.DstIP == "" || ev.DstPort == 0 {
		return nil
	}
	b, err := s.store.GetConnectionDurationBaseline(ctx, tenantID, ev.DstIP, ev.DstPort)
	if err != nil || b == nil {
		return nil
	}
	if b.SampleCount < 50 || b.P95MS <= 0 {
		return nil
	}
	if ev.DurationMS < 3*b.P95MS {
		return nil
	}
	return &IngestedEvent{
		Type:          "anomaly.long_connection",
		TS:            ev.TS,
		Severity:      "high",
		CorrelationID: ev.CorrelationID,
		ConnID:        ev.ConnID,
		PID:           ev.PID,
		ProcessName:   ev.ProcessName,
		UserName:      ev.UserName,
		SrcIP:         ev.SrcIP,
		DstIP:         ev.DstIP,
		DstPort:       ev.DstPort,
		Protocol:      ev.Protocol,
		DurationMS:    ev.DurationMS,
		Message:       fmt.Sprintf("connection to %s:%d ran %dms (p95 %dms)", ev.DstIP, ev.DstPort, ev.DurationMS, b.P95MS),
		Details: map[string]any{
			"baseline_p95_ms": b.P95MS,
			"sample_count":    b.SampleCount,
		},
		DedupKey: fmt.Sprintf("anomaly.long_conn:%s", ev.ConnID),
	}
}

// F.4: excessive bytes / packet-scan / fast-bulk-transfer. Returns up to
// three anomaly events for one conn.close.
func (s *Server) detectExcessiveBytes(ctx context.Context, tenantID, nodeID uuid.UUID, ev *IngestedEvent) []IngestedEvent {
	out := make([]IngestedEvent, 0, 2)

	// Detector A: high bytes_out vs baseline.
	if ev.BytesOut > 0 && ev.ProcessName != "" && ev.DstPort > 0 {
		if b, err := s.store.GetConnectionBytesBaseline(ctx, tenantID, ev.ProcessName, ev.DstPort); err == nil && b != nil &&
			b.SampleCount >= 30 && b.P95BytesOut > 4096 && ev.BytesOut > 5*b.P95BytesOut {
			out = append(out, IngestedEvent{
				Type:          "anomaly.high_bytes_out",
				TS:            ev.TS,
				Severity:      "high",
				CorrelationID: ev.CorrelationID,
				ConnID:        ev.ConnID,
				PID:           ev.PID,
				ProcessName:   ev.ProcessName,
				UserName:      ev.UserName,
				DstIP:         ev.DstIP,
				DstPort:       ev.DstPort,
				BytesOut:      ev.BytesOut,
				Message:       fmt.Sprintf("%s sent %d bytes to %s:%d (p95 %d)", ev.ProcessName, ev.BytesOut, ev.DstIP, ev.DstPort, b.P95BytesOut),
				Details:       map[string]any{"baseline_p95_bytes_out": b.P95BytesOut, "sample_count": b.SampleCount},
				DedupKey:      fmt.Sprintf("anomaly.high_bytes:%s", ev.ConnID),
			})
		}
	}

	// Detector B: fast bulk transfer (>=100 MiB in <5 s, no baseline needed).
	if ev.BytesOut >= 100*1024*1024 && ev.DurationMS > 0 && ev.DurationMS < 5000 {
		out = append(out, IngestedEvent{
			Type:          "anomaly.fast_bulk_transfer",
			TS:            ev.TS,
			Severity:      "high",
			CorrelationID: ev.CorrelationID,
			ConnID:        ev.ConnID,
			PID:           ev.PID,
			ProcessName:   ev.ProcessName,
			UserName:      ev.UserName,
			DstIP:         ev.DstIP,
			DstPort:       ev.DstPort,
			BytesOut:      ev.BytesOut,
			DurationMS:    ev.DurationMS,
			Message:       fmt.Sprintf("%d MB bulk transfer in %dms", ev.BytesOut/(1024*1024), ev.DurationMS),
			DedupKey:      fmt.Sprintf("anomaly.bulk:%s", ev.ConnID),
		})
	}

	// Detector C: packet-scan signal — many packets, low bytes/pkt ratio.
	if d := ev.Details; d != nil {
		if pktsAny, ok := d["packets_out"]; ok {
			pkts := toInt64(pktsAny)
			if pkts > 10000 && ev.BytesOut > 0 && ev.BytesOut/pkts < 60 {
				out = append(out, IngestedEvent{
					Type:          "anomaly.packet_scan",
					TS:            ev.TS,
					Severity:      "medium",
					CorrelationID: ev.CorrelationID,
					ConnID:        ev.ConnID,
					PID:           ev.PID,
					ProcessName:   ev.ProcessName,
					UserName:      ev.UserName,
					DstIP:         ev.DstIP,
					DstPort:       ev.DstPort,
					BytesOut:      ev.BytesOut,
					Message:       fmt.Sprintf("%s: %d pkts / %d bytes (avg %d B/pkt) — probable scan", ev.ProcessName, pkts, ev.BytesOut, ev.BytesOut/pkts),
					Details:       map[string]any{"packets_out": pkts, "bytes_per_pkt": ev.BytesOut / pkts},
					DedupKey:      fmt.Sprintf("anomaly.scan:%s", ev.ConnID),
				})
			}
		}
	}
	return out
}

// F.3 (a): new executable hash — never seen on this tenant.
func (s *Server) detectNewExecutable(ctx context.Context, tenantID, nodeID uuid.UUID, ev *IngestedEvent) *IngestedEvent {
	if ev.Details == nil {
		return nil
	}
	hashAny, ok := ev.Details["exe_hash"]
	if !ok {
		return nil
	}
	hash, _ := hashAny.(string)
	if hash == "" {
		return nil
	}
	pathStr, _ := ev.Details["exe_path"].(string)
	res, err := s.store.UpsertKnownExeHash(ctx, tenantID, hash, pathStr, ev.PID, &nodeID)
	if err != nil || !res.FirstSighting {
		return nil
	}
	sev := "medium"
	if strings.HasPrefix(pathStr, "/tmp/") || strings.HasPrefix(pathStr, "/dev/shm/") || strings.HasPrefix(pathStr, "/var/tmp/") {
		sev = "high"
	}
	return &IngestedEvent{
		Type:          "anomaly.new_executable",
		TS:            ev.TS,
		Severity:      sev,
		CorrelationID: ev.CorrelationID,
		PID:           ev.PID,
		ProcessName:   ev.ProcessName,
		UserName:      ev.UserName,
		Message:       fmt.Sprintf("first execution of %s (%s)", pathStr, hash),
		Details: map[string]any{
			"exe_hash": hash,
			"exe_path": pathStr,
		},
		DedupKey: fmt.Sprintf("anomaly.new_exe:%s:%s", tenantID, hash),
	}
}

// F.3 (b): executable dropped — file.write into a typical exec path.
// Path-based heuristic until eBPF mode-bit detection lands; once that's
// in place this becomes precise.
var executableDropPrefixes = []string{
	"/usr/local/bin/", "/usr/bin/", "/usr/sbin/",
	"/opt/", "/srv/",
	"/var/tmp/", "/dev/shm/", "/tmp/",
	"/home/", "/root/",
}

func (s *Server) detectExecutableDropped(ctx context.Context, tenantID, nodeID uuid.UUID, ev *IngestedEvent) *IngestedEvent {
	if ev.Details == nil {
		return nil
	}
	pathStr, _ := ev.Details["path"].(string)
	if pathStr == "" {
		return nil
	}
	matched := false
	for _, pfx := range executableDropPrefixes {
		if strings.HasPrefix(pathStr, pfx) {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}
	// Restrict to writes ≥1 KiB (skip noisy small writes).
	if ev.BytesIn < 1024 && ev.BytesOut < 1024 {
		return nil
	}
	return &IngestedEvent{
		Type:          "anomaly.executable_dropped",
		TS:            ev.TS,
		Severity:      "high",
		CorrelationID: ev.CorrelationID,
		PID:           ev.PID,
		ProcessName:   ev.ProcessName,
		UserName:      ev.UserName,
		Message:       fmt.Sprintf("%s wrote possible executable to %s", ev.ProcessName, pathStr),
		Details: map[string]any{
			"path":  pathStr,
			"bytes": ev.BytesIn + ev.BytesOut,
		},
		DedupKey: fmt.Sprintf("anomaly.drop:%s:%s", tenantID, pathStr),
	}
}

// F.5: anomalous DB query.
func (s *Server) detectDBQueryAnomalies(ctx context.Context, tenantID, nodeID uuid.UUID, ev *IngestedEvent) []IngestedEvent {
	if ev.Details == nil {
		return nil
	}
	engine, _ := ev.Details["engine"].(string)
	dbName, _ := ev.Details["database_name"].(string)
	user, _ := ev.Details["user_name"].(string)
	hash, _ := ev.Details["query_hash"].(string)
	if hash == "" {
		return nil
	}
	rows := toInt64(ev.Details["rows_affected"])
	execMS := toInt64(ev.Details["exec_time_ms"])
	res, err := s.store.UpsertKnownQueryHash(ctx, tenantID, engine, dbName, user, hash, ev.Message, rows, execMS)
	if err != nil {
		return nil
	}
	out := make([]IngestedEvent, 0, 2)
	if res.FirstSighting {
		sev := "medium"
		if isPrivilegedDBUser(user) || rows > 10000 {
			sev = "high"
		}
		out = append(out, IngestedEvent{
			Type:          "anomaly.new_db_query",
			TS:            ev.TS,
			Severity:      sev,
			CorrelationID: ev.CorrelationID,
			PID:           ev.PID,
			ProcessName:   ev.ProcessName,
			UserName:      user,
			Message:       fmt.Sprintf("new query by %s on %s.%s: %d rows in %dms", user, engine, dbName, rows, execMS),
			Details: map[string]any{
				"engine":        engine,
				"database_name": dbName,
				"user_name":     user,
				"query_hash":    hash,
				"rows_affected": rows,
				"exec_time_ms":  execMS,
			},
			DedupKey: fmt.Sprintf("anomaly.new_query:%s:%s:%s:%s:%s", tenantID, engine, dbName, user, hash),
		})
	} else {
		// High-rows variant: rows > 5× rolling max for this digest.
		if res.MaxRows.Valid && res.MaxRows.Int64 > 0 && rows > 5*res.MaxRows.Int64 && rows > 1000 {
			out = append(out, IngestedEvent{
				Type:          "anomaly.db_query_high_rows",
				TS:            ev.TS,
				Severity:      "high",
				CorrelationID: ev.CorrelationID,
				PID:           ev.PID,
				ProcessName:   ev.ProcessName,
				UserName:      user,
				Message:       fmt.Sprintf("query returned %d rows (max %d) — possible exfil", rows, res.MaxRows.Int64),
				Details: map[string]any{
					"engine":        engine,
					"database_name": dbName,
					"user_name":     user,
					"query_hash":    hash,
					"rows_affected": rows,
					"max_rows":      res.MaxRows.Int64,
				},
				DedupKey: fmt.Sprintf("anomaly.high_rows:%s:%s:%d", tenantID, hash, ev.TS.Unix()),
			})
		}
	}
	return out
}

func isPrivilegedDBUser(user string) bool {
	switch strings.ToLower(user) {
	case "root", "sa", "postgres", "admin", "administrator", "dba":
		return true
	}
	return false
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case float64:
		return int64(x)
	case float32:
		return int64(x)
	}
	return 0
}

var _ = time.Now // silence unused import when nothing else in this file uses time
