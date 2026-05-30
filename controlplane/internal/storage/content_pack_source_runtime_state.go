package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

type ContentPackSourceRuntimeStateRecord struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	State     contentpacks.SourceRuntimeState
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpsertContentPackSourceRuntimeStateParams struct {
	TenantID uuid.UUID
	State    contentpacks.SourceRuntimeState
}

type ContentPackSourceRuntimeStateFilter struct {
	Query          string
	CoverageStates []contentpacks.CoverageState
	StaleBefore    *time.Time
}

type ContentPackSourceRuntimeStateSummary struct {
	Total               int
	CollectorsReporting int
	ByState             map[string]int
	Metrics             contentpacks.SourceRuntimeMetrics
}

func (s *Store) UpsertContentPackSourceRuntimeState(ctx context.Context, p UpsertContentPackSourceRuntimeStateParams) (*ContentPackSourceRuntimeStateRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	state, err := normalizeContentPackSourceRuntimeState(p.State)
	if err != nil {
		return nil, err
	}
	metricsBytes, err := marshalContentPackSourceRuntimeMetrics(state.Metrics)
	if err != nil {
		return nil, err
	}
	labelsBytes, err := marshalContentPackStringMap(state.Labels)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO content_pack_source_runtime_states (
			tenant_id, source_instance_id, source_id, pack_id, pack_version, display_name,
			node_id, collector_id, collector_mode, parser_id, coverage_state, approval_required,
			approval_id, config_version, content_version, last_event_at, last_parsed_at,
			last_health_at, last_error, metrics, labels, created_at, updated_at
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20::jsonb,$21::jsonb,NOW(),NOW()
		)
		ON CONFLICT (tenant_id, source_instance_id) DO UPDATE
		SET source_id = EXCLUDED.source_id,
		    pack_id = EXCLUDED.pack_id,
		    pack_version = EXCLUDED.pack_version,
		    display_name = EXCLUDED.display_name,
		    node_id = EXCLUDED.node_id,
		    collector_id = EXCLUDED.collector_id,
		    collector_mode = EXCLUDED.collector_mode,
		    parser_id = EXCLUDED.parser_id,
		    coverage_state = EXCLUDED.coverage_state,
		    approval_required = EXCLUDED.approval_required,
		    approval_id = EXCLUDED.approval_id,
		    config_version = EXCLUDED.config_version,
		    content_version = EXCLUDED.content_version,
		    last_event_at = CASE
		        WHEN COALESCE(EXCLUDED.labels->>'control_one.metrics_semantics', '') = 'delta' THEN
		            CASE
		                WHEN content_pack_source_runtime_states.last_event_at IS NULL THEN EXCLUDED.last_event_at
		                WHEN EXCLUDED.last_event_at IS NULL THEN content_pack_source_runtime_states.last_event_at
		                WHEN EXCLUDED.last_event_at > content_pack_source_runtime_states.last_event_at THEN EXCLUDED.last_event_at
		                ELSE content_pack_source_runtime_states.last_event_at
		            END
		        ELSE EXCLUDED.last_event_at
		    END,
		    last_parsed_at = CASE
		        WHEN COALESCE(EXCLUDED.labels->>'control_one.metrics_semantics', '') = 'delta' THEN
		            CASE
		                WHEN content_pack_source_runtime_states.last_parsed_at IS NULL THEN EXCLUDED.last_parsed_at
		                WHEN EXCLUDED.last_parsed_at IS NULL THEN content_pack_source_runtime_states.last_parsed_at
		                WHEN EXCLUDED.last_parsed_at > content_pack_source_runtime_states.last_parsed_at THEN EXCLUDED.last_parsed_at
		                ELSE content_pack_source_runtime_states.last_parsed_at
		            END
		        ELSE EXCLUDED.last_parsed_at
		    END,
		    last_health_at = EXCLUDED.last_health_at,
		    last_error = EXCLUDED.last_error,
		    metrics = CASE
		        WHEN COALESCE(EXCLUDED.labels->>'control_one.metrics_semantics', '') = 'delta' THEN jsonb_build_object(
		            'events_received',
		                COALESCE((content_pack_source_runtime_states.metrics->>'events_received')::bigint, 0)
		                + COALESCE((EXCLUDED.metrics->>'events_received')::bigint, 0),
		            'events_parsed',
		                COALESCE((content_pack_source_runtime_states.metrics->>'events_parsed')::bigint, 0)
		                + COALESCE((EXCLUDED.metrics->>'events_parsed')::bigint, 0),
		            'events_dropped',
		                COALESCE((content_pack_source_runtime_states.metrics->>'events_dropped')::bigint, 0)
		                + COALESCE((EXCLUDED.metrics->>'events_dropped')::bigint, 0),
		            'parse_failures',
		                COALESCE((content_pack_source_runtime_states.metrics->>'parse_failures')::bigint, 0)
		                + COALESCE((EXCLUDED.metrics->>'parse_failures')::bigint, 0),
		            'lag_millis',
		                GREATEST(COALESCE((content_pack_source_runtime_states.metrics->>'lag_millis')::bigint, 0),
		                         COALESCE((EXCLUDED.metrics->>'lag_millis')::bigint, 0)),
		            'queue_depth',
		                COALESCE((EXCLUDED.metrics->>'queue_depth')::bigint,
		                         COALESCE((content_pack_source_runtime_states.metrics->>'queue_depth')::bigint, 0)),
		            'cursor_age_millis',
		                GREATEST(COALESCE((content_pack_source_runtime_states.metrics->>'cursor_age_millis')::bigint, 0),
		                         COALESCE((EXCLUDED.metrics->>'cursor_age_millis')::bigint, 0)),
		            'retry_count',
		                COALESCE((content_pack_source_runtime_states.metrics->>'retry_count')::bigint, 0)
		                + COALESCE((EXCLUDED.metrics->>'retry_count')::bigint, 0)
		        )
		        ELSE EXCLUDED.metrics
		    END,
		    labels = EXCLUDED.labels,
		    updated_at = NOW()
		RETURNING id
	`, p.TenantID,
		state.SourceInstanceID,
		state.SourceID,
		state.PackID,
		state.PackVersion,
		state.DisplayName,
		state.NodeID,
		state.CollectorID,
		state.CollectorMode,
		state.ParserID,
		string(state.CoverageState),
		state.ApprovalRequired,
		state.ApprovalID,
		state.ConfigVersion,
		state.ContentVersion,
		nullTimePtr(state.LastEventAt),
		nullTimePtr(state.LastParsedAt),
		nullTimePtr(state.LastHealthAt),
		state.LastError,
		metricsBytes,
		labelsBytes,
	).Scan(&id); err != nil {
		return nil, fmt.Errorf("upsert content pack source runtime state: %w", err)
	}
	return s.GetContentPackSourceRuntimeState(ctx, id)
}

func (s *Store) GetContentPackSourceRuntimeState(ctx context.Context, id uuid.UUID) (*ContentPackSourceRuntimeStateRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("source runtime state id is required")
	}
	row := s.db.QueryRowContext(ctx, contentPackSourceRuntimeStateSelectSQL+` WHERE id = $1`, id)
	record, err := scanContentPackSourceRuntimeState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) ListContentPackSourceRuntimeStates(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ContentPackSourceRuntimeStateRecord, int, error) {
	return s.ListContentPackSourceRuntimeStatesFiltered(ctx, tenantID, ContentPackSourceRuntimeStateFilter{}, limit, offset)
}

func (s *Store) ListContentPackSourceRuntimeStatesFiltered(ctx context.Context, tenantID uuid.UUID, filter ContentPackSourceRuntimeStateFilter, limit, offset int) ([]ContentPackSourceRuntimeStateRecord, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		return nil, 0, errors.New("offset must be non-negative")
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	pattern := "%" + query + "%"
	states, err := normalizeContentPackSourceRuntimeStateFilterStates(filter.CoverageStates)
	if err != nil {
		return nil, 0, err
	}
	var staleBefore any
	if filter.StaleBefore != nil && !filter.StaleBefore.IsZero() {
		staleBefore = filter.StaleBefore.UTC()
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM content_pack_source_runtime_states
		WHERE tenant_id = $1
		  AND (
		    $2 = ''
		    OR LOWER(source_instance_id) LIKE $3
		    OR LOWER(source_id) LIKE $3
		    OR LOWER(COALESCE(display_name, '')) LIKE $3
		    OR LOWER(COALESCE(node_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_mode, '')) LIKE $3
		    OR LOWER(COALESCE(parser_id, '')) LIKE $3
		    OR LOWER(COALESCE(approval_id, '')) LIKE $3
		    OR LOWER(COALESCE(config_version, '')) LIKE $3
		    OR LOWER(COALESCE(content_version, '')) LIKE $3
		    OR LOWER(COALESCE(last_error, '')) LIKE $3
		    OR LOWER(labels::text) LIKE $3
		  )
		  AND (
		    cardinality($5::text[]) = 0
		    OR (
		      CASE
		        WHEN $4::timestamptz IS NOT NULL AND last_health_at IS NOT NULL AND last_health_at < $4::timestamptz THEN 'stale'
		        ELSE coverage_state
		      END
		    ) = ANY($5::text[])
		  )
	`, tenantID, query, pattern, staleBefore, pq.Array(states)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count content pack source runtime states: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, contentPackSourceRuntimeStateSelectSQL+`
		WHERE tenant_id = $1
		  AND (
		    $2 = ''
		    OR LOWER(source_instance_id) LIKE $3
		    OR LOWER(source_id) LIKE $3
		    OR LOWER(COALESCE(display_name, '')) LIKE $3
		    OR LOWER(COALESCE(node_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_mode, '')) LIKE $3
		    OR LOWER(COALESCE(parser_id, '')) LIKE $3
		    OR LOWER(COALESCE(approval_id, '')) LIKE $3
		    OR LOWER(COALESCE(config_version, '')) LIKE $3
		    OR LOWER(COALESCE(content_version, '')) LIKE $3
		    OR LOWER(COALESCE(last_error, '')) LIKE $3
		    OR LOWER(labels::text) LIKE $3
		  )
		  AND (
		    cardinality($5::text[]) = 0
		    OR (
		      CASE
		        WHEN $4::timestamptz IS NOT NULL AND last_health_at IS NOT NULL AND last_health_at < $4::timestamptz THEN 'stale'
		        ELSE coverage_state
		      END
		    ) = ANY($5::text[])
		  )
		ORDER BY updated_at DESC
		LIMIT $6 OFFSET $7
	`, tenantID, query, pattern, staleBefore, pq.Array(states), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query content pack source runtime states: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackSourceRuntimeStateRecord, 0, limit)
	for rows.Next() {
		record, err := scanContentPackSourceRuntimeState(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) ContentPackSourceRuntimeStateSummaryFiltered(ctx context.Context, tenantID uuid.UUID, filter ContentPackSourceRuntimeStateFilter) (ContentPackSourceRuntimeStateSummary, error) {
	if s.db == nil {
		return ContentPackSourceRuntimeStateSummary{}, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return ContentPackSourceRuntimeStateSummary{}, errors.New("tenant id is required")
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	pattern := "%" + query + "%"
	states, err := normalizeContentPackSourceRuntimeStateFilterStates(filter.CoverageStates)
	if err != nil {
		return ContentPackSourceRuntimeStateSummary{}, err
	}
	var staleBefore any
	if filter.StaleBefore != nil && !filter.StaleBefore.IsZero() {
		staleBefore = filter.StaleBefore.UTC()
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH filtered AS (
			SELECT
				CASE
					WHEN $4::timestamptz IS NOT NULL AND last_health_at IS NOT NULL AND last_health_at < $4::timestamptz THEN 'stale'
					ELSE coverage_state
				END AS effective_state,
				collector_id,
				metrics
			FROM content_pack_source_runtime_states
			WHERE tenant_id = $1
			  AND (
			    $2 = ''
			    OR LOWER(source_instance_id) LIKE $3
			    OR LOWER(source_id) LIKE $3
			    OR LOWER(COALESCE(display_name, '')) LIKE $3
			    OR LOWER(COALESCE(node_id, '')) LIKE $3
			    OR LOWER(COALESCE(collector_id, '')) LIKE $3
			    OR LOWER(COALESCE(collector_mode, '')) LIKE $3
			    OR LOWER(COALESCE(parser_id, '')) LIKE $3
			    OR LOWER(COALESCE(approval_id, '')) LIKE $3
			    OR LOWER(COALESCE(config_version, '')) LIKE $3
			    OR LOWER(COALESCE(content_version, '')) LIKE $3
			    OR LOWER(COALESCE(last_error, '')) LIKE $3
			    OR LOWER(labels::text) LIKE $3
			  )
		), filtered_state AS (
			SELECT *
			FROM filtered
			WHERE cardinality($5::text[]) = 0 OR effective_state = ANY($5::text[])
		)
		SELECT
			effective_state,
			COUNT(*),
			COALESCE(ARRAY_REMOVE(ARRAY_AGG(DISTINCT collector_id), ''), ARRAY[]::text[]),
			COALESCE(SUM((metrics->>'events_received')::bigint), 0),
			COALESCE(SUM((metrics->>'events_parsed')::bigint), 0),
			COALESCE(SUM((metrics->>'events_dropped')::bigint), 0),
			COALESCE(SUM((metrics->>'parse_failures')::bigint), 0),
			COALESCE(MAX((metrics->>'lag_millis')::bigint), 0),
			COALESCE(SUM((metrics->>'queue_depth')::bigint), 0),
			COALESCE(MAX((metrics->>'cursor_age_millis')::bigint), 0),
			COALESCE(SUM((metrics->>'retry_count')::bigint), 0)
		FROM filtered_state
		GROUP BY effective_state
	`, tenantID, query, pattern, staleBefore, pq.Array(states))
	if err != nil {
		return ContentPackSourceRuntimeStateSummary{}, fmt.Errorf("summarize content pack source runtime states: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summary := ContentPackSourceRuntimeStateSummary{ByState: map[string]int{}}
	collectors := map[string]struct{}{}
	for rows.Next() {
		var state string
		var count int
		var collectorIDs pq.StringArray
		var metrics contentpacks.SourceRuntimeMetrics
		if err := rows.Scan(
			&state,
			&count,
			&collectorIDs,
			&metrics.EventsReceived,
			&metrics.EventsParsed,
			&metrics.EventsDropped,
			&metrics.ParseFailures,
			&metrics.LagMillis,
			&metrics.QueueDepth,
			&metrics.CursorAgeMillis,
			&metrics.RetryCount,
		); err != nil {
			return ContentPackSourceRuntimeStateSummary{}, err
		}
		summary.Total += count
		summary.ByState[state] = count
		summary.Metrics.EventsReceived += metrics.EventsReceived
		summary.Metrics.EventsParsed += metrics.EventsParsed
		summary.Metrics.EventsDropped += metrics.EventsDropped
		summary.Metrics.ParseFailures += metrics.ParseFailures
		summary.Metrics.LagMillis += metrics.LagMillis
		summary.Metrics.QueueDepth += metrics.QueueDepth
		summary.Metrics.CursorAgeMillis += metrics.CursorAgeMillis
		summary.Metrics.RetryCount += metrics.RetryCount
		for _, collectorID := range collectorIDs {
			collectorID = strings.TrimSpace(collectorID)
			if collectorID != "" {
				collectors[collectorID] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return ContentPackSourceRuntimeStateSummary{}, err
	}
	summary.CollectorsReporting = len(collectors)
	return summary, nil
}

func normalizeContentPackSourceRuntimeStateFilterStates(states []contentpacks.CoverageState) ([]string, error) {
	if len(states) == 0 {
		return []string{}, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(states))
	for _, state := range states {
		normalized := contentpacks.NormalizeCoverageState(string(state))
		if normalized == "" {
			continue
		}
		value := string(normalized)
		if !contentpacks.ValidCoverageState(value) {
			return nil, fmt.Errorf("unsupported coverage state filter %q", state)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

const contentPackSourceRuntimeStateSelectSQL = `
	SELECT id, tenant_id, source_instance_id, source_id, pack_id, pack_version, display_name,
	       node_id, collector_id, collector_mode, parser_id, coverage_state, approval_required,
	       approval_id, config_version, content_version, last_event_at, last_parsed_at,
	       last_health_at, last_error, metrics, labels, created_at, updated_at
	FROM content_pack_source_runtime_states
`

func scanContentPackSourceRuntimeState(row scanner) (*ContentPackSourceRuntimeStateRecord, error) {
	var record ContentPackSourceRuntimeStateRecord
	var state contentpacks.SourceRuntimeState
	var coverageState string
	var lastEventAt, lastParsedAt, lastHealthAt sql.NullTime
	var metricsRaw, labelsRaw []byte
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&state.SourceInstanceID,
		&state.SourceID,
		&state.PackID,
		&state.PackVersion,
		&state.DisplayName,
		&state.NodeID,
		&state.CollectorID,
		&state.CollectorMode,
		&state.ParserID,
		&coverageState,
		&state.ApprovalRequired,
		&state.ApprovalID,
		&state.ConfigVersion,
		&state.ContentVersion,
		&lastEventAt,
		&lastParsedAt,
		&lastHealthAt,
		&state.LastError,
		&metricsRaw,
		&labelsRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	state.CoverageState = contentpacks.CoverageState(coverageState)
	state.Metrics = decodeContentPackSourceRuntimeMetrics(metricsRaw)
	labels, err := decodeContentPackStringMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	state.Labels = labels
	if lastEventAt.Valid {
		t := lastEventAt.Time
		state.LastEventAt = &t
	}
	if lastParsedAt.Valid {
		t := lastParsedAt.Time
		state.LastParsedAt = &t
	}
	if lastHealthAt.Valid {
		t := lastHealthAt.Time
		state.LastHealthAt = &t
	}
	state.UpdatedAt = record.UpdatedAt
	record.State = state
	return &record, nil
}

func normalizeContentPackSourceRuntimeState(state contentpacks.SourceRuntimeState) (contentpacks.SourceRuntimeState, error) {
	state.SourceInstanceID = strings.TrimSpace(state.SourceInstanceID)
	state.SourceID = strings.TrimSpace(state.SourceID)
	if state.SourceInstanceID == "" {
		state.SourceInstanceID = state.SourceID
	}
	if state.SourceInstanceID == "" {
		return contentpacks.SourceRuntimeState{}, errors.New("source instance id is required")
	}
	if state.SourceID == "" {
		return contentpacks.SourceRuntimeState{}, errors.New("source id is required")
	}
	state.PackID = strings.TrimSpace(state.PackID)
	state.PackVersion = strings.TrimSpace(state.PackVersion)
	state.DisplayName = strings.TrimSpace(state.DisplayName)
	state.NodeID = strings.TrimSpace(state.NodeID)
	state.CollectorID = strings.TrimSpace(state.CollectorID)
	state.CollectorMode = strings.TrimSpace(state.CollectorMode)
	state.ParserID = strings.TrimSpace(state.ParserID)
	state.ApprovalID = strings.TrimSpace(state.ApprovalID)
	state.ConfigVersion = strings.TrimSpace(state.ConfigVersion)
	state.ContentVersion = strings.TrimSpace(state.ContentVersion)
	state.LastError = strings.TrimSpace(state.LastError)
	state.CoverageState = contentpacks.NormalizeCoverageState(string(state.CoverageState))
	if !contentpacks.ValidCoverageState(string(state.CoverageState)) {
		return contentpacks.SourceRuntimeState{}, fmt.Errorf("unsupported source runtime coverage state %q", state.CoverageState)
	}
	if state.LastHealthAt == nil {
		now := time.Now().UTC()
		state.LastHealthAt = &now
	}
	state.UpdatedAt = time.Now().UTC()
	return state, nil
}

func marshalContentPackSourceRuntimeMetrics(metrics contentpacks.SourceRuntimeMetrics) ([]byte, error) {
	data, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("marshal source runtime metrics: %w", err)
	}
	if string(data) == "null" {
		return []byte(`{}`), nil
	}
	return data, nil
}

func decodeContentPackSourceRuntimeMetrics(raw []byte) contentpacks.SourceRuntimeMetrics {
	if len(raw) == 0 {
		return contentpacks.SourceRuntimeMetrics{}
	}
	var metrics contentpacks.SourceRuntimeMetrics
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return contentpacks.SourceRuntimeMetrics{}
	}
	return metrics
}

func marshalContentPackStringMap(input map[string]string) ([]byte, error) {
	if len(input) == 0 {
		return []byte(`{}`), nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal source runtime labels: %w", err)
	}
	return data, nil
}

func decodeContentPackStringMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var labels map[string]string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return nil, fmt.Errorf("decode source runtime labels: %w", err)
	}
	if labels == nil {
		return map[string]string{}, nil
	}
	return labels, nil
}

func nullTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}
