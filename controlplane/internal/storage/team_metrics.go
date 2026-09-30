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

// TeamSummary aggregates SOC activity attributed to human analysts across
// alerts (review/resolve), promoted cases, containment actions, and case notes.
type TeamSummary struct {
	AlertsReviewed              int     `json:"alerts_reviewed"`
	AlertsResolved              int     `json:"alerts_resolved"`
	CasesCreated                int     `json:"cases_created"`
	ContainmentActions          int     `json:"containment_actions"`
	NotesAdded                  int     `json:"notes_added"`
	ActiveAnalysts              int     `json:"active_analysts"`
	AvgTimeToInvestigateSeconds float64 `json:"avg_time_to_investigate_seconds"`
	AvgTimeToResolveSeconds     float64 `json:"avg_time_to_resolve_seconds"`
}

// TeamAnalystMetric is the per-analyst row for the team table.
type TeamAnalystMetric struct {
	AnalystID                   uuid.UUID `json:"analyst_id"`
	AnalystName                 string    `json:"analyst_name"`
	AlertsReviewed              int       `json:"alerts_reviewed"`
	AlertsResolved              int       `json:"alerts_resolved"`
	CasesCreated                int       `json:"cases_created"`
	ContainmentActions          int       `json:"containment_actions"`
	NotesAdded                  int       `json:"notes_added"`
	AvgTimeToInvestigateSeconds float64   `json:"avg_time_to_investigate_seconds"`
}

// TeamTrendPoint is one time bucket of the team activity trend charts.
type TeamTrendPoint struct {
	Bucket             time.Time `json:"bucket"`
	AlertsReviewed     int       `json:"alerts_reviewed"`
	AlertsResolved     int       `json:"alerts_resolved"`
	CasesCreated       int       `json:"cases_created"`
	ContainmentActions int       `json:"containment_actions"`
	CasesClosed        int       `json:"cases_closed"`
}

// TeamActivityItem is a single chronological entry in the activity feed.
type TeamActivityItem struct {
	Timestamp time.Time `json:"timestamp"`
	Kind      string    `json:"kind"` // alert_reviewed | alert_resolved | case_created | containment | note_added
	ActorID   uuid.UUID `json:"actor_id,omitempty"`
	ActorName string    `json:"actor_name,omitempty"`
	Detail    string    `json:"detail"`
	Severity  string    `json:"severity,omitempty"`
	LinkID    string    `json:"link_id,omitempty"`
}

// TeamCoverageGaps surfaces coverage blind spots for the CISO summary.
type TeamCoverageGaps struct {
	UnreviewedOpenAlerts    int `json:"unreviewed_open_alerts"`
	OpenAlertsOlderThan24h  int `json:"open_alerts_older_than_24h"`
	StaleCases              int `json:"stale_cases"`
	ActiveAnalystsLast7Days int `json:"active_analysts_last_7_days"`
	InactiveAnalysts90d     int `json:"inactive_analysts_90d"`
}

// teamActivityUnion normalizes every actor-attributed activity source into a
// single (ts, kind, actor, detail, severity, link, tti_secs, ttr_secs) shape.
//
// Placeholder contract, set by the caller:
//   - tenant id is always $1;
//   - windowClause is the SQL for the activity window (e.g. "BETWEEN $2 AND $3");
//   - actorIdx is the placeholder index for the optional analyst filter
//     ("= $4"), or 0 when no analyst filter is applied.
func teamActivityUnion(analystID uuid.UUID, actorIdx int, windowClause string) string {
	actorClause := func(column string) string {
		if analystID == uuid.Nil || actorIdx == 0 {
			return ""
		}
		return fmt.Sprintf(" AND %s = $%d", column, actorIdx)
	}
	return fmt.Sprintf(`
		SELECT ts, kind, actor, detail, severity, link, tti_secs, ttr_secs FROM (
			SELECT acked_at  AS ts, 'alert_reviewed' AS kind, acked_by AS actor, title AS detail,
			       severity, id::text AS link, GREATEST(0, EXTRACT(EPOCH FROM (acked_at - opened_at))) AS tti_secs,
			       NULL::float8 AS ttr_secs
			FROM alerts
			WHERE tenant_id = $1 AND acked_by IS NOT NULL AND acked_at IS NOT NULL AND acked_at %s%s
			UNION ALL
			SELECT resolved_at, 'alert_resolved', resolved_by, title, severity, id::text,
			       GREATEST(0, EXTRACT(EPOCH FROM (resolved_at - opened_at))), NULL::float8
			FROM alerts
			WHERE tenant_id = $1 AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL AND resolved_at %s%s
			UNION ALL
			SELECT created_at, 'case_created', created_by, summary, severity, id::text, NULL::float8, NULL::float8
			FROM ai_investigations
			WHERE tenant_id = $1 AND created_by IS NOT NULL AND created_at %s%s
			UNION ALL
			SELECT created_at, 'containment', created_by, action, '', entity_id, NULL::float8, NULL::float8
			FROM entity_actions
			WHERE tenant_id = $1 AND created_by IS NOT NULL AND created_at %s%s
			UNION ALL
			SELECT created_at, 'note_added', actor_id, COALESCE(metadata->>'note', ''), '', resource_id, NULL::float8, NULL::float8
			FROM audit_logs
			WHERE tenant_id = $1 AND action = 'soc.case.note.add' AND actor_id IS NOT NULL AND created_at %s%s
		) t
	`,
		windowClause, actorClause("acked_by"),
		windowClause, actorClause("resolved_by"),
		windowClause, actorClause("created_by"),
		windowClause, actorClause("created_by"),
		windowClause, actorClause("actor_id"))
}

