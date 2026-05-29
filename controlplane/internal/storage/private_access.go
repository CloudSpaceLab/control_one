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

	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

type PrivateAccessSnapshotRecord struct {
	ID          uuid.UUID                  `json:"id"`
	TenantID    uuid.UUID                  `json:"tenant_id"`
	Provider    privateaccess.ProviderKind `json:"provider"`
	AccountID   string                     `json:"account_id"`
	CollectedAt time.Time                  `json:"collected_at"`
	Snapshot    privateaccess.Snapshot     `json:"snapshot"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

type PrivateAccessExposureFindingRecord struct {
	ID         uuid.UUID                  `json:"id"`
	TenantID   uuid.UUID                  `json:"tenant_id"`
	Provider   privateaccess.ProviderKind `json:"provider,omitempty"`
	Type       string                     `json:"type"`
	Severity   string                     `json:"severity"`
	NodeID     *uuid.UUID                 `json:"node_id,omitempty"`
	ServiceID  *uuid.UUID                 `json:"service_id,omitempty"`
	Detail     string                     `json:"detail"`
	Evidence   []string                   `json:"evidence,omitempty"`
	ObservedAt time.Time                  `json:"observed_at"`
	ResolvedAt *time.Time                 `json:"resolved_at,omitempty"`
	CreatedAt  time.Time                  `json:"created_at"`
	UpdatedAt  time.Time                  `json:"updated_at"`
}

func (s *Store) UpsertPrivateAccessSnapshot(ctx context.Context, tenantID uuid.UUID, snapshot privateaccess.Snapshot) (*PrivateAccessSnapshotRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	snapshot = normalizePrivateAccessSnapshot(snapshot)
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if !validPrivateAccessProvider(snapshot.Provider) {
		return nil, fmt.Errorf("unsupported private-access provider %q", snapshot.Provider)
	}
	if snapshot.CollectedAt.IsZero() {
		snapshot.CollectedAt = s.clock().UTC()
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal private access snapshot: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO private_access_provider_snapshots (
			tenant_id, provider, account_id, collected_at, snapshot
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, provider, account_id) DO UPDATE SET
			collected_at = EXCLUDED.collected_at,
			snapshot = EXCLUDED.snapshot,
			updated_at = NOW()
		RETURNING id, tenant_id, provider, account_id, collected_at, snapshot, created_at, updated_at
	`, tenantID, snapshot.Provider, snapshot.AccountID, snapshot.CollectedAt, raw)
	return scanPrivateAccessSnapshot(row)
}

