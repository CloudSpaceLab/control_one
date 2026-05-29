package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SIEMForwardingKindLoki          = "loki"
	SIEMForwardingKindElasticsearch = "elasticsearch"
	SIEMForwardingKindSplunkHEC     = "splunk_hec"
	SIEMForwardingKindSentinel      = "sentinel"

	SIEMForwardingDestinationStatusEnabled  = "enabled"
	SIEMForwardingDestinationStatusDisabled = "disabled"

	SIEMForwardingDeliveryStatusSucceeded = "succeeded"
	SIEMForwardingDeliveryStatusFailed    = "failed"
)

type SIEMForwardingDestination struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Name             string
	Kind             string
	Status           string
	URL              string
	Config           map[string]any
	CreatedBySubject string
	UpdatedBySubject string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type UpsertSIEMForwardingDestinationParams struct {
	TenantID         uuid.UUID
	Name             string
	Kind             string
	Status           string
	URL              string
	Config           map[string]any
	UpdatedBySubject string
}

type SIEMForwardingCheckpoint struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	DestinationID    uuid.UUID
	CursorAt         time.Time
	CursorLogID      uuid.UUID
	LastRecordAt     *time.Time
	LastSuccessAt    *time.Time
	LastError        string
	RecordsForwarded int64
	BatchesForwarded int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type RecordSIEMForwardingCheckpointParams struct {
	TenantID         uuid.UUID
	DestinationID    uuid.UUID
	CursorAt         time.Time
	CursorLogID      uuid.UUID
	LastRecordAt     *time.Time
	LastSuccessAt    *time.Time
	LastError        string
	RecordsForwarded int64
	BatchesForwarded int64
}

type SIEMForwardingDeliveryAttempt struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	DestinationID uuid.UUID
	Status        string
	RecordCount   int
	BatchStartAt  *time.Time
	BatchEndAt    *time.Time
	Error         string
	Details       map[string]any
	AttemptedAt   time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
}

type RecordSIEMForwardingDeliveryAttemptParams struct {
	TenantID      uuid.UUID
	DestinationID uuid.UUID
	Status        string
	RecordCount   int
	BatchStartAt  *time.Time
	BatchEndAt    *time.Time
	Error         string
	Details       map[string]any
	CompletedAt   *time.Time
}

func (s *Store) UpsertSIEMForwardingDestination(ctx context.Context, p UpsertSIEMForwardingDestinationParams) (*SIEMForwardingDestination, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	record, err := normalizeSIEMForwardingDestination(p)
	if err != nil {
		return nil, err
	}
	configJSON, err := marshalJSONBMap(record.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal SIEM forwarding config: %w", err)
	}
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO siem_forwarding_destinations (
			tenant_id, name, kind, status, url, config,
			created_by_subject, updated_by_subject, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$7,NOW(),NOW())
		ON CONFLICT (tenant_id, name) DO UPDATE
		SET kind = EXCLUDED.kind,
		    status = EXCLUDED.status,
		    url = EXCLUDED.url,
		    config = EXCLUDED.config,
		    updated_by_subject = EXCLUDED.updated_by_subject,
		    updated_at = NOW()
		RETURNING id
	`, record.TenantID, record.Name, record.Kind, record.Status, record.URL, configJSON, record.UpdatedBySubject).Scan(&id); err != nil {
		return nil, fmt.Errorf("upsert SIEM forwarding destination: %w", err)
	}
	return s.GetSIEMForwardingDestination(ctx, id)
}

func (s *Store) GetSIEMForwardingDestination(ctx context.Context, id uuid.UUID) (*SIEMForwardingDestination, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("destination id is required")
	}
	record, err := scanSIEMForwardingDestination(s.db.QueryRowContext(ctx, siemForwardingDestinationSelectSQL+` WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) ListSIEMForwardingDestinations(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]SIEMForwardingDestination, int, error) {
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
	normalizedStatus := strings.ToLower(strings.TrimSpace(status))
	if normalizedStatus != "" {
		if _, err := normalizeSIEMForwardingDestinationStatus(normalizedStatus); err != nil {
			return nil, 0, err
		}
	}
	args := []any{tenantID, normalizedStatus}
	where := `
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
	`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM siem_forwarding_destinations `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count SIEM forwarding destinations: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, siemForwardingDestinationSelectSQL+where+`
		ORDER BY updated_at DESC
		LIMIT $3 OFFSET $4
	`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query SIEM forwarding destinations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]SIEMForwardingDestination, 0, limit)
	for rows.Next() {
		record, err := scanSIEMForwardingDestination(rows)
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

func (s *Store) RecordSIEMForwardingCheckpoint(ctx context.Context, p RecordSIEMForwardingCheckpointParams) (*SIEMForwardingCheckpoint, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	record, err := normalizeSIEMForwardingCheckpoint(p)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO siem_forwarding_checkpoints (
			tenant_id, destination_id, cursor_at, cursor_log_id, last_record_at, last_success_at,
			last_error, records_forwarded, batches_forwarded, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
		ON CONFLICT (tenant_id, destination_id) DO UPDATE
		SET cursor_at = EXCLUDED.cursor_at,
		    cursor_log_id = EXCLUDED.cursor_log_id,
		    last_record_at = EXCLUDED.last_record_at,
		    last_success_at = COALESCE(EXCLUDED.last_success_at, siem_forwarding_checkpoints.last_success_at),
		    last_error = EXCLUDED.last_error,
		    records_forwarded = siem_forwarding_checkpoints.records_forwarded + EXCLUDED.records_forwarded,
		    batches_forwarded = siem_forwarding_checkpoints.batches_forwarded + EXCLUDED.batches_forwarded,
		    updated_at = NOW()
		RETURNING id
	`, record.TenantID, record.DestinationID, record.CursorAt, nullUUIDParam(record.CursorLogID), nullTimePtr(record.LastRecordAt), nullTimePtr(record.LastSuccessAt), record.LastError, record.RecordsForwarded, record.BatchesForwarded).Scan(&id); err != nil {
		return nil, fmt.Errorf("record SIEM forwarding checkpoint: %w", err)
	}
	return s.GetSIEMForwardingCheckpoint(ctx, record.TenantID, record.DestinationID)
}

