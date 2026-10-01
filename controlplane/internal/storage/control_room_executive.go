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
	FailedCritical       int
}

type ExecutiveAttentionItem struct {
	ID           uuid.UUID
	Kind         string
	Source       string
	Severity     string
	Domain       string
	AlertTitle   string
	AlertSummary string
	NodeHostname string
	Mode         string
	RuleID       string
	IPCIDR       string
	Reason       string
	ActionDomain string
	ActionKind   string
	CreatedAt    time.Time
}

type ExecutiveAttentionSummary struct {
	Total         int
	Critical      int
	Reviews       int
	Approvals     int
	Interventions int
	Items         []ExecutiveAttentionItem
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
// for one selected period. Attention samples are aggregated separately.
func (s *Store) GetAutomaticResponseSummary(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	until time.Time,
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
	err := s.db.QueryRowContext(ctx, `
		WITH candidates AS (
			SELECT
				p.id,
				LOWER(p.domain) AS domain,
				LOWER(p.action_kind) AS action_kind,
				p.state,
				p.risk,
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
			COUNT(*) FILTER (WHERE state = 'failed'),
			COUNT(*) FILTER (WHERE state = 'failed' AND LOWER(COALESCE(risk, '')) = 'critical')
		FROM classified
	`, tenantID, since, until).Scan(
		&out.HandledAutomatically,
		&out.Blocked,
		&out.Contained,
		&out.Remediated,
		&out.Failed,
		&out.FailedCritical,
	)
	if err != nil {
		return out, fmt.Errorf("count automatic responses: %w", err)
	}

	return out, nil
}

// GetExecutiveAttentionSummary returns exact human-work totals plus a bounded
// priority sample. Deduplication of alerts represented by pending block
// approvals or verified automatic responses happens in SQL so dashboard cost is
// independent of queue size.
func (s *Store) GetExecutiveAttentionSummary(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	until time.Time,
	itemLimit int,
) (ExecutiveAttentionSummary, error) {
	var out ExecutiveAttentionSummary
	if s.db == nil {
		return out, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return out, errors.New("tenant id is required")
	}
	if since.IsZero() || until.IsZero() || !since.Before(until) {
		return out, errors.New("attention window is invalid")
	}
	if itemLimit <= 0 {
		itemLimit = 8
	}
	if itemLimit > 50 {
		itemLimit = 50
	}

	const query = `
		WITH proposed_blocks AS (
			SELECT
				b.id,
				b.ip_cidr,
				b.reason,
				b.score,
				b.created_at,
				CASE
					WHEN b.reason LIKE 'Correlation response:%'
						THEN substring(b.reason FROM 'alert_id=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})')::uuid
				END AS alert_id
			FROM ip_blocklist_entries b
			WHERE b.tenant_id = $1
			  AND b.status = 'proposed'
		),
		verified_handled_alerts AS (
			SELECT
				CASE
					WHEN COALESCE(NULLIF(p.diff->>'reason', ''), NULLIF(p.source_ref->>'reason', ''), '')
						LIKE 'Correlation response:%'
						THEN substring(
							COALESCE(NULLIF(p.diff->>'reason', ''), NULLIF(p.source_ref->>'reason', ''), '')
							FROM 'alert_id=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})'
						)::uuid
				END AS alert_id
			FROM action_plans p
			JOIN LATERAL (
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
			  AND p.state IN ('succeeded', 'verified')
			  AND r.state IN ('succeeded', 'verified')
			  AND COALESCE(r.error, '') = ''
			  AND (
				LOWER(COALESCE(p.diff->>'auto_triggered', '')) IN ('true', '1', 'yes')
				OR LOWER(COALESCE(p.source_ref->>'auto_triggered', '')) IN ('true', '1', 'yes')
			  )
		),
		auto_failed_raw AS (
			SELECT
				p.id,
				LOWER(COALESCE(NULLIF(p.risk, ''), 'medium')) AS plan_severity,
				p.domain,
				p.action_kind,
				p.updated_at AS created_at,
				CASE
					WHEN COALESCE(NULLIF(p.diff->>'reason', ''), NULLIF(p.source_ref->>'reason', ''), '')
						LIKE 'Correlation response:%'
						THEN substring(
							COALESCE(NULLIF(p.diff->>'reason', ''), NULLIF(p.source_ref->>'reason', ''), '')
							FROM 'alert_id=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})'
						)::uuid
				END AS alert_id
			FROM action_plans p
			WHERE p.tenant_id = $1
			  AND p.updated_at >= $2
			  AND p.updated_at < $3
			  AND p.state = 'failed'
			  AND (
				LOWER(COALESCE(p.diff->>'auto_triggered', '')) IN ('true', '1', 'yes')
				OR LOWER(COALESCE(p.source_ref->>'auto_triggered', '')) IN ('true', '1', 'yes')
			  )
		),
		auto_failed AS (
			SELECT
				f.id,
				CASE GREATEST(
						CASE f.plan_severity
							WHEN 'critical' THEN 5
							WHEN 'high' THEN 4
							WHEN 'medium' THEN 3
							WHEN 'low' THEN 2
							WHEN 'info' THEN 1
							ELSE 0
						END,
						CASE LOWER(COALESCE(a.severity, ''))
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
					ELSE 'medium'
				END AS severity,
				f.domain,
				f.action_kind,
				f.created_at,
				f.alert_id
			FROM auto_failed_raw f
			LEFT JOIN alerts a
			  ON a.id = f.alert_id
			 AND a.tenant_id = $1
		),
		excluded_alerts AS (
			SELECT alert_id FROM proposed_blocks WHERE alert_id IS NOT NULL
			UNION
			SELECT alert_id FROM verified_handled_alerts WHERE alert_id IS NOT NULL
			UNION
			SELECT alert_id FROM auto_failed WHERE alert_id IS NOT NULL
		),
		review_alerts AS (
			SELECT
				a.id,
				LOWER(COALESCE(NULLIF(a.severity, ''), 'medium')) AS severity,
				a.title,
				COALESCE(a.summary, '') AS summary,
				a.opened_at
			FROM alerts a
			WHERE a.tenant_id = $1
			  AND a.state IN ('open', 'acked')
			  AND NOT EXISTS (
				SELECT 1 FROM excluded_alerts e WHERE e.alert_id = a.id
			  )
		),
		network_approvals AS (
			SELECT
				pb.id,
				pb.ip_cidr,
				pb.reason,
				pb.created_at,
				CASE GREATEST(
						CASE
							WHEN pb.score >= 100 THEN 5
							WHEN pb.score >= 80 THEN 4
							WHEN pb.score >= 50 THEN 3
							ELSE 2
						END,
						CASE LOWER(COALESCE(a.severity, ''))
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
						ELSE 'low'
				END AS severity
			FROM proposed_blocks pb
			LEFT JOIN alerts a
			  ON a.id = pb.alert_id
			 AND a.tenant_id = $1
		),
		patch_pending AS (
			SELECT p.id, p.mode, p.created_at, COALESCE(n.hostname, '') AS hostname
			FROM patch_approvals p
			LEFT JOIN nodes n ON n.id = p.node_id AND n.tenant_id = p.tenant_id
			WHERE p.tenant_id = $1 AND p.status = 'pending'
		),
		remediation_pending AS (
			SELECT
				r.id,
				LOWER(COALESCE(NULLIF(r.severity, ''), 'high')) AS severity,
				r.rule_id,
				r.created_at,
				COALESCE(n.hostname, '') AS hostname
			FROM remediation_approvals r
			LEFT JOIN nodes n ON n.id = r.node_id AND n.tenant_id = r.tenant_id
			WHERE r.tenant_id = $1 AND r.status = 'pending'
		),
		review_stats AS (
			SELECT
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE severity = 'critical')::int AS critical
			FROM review_alerts
		),
		patch_stats AS (
			SELECT COUNT(*)::int AS total
			FROM patch_pending
		),
		remediation_stats AS (
			SELECT
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE severity = 'critical')::int AS critical
			FROM remediation_pending
		),
		network_stats AS (
			SELECT
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE severity = 'critical')::int AS critical
			FROM network_approvals
		),
		failed_stats AS (
			SELECT
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE severity = 'critical')::int AS critical
			FROM auto_failed
		),
		counts AS (
			SELECT
				r.total AS reviews,
				(p.total + m.total + n.total)::int AS approvals,
				f.total AS interventions,
				(r.critical + m.critical + n.critical + f.critical)::int AS critical
			FROM review_stats r
			CROSS JOIN patch_stats p
			CROSS JOIN remediation_stats m
			CROSS JOIN network_stats n
			CROSS JOIN failed_stats f
		),
		ranked_items AS (
			(
				SELECT
					a.id, 'review'::text AS kind, 'alert'::text AS source,
					a.severity,
					'alerts'::text AS domain,
					a.title AS alert_title,
					a.summary AS alert_summary,
					''::text AS node_hostname,
					''::text AS mode,
					''::text AS rule_id,
					''::text AS ip_cidr,
					''::text AS reason,
					''::text AS action_domain,
					''::text AS action_kind,
					a.opened_at AS created_at,
					CASE a.severity
						WHEN 'critical' THEN 5
						WHEN 'high' THEN 4
						WHEN 'medium' THEN 3
						WHEN 'low' THEN 2
						WHEN 'info' THEN 1
						ELSE 0
					END AS severity_rank
				FROM review_alerts a
				ORDER BY severity_rank DESC, a.opened_at DESC, a.id
				LIMIT $4
			)

			UNION ALL

			(
				SELECT
					p.id, 'approval', 'patch', 'medium', 'patch',
					'', '', p.hostname, p.mode, '', '', '', '', '', p.created_at, 3
				FROM patch_pending p
				ORDER BY p.created_at DESC, p.id
				LIMIT $4
			)

			UNION ALL

			(
				SELECT
					r.id, 'approval', 'remediation', r.severity, 'compliance',
					'', '', r.hostname, '', r.rule_id, '', '', '', '', r.created_at,
					CASE r.severity
						WHEN 'critical' THEN 5
						WHEN 'high' THEN 4
						WHEN 'medium' THEN 3
						WHEN 'low' THEN 2
						WHEN 'info' THEN 1
						ELSE 0
					END
				FROM remediation_pending r
				ORDER BY 16 DESC, r.created_at DESC, r.id
				LIMIT $4
			)

			UNION ALL

			(
				SELECT
					n.id, 'approval', 'network', n.severity, 'network',
					'', '', '', '', '', n.ip_cidr, n.reason, '', '', n.created_at,
					CASE n.severity
						WHEN 'critical' THEN 5
						WHEN 'high' THEN 4
						WHEN 'medium' THEN 3
						WHEN 'low' THEN 2
						WHEN 'info' THEN 1
						ELSE 0
					END
				FROM network_approvals n
				ORDER BY 16 DESC, n.created_at DESC, n.id
				LIMIT $4
			)

			UNION ALL

			(
				SELECT
					f.id, 'intervention', 'automatic_response', f.severity,
					COALESCE(NULLIF(f.domain, ''), 'automation'),
					'', '', '', '', '', '', 'Automatic response failed',
					f.domain, f.action_kind, f.created_at,
					CASE f.severity
						WHEN 'critical' THEN 5
						WHEN 'high' THEN 4
						WHEN 'medium' THEN 3
						WHEN 'low' THEN 2
						WHEN 'info' THEN 1
						ELSE 0
					END
				FROM auto_failed f
				ORDER BY 16 DESC, f.created_at DESC, f.id
				LIMIT $4
			)
		)
		SELECT
			c.reviews,
			c.approvals,
			c.interventions,
			c.critical,
			(c.reviews + c.approvals + c.interventions)::int AS total,
			i.id,
			i.kind,
			i.source,
			i.severity,
			i.domain,
			i.alert_title,
			i.alert_summary,
			i.node_hostname,
			i.mode,
			i.rule_id,
			i.ip_cidr,
			i.reason,
			i.action_domain,
			i.action_kind,
			i.created_at
		FROM counts c
		LEFT JOIN LATERAL (
			SELECT *
			FROM ranked_items
			ORDER BY severity_rank DESC, created_at DESC, id
			LIMIT $4
		) i ON TRUE
		ORDER BY i.severity_rank DESC NULLS LAST, i.created_at DESC NULLS LAST, i.id
	`
	rows, err := s.db.QueryContext(ctx, query, tenantID, since, until, itemLimit)
	if err != nil {
		return out, fmt.Errorf("query executive attention summary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out.Items = make([]ExecutiveAttentionItem, 0, itemLimit)
	first := true
	for rows.Next() {
		var (
			itemID       uuid.NullUUID
			kind         sql.NullString
			source       sql.NullString
			severity     sql.NullString
			domain       sql.NullString
			alertTitle   sql.NullString
			alertSummary sql.NullString
			nodeHostname sql.NullString
			mode         sql.NullString
			ruleID       sql.NullString
			ipCIDR       sql.NullString
			reason       sql.NullString
			actionDomain sql.NullString
			actionKind   sql.NullString
			createdAt    sql.NullTime
		)
		var reviews, approvals, interventions, critical, total int
		if err := rows.Scan(
			&reviews,
			&approvals,
			&interventions,
			&critical,
			&total,
			&itemID,
			&kind,
			&source,
			&severity,
			&domain,
			&alertTitle,
			&alertSummary,
			&nodeHostname,
			&mode,
			&ruleID,
			&ipCIDR,
			&reason,
			&actionDomain,
			&actionKind,
			&createdAt,
		); err != nil {
			return out, fmt.Errorf("scan executive attention summary: %w", err)
		}
		if first {
			out.Reviews = reviews
			out.Approvals = approvals
			out.Interventions = interventions
			out.Critical = critical
			out.Total = total
			first = false
		}
		if !itemID.Valid {
			continue
		}
		out.Items = append(out.Items, ExecutiveAttentionItem{
			ID:           itemID.UUID,
			Kind:         strings.ToLower(strings.TrimSpace(kind.String)),
			Source:       strings.ToLower(strings.TrimSpace(source.String)),
			Severity:     strings.ToLower(strings.TrimSpace(severity.String)),
			Domain:       strings.ToLower(strings.TrimSpace(domain.String)),
			AlertTitle:   strings.TrimSpace(alertTitle.String),
			AlertSummary: strings.TrimSpace(alertSummary.String),
			NodeHostname: strings.TrimSpace(nodeHostname.String),
			Mode:         strings.TrimSpace(mode.String),
			RuleID:       strings.TrimSpace(ruleID.String),
			IPCIDR:       strings.TrimSpace(ipCIDR.String),
			Reason:       strings.TrimSpace(reason.String),
			ActionDomain: strings.TrimSpace(actionDomain.String),
			ActionKind:   strings.TrimSpace(actionKind.String),
			CreatedAt:    createdAt.Time.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}
