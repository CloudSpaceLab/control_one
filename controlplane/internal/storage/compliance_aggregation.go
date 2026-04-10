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

// ComplianceAggregation represents aggregated compliance statistics.
type ComplianceAggregation struct {
	Total       int
	Passed      int
	Failed      int
	BySeverity  map[string]int
	ByRuleID    map[string]int
	LastChecked *time.Time
}

// ComplianceTrend represents compliance trends over time.
type ComplianceTrend struct {
	Date     time.Time
	Total    int
	Passed   int
	Failed   int
	PassRate float64
}

// GetComplianceAggregation returns aggregated compliance statistics.
func (s *Store) GetComplianceAggregation(ctx context.Context, filter ComplianceResultFilter) (*ComplianceAggregation, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
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
	if strings.TrimSpace(filter.ScanID) != "" {
		args = append(args, strings.TrimSpace(filter.ScanID))
		clauses = append(clauses, fmt.Sprintf("scan_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.RuleID) != "" {
		args = append(args, strings.TrimSpace(filter.RuleID))
		clauses = append(clauses, fmt.Sprintf("rule_id = $%d", len(args)))
	}
	if filter.Passed != nil {
		args = append(args, *filter.Passed)
		clauses = append(clauses, fmt.Sprintf("passed = $%d", len(args)))
	}
	if strings.TrimSpace(filter.Severity) != "" {
		args = append(args, strings.TrimSpace(filter.Severity))
		clauses = append(clauses, fmt.Sprintf("severity = $%d", len(args)))
	}
	if filter.Since != nil {
		args = append(args, *filter.Since)
		clauses = append(clauses, fmt.Sprintf("checked_at >= $%d", len(args)))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		clauses = append(clauses, fmt.Sprintf("checked_at <= $%d", len(args)))
	}

	whereClause := strings.Join(clauses, " AND ")

	query := fmt.Sprintf(`
		SELECT 
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE passed = true) as passed,
			COUNT(*) FILTER (WHERE passed = false) as failed,
			COUNT(*) FILTER (WHERE severity = 'high') as high_severity,
			COUNT(*) FILTER (WHERE severity = 'medium') as medium_severity,
			COUNT(*) FILTER (WHERE severity = 'low') as low_severity,
			COUNT(*) FILTER (WHERE severity = 'info') as info_severity,
			MAX(checked_at) as last_checked
		FROM compliance_results
		WHERE %s
	`, whereClause)

	row := s.db.QueryRowContext(ctx, query, args...)

	var agg ComplianceAggregation
	var high, medium, low, info int
	var lastChecked sql.NullTime

	if err := row.Scan(
		&agg.Total,
		&agg.Passed,
		&agg.Failed,
		&high,
		&medium,
		&low,
		&info,
		&lastChecked,
	); err != nil {
		return nil, fmt.Errorf("scan compliance aggregation: %w", err)
	}

	agg.BySeverity = make(map[string]int)
	if high > 0 {
		agg.BySeverity["high"] = high
	}
	if medium > 0 {
		agg.BySeverity["medium"] = medium
	}
	if low > 0 {
		agg.BySeverity["low"] = low
	}
	if info > 0 {
		agg.BySeverity["info"] = info
	}

	if lastChecked.Valid {
		agg.LastChecked = &lastChecked.Time
	}

	ruleQuery := fmt.Sprintf(`
		SELECT rule_id, COUNT(*) as count
		FROM compliance_results
		WHERE %s
		GROUP BY rule_id
		ORDER BY count DESC
		LIMIT 20
	`, whereClause)

	ruleRows, err := s.db.QueryContext(ctx, ruleQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query compliance by rule: %w", err)
	}
	defer func() { _ = ruleRows.Close() }()

	agg.ByRuleID = make(map[string]int)
	for ruleRows.Next() {
		var ruleID string
		var count int
		if err := ruleRows.Scan(&ruleID, &count); err != nil {
			return nil, fmt.Errorf("scan rule aggregation: %w", err)
		}
		agg.ByRuleID[ruleID] = count
	}

	return &agg, nil
}

// GetComplianceTrends returns compliance trends over time.
func (s *Store) GetComplianceTrends(ctx context.Context, filter ComplianceResultFilter, intervalDays int) ([]ComplianceTrend, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if intervalDays <= 0 {
		intervalDays = 1
	} else if intervalDays > 90 {
		intervalDays = 90
	}
	_ = intervalDays // validated for future use in interval grouping

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
	if filter.Since != nil {
		args = append(args, *filter.Since)
		clauses = append(clauses, fmt.Sprintf("checked_at >= $%d", len(args)))
	} else {
		since := time.Now().AddDate(0, 0, -30)
		args = append(args, since)
		clauses = append(clauses, fmt.Sprintf("checked_at >= $%d", len(args)))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		clauses = append(clauses, fmt.Sprintf("checked_at <= $%d", len(args)))
	}

	whereClause := strings.Join(clauses, " AND ")

	query := fmt.Sprintf(`
		SELECT 
			DATE_TRUNC('day', checked_at) as date,
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE passed = true) as passed,
			COUNT(*) FILTER (WHERE passed = false) as failed
		FROM compliance_results
		WHERE %s AND checked_at IS NOT NULL
		GROUP BY DATE_TRUNC('day', checked_at)
		ORDER BY date ASC
	`, whereClause)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query compliance trends: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var trends []ComplianceTrend
	for rows.Next() {
		var trend ComplianceTrend
		if err := rows.Scan(
			&trend.Date,
			&trend.Total,
			&trend.Passed,
			&trend.Failed,
		); err != nil {
			return nil, fmt.Errorf("scan compliance trend: %w", err)
		}
		if trend.Total > 0 {
			trend.PassRate = float64(trend.Passed) / float64(trend.Total) * 100
		}
		trends = append(trends, trend)
	}

	return trends, nil
}
