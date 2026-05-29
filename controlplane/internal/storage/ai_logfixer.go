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

type AILogFixerRun struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	NodeID               uuid.UUID
	InvestigationID      uuid.UUID
	ProposalID           uuid.UUID
	JobID                uuid.UUID
	ApprovalID           uuid.UUID
	TriggerEventType     string
	TriggerDedupKey      string
	ServiceKey           string
	Status               string
	RiskLevel            string
	InvestigationRequest json.RawMessage
	Diagnosis            json.RawMessage
	RemediationPlan      json.RawMessage
	Attempt              json.RawMessage
	Receipt              json.RawMessage
	Evidence             json.RawMessage
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type CreateAILogFixerRunParams struct {
	TenantID             uuid.UUID
	NodeID               uuid.UUID
	InvestigationID      uuid.UUID
	ProposalID           uuid.UUID
	JobID                uuid.UUID
	ApprovalID           uuid.UUID
	TriggerEventType     string
	TriggerDedupKey      string
	ServiceKey           string
	Status               string
	RiskLevel            string
	InvestigationRequest json.RawMessage
	Diagnosis            json.RawMessage
	RemediationPlan      json.RawMessage
	Attempt              json.RawMessage
	Receipt              json.RawMessage
	Evidence             json.RawMessage
}

type ListAILogFixerRunsFilter struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Status   string
}

type AILogFixerAction struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	NodeID       uuid.UUID
	RunID        uuid.NullUUID
	JobID        uuid.UUID
	Action       string
	Status       string
	Policy       map[string]any
	Result       map[string]any
	ErrorMessage sql.NullString
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateAILogFixerActionParams struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	RunID    *uuid.UUID
	JobID    uuid.UUID
	Action   string
	Policy   map[string]any
}

func (s *Store) CreateAILogFixerRun(ctx context.Context, params CreateAILogFixerRunParams) (*AILogFixerRun, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	eventType := strings.TrimSpace(params.TriggerEventType)
	if eventType == "" {
		return nil, errors.New("trigger event type is required")
	}
	dedupKey := strings.TrimSpace(params.TriggerDedupKey)
	if dedupKey == "" {
		return nil, errors.New("trigger dedup key is required")
	}
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = "planned"
	}
	riskLevel := strings.TrimSpace(params.RiskLevel)
	if riskLevel == "" {
		riskLevel = "read_only"
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_logfixer_runs (
			tenant_id, node_id, investigation_id, proposal_id, job_id, approval_id,
			trigger_event_type, trigger_dedup_key, service_key, status, risk_level,
			investigation_request, diagnosis, remediation_plan, attempt, receipt, evidence
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16::jsonb,$17::jsonb)
		ON CONFLICT (tenant_id, trigger_dedup_key)
		DO UPDATE SET
			node_id               = COALESCE(EXCLUDED.node_id, ai_logfixer_runs.node_id),
			investigation_id      = COALESCE(EXCLUDED.investigation_id, ai_logfixer_runs.investigation_id),
			proposal_id           = COALESCE(EXCLUDED.proposal_id, ai_logfixer_runs.proposal_id),
			job_id                = COALESCE(EXCLUDED.job_id, ai_logfixer_runs.job_id),
			approval_id           = COALESCE(EXCLUDED.approval_id, ai_logfixer_runs.approval_id),
			trigger_event_type    = EXCLUDED.trigger_event_type,
			service_key           = EXCLUDED.service_key,
			status                = EXCLUDED.status,
			risk_level            = EXCLUDED.risk_level,
			investigation_request = EXCLUDED.investigation_request,
			diagnosis             = EXCLUDED.diagnosis,
			remediation_plan      = EXCLUDED.remediation_plan,
			evidence              = EXCLUDED.evidence,
			updated_at            = NOW()
		RETURNING id, tenant_id, node_id, investigation_id, proposal_id, job_id, approval_id,
		          trigger_event_type, trigger_dedup_key, service_key, status, risk_level,
		          investigation_request, diagnosis, remediation_plan, attempt, receipt, evidence,
		          created_at, updated_at
	`,
		params.TenantID,
		nullableUUID(params.NodeID),
		nullableUUID(params.InvestigationID),
		nullableUUID(params.ProposalID),
		nullableUUID(params.JobID),
		nullableUUID(params.ApprovalID),
		eventType,
		dedupKey,
		strings.TrimSpace(params.ServiceKey),
		status,
		riskLevel,
		[]byte(normalizeJSON(params.InvestigationRequest)),
		[]byte(normalizeJSON(params.Diagnosis)),
		[]byte(normalizeJSON(params.RemediationPlan)),
		[]byte(normalizeJSON(params.Attempt)),
		[]byte(normalizeJSON(params.Receipt)),
		[]byte(normalizeJSON(params.Evidence)),
	)
	return scanAILogFixerRun(row)
}

func (s *Store) GetAILogFixerRunByDedupKey(ctx context.Context, tenantID uuid.UUID, dedupKey string) (*AILogFixerRun, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, investigation_id, proposal_id, job_id, approval_id,
		       trigger_event_type, trigger_dedup_key, service_key, status, risk_level,
		       investigation_request, diagnosis, remediation_plan, attempt, receipt, evidence,
		       created_at, updated_at
		FROM ai_logfixer_runs
		WHERE tenant_id = $1 AND trigger_dedup_key = $2
	`, tenantID, strings.TrimSpace(dedupKey))
	out, err := scanAILogFixerRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ai logfixer run by dedup key: %w", err)
	}
	return out, nil
}

