package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	// DefaultBatchSize is the default number of items to insert in a single batch.
	DefaultBatchSize = 1000
	// MaxBatchSize is the maximum number of items allowed in a single batch.
	MaxBatchSize = 10000
)

// BatchIngestionOptions configures batch ingestion behavior.
type BatchIngestionOptions struct {
	BatchSize int
	UseCopy   bool // Use PostgreSQL COPY for better performance
}

// CreateTelemetryMetricsBatch persists multiple metrics in optimized batches.
func (s *Store) CreateTelemetryMetricsBatch(ctx context.Context, metrics []CreateTelemetryMetricParams, opts BatchIngestionOptions) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if len(metrics) == 0 {
		return nil
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		batchSize = MaxBatchSize
	}

	if opts.UseCopy && s.supportsCopy() {
		return s.createTelemetryMetricsCopy(ctx, metrics)
	}

	// Use batched INSERT for better performance
	for i := 0; i < len(metrics); i += batchSize {
		end := i + batchSize
		if end > len(metrics) {
			end = len(metrics)
		}
		batch := metrics[i:end]
		if err := s.createTelemetryMetricsBatch(ctx, batch); err != nil {
			return fmt.Errorf("batch insert metrics [%d:%d]: %w", i, end, err)
		}
	}

	return nil
}

// CreateTelemetryLogsBatch persists multiple log entries in optimized batches.
func (s *Store) CreateTelemetryLogsBatch(ctx context.Context, logs []CreateTelemetryLogParams, opts BatchIngestionOptions) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if len(logs) == 0 {
		return nil
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		batchSize = MaxBatchSize
	}

	if opts.UseCopy && s.supportsCopy() {
		return s.createTelemetryLogsCopy(ctx, logs)
	}

	// Use batched INSERT for better performance
	for i := 0; i < len(logs); i += batchSize {
		end := i + batchSize
		if end > len(logs) {
			end = len(logs)
		}
		batch := logs[i:end]
		if err := s.createTelemetryLogsBatch(ctx, batch); err != nil {
			return fmt.Errorf("batch insert logs [%d:%d]: %w", i, end, err)
		}
	}

	return nil
}

func (s *Store) createTelemetryMetricsBatch(ctx context.Context, metrics []CreateTelemetryMetricParams) error {
	if len(metrics) == 0 {
		return nil
	}

	query := `
		INSERT INTO telemetry_metrics (
			id, tenant_id, node_id, metric_name, metric_value, metric_unit, labels, timestamp, created_at
		) VALUES
	`

	args := make([]any, 0, len(metrics)*9)
	valueStrings := make([]string, 0, len(metrics))

	now := s.clock()
	for i := range metrics {
		metric := &metrics[i]
		valueStrings = append(valueStrings, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+5, len(args)+6, len(args)+7, len(args)+8, len(args)+9))

		args = append(args, uuid.New(), metric.TenantID, metric.NodeID, metric.MetricName, metric.MetricValue)

		unit := sql.NullString{}
		if metric.MetricUnit != nil {
			unit = sql.NullString{String: strings.TrimSpace(*metric.MetricUnit), Valid: true}
		}
		args = append(args, unit)

		labelsJSON, err := encodeStringMap(metric.Labels)
		if err != nil {
			return fmt.Errorf("encode labels: %w", err)
		}
		args = append(args, labelsJSON)
		args = append(args, metric.Timestamp)
		args = append(args, now)
	}

	query += strings.Join(valueStrings, ",")

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert telemetry metrics: %w", err)
	}
	return nil
}

func (s *Store) createTelemetryLogsBatch(ctx context.Context, logs []CreateTelemetryLogParams) error {
	if len(logs) == 0 {
		return nil
	}

	query := `
		INSERT INTO telemetry_logs (
			id, tenant_id, node_id, log_level, log_message, log_source, log_program, labels, timestamp, created_at
		) VALUES
	`

	args := make([]any, 0, len(logs)*10)
	valueStrings := make([]string, 0, len(logs))

	now := s.clock()
	for i := range logs {
		log := &logs[i]
		valueStrings = append(valueStrings, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+5, len(args)+6, len(args)+7, len(args)+8, len(args)+9, len(args)+10))

		args = append(args, uuid.New(), log.TenantID, log.NodeID, log.LogLevel, log.LogMessage)

		source := sql.NullString{}
		if log.LogSource != nil {
			source = sql.NullString{String: strings.TrimSpace(*log.LogSource), Valid: true}
		}
		args = append(args, source)

		program := sql.NullString{}
		if log.LogProgram != nil {
			program = sql.NullString{String: strings.TrimSpace(*log.LogProgram), Valid: true}
		}
		args = append(args, program)

		labelsJSON, err := encodeStringMap(log.Labels)
		if err != nil {
			return fmt.Errorf("encode labels: %w", err)
		}
		args = append(args, labelsJSON)
		args = append(args, log.Timestamp)
		args = append(args, now)
	}

	query += strings.Join(valueStrings, ",")

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert telemetry logs: %w", err)
	}
	return nil
}