func (s *Store) ListPrivateAccessSnapshots(ctx context.Context, tenantID uuid.UUID) ([]PrivateAccessSnapshotRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, provider, account_id, collected_at, snapshot, created_at, updated_at
		FROM private_access_provider_snapshots
		WHERE tenant_id = $1
		ORDER BY updated_at DESC, provider ASC, account_id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list private access snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PrivateAccessSnapshotRecord
	for rows.Next() {
		record, err := scanPrivateAccessSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

func (s *Store) ReplacePrivateAccessExposureFindings(ctx context.Context, tenantID uuid.UUID, findings []privateaccess.ExposureFinding, observedAt time.Time) ([]PrivateAccessExposureFindingRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if observedAt.IsZero() {
		observedAt = s.clock().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin private access findings tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE private_access_exposure_findings
		SET resolved_at = $2, updated_at = NOW()
		WHERE tenant_id = $1 AND resolved_at IS NULL
	`, tenantID, observedAt); err != nil {
		return nil, fmt.Errorf("resolve previous private access findings: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO private_access_exposure_findings (
			tenant_id, provider, finding_type, severity, node_id, service_id, detail, evidence, observed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, provider, finding_type, severity, node_id, service_id, detail,
		          evidence, observed_at, resolved_at, created_at, updated_at
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare private access finding insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	out := make([]PrivateAccessExposureFindingRecord, 0, len(findings))
	for _, finding := range findings {
		normalized := normalizePrivateAccessExposureFinding(finding)
		if normalized.Type == "" {
			continue
		}
		evidence, err := json.Marshal(normalized.Evidence)
		if err != nil {
			return nil, fmt.Errorf("marshal private access finding evidence: %w", err)
		}
		record, err := scanPrivateAccessFinding(stmt.QueryRowContext(ctx,
			tenantID,
			nullableStringArg(string(normalized.Provider)),
			normalized.Type,
			normalized.Severity,
			parseOptionalUUID(normalized.NodeID),
			parseOptionalUUID(normalized.ServiceID),
			normalized.Detail,
			evidence,
			observedAt,
		))
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit private access findings tx: %w", err)
	}
	return out, nil
}

func (s *Store) ListPrivateAccessExposureFindings(ctx context.Context, tenantID uuid.UUID, openOnly bool, limit, offset int) ([]PrivateAccessExposureFindingRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where := "tenant_id = $1"
	if openOnly {
		where += " AND resolved_at IS NULL"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, provider, finding_type, severity, node_id, service_id, detail,
		       evidence, observed_at, resolved_at, created_at, updated_at
		FROM private_access_exposure_findings
		WHERE `+where+`
		ORDER BY observed_at DESC, severity DESC, finding_type ASC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list private access findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PrivateAccessExposureFindingRecord
	for rows.Next() {
		record, err := scanPrivateAccessFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

func (s *Store) GetPrivateAccessExposureFinding(ctx context.Context, tenantID, id uuid.UUID) (*PrivateAccessExposureFindingRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, errors.New("tenant_id and finding id are required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, provider, finding_type, severity, node_id, service_id, detail,
		       evidence, observed_at, resolved_at, created_at, updated_at
		FROM private_access_exposure_findings
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id)
	record, err := scanPrivateAccessFinding(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

func normalizePrivateAccessSnapshot(in privateaccess.Snapshot) privateaccess.Snapshot {
	in.Provider = privateaccess.ProviderKind(strings.ToLower(strings.TrimSpace(string(in.Provider))))
	in.AccountID = strings.TrimSpace(in.AccountID)
	if in.AccountID == "" {
		in.AccountID = "default"
	}
	return in
}

func normalizePrivateAccessExposureFinding(in privateaccess.ExposureFinding) privateaccess.ExposureFinding {
	in.Type = strings.TrimSpace(in.Type)
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	if in.Severity == "" {
		in.Severity = "info"
	}
	in.Provider = privateaccess.ProviderKind(strings.ToLower(strings.TrimSpace(string(in.Provider))))
	in.NodeID = strings.TrimSpace(in.NodeID)
	in.ServiceID = strings.TrimSpace(in.ServiceID)
	in.Detail = strings.TrimSpace(in.Detail)
	evidence := in.Evidence[:0]
	for _, item := range in.Evidence {
		item = strings.TrimSpace(item)
		if item != "" {
			evidence = append(evidence, item)
		}
	}
	in.Evidence = evidence
	return in
}

func validPrivateAccessProvider(provider privateaccess.ProviderKind) bool {
	return privateaccess.ValidProvider(provider)
}

func scanPrivateAccessSnapshot(row interface {
	Scan(dest ...any) error
}) (*PrivateAccessSnapshotRecord, error) {
	var out PrivateAccessSnapshotRecord
	var provider string
	var raw []byte
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&provider,
		&out.AccountID,
		&out.CollectedAt,
		&raw,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan private access snapshot: %w", err)
	}
	out.Provider = privateaccess.ProviderKind(provider)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.Snapshot); err != nil {
			return nil, fmt.Errorf("decode private access snapshot: %w", err)
		}
	}
	return &out, nil
}

func scanPrivateAccessFinding(row interface {
	Scan(dest ...any) error
}) (*PrivateAccessExposureFindingRecord, error) {
	var out PrivateAccessExposureFindingRecord
	var provider sql.NullString
	var nodeID, serviceID uuid.NullUUID
	var resolvedAt sql.NullTime
	var rawEvidence []byte
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&provider,
		&out.Type,
		&out.Severity,
		&nodeID,
		&serviceID,
		&out.Detail,
		&rawEvidence,
		&out.ObservedAt,
		&resolvedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan private access finding: %w", err)
	}
	if provider.Valid {
		out.Provider = privateaccess.ProviderKind(provider.String)
	}
	if nodeID.Valid {
		id := nodeID.UUID
		out.NodeID = &id
	}
	if serviceID.Valid {
		id := serviceID.UUID
		out.ServiceID = &id
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		out.ResolvedAt = &value
	}
	if len(rawEvidence) > 0 {
		if err := json.Unmarshal(rawEvidence, &out.Evidence); err != nil {
			return nil, fmt.Errorf("decode private access finding evidence: %w", err)
		}
	}
	return &out, nil
}

func parseOptionalUUID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return id
}

func nullableStringArg(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
