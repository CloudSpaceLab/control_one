package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MisconductCase represents an investigator-tracked case under UC7.
type MisconductCase struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	Status        string     `json:"status"`
	OpenedAt      time.Time  `json:"opened_at"`
	OpenedBy      *uuid.UUID `json:"opened_by,omitempty"`
	Summary       string     `json:"summary"`
	RiskScore     int        `json:"risk_score"`
	SubjectUserID *uuid.UUID `json:"subject_user_id,omitempty"`
	SubjectLabel  *string    `json:"subject_label,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// MisconductCaseFilter narrows ListMisconductCases.
type MisconductCaseFilter struct {
	TenantID uuid.UUID
	Status   string
}

// CreateMisconductCaseParams is the input for CreateMisconductCase.
type CreateMisconductCaseParams struct {
	TenantID      uuid.UUID
	OpenedBy      *uuid.UUID
	Summary       string
	SubjectUserID *uuid.UUID
	SubjectLabel  *string
}

// UpdateMisconductCaseParams captures investigator-driven mutations.
// Pointer fields mean "leave alone if nil"; status="" means "no change".
type UpdateMisconductCaseParams struct {
	Status        string
	Summary       *string
	RiskScore     *int
	SubjectUserID *uuid.UUID
	SubjectLabel  *string
}

// WhistleblowerSubmission is the PII-free anonymous intake row.
type WhistleblowerSubmission struct {
	ID             uuid.UUID
	TenantID       *uuid.UUID
	TokenHash      string
	SubmittedAt    time.Time
	BodyEncrypted  []byte
	BodyNonce      []byte
	RetentionUntil time.Time
	Status         string
}

// CreateWhistleblowerSubmissionParams creates a submission row.
type CreateWhistleblowerSubmissionParams struct {
	TenantID       *uuid.UUID
	TokenHash      string
	BodyEncrypted  []byte
	BodyNonce      []byte
	RetentionUntil time.Time
}

// CaseEvidenceLink ties a misconduct case to a compliance_evidence row.
type CaseEvidenceLink struct {
	CaseID     uuid.UUID `json:"case_id"`
	EvidenceID uuid.UUID `json:"evidence_id"`
	AttachedAt time.Time `json:"attached_at"`
}

// RiskSignal is one weighted contributor to a case's risk score.
type RiskSignal struct {
	ID          uuid.UUID  `json:"id"`
	CaseID      uuid.UUID  `json:"case_id"`
	SignalType  string     `json:"signal_type"`
	Severity    string     `json:"severity"`
	SourceID    *uuid.UUID `json:"source_id,omitempty"`
	SourceTable *string    `json:"source_table,omitempty"`
	OccurredAt  time.Time  `json:"occurred_at"`
	Weight      int        `json:"weight"`
}

// CreateRiskSignalParams creates a risk_signals row.
type CreateRiskSignalParams struct {
	CaseID      uuid.UUID
	SignalType  string
	Severity    string
	SourceID    *uuid.UUID
	SourceTable string
	OccurredAt  time.Time
	Weight      int
}

// CreateMisconductCase persists a new case in the open state.
func (s *Store) CreateMisconductCase(ctx context.Context, p CreateMisconductCaseParams) (*MisconductCase, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	id := uuid.New()
	now := s.clock()
	var openedBy any
	if p.OpenedBy != nil && *p.OpenedBy != uuid.Nil {
		openedBy = *p.OpenedBy
	}
	var subjectUser any
	if p.SubjectUserID != nil && *p.SubjectUserID != uuid.Nil {
		subjectUser = *p.SubjectUserID
	}
	var subjectLabel any
	if p.SubjectLabel != nil && *p.SubjectLabel != "" {
		subjectLabel = *p.SubjectLabel
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO misconduct_cases
			(id, tenant_id, status, opened_at, opened_by, summary, risk_score,
			 subject_user_id, subject_label, created_at, updated_at)
		VALUES ($1, $2, 'open', $3, $4, $5, 0, $6, $7, $3, $3)
	`, id, p.TenantID, now, openedBy, p.Summary, subjectUser, subjectLabel)
	if err != nil {
		return nil, fmt.Errorf("insert misconduct case: %w", err)
	}
	return s.GetMisconductCase(ctx, id)
}

