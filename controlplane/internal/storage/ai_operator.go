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

type AIInvestigationStatus string

const (
	AIInvestigationStatusOpen      AIInvestigationStatus = "open"
	AIInvestigationStatusReviewing AIInvestigationStatus = "reviewing"
	AIInvestigationStatusClosed    AIInvestigationStatus = "closed"
)

type AIInvestigation struct {
	ID               uuid.UUID             `json:"id"`
	TenantID         uuid.UUID             `json:"tenant_id"`
	NodeID           uuid.UUID             `json:"node_id,omitempty"`
	TriggerType      string                `json:"trigger_type"`
	TriggerEventType string                `json:"trigger_event_type"`
	TriggerDedupKey  string                `json:"trigger_dedup_key"`
	Severity         string                `json:"severity"`
	Summary          string                `json:"summary"`
	Evidence         json.RawMessage       `json:"evidence"`
	Status           AIInvestigationStatus `json:"status"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

type CreateAIInvestigationParams struct {
	TenantID         uuid.UUID
	NodeID           uuid.UUID
	TriggerType      string
	TriggerEventType string
	TriggerDedupKey  string
	Severity         string
	Summary          string
	Evidence         json.RawMessage
	Status           AIInvestigationStatus
}

type ListAIInvestigationsFilter struct {
	TenantID         uuid.UUID
	NodeID           uuid.UUID
	Status           AIInvestigationStatus
	TriggerType      string
	TriggerEventType string
}

type AIOperatorProposalStatus string

const (
	AIOperatorProposalStatusProposed AIOperatorProposalStatus = "proposed"
	AIOperatorProposalStatusApproved AIOperatorProposalStatus = "approved"
	AIOperatorProposalStatusDenied   AIOperatorProposalStatus = "denied"
	AIOperatorProposalStatusExpired  AIOperatorProposalStatus = "expired"
)

type AIOperatorProposal struct {
	ID           uuid.UUID                `json:"id"`
	TenantID     uuid.UUID                `json:"tenant_id"`
	NodeID       uuid.UUID                `json:"node_id,omitempty"`
	Action       string                   `json:"action"`
	Reason       string                   `json:"reason"`
	Status       AIOperatorProposalStatus `json:"status"`
	DryRun       bool                     `json:"dry_run"`
	ApprovalKind string                   `json:"approval_kind"`
	ApprovalPath string                   `json:"approval_path,omitempty"`
	SourceTool   string                   `json:"source_tool"`
	Metadata     json.RawMessage          `json:"metadata,omitempty"`
	CreatedBy    *uuid.UUID               `json:"created_by,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
}

type CreateAIOperatorProposalParams struct {
	TenantID     uuid.UUID
	NodeID       uuid.UUID
	Action       string
	Reason       string
	Status       AIOperatorProposalStatus
	DryRun       bool
	ApprovalKind string
	ApprovalPath string
	SourceTool   string
	Metadata     json.RawMessage
	CreatedBy    uuid.UUID
}

type ListAIOperatorProposalsFilter struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Status   AIOperatorProposalStatus
	Action   string
}

func (s *Store) CreateAIInvestigation(ctx context.Context, params CreateAIInvestigationParams) (*AIInvestigation, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	id := uuid.New()
	triggerType := strings.TrimSpace(params.TriggerType)
	if triggerType == "" {
		triggerType = "manual"
	}
	eventType := strings.TrimSpace(params.TriggerEventType)
	if eventType == "" {
		eventType = triggerType
	}
	dedupKey := strings.TrimSpace(params.TriggerDedupKey)
	if dedupKey == "" {
		dedupKey = id.String()
	}
	severity := strings.ToLower(strings.TrimSpace(params.Severity))
	if severity == "" {
		severity = "info"
	}
	summary := strings.TrimSpace(params.Summary)
	if summary == "" {
		summary = eventType
	}
	status := params.Status
	if status == "" {
		status = AIInvestigationStatusOpen
	}
	evidence := normalizeJSON(params.Evidence)

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_investigations (
			id, tenant_id, node_id, trigger_type, trigger_event_type,
			trigger_dedup_key, severity, summary, evidence, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
		ON CONFLICT (tenant_id, trigger_dedup_key)
		DO UPDATE SET
			trigger_event_type = EXCLUDED.trigger_event_type,
			severity           = EXCLUDED.severity,
			summary            = EXCLUDED.summary,
			evidence           = EXCLUDED.evidence,
			updated_at         = NOW()
		RETURNING id, tenant_id, node_id, trigger_type, trigger_event_type,
		          trigger_dedup_key, severity, summary, evidence, status,
		          created_at, updated_at
	`, id, params.TenantID, nullableUUID(params.NodeID), triggerType, eventType, dedupKey, severity, summary, []byte(evidence), string(status))

	return scanAIInvestigation(row)
}

func (s *Store) GetAIInvestigation(ctx context.Context, id uuid.UUID) (*AIInvestigation, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("investigation id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, trigger_type, trigger_event_type,
		       trigger_dedup_key, severity, summary, evidence, status,
		       created_at, updated_at
		FROM ai_investigations
		WHERE id = $1
	`, id)
	investigation, err := scanAIInvestigation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ai investigation: %w", err)
	}
	return investigation, nil
}