func (s *Store) GetAILogFixerRun(ctx context.Context, id uuid.UUID) (*AILogFixerRun, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, investigation_id, proposal_id, job_id, approval_id,
		       trigger_event_type, trigger_dedup_key, service_key, status, risk_level,
		       investigation_request, diagnosis, remediation_plan, attempt, receipt, evidence,
		       created_at, updated_at
		FROM ai_logfixer_runs
		WHERE id = $1
	`, id)
	out, err := scanAILogFixerRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ai logfixer run: %w", err)
	}
	return out, nil
}

func (s *Store) ListAILogFixerRuns(ctx context.Context, filter ListAILogFixerRunsFilter, limit, offset int) ([]AILogFixerRun, int, error) {
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
	if strings.TrimSpace(filter.Status) != "" {
		args = append(args, strings.TrimSpace(filter.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM ai_logfixer_runs WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ai logfixer runs: %w", err)
	}
	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, investigation_id, proposal_id, job_id, approval_id,
		       trigger_event_type, trigger_dedup_key, service_key, status, risk_level,
		       investigation_request, diagnosis, remediation_plan, attempt, receipt, evidence,
		       created_at, updated_at
		FROM ai_logfixer_runs
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
		return nil, 0, fmt.Errorf("query ai logfixer runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AILogFixerRun
	for rows.Next() {
		row, err := scanAILogFixerRun(rows)
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

func (s *Store) MarkAILogFixerRunStatus(ctx context.Context, id uuid.UUID, status string, attempt, receipt map[string]any) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("run id required")
	}
	attemptJSON, err := marshalJSONBMap(attempt)
	if err != nil {
		return err
	}
	receiptJSON, err := marshalJSONBMap(receipt)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE ai_logfixer_runs
		SET status = $2, attempt = $3, receipt = $4, updated_at = NOW()
		WHERE id = $1
	`, id, strings.TrimSpace(status), attemptJSON, receiptJSON)
	return err
}