// GetMisconductCase fetches a case by id (returns nil, nil when not found).
func (s *Store) GetMisconductCase(ctx context.Context, id uuid.UUID) (*MisconductCase, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, status, opened_at, opened_by, summary, risk_score,
		       subject_user_id, subject_label, created_at, updated_at
		FROM misconduct_cases WHERE id = $1
	`, id)
	return scanMisconductCaseRow(row)
}

// ListMisconductCases returns cases for the tenant, optionally filtered by status.
func (s *Store) ListMisconductCases(ctx context.Context, filter MisconductCaseFilter, limit, offset int) ([]MisconductCase, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if filter.TenantID == uuid.Nil {
		return nil, 0, errors.New("tenant_id required")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := []any{filter.TenantID}
	whereStatus := ""
	if filter.Status != "" {
		whereStatus = " AND status = $2"
		args = append(args, filter.Status)
	}
	countQ := "SELECT COUNT(*) FROM misconduct_cases WHERE tenant_id = $1" + whereStatus
	var total int
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count misconduct cases: %w", err)
	}

	listQ := `
		SELECT id, tenant_id, status, opened_at, opened_by, summary, risk_score,
		       subject_user_id, subject_label, created_at, updated_at
		FROM misconduct_cases
		WHERE tenant_id = $1` + whereStatus + `
		ORDER BY opened_at DESC
		LIMIT ` + fmt.Sprintf("%d", limit) + ` OFFSET ` + fmt.Sprintf("%d", offset)
	rows, err := s.db.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list misconduct cases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MisconductCase
	for rows.Next() {
		c, err := scanMisconductCaseRow(rows)
		if err != nil {
			return nil, 0, err
		}
		if c != nil {
			out = append(out, *c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateMisconductCase mutates a case in place and returns the post-update row.
func (s *Store) UpdateMisconductCase(ctx context.Context, id uuid.UUID, p UpdateMisconductCaseParams) (*MisconductCase, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	now := s.clock()
	// Build the COALESCE-driven update statement so callers can update one
	// field without clobbering the others.
	var statusVal any
	if p.Status != "" {
		statusVal = p.Status
	}
	var summaryVal any
	if p.Summary != nil {
		summaryVal = *p.Summary
	}
	var riskVal any
	if p.RiskScore != nil {
		riskVal = *p.RiskScore
	}
	var subjectUser any
	if p.SubjectUserID != nil {
		if *p.SubjectUserID != uuid.Nil {
			subjectUser = *p.SubjectUserID
		}
	}
	var subjectLabel any
	if p.SubjectLabel != nil {
		subjectLabel = *p.SubjectLabel
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE misconduct_cases
		SET status = COALESCE($2, status),
		    summary = COALESCE($3, summary),
		    risk_score = COALESCE($4, risk_score),
		    subject_user_id = COALESCE($5, subject_user_id),
		    subject_label = COALESCE($6, subject_label),
		    updated_at = $7
		WHERE id = $1
	`, id, statusVal, summaryVal, riskVal, subjectUser, subjectLabel, now)
	if err != nil {
		return nil, fmt.Errorf("update misconduct case: %w", err)
	}
	return s.GetMisconductCase(ctx, id)
}

// SetMisconductCaseRiskScore is a focused helper for the misconduct.score job.
func (s *Store) SetMisconductCaseRiskScore(ctx context.Context, id uuid.UUID, score int) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE misconduct_cases SET risk_score = $2, updated_at = $3 WHERE id = $1
	`, id, score, s.clock())
	return err
}

func scanMisconductCaseRow(row interface {
	Scan(dest ...any) error
}) (*MisconductCase, error) {
	var c MisconductCase
	var openedBy uuid.NullUUID
	var subjectUser uuid.NullUUID
	var subjectLabel sql.NullString
	if err := row.Scan(
		&c.ID, &c.TenantID, &c.Status, &c.OpenedAt, &openedBy, &c.Summary,
		&c.RiskScore, &subjectUser, &subjectLabel, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if openedBy.Valid {
		v := openedBy.UUID
		c.OpenedBy = &v
	}
	if subjectUser.Valid {
		v := subjectUser.UUID
		c.SubjectUserID = &v
	}
	if subjectLabel.Valid {
		v := subjectLabel.String
		c.SubjectLabel = &v
	}
	return &c, nil
}

// CreateWhistleblowerSubmission inserts a PII-free submission row. The caller
// supplies the bcrypt token hash and pre-encrypted body + nonce.
func (s *Store) CreateWhistleblowerSubmission(ctx context.Context, p CreateWhistleblowerSubmissionParams) (*WhistleblowerSubmission, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TokenHash == "" {
		return nil, errors.New("token_hash required")
	}
	if p.RetentionUntil.IsZero() {
		// Default 90-day retention for the sweep job.
		p.RetentionUntil = s.clock().Add(90 * 24 * time.Hour)
	}
	id := uuid.New()
	var tenantArg any
	if p.TenantID != nil && *p.TenantID != uuid.Nil {
		tenantArg = *p.TenantID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO whistleblower_submissions
			(id, tenant_id, token_hash, submitted_at, body_encrypted, body_nonce,
			 retention_until, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'received')
	`, id, tenantArg, p.TokenHash, s.clock(), p.BodyEncrypted, p.BodyNonce, p.RetentionUntil)
	if err != nil {
		return nil, fmt.Errorf("insert whistleblower submission: %w", err)
	}
	return s.GetWhistleblowerSubmission(ctx, id)
}

