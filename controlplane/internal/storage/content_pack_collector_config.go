package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

const (
	ContentPackCollectorConfigStatusRendered   = "rendered"
	ContentPackCollectorConfigStatusApproved   = "approved"
	ContentPackCollectorConfigStatusQueued     = "queued"
	ContentPackCollectorConfigStatusDeployed   = "deployed"
	ContentPackCollectorConfigStatusSuperseded = "superseded"
	ContentPackCollectorConfigStatusFailed     = "failed"
	ContentPackCollectorConfigStatusRolledBack = "rolled_back"
)

type ContentPackCollectorConfigCandidate struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
	RegistrySnapshotID    uuid.UUID
	Status                string
	ConfigVersion         string
	CollectorID           string
	Endpoint              string
	SourceIDs             []string
	Plan                  contentpacks.OTelCollectorConfigPlan
	RenderedYAML          string
	CreatedBySubject      string
	ApprovedBySubject     string
	ApprovalNote          string
	ReviewedConfigVersion string
	ReviewedYAMLSHA256    string
	ApprovedAt            *time.Time
	QueuedBySubject       string
	QueueNote             string
	TargetCollectorID     string
	QueuedAt              *time.Time
	DeployedAt            *time.Time
	FailedAt              *time.Time
	DeploymentError       string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type CreateContentPackCollectorConfigCandidateParams struct {
	TenantID           uuid.UUID
	RegistrySnapshotID uuid.UUID
	ConfigVersion      string
	CollectorID        string
	Endpoint           string
	SourceIDs          []string
	Plan               contentpacks.OTelCollectorConfigPlan
	RenderedYAML       string
	CreatedBySubject   string
}

type ApproveContentPackCollectorConfigCandidateParams struct {
	TenantID              uuid.UUID
	CandidateID           uuid.UUID
	ApprovedBySubject     string
	ApprovalNote          string
	ReviewedConfigVersion string
}

type QueueContentPackCollectorConfigCandidateParams struct {
	TenantID              uuid.UUID
	CandidateID           uuid.UUID
	TargetCollectorID     string
	QueuedBySubject       string
	QueueNote             string
	ExpectedConfigVersion string
}

type QueueContentPackCollectorConfigRollbackParams struct {
	TenantID        uuid.UUID
	CollectorID     string
	CandidateID     uuid.UUID
	ConfigVersion   string
	QueuedBySubject string
	QueueNote       string
}

type RecordContentPackCollectorConfigApplyResultParams struct {
	TenantID      uuid.UUID
	CollectorID   string
	ConfigVersion string
	Status        string
	ErrorMessage  string
}

