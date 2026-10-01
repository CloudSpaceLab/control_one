package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RuleViolationTopRule is one organisation-defined rule ranked by trigger count
// inside an executive dashboard window.
type RuleViolationTopRule struct {
	RuleID   uuid.UUID
	RuleType string
	Severity string
	Count    int
}

// RuleViolationSummary is the exact rule-trigger aggregate for one window.
// TopRules is intentionally a bounded sample; Total is never derived from it.
type RuleViolationSummary struct {
	Total         int
	Critical      int
	High          int
	Medium        int
	Low           int
	Info          int
	Other         int
	PreviousTotal int
	TopRules      []RuleViolationTopRule
}

// GetRuleViolationSummary returns exact tenant-scoped rule violation counts for
// the current and previous windows plus a bounded top-rule sample.
func (s *Store) GetRuleViolationSummary(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	prevSince time.Time,
	prevUntil time.Time,
	topLimit int,
) (RuleViolationSummary, error) {
	var out RuleViolationSummary
	if s.db == nil {
		return out, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return out, errors.New("tenant id is required")
	}
	if since.IsZero() || prevSince.IsZero() || prevUntil.IsZero() {
		return out, errors.New("rule violation windows are required")
	}
	if topLimit <= 0 {
		topLimit = 5
	}
	if topLimit > 25 {
		topLimit = 25
	}

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE LOWER(severity) = 'critical'),
			COUNT(*) FILTER (WHERE LOWER(severity) = 'high'),
			COUNT(*) FILTER (WHERE LOWER(severity) = 'medium'),
			COUNT(*) FILTER (WHERE LOWER(severity) = 'low'),
			COUNT(*) FILTER (WHERE LOWER(severity) = 'info'),
			COUNT(*) FILTER (
				WHERE LOWER(severity) NOT IN ('critical','high','medium','low','info')
			),
			COUNT(*)
		FROM rule_trigger_log
		WHERE tenant_id = $1 AND triggered_at >= $2
	`, tenantID, since).Scan(
		&out.Critical,
		&out.High,
		&out.Medium,
		&out.Low,
		&out.Info,
		&out.Other,
		&out.Total,
	)
	if err != nil {
		return out, fmt.Errorf("count rule violations: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM rule_trigger_log
		WHERE tenant_id = $1
		  AND triggered_at >= $2
		  AND triggered_at < $3
	`, tenantID, prevSince, prevUntil).Scan(&out.PreviousTotal); err != nil {
		return out, fmt.Errorf("count previous rule violations: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			rule_id,
			rule_type,
			CASE MAX(
				CASE LOWER(severity)
					WHEN 'critical' THEN 5
					WHEN 'high' THEN 4
					WHEN 'medium' THEN 3
					WHEN 'low' THEN 2
					WHEN 'info' THEN 1
					ELSE 0
				END
			)
				WHEN 5 THEN 'critical'
				WHEN 4 THEN 'high'
				WHEN 3 THEN 'medium'
				WHEN 2 THEN 'low'
				WHEN 1 THEN 'info'
				ELSE 'unknown'
			END AS severity,
			COUNT(*) AS trigger_count
		FROM rule_trigger_log
		WHERE tenant_id = $1 AND triggered_at >= $2
		GROUP BY rule_id, rule_type
		ORDER BY trigger_count DESC, rule_id
		LIMIT $3
	`, tenantID, since, topLimit)
	if err != nil {
		return out, fmt.Errorf("query top rule violations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out.TopRules = make([]RuleViolationTopRule, 0, topLimit)
	for rows.Next() {
		var row RuleViolationTopRule
		if err := rows.Scan(&row.RuleID, &row.RuleType, &row.Severity, &row.Count); err != nil {
			return out, fmt.Errorf("scan top rule violation: %w", err)
		}
		row.RuleType = strings.ToLower(strings.TrimSpace(row.RuleType))
		row.Severity = strings.ToLower(strings.TrimSpace(row.Severity))
		out.TopRules = append(out.TopRules, row)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}
