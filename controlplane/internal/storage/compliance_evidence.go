package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ComplianceEvidence is a file or record uploaded as evidence for a compliance control.
type ComplianceEvidence struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	EvidenceType  string
	Framework     *string
	ControlRef    *string
	Title         string
	Description   *string
	FilePath      *string
	FileSizeBytes *int64
	MimeType      *string
	Checksum      *string
	UploadedBy    uuid.UUID
	UploadedAt    time.Time
	ExpiresAt     *time.Time
}

type ComplianceEvidenceFilter struct {
	TenantID            uuid.UUID
	Framework           string
	ControlRef          string
	EvidenceType        string
	UploadedSince       *time.Time
	UploadedUntil       *time.Time
	ExpirationReference *time.Time
	IncludeExpired      bool
}

// ComplianceReview represents a scheduled or completed compliance review.
type ComplianceReview struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	ReviewType   string     `json:"review_type"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ReviewedBy   *uuid.UUID `json:"reviewed_by,omitempty"`
	Status       string     `json:"status"`
	Notes        *string    `json:"notes,omitempty"`
	Recurrence   *string    `json:"recurrence,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// AuditReport represents a generated (or in-progress) compliance audit report.
type AuditReport struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Framework   string     `json:"framework"`
	PeriodStart time.Time  `json:"period_start"`
	PeriodEnd   time.Time  `json:"period_end"`
	Status      string     `json:"status"`
	PDFPath     *string    `json:"pdf_path,omitempty"`
	GeneratedBy *uuid.UUID `json:"generated_by,omitempty"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	CreatedAt   time.Time  `json:"-"`
}

// CreateComplianceEvidence inserts a new evidence record.
func (s *Store) CreateComplianceEvidence(ctx context.Context, e *ComplianceEvidence) (*ComplianceEvidence, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.UploadedAt.IsZero() {
		e.UploadedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO compliance_evidence
			(id, tenant_id, evidence_type, framework, control_ref, title, description,
			 file_path, file_size_bytes, mime_type, checksum, uploaded_by, uploaded_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`,
		e.ID, e.TenantID, e.EvidenceType, e.Framework, e.ControlRef, e.Title, e.Description,
		e.FilePath, e.FileSizeBytes, e.MimeType, e.Checksum, e.UploadedBy, e.UploadedAt, e.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create compliance evidence: %w", err)
	}
	return e, nil
}

// ListComplianceEvidence returns a paginated list of evidence filtered by optional framework and type.
func (s *Store) ListComplianceEvidence(ctx context.Context, tenantID uuid.UUID, framework, evidenceType string, limit, offset int) ([]ComplianceEvidence, int, error) {
	return s.ListComplianceEvidenceFiltered(ctx, ComplianceEvidenceFilter{
		TenantID:       tenantID,
		Framework:      framework,
		EvidenceType:   evidenceType,
		IncludeExpired: true,
	}, limit, offset)
}

func (s *Store) ListComplianceEvidenceFiltered(ctx context.Context, filter ComplianceEvidenceFilter, limit, offset int) ([]ComplianceEvidence, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}

	args := []any{filter.TenantID}
	where := "WHERE tenant_id = $1"
	idx := 2
	if filter.Framework != "" {
		where += fmt.Sprintf(" AND framework = $%d", idx)
		args = append(args, filter.Framework)
		idx++
	}
	if filter.ControlRef != "" {
		where += fmt.Sprintf(" AND control_ref = $%d", idx)
		args = append(args, filter.ControlRef)
		idx++
	}
	if filter.EvidenceType != "" {
		where += fmt.Sprintf(" AND evidence_type = $%d", idx)
		args = append(args, filter.EvidenceType)
		idx++
	}
	if filter.UploadedSince != nil {
		where += fmt.Sprintf(" AND uploaded_at >= $%d", idx)
		args = append(args, filter.UploadedSince)
		idx++
	}
	if filter.UploadedUntil != nil {
		where += fmt.Sprintf(" AND uploaded_at <= $%d", idx)
		args = append(args, filter.UploadedUntil)
		idx++
	}
	if !filter.IncludeExpired {
		expiresAtReference := time.Now().UTC()
		if filter.ExpirationReference != nil && !filter.ExpirationReference.IsZero() {
			expiresAtReference = filter.ExpirationReference.UTC()
		}
		where += fmt.Sprintf(" AND (expires_at IS NULL OR expires_at > $%d)", idx)
		args = append(args, expiresAtReference)
		idx++
	}

	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM compliance_evidence `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count compliance evidence: %w", err)
	}

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, evidence_type, framework, control_ref, title, description,
		       file_path, file_size_bytes, mime_type, checksum, uploaded_by, uploaded_at, expires_at
		FROM compliance_evidence
		`+where+`
		ORDER BY uploaded_at DESC
		LIMIT $`+fmt.Sprintf("%d", idx)+` OFFSET $`+fmt.Sprintf("%d", idx+1),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list compliance evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ComplianceEvidence
	for rows.Next() {
		var e ComplianceEvidence
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.EvidenceType, &e.Framework, &e.ControlRef, &e.Title, &e.Description,
			&e.FilePath, &e.FileSizeBytes, &e.MimeType, &e.Checksum, &e.UploadedBy, &e.UploadedAt, &e.ExpiresAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan compliance evidence: %w", err)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// GetComplianceEvidence returns a single evidence record by ID.
func (s *Store) GetComplianceEvidence(ctx context.Context, id uuid.UUID) (*ComplianceEvidence, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var e ComplianceEvidence
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, evidence_type, framework, control_ref, title, description,
		       file_path, file_size_bytes, mime_type, checksum, uploaded_by, uploaded_at, expires_at
		FROM compliance_evidence WHERE id = $1
	`, id).Scan(
		&e.ID, &e.TenantID, &e.EvidenceType, &e.Framework, &e.ControlRef, &e.Title, &e.Description,
		&e.FilePath, &e.FileSizeBytes, &e.MimeType, &e.Checksum, &e.UploadedBy, &e.UploadedAt, &e.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get compliance evidence: %w", err)
	}
	return &e, nil
}

// DeleteComplianceEvidence removes an evidence record by ID.
func (s *Store) DeleteComplianceEvidence(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM compliance_evidence WHERE id = $1`, id)
	return err
}

