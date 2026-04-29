package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Subprocessor represents a third-party service provider.
type Subprocessor struct {
	ID         string    `db:"id" json:"id"`
	TenantID   string    `db:"tenant_id" json:"tenant_id"`
	Name       string    `db:"name" json:"name"`
	Purpose    string    `db:"purpose" json:"purpose"`
	Location   string    `db:"location" json:"location"`
	DataTypes  []string  `db:"data_types" json:"data_types"`
	DPAInPlace bool      `db:"dpa_in_place" json:"dpa_in_place"`
	SOC2       bool      `db:"soc2" json:"soc2"`
	ISO27001   bool      `db:"iso27001" json:"iso27001"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

// Certification represents a compliance certification.
type Certification struct {
	ID        string    `db:"id" json:"id"`
	TenantID  string    `db:"tenant_id" json:"tenant_id"`
	Type      string    `db:"type" json:"type"` // SOC2, ISO27001, PCI-DSS, etc.
	Scope     string    `db:"scope" json:"scope"`
	IssuedAt  time.Time `db:"issued_at" json:"issued_at"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
	Auditor   string    `db:"auditor" json:"auditor"`
	ReportURL *string   `db:"report_url" json:"report_url,omitempty"`
	Status    string    `db:"status" json:"status"` // active, expired, pending
}

// SecurityFAQItem represents a Q&A for the security FAQ.
type SecurityFAQItem struct {
	ID        string    `db:"id" json:"id"`
	TenantID  string    `db:"tenant_id" json:"tenant_id"`
	Question  string    `db:"question" json:"question"`
	Answer    string    `db:"answer" json:"answer"`
	Category  string    `db:"category" json:"category"`
	OrderIdx  int       `db:"order_idx" json:"order_idx"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// IncidentReport represents a published security incident.
type IncidentReport struct {
	ID          string     `db:"id" json:"id"`
	TenantID    string     `db:"tenant_id" json:"tenant_id"`
	IncidentID  string     `db:"incident_id" json:"incident_id"`
	Title       string     `db:"title" json:"title"`
	Summary     string     `db:"summary" json:"summary"`
	Severity    string     `db:"severity" json:"severity"` // critical, high, medium, low
	Status      string     `db:"status" json:"status"`     // open, resolved, postmortem
	StartedAt   time.Time  `db:"started_at" json:"started_at"`
	ResolvedAt  *time.Time `db:"resolved_at" json:"resolved_at,omitempty"`
	PublishedAt time.Time  `db:"published_at" json:"published_at"`
	ReportURL   *string    `db:"report_url" json:"report_url,omitempty"`
}

// TrustCenterData aggregates all public trust data for a tenant.
type TrustCenterData struct {
	TenantSlug     string            `json:"tenant_slug"`
	TenantName     string            `json:"tenant_name"`
	Subprocessors  []Subprocessor    `json:"subprocessors"`
	Certifications []Certification   `json:"certifications"`
	FAQItems       []SecurityFAQItem `json:"faq_items"`
	Incidents      []IncidentReport  `json:"incidents"`
	LastUpdated    time.Time         `json:"last_updated"`
}

// Subprocessor CRUD
func (s *Store) CreateSubprocessor(ctx context.Context, sp *Subprocessor) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	sp.ID = uuid.New().String()
	sp.CreatedAt = time.Now().UTC()
	q := `INSERT INTO subprocessors (id, tenant_id, name, purpose, location, data_types, dpa_in_place, soc2, iso27001, created_at)
	      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := s.db.ExecContext(ctx, q, sp.ID, sp.TenantID, sp.Name, sp.Purpose, sp.Location, pq.Array(sp.DataTypes), sp.DPAInPlace, sp.SOC2, sp.ISO27001)
	return err
}

func (s *Store) ListSubprocessors(ctx context.Context, tenantID string) ([]Subprocessor, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var list []Subprocessor
	q := `SELECT id, tenant_id, name, purpose, location, data_types, dpa_in_place, soc2, iso27001, created_at FROM subprocessors WHERE tenant_id = $1 ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sp Subprocessor
		if err := rows.Scan(&sp.ID, &sp.TenantID, &sp.Name, &sp.Purpose, &sp.Location, pq.Array(&sp.DataTypes), &sp.DPAInPlace, &sp.SOC2, &sp.ISO27001, &sp.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, sp)
	}
	return list, rows.Err()
}

func (s *Store) DeleteSubprocessor(ctx context.Context, id string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM subprocessors WHERE id = $1`, id)
	return err
}