// createTelemetryMetricsCopy uses PostgreSQL COPY for high-performance bulk insert.
func (s *Store) createTelemetryMetricsCopy(ctx context.Context, metrics []CreateTelemetryMetricParams) error {
	if len(metrics) == 0 {
		return nil
	}

	txn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer txn.Rollback()

	stmt, err := txn.Prepare(pq.CopyIn("telemetry_metrics",
		"id", "tenant_id", "node_id", "metric_name", "metric_value", "metric_unit", "labels", "timestamp", "created_at"))
	if err != nil {
		return fmt.Errorf("prepare copy statement: %w", err)
	}

	now := s.clock()
	for _, metric := range metrics {
		unit := sql.NullString{}
		if metric.MetricUnit != nil {
			unit = sql.NullString{String: strings.TrimSpace(*metric.MetricUnit), Valid: true}
		}

		labelsJSON, err := encodeStringMap(metric.Labels)
		if err != nil {
			stmt.Close()
			return fmt.Errorf("encode labels: %w", err)
		}

		_, err = stmt.Exec(
			uuid.New(),
			metric.TenantID,
			metric.NodeID,
			metric.MetricName,
			metric.MetricValue,
			unit,
			labelsJSON,
			metric.Timestamp,
			now,
		)
		if err != nil {
			stmt.Close()
			return fmt.Errorf("copy row: %w", err)
		}
	}

	if _, err := stmt.Exec(); err != nil {
		stmt.Close()
		return fmt.Errorf("finalize copy: %w", err)
	}

	if err := stmt.Close(); err != nil {
		return fmt.Errorf("close copy statement: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// createTelemetryLogsCopy uses PostgreSQL COPY for high-performance bulk insert.
func (s *Store) createTelemetryLogsCopy(ctx context.Context, logs []CreateTelemetryLogParams) error {
	if len(logs) == 0 {
		return nil
	}

	txn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer txn.Rollback()

	stmt, err := txn.Prepare(pq.CopyIn("telemetry_logs",
		"id", "tenant_id", "node_id", "log_level", "log_message", "log_source", "log_program", "labels", "timestamp", "created_at"))
	if err != nil {
		return fmt.Errorf("prepare copy statement: %w", err)
	}

	now := s.clock()
	for _, log := range logs {
		source := sql.NullString{}
		if log.LogSource != nil {
			source = sql.NullString{String: strings.TrimSpace(*log.LogSource), Valid: true}
		}

		program := sql.NullString{}
		if log.LogProgram != nil {
			program = sql.NullString{String: strings.TrimSpace(*log.LogProgram), Valid: true}
		}

		labelsJSON, err := encodeStringMap(log.Labels)
		if err != nil {
			stmt.Close()
			return fmt.Errorf("encode labels: %w", err)
		}

		_, err = stmt.Exec(
			uuid.New(),
			log.TenantID,
			log.NodeID,
			log.LogLevel,
			log.LogMessage,
			source,
			program,
			labelsJSON,
			log.Timestamp,
			now,
		)
		if err != nil {
			stmt.Close()
			return fmt.Errorf("copy row: %w", err)
		}
	}

	if _, err := stmt.Exec(); err != nil {
		stmt.Close()
		return fmt.Errorf("finalize copy: %w", err)
	}

	if err := stmt.Close(); err != nil {
		return fmt.Errorf("close copy statement: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *Store) supportsCopy() bool {
	// Check if we're using lib/pq driver which supports COPY
	// We can detect this by checking if the connection supports the CopyIn method
	// For now, assume COPY is available if we're using PostgreSQL
	if s.db == nil {
		return false
	}
	// Try to detect by checking if we can import pq
	// In practice, if this code compiles, we have pq available
	return true
}