// GetTeamSummary aggregates analyst activity in the window.
func (s *Store) GetTeamSummary(ctx context.Context, tenantID uuid.UUID, since, until time.Time) (*TeamSummary, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	union := teamActivityUnion(uuid.Nil, 0, "BETWEEN $2 AND $3")
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(SUM(CASE WHEN kind = 'alert_reviewed' THEN 1 ELSE 0 END), 0)::int AS alerts_reviewed,
			COALESCE(SUM(CASE WHEN kind = 'alert_resolved' THEN 1 ELSE 0 END), 0)::int AS alerts_resolved,
			COALESCE(SUM(CASE WHEN kind = 'case_created' THEN 1 ELSE 0 END), 0)::int AS cases_created,
			COALESCE(SUM(CASE WHEN kind = 'containment' THEN 1 ELSE 0 END), 0)::int AS containment_actions,
			COALESCE(SUM(CASE WHEN kind = 'note_added' THEN 1 ELSE 0 END), 0)::int AS notes_added,
			COUNT(DISTINCT actor)::int AS active_analysts,
			AVG(tti_secs) AS avg_tti,
			AVG(ttr_secs) AS avg_ttr
		FROM (%s) t
	`, union), tenantID, since, until)

	var summary TeamSummary
	err := row.Scan(
		&summary.AlertsReviewed,
		&summary.AlertsResolved,
		&summary.CasesCreated,
		&summary.ContainmentActions,
		&summary.NotesAdded,
		&summary.ActiveAnalysts,
		&summary.AvgTimeToInvestigateSeconds,
		&summary.AvgTimeToResolveSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("query team summary: %w", err)
	}
	return &summary, nil
}

// GetTeamAnalystMetrics returns per-analyst aggregates for the window,
// ordered by total workload (reviewed + created + containment + notes).
func (s *Store) GetTeamAnalystMetrics(ctx context.Context, tenantID uuid.UUID, since, until time.Time) ([]TeamAnalystMetric, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	union := teamActivityUnion(uuid.Nil, 0, "BETWEEN $2 AND $3")
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			actor,
			SUM(CASE WHEN kind = 'alert_reviewed' THEN 1 ELSE 0 END)::int AS alerts_reviewed,
			SUM(CASE WHEN kind = 'alert_resolved' THEN 1 ELSE 0 END)::int AS alerts_resolved,
			SUM(CASE WHEN kind = 'case_created' THEN 1 ELSE 0 END)::int AS cases_created,
			SUM(CASE WHEN kind = 'containment' THEN 1 ELSE 0 END)::int AS containment_actions,
			SUM(CASE WHEN kind = 'note_added' THEN 1 ELSE 0 END)::int AS notes_added,
			AVG(tti_secs) AS avg_tti
		FROM (%s) t
		GROUP BY actor
		ORDER BY
			(SUM(CASE WHEN kind = 'alert_reviewed' THEN 1 ELSE 0 END) +
			 SUM(CASE WHEN kind = 'case_created' THEN 1 ELSE 0 END) +
			 SUM(CASE WHEN kind = 'containment' THEN 1 ELSE 0 END) +
			 SUM(CASE WHEN kind = 'note_added' THEN 1 ELSE 0 END)) DESC,
			actor
	`, union), tenantID, since, until)
	if err != nil {
		return nil, fmt.Errorf("query team analyst metrics: %w", err)
	}
	defer rows.Close()

	type rawMetric struct {
		actor uuid.UUID
		TeamAnalystMetric
	}
	raw := []rawMetric{}
	actors := []uuid.UUID{}
	for rows.Next() {
		var m rawMetric
		if err := rows.Scan(
			&m.actor,
			&m.AlertsReviewed,
			&m.AlertsResolved,
			&m.CasesCreated,
			&m.ContainmentActions,
			&m.NotesAdded,
			&m.AvgTimeToInvestigateSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan team analyst metrics: %w", err)
		}
		m.AnalystID = m.actor
		raw = append(raw, m)
		actors = append(actors, m.actor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team analyst metrics: %w", err)
	}

	names := map[uuid.UUID]string{}
	if len(actors) > 0 {
		names, err = s.lookupUserDisplayNames(ctx, actors)
		if err != nil {
			return nil, fmt.Errorf("resolve analyst names: %w", err)
		}
	}

	out := make([]TeamAnalystMetric, 0, len(raw))
	for _, m := range raw {
		m.AnalystName = names[m.actor]
		out = append(out, m.TeamAnalystMetric)
	}
	return out, nil
}