func (s *Store) GetSIEMForwardingCheckpoint(ctx context.Context, tenantID, destinationID uuid.UUID) (*SIEMForwardingCheckpoint, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || destinationID == uuid.Nil {
		return nil, errors.New("tenant id and destination id are required")
	}
	record, err := scanSIEMForwardingCheckpoint(s.db.QueryRowContext(ctx, siemForwardingCheckpointSelectSQL+`
		WHERE tenant_id = $1 AND destination_id = $2
	`, tenantID, destinationID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) RecordSIEMForwardingDeliveryAttempt(ctx context.Context, p RecordSIEMForwardingDeliveryAttemptParams) (*SIEMForwardingDeliveryAttempt, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	record, err := normalizeSIEMForwardingDeliveryAttempt(p)
	if err != nil {
		return nil, err
	}
	detailsJSON, err := marshalJSONBMap(record.Details)
	if err != nil {
		return nil, fmt.Errorf("marshal SIEM forwarding delivery details: %w", err)
	}
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO siem_forwarding_delivery_attempts (
			tenant_id, destination_id, status, record_count, batch_start_at, batch_end_at,
			error, details, completed_at, attempted_at, created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,NOW(),NOW())
		RETURNING id
	`, record.TenantID, record.DestinationID, record.Status, record.RecordCount, nullTimePtr(record.BatchStartAt), nullTimePtr(record.BatchEndAt), record.Error, detailsJSON, nullTimePtr(record.CompletedAt)).Scan(&id); err != nil {
		return nil, fmt.Errorf("record SIEM forwarding delivery attempt: %w", err)
	}
	return s.getSIEMForwardingDeliveryAttempt(ctx, id)
}

func (s *Store) getSIEMForwardingDeliveryAttempt(ctx context.Context, id uuid.UUID) (*SIEMForwardingDeliveryAttempt, error) {
	record, err := scanSIEMForwardingDeliveryAttempt(s.db.QueryRowContext(ctx, siemForwardingDeliveryAttemptSelectSQL+` WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func normalizeSIEMForwardingDestination(p UpsertSIEMForwardingDestinationParams) (SIEMForwardingDestination, error) {
	if p.TenantID == uuid.Nil {
		return SIEMForwardingDestination{}, errors.New("tenant id is required")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return SIEMForwardingDestination{}, errors.New("destination name is required")
	}
	kind, err := normalizeSIEMForwardingKind(p.Kind)
	if err != nil {
		return SIEMForwardingDestination{}, err
	}
	status, err := normalizeSIEMForwardingDestinationStatus(p.Status)
	if err != nil {
		return SIEMForwardingDestination{}, err
	}
	endpoint := strings.TrimSpace(p.URL)
	if err := validateSIEMForwardingURL(endpoint); err != nil {
		return SIEMForwardingDestination{}, err
	}
	config, err := sanitizeSIEMForwardingConfig(p.Config)
	if err != nil {
		return SIEMForwardingDestination{}, err
	}
	if requiresSIEMForwardingCredentialRef(kind) && siemForwardingCredentialRef(config) == "" {
		return SIEMForwardingDestination{}, fmt.Errorf("%s destination requires credential_ref or secret_ref", kind)
	}
	return SIEMForwardingDestination{
		TenantID:         p.TenantID,
		Name:             name,
		Kind:             kind,
		Status:           status,
		URL:              endpoint,
		Config:           config,
		UpdatedBySubject: strings.TrimSpace(p.UpdatedBySubject),
	}, nil
}

func normalizeSIEMForwardingCheckpoint(p RecordSIEMForwardingCheckpointParams) (SIEMForwardingCheckpoint, error) {
	if p.TenantID == uuid.Nil || p.DestinationID == uuid.Nil {
		return SIEMForwardingCheckpoint{}, errors.New("tenant id and destination id are required")
	}
	if p.RecordsForwarded < 0 || p.BatchesForwarded < 0 {
		return SIEMForwardingCheckpoint{}, errors.New("forwarded counters must be non-negative")
	}
	cursor := p.CursorAt.UTC()
	if cursor.IsZero() {
		cursor = time.Unix(0, 0).UTC()
	}
	return SIEMForwardingCheckpoint{
		TenantID:         p.TenantID,
		DestinationID:    p.DestinationID,
		CursorAt:         cursor,
		CursorLogID:      p.CursorLogID,
		LastRecordAt:     utcTimePtr(p.LastRecordAt),
		LastSuccessAt:    utcTimePtr(p.LastSuccessAt),
		LastError:        strings.TrimSpace(p.LastError),
		RecordsForwarded: p.RecordsForwarded,
		BatchesForwarded: p.BatchesForwarded,
	}, nil
}

func normalizeSIEMForwardingDeliveryAttempt(p RecordSIEMForwardingDeliveryAttemptParams) (SIEMForwardingDeliveryAttempt, error) {
	if p.TenantID == uuid.Nil || p.DestinationID == uuid.Nil {
		return SIEMForwardingDeliveryAttempt{}, errors.New("tenant id and destination id are required")
	}
	status, err := normalizeSIEMForwardingDeliveryStatus(p.Status)
	if err != nil {
		return SIEMForwardingDeliveryAttempt{}, err
	}
	if p.RecordCount < 0 {
		return SIEMForwardingDeliveryAttempt{}, errors.New("record count must be non-negative")
	}
	start := utcTimePtr(p.BatchStartAt)
	end := utcTimePtr(p.BatchEndAt)
	if start != nil && end != nil && end.Before(*start) {
		return SIEMForwardingDeliveryAttempt{}, errors.New("batch_end_at cannot be before batch_start_at")
	}
	return SIEMForwardingDeliveryAttempt{
		TenantID:      p.TenantID,
		DestinationID: p.DestinationID,
		Status:        status,
		RecordCount:   p.RecordCount,
		BatchStartAt:  start,
		BatchEndAt:    end,
		Error:         strings.TrimSpace(p.Error),
		Details:       cloneSIEMForwardingMap(p.Details),
		CompletedAt:   utcTimePtr(p.CompletedAt),
	}, nil
}

func normalizeSIEMForwardingKind(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SIEMForwardingKindLoki:
		return SIEMForwardingKindLoki, nil
	case "elastic", SIEMForwardingKindElasticsearch:
		return SIEMForwardingKindElasticsearch, nil
	case "splunk", SIEMForwardingKindSplunkHEC:
		return SIEMForwardingKindSplunkHEC, nil
	case "azure_monitor", "azure_logs_ingestion", "log_analytics", "microsoft_sentinel", SIEMForwardingKindSentinel:
		return SIEMForwardingKindSentinel, nil
	default:
		return "", fmt.Errorf("unsupported SIEM forwarding kind %q", strings.TrimSpace(raw))
	}
}

func normalizeSIEMForwardingDestinationStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return SIEMForwardingDestinationStatusEnabled, nil
	case SIEMForwardingDestinationStatusEnabled:
		return SIEMForwardingDestinationStatusEnabled, nil
	case SIEMForwardingDestinationStatusDisabled:
		return SIEMForwardingDestinationStatusDisabled, nil
	default:
		return "", fmt.Errorf("unsupported SIEM forwarding destination status %q", strings.TrimSpace(raw))
	}
}

func normalizeSIEMForwardingDeliveryStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SIEMForwardingDeliveryStatusSucceeded:
		return SIEMForwardingDeliveryStatusSucceeded, nil
	case SIEMForwardingDeliveryStatusFailed:
		return SIEMForwardingDeliveryStatusFailed, nil
	default:
		return "", fmt.Errorf("unsupported SIEM forwarding delivery status %q", strings.TrimSpace(raw))
	}
}

func validateSIEMForwardingURL(raw string) error {
	if raw == "" {
		return errors.New("destination url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("destination url must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("destination url must use http or https")
	}
	return nil
}

func sanitizeSIEMForwardingConfig(config map[string]any) (map[string]any, error) {
	out := cloneSIEMForwardingMap(config)
	if out == nil {
		out = map[string]any{}
	}
	for key := range out {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
		if normalized == "" {
			delete(out, key)
			continue
		}
		if isRawSIEMForwardingSecretKey(normalized) {
			return nil, fmt.Errorf("SIEM forwarding config must store secret references, not raw %s", key)
		}
	}
	return out, nil
}

func isRawSIEMForwardingSecretKey(key string) bool {
	if strings.HasSuffix(key, "_ref") || strings.HasSuffix(key, "_reference") {
		return false
	}
	switch key {
	case "token", "api_key", "apikey", "password", "secret", "bearer_token", "authorization":
		return true
	default:
		return false
	}
}

func requiresSIEMForwardingCredentialRef(kind string) bool {
	return kind == SIEMForwardingKindElasticsearch || kind == SIEMForwardingKindSplunkHEC || kind == SIEMForwardingKindSentinel
}

func siemForwardingCredentialRef(config map[string]any) string {
	for _, key := range []string{"credential_ref", "secret_ref", "token_ref", "api_key_ref"} {
		if value := strings.TrimSpace(fmt.Sprint(config[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func nullUUIDParam(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func cloneSIEMForwardingMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

const siemForwardingDestinationSelectSQL = `
	SELECT id, tenant_id, name, kind, status, url, config,
	       created_by_subject, updated_by_subject, created_at, updated_at
	FROM siem_forwarding_destinations
`

func scanSIEMForwardingDestination(row scanner) (*SIEMForwardingDestination, error) {
	var record SIEMForwardingDestination
	var configRaw []byte
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.Name,
		&record.Kind,
		&record.Status,
		&record.URL,
		&configRaw,
		&record.CreatedBySubject,
		&record.UpdatedBySubject,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	config, err := decodeJSONBMap(configRaw)
	if err != nil {
		return nil, fmt.Errorf("decode SIEM forwarding config: %w", err)
	}
	record.Config = config
	return &record, nil
}

const siemForwardingCheckpointSelectSQL = `
	SELECT id, tenant_id, destination_id, cursor_at, cursor_log_id, last_record_at, last_success_at,
	       last_error, records_forwarded, batches_forwarded, created_at, updated_at
	FROM siem_forwarding_checkpoints
`

func scanSIEMForwardingCheckpoint(row scanner) (*SIEMForwardingCheckpoint, error) {
	var record SIEMForwardingCheckpoint
	var cursorLogID uuid.NullUUID
	var lastRecordAt sql.NullTime
	var lastSuccessAt sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.DestinationID,
		&record.CursorAt,
		&cursorLogID,
		&lastRecordAt,
		&lastSuccessAt,
		&record.LastError,
		&record.RecordsForwarded,
		&record.BatchesForwarded,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if lastRecordAt.Valid {
		t := lastRecordAt.Time
		record.LastRecordAt = &t
	}
	if cursorLogID.Valid {
		record.CursorLogID = cursorLogID.UUID
	}
	if lastSuccessAt.Valid {
		t := lastSuccessAt.Time
		record.LastSuccessAt = &t
	}
	return &record, nil
}

const siemForwardingDeliveryAttemptSelectSQL = `
	SELECT id, tenant_id, destination_id, status, record_count, batch_start_at, batch_end_at,
	       error, details, attempted_at, completed_at, created_at
	FROM siem_forwarding_delivery_attempts
`

func scanSIEMForwardingDeliveryAttempt(row scanner) (*SIEMForwardingDeliveryAttempt, error) {
	var record SIEMForwardingDeliveryAttempt
	var batchStartAt sql.NullTime
	var batchEndAt sql.NullTime
	var completedAt sql.NullTime
	var detailsRaw []byte
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.DestinationID,
		&record.Status,
		&record.RecordCount,
		&batchStartAt,
		&batchEndAt,
		&record.Error,
		&detailsRaw,
		&record.AttemptedAt,
		&completedAt,
		&record.CreatedAt,
	); err != nil {
		return nil, err
	}
	if batchStartAt.Valid {
		t := batchStartAt.Time
		record.BatchStartAt = &t
	}
	if batchEndAt.Valid {
		t := batchEndAt.Time
		record.BatchEndAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		record.CompletedAt = &t
	}
	details, err := decodeJSONBMap(detailsRaw)
	if err != nil {
		return nil, fmt.Errorf("decode SIEM forwarding delivery details: %w", err)
	}
	record.Details = details
	return &record, nil
}
