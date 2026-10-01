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

type AutomaticResponseSummary struct {
	HandledAutomatically int
	Blocked              int
	Contained            int
	Remediated           int
	Failed               int
	FailedPlans          []ActionPlan
}

// GetRuleViolationSummary returns exact tenant-scoped rule violation counts for
// the current and previous windows plus a bounded top-rule sample.
func (s *Store) GetRuleViolationSummary(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	until time.Time,
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
	if since.IsZero() || until.IsZero() || prevSince.IsZero() || prevUntil.IsZero() {
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
		WHERE tenant_id = $1
		  AND triggered_at >= $2
		  AND triggered_at < $3
	`, tenantID, since, until).Scan(
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
		WHERE tenant_id = $1
		  AND triggered_at >= $2
		  AND triggered_at < $3
		GROUP BY rule_id, rule_type
		ORDER BY trigger_count DESC, rule_id
		LIMIT $4
	`, tenantID, since, until, topLimit)
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

// GetAutomaticResponseSummary returns exact verified automatic-response counts
// for one selected period. The failed-plan list is a bounded sample; Failed is
// always the exact total.
func (s *Store) GetAutomaticResponseSummary(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	until time.Time,
	failedLimit int,
) (AutomaticResponseSummary, error) {
	var out AutomaticResponseSummary
	if s.db == nil {
		return out, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return out, errors.New("tenant id is required")
	}
	if since.IsZero() || until.IsZero() || !since.Before(until) {
		return out, errors.New("automatic response window is invalid")
	}
	if failedLimit <= 0 {
		failedLimit = 8
	}
	if failedLimit > 50 {
		failedLimit = 50
	}

	err := s.db.QueryRowContext(ctx, `
		WITH candidates AS (
			SELECT
				p.id,
				LOWER(p.domain) AS domain,
				LOWER(p.action_kind) AS action_kind,
				p.state,
				r.state AS receipt_state,
				COALESCE(r.error, '') AS receipt_error
			FROM action_plans p
			LEFT JOIN LATERAL (
				SELECT ar.state, ar.error
				FROM action_receipts ar
				WHERE ar.action_plan_id = p.id
				  AND ar.created_at >= $2
				  AND ar.created_at < $3
				ORDER BY ar.created_at DESC, ar.id DESC
				LIMIT 1
			) r ON TRUE
			WHERE p.tenant_id = $1
			  AND p.updated_at >= $2
			  AND p.updated_at < $3
			  AND (
				LOWER(COALESCE(p.diff->>'auto_triggered', '')) IN ('true', '1', 'yes')
				OR LOWER(COALESCE(p.source_ref->>'auto_triggered', '')) IN ('true', '1', 'yes')
			  )
		),
		classified AS (
			SELECT *,
				(
					state IN ('succeeded', 'verified')
					AND receipt_state IN ('succeeded', 'verified')
					AND receipt_error = ''
				) AS handled,
				(domain = 'firewall' AND action_kind LIKE '%block%') AS is_blocked,
				(domain = 'remediation') AS is_remediated,
				(
					domain <> 'remediation'
					AND NOT (domain = 'firewall' AND action_kind LIKE '%block%')
					AND (
						action_kind LIKE '%contain%'
						OR action_kind LIKE '%isolation%'
						OR action_kind LIKE '%quarantine%'
					)
				) AS is_contained
			FROM candidates
		)
		SELECT
			COUNT(*) FILTER (WHERE handled),
			COUNT(*) FILTER (WHERE handled AND is_blocked),
			COUNT(*) FILTER (WHERE handled AND is_contained),
			COUNT(*) FILTER (WHERE handled AND is_remediated),
			COUNT(*) FILTER (WHERE state = 'failed')
		FROM classified
	`, tenantID, since, until).Scan(
		&out.HandledAutomatically,
		&out.Blocked,
		&out.Contained,
		&out.Remediated,
		&out.Failed,
	)
	if err != nil {
		return out, fmt.Errorf("count automatic responses: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, domain, action_kind, state, risk,
		       scope, diff, required_approvals, maintenance_window,
		       rollback_plan, verification_plan, idempotency_key, created_by,
		       source_ref, created_at, updated_at
		FROM action_plans p
		WHERE p.tenant_id = $1
		  AND p.updated_at >= $2
		  AND p.updated_at < $3
		  AND p.state = 'failed'
		  AND (
			LOWER(COALESCE(p.diff->>'auto_triggered', '')) IN ('true', '1', 'yes')
			OR LOWER(COALESCE(p.source_ref->>'auto_triggered', '')) IN ('true', '1', 'yes')
		  )
		ORDER BY p.updated_at DESC, p.id DESC
		LIMIT $4
	`, tenantID, since, until, failedLimit)
	if err != nil {
		return out, fmt.Errorf("query failed automatic responses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out.FailedPlans = make([]ActionPlan, 0, failedLimit)
	for rows.Next() {
		plan, err := scanActionPlan(rows)
		if err != nil {
			return out, fmt.Errorf("scan failed automatic response: %w", err)
		}
		out.FailedPlans = append(out.FailedPlans, *plan)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}