// GetTeamTrends buckets analyst activity by the requested granularity
// ("day", "week", or "month", defaulting to "day") across the window.
func (s *Store) GetTeamTrends(ctx context.Context, tenantID uuid.UUID, since, until time.Time, granularity string) ([]TeamTrendPoint, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	trunc := timeBucketExpr(granularity, "ts")
	truncClosed := timeBucketExpr(granularity, "updated_at")
	union := teamActivityUnion(uuid.Nil, 0, "BETWEEN $2 AND $3")
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			bucket,
			SUM(CASE WHEN kind = 'alert_reviewed' THEN 1 ELSE 0 END)::int AS alerts_reviewed,
			SUM(CASE WHEN kind = 'alert_resolved' THEN 1 ELSE 0 END)::int AS alerts_resolved,
			SUM(CASE WHEN kind = 'case_created' THEN 1 ELSE 0 END)::int AS cases_created,
			SUM(CASE WHEN kind = 'containment' THEN 1 ELSE 0 END)::int AS containment_actions,
			SUM(CASE WHEN kind = 'case_closed' THEN 1 ELSE 0 END)::int AS cases_closed
		FROM (
			SELECT %s AS bucket, kind FROM (%s) t
			UNION ALL
			SELECT %s, 'case_closed' AS kind
			FROM ai_investigations
			WHERE tenant_id = $1 AND status = 'closed' AND updated_at BETWEEN $2 AND $3
		) u
		GROUP BY bucket
		ORDER BY bucket ASC
	`, trunc, union, truncClosed), tenantID, since, until)
	if err != nil {
		return nil, fmt.Errorf("query team trends: %w", err)
	}
	defer rows.Close()

	points := []TeamTrendPoint{}
	for rows.Next() {
		var p TeamTrendPoint
		if err := rows.Scan(
			&p.Bucket,
			&p.AlertsReviewed,
			&p.AlertsResolved,
			&p.CasesCreated,
			&p.ContainmentActions,
			&p.CasesClosed,
		); err != nil {
			return nil, fmt.Errorf("scan team trend: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team trends: %w", err)
	}
	return points, nil
}

// GetTeamActivityFeed returns the time-ordered activity feed in the window,
// newest first. When analystID is non-zero only that analyst's activity is
// returned.
func (s *Store) GetTeamActivityFeed(ctx context.Context, tenantID uuid.UUID, since, until time.Time, analystID uuid.UUID, limit, offset int) ([]TeamActivityItem, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}
	if limit == 0 {
		limit = 50
	}

	filtered := analystID != uuid.Nil
	actorIdx := 0
	limitIdx, offsetIdx := 4, 5
	if filtered {
		actorIdx = 4
		limitIdx, offsetIdx = 5, 6
	}
	union := teamActivityUnion(analystID, actorIdx, "BETWEEN $2 AND $3")

	args := []any{tenantID, since, until}
	if filtered {
		args = append(args, analystID)
	}

	countRow := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM (%s) t`, union), args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count team activity: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT ts, kind, actor, detail, severity, link
		FROM (%s) t
		ORDER BY ts DESC
		LIMIT $%d OFFSET $%d
	`, union, limitIdx, offsetIdx), append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("query team activity: %w", err)
	}
	defer rows.Close()

	type feedRow struct {
		item  TeamActivityItem
		actor uuid.UUID
	}
	items := []feedRow{}
	actors := []uuid.UUID{}
	for rows.Next() {
		var fr feedRow
		if err := rows.Scan(
			&fr.item.Timestamp,
			&fr.item.Kind,
			&fr.actor,
			&fr.item.Detail,
			&fr.item.Severity,
			&fr.item.LinkID,
		); err != nil {
			return nil, 0, fmt.Errorf("scan team activity: %w", err)
		}
		items = append(items, fr)
		actors = append(actors, fr.actor)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate team activity: %w", err)
	}

	names := map[uuid.UUID]string{}
	if len(actors) > 0 {
		names, err = s.lookupUserDisplayNames(ctx, actors)
		if err != nil {
			return nil, 0, fmt.Errorf("resolve activity actor names: %w", err)
		}
	}
	out := make([]TeamActivityItem, 0, len(items))
	for _, fr := range items {
		if fr.item.Kind != "note_added" {
			fr.item.Severity = strings.TrimSpace(fr.item.Severity)
		}
		fr.item.ActorID = fr.actor
		fr.item.ActorName = names[fr.actor]
		out = append(out, fr.item)
	}
	return out, total, nil
}

// GetTeamCoverageGaps reports coverage blind spots: never-acknowledged open
// alerts, stale open cases, and analysts who were active in the last 90 days
// but have gone quiet over the last 7.
func (s *Store) GetTeamCoverageGaps(ctx context.Context, tenantID uuid.UUID) (*TeamCoverageGaps, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	gaps := &TeamCoverageGaps{}
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			(SELECT COUNT(*) FROM alerts
			 WHERE tenant_id = $1 AND state = 'open' AND acked_at IS NULL AND resolved_at IS NULL)::int AS unreviewed,
			(SELECT COUNT(*) FROM alerts
			 WHERE tenant_id = $1 AND state = 'open' AND opened_at < NOW() - INTERVAL '24 hours')::int AS stale_alerts,
			(SELECT COUNT(*) FROM ai_investigations
			 WHERE tenant_id = $1 AND status <> 'closed' AND updated_at < NOW() - INTERVAL '7 days')::int AS stale_cases,
			%s AS active_7d,
			%s AS active_90d
	`, activeAnalystsExpr("NOW() - INTERVAL '7 days'"), activeAnalystsExpr("NOW() - INTERVAL '90 days'")),
		tenantID,
	).Scan(
		&gaps.UnreviewedOpenAlerts,
		&gaps.OpenAlertsOlderThan24h,
		&gaps.StaleCases,
		&gaps.ActiveAnalystsLast7Days,
		&gaps.InactiveAnalysts90d,
	); err != nil {
		return nil, fmt.Errorf("query team coverage gaps: %w", err)
	}
	gaps.InactiveAnalysts90d -= gaps.ActiveAnalystsLast7Days
	if gaps.InactiveAnalysts90d < 0 {
		gaps.InactiveAnalysts90d = 0
	}
	return gaps, nil
}