func (s *Store) CreateAILogFixerAction(ctx context.Context, p CreateAILogFixerActionParams) (*AILogFixerAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || p.NodeID == uuid.Nil || p.JobID == uuid.Nil {
		return nil, errors.New("tenant_id, node_id, and job_id are required")
	}
	policy, err := marshalJSONBMap(p.Policy)
	if err != nil {
		return nil, err
	}
	var runID any
	if p.RunID != nil && *p.RunID != uuid.Nil {
		runID = *p.RunID
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_logfixer_actions (tenant_id, node_id, run_id, job_id, action, policy)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, tenant_id, node_id, run_id, job_id, action, status, policy, result, error_message, created_at, updated_at
	`, p.TenantID, p.NodeID, runID, p.JobID, strings.TrimSpace(p.Action), policy)
	return scanAILogFixerAction(row)
}

func (s *Store) ListPendingAILogFixerActions(ctx context.Context, nodeID uuid.UUID) ([]AILogFixerAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, run_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM ai_logfixer_actions
		WHERE node_id = $1
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
	var out []AILogFixerAction
	for rows.Next() {
		action, err := scanAILogFixerAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *action)
	}
	return out, rows.Err()
}

func (s *Store) GetAILogFixerActionByJobID(ctx context.Context, jobID uuid.UUID) (*AILogFixerAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, run_id, job_id, action, status, policy, result, error_message, created_at, updated_at
		FROM ai_logfixer_actions
		WHERE job_id = $1
	`, jobID)
	action, err := scanAILogFixerAction(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ai logfixer action by job id: %w", err)
	}
	return action, nil
}

func (s *Store) MarkAILogFixerActionStatus(ctx context.Context, jobID uuid.UUID, status string, result map[string]any, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	resultJSON, err := marshalJSONBMap(result)
	if err != nil {
		return err
	}
	var errArg any
	if strings.TrimSpace(errMsg) != "" {
		errArg = errMsg
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE ai_logfixer_actions
		SET status = $2, result = $3, error_message = $4, updated_at = NOW()
		WHERE job_id = $1
	`, jobID, strings.TrimSpace(status), resultJSON, errArg)
	return err
}

func scanAILogFixerRun(row interface{ Scan(dest ...any) error }) (*AILogFixerRun, error) {
	var (
		out                                                                    AILogFixerRun
		nodeID, investigationID, proposalID, jobID, approvalID                 sql.NullString
		investigationRequest, diagnosis, remediationPlan, attempt, receipt, ev []byte
	)
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&nodeID,
		&investigationID,
		&proposalID,
		&jobID,
		&approvalID,
		&out.TriggerEventType,
		&out.TriggerDedupKey,
		&out.ServiceKey,
		&out.Status,
		&out.RiskLevel,
		&investigationRequest,
		&diagnosis,
		&remediationPlan,
		&attempt,
		&receipt,
		&ev,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	out.NodeID = parseNullableUUID(nodeID)
	out.InvestigationID = parseNullableUUID(investigationID)
	out.ProposalID = parseNullableUUID(proposalID)
	out.JobID = parseNullableUUID(jobID)
	out.ApprovalID = parseNullableUUID(approvalID)
	out.InvestigationRequest = json.RawMessage(investigationRequest)
	out.Diagnosis = json.RawMessage(diagnosis)
	out.RemediationPlan = json.RawMessage(remediationPlan)
	out.Attempt = json.RawMessage(attempt)
	out.Receipt = json.RawMessage(receipt)
	out.Evidence = json.RawMessage(ev)
	return &out, nil
}

func scanAILogFixerAction(row interface{ Scan(dest ...any) error }) (*AILogFixerAction, error) {
	var (
		out          AILogFixerAction
		runID        sql.NullString
		policyJSON   []byte
		resultJSON   []byte
		errorMessage sql.NullString
	)
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&out.NodeID,
		&runID,
		&out.JobID,
		&out.Action,
		&out.Status,
		&policyJSON,
		&resultJSON,
		&errorMessage,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if runID.Valid {
		if parsed, err := uuid.Parse(runID.String); err == nil {
			out.RunID = uuid.NullUUID{UUID: parsed, Valid: true}
		}
	}
	_ = json.Unmarshal(policyJSON, &out.Policy)
	_ = json.Unmarshal(resultJSON, &out.Result)
	if out.Policy == nil {
		out.Policy = map[string]any{}
	}
	if out.Result == nil {
		out.Result = map[string]any{}
	}
	out.ErrorMessage = errorMessage
	return &out, nil
}

func parseNullableUUID(value sql.NullString) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(value.String)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}