// GetWhistleblowerSubmission fetches a submission by id.
func (s *Store) GetWhistleblowerSubmission(ctx context.Context, id uuid.UUID) (*WhistleblowerSubmission, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, token_hash, submitted_at, body_encrypted, body_nonce,
		       retention_until, status
		FROM whistleblower_submissions WHERE id = $1
	`, id)
	return scanWhistleblowerRow(row)
}

// ListWhistleblowerSubmissionsByTokenHash retrieves candidates matching a
// bcrypt hash prefix. Because bcrypt hashes are unique per-call, callers
// supply the full hash and we expect 0/1 rows; we still iterate to handle
// the rare collision-from-test case.
func (s *Store) ListWhistleblowerSubmissionsByTokenHash(ctx context.Context, tokenHash string) ([]WhistleblowerSubmission, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, token_hash, submitted_at, body_encrypted, body_nonce,
		       retention_until, status
		FROM whistleblower_submissions
		WHERE token_hash = $1
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WhistleblowerSubmission
	for rows.Next() {
		w, err := scanWhistleblowerRow(rows)
		if err != nil {
			return nil, err
		}
		if w != nil {
			out = append(out, *w)
		}
	}
	return out, rows.Err()
}

// ListAllWhistleblowerSubmissions returns every submission (used by the status
// lookup handler — it must constant-time compare the candidate token against
// every stored hash because token_hash is bcrypt-derived and not directly
// indexable).
func (s *Store) ListAllWhistleblowerSubmissions(ctx context.Context) ([]WhistleblowerSubmission, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, token_hash, submitted_at, body_encrypted, body_nonce,
		       retention_until, status
		FROM whistleblower_submissions
		ORDER BY submitted_at DESC
		LIMIT 10000
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WhistleblowerSubmission
	for rows.Next() {
		w, err := scanWhistleblowerRow(rows)
		if err != nil {
			return nil, err
		}
		if w != nil {
			out = append(out, *w)
		}
	}
	return out, rows.Err()
}

// SweepWhistleblowerSubmissions deletes submissions whose retention deadline
// has passed. Returns the number of rows deleted. Driven by the
// misconduct.retention_sweep job.
func (s *Store) SweepWhistleblowerSubmissions(ctx context.Context, now time.Time) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if now.IsZero() {
		now = s.clock()
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM whistleblower_submissions WHERE retention_until < $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("sweep whistleblower submissions: %w", err)
	}
	return res.RowsAffected()
}

func scanWhistleblowerRow(row interface {
	Scan(dest ...any) error
}) (*WhistleblowerSubmission, error) {
	var ws WhistleblowerSubmission
	var tenant uuid.NullUUID
	if err := row.Scan(
		&ws.ID, &tenant, &ws.TokenHash, &ws.SubmittedAt,
		&ws.BodyEncrypted, &ws.BodyNonce, &ws.RetentionUntil, &ws.Status,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if tenant.Valid {
		v := tenant.UUID
		ws.TenantID = &v
	}
	return &ws, nil
}

// AttachCaseEvidence links an evidence row to a case (idempotent).
func (s *Store) AttachCaseEvidence(ctx context.Context, caseID, evidenceID uuid.UUID) (*CaseEvidenceLink, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	now := s.clock()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO case_evidence (case_id, evidence_id, attached_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (case_id, evidence_id) DO NOTHING
	`, caseID, evidenceID, now)
	if err != nil {
		return nil, fmt.Errorf("insert case evidence: %w", err)
	}
	return &CaseEvidenceLink{CaseID: caseID, EvidenceID: evidenceID, AttachedAt: now}, nil
}