// activeAnalystsExpr counts distinct analysts with attributed activity after
// the given lower-bound expression (same placeholder $1 for tenant id).
func activeAnalystsExpr(sinceExpr string) string {
	return fmt.Sprintf(`(SELECT COUNT(DISTINCT actor) FROM (
		SELECT acked_by AS actor FROM alerts WHERE tenant_id = $1 AND acked_by IS NOT NULL AND acked_at >= %s
		UNION ALL SELECT resolved_by FROM alerts WHERE tenant_id = $1 AND resolved_by IS NOT NULL AND resolved_at >= %s
		UNION ALL SELECT created_by FROM ai_investigations WHERE tenant_id = $1 AND created_by IS NOT NULL AND created_at >= %s
		UNION ALL SELECT created_by FROM entity_actions WHERE tenant_id = $1 AND created_by IS NOT NULL AND created_at >= %s
		UNION ALL SELECT actor_id FROM audit_logs WHERE tenant_id = $1 AND action = 'soc.case.note.add' AND actor_id IS NOT NULL AND created_at >= %s
	) a)`, sinceExpr, sinceExpr, sinceExpr, sinceExpr, sinceExpr)
}

// lookupUserDisplayNames resolves user ids to display names (falling back to
// the email local part) via a single batched query.
func (s *Store) lookupUserDisplayNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(ids) == 0 || s.db == nil {
		return map[uuid.UUID]string{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, display_name, email FROM users WHERE id IN (%s)`, strings.Join(placeholders, ", ")),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make(map[uuid.UUID]string, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var displayName, email sql.NullString
		if err := rows.Scan(&id, &displayName, &email); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(displayName.String)
		if name == "" {
			if email.Valid {
				name = strings.TrimSpace(email.String)
				if at := strings.Index(name, "@"); at > 0 {
					name = name[:at]
				}
			}
		}
		if name == "" {
			name = id.String()[:8]
		}
		names[id] = name
	}
	return names, rows.Err()
}

func timeBucketExpr(granularity, column string) string {
	unit := "day"
	switch strings.ToLower(strings.TrimSpace(granularity)) {
	case "week":
		unit = "week"
	case "month":
		unit = "month"
	}
	return fmt.Sprintf("date_trunc('%s', %s)", unit, column)
}
