package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MTTDMetrics represents Mean Time To Detect for security events.
type MTTDMetrics struct {
	Severity      string    `json:"severity"`
	MeanMinutes   float64   `json:"mean_minutes"`
	MedianMinutes float64   `json:"median_minutes"`
	P95Minutes    float64   `json:"p95_minutes"`
	EventCount    int       `json:"event_count"`
	Period        string    `json:"period"`
	CalculatedAt  time.Time `json:"calculated_at"`
}

// MTTRMetrics represents Mean Time To Remediate for compliance findings.
type MTTRMetrics struct {
	Severity         string    `json:"severity"`
	MeanMinutes      float64   `json:"mean_minutes"`
	MedianMinutes    float64   `json:"median_minutes"`
	P95Minutes       float64   `json:"p95_minutes"`
	RemediationCount int       `json:"remediation_count"`
	Period           string    `json:"period"`
	CalculatedAt     time.Time `json:"calculated_at"`
}

// RemediationVelocity represents the rate of remediations over time.
type RemediationVelocity struct {
	Period         string  `json:"period"`
	PeriodCount    int     `json:"period_count"`
	Remediations   int     `json:"remediations"`
	AvgPerPeriod   float64 `json:"avg_per_period"`
	TrendDirection string  `json:"trend_direction"` // "up", "down", "stable"
	TrendPercent   float64 `json:"trend_percent"`
}

// FindingAging represents the age distribution of open findings.
type FindingAging struct {
	Severity      string `json:"severity"`
	LessThan7Days int    `json:"less_than_7_days"`
	Days7to30     int    `json:"days_7_to_30"`
	Days30to90    int    `json:"days_30_to_90"`
	Over90Days    int    `json:"over_90_days"`
	TotalOpen     int    `json:"total_open"`
}

// RiskScore represents the executive risk score calculation.
type RiskScore struct {
	Score          int             `json:"score"`
	MaxScore       int             `json:"max_score"`
	Percent        float64         `json:"percent"`
	TrendDirection string          `json:"trend_direction"`
	TrendDelta     float64         `json:"trend_delta"`
	Components     []RiskComponent `json:"components"`
	CalculatedAt   time.Time       `json:"calculated_at"`
}

// RiskComponent represents a component of the risk score.
type RiskComponent struct {
	Name        string  `json:"name"`
	Weight      float64 `json:"weight"`
	RawScore    float64 `json:"raw_score"`
	MaxScore    float64 `json:"max_score"`
	Description string  `json:"description"`
}

// GetMTTDMetrics calculates Mean Time To Detect for security events.
func (s *Store) GetMTTDMetrics(ctx context.Context, tenantID uuid.UUID, severity string, since time.Time) (*MTTDMetrics, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	query := `
		SELECT 
			COUNT(*) as event_count,
			AVG(EXTRACT(EPOCH FROM (detected_at - occurred_at)) / 60) as mean_minutes,
			PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (detected_at - occurred_at)) / 60) as median_minutes,
			PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (detected_at - occurred_at)) / 60) as p95_minutes
		FROM security_events
		WHERE tenant_id = $1
			AND severity = $2
			AND detected_at IS NOT NULL
			AND occurred_at IS NOT NULL
			AND occurred_at >= $3
			AND detected_at > occurred_at
	`

	var metrics MTTDMetrics
	metrics.Severity = severity
	metrics.Period = since.Format("2006-01-02") + " to " + time.Now().Format("2006-01-02")
	metrics.CalculatedAt = time.Now().UTC()

	err := s.db.QueryRowContext(ctx, query, tenantID, severity, since).Scan(
		&metrics.EventCount,
		&metrics.MeanMinutes,
		&metrics.MedianMinutes,
		&metrics.P95Minutes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &metrics, nil // Return zero values if no events
		}
		return nil, fmt.Errorf("query MTTD metrics: %w", err)
	}

	return &metrics, nil
}

