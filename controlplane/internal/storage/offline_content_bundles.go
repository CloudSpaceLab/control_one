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
)

type OfflineContentBundle struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	BundleID             string
	Version              string
	Sequence             int64
	Status               string
	PublicKeyFingerprint string
	Signature            string
	ManifestSHA256       string
	StoragePath          string
	Manifest             map[string]any
	Contents             []map[string]any
	Warnings             []string
	Error                string
	ImportedBy           uuid.NullUUID
	ImportedAt           time.Time
	IssuedAt             sql.NullTime
	ExpiresAt            sql.NullTime
	RollbackTo           uuid.NullUUID
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type RecordOfflineContentBundleParams struct {
	TenantID             uuid.UUID
	BundleID             string
	Version              string
	Sequence             int64
	Status               string
	PublicKeyFingerprint string
	Signature            string
	ManifestSHA256       string
	StoragePath          string
	Manifest             map[string]any
	Contents             []map[string]any
	Warnings             []string
	Error                string
	ImportedBy           *uuid.UUID
	ImportedAt           time.Time
	IssuedAt             time.Time
	ExpiresAt            time.Time
}

type OfflineContentBundleFilter struct {
	TenantID uuid.UUID
	BundleID string
	Status   string
}

type OfflineContentBundleAuditParams struct {
	TenantID    uuid.UUID
	BundleRowID *uuid.UUID
	Action      string
	Status      string
	Reason      string
	ActorID     *uuid.UUID
	Metadata    map[string]any
}