// Certification CRUD
func (s *Store) CreateCertification(ctx context.Context, c *Certification) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	c.ID = uuid.New().String()
	q := `INSERT INTO certifications (id, tenant_id, type, scope, issued_at, expires_at, auditor, report_url, status)
	      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := s.db.ExecContext(ctx, q, c.ID, c.TenantID, c.Type, c.Scope, c.IssuedAt, c.ExpiresAt, c.Auditor, c.ReportURL, c.Status)
	return err
}

func (s *Store) ListCertifications(ctx context.Context, tenantID string) ([]Certification, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var list []Certification
	q := `SELECT id, tenant_id, type, scope, issued_at, expires_at, auditor, report_url, status FROM certifications WHERE tenant_id = $1 ORDER BY issued_at DESC`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Certification
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Type, &c.Scope, &c.IssuedAt, &c.ExpiresAt, &c.Auditor, &c.ReportURL, &c.Status); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) DeleteCertification(ctx context.Context, id string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM certifications WHERE id = $1`, id)
	return err
}

// FAQ CRUD
func (s *Store) CreateFAQItem(ctx context.Context, f *SecurityFAQItem) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	f.ID = uuid.New().String()
	f.CreatedAt = time.Now().UTC()
	q := `INSERT INTO security_faq (id, tenant_id, question, answer, category, order_idx, created_at)
	      VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := s.db.ExecContext(ctx, q, f.ID, f.TenantID, f.Question, f.Answer, f.Category, f.OrderIdx, f.CreatedAt)
	return err
}

func (s *Store) ListFAQItems(ctx context.Context, tenantID string) ([]SecurityFAQItem, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var list []SecurityFAQItem
	q := `SELECT id, tenant_id, question, answer, category, order_idx, created_at FROM security_faq WHERE tenant_id = $1 ORDER BY category, order_idx`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f SecurityFAQItem
		if err := rows.Scan(&f.ID, &f.TenantID, &f.Question, &f.Answer, &f.Category, &f.OrderIdx, &f.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

func (s *Store) DeleteFAQItem(ctx context.Context, id string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM security_faq WHERE id = $1`, id)
	return err
}

// Incident CRUD
func (s *Store) CreateIncidentReport(ctx context.Context, i *IncidentReport) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	i.ID = uuid.New().String()
	i.PublishedAt = time.Now().UTC()
	q := `INSERT INTO incident_reports (id, tenant_id, incident_id, title, summary, severity, status, started_at, resolved_at, published_at, report_url)
	      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := s.db.ExecContext(ctx, q, i.ID, i.TenantID, i.IncidentID, i.Title, i.Summary, i.Severity, i.Status, i.StartedAt, i.ResolvedAt, i.PublishedAt, i.ReportURL)
	return err
}

func (s *Store) ListIncidentReports(ctx context.Context, tenantID string, limit int) ([]IncidentReport, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	var list []IncidentReport
	q := `SELECT id, tenant_id, incident_id, title, summary, severity, status, started_at, resolved_at, published_at, report_url FROM incident_reports WHERE tenant_id = $1 ORDER BY published_at DESC LIMIT $2`
	rows, err := s.db.QueryContext(ctx, q, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i IncidentReport
		if err := rows.Scan(&i.ID, &i.TenantID, &i.IncidentID, &i.Title, &i.Summary, &i.Severity, &i.Status, &i.StartedAt, &i.ResolvedAt, &i.PublishedAt, &i.ReportURL); err != nil {
			return nil, err
		}
		list = append(list, i)
	}
	return list, rows.Err()
}

func (s *Store) DeleteIncidentReport(ctx context.Context, id string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM incident_reports WHERE id = $1`, id)
	return err
}

// GetTrustCenterData retrieves all public trust data for a tenant by name (URL-encoded).
func (s *Store) GetTrustCenterData(ctx context.Context, tenantName string) (*TrustCenterData, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	// Get tenant info by name (URL-decoded by caller)
	var tenant struct {
		ID   string
		Name string
	}
	err := s.db.QueryRowContext(ctx, `SELECT id, name FROM tenants WHERE name = $1`, tenantName).Scan(&tenant.ID, &tenant.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	data := &TrustCenterData{
		TenantSlug:  tenant.Name, // Use name as slug
		TenantName:  tenant.Name,
		LastUpdated: time.Now().UTC(),
	}

	data.Subprocessors, _ = s.ListSubprocessors(ctx, tenant.ID)
	data.Certifications, _ = s.ListCertifications(ctx, tenant.ID)
	data.FAQItems, _ = s.ListFAQItems(ctx, tenant.ID)
	data.Incidents, _ = s.ListIncidentReports(ctx, tenant.ID, 20)

	return data, nil
}

// GetTenantByName retrieves tenant by name for public access.
func (s *Store) GetTenantByName(ctx context.Context, name string) (*Tenant, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var t Tenant
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM tenants WHERE name = $1`, name).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