// ListCaseEvidence returns evidence ids attached to a case.
func (s *Store) ListCaseEvidence(ctx context.Context, caseID uuid.UUID) ([]CaseEvidenceLink, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT case_id, evidence_id, attached_at
		FROM case_evidence WHERE case_id = $1 ORDER BY attached_at DESC
	`, caseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CaseEvidenceLink
	for rows.Next() {
		var link CaseEvidenceLink
		if err := rows.Scan(&link.CaseID, &link.EvidenceID, &link.AttachedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

// CreateRiskSignal inserts a risk signal row.
func (s *Store) CreateRiskSignal(ctx context.Context, p CreateRiskSignalParams) (*RiskSignal, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.CaseID == uuid.Nil {
		return nil, errors.New("case_id required")
	}
	id := uuid.New()
	occurred := p.OccurredAt
	if occurred.IsZero() {
		occurred = s.clock()
	}
	var sourceID any
	if p.SourceID != nil && *p.SourceID != uuid.Nil {
		sourceID = *p.SourceID
	}
	var sourceTable any
	if p.SourceTable != "" {
		sourceTable = p.SourceTable
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_signals
			(id, case_id, signal_type, severity, source_id, source_table,
			 occurred_at, weight)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, p.CaseID, p.SignalType, p.Severity, sourceID, sourceTable, occurred, p.Weight)
	if err != nil {
		return nil, fmt.Errorf("insert risk signal: %w", err)
	}
	return &RiskSignal{
		ID:         id,
		CaseID:     p.CaseID,
		SignalType: p.SignalType,
		Severity:   p.Severity,
		SourceID:   p.SourceID,
		SourceTable: func() *string {
			if p.SourceTable == "" {
				return nil
			}
			v := p.SourceTable
			return &v
		}(),
		OccurredAt: occurred,
		Weight:     p.Weight,
	}, nil
}

// ListRiskSignals returns risk signals for a case ordered by recency.
func (s *Store) ListRiskSignals(ctx context.Context, caseID uuid.UUID) ([]RiskSignal, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, case_id, signal_type, severity, source_id, source_table,
		       occurred_at, weight
		FROM risk_signals
		WHERE case_id = $1
		ORDER BY occurred_at DESC
	`, caseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RiskSignal
	for rows.Next() {
		var r RiskSignal
		var sourceID uuid.NullUUID
		var sourceTable sql.NullString
		if err := rows.Scan(
			&r.ID, &r.CaseID, &r.SignalType, &r.Severity, &sourceID, &sourceTable,
			&r.OccurredAt, &r.Weight,
		); err != nil {
			return nil, err
		}
		if sourceID.Valid {
			v := sourceID.UUID
			r.SourceID = &v
		}
		if sourceTable.Valid {
			v := sourceTable.String
			r.SourceTable = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRiskSignalsForCase removes prior signals for a case (used by the
// misconduct.score job to recompute fresh signals each run).
func (s *Store) DeleteRiskSignalsForCase(ctx context.Context, caseID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM risk_signals WHERE case_id = $1`, caseID)
	return err
}

// CountAuditLogsForActor returns the count of audit_logs rows for a given
// actor in the lookback window. Used by the misconduct.score job.
func (s *Store) CountAuditLogsForActor(ctx context.Context, actorID uuid.UUID, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM audit_logs
		WHERE actor_id = $1 AND created_at >= $2
	`, actorID, since).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CountSecurityEventsForNode returns counts grouped by severity for a tenant
// (since `since`) — used by the misconduct.score job when subject_label is
// set instead of subject_user_id (we treat the label as a hostname filter).
func (s *Store) CountSecurityEventsBySeverity(ctx context.Context, tenantID uuid.UUID, since time.Time) (map[string]int, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT severity, COUNT(*)
		FROM security_events
		WHERE tenant_id = $1 AND fired_at >= $2
		GROUP BY severity
	`, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var sev string
		var n int
		if err := rows.Scan(&sev, &n); err != nil {
			return nil, err
		}
		out[sev] = n
	}
	return out, rows.Err()
}

// CountFailedComplianceForTenant counts failed compliance results for a tenant
// since a cutoff. Used by the misconduct.score job.
func (s *Store) CountFailedComplianceForTenant(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM compliance_results
		WHERE tenant_id = $1 AND passed = false AND created_at >= $2
	`, tenantID, since).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