// GetMTTRMetrics calculates Mean Time To Remediate for compliance findings.
func (s *Store) GetMTTRMetrics(ctx context.Context, tenantID uuid.UUID, severity string, since time.Time) (*MTTRMetrics, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	query := `
		SELECT 
			COUNT(*) as remediation_count,
			AVG(EXTRACT(EPOCH FROM (remediated_at - detected_at)) / 60) as mean_minutes,
			PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (remediated_at - detected_at)) / 60) as median_minutes,
			PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (remediated_at - detected_at)) / 60) as p95_minutes
		FROM compliance_results
		WHERE tenant_id = $1
			AND severity = $2
			AND passed = false
			AND remediated_at IS NOT NULL
			AND detected_at IS NOT NULL
			AND detected_at >= $3
			AND remediated_at > detected_at
	`

	var metrics MTTRMetrics
	metrics.Severity = severity
	metrics.Period = since.Format("2006-01-02") + " to " + time.Now().Format("2006-01-02")
	metrics.CalculatedAt = time.Now().UTC()

	err := s.db.QueryRowContext(ctx, query, tenantID, severity, since).Scan(
		&metrics.RemediationCount,
		&metrics.MeanMinutes,
		&metrics.MedianMinutes,
		&metrics.P95Minutes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &metrics, nil
		}
		return nil, fmt.Errorf("query MTTR metrics: %w", err)
	}

	return &metrics, nil
}

// GetRemediationVelocity calculates remediation velocity over time.
func (s *Store) GetRemediationVelocity(ctx context.Context, tenantID uuid.UUID, periodDays int) (*RemediationVelocity, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	query := `
		WITH daily_remediations AS (
			SELECT 
				DATE_TRUNC('day', remediated_at) as day,
				COUNT(*) as count
			FROM compliance_results
			WHERE tenant_id = $1
				AND passed = false
				AND remediated_at IS NOT NULL
				AND remediated_at >= NOW() - INTERVAL '%d days'
			GROUP BY DATE_TRUNC('day', remediated_at)
		)
		SELECT 
			COUNT(*) as period_count,
			SUM(count) as total_remediations,
			AVG(count) as avg_per_period
		FROM daily_remediations
	`

	velocity := RemediationVelocity{
		Period: fmt.Sprintf("%d days", periodDays),
	}

	err := s.db.QueryRowContext(ctx, fmt.Sprintf(query, periodDays), tenantID).Scan(
		&velocity.PeriodCount,
		&velocity.Remediations,
		&velocity.AvgPerPeriod,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &velocity, nil
		}
		return nil, fmt.Errorf("query remediation velocity: %w", err)
	}

	// Calculate trend by comparing first half to second half of period
	if velocity.PeriodCount > 1 {
		trendQuery := `
			WITH daily_remediations AS (
				SELECT 
					DATE_TRUNC('day', remediated_at) as day,
					COUNT(*) as count,
					ROW_NUMBER() OVER (ORDER BY DATE_TRUNC('day', remediated_at)) as row_num
				FROM compliance_results
				WHERE tenant_id = $1
					AND passed = false
					AND remediated_at IS NOT NULL
					AND remediated_at >= NOW() - INTERVAL '%d days'
				GROUP BY DATE_TRUNC('day', remediated_at)
			),
			halfway AS (
				SELECT MAX(row_num) / 2 as mid FROM daily_remediations
			)
			SELECT 
				AVG(CASE WHEN dr.row_num <= h.mid THEN dr.count END) as first_half_avg,
				AVG(CASE WHEN dr.row_num > h.mid THEN dr.count END) as second_half_avg
			FROM daily_remediations dr, halfway h
		`

		var firstHalfAvg, secondHalfAvg float64
		err := s.db.QueryRowContext(ctx, fmt.Sprintf(trendQuery, periodDays), tenantID).Scan(&firstHalfAvg, &secondHalfAvg)
		if err == nil && firstHalfAvg > 0 {
			velocity.TrendPercent = ((secondHalfAvg - firstHalfAvg) / firstHalfAvg) * 100
			if velocity.TrendPercent > 10 {
				velocity.TrendDirection = "up"
			} else if velocity.TrendPercent < -10 {
				velocity.TrendDirection = "down"
			} else {
				velocity.TrendDirection = "stable"
			}
		}
	}

	return &velocity, nil
}

// GetFindingAging returns the age distribution of open findings.
func (s *Store) GetFindingAging(ctx context.Context, tenantID uuid.UUID, severity string) (*FindingAging, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	query := `
		SELECT 
			COUNT(*) FILTER (WHERE detected_at >= NOW() - INTERVAL '7 days') as less_than_7_days,
			COUNT(*) FILTER (WHERE detected_at >= NOW() - INTERVAL '30 days' AND detected_at < NOW() - INTERVAL '7 days') as days_7_to_30,
			COUNT(*) FILTER (WHERE detected_at >= NOW() - INTERVAL '90 days' AND detected_at < NOW() - INTERVAL '30 days') as days_30_to_90,
			COUNT(*) FILTER (WHERE detected_at < NOW() - INTERVAL '90 days') as over_90_days,
			COUNT(*) as total_open
		FROM compliance_results
		WHERE tenant_id = $1
			AND severity = $2
			AND passed = false
			AND (remediated_at IS NULL OR remediated_at > NOW())
	`

	var aging FindingAging
	aging.Severity = severity

	err := s.db.QueryRowContext(ctx, query, tenantID, severity).Scan(
		&aging.LessThan7Days,
		&aging.Days7to30,
		&aging.Days30to90,
		&aging.Over90Days,
		&aging.TotalOpen,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &aging, nil
		}
		return nil, fmt.Errorf("query finding aging: %w", err)
	}

	return &aging, nil
}

