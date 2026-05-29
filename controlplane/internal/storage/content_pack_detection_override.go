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

const (
	ContentPackDetectionOverrideStateEnabled    = "enabled"
	ContentPackDetectionOverrideStateDisabled   = "disabled"
	ContentPackDetectionOverrideStateSuppressed = "suppressed"
)

type ContentPackDetectionOverride struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	PackID           string
	PackVersion      string
	SourceID         string
	DetectionID      string
	State            string
	SuppressUntil    *time.Time
	Reason           string
	CreatedBySubject string
	UpdatedBySubject string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type UpsertContentPackDetectionOverrideParams struct {
	TenantID         uuid.UUID
	PackID           string
	PackVersion      string
	SourceID         string
	DetectionID      string
	State            string
	SuppressUntil    *time.Time
	Reason           string
	UpdatedBySubject string
}

type ContentPackDetectionOverrideFilter struct {
	PackID         string
	PackVersion    string
	SourceID       string
	DetectionID    string
	State          string
	IncludeExpired bool
}

func (s *Store) UpsertContentPackDetectionOverride(ctx context.Context, p UpsertContentPackDetectionOverrideParams) (*ContentPackDetectionOverride, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	record, err := normalizeContentPackDetectionOverride(p)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO content_pack_detection_overrides (
			tenant_id, pack_id, pack_version, source_id, detection_id, state,
			suppress_until, reason, created_by_subject, updated_by_subject, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,NOW(),NOW())
		ON CONFLICT (tenant_id, pack_id, pack_version, source_id, detection_id) DO UPDATE
		SET state = EXCLUDED.state,
		    suppress_until = EXCLUDED.suppress_until,
		    reason = EXCLUDED.reason,
		    updated_by_subject = EXCLUDED.updated_by_subject,
		    updated_at = NOW()
		RETURNING id
	`, record.TenantID, record.PackID, record.PackVersion, record.SourceID, record.DetectionID, record.State, nullTimePtr(record.SuppressUntil), record.Reason, record.UpdatedBySubject).Scan(&id); err != nil {
		return nil, fmt.Errorf("upsert content pack detection override: %w", err)
	}
	return s.getContentPackDetectionOverride(ctx, id)
}

func (s *Store) ListContentPackDetectionOverrides(ctx context.Context, tenantID uuid.UUID, filter ContentPackDetectionOverrideFilter, limit, offset int) ([]ContentPackDetectionOverride, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit <= 0 {
		limit = 500
	}
	if offset < 0 {
		return nil, 0, errors.New("offset must be non-negative")
	}
	filter.State = strings.ToLower(strings.TrimSpace(filter.State))
	if filter.State != "" {
		if _, err := normalizeContentPackDetectionOverrideState(filter.State); err != nil {
			return nil, 0, err
		}
	}
	args := []any{
		tenantID,
		strings.TrimSpace(filter.PackID),
		strings.TrimSpace(filter.PackVersion),
		strings.TrimSpace(filter.SourceID),
		strings.TrimSpace(filter.DetectionID),
		filter.State,
		filter.IncludeExpired,
	}
	where := `
		WHERE tenant_id = $1
		  AND ($2 = '' OR pack_id = $2)
		  AND ($3 = '' OR pack_version = $3)
		  AND ($4 = '' OR source_id = $4)
		  AND ($5 = '' OR detection_id = $5)
		  AND ($6 = '' OR state = $6)
		  AND ($7::bool OR state <> 'suppressed' OR suppress_until > NOW())
	`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_pack_detection_overrides `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count content pack detection overrides: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, contentPackDetectionOverrideSelectSQL+where+`
		ORDER BY pack_id, pack_version, source_id, detection_id
		LIMIT $8 OFFSET $9
	`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query content pack detection overrides: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]ContentPackDetectionOverride, 0, limit)
	for rows.Next() {
		record, err := scanContentPackDetectionOverride(rows)
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

func (s *Store) getContentPackDetectionOverride(ctx context.Context, id uuid.UUID) (*ContentPackDetectionOverride, error) {
	row := s.db.QueryRowContext(ctx, contentPackDetectionOverrideSelectSQL+` WHERE id = $1`, id)
	return scanContentPackDetectionOverride(row)
}

func normalizeContentPackDetectionOverride(p UpsertContentPackDetectionOverrideParams) (ContentPackDetectionOverride, error) {
	if p.TenantID == uuid.Nil {
		return ContentPackDetectionOverride{}, errors.New("tenant id is required")
	}
	state, err := normalizeContentPackDetectionOverrideState(p.State)
	if err != nil {
		return ContentPackDetectionOverride{}, err
	}
	packID := strings.TrimSpace(p.PackID)
	packVersion := strings.TrimSpace(p.PackVersion)
	detectionID := strings.TrimSpace(p.DetectionID)
	if packID == "" || packVersion == "" || detectionID == "" {
		return ContentPackDetectionOverride{}, errors.New("pack_id, pack_version, and detection_id are required")
	}
	var suppressUntil *time.Time
	if p.SuppressUntil != nil && !p.SuppressUntil.IsZero() {
		normalized := p.SuppressUntil.UTC()
		suppressUntil = &normalized
	}
	if state == ContentPackDetectionOverrideStateSuppressed {
		if suppressUntil == nil {
			return ContentPackDetectionOverride{}, errors.New("suppress_until is required for suppressed detections")
		}
		if !suppressUntil.After(time.Now().UTC()) {
			return ContentPackDetectionOverride{}, errors.New("suppress_until must be in the future")
		}
	} else {
		suppressUntil = nil
	}
	return ContentPackDetectionOverride{
		TenantID:         p.TenantID,
		PackID:           packID,
		PackVersion:      packVersion,
		SourceID:         strings.TrimSpace(p.SourceID),
		DetectionID:      detectionID,
		State:            state,
		SuppressUntil:    suppressUntil,
		Reason:           strings.TrimSpace(p.Reason),
		UpdatedBySubject: strings.TrimSpace(p.UpdatedBySubject),
	}, nil
}

func normalizeContentPackDetectionOverrideState(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ContentPackDetectionOverrideStateEnabled:
		return ContentPackDetectionOverrideStateEnabled, nil
	case ContentPackDetectionOverrideStateDisabled:
		return ContentPackDetectionOverrideStateDisabled, nil
	case ContentPackDetectionOverrideStateSuppressed:
		return ContentPackDetectionOverrideStateSuppressed, nil
	default:
		return "", fmt.Errorf("invalid detection override state %q", strings.TrimSpace(raw))
	}
}

const contentPackDetectionOverrideSelectSQL = `
	SELECT id, tenant_id, pack_id, pack_version, source_id, detection_id, state,
	       suppress_until, reason, created_by_subject, updated_by_subject, created_at, updated_at
	FROM content_pack_detection_overrides
`

func scanContentPackDetectionOverride(row scanner) (*ContentPackDetectionOverride, error) {
	var record ContentPackDetectionOverride
	var suppressUntil sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.PackID,
		&record.PackVersion,
		&record.SourceID,
		&record.DetectionID,
		&record.State,
		&suppressUntil,
		&record.Reason,
		&record.CreatedBySubject,
		&record.UpdatedBySubject,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if suppressUntil.Valid {
		t := suppressUntil.Time.UTC()
		record.SuppressUntil = &t
	}
	return &record, nil
}