func (s *Store) ListAIInvestigations(ctx context.Context, filter ListAIInvestigationsFilter, limit, offset int) ([]AIInvestigation, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}
	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(filter.TriggerType) != "" {
		args = append(args, strings.TrimSpace(filter.TriggerType))
		clauses = append(clauses, fmt.Sprintf("trigger_type = $%d", len(args)))
	}
	if strings.TrimSpace(filter.TriggerEventType) != "" {
		args = append(args, strings.TrimSpace(filter.TriggerEventType))
		clauses = append(clauses, fmt.Sprintf("trigger_event_type = $%d", len(args)))
	}

	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM ai_investigations WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ai investigations: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, trigger_type, trigger_event_type,
		       trigger_dedup_key, severity, summary, evidence, status,
		       created_at, updated_at
		FROM ai_investigations
		WHERE %s
		ORDER BY created_at DESC
	`, where)
	pagedArgs := append([]any{}, args...)
	if limit > 0 {
		pagedArgs = append(pagedArgs, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(pagedArgs))
	}
	if offset > 0 {
		pagedArgs = append(pagedArgs, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(pagedArgs))
	}

	rows, err := s.db.QueryContext(ctx, query, pagedArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query ai investigations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []AIInvestigation{}
	for rows.Next() {
		row, err := scanAIInvestigation(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) CreateAIOperatorProposal(ctx context.Context, params CreateAIOperatorProposalParams) (*AIOperatorProposal, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	action := strings.TrimSpace(params.Action)
	reason := strings.TrimSpace(params.Reason)
	if action == "" || reason == "" {
		return nil, errors.New("action and reason are required")
	}

	id := uuid.New()
	status := params.Status
	if status == "" {
		status = AIOperatorProposalStatusProposed
	}
	approvalKind := strings.TrimSpace(params.ApprovalKind)
	if approvalKind == "" {
		approvalKind = "manual"
	}
	sourceTool := strings.TrimSpace(params.SourceTool)
	if sourceTool == "" {
		sourceTool = "operator_propose_action"
	}
	metadata := normalizeJSON(params.Metadata)

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_operator_proposals (
			id, tenant_id, node_id, action, reason, status, dry_run,
			approval_kind, approval_path, source_tool, metadata, created_by
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)
		RETURNING id, tenant_id, node_id, action, reason, status, dry_run,
		          approval_kind, approval_path, source_tool, metadata,
		          created_by, created_at
	`, id, params.TenantID, nullableUUID(params.NodeID), action, reason, string(status), true,
		approvalKind, strings.TrimSpace(params.ApprovalPath), sourceTool, []byte(metadata), nullableUUID(params.CreatedBy))

	return scanAIOperatorProposal(row)
}

func (s *Store) ListAIOperatorProposals(ctx context.Context, filter ListAIOperatorProposalsFilter, limit, offset int) ([]AIOperatorProposal, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}
	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(filter.Action) != "" {
		args = append(args, strings.TrimSpace(filter.Action))
		clauses = append(clauses, fmt.Sprintf("action = $%d", len(args)))
	}

	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM ai_operator_proposals WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ai operator proposals: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, action, reason, status, dry_run,
		       approval_kind, approval_path, source_tool, metadata,
		       created_by, created_at
		FROM ai_operator_proposals
		WHERE %s
		ORDER BY created_at DESC
	`, where)
	pagedArgs := append([]any{}, args...)
	if limit > 0 {
		pagedArgs = append(pagedArgs, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(pagedArgs))
	}
	if offset > 0 {
		pagedArgs = append(pagedArgs, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(pagedArgs))
	}

	rows, err := s.db.QueryContext(ctx, query, pagedArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query ai operator proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []AIOperatorProposal{}
	for rows.Next() {
		row, err := scanAIOperatorProposal(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func scanAIInvestigation(row interface {
	Scan(dest ...any) error
}) (*AIInvestigation, error) {
	var (
		out      AIInvestigation
		nodeID   sql.NullString
		evidence []byte
		status   string
	)
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&nodeID,
		&out.TriggerType,
		&out.TriggerEventType,
		&out.TriggerDedupKey,
		&out.Severity,
		&out.Summary,
		&evidence,
		&status,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if nodeID.Valid {
		if parsed, err := uuid.Parse(nodeID.String); err == nil {
			out.NodeID = parsed
		}
	}
	out.Evidence = json.RawMessage(evidence)
	out.Status = AIInvestigationStatus(status)
	return &out, nil
}

func scanAIOperatorProposal(row interface {
	Scan(dest ...any) error
}) (*AIOperatorProposal, error) {
	var (
		out       AIOperatorProposal
		nodeID    sql.NullString
		metadata  []byte
		status    string
		createdBy sql.NullString
	)
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&nodeID,
		&out.Action,
		&out.Reason,
		&status,
		&out.DryRun,
		&out.ApprovalKind,
		&out.ApprovalPath,
		&out.SourceTool,
		&metadata,
		&createdBy,
		&out.CreatedAt,
	); err != nil {
		return nil, err
	}
	if nodeID.Valid {
		if parsed, err := uuid.Parse(nodeID.String); err == nil {
			out.NodeID = parsed
		}
	}
	if createdBy.Valid {
		if parsed, err := uuid.Parse(createdBy.String); err == nil {
			out.CreatedBy = &parsed
		}
	}
	out.Status = AIOperatorProposalStatus(status)
	out.Metadata = json.RawMessage(metadata)
	return &out, nil
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}