// CalculateRiskScore computes the executive risk score.
func (s *Store) CalculateRiskScore(ctx context.Context, tenantID uuid.UUID) (*RiskScore, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	now := time.Now().UTC()
	score := RiskScore{
		Score:        0,
		MaxScore:     100,
		Percent:      0,
		CalculatedAt: now,
		Components:   []RiskComponent{},
	}

	// Component 1: Compliance Posture (30% weight)
	complianceQuery := `
		SELECT 
			COUNT(*) FILTER (WHERE passed = true) as passed,
			COUNT(*) as total
		FROM compliance_results
		WHERE tenant_id = $1
			AND checked_at >= NOW() - INTERVAL '7 days'
	`
	var passed, total int
	err := s.db.QueryRowContext(ctx, complianceQuery, tenantID).Scan(&passed, &total)
	if err == nil && total > 0 {
		complianceScore := float64(passed) / float64(total) * 30
		score.Components = append(score.Components, RiskComponent{
			Name:        "Compliance Posture",
			Weight:      0.30,
			RawScore:    complianceScore,
			MaxScore:    30,
			Description: fmt.Sprintf("%.1f%% of checks passing", float64(passed)/float64(total)*100),
		})
		score.Score += int(complianceScore)
	}

	// Component 2: Critical Findings (25% weight)
	criticalQuery := `
		SELECT COUNT(*)
		FROM compliance_results
		WHERE tenant_id = $1
			AND severity = 'critical'
			AND passed = false
			AND (remediated_at IS NULL OR remediated_at > NOW())
			AND detected_at < NOW() - INTERVAL '7 days'
	`
	var criticalCount int
	err = s.db.QueryRowContext(ctx, criticalQuery, tenantID).Scan(&criticalCount)
	if err == nil {
		// Deduct up to 25 points based on aging critical findings
		criticalDeduction := float64(criticalCount) * 5
		if criticalDeduction > 25 {
			criticalDeduction = 25
		}
		criticalScore := 25 - criticalDeduction
		score.Components = append(score.Components, RiskComponent{
			Name:        "Critical Findings",
			Weight:      0.25,
			RawScore:    criticalScore,
			MaxScore:    25,
			Description: fmt.Sprintf("%d critical findings >7 days", criticalCount),
		})
		score.Score += int(criticalScore)
	}

	// Component 3: Fleet Health (20% weight)
	fleetQuery := `
		SELECT 
			COUNT(*) FILTER (WHERE last_seen_at >= NOW() - INTERVAL '5 minutes') as healthy,
			COUNT(*) as total
		FROM nodes
		WHERE tenant_id = $1
	`
	var healthy, fleetTotal int
	err = s.db.QueryRowContext(ctx, fleetQuery, tenantID).Scan(&healthy, &fleetTotal)
	if err == nil && fleetTotal > 0 {
		fleetScore := float64(healthy) / float64(fleetTotal) * 20
		score.Components = append(score.Components, RiskComponent{
			Name:        "Fleet Health",
			Weight:      0.20,
			RawScore:    fleetScore,
			MaxScore:    20,
			Description: fmt.Sprintf("%d/%d nodes reporting", healthy, fleetTotal),
		})
		score.Score += int(fleetScore)
	}

	// Component 4: Remediation Velocity (15% weight)
	velocityQuery := `
		SELECT COUNT(*)
		FROM compliance_results
		WHERE tenant_id = $1
			AND passed = false
			AND remediated_at >= NOW() - INTERVAL '7 days'
	`
	var recentRemediations int
	err = s.db.QueryRowContext(ctx, velocityQuery, tenantID).Scan(&recentRemediations)
	if err == nil {
		// Score based on remediation count (target: 10+ per week = full score)
		velocityScore := float64(recentRemediations) / 10 * 15
		if velocityScore > 15 {
			velocityScore = 15
		}
		score.Components = append(score.Components, RiskComponent{
			Name:        "Remediation Velocity",
			Weight:      0.15,
			RawScore:    velocityScore,
			MaxScore:    15,
			Description: fmt.Sprintf("%d remediations this week", recentRemediations),
		})
		score.Score += int(velocityScore)
	}

	// Component 5: Threat Activity (10% weight)
	threatQuery := `
		SELECT COUNT(*)
		FROM security_events
		WHERE tenant_id = $1
			AND severity IN ('critical', 'high')
			AND detected_at >= NOW() - INTERVAL '24 hours'
	`
	var recentThreats int
	err = s.db.QueryRowContext(ctx, threatQuery, tenantID).Scan(&recentThreats)
	if err == nil {
		// Deduct based on recent high-severity events (target: 0 = full score)
		threatDeduction := float64(recentThreats) * 2
		if threatDeduction > 10 {
			threatDeduction = 10
		}
		threatScore := 10 - threatDeduction
		score.Components = append(score.Components, RiskComponent{
			Name:        "Threat Activity",
			Weight:      0.10,
			RawScore:    threatScore,
			MaxScore:    10,
			Description: fmt.Sprintf("%d high-severity events in 24h", recentThreats),
		})
		score.Score += int(threatScore)
	}

	// Calculate percentage
	if score.MaxScore > 0 {
		score.Percent = float64(score.Score) / float64(score.MaxScore) * 100
	}

	// Calculate trend (compare to 7 days ago)
	trendQuery := `
		SELECT COUNT(*)
		FROM compliance_results
		WHERE tenant_id = $1
			AND passed = false
			AND detected_at >= NOW() - INTERVAL '14 days'
			AND detected_at < NOW() - INTERVAL '7 days'
	`
	var oldFindings int
	err = s.db.QueryRowContext(ctx, trendQuery, tenantID).Scan(&oldFindings)
	if err == nil {
		currentQuery := `
			SELECT COUNT(*)
			FROM compliance_results
			WHERE tenant_id = $1
				AND passed = false
				AND detected_at >= NOW() - INTERVAL '7 days'
		`
		var currentFindings int
		err = s.db.QueryRowContext(ctx, currentQuery, tenantID).Scan(&currentFindings)
		if err == nil && oldFindings > 0 {
			delta := float64(oldFindings-currentFindings) / float64(oldFindings) * 100
			score.TrendDelta = delta
			if delta > 5 {
				score.TrendDirection = "up" // Fewer findings = risk improving
			} else if delta < -5 {
				score.TrendDirection = "down"
			} else {
				score.TrendDirection = "stable"
			}
		}
	}

	return &score, nil
}

