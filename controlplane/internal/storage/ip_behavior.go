package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type IPBehaviorObservation struct {
	TenantID    uuid.UUID
	NodeID      uuid.UUID
	Timestamp   time.Time
	ServerGroup string
	App         string
	CountryCode string
	Country     string
	ASN         string
	ISP         string
	SourceIP    string
	StatusCode  int
	BytesOut    int64
}

type IPBehaviorCountrySummary struct {
	CountryCode     string           `json:"country_code"`
	Country         string           `json:"country"`
	UniqueSourceIPs int64            `json:"unique_source_ips"`
	RequestCount    int64            `json:"request_count"`
	BytesOut        int64            `json:"bytes_out"`
	StatusCounts    map[string]int64 `json:"status_counts"`
	FirstSeenAt     time.Time        `json:"first_seen_at"`
	LastSeenAt      time.Time        `json:"last_seen_at"`
	TopASNs         []string         `json:"top_asns,omitempty"`
	TopApps         []string         `json:"top_apps,omitempty"`
	ServerGroups    []string         `json:"server_groups,omitempty"`
}

type IPBehaviorIPProfile struct {
	SourceIP     string                   `json:"source_ip"`
	Countries    []string                 `json:"countries"`
	ASNs         []string                 `json:"asns"`
	ISPs         []string                 `json:"isps,omitempty"`
	Apps         []string                 `json:"apps,omitempty"`
	ServerGroups []string                 `json:"server_groups,omitempty"`
	NodeIDs      []string                 `json:"node_ids,omitempty"`
	History      []IPBehaviorHistoryPoint `json:"history,omitempty"`
	RequestCount int64                    `json:"request_count"`
	BytesOut     int64                    `json:"bytes_out"`
	StatusCounts map[string]int64         `json:"status_counts"`
	FirstSeenAt  time.Time                `json:"first_seen_at"`
	LastSeenAt   time.Time                `json:"last_seen_at"`
}

type IPBehaviorASNSource struct {
	SourceIP     string    `json:"source_ip"`
	RequestCount int64     `json:"request_count"`
	BytesOut     int64     `json:"bytes_out"`
	Countries    []string  `json:"countries,omitempty"`
	Apps         []string  `json:"apps,omitempty"`
	ServerGroups []string  `json:"server_groups,omitempty"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type IPBehaviorHistoryPoint struct {
	HourTS       time.Time        `json:"hour_ts"`
	RequestCount int64            `json:"request_count"`
	BytesOut     int64            `json:"bytes_out"`
	StatusCounts map[string]int64 `json:"status_counts"`
}

type IPBehaviorBaseline struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	Dimension    string         `json:"dimension"`
	DimensionKey string         `json:"dimension_key"`
	Baseline     map[string]any `json:"baseline"`
	WindowDays   int            `json:"window_days"`
	SampleCount  int64          `json:"sample_count"`
	ComputedAt   time.Time      `json:"computed_at"`
}

type IPBehaviorBaselineStats struct {
	TenantID       uuid.UUID
	NodeID         uuid.NullUUID
	SignalType     string
	Dimension      string
	SampleCount    int64
	ObservedHours  int64
	TotalRequests  int64
	RequestMin     int64
	RequestAvg     float64
	RequestP50     float64
	RequestP95     float64
	RequestP99     float64
	RequestPeak    int64
	BytesMin       int64
	BytesAvg       float64
	BytesP50       float64
	BytesP95       float64
	BytesP99       float64
	BytesPeak      int64
	StatusCounts   map[string]int64
	ActiveHours    []int64
	ActiveWeekdays []int64
	ActiveWeeks    []int64
	ActiveMonths   []int64
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
}

type UpsertIPBehaviorFindingParams struct {
	TenantID    uuid.UUID
	NodeID      *uuid.UUID
	DedupKey    string
	SourceIP    string
	CountryCode string
	ASN         string
	Category    string
	Severity    string
	Score       int
	Reason      string
	Evidence    map[string]any
	SeenAt      time.Time
}

type IPBehaviorFinding struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	NodeID      uuid.NullUUID
	DedupKey    string
	SourceIP    sql.NullString
	CountryCode string
	ASN         string
	Category    string
	Severity    string
	Score       int
	Status      string
	Reason      string
	Evidence    map[string]any
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateIPBlocklistEntryParams struct {
	TenantID                uuid.UUID
	FindingID               *uuid.UUID
	IPCIDR                  string
	Scope                   string
	TargetType              string
	TargetID                *uuid.UUID
	ServerGroup             string
	App                     string
	VHost                   string
	Enforcement             string
	Reason                  string
	Score                   int
	ExpiresAt               *time.Time
	ProtectedOverride       bool
	ProtectedOverrideReason string
}

type IPBlocklistEntry struct {
	ID                      uuid.UUID
	TenantID                uuid.UUID
	FindingID               uuid.NullUUID
	EntityActionID          uuid.NullUUID
	IPCIDR                  string
	Scope                   string
	TargetType              string
	TargetID                uuid.NullUUID
	ServerGroup             string
	App                     string
	VHost                   string
	Enforcement             string
	ProtectedOverride       bool
	ProtectedOverrideReason string
	Status                  string
	Reason                  string
	Score                   int
	ExpiresAt               sql.NullTime
	ApprovedBy              uuid.NullUUID
	ApprovedAt              sql.NullTime
	LastError               sql.NullString
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type IPBehaviorFindingFilter struct {
	TenantID uuid.UUID
	Resolved *bool
	SourceIP string
	Since    time.Time
}

type IPBlocklistEntryFilter struct {
	TenantID    uuid.UUID
	FindingID   uuid.UUID
	IPCIDR      string
	Status      string
	ServerGroup string
	TargetType  string
	TargetID    uuid.UUID
	App         string
	VHost       string
}

type WebserverInstance struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	NodeID        uuid.UUID
	Kind          string
	Version       string
	ServiceName   string
	ConfigPath    string
	AccessLogPath string
	ErrorLogPath  string
	VHosts        []map[string]any
	Capabilities  map[string]any
	ObservedAt    time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type WebserverConfigAction struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	NodeID              uuid.UUID
	WebserverInstanceID uuid.NullUUID
	JobID               uuid.NullUUID
	Action              string
	Status              string
	Policy              map[string]any
	Result              map[string]any
	ErrorMessage        sql.NullString
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateWebserverConfigActionParams struct {
	TenantID            uuid.UUID
	NodeID              uuid.UUID
	WebserverInstanceID *uuid.UUID
	JobID               *uuid.UUID
	Action              string
	Policy              map[string]any
}

type CreateWebserverConfigReceiptParams struct {
	TenantID            uuid.UUID
	NodeID              uuid.UUID
	WebserverInstanceID *uuid.UUID
	ActionID            *uuid.UUID
	Action              string
	ChecksumBefore      string
	ChecksumAfter       string
	ValidationStatus    string
	ReloadStatus        string
	RollbackRef         string
	Diff                string
	Metadata            map[string]any
}

type WebserverConfigReceipt struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	NodeID              uuid.UUID
	WebserverInstanceID uuid.NullUUID
	ActionID            uuid.NullUUID
	Action              string
	ChecksumBefore      string
	ChecksumAfter       string
	ValidationStatus    string
	ReloadStatus        string
	RollbackRef         string
	Diff                string
	Metadata            map[string]any
	CreatedAt           time.Time
}

func (s *Store) IncrementIPBehaviorRollups(ctx context.Context, observations []IPBehaviorObservation) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	for _, obs := range observations {
		if obs.TenantID == uuid.Nil || obs.NodeID == uuid.Nil || net.ParseIP(obs.SourceIP) == nil {
			continue
		}
		ts := obs.Timestamp
		if ts.IsZero() {
			ts = s.clock()
		}
		hour := ts.UTC().Truncate(time.Hour)
		counters := statusCounters(obs.StatusCode)
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO ip_behavior_rollups (
				tenant_id, node_id, hour_ts, server_group, app, country_code, country, asn, isp, src_ip,
				request_count, bytes_out,
				status_301, status_401, status_403, status_404, status_429, status_500, status_502, status_503,
				status_2xx, status_3xx, status_4xx, status_5xx,
				first_seen_at, last_seen_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,
				1,$11,
				$12,$13,$14,$15,$16,$17,$18,$19,
				$20,$21,$22,$23,
				$24,$24
			)
			ON CONFLICT (tenant_id, node_id, hour_ts, server_group, app, country_code, asn, src_ip)
			DO UPDATE SET
				request_count = ip_behavior_rollups.request_count + 1,
				bytes_out = ip_behavior_rollups.bytes_out + EXCLUDED.bytes_out,
				status_301 = ip_behavior_rollups.status_301 + EXCLUDED.status_301,
				status_401 = ip_behavior_rollups.status_401 + EXCLUDED.status_401,
				status_403 = ip_behavior_rollups.status_403 + EXCLUDED.status_403,
				status_404 = ip_behavior_rollups.status_404 + EXCLUDED.status_404,
				status_429 = ip_behavior_rollups.status_429 + EXCLUDED.status_429,
				status_500 = ip_behavior_rollups.status_500 + EXCLUDED.status_500,
				status_502 = ip_behavior_rollups.status_502 + EXCLUDED.status_502,
				status_503 = ip_behavior_rollups.status_503 + EXCLUDED.status_503,
				status_2xx = ip_behavior_rollups.status_2xx + EXCLUDED.status_2xx,
				status_3xx = ip_behavior_rollups.status_3xx + EXCLUDED.status_3xx,
				status_4xx = ip_behavior_rollups.status_4xx + EXCLUDED.status_4xx,
				status_5xx = ip_behavior_rollups.status_5xx + EXCLUDED.status_5xx,
				country = CASE WHEN ip_behavior_rollups.country = '' THEN EXCLUDED.country ELSE ip_behavior_rollups.country END,
				isp = CASE WHEN ip_behavior_rollups.isp = '' THEN EXCLUDED.isp ELSE ip_behavior_rollups.isp END,
				last_seen_at = GREATEST(ip_behavior_rollups.last_seen_at, EXCLUDED.last_seen_at)
		`, obs.TenantID, obs.NodeID, hour, cleanDim(obs.ServerGroup), cleanDim(obs.App), cleanCountryCode(obs.CountryCode),
			cleanDim(obs.Country), cleanDim(obs.ASN), cleanDim(obs.ISP), obs.SourceIP, obs.BytesOut,
			counters["301"], counters["401"], counters["403"], counters["404"], counters["429"], counters["500"], counters["502"], counters["503"],
			counters["2xx"], counters["3xx"], counters["4xx"], counters["5xx"], ts.UTC())
		if err != nil {
			return fmt.Errorf("increment ip behavior rollup: %w", err)
		}
	}
	return nil
}

