package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ContentPackEdgeCollectorKindOTel      = "otel"
	ContentPackEdgeCollectorKindAlloy     = "alloy"
	ContentPackEdgeCollectorKindFluentBit = "fluent_bit"
	ContentPackEdgeCollectorKindVector    = "vector"
	ContentPackEdgeCollectorKindNodeAgent = "node_agent"

	ContentPackEdgeCollectorStatusRegistered = "registered"
	ContentPackEdgeCollectorStatusHealthy    = "healthy"
	ContentPackEdgeCollectorStatusDegraded   = "degraded"
	ContentPackEdgeCollectorStatusStale      = "stale"
	ContentPackEdgeCollectorStatusDisabled   = "disabled"

	ContentPackEdgeCollectorTokenPrefix = "c1ec_"
)

type ContentPackEdgeCollector struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	CollectorID          string
	Kind                 string
	DisplayName          string
	Endpoint             string
	Version              string
	Status               string
	DesiredConfigVersion string
	RunningConfigVersion string
	AuthTokenHash        string
	TokenLastFour        string
	TokenIssuedAt        *time.Time
	Health               map[string]any
	LastError            string
	LastHeartbeatAt      *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type UpsertContentPackEdgeCollectorRegistrationParams struct {
	TenantID             uuid.UUID
	CollectorID          string
	Kind                 string
	DisplayName          string
	Endpoint             string
	Version              string
	DesiredConfigVersion string
}

type RecordContentPackEdgeCollectorHeartbeatParams struct {
	TenantID             uuid.UUID
	CollectorID          string
	Kind                 string
	Version              string
	Status               string
	DesiredConfigVersion string
	RunningConfigVersion string
	Health               map[string]any
	LastError            string
}

type RotateContentPackEdgeCollectorTokenParams struct {
	TenantID    uuid.UUID
	CollectorID string
}

type ContentPackEdgeCollectorToken struct {
	Collector ContentPackEdgeCollector
	Token     string
}

func (s *Store) UpsertContentPackEdgeCollectorRegistration(ctx context.Context, p UpsertContentPackEdgeCollectorRegistrationParams) (*ContentPackEdgeCollector, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	kind, err := normalizeContentPackEdgeCollectorKind(p.Kind)
	if err != nil {
		return nil, err
	}
	desiredConfigVersion := strings.TrimSpace(p.DesiredConfigVersion)
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO content_pack_edge_collectors (
			tenant_id, collector_id, kind, display_name, endpoint, version,
			status, desired_config_version, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,'registered',$7,NOW(),NOW())
		ON CONFLICT (tenant_id, collector_id) DO UPDATE
		SET kind = EXCLUDED.kind,
		    display_name = EXCLUDED.display_name,
		    endpoint = EXCLUDED.endpoint,
		    version = EXCLUDED.version,
		    desired_config_version = COALESCE(NULLIF(EXCLUDED.desired_config_version, ''), content_pack_edge_collectors.desired_config_version),
		    status = CASE
		        WHEN content_pack_edge_collectors.status = 'disabled' THEN 'disabled'
		        WHEN content_pack_edge_collectors.last_heartbeat_at IS NULL THEN 'registered'
		        ELSE content_pack_edge_collectors.status
		    END,
		    updated_at = NOW()
		RETURNING id
	`, p.TenantID, collectorID, kind, strings.TrimSpace(p.DisplayName), strings.TrimSpace(p.Endpoint), strings.TrimSpace(p.Version), desiredConfigVersion).Scan(&id); err != nil {
		return nil, fmt.Errorf("upsert content pack edge collector registration: %w", err)
	}
	return s.GetContentPackEdgeCollector(ctx, id)
}

func (s *Store) RecordContentPackEdgeCollectorHeartbeat(ctx context.Context, p RecordContentPackEdgeCollectorHeartbeatParams) (*ContentPackEdgeCollector, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	kind, err := normalizeContentPackEdgeCollectorKind(p.Kind)
	if err != nil {
		return nil, err
	}
	status, err := normalizeContentPackEdgeCollectorHeartbeatStatus(p.Status, p.LastError)
	if err != nil {
		return nil, err
	}
	healthBytes, err := marshalJSONBMap(p.Health)
	if err != nil {
		return nil, fmt.Errorf("marshal collector health: %w", err)
	}
	desiredConfigVersion := strings.TrimSpace(p.DesiredConfigVersion)
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO content_pack_edge_collectors (
			tenant_id, collector_id, kind, version, status, desired_config_version,
			running_config_version, health, last_error, last_heartbeat_at, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,NOW(),NOW(),NOW())
		ON CONFLICT (tenant_id, collector_id) DO UPDATE
		SET kind = EXCLUDED.kind,
		    version = COALESCE(NULLIF(EXCLUDED.version, ''), content_pack_edge_collectors.version),
		    status = CASE
		        WHEN content_pack_edge_collectors.status = 'disabled' THEN 'disabled'
		        ELSE EXCLUDED.status
		    END,
		    desired_config_version = COALESCE(NULLIF(EXCLUDED.desired_config_version, ''), content_pack_edge_collectors.desired_config_version),
		    running_config_version = EXCLUDED.running_config_version,
		    health = EXCLUDED.health,
		    last_error = EXCLUDED.last_error,
		    last_heartbeat_at = EXCLUDED.last_heartbeat_at,
		    updated_at = NOW()
		RETURNING id
	`, p.TenantID, collectorID, kind, strings.TrimSpace(p.Version), status, desiredConfigVersion, strings.TrimSpace(p.RunningConfigVersion), healthBytes, strings.TrimSpace(p.LastError)).Scan(&id); err != nil {
		return nil, fmt.Errorf("record content pack edge collector heartbeat: %w", err)
	}
	return s.GetContentPackEdgeCollector(ctx, id)
}