func (s *Store) ActiveOfflineContentBundle(ctx context.Context, tenantID uuid.UUID, bundleID string) (*OfflineContentBundle, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, offlineContentBundleSelectSQL+`
		WHERE tenant_id = $1 AND bundle_id = $2 AND status = 'active'
		ORDER BY sequence DESC LIMIT 1
	`, tenantID, strings.TrimSpace(bundleID))
	out, err := scanOfflineContentBundle(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

func (s *Store) ListOfflineContentBundles(ctx context.Context, filter OfflineContentBundleFilter, limit, offset int) ([]OfflineContentBundle, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	where := []string{"TRUE"}
	args := []any{}
	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		where = append(where, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.BundleID) != "" {
		args = append(args, strings.TrimSpace(filter.BundleID))
		where = append(where, fmt.Sprintf("bundle_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.Status) != "" {
		args = append(args, strings.TrimSpace(filter.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM offline_content_bundles WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, offlineContentBundleSelectSQL+`
		WHERE `+whereSQL+`
		ORDER BY imported_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []OfflineContentBundle{}
	for rows.Next() {
		row, err := scanOfflineContentBundle(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *row)
	}
	return out, total, rows.Err()
}

func (s *Store) RecordOfflineContentBundle(ctx context.Context, p RecordOfflineContentBundleParams) (*OfflineContentBundle, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.BundleID) == "" || strings.TrimSpace(p.Version) == "" || p.Sequence <= 0 {
		return nil, errors.New("tenant_id, bundle_id, version, and positive sequence required")
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.ImportedAt.IsZero() {
		p.ImportedAt = s.clock()
	}
	manifest, err := marshalJSONBMap(p.Manifest)
	if err != nil {
		return nil, err
	}
	contents, err := json.Marshal(p.Contents)
	if err != nil {
		return nil, err
	}
	warnings, err := json.Marshal(p.Warnings)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if p.Status == "active" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE offline_content_bundles
			SET status = 'superseded', updated_at = NOW()
			WHERE tenant_id = $1 AND bundle_id = $2 AND status = 'active'
		`, p.TenantID, strings.TrimSpace(p.BundleID)); err != nil {
			return nil, err
		}
	}
	id := uuid.New()
	var importedBy any
	if p.ImportedBy != nil && *p.ImportedBy != uuid.Nil {
		importedBy = *p.ImportedBy
	}
	var issuedAt, expiresAt any
	if !p.IssuedAt.IsZero() {
		issuedAt = p.IssuedAt.UTC()
	}
	if !p.ExpiresAt.IsZero() {
		expiresAt = p.ExpiresAt.UTC()
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO offline_content_bundles (
			id, tenant_id, bundle_id, version, sequence, status, public_key_fingerprint, signature,
			manifest_sha256, storage_path, manifest, contents, warnings, error, imported_by,
			imported_at, issued_at, expires_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,NOW(),NOW())
		ON CONFLICT (tenant_id, bundle_id, sequence) DO UPDATE SET
			status = EXCLUDED.status,
			public_key_fingerprint = EXCLUDED.public_key_fingerprint,
			signature = EXCLUDED.signature,
			manifest_sha256 = EXCLUDED.manifest_sha256,
			storage_path = EXCLUDED.storage_path,
			manifest = EXCLUDED.manifest,
			contents = EXCLUDED.contents,
			warnings = EXCLUDED.warnings,
			error = EXCLUDED.error,
			imported_by = EXCLUDED.imported_by,
			imported_at = EXCLUDED.imported_at,
			issued_at = EXCLUDED.issued_at,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
		RETURNING id
	`, id, p.TenantID, strings.TrimSpace(p.BundleID), strings.TrimSpace(p.Version), p.Sequence, p.Status,
		p.PublicKeyFingerprint, p.Signature, p.ManifestSHA256, p.StoragePath, manifest, contents, warnings,
		p.Error, importedBy, p.ImportedAt.UTC(), issuedAt, expiresAt).Scan(&id); err != nil {
		return nil, fmt.Errorf("record offline content bundle: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetOfflineContentBundle(ctx, id)
}

func (s *Store) GetOfflineContentBundle(ctx context.Context, id uuid.UUID) (*OfflineContentBundle, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, offlineContentBundleSelectSQL+` WHERE id = $1`, id)
	return scanOfflineContentBundle(row)
}

func (s *Store) MarkOfflineContentBundleStatus(ctx context.Context, tenantID, id uuid.UUID, status, errMsg string, rollbackTo *uuid.UUID) (*OfflineContentBundle, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var rollbackArg any
	if rollbackTo != nil && *rollbackTo != uuid.Nil {
		rollbackArg = *rollbackTo
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE offline_content_bundles
		SET status = $3, error = $4, rollback_to = $5, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id
	`, tenantID, id, strings.TrimSpace(status), strings.TrimSpace(errMsg), rollbackArg)
	var outID uuid.UUID
	if err := row.Scan(&outID); err != nil {
		return nil, err
	}
	return s.GetOfflineContentBundle(ctx, outID)
}

func (s *Store) RecordOfflineContentBundleAudit(ctx context.Context, p OfflineContentBundleAuditParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.Action) == "" {
		return errors.New("tenant_id and action required")
	}
	if p.Status == "" {
		p.Status = "ok"
	}
	metadata, err := marshalJSONBMap(p.Metadata)
	if err != nil {
		return err
	}
	var bundleArg, actorArg any
	if p.BundleRowID != nil && *p.BundleRowID != uuid.Nil {
		bundleArg = *p.BundleRowID
	}
	if p.ActorID != nil && *p.ActorID != uuid.Nil {
		actorArg = *p.ActorID
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO offline_content_bundle_audit (id, tenant_id, bundle_row_id, action, status, reason, actor_id, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
	`, uuid.New(), p.TenantID, bundleArg, strings.TrimSpace(p.Action), strings.TrimSpace(p.Status), strings.TrimSpace(p.Reason), actorArg, metadata)
	return err
}

const offlineContentBundleSelectSQL = `
	SELECT id, tenant_id, bundle_id, version, sequence, status, public_key_fingerprint, signature,
	       manifest_sha256, storage_path, manifest, contents, warnings, error, imported_by,
	       imported_at, issued_at, expires_at, rollback_to, created_at, updated_at
	FROM offline_content_bundles
`

func scanOfflineContentBundle(row scanner) (*OfflineContentBundle, error) {
	var b OfflineContentBundle
	var manifestRaw, contentsRaw, warningsRaw []byte
	if err := row.Scan(&b.ID, &b.TenantID, &b.BundleID, &b.Version, &b.Sequence, &b.Status,
		&b.PublicKeyFingerprint, &b.Signature, &b.ManifestSHA256, &b.StoragePath,
		&manifestRaw, &contentsRaw, &warningsRaw, &b.Error, &b.ImportedBy,
		&b.ImportedAt, &b.IssuedAt, &b.ExpiresAt, &b.RollbackTo, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	manifest, err := decodeJSONBMap(manifestRaw)
	if err != nil {
		return nil, err
	}
	b.Manifest = manifest
	if len(contentsRaw) > 0 {
		_ = json.Unmarshal(contentsRaw, &b.Contents)
	}
	if len(warningsRaw) > 0 {
		_ = json.Unmarshal(warningsRaw, &b.Warnings)
	}
	if b.Contents == nil {
		b.Contents = []map[string]any{}
	}
	if b.Warnings == nil {
		b.Warnings = []string{}
	}
	return &b, nil
}