// CreateAuditReport inserts a new audit report record.
func (s *Store) CreateAuditReport(ctx context.Context, r *AuditReport) (*AuditReport, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.Status == "" {
		r.Status = "pending"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_reports
			(id, tenant_id, framework, period_start, period_end, status, pdf_path, generated_by, generated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`,
		r.ID, r.TenantID, r.Framework, r.PeriodStart, r.PeriodEnd, r.Status, r.PDFPath, r.GeneratedBy, r.GeneratedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create audit report: %w", err)
	}
	return r, nil
}

// ListAuditReports returns a paginated list of audit reports for a tenant.
func (s *Store) ListAuditReports(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]AuditReport, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_reports WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit reports: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, framework, period_start, period_end, status, pdf_path, generated_by, generated_at, metadata
		FROM audit_reports
		WHERE tenant_id = $1
		ORDER BY period_end DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit reports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]AuditReport, 0)
	for rows.Next() {
		var r AuditReport
		var metadata []byte
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.Framework, &r.PeriodStart, &r.PeriodEnd, &r.Status,
			&r.PDFPath, &r.GeneratedBy, &r.GeneratedAt, &metadata,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit report: %w", err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// GetAuditReport returns a single audit report by ID.
func (s *Store) GetAuditReport(ctx context.Context, id uuid.UUID) (*AuditReport, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var r AuditReport
	var metadata []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, framework, period_start, period_end, status, pdf_path, generated_by, generated_at, metadata
		FROM audit_reports WHERE id = $1
	`, id).Scan(
		&r.ID, &r.TenantID, &r.Framework, &r.PeriodStart, &r.PeriodEnd, &r.Status,
		&r.PDFPath, &r.GeneratedBy, &r.GeneratedAt, &metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get audit report: %w", err)
	}
	return &r, nil
}

// UpdateAuditReportStatus updates the status and optional PDF path/generated_at of a report.
func (s *Store) UpdateAuditReportStatus(ctx context.Context, id uuid.UUID, status string, pdfPath *string, generatedAt *time.Time) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE audit_reports SET status=$2, pdf_path=$3, generated_at=$4 WHERE id=$1
	`, id, status, pdfPath, generatedAt)
	return err
}

// ListComplianceReviews returns a paginated list of compliance reviews for a tenant.
func (s *Store) ListComplianceReviews(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ComplianceReview, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM compliance_reviews WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count compliance reviews: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, review_type, scheduled_for, completed_at, reviewed_by, status, notes, recurrence, created_at
		FROM compliance_reviews
		WHERE tenant_id = $1
		ORDER BY scheduled_for ASC NULLS LAST, created_at DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list compliance reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ComplianceReview, 0)
	for rows.Next() {
		var r ComplianceReview
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.ReviewType, &r.ScheduledFor, &r.CompletedAt,
			&r.ReviewedBy, &r.Status, &r.Notes, &r.Recurrence, &r.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan compliance review: %w", err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// CreateComplianceReview inserts a new compliance review.
func (s *Store) CreateComplianceReview(ctx context.Context, r *ComplianceReview) (*ComplianceReview, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.Status == "" {
		r.Status = "pending"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO compliance_reviews (id, tenant_id, review_type, scheduled_for, completed_at, reviewed_by, status, notes, recurrence, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, r.ID, r.TenantID, r.ReviewType, r.ScheduledFor, r.CompletedAt, r.ReviewedBy, r.Status, r.Notes, r.Recurrence, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create compliance review: %w", err)
	}
	return r, nil
}

// GetComplianceReview returns a single compliance review by ID.
func (s *Store) GetComplianceReview(ctx context.Context, id uuid.UUID) (*ComplianceReview, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var r ComplianceReview
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, review_type, scheduled_for, completed_at, reviewed_by, status, notes, recurrence, created_at
		FROM compliance_reviews WHERE id = $1
	`, id).Scan(
		&r.ID, &r.TenantID, &r.ReviewType, &r.ScheduledFor, &r.CompletedAt,
		&r.ReviewedBy, &r.Status, &r.Notes, &r.Recurrence, &r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get compliance review: %w", err)
	}
	return &r, nil
}

// CompleteComplianceReview marks a review as completed.
func (s *Store) CompleteComplianceReview(ctx context.Context, id uuid.UUID, reviewedBy uuid.UUID, notes *string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE compliance_reviews SET status=$2, completed_at=$3, reviewed_by=$4, notes=$5 WHERE id=$1
	`, id, "completed", now, reviewedBy, notes)
	return err
}

// DeleteComplianceReview removes a compliance review.
func (s *Store) DeleteComplianceReview(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM compliance_reviews WHERE id = $1`, id)
	return err
}
