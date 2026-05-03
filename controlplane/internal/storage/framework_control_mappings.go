package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ControlMappingRow is one row of framework_control_mappings.
type ControlMappingRow struct {
	Framework    string
	ControlID    string
	PolicyRuleID string
	Rationale    string
}

// ControlCoverage is a per-control coverage roll-up for a tenant within a period.
// Status is one of PASS / PARTIAL / FAIL / NO_COVERAGE.
type ControlCoverage struct {
	Framework     string
	ControlID     string
	Title         string
	Status        string
	NodesChecked  int
	NodesPassing  int
	NodesFailing  int
	EvidenceCount int
	LastCheckedAt *time.Time
}

// NodeControlRow is one cell of the per-node compliance matrix appendix.
// Status is PASS / FAIL / "" (no result for that node × control).
type NodeControlRow struct {
	NodeName      string
	ControlID     string
	Status        string
	LastCheckedAt *time.Time
}

// ListControlMappings returns every mapping for a framework.
func (s *Store) ListControlMappings(ctx context.Context, framework string) ([]ControlMappingRow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT framework, control_id, policy_rule_id, COALESCE(rationale, '')
		FROM framework_control_mappings
		WHERE framework = $1
		ORDER BY control_id, policy_rule_id
	`, framework)
	if err != nil {
		return nil, fmt.Errorf("list control mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ControlMappingRow
	for rows.Next() {
		var m ControlMappingRow
		if err := rows.Scan(&m.Framework, &m.ControlID, &m.PolicyRuleID, &m.Rationale); err != nil {
			return nil, fmt.Errorf("scan control mapping: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetControlCoverage rolls up compliance_results × framework_control_mappings for
// a tenant within [periodStart, periodEnd) and returns one ControlCoverage per
// control_id mapped to that framework. Controls with no results in the period
// are returned with Status="NO_COVERAGE" and zero counts. Title is left blank
// here; callers join against compliance.FrameworkControls to fill it.
func (s *Store) GetControlCoverage(ctx context.Context, tenantID uuid.UUID, framework string, periodStart, periodEnd time.Time) ([]ControlCoverage, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if framework == "" {
		return nil, errors.New("framework is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (cr.node_id, cr.rule_id)
				cr.node_id,
				cr.rule_id,
				cr.passed,
				COALESCE(cr.checked_at, cr.created_at) AS at
			FROM compliance_results cr
			WHERE cr.tenant_id = $1
			  AND COALESCE(cr.checked_at, cr.created_at) >= $3
			  AND COALESCE(cr.checked_at, cr.created_at) <  $4
			ORDER BY cr.node_id, cr.rule_id, COALESCE(cr.checked_at, cr.created_at) DESC
		),
		mapped AS (
			SELECT control_id, policy_rule_id
			FROM framework_control_mappings
			WHERE framework = $2
		),
		coverage AS (
			SELECT
				m.control_id,
				COUNT(DISTINCT lr.node_id) FILTER (WHERE lr.passed IS NOT NULL) AS nodes_checked,
				COUNT(DISTINCT lr.node_id) FILTER (WHERE lr.passed = TRUE)      AS nodes_passing,
				COUNT(DISTINCT lr.node_id) FILTER (WHERE lr.passed = FALSE)     AS nodes_failing,
				MAX(lr.at)                                                       AS last_at
			FROM mapped m
			LEFT JOIN latest lr ON lr.rule_id = m.policy_rule_id
			GROUP BY m.control_id
		),
		evidence AS (
			SELECT control_ref, COUNT(*) AS n
			FROM compliance_evidence
			WHERE tenant_id = $1 AND framework = $2 AND control_ref IS NOT NULL
			GROUP BY control_ref
		)
		SELECT
			c.control_id,
			c.nodes_checked,
			c.nodes_passing,
			c.nodes_failing,
			COALESCE(e.n, 0) AS evidence_count,
			c.last_at
		FROM coverage c
		LEFT JOIN evidence e ON e.control_ref = c.control_id
		ORDER BY c.control_id
	`, tenantID, framework, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("get control coverage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ControlCoverage
	for rows.Next() {
		var c ControlCoverage
		var lastAt sql.NullTime
		if err := rows.Scan(
			&c.ControlID,
			&c.NodesChecked,
			&c.NodesPassing,
			&c.NodesFailing,
			&c.EvidenceCount,
			&lastAt,
		); err != nil {
			return nil, fmt.Errorf("scan control coverage: %w", err)
		}
		c.Framework = framework
		switch {
		case c.NodesChecked == 0:
			c.Status = "NO_COVERAGE"
		case c.NodesFailing == 0:
			c.Status = "PASS"
		case c.NodesPassing == 0:
			c.Status = "FAIL"
		default:
			c.Status = "PARTIAL"
		}
		if lastAt.Valid {
			t := lastAt.Time
			c.LastCheckedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountResultsForReport returns the number of distinct (node_id, rule_id) pairs
// passing and failing for a tenant + framework within [periodStart, periodEnd).
// Joined to framework_control_mappings so unmapped rules do not pollute the count.
func (s *Store) CountResultsForReport(ctx context.Context, tenantID uuid.UUID, framework string, periodStart, periodEnd time.Time) (int, int, error) {
	if s.db == nil {
		return 0, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return 0, 0, errors.New("tenant id is required")
	}
	if framework == "" {
		return 0, 0, errors.New("framework is required")
	}

	var passed, failed int
	err := s.db.QueryRowContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (cr.node_id, cr.rule_id)
				cr.node_id, cr.rule_id, cr.passed
			FROM compliance_results cr
			JOIN framework_control_mappings fcm
			  ON fcm.policy_rule_id = cr.rule_id AND fcm.framework = $2
			WHERE cr.tenant_id = $1
			  AND COALESCE(cr.checked_at, cr.created_at) >= $3
			  AND COALESCE(cr.checked_at, cr.created_at) <  $4
			ORDER BY cr.node_id, cr.rule_id, COALESCE(cr.checked_at, cr.created_at) DESC
		)
		SELECT
			COALESCE(SUM(CASE WHEN passed THEN 1 ELSE 0 END), 0) AS passed,
			COALESCE(SUM(CASE WHEN passed THEN 0 ELSE 1 END), 0) AS failed
		FROM latest
	`, tenantID, framework, periodStart, periodEnd).Scan(&passed, &failed)
	if err != nil {
		return 0, 0, fmt.Errorf("count results for report: %w", err)
	}
	return passed, failed, nil
}

// GetPerNodeMatrix returns one row per (node, control) pair for the audit-report
// appendix. Capped at maxRows (callers typically pass 500). Each row reflects the
// latest compliance_results entry for that node × policy_rule_id within the
// period, joined to mappings to project onto framework controls.
func (s *Store) GetPerNodeMatrix(ctx context.Context, tenantID uuid.UUID, framework string, periodStart, periodEnd time.Time, maxRows int) ([]NodeControlRow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if framework == "" {
		return nil, errors.New("framework is required")
	}
	if maxRows <= 0 {
		maxRows = 500
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (cr.node_id, cr.rule_id)
				cr.node_id,
				cr.rule_id,
				cr.passed,
				COALESCE(cr.checked_at, cr.created_at) AS at
			FROM compliance_results cr
			WHERE cr.tenant_id = $1
			  AND COALESCE(cr.checked_at, cr.created_at) >= $3
			  AND COALESCE(cr.checked_at, cr.created_at) <  $4
			ORDER BY cr.node_id, cr.rule_id, COALESCE(cr.checked_at, cr.created_at) DESC
		)
		SELECT
			COALESCE(n.hostname, lr.node_id::text) AS node_name,
			fcm.control_id,
			CASE WHEN lr.passed THEN 'PASS' ELSE 'FAIL' END AS status,
			lr.at
		FROM latest lr
		JOIN framework_control_mappings fcm
		  ON fcm.policy_rule_id = lr.rule_id AND fcm.framework = $2
		LEFT JOIN nodes n ON n.id = lr.node_id
		ORDER BY node_name, fcm.control_id
		LIMIT $5
	`, tenantID, framework, periodStart, periodEnd, maxRows)
	if err != nil {
		return nil, fmt.Errorf("get per-node matrix: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NodeControlRow
	for rows.Next() {
		var r NodeControlRow
		var at sql.NullTime
		if err := rows.Scan(&r.NodeName, &r.ControlID, &r.Status, &at); err != nil {
			return nil, fmt.Errorf("scan per-node matrix row: %w", err)
		}
		if at.Valid {
			t := at.Time
			r.LastCheckedAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
