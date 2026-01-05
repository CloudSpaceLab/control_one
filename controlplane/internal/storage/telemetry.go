package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TelemetryMetric represents a stored metric data point.
type TelemetryMetric struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	NodeID      uuid.UUID
	MetricName  string
	MetricValue float64
	MetricUnit  sql.NullString
	Labels      map[string]string
	Timestamp   time.Time
	CreatedAt   time.Time
}

// TelemetryLog represents a stored log entry.
type TelemetryLog struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	NodeID     uuid.UUID
	LogLevel   string
	LogMessage string
	LogSource  sql.NullString
	LogProgram sql.NullString
	Labels     map[string]string
	Timestamp  time.Time
	CreatedAt  time.Time
}

// TelemetryMetricFilter captures filters for querying metrics.
type TelemetryMetricFilter struct {
	TenantID   uuid.UUID
	NodeID     uuid.UUID
	MetricName string
	Since      *time.Time
	Until      *time.Time
}

// TelemetryLogFilter captures filters for querying logs.
type TelemetryLogFilter struct {
	TenantID  uuid.UUID
	NodeID    uuid.UUID
	LogLevel  string
	LogSource string
	Since     *time.Time
	Until     *time.Time
}

// CreateTelemetryMetricParams defines input for creating a metric.
type CreateTelemetryMetricParams struct {
	TenantID    uuid.UUID
	NodeID      uuid.UUID
	MetricName  string
	MetricValue float64
	MetricUnit  *string
	Labels      map[string]string
	Timestamp   time.Time
}

// CreateTelemetryLogParams defines input for creating a log entry.
type CreateTelemetryLogParams struct {
	TenantID   uuid.UUID
	NodeID     uuid.UUID
	LogLevel   string
	LogMessage string
	LogSource  *string
	LogProgram *string
	Labels     map[string]string
	Timestamp  time.Time
}

// ListTelemetryMetrics returns metrics matching the filter with pagination.
func (s *Store) ListTelemetryMetrics(ctx context.Context, filter TelemetryMetricFilter, limit, offset int) ([]TelemetryMetric, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.MetricName) != "" {
		args = append(args, strings.TrimSpace(filter.MetricName))
		clauses = append(clauses, fmt.Sprintf("metric_name = $%d", len(args)))
	}
	if filter.Since != nil {
		args = append(args, *filter.Since)
		clauses = append(clauses, fmt.Sprintf("timestamp >= $%d", len(args)))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		clauses = append(clauses, fmt.Sprintf("timestamp <= $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM telemetry_metrics WHERE %s`, strings.Join(clauses, " AND "))
	argsForCount := make([]any, len(args))
	copy(argsForCount, args)

	countRow := s.db.QueryRowContext(ctx, countQuery, argsForCount...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count telemetry metrics: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, metric_name, metric_value, metric_unit, labels, timestamp, created_at
		FROM telemetry_metrics
		WHERE %s
		ORDER BY timestamp DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query telemetry metrics: %w", err)
	}
	defer rows.Close()

	var metrics []TelemetryMetric
	for rows.Next() {
		var m TelemetryMetric
		var labelsRaw []byte
		if err := rows.Scan(
			&m.ID,
			&m.TenantID,
			&m.NodeID,
			&m.MetricName,
			&m.MetricValue,
			&m.MetricUnit,
			&labelsRaw,
			&m.Timestamp,
			&m.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan telemetry metric: %w", err)
		}
		labels, err := decodeStringMap(labelsRaw)
		if err != nil {
			return nil, 0, err
		}
		m.Labels = labels
		metrics = append(metrics, m)
	}

	return metrics, total, nil
}

// ListTelemetryLogs returns logs matching the filter with pagination.
func (s *Store) ListTelemetryLogs(ctx context.Context, filter TelemetryLogFilter, limit, offset int) ([]TelemetryLog, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.LogLevel) != "" {
		args = append(args, strings.TrimSpace(filter.LogLevel))
		clauses = append(clauses, fmt.Sprintf("log_level = $%d", len(args)))
	}
	if strings.TrimSpace(filter.LogSource) != "" {
		args = append(args, strings.TrimSpace(filter.LogSource))
		clauses = append(clauses, fmt.Sprintf("log_source = $%d", len(args)))
	}
	if filter.Since != nil {
		args = append(args, *filter.Since)
		clauses = append(clauses, fmt.Sprintf("timestamp >= $%d", len(args)))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		clauses = append(clauses, fmt.Sprintf("timestamp <= $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM telemetry_logs WHERE %s`, strings.Join(clauses, " AND "))
	argsForCount := make([]any, len(args))
	copy(argsForCount, args)

	countRow := s.db.QueryRowContext(ctx, countQuery, argsForCount...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count telemetry logs: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, log_level, log_message, log_source, log_program, labels, timestamp, created_at
		FROM telemetry_logs
		WHERE %s
		ORDER BY timestamp DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query telemetry logs: %w", err)
	}
	defer rows.Close()

	var logs []TelemetryLog
	for rows.Next() {
		var l TelemetryLog
		var labelsRaw []byte
		if err := rows.Scan(
			&l.ID,
			&l.TenantID,
			&l.NodeID,
			&l.LogLevel,
			&l.LogMessage,
			&l.LogSource,
			&l.LogProgram,
			&labelsRaw,
			&l.Timestamp,
			&l.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan telemetry log: %w", err)
		}
		labels, err := decodeStringMap(labelsRaw)
		if err != nil {
			return nil, 0, err
		}
		l.Labels = labels
		logs = append(logs, l)
	}

	return logs, total, nil
}

// CreateTelemetryMetrics persists multiple metrics in a batch.
func (s *Store) CreateTelemetryMetrics(ctx context.Context, metrics []CreateTelemetryMetricParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
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
		id := uuid.New()
		valueStrings = append(valueStrings, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+5, len(args)+6, len(args)+7, len(args)+8, len(args)+9))

		args = append(args, id, metric.TenantID, metric.NodeID, metric.MetricName, metric.MetricValue)

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

// CreateTelemetryLogs persists multiple log entries in a batch.
func (s *Store) CreateTelemetryLogs(ctx context.Context, logs []CreateTelemetryLogParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
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
		id := uuid.New()
		valueStrings = append(valueStrings, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+5, len(args)+6, len(args)+7, len(args)+8, len(args)+9, len(args)+10))

		args = append(args, id, log.TenantID, log.NodeID, log.LogLevel, log.LogMessage)

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