// RiskScorePoint is a single point on the risk-score history curve.
type RiskScorePoint struct {
	Timestamp time.Time `json:"ts"`
	Score     int       `json:"score"`
}

// RemediationVelocityPoint is a single day's remediation count.
type RemediationVelocityPoint struct {
	Timestamp time.Time `json:"ts"`
	Count     int       `json:"count"`
}

// FrameworkComplianceSummary aggregates compliance results by framework.
type FrameworkComplianceSummary struct {
	Name     string  `json:"name"`
	Pass     int     `json:"pass"`
	Fail     int     `json:"fail"`
	Coverage float64 `json:"coverage"`
}

// GetRiskScoreHistory returns one risk-score data point per day for the last
// `days` days. The score is recomputed for each day using the same component
// definitions as CalculateRiskScore but bucketed against historical data.
//
// TODO: this is computed on the fly which is acceptable for short windows but
// should be replaced with a materialised daily snapshot table once the metrics
// pipeline is wired up.
func (s *Store) GetRiskScoreHistory(ctx context.Context, tenantID uuid.UUID, days int) ([]RiskScorePoint, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	// For each day in the window, approximate the risk score as a weighted
	// blend of compliance pass-rate (60) and inverse critical-finding count (40),
	// using only data that existed up to the end of that day.
	query := `
		WITH days AS (
			SELECT generate_series(
				DATE_TRUNC('day', NOW() - ($1::int - 1) * INTERVAL '1 day'),
				DATE_TRUNC('day', NOW()),
				INTERVAL '1 day'
			)::timestamp AS day
		),
		compliance AS (
			SELECT
				d.day,
				COUNT(*) FILTER (WHERE cr.passed = true) AS passed,
				COUNT(*) AS total
			FROM days d
			LEFT JOIN compliance_results cr
				ON cr.tenant_id = $2
				AND cr.checked_at IS NOT NULL
				AND cr.checked_at <= d.day + INTERVAL '1 day'
				AND cr.checked_at >= d.day + INTERVAL '1 day' - INTERVAL '7 days'
			GROUP BY d.day
		),
		critical AS (
			SELECT
				d.day,
				COUNT(*) AS open_critical
			FROM days d
			LEFT JOIN compliance_results cr
				ON cr.tenant_id = $2
				AND cr.severity = 'critical'
				AND cr.passed = false
				AND cr.detected_at IS NOT NULL
				AND cr.detected_at <= d.day + INTERVAL '1 day'
				AND (cr.remediated_at IS NULL OR cr.remediated_at > d.day + INTERVAL '1 day')
			GROUP BY d.day
		)
		SELECT c.day,
			CASE WHEN c.total > 0 THEN (c.passed::float / c.total::float) * 60 ELSE 0 END
				+ GREATEST(0, 40 - LEAST(40, cr.open_critical * 5)) AS score
		FROM compliance c
		JOIN critical cr USING (day)
		ORDER BY c.day ASC
	`

	rows, err := s.db.QueryContext(ctx, query, days, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query risk-score history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	points := make([]RiskScorePoint, 0, days)
	for rows.Next() {
		var p RiskScorePoint
		var raw float64
		if err := rows.Scan(&p.Timestamp, &raw); err != nil {
			return nil, fmt.Errorf("scan risk-score point: %w", err)
		}
		p.Score = int(raw)
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate risk-score history: %w", err)
	}
	return points, nil
}

// GetRemediationVelocityHistory groups successful remediations by day.
func (s *Store) GetRemediationVelocityHistory(ctx context.Context, tenantID uuid.UUID, days int) ([]RemediationVelocityPoint, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	query := `
		WITH days AS (
			SELECT generate_series(
				DATE_TRUNC('day', NOW() - ($1::int - 1) * INTERVAL '1 day'),
				DATE_TRUNC('day', NOW()),
				INTERVAL '1 day'
			)::timestamp AS day
		)
		SELECT d.day,
			COUNT(cr.id) FILTER (
				WHERE cr.tenant_id = $2
					AND cr.passed = false
					AND cr.remediated_at IS NOT NULL
					AND DATE_TRUNC('day', cr.remediated_at) = d.day
			) AS count
		FROM days d
		LEFT JOIN compliance_results cr
			ON DATE_TRUNC('day', cr.remediated_at) = d.day
		GROUP BY d.day
		ORDER BY d.day ASC
	`

	rows, err := s.db.QueryContext(ctx, query, days, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query remediation velocity history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	points := make([]RemediationVelocityPoint, 0, days)
	for rows.Next() {
		var p RemediationVelocityPoint
		if err := rows.Scan(&p.Timestamp, &p.Count); err != nil {
			return nil, fmt.Errorf("scan velocity point: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate velocity history: %w", err)
	}
	return points, nil
}

// GetComplianceByFramework groups recent compliance results by framework. The
// framework is derived from the rule_id prefix up to the first dot (e.g.
// `cis-foundations.iam.1` becomes framework `cis-foundations`).
//
// TODO: ComplianceResult has no explicit framework column today; once a
// `framework` column or rule-catalog join exists, switch to that instead of
// string-prefix parsing.
func (s *Store) GetComplianceByFramework(ctx context.Context, tenantID uuid.UUID) ([]FrameworkComplianceSummary, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	query := `
		SELECT
			COALESCE(NULLIF(SPLIT_PART(rule_id, '.', 1), ''), 'unknown') AS framework,
			COUNT(*) FILTER (WHERE passed = true) AS pass,
			COUNT(*) FILTER (WHERE passed = false) AS fail,
			COUNT(DISTINCT node_id) AS nodes
		FROM compliance_results
		WHERE tenant_id = $1
			AND checked_at >= NOW() - INTERVAL '30 days'
		GROUP BY framework
		ORDER BY framework ASC
	`

	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query compliance by framework: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summaries := []FrameworkComplianceSummary{}
	for rows.Next() {
		var sum FrameworkComplianceSummary
		var nodes int
		if err := rows.Scan(&sum.Name, &sum.Pass, &sum.Fail, &nodes); err != nil {
			return nil, fmt.Errorf("scan framework row: %w", err)
		}
		total := sum.Pass + sum.Fail
		if total > 0 {
			sum.Coverage = float64(sum.Pass) / float64(total)
		}
		summaries = append(summaries, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate framework rows: %w", err)
	}
	return summaries, nil
}

// CountRemediationsSince returns the number of compliance results that were
// successfully remediated in the half-open window [since, until).
func (s *Store) CountRemediationsSince(ctx context.Context, tenantID uuid.UUID, since, until time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM compliance_results
		WHERE tenant_id = $1
			AND passed = false
			AND remediated_at >= $2
			AND remediated_at < $3
	`, tenantID, since, until).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count remediations: %w", err)
	}
	return n, nil
}