func (s *Store) ListIPBehaviorCountries(ctx context.Context, tenantID uuid.UUID, since time.Time, countryCode string) ([]IPBehaviorCountrySummary, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	where := []string{"tenant_id = $1", "hour_ts >= $2"}
	args := []any{tenantID, since}
	if countryCode = cleanCountryCode(countryCode); countryCode != "" {
		args = append(args, countryCode)
		where = append(where, fmt.Sprintf("country_code = $%d", len(args)))
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH filtered AS (
			SELECT *
			FROM ip_behavior_rollups
			WHERE `+strings.Join(where, " AND ")+`
		),
		country_rows AS (
			SELECT country_code, MAX(country) AS country, COUNT(DISTINCT src_ip) AS unique_source_ips,
			       SUM(request_count) AS request_count, SUM(bytes_out) AS bytes_out,
			       SUM(status_301) AS status_301, SUM(status_401) AS status_401, SUM(status_403) AS status_403,
			       SUM(status_404) AS status_404, SUM(status_429) AS status_429,
			       SUM(status_500) AS status_500, SUM(status_502) AS status_502, SUM(status_503) AS status_503,
			       SUM(status_2xx) AS status_2xx, SUM(status_3xx) AS status_3xx, SUM(status_4xx) AS status_4xx,
			       SUM(status_5xx) AS status_5xx, MIN(first_seen_at) AS first_seen_at, MAX(last_seen_at) AS last_seen_at
			FROM filtered
			GROUP BY country_code
		)
		SELECT cr.country_code, cr.country, cr.unique_source_ips, cr.request_count, cr.bytes_out,
		       cr.status_301, cr.status_401, cr.status_403, cr.status_404, cr.status_429,
		       cr.status_500, cr.status_502, cr.status_503, cr.status_2xx, cr.status_3xx, cr.status_4xx, cr.status_5xx,
		       cr.first_seen_at, cr.last_seen_at,
		       COALESCE((
			       SELECT ARRAY_AGG(rank_rows.asn)
			       FROM (
				       SELECT asn FROM filtered f
				       WHERE f.country_code = cr.country_code AND asn <> ''
				       GROUP BY asn
				       ORDER BY SUM(request_count) DESC, SUM(bytes_out) DESC
				       LIMIT 5
			       ) rank_rows
		       ), ARRAY[]::text[]) AS top_asns,
		       COALESCE((
			       SELECT ARRAY_AGG(rank_rows.app)
			       FROM (
				       SELECT app FROM filtered f
				       WHERE f.country_code = cr.country_code AND app <> ''
				       GROUP BY app
				       ORDER BY SUM(request_count) DESC, SUM(bytes_out) DESC
				       LIMIT 5
			       ) rank_rows
		       ), ARRAY[]::text[]) AS top_apps,
		       COALESCE((
			       SELECT ARRAY_AGG(rank_rows.server_group)
			       FROM (
				       SELECT server_group FROM filtered f
				       WHERE f.country_code = cr.country_code AND server_group <> ''
				       GROUP BY server_group
				       ORDER BY SUM(request_count) DESC, SUM(bytes_out) DESC
				       LIMIT 5
			       ) rank_rows
		       ), ARRAY[]::text[]) AS server_groups
		FROM country_rows cr
		ORDER BY cr.request_count DESC, cr.bytes_out DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query ip behavior countries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBehaviorCountrySummary
	for rows.Next() {
		var r IPBehaviorCountrySummary
		var s301, s401, s403, s404, s429, s500, s502, s503, s2xx, s3xx, s4xx, s5xx int64
		if err := rows.Scan(&r.CountryCode, &r.Country, &r.UniqueSourceIPs, &r.RequestCount, &r.BytesOut,
			&s301, &s401, &s403, &s404, &s429, &s500, &s502, &s503, &s2xx, &s3xx, &s4xx, &s5xx,
			&r.FirstSeenAt, &r.LastSeenAt, pq.Array(&r.TopASNs), pq.Array(&r.TopApps), pq.Array(&r.ServerGroups)); err != nil {
			return nil, err
		}
		r.StatusCounts = map[string]int64{
			"301": s301, "401": s401, "403": s403, "404": s404, "429": s429,
			"500": s500, "502": s502, "503": s503, "2xx": s2xx, "3xx": s3xx, "4xx": s4xx, "5xx": s5xx,
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetIPBehaviorIPProfile(ctx context.Context, tenantID uuid.UUID, sourceIP string, since time.Time) (*IPBehaviorIPProfile, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || net.ParseIP(sourceIP) == nil {
		return nil, errors.New("tenant_id and valid source_ip required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT src_ip::text,
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(country_code, '')), NULL),
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(asn, '')), NULL),
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(isp, '')), NULL),
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(app, '')), NULL),
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(server_group, '')), NULL),
		       ARRAY_AGG(DISTINCT node_id::text),
		       SUM(request_count), SUM(bytes_out),
		       SUM(status_301), SUM(status_401), SUM(status_403), SUM(status_404), SUM(status_429),
		       SUM(status_500), SUM(status_502), SUM(status_503),
		       SUM(status_2xx), SUM(status_3xx), SUM(status_4xx), SUM(status_5xx),
		       MIN(first_seen_at), MAX(last_seen_at)
		FROM ip_behavior_rollups
		WHERE tenant_id = $1 AND src_ip = $2 AND hour_ts >= $3
		GROUP BY src_ip
	`, tenantID, sourceIP, since)
	var p IPBehaviorIPProfile
	var s301, s401, s403, s404, s429, s500, s502, s503, s2xx, s3xx, s4xx, s5xx int64
	if err := row.Scan(&p.SourceIP, pq.Array(&p.Countries), pq.Array(&p.ASNs), pq.Array(&p.ISPs), pq.Array(&p.Apps), pq.Array(&p.ServerGroups), pq.Array(&p.NodeIDs), &p.RequestCount, &p.BytesOut,
		&s301, &s401, &s403, &s404, &s429, &s500, &s502, &s503, &s2xx, &s3xx, &s4xx, &s5xx, &p.FirstSeenAt, &p.LastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	p.StatusCounts = map[string]int64{
		"301": s301, "401": s401, "403": s403, "404": s404, "429": s429,
		"500": s500, "502": s502, "503": s503, "2xx": s2xx, "3xx": s3xx, "4xx": s4xx, "5xx": s5xx,
	}
	history, err := s.listIPBehaviorIPHistory(ctx, tenantID, sourceIP, since)
	if err != nil {
		return nil, err
	}
	p.History = history
	return &p, nil
}

func (s *Store) ListIPBehaviorSourceIPsByASN(ctx context.Context, tenantID uuid.UUID, asn string, since time.Time, limit int) ([]IPBehaviorASNSource, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || strings.TrimSpace(asn) == "" {
		return nil, errors.New("tenant_id and asn required")
	}
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_ip::text,
		       SUM(request_count) AS request_count,
		       SUM(bytes_out) AS bytes_out,
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(country_code, '')), NULL) AS countries,
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(app, '')), NULL) AS apps,
		       ARRAY_REMOVE(ARRAY_AGG(DISTINCT NULLIF(server_group, '')), NULL) AS server_groups,
		       MIN(first_seen_at) AS first_seen_at,
		       MAX(last_seen_at) AS last_seen_at
		FROM ip_behavior_rollups
		WHERE tenant_id = $1
		  AND LOWER(asn) = LOWER($2)
		  AND hour_ts >= $3
		GROUP BY src_ip
		ORDER BY SUM(request_count) DESC, SUM(bytes_out) DESC, MAX(last_seen_at) DESC
		LIMIT $4
	`, tenantID, strings.TrimSpace(asn), since.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query ip behavior asn sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBehaviorASNSource
	for rows.Next() {
		var src IPBehaviorASNSource
		if err := rows.Scan(&src.SourceIP, &src.RequestCount, &src.BytesOut, pq.Array(&src.Countries), pq.Array(&src.Apps), pq.Array(&src.ServerGroups), &src.FirstSeenAt, &src.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) listIPBehaviorIPHistory(ctx context.Context, tenantID uuid.UUID, sourceIP string, since time.Time) ([]IPBehaviorHistoryPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT hour_ts, SUM(request_count), SUM(bytes_out),
		       SUM(status_301), SUM(status_401), SUM(status_403), SUM(status_404), SUM(status_429),
		       SUM(status_500), SUM(status_502), SUM(status_503),
		       SUM(status_2xx), SUM(status_3xx), SUM(status_4xx), SUM(status_5xx)
		FROM ip_behavior_rollups
		WHERE tenant_id = $1 AND src_ip = $2 AND hour_ts >= $3
		GROUP BY hour_ts
		ORDER BY hour_ts
	`, tenantID, sourceIP, since)
	if err != nil {
		return nil, fmt.Errorf("query ip behavior ip history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBehaviorHistoryPoint
	for rows.Next() {
		var p IPBehaviorHistoryPoint
		var s301, s401, s403, s404, s429, s500, s502, s503, s2xx, s3xx, s4xx, s5xx int64
		if err := rows.Scan(&p.HourTS, &p.RequestCount, &p.BytesOut,
			&s301, &s401, &s403, &s404, &s429, &s500, &s502, &s503, &s2xx, &s3xx, &s4xx, &s5xx); err != nil {
			return nil, err
		}
		p.StatusCounts = map[string]int64{
			"301": s301, "401": s401, "403": s403, "404": s404, "429": s429,
			"500": s500, "502": s502, "503": s503, "2xx": s2xx, "3xx": s3xx, "4xx": s4xx, "5xx": s5xx,
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListIPBehaviorBaselines(ctx context.Context, tenantID uuid.UUID, dimension string, limit, offset int) ([]IPBehaviorBaseline, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant_id required")
	}
	where := []string{"tenant_id = $1", "signal_type LIKE 'ip_behavior.%'"}
	args := []any{tenantID}
	if dimension = strings.TrimSpace(dimension); dimension != "" {
		if !strings.HasPrefix(dimension, "ip_behavior.") {
			dimension = "ip_behavior." + dimension
		}
		args = append(args, dimension)
		where = append(where, fmt.Sprintf("signal_type = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM behavioral_baselines WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, signal_type, dimension, baseline, window_days, computed_at
		FROM behavioral_baselines
		WHERE `+whereSQL+fmt.Sprintf(" ORDER BY computed_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []IPBehaviorBaseline
	for rows.Next() {
		var b IPBehaviorBaseline
		var signalType string
		var raw []byte
		if err := rows.Scan(&b.ID, &b.TenantID, &signalType, &b.DimensionKey, &raw, &b.WindowDays, &b.ComputedAt); err != nil {
			return nil, 0, err
		}
		m, err := decodeJSONBMap(raw)
		if err != nil {
			return nil, 0, err
		}
		b.Dimension = strings.TrimPrefix(signalType, "ip_behavior.")
		b.Baseline = m
		b.SampleCount = int64FromAny(m["sample_count"])
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func (s *Store) AggregateIPBehaviorBaselineStats(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]IPBehaviorBaselineStats, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	query := `
WITH base AS (
	SELECT tenant_id, node_id, hour_ts, server_group, app, country_code, asn, src_ip::text AS src_ip,
	       request_count, bytes_out,
	       status_301, status_401, status_403, status_404, status_429,
	       status_500, status_502, status_503, status_2xx, status_3xx, status_4xx, status_5xx,
	       first_seen_at, last_seen_at
	FROM ip_behavior_rollups
	WHERE tenant_id = $1 AND hour_ts >= $2
),
dimensions AS (
	SELECT tenant_id, NULL::uuid AS node_id, 'ip_behavior.country_app' AS signal_type,
	       concat_ws('|', server_group, app, country_code) AS dimension,
	       hour_ts, request_count, bytes_out,
	       status_301, status_401, status_403, status_404, status_429,
	       status_500, status_502, status_503, status_2xx, status_3xx, status_4xx, status_5xx,
	       first_seen_at, last_seen_at
	FROM base WHERE country_code <> ''
	UNION ALL
	SELECT tenant_id, node_id, 'ip_behavior.node_country_app' AS signal_type,
	       concat_ws('|', app, country_code) AS dimension,
	       hour_ts, request_count, bytes_out,
	       status_301, status_401, status_403, status_404, status_429,
	       status_500, status_502, status_503, status_2xx, status_3xx, status_4xx, status_5xx,
	       first_seen_at, last_seen_at
	FROM base WHERE country_code <> ''
	UNION ALL
	SELECT tenant_id, NULL::uuid AS node_id, 'ip_behavior.asn_app' AS signal_type,
	       concat_ws('|', app, asn) AS dimension,
	       hour_ts, request_count, bytes_out,
	       status_301, status_401, status_403, status_404, status_429,
	       status_500, status_502, status_503, status_2xx, status_3xx, status_4xx, status_5xx,
	       first_seen_at, last_seen_at
	FROM base WHERE asn <> ''
	UNION ALL
	SELECT tenant_id, NULL::uuid AS node_id, 'ip_behavior.source_ip_app' AS signal_type,
	       concat_ws('|', app, src_ip) AS dimension,
	       hour_ts, request_count, bytes_out,
	       status_301, status_401, status_403, status_404, status_429,
	       status_500, status_502, status_503, status_2xx, status_3xx, status_4xx, status_5xx,
	       first_seen_at, last_seen_at
	FROM base
)
SELECT tenant_id, node_id, signal_type, dimension,
       COUNT(*)::bigint AS sample_count,
       COUNT(DISTINCT hour_ts)::bigint AS observed_hours,
       COALESCE(SUM(request_count), 0)::bigint AS total_requests,
       COALESCE(MIN(request_count), 0)::bigint AS request_min,
       COALESCE(AVG(request_count), 0)::double precision AS request_avg,
       COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY request_count), 0)::double precision AS request_p50,
       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY request_count), 0)::double precision AS request_p95,
       COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY request_count), 0)::double precision AS request_p99,
       COALESCE(MAX(request_count), 0)::bigint AS request_peak,
       COALESCE(MIN(bytes_out), 0)::bigint AS bytes_min,
       COALESCE(AVG(bytes_out), 0)::double precision AS bytes_avg,
       COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY bytes_out), 0)::double precision AS bytes_p50,
       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY bytes_out), 0)::double precision AS bytes_p95,
       COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY bytes_out), 0)::double precision AS bytes_p99,
       COALESCE(MAX(bytes_out), 0)::bigint AS bytes_peak,
       COALESCE(SUM(status_301), 0)::bigint, COALESCE(SUM(status_401), 0)::bigint,
       COALESCE(SUM(status_403), 0)::bigint, COALESCE(SUM(status_404), 0)::bigint,
       COALESCE(SUM(status_429), 0)::bigint, COALESCE(SUM(status_500), 0)::bigint,
       COALESCE(SUM(status_502), 0)::bigint, COALESCE(SUM(status_503), 0)::bigint,
       COALESCE(SUM(status_2xx), 0)::bigint, COALESCE(SUM(status_3xx), 0)::bigint,
       COALESCE(SUM(status_4xx), 0)::bigint, COALESCE(SUM(status_5xx), 0)::bigint,
       ARRAY_AGG(DISTINCT EXTRACT(HOUR FROM hour_ts)::bigint) AS active_hours,
       ARRAY_AGG(DISTINCT EXTRACT(DOW FROM hour_ts)::bigint) AS active_weekdays,
       ARRAY_AGG(DISTINCT CEIL(EXTRACT(DAY FROM hour_ts)::double precision / 7.0)::bigint) AS active_weeks_of_month,
       ARRAY_AGG(DISTINCT EXTRACT(MONTH FROM hour_ts)::bigint) AS active_months,
       MIN(first_seen_at), MAX(last_seen_at)
FROM dimensions
WHERE dimension <> ''
GROUP BY tenant_id, node_id, signal_type, dimension
HAVING SUM(request_count) >= 5
ORDER BY signal_type, dimension
`
	rows, err := s.db.QueryContext(ctx, query, tenantID, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("aggregate ip behavior baselines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBehaviorBaselineStats
	for rows.Next() {
		var st IPBehaviorBaselineStats
		var s301, s401, s403, s404, s429, s500, s502, s503, s2xx, s3xx, s4xx, s5xx int64
		var activeHours, activeWeekdays, activeWeeks, activeMonths pq.Int64Array
		if err := rows.Scan(&st.TenantID, &st.NodeID, &st.SignalType, &st.Dimension,
			&st.SampleCount, &st.ObservedHours, &st.TotalRequests,
			&st.RequestMin, &st.RequestAvg, &st.RequestP50, &st.RequestP95, &st.RequestP99, &st.RequestPeak,
			&st.BytesMin, &st.BytesAvg, &st.BytesP50, &st.BytesP95, &st.BytesP99, &st.BytesPeak,
			&s301, &s401, &s403, &s404, &s429, &s500, &s502, &s503, &s2xx, &s3xx, &s4xx, &s5xx,
			&activeHours, &activeWeekdays, &activeWeeks, &activeMonths, &st.FirstSeenAt, &st.LastSeenAt); err != nil {
			return nil, err
		}
		st.StatusCounts = map[string]int64{
			"301": s301, "401": s401, "403": s403, "404": s404, "429": s429,
			"500": s500, "502": s502, "503": s503, "2xx": s2xx, "3xx": s3xx, "4xx": s4xx, "5xx": s5xx,
		}
		st.ActiveHours = []int64(activeHours)
		st.ActiveWeekdays = []int64(activeWeekdays)
		st.ActiveWeeks = []int64(activeWeeks)
		st.ActiveMonths = []int64(activeMonths)
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) UpsertIPBehaviorFinding(ctx context.Context, p UpsertIPBehaviorFindingParams) (*IPBehaviorFinding, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.DedupKey) == "" {
		return nil, errors.New("tenant_id and dedup_key required")
	}
	if p.Category == "" {
		p.Category = "ip_behavior"
	}
	if p.Severity == "" {
		p.Severity = "medium"
	}
	if p.SeenAt.IsZero() {
		p.SeenAt = s.clock()
	}
	evidence, err := marshalJSONBMap(p.Evidence)
	if err != nil {
		return nil, err
	}
	var nodeArg any
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nodeArg = *p.NodeID
	}
	var ipArg any
	if net.ParseIP(p.SourceIP) != nil {
		ipArg = p.SourceIP
	}
	var id uuid.UUID
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO ip_behavior_findings (
			tenant_id, node_id, dedup_key, src_ip, country_code, asn, category, severity, score, reason, evidence,
			first_seen_at, last_seen_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,NOW(),NOW())
		ON CONFLICT (tenant_id, dedup_key) DO UPDATE SET
			node_id = COALESCE(ip_behavior_findings.node_id, EXCLUDED.node_id),
			severity = EXCLUDED.severity,
			score = GREATEST(ip_behavior_findings.score, EXCLUDED.score),
			reason = EXCLUDED.reason,
			evidence = EXCLUDED.evidence,
			last_seen_at = GREATEST(ip_behavior_findings.last_seen_at, EXCLUDED.last_seen_at),
			updated_at = NOW()
		RETURNING id
	`, p.TenantID, nodeArg, p.DedupKey, ipArg, cleanCountryCode(p.CountryCode), cleanDim(p.ASN),
		p.Category, p.Severity, p.Score, p.Reason, evidence, p.SeenAt.UTC()).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("upsert ip behavior finding: %w", err)
	}
	return s.GetIPBehaviorFinding(ctx, id)
}

func (s *Store) GetIPBehaviorFinding(ctx context.Context, id uuid.UUID) (*IPBehaviorFinding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, dedup_key, src_ip::text, country_code, asn, category, severity, score, status, reason,
		       evidence, first_seen_at, last_seen_at, created_at, updated_at
		FROM ip_behavior_findings WHERE id = $1
	`, id)
	return scanIPBehaviorFinding(row)
}

func (s *Store) ListIPBehaviorFindings(ctx context.Context, filter IPBehaviorFindingFilter, limit, offset int) ([]IPBehaviorFinding, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	where := []string{"TRUE"}
	args := []any{}
	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		where = append(where, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.Resolved != nil {
		if *filter.Resolved {
			where = append(where, "status IN ('resolved','suppressed')")
		} else {
			where = append(where, "status NOT IN ('resolved','suppressed')")
		}
	}
	if sourceIP := strings.TrimSpace(filter.SourceIP); sourceIP != "" {
		if net.ParseIP(sourceIP) == nil {
			return nil, 0, errors.New("valid source_ip required")
		}
		args = append(args, sourceIP)
		where = append(where, fmt.Sprintf("src_ip = $%d", len(args)))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since.UTC())
		where = append(where, fmt.Sprintf("last_seen_at >= $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_behavior_findings WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, dedup_key, src_ip::text, country_code, asn, category, severity, score, status, reason,
		       evidence, first_seen_at, last_seen_at, created_at, updated_at
		FROM ip_behavior_findings
		WHERE `+whereSQL+fmt.Sprintf(" ORDER BY last_seen_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []IPBehaviorFinding
	for rows.Next() {
		f, err := scanIPBehaviorFinding(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *f)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateIPBehaviorFindingStatus(ctx context.Context, id uuid.UUID, status string) (*IPBehaviorFinding, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "open", "proposed", "resolved", "suppressed":
	default:
		return nil, fmt.Errorf("invalid ip behavior finding status %q", status)
	}
	var updated uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		UPDATE ip_behavior_findings
		SET status = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id
	`, id, status).Scan(&updated); err != nil {
		return nil, fmt.Errorf("update ip behavior finding status: %w", err)
	}
	return s.GetIPBehaviorFinding(ctx, updated)
}

func scanIPBehaviorFinding(row scanner) (*IPBehaviorFinding, error) {
	var f IPBehaviorFinding
	var raw []byte
	if err := row.Scan(&f.ID, &f.TenantID, &f.NodeID, &f.DedupKey, &f.SourceIP, &f.CountryCode, &f.ASN, &f.Category,
		&f.Severity, &f.Score, &f.Status, &f.Reason, &raw, &f.FirstSeenAt, &f.LastSeenAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m, err := decodeJSONBMap(raw)
	if err != nil {
		return nil, err
	}
	f.Evidence = m
	return &f, nil
}

func (s *Store) CreateIPBlocklistEntry(ctx context.Context, p CreateIPBlocklistEntryParams) (*IPBlocklistEntry, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.IPCIDR) == "" {
		return nil, errors.New("tenant_id and ip_cidr required")
	}
	if p.Scope == "" {
		p.Scope = "node"
	}
	if p.TargetType == "" {
		p.TargetType = "node"
	}
	if p.Enforcement == "" {
		p.Enforcement = "firewall"
	}
	var findingArg, targetArg any
	if p.FindingID != nil && *p.FindingID != uuid.Nil {
		findingArg = *p.FindingID
	}
	if p.TargetID != nil && *p.TargetID != uuid.Nil {
		targetArg = *p.TargetID
	}
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO ip_blocklist_entries (
			tenant_id, finding_id, ip_cidr, scope, target_type, target_id, server_group, app, vhost, enforcement,
			protected_override, protected_override_reason, reason, score, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id
	`, p.TenantID, findingArg, strings.TrimSpace(p.IPCIDR), p.Scope, p.TargetType, targetArg, strings.TrimSpace(p.ServerGroup),
		strings.TrimSpace(p.App), strings.TrimSpace(p.VHost), p.Enforcement, p.ProtectedOverride, strings.TrimSpace(p.ProtectedOverrideReason), p.Reason, p.Score, p.ExpiresAt).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create ip blocklist entry: %w", err)
	}
	return s.GetIPBlocklistEntry(ctx, id)
}

func (s *Store) GetIPBlocklistEntry(ctx context.Context, id uuid.UUID) (*IPBlocklistEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, finding_id, entity_action_id, ip_cidr, scope, target_type, target_id, server_group, app, vhost, enforcement,
		       protected_override, protected_override_reason, status, reason, score,
		       expires_at, approved_by, approved_at, last_error, created_at, updated_at
		FROM ip_blocklist_entries WHERE id = $1
	`, id)
	var e IPBlocklistEntry
	if err := row.Scan(&e.ID, &e.TenantID, &e.FindingID, &e.EntityActionID, &e.IPCIDR, &e.Scope, &e.TargetType, &e.TargetID,
		&e.ServerGroup, &e.App, &e.VHost, &e.Enforcement, &e.ProtectedOverride, &e.ProtectedOverrideReason, &e.Status, &e.Reason, &e.Score, &e.ExpiresAt, &e.ApprovedBy, &e.ApprovedAt,
		&e.LastError, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (s *Store) GetIPBlocklistEntryByEntityAction(ctx context.Context, entityActionID uuid.UUID) (*IPBlocklistEntry, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if entityActionID == uuid.Nil {
		return nil, errors.New("entity_action_id required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, finding_id, entity_action_id, ip_cidr, scope, target_type, target_id, server_group, app, vhost, enforcement,
		       protected_override, protected_override_reason, status, reason, score,
		       expires_at, approved_by, approved_at, last_error, created_at, updated_at
		FROM ip_blocklist_entries
		WHERE entity_action_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, entityActionID)
	return scanIPBlocklistEntry(row)
}

func (s *Store) ListIPBlocklistEntries(ctx context.Context, filter IPBlocklistEntryFilter, limit, offset int) ([]IPBlocklistEntry, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"TRUE"}
	args := []any{}
	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		where = append(where, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.FindingID != uuid.Nil {
		args = append(args, filter.FindingID)
		where = append(where, fmt.Sprintf("finding_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.IPCIDR) != "" {
		args = append(args, strings.TrimSpace(filter.IPCIDR))
		where = append(where, fmt.Sprintf("ip_cidr = $%d", len(args)))
	}
	if strings.TrimSpace(filter.Status) != "" {
		args = append(args, strings.TrimSpace(filter.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(filter.ServerGroup) != "" {
		args = append(args, strings.TrimSpace(filter.ServerGroup))
		where = append(where, fmt.Sprintf("server_group = $%d", len(args)))
	}
	if strings.TrimSpace(filter.TargetType) != "" {
		args = append(args, strings.TrimSpace(filter.TargetType))
		where = append(where, fmt.Sprintf("target_type = $%d", len(args)))
	}
	if filter.TargetID != uuid.Nil {
		args = append(args, filter.TargetID)
		where = append(where, fmt.Sprintf("target_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.App) != "" {
		args = append(args, strings.TrimSpace(filter.App))
		where = append(where, fmt.Sprintf("app = $%d", len(args)))
	}
	if strings.TrimSpace(filter.VHost) != "" {
		args = append(args, strings.TrimSpace(filter.VHost))
		where = append(where, fmt.Sprintf("vhost = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_blocklist_entries WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ip blocklist entries: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, finding_id, entity_action_id, ip_cidr, scope, target_type, target_id, server_group, app, vhost, enforcement,
		       protected_override, protected_override_reason, status, reason, score,
		       expires_at, approved_by, approved_at, last_error, created_at, updated_at
		FROM ip_blocklist_entries
		WHERE `+whereSQL+fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list ip blocklist entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBlocklistEntry
	for rows.Next() {
		e, err := scanIPBlocklistEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *e)
	}
	return out, total, rows.Err()
}

func (s *Store) SetIPBlocklistEntryEntityAction(ctx context.Context, id, entityActionID uuid.UUID) (*IPBlocklistEntry, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil || entityActionID == uuid.Nil {
		return nil, errors.New("id and entity_action_id required")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE ip_blocklist_entries
		SET entity_action_id = $2, updated_at = NOW()
		WHERE id = $1
	`, id, entityActionID)
	if err != nil {
		return nil, fmt.Errorf("set ip blocklist entity action: %w", err)
	}
	return s.GetIPBlocklistEntry(ctx, id)
}

func (s *Store) UpdateIPBlocklistEntryStatus(ctx context.Context, id uuid.UUID, status string, approverID *uuid.UUID, errMsg string) (*IPBlocklistEntry, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var approver any
	if approverID != nil && *approverID != uuid.Nil {
		approver = *approverID
	}
	var lastErr any
	if strings.TrimSpace(errMsg) != "" {
		lastErr = errMsg
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE ip_blocklist_entries
		SET status = $2,
		    approved_by = COALESCE(approved_by, $3),
		    approved_at = CASE WHEN $3::uuid IS NULL THEN approved_at ELSE COALESCE(approved_at, NOW()) END,
		    last_error = $4,
		    updated_at = NOW()
		WHERE id = $1
	`, id, status, approver, lastErr)
	if err != nil {
		return nil, fmt.Errorf("update ip blocklist entry: %w", err)
	}
	return s.GetIPBlocklistEntry(ctx, id)
}

func (s *Store) CountRecentIPBlocklistEntries(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return 0, errors.New("tenant_id required")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ip_blocklist_entries
		WHERE tenant_id = $1
		  AND created_at >= $2
	`, tenantID, since.UTC()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent ip blocklist entries: %w", err)
	}
	return count, nil
}

func (s *Store) CountRecentIPBlocklistEntriesForServerGroup(ctx context.Context, tenantID uuid.UUID, serverGroup string, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return 0, errors.New("tenant_id required")
	}
	serverGroup = strings.TrimSpace(serverGroup)
	if serverGroup == "" {
		return 0, nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ip_blocklist_entries
		WHERE tenant_id = $1
		  AND server_group = $2
		  AND created_at >= $3
	`, tenantID, serverGroup, since.UTC()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent server-group ip blocklist entries: %w", err)
	}
	return count, nil
}

func (s *Store) CountRecentIPBlocklistEntriesGlobal(ctx context.Context, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ip_blocklist_entries
		WHERE created_at >= $1
	`, since.UTC()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent global ip blocklist entries: %w", err)
	}
	return count, nil
}

func (s *Store) ListExpiredIPBlocklistEntries(ctx context.Context, now time.Time, limit int) ([]IPBlocklistEntry, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, finding_id, entity_action_id, ip_cidr, scope, target_type, target_id, server_group, app, vhost, enforcement,
		       protected_override, protected_override_reason, status, reason, score,
		       expires_at, approved_by, approved_at, last_error, created_at, updated_at
		FROM ip_blocklist_entries
		WHERE expires_at IS NOT NULL
		  AND expires_at <= $1
		  AND status IN ('proposed','approved','canary','dispatching','active')
		ORDER BY expires_at ASC
		LIMIT $2
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired ip blocklist entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBlocklistEntry
	for rows.Next() {
		e, err := scanIPBlocklistEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) ListActiveIPBlocklistEntriesForNode(ctx context.Context, tenantID, nodeID uuid.UUID, now time.Time, limit int) ([]IPBlocklistEntry, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || nodeID == uuid.Nil {
		return nil, errors.New("tenant_id and node_id required")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, finding_id, entity_action_id, ip_cidr, scope, target_type, target_id, server_group, app, vhost, enforcement,
		       protected_override, protected_override_reason, status, reason, score,
		       expires_at, approved_by, approved_at, last_error, created_at, updated_at
		FROM ip_blocklist_entries
		WHERE tenant_id = $1
		  AND status IN ('approved','canary','dispatching','active')
		  AND (expires_at IS NULL OR expires_at > $3)
		  AND (
			(LOWER(target_type) = 'node' AND target_id = $2)
			OR LOWER(target_type) IN ('tenant','fleet')
			OR LOWER(scope) IN ('tenant','fleet')
		  )
		ORDER BY created_at ASC
		LIMIT $4
	`, tenantID, nodeID, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list active ip blocklist entries for node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IPBlocklistEntry
	for rows.Next() {
		e, err := scanIPBlocklistEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func scanIPBlocklistEntry(row scanner) (*IPBlocklistEntry, error) {
	var e IPBlocklistEntry
	if err := row.Scan(&e.ID, &e.TenantID, &e.FindingID, &e.EntityActionID, &e.IPCIDR, &e.Scope, &e.TargetType, &e.TargetID,
		&e.ServerGroup, &e.App, &e.VHost, &e.Enforcement, &e.ProtectedOverride, &e.ProtectedOverrideReason, &e.Status, &e.Reason, &e.Score, &e.ExpiresAt, &e.ApprovedBy, &e.ApprovedAt,
		&e.LastError, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListWebserverInstances(ctx context.Context, tenantID, nodeID uuid.UUID, limit, offset int) ([]WebserverInstance, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if nodeID != uuid.Nil {
		args = append(args, nodeID)
		where = append(where, fmt.Sprintf("node_id = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webserver_instances WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, kind, version, service_name, config_path, access_log_path, error_log_path,
		       vhosts, capabilities, observed_at, created_at, updated_at
		FROM webserver_instances
		WHERE `+whereSQL+fmt.Sprintf(" ORDER BY observed_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []WebserverInstance
	for rows.Next() {
		var w WebserverInstance
		var vhostsRaw, capsRaw []byte
		if err := rows.Scan(&w.ID, &w.TenantID, &w.NodeID, &w.Kind, &w.Version, &w.ServiceName, &w.ConfigPath,
			&w.AccessLogPath, &w.ErrorLogPath, &vhostsRaw, &capsRaw, &w.ObservedAt, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(vhostsRaw, &w.VHosts)
		caps, _ := decodeJSONBMap(capsRaw)
		w.Capabilities = caps
		out = append(out, w)
	}
	return out, total, rows.Err()
}

func (s *Store) GetWebserverInstance(ctx context.Context, id uuid.UUID) (*WebserverInstance, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, kind, version, service_name, config_path, access_log_path, error_log_path,
		       vhosts, capabilities, observed_at, created_at, updated_at
		FROM webserver_instances
		WHERE id = $1
	`, id)
	var w WebserverInstance
	var vhostsRaw, capsRaw []byte
	if err := row.Scan(&w.ID, &w.TenantID, &w.NodeID, &w.Kind, &w.Version, &w.ServiceName, &w.ConfigPath,
		&w.AccessLogPath, &w.ErrorLogPath, &vhostsRaw, &capsRaw, &w.ObservedAt, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(vhostsRaw, &w.VHosts)
	caps, _ := decodeJSONBMap(capsRaw)
	w.Capabilities = caps
	return &w, nil
}

func (s *Store) UpsertWebserverInstances(ctx context.Context, tenantID, nodeID uuid.UUID, instances []WebserverInstance) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || nodeID == uuid.Nil {
		return errors.New("tenant_id and node_id required")
	}
	for _, inst := range instances {
		kind := strings.TrimSpace(inst.Kind)
		if kind == "" {
			continue
		}
		vhosts, err := json.Marshal(inst.VHosts)
		if err != nil {
			return fmt.Errorf("marshal webserver vhosts: %w", err)
		}
		caps, err := marshalJSONBMap(inst.Capabilities)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO webserver_instances (
				tenant_id, node_id, kind, version, service_name, config_path, access_log_path, error_log_path,
				vhosts, capabilities, observed_at, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW(),NOW())
			ON CONFLICT (tenant_id, node_id, kind, service_name, config_path)
			DO UPDATE SET
				version = EXCLUDED.version,
				access_log_path = EXCLUDED.access_log_path,
				error_log_path = EXCLUDED.error_log_path,
				vhosts = EXCLUDED.vhosts,
				capabilities = EXCLUDED.capabilities,
				observed_at = NOW(),
				updated_at = NOW()
		`, tenantID, nodeID, kind, strings.TrimSpace(inst.Version), strings.TrimSpace(inst.ServiceName),
			strings.TrimSpace(inst.ConfigPath), strings.TrimSpace(inst.AccessLogPath), strings.TrimSpace(inst.ErrorLogPath),
			vhosts, caps)
		if err != nil {
			return fmt.Errorf("upsert webserver instance: %w", err)
		}
	}
	return nil
}

func (s *Store) CreateWebserverConfigAction(ctx context.Context, p CreateWebserverConfigActionParams) (*WebserverConfigAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	policy, err := marshalJSONBMap(p.Policy)
	if err != nil {
		return nil, err
	}
	var instanceArg, jobArg any
	if p.WebserverInstanceID != nil && *p.WebserverInstanceID != uuid.Nil {
		instanceArg = *p.WebserverInstanceID
	}
	if p.JobID != nil && *p.JobID != uuid.Nil {
		jobArg = *p.JobID
	}
	var id uuid.UUID
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO webserver_config_actions (tenant_id, node_id, webserver_instance_id, job_id, action, policy)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id
	`, p.TenantID, p.NodeID, instanceArg, jobArg, strings.TrimSpace(p.Action), policy).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create webserver config action: %w", err)
	}
	return s.GetWebserverConfigAction(ctx, id)
}

func (s *Store) GetWebserverConfigAction(ctx context.Context, id uuid.UUID) (*WebserverConfigAction, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM webserver_config_actions WHERE id = $1
	`, id)
	return scanWebserverConfigAction(row)
}

func (s *Store) ListWebserverConfigActions(ctx context.Context, tenantID, instanceID uuid.UUID, limit int) ([]WebserverConfigAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM webserver_config_actions
		WHERE tenant_id = $1
		  AND webserver_instance_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, tenantID, instanceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list webserver config actions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WebserverConfigAction
	for rows.Next() {
		action, err := scanWebserverConfigAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *action)
	}
	return out, rows.Err()
}

func (s *Store) GetWebserverConfigActionByJobID(ctx context.Context, jobID uuid.UUID) (*WebserverConfigAction, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM webserver_config_actions WHERE job_id = $1
	`, jobID)
	return scanWebserverConfigAction(row)
}

func (s *Store) ListPendingWebserverConfigActions(ctx context.Context, nodeID uuid.UUID) ([]WebserverConfigAction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM webserver_config_actions
		WHERE node_id = $1
		  AND job_id IS NOT NULL
		  AND (
			status = 'pending'
			OR (status = 'running' AND updated_at < NOW() - INTERVAL '5 minutes')
		  )
		ORDER BY created_at ASC
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WebserverConfigAction
	for rows.Next() {
		a, err := scanWebserverConfigAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) MarkWebserverConfigActionStatus(ctx context.Context, jobID uuid.UUID, status string, result map[string]any, errMsg string) error {
	resultJSON, err := marshalJSONBMap(result)
	if err != nil {
		return err
	}
	var errArg any
	if strings.TrimSpace(errMsg) != "" {
		errArg = errMsg
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE webserver_config_actions
		SET status = $2, result = $3, error_message = $4, updated_at = NOW()
		WHERE job_id = $1
	`, jobID, status, resultJSON, errArg)
	return err
}

func (s *Store) CountRecentFailedWebserverConfigActions(ctx context.Context, tenantID, nodeID uuid.UUID, action string, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || nodeID == uuid.Nil {
		return 0, errors.New("tenant_id and node_id required")
	}
	where := []string{"tenant_id = $1", "node_id = $2", "status = 'failed'", "updated_at >= $3"}
	args := []any{tenantID, nodeID, since.UTC()}
	if strings.TrimSpace(action) != "" {
		args = append(args, strings.TrimSpace(action))
		where = append(where, fmt.Sprintf("action = $%d", len(args)))
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM webserver_config_actions
		WHERE `+strings.Join(where, " AND "), args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent failed webserver config actions: %w", err)
	}
	return count, nil
}

func (s *Store) CountRecentWebserverConfigActions(ctx context.Context, tenantID, nodeID uuid.UUID, action string, status string, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || nodeID == uuid.Nil {
		return 0, errors.New("tenant_id and node_id required")
	}
	where := []string{"tenant_id = $1", "node_id = $2", "updated_at >= $3"}
	args := []any{tenantID, nodeID, since.UTC()}
	if strings.TrimSpace(action) != "" {
		args = append(args, strings.TrimSpace(action))
		where = append(where, fmt.Sprintf("action = $%d", len(args)))
	}
	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM webserver_config_actions
		WHERE `+strings.Join(where, " AND "), args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count recent webserver config actions: %w", err)
	}
	return count, nil
}

func (s *Store) ListWebserverConfigActionsForBlockEntry(ctx context.Context, tenantID, blockEntryID uuid.UUID) ([]WebserverConfigAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || blockEntryID == uuid.Nil {
		return nil, errors.New("tenant_id and block_entry_id required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM webserver_config_actions
		WHERE tenant_id = $1
		  AND action = 'webserver.blocklist_update'
		  AND (
			policy->>'source_block_entry' = $2
			OR policy->'metadata'->>'ip_blocklist_entry_id' = $2
		  )
		ORDER BY created_at ASC
	`, tenantID, blockEntryID.String())
	if err != nil {
		return nil, fmt.Errorf("list webserver config actions for block entry: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WebserverConfigAction
	for rows.Next() {
		action, err := scanWebserverConfigAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *action)
	}
	return out, rows.Err()
}

func (s *Store) CreateWebserverConfigReceipt(ctx context.Context, p CreateWebserverConfigReceiptParams) (*WebserverConfigReceipt, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || p.NodeID == uuid.Nil {
		return nil, errors.New("tenant_id and node_id required")
	}
	metadata, err := marshalJSONBMap(p.Metadata)
	if err != nil {
		return nil, err
	}
	var instanceArg, actionArg any
	if p.WebserverInstanceID != nil && *p.WebserverInstanceID != uuid.Nil {
		instanceArg = *p.WebserverInstanceID
	}
	if p.ActionID != nil && *p.ActionID != uuid.Nil {
		actionArg = *p.ActionID
	}
	var id uuid.UUID
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO webserver_config_receipts (
			tenant_id, node_id, webserver_instance_id, action_id, action, checksum_before, checksum_after,
			validation_status, reload_status, rollback_ref, diff, metadata
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id
	`, p.TenantID, p.NodeID, instanceArg, actionArg, strings.TrimSpace(p.Action), strings.TrimSpace(p.ChecksumBefore),
		strings.TrimSpace(p.ChecksumAfter), strings.TrimSpace(p.ValidationStatus), strings.TrimSpace(p.ReloadStatus),
		strings.TrimSpace(p.RollbackRef), p.Diff, metadata).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create webserver config receipt: %w", err)
	}
	return s.GetWebserverConfigReceipt(ctx, id)
}

func (s *Store) GetWebserverConfigReceipt(ctx context.Context, id uuid.UUID) (*WebserverConfigReceipt, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, action_id, action, checksum_before, checksum_after,
		       validation_status, reload_status, rollback_ref, diff, metadata, created_at
		FROM webserver_config_receipts WHERE id = $1
	`, id)
	return scanWebserverConfigReceipt(row)
}

func (s *Store) ListWebserverConfigReceipts(ctx context.Context, tenantID, instanceID uuid.UUID, limit int) ([]WebserverConfigReceipt, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, webserver_instance_id, action_id, action, checksum_before, checksum_after,
		       validation_status, reload_status, rollback_ref, diff, metadata, created_at
		FROM webserver_config_receipts
		WHERE tenant_id = $1
		  AND webserver_instance_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, tenantID, instanceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list webserver config receipts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WebserverConfigReceipt
	for rows.Next() {
		receipt, err := scanWebserverConfigReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *receipt)
	}
	return out, rows.Err()
}

func scanWebserverConfigReceipt(row scanner) (*WebserverConfigReceipt, error) {
	var r WebserverConfigReceipt
	var raw []byte
	if err := row.Scan(&r.ID, &r.TenantID, &r.NodeID, &r.WebserverInstanceID, &r.ActionID, &r.Action,
		&r.ChecksumBefore, &r.ChecksumAfter, &r.ValidationStatus, &r.ReloadStatus, &r.RollbackRef, &r.Diff,
		&raw, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.Metadata, _ = decodeJSONBMap(raw)
	return &r, nil
}

func scanWebserverConfigAction(sc scanner) (*WebserverConfigAction, error) {
	var a WebserverConfigAction
	var policyRaw, resultRaw []byte
	if err := sc.Scan(&a.ID, &a.TenantID, &a.NodeID, &a.WebserverInstanceID, &a.JobID, &a.Action, &a.Status,
		&policyRaw, &resultRaw, &a.ErrorMessage, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	a.Policy, _ = decodeJSONBMap(policyRaw)
	a.Result, _ = decodeJSONBMap(resultRaw)
	return &a, nil
}

func statusCounters(code int) map[string]int64 {
	out := map[string]int64{"301": 0, "401": 0, "403": 0, "404": 0, "429": 0, "500": 0, "502": 0, "503": 0, "2xx": 0, "3xx": 0, "4xx": 0, "5xx": 0}
	switch code {
	case 301:
		out["301"] = 1
	case 401:
		out["401"] = 1
	case 403:
		out["403"] = 1
	case 404:
		out["404"] = 1
	case 429:
		out["429"] = 1
	case 500:
		out["500"] = 1
	case 502:
		out["502"] = 1
	case 503:
		out["503"] = 1
	}
	switch {
	case code >= 200 && code <= 299:
		out["2xx"] = 1
	case code >= 300 && code <= 399:
		out["3xx"] = 1
	case code >= 400 && code <= 499:
		out["4xx"] = 1
	case code >= 500 && code <= 599:
		out["5xx"] = 1
	}
	return out
}

func int64FromAny(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func cleanCountryCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func cleanDim(s string) string {
	return strings.TrimSpace(s)
}