func (s *Store) GetContentPackEdgeCollector(ctx context.Context, id uuid.UUID) (*ContentPackEdgeCollector, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("collector row id is required")
	}
	row := s.db.QueryRowContext(ctx, contentPackEdgeCollectorSelectSQL+` WHERE id = $1`, id)
	record, err := scanContentPackEdgeCollector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) ListContentPackEdgeCollectors(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ContentPackEdgeCollector, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		return nil, 0, errors.New("offset must be non-negative")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM content_pack_edge_collectors
		WHERE tenant_id = $1
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count content pack edge collectors: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, contentPackEdgeCollectorSelectSQL+`
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query content pack edge collectors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackEdgeCollector, 0, limit)
	for rows.Next() {
		record, err := scanContentPackEdgeCollector(rows)
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

func (s *Store) RotateContentPackEdgeCollectorToken(ctx context.Context, p RotateContentPackEdgeCollectorTokenParams) (*ContentPackEdgeCollectorToken, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	token, err := newContentPackEdgeCollectorTokenSecret()
	if err != nil {
		return nil, err
	}
	tokenHash := contentPackEdgeCollectorTokenHash(token)
	tokenLastFour := contentPackEdgeCollectorTokenLastFour(token)
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		UPDATE content_pack_edge_collectors
		SET auth_token_hash = $3,
		    token_last_four = $4,
		    token_issued_at = NOW(),
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND collector_id = $2
		  AND status <> 'disabled'
		RETURNING id
	`, p.TenantID, collectorID, tokenHash, tokenLastFour).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("edge collector is not registered or is disabled")
		}
		return nil, fmt.Errorf("rotate content pack edge collector token: %w", err)
	}
	collector, err := s.GetContentPackEdgeCollector(ctx, id)
	if err != nil {
		return nil, err
	}
	if collector == nil {
		return nil, errors.New("edge collector is not registered or is disabled")
	}
	return &ContentPackEdgeCollectorToken{Collector: *collector, Token: token}, nil
}

func (s *Store) ValidateContentPackEdgeCollectorToken(ctx context.Context, tenantID uuid.UUID, collectorID, token string) (*ContentPackEdgeCollector, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	collectorID = strings.TrimSpace(collectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	token = strings.TrimSpace(token)
	if token == "" || !strings.HasPrefix(token, ContentPackEdgeCollectorTokenPrefix) {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, contentPackEdgeCollectorSelectSQL+`
		WHERE tenant_id = $1
		  AND collector_id = $2
		  AND auth_token_hash <> ''
		  AND status <> 'disabled'
	`, tenantID, collectorID)
	record, err := scanContentPackEdgeCollector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expected := contentPackEdgeCollectorTokenHash(token)
	if subtle.ConstantTimeCompare([]byte(record.AuthTokenHash), []byte(expected)) != 1 {
		return nil, nil
	}
	return record, nil
}

const contentPackEdgeCollectorSelectSQL = `
	SELECT id, tenant_id, collector_id, kind, display_name, endpoint, version, status,
	       desired_config_version, running_config_version, auth_token_hash, token_last_four,
	       token_issued_at, health, last_error, last_heartbeat_at, created_at, updated_at
	FROM content_pack_edge_collectors
`

func scanContentPackEdgeCollector(row scanner) (*ContentPackEdgeCollector, error) {
	var record ContentPackEdgeCollector
	var healthRaw []byte
	var tokenIssuedAt sql.NullTime
	var lastHeartbeat sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.CollectorID,
		&record.Kind,
		&record.DisplayName,
		&record.Endpoint,
		&record.Version,
		&record.Status,
		&record.DesiredConfigVersion,
		&record.RunningConfigVersion,
		&record.AuthTokenHash,
		&record.TokenLastFour,
		&tokenIssuedAt,
		&healthRaw,
		&record.LastError,
		&lastHeartbeat,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	health, err := decodeJSONBMap(healthRaw)
	if err != nil {
		return nil, fmt.Errorf("decode collector health: %w", err)
	}
	record.Health = health
	if tokenIssuedAt.Valid {
		t := tokenIssuedAt.Time
		record.TokenIssuedAt = &t
	}
	if lastHeartbeat.Valid {
		t := lastHeartbeat.Time
		record.LastHeartbeatAt = &t
	}
	return &record, nil
}

func newContentPackEdgeCollectorTokenSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate content pack edge collector token: %w", err)
	}
	return ContentPackEdgeCollectorTokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func contentPackEdgeCollectorTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func contentPackEdgeCollectorTokenLastFour(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}

func normalizeContentPackEdgeCollectorKind(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ContentPackEdgeCollectorKindOTel, nil
	}
	switch value {
	case ContentPackEdgeCollectorKindOTel,
		ContentPackEdgeCollectorKindAlloy,
		ContentPackEdgeCollectorKindFluentBit,
		ContentPackEdgeCollectorKindVector,
		ContentPackEdgeCollectorKindNodeAgent:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported collector kind %q", value)
	}
}

func normalizeContentPackEdgeCollectorHeartbeatStatus(status, lastError string) (string, error) {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		if strings.TrimSpace(lastError) != "" {
			return ContentPackEdgeCollectorStatusDegraded, nil
		}
		return ContentPackEdgeCollectorStatusHealthy, nil
	}
	switch status {
	case ContentPackEdgeCollectorStatusHealthy,
		ContentPackEdgeCollectorStatusDegraded,
		ContentPackEdgeCollectorStatusStale:
		return status, nil
	default:
		return "", fmt.Errorf("unsupported collector heartbeat status %q", status)
	}
}