func (s *Store) CreateContentPackCollectorConfigCandidate(ctx context.Context, p CreateContentPackCollectorConfigCandidateParams) (*ContentPackCollectorConfigCandidate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.RegistrySnapshotID == uuid.Nil {
		return nil, errors.New("registry snapshot id is required")
	}
	planBytes, err := marshalContentPackCollectorConfigPlan(p.Plan, p.ConfigVersion, p.RenderedYAML)
	if err != nil {
		return nil, err
	}
	sourceIDs := normalizeContentPackCollectorSourceIDs(p.SourceIDs)
	if len(sourceIDs) == 0 {
		return nil, errors.New("at least one source id is required")
	}
	configVersion := strings.TrimSpace(p.ConfigVersion)
	collectorID := strings.TrimSpace(p.CollectorID)
	endpoint := strings.TrimSpace(p.Endpoint)
	renderedYAML := p.RenderedYAML
	createdBy := strings.TrimSpace(p.CreatedBySubject)

	var id uuid.UUID
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO content_pack_collector_config_candidates (
			tenant_id, registry_snapshot_id, status, config_version, collector_id, endpoint,
			source_ids, plan, rendered_yaml, created_by_subject, created_at, updated_at
		)
		VALUES ($1,$2,'rendered',$3,$4,$5,$6,$7::jsonb,$8,$9,NOW(),NOW())
		RETURNING id
	`, p.TenantID, p.RegistrySnapshotID, configVersion, collectorID, endpoint, pq.Array(sourceIDs), planBytes, renderedYAML, createdBy).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert content pack collector config candidate: %w", err)
	}
	return s.GetContentPackCollectorConfigCandidate(ctx, id)
}

func (s *Store) ApproveContentPackCollectorConfigCandidate(ctx context.Context, p ApproveContentPackCollectorConfigCandidateParams) (*ContentPackCollectorConfigCandidate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.CandidateID == uuid.Nil {
		return nil, errors.New("collector config candidate id is required")
	}
	approvedBy := strings.TrimSpace(p.ApprovedBySubject)
	if approvedBy == "" {
		return nil, errors.New("approved by subject is required")
	}
	note := strings.TrimSpace(p.ApprovalNote)
	reviewedConfigVersion := strings.TrimSpace(p.ReviewedConfigVersion)
	if reviewedConfigVersion == "" {
		return nil, errors.New("reviewed config version is required")
	}
	reviewedYAMLSHA256 := strings.TrimPrefix(reviewedConfigVersion, "sha256:")
	if reviewedYAMLSHA256 == reviewedConfigVersion || len(reviewedYAMLSHA256) != 64 {
		return nil, errors.New("reviewed config version must be a sha256: digest")
	}
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `
		UPDATE content_pack_collector_config_candidates
		SET status = 'approved',
		    approved_by_subject = $3,
		    approval_note = $4,
		    reviewed_config_version = $5,
		    reviewed_yaml_sha256 = $6,
		    approved_at = NOW(),
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND status = 'rendered'
		  AND config_version = $5
		RETURNING id
	`, p.TenantID, p.CandidateID, approvedBy, note, reviewedConfigVersion, reviewedYAMLSHA256).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("content pack collector config candidate is not rendered, not found, or reviewed config version does not match")
	}
	if err != nil {
		return nil, fmt.Errorf("approve content pack collector config candidate: %w", err)
	}
	return s.GetContentPackCollectorConfigCandidate(ctx, id)
}

func (s *Store) QueueContentPackCollectorConfigCandidate(ctx context.Context, p QueueContentPackCollectorConfigCandidateParams) (*ContentPackCollectorConfigCandidate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.CandidateID == uuid.Nil {
		return nil, errors.New("collector config candidate id is required")
	}
	queuedBy := strings.TrimSpace(p.QueuedBySubject)
	if queuedBy == "" {
		return nil, errors.New("queued by subject is required")
	}
	expectedConfigVersion := strings.TrimSpace(p.ExpectedConfigVersion)
	if expectedConfigVersion == "" {
		return nil, errors.New("expected config version is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin queue collector config candidate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var candidateCollectorID string
	var configVersion string
	var reviewedConfigVersion string
	err = tx.QueryRowContext(ctx, `
		SELECT collector_id, config_version, reviewed_config_version
		FROM content_pack_collector_config_candidates
		WHERE tenant_id = $1
		  AND id = $2
		  AND status = 'approved'
		FOR UPDATE
	`, p.TenantID, p.CandidateID).Scan(&candidateCollectorID, &configVersion, &reviewedConfigVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("content pack collector config candidate is not approved or not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load approved content pack collector config candidate: %w", err)
	}
	if expectedConfigVersion != configVersion {
		return nil, errors.New("expected config version does not match approved candidate")
	}
	if strings.TrimSpace(reviewedConfigVersion) != configVersion {
		return nil, errors.New("approved candidate has no matching reviewed config version")
	}

	targetCollectorID := strings.TrimSpace(p.TargetCollectorID)
	if targetCollectorID == "" {
		targetCollectorID = strings.TrimSpace(candidateCollectorID)
	}
	if targetCollectorID == "" {
		return nil, errors.New("target collector id is required")
	}
	var collectorRowID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		UPDATE content_pack_edge_collectors
		SET desired_config_version = $3,
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND collector_id = $2
		  AND status <> 'disabled'
		RETURNING id
	`, p.TenantID, targetCollectorID, configVersion).Scan(&collectorRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("target edge collector is not registered or is disabled")
	}
	if err != nil {
		return nil, fmt.Errorf("set edge collector desired config version: %w", err)
	}

	var id uuid.UUID
	err = tx.QueryRowContext(ctx, `
		UPDATE content_pack_collector_config_candidates
		SET status = 'queued',
		    target_collector_id = $3,
		    queued_by_subject = $4,
		    queue_note = $5,
		    queued_at = NOW(),
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND status = 'approved'
		RETURNING id
	`, p.TenantID, p.CandidateID, targetCollectorID, queuedBy, strings.TrimSpace(p.QueueNote)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("content pack collector config candidate is not approved or not found")
	}
	if err != nil {
		return nil, fmt.Errorf("queue content pack collector config candidate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit queue collector config candidate transaction: %w", err)
	}
	return s.GetContentPackCollectorConfigCandidate(ctx, id)
}

func (s *Store) QueueContentPackCollectorConfigRollback(ctx context.Context, p QueueContentPackCollectorConfigRollbackParams) (*ContentPackCollectorConfigCandidate, error) {
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
	queuedBy := strings.TrimSpace(p.QueuedBySubject)
	if queuedBy == "" {
		return nil, errors.New("queued by subject is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin queue collector config rollback transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id uuid.UUID
	var configVersion string
	switch {
	case p.CandidateID != uuid.Nil:
		err = tx.QueryRowContext(ctx, `
			SELECT id, config_version
			FROM content_pack_collector_config_candidates
			WHERE tenant_id = $1
			  AND id = $2
			  AND target_collector_id = $3
			  AND status = 'superseded'
			FOR UPDATE
		`, p.TenantID, p.CandidateID, collectorID).Scan(&id, &configVersion)
	case strings.TrimSpace(p.ConfigVersion) != "":
		err = tx.QueryRowContext(ctx, `
			SELECT id, config_version
			FROM content_pack_collector_config_candidates
			WHERE tenant_id = $1
			  AND target_collector_id = $2
			  AND config_version = $3
			  AND status = 'superseded'
			ORDER BY deployed_at DESC NULLS LAST, updated_at DESC
			LIMIT 1
			FOR UPDATE
		`, p.TenantID, collectorID, strings.TrimSpace(p.ConfigVersion)).Scan(&id, &configVersion)
	default:
		err = tx.QueryRowContext(ctx, `
			SELECT id, config_version
			FROM content_pack_collector_config_candidates
			WHERE tenant_id = $1
			  AND target_collector_id = $2
			  AND status = 'superseded'
			ORDER BY deployed_at DESC NULLS LAST, updated_at DESC
			LIMIT 1
			FOR UPDATE
		`, p.TenantID, collectorID).Scan(&id, &configVersion)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("rollback target config candidate is not superseded or not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load rollback target config candidate: %w", err)
	}

	var collectorRowID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		UPDATE content_pack_edge_collectors
		SET desired_config_version = $3,
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND collector_id = $2
		  AND status <> 'disabled'
		RETURNING id
	`, p.TenantID, collectorID, configVersion).Scan(&collectorRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("target edge collector is not registered or is disabled")
	}
	if err != nil {
		return nil, fmt.Errorf("set edge collector rollback desired config version: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE content_pack_collector_config_candidates
		SET status = 'queued',
		    queued_by_subject = $3,
		    queue_note = $4,
		    queued_at = NOW(),
		    failed_at = NULL,
		    deployment_error = '',
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND status = 'superseded'
		RETURNING id
	`, p.TenantID, id, queuedBy, strings.TrimSpace(p.QueueNote)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("rollback target config candidate is not superseded or not found")
	}
	if err != nil {
		return nil, fmt.Errorf("queue rollback config candidate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit queue collector config rollback transaction: %w", err)
	}
	return s.GetContentPackCollectorConfigCandidate(ctx, id)
}

func (s *Store) QueuedContentPackCollectorConfigForCollector(ctx context.Context, tenantID uuid.UUID, collectorID string) (*ContentPackCollectorConfigCandidate, error) {
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
	row := s.db.QueryRowContext(ctx, contentPackCollectorConfigCandidateSelectSQL+`
		WHERE tenant_id = $1
		  AND target_collector_id = $2
		  AND status = 'queued'
		ORDER BY queued_at DESC NULLS LAST, updated_at DESC
		LIMIT 1
	`, tenantID, collectorID)
	record, err := scanContentPackCollectorConfigCandidate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) RecordContentPackCollectorConfigApplyResult(ctx context.Context, p RecordContentPackCollectorConfigApplyResultParams) (*ContentPackCollectorConfigCandidate, error) {
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
	configVersion := strings.TrimSpace(p.ConfigVersion)
	if configVersion == "" {
		return nil, errors.New("config version is required")
	}
	status := strings.TrimSpace(strings.ToLower(p.Status))
	if status != ContentPackCollectorConfigStatusDeployed && status != ContentPackCollectorConfigStatusFailed {
		return nil, fmt.Errorf("unsupported collector config apply status %q", status)
	}
	errMsg := strings.TrimSpace(p.ErrorMessage)
	if status == ContentPackCollectorConfigStatusFailed && errMsg == "" {
		errMsg = "collector reported config apply failure"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin collector config apply result transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id uuid.UUID
	if status == ContentPackCollectorConfigStatusDeployed {
		err = tx.QueryRowContext(ctx, `
			UPDATE content_pack_collector_config_candidates
			SET status = 'deployed',
			    deployed_at = NOW(),
			    failed_at = NULL,
			    deployment_error = '',
			    updated_at = NOW()
			WHERE tenant_id = $1
			  AND target_collector_id = $2
			  AND config_version = $3
			  AND status = 'queued'
			RETURNING id
		`, p.TenantID, collectorID, configVersion).Scan(&id)
	} else {
		err = tx.QueryRowContext(ctx, `
			UPDATE content_pack_collector_config_candidates
			SET status = 'failed',
			    failed_at = NOW(),
			    deployment_error = $4,
			    updated_at = NOW()
			WHERE tenant_id = $1
			  AND target_collector_id = $2
			  AND config_version = $3
			  AND status = 'queued'
			RETURNING id
		`, p.TenantID, collectorID, configVersion, errMsg).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("queued collector config candidate is not found")
	}
	if err != nil {
		return nil, fmt.Errorf("record collector config apply result: %w", err)
	}

	if status == ContentPackCollectorConfigStatusDeployed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE content_pack_collector_config_candidates
			SET status = 'superseded',
			    updated_at = NOW()
			WHERE tenant_id = $1
			  AND target_collector_id = $2
			  AND id <> $3
			  AND status = 'deployed'
		`, p.TenantID, collectorID, id); err != nil {
			return nil, fmt.Errorf("supersede previous deployed collector configs: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE content_pack_edge_collectors
			SET running_config_version = $3,
			    status = 'healthy',
			    last_error = '',
			    updated_at = NOW()
			WHERE tenant_id = $1
			  AND collector_id = $2
		`, p.TenantID, collectorID, configVersion)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE content_pack_edge_collectors
			SET status = 'degraded',
			    last_error = $3,
			    updated_at = NOW()
			WHERE tenant_id = $1
			  AND collector_id = $2
		`, p.TenantID, collectorID, errMsg)
	}
	if err != nil {
		return nil, fmt.Errorf("update edge collector apply result: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit collector config apply result transaction: %w", err)
	}
	return s.GetContentPackCollectorConfigCandidate(ctx, id)
}

func (s *Store) GetContentPackCollectorConfigCandidate(ctx context.Context, id uuid.UUID) (*ContentPackCollectorConfigCandidate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("collector config candidate id is required")
	}
	row := s.db.QueryRowContext(ctx, contentPackCollectorConfigCandidateSelectSQL+` WHERE id = $1`, id)
	record, err := scanContentPackCollectorConfigCandidate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) ListContentPackCollectorConfigCandidates(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ContentPackCollectorConfigCandidate, int, error) {
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
		FROM content_pack_collector_config_candidates
		WHERE tenant_id = $1
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count content pack collector config candidates: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, contentPackCollectorConfigCandidateSelectSQL+`
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query content pack collector config candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackCollectorConfigCandidate, 0, limit)
	for rows.Next() {
		record, err := scanContentPackCollectorConfigCandidate(rows)
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

const contentPackCollectorConfigCandidateSelectSQL = `
	SELECT id, tenant_id, registry_snapshot_id, status, config_version, collector_id, endpoint,
	       source_ids, plan, rendered_yaml, created_by_subject, approved_by_subject,
	       approval_note, reviewed_config_version, reviewed_yaml_sha256, approved_at, queued_by_subject, queue_note, target_collector_id,
	       queued_at, deployed_at, failed_at, deployment_error, created_at, updated_at
	FROM content_pack_collector_config_candidates
`

func scanContentPackCollectorConfigCandidate(row scanner) (*ContentPackCollectorConfigCandidate, error) {
	var record ContentPackCollectorConfigCandidate
	var snapshotID sql.NullString
	var approvedAt sql.NullTime
	var queuedAt sql.NullTime
	var deployedAt sql.NullTime
	var failedAt sql.NullTime
	var sourceIDs []string
	var planRaw []byte
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&snapshotID,
		&record.Status,
		&record.ConfigVersion,
		&record.CollectorID,
		&record.Endpoint,
		pq.Array(&sourceIDs),
		&planRaw,
		&record.RenderedYAML,
		&record.CreatedBySubject,
		&record.ApprovedBySubject,
		&record.ApprovalNote,
		&record.ReviewedConfigVersion,
		&record.ReviewedYAMLSHA256,
		&approvedAt,
		&record.QueuedBySubject,
		&record.QueueNote,
		&record.TargetCollectorID,
		&queuedAt,
		&deployedAt,
		&failedAt,
		&record.DeploymentError,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if snapshotID.Valid {
		parsed, err := uuid.Parse(snapshotID.String)
		if err != nil {
			return nil, fmt.Errorf("parse registry_snapshot_id: %w", err)
		}
		record.RegistrySnapshotID = parsed
	}
	if approvedAt.Valid {
		t := approvedAt.Time
		record.ApprovedAt = &t
	}
	if queuedAt.Valid {
		t := queuedAt.Time
		record.QueuedAt = &t
	}
	if deployedAt.Valid {
		t := deployedAt.Time
		record.DeployedAt = &t
	}
	if failedAt.Valid {
		t := failedAt.Time
		record.FailedAt = &t
	}
	plan, err := decodeContentPackCollectorConfigPlan(planRaw, record.ConfigVersion, record.RenderedYAML)
	if err != nil {
		return nil, err
	}
	record.SourceIDs = normalizeContentPackCollectorSourceIDs(sourceIDs)
	record.Plan = plan
	return &record, nil
}

func marshalContentPackCollectorConfigPlan(plan contentpacks.OTelCollectorConfigPlan, version, renderedYAML string) ([]byte, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, errors.New("config version is required")
	}
	if strings.TrimSpace(renderedYAML) == "" {
		return nil, errors.New("rendered yaml is required")
	}
	if got := contentpacks.OTelCollectorConfigVersion([]byte(renderedYAML)); got != version {
		return nil, fmt.Errorf("config version %s does not match rendered yaml digest %s", version, got)
	}
	if len(plan.Config.Receivers) == 0 || len(plan.Config.Exporters) == 0 || len(plan.Config.Service.Pipelines) == 0 {
		return nil, errors.New("collector config plan is incomplete")
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("marshal collector config plan: %w", err)
	}
	return data, nil
}

func decodeContentPackCollectorConfigPlan(raw []byte, version, renderedYAML string) (contentpacks.OTelCollectorConfigPlan, error) {
	var plan contentpacks.OTelCollectorConfigPlan
	if len(raw) == 0 {
		return plan, errors.New("collector config plan is empty")
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return plan, fmt.Errorf("unmarshal collector config plan: %w", err)
	}
	if _, err := marshalContentPackCollectorConfigPlan(plan, version, renderedYAML); err != nil {
		return plan, err
	}
	return plan, nil
}

func normalizeContentPackCollectorSourceIDs(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
