package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ---------- access_requests ----------

type AccessRequest struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	UserID             uuid.NullUUID
	TargetNodeID       uuid.NullUUID
	TargetResourceType string
	RequestedAccess    string
	Justification      sql.NullString
	Status             string
	TTLSeconds         int
	RequestedAt        time.Time
	DecidedAt          sql.NullTime
	DecidedBy          uuid.NullUUID
	DecisionReason     sql.NullString
	ExpiresAt          sql.NullTime
}

type CreateAccessRequestParams struct {
	TenantID           uuid.UUID
	UserID             *uuid.UUID
	TargetNodeID       *uuid.UUID
	TargetResourceType string
	RequestedAccess    string
	Justification      string
	TTLSeconds         int
}

func (s *Store) CreateAccessRequest(ctx context.Context, p CreateAccessRequestParams) (*AccessRequest, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.RequestedAccess) == "" {
		return nil, errors.New("tenant_id and requested_access required")
	}
	rt := strings.ToLower(strings.TrimSpace(p.TargetResourceType))
	if rt != "ssh" && rt != "rdp" && rt != "db" {
		return nil, errors.New("target_resource_type must be ssh|rdp|db")
	}
	if p.TTLSeconds <= 0 {
		p.TTLSeconds = 1800
	}
	var userID, nodeID any
	if p.UserID != nil && *p.UserID != uuid.Nil {
		userID = *p.UserID
	}
	if p.TargetNodeID != nil && *p.TargetNodeID != uuid.Nil {
		nodeID = *p.TargetNodeID
	}
	var justArg any
	if strings.TrimSpace(p.Justification) != "" {
		justArg = p.Justification
	}
	id := uuid.New()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_requests (id, tenant_id, user_id, target_node_id, target_resource_type, requested_access, justification, status, ttl_seconds, requested_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',$8,$9)
	`, id, p.TenantID, userID, nodeID, rt, p.RequestedAccess, justArg, p.TTLSeconds, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert access request: %w", err)
	}
	return s.GetAccessRequest(ctx, id)
}

func (s *Store) GetAccessRequest(ctx context.Context, id uuid.UUID) (*AccessRequest, error) {
	row := s.db.QueryRowContext(ctx, accessRequestSelectSQL+` WHERE id = $1`, id)
	return scanAccessRequest(row)
}

type AccessRequestFilter struct {
	TenantID uuid.UUID
	Status   string
	UserID   uuid.UUID
}

func (s *Store) ListAccessRequests(ctx context.Context, f AccessRequestFilter, limit, offset int) ([]AccessRequest, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if f.TenantID != uuid.Nil {
		where = append(where, fmt.Sprintf("tenant_id = $%d", idx))
		args = append(args, f.TenantID)
		idx++
	}
	if strings.TrimSpace(f.Status) != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, f.Status)
		idx++
	}
	if f.UserID != uuid.Nil {
		where = append(where, fmt.Sprintf("user_id = $%d", idx))
		args = append(args, f.UserID)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_requests WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	q := accessRequestSelectSQL + ` WHERE ` + whereSQL + fmt.Sprintf(` ORDER BY requested_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []AccessRequest
	for rows.Next() {
		r, err := scanAccessRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

// DecideAccessRequest sets status=approved|denied with optional expiry.
func (s *Store) DecideAccessRequest(ctx context.Context, id uuid.UUID, status string, decidedBy uuid.UUID, reason string, expiresAt *time.Time) (*AccessRequest, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if status != "approved" && status != "denied" {
		return nil, errors.New("status must be approved or denied")
	}
	var byArg, reasonArg, expArg any
	if decidedBy != uuid.Nil {
		byArg = decidedBy
	}
	if strings.TrimSpace(reason) != "" {
		reasonArg = reason
	}
	if expiresAt != nil {
		expArg = *expiresAt
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE access_requests
		   SET status = $1, decided_at = NOW(), decided_by = $2, decision_reason = $3, expires_at = $4
		 WHERE id = $5 AND status = 'pending'
	`, status, byArg, reasonArg, expArg, id)
	if err != nil {
		return nil, err
	}
	return s.GetAccessRequest(ctx, id)
}

const accessRequestSelectSQL = `
	SELECT id, tenant_id, user_id, target_node_id, target_resource_type, requested_access, justification,
		status, ttl_seconds, requested_at, decided_at, decided_by, decision_reason, expires_at
	FROM access_requests
`

func scanAccessRequest(sc scanner) (*AccessRequest, error) {
	var a AccessRequest
	if err := sc.Scan(
		&a.ID, &a.TenantID, &a.UserID, &a.TargetNodeID, &a.TargetResourceType, &a.RequestedAccess, &a.Justification,
		&a.Status, &a.TTLSeconds, &a.RequestedAt, &a.DecidedAt, &a.DecidedBy, &a.DecisionReason, &a.ExpiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// ---------- ssh_ca ----------

type SSHCA struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	PublicKey      string
	PrivateSealed  []byte
	Nonce          []byte
	KeyType        string
	Active         bool
	CreatedAt      time.Time
	RotatedAt      sql.NullTime
}

type CreateSSHCAParams struct {
	TenantID      uuid.UUID
	PublicKey     string
	PrivateSealed []byte
	Nonce         []byte
	KeyType       string
}

func (s *Store) CreateSSHCA(ctx context.Context, p CreateSSHCAParams) (*SSHCA, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || len(p.PrivateSealed) == 0 || p.PublicKey == "" {
		return nil, errors.New("tenant_id, public_key, private_sealed required")
	}
	if p.KeyType == "" {
		p.KeyType = "ed25519"
	}
	// Deactivate any existing active CAs for the tenant.
	if _, err := s.db.ExecContext(ctx, `UPDATE ssh_ca SET active = false, rotated_at = NOW() WHERE tenant_id = $1 AND active = true`, p.TenantID); err != nil {
		return nil, fmt.Errorf("deactivate previous ca: %w", err)
	}
	id := uuid.New()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ssh_ca (id, tenant_id, ca_public_key, ca_private_key_sealed, nonce, key_type, active, created_at)
		VALUES ($1,$2,$3,$4,$5,$6, true, $7)
	`, id, p.TenantID, p.PublicKey, p.PrivateSealed, p.Nonce, p.KeyType, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert ssh ca: %w", err)
	}
	return s.GetActiveSSHCA(ctx, p.TenantID)
}

func (s *Store) GetActiveSSHCA(ctx context.Context, tenantID uuid.UUID) (*SSHCA, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, ca_public_key, ca_private_key_sealed, nonce, key_type, active, created_at, rotated_at
		FROM ssh_ca WHERE tenant_id = $1 AND active = true LIMIT 1
	`, tenantID)
	var c SSHCA
	if err := row.Scan(&c.ID, &c.TenantID, &c.PublicKey, &c.PrivateSealed, &c.Nonce, &c.KeyType, &c.Active, &c.CreatedAt, &c.RotatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// ---------- issued_certs ----------

type IssuedCert struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	AccessRequestID uuid.NullUUID
	CAID           uuid.UUID
	SubjectUser    string
	Principals     []string
	Serial         int64
	PublicKey      string
	SignedCert     string
	IssuedAt       time.Time
	ExpiresAt      time.Time
	RevokedAt      sql.NullTime
	RevokedReason  sql.NullString
}

type CreateIssuedCertParams struct {
	TenantID        uuid.UUID
	AccessRequestID *uuid.UUID
	CAID            uuid.UUID
	SubjectUser     string
	Principals      []string
	Serial          int64
	PublicKey       string
	SignedCert      string
	ExpiresAt       time.Time
}

func (s *Store) CreateIssuedCert(ctx context.Context, p CreateIssuedCertParams) (*IssuedCert, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || p.CAID == uuid.Nil {
		return nil, errors.New("tenant_id and ca_id required")
	}
	var arID any
	if p.AccessRequestID != nil && *p.AccessRequestID != uuid.Nil {
		arID = *p.AccessRequestID
	}
	id := uuid.New()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO issued_certs (id, tenant_id, access_request_id, ca_id, subject_user, principals, serial, public_key, signed_cert, issued_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, id, p.TenantID, arID, p.CAID, p.SubjectUser, pq.Array(p.Principals), p.Serial, p.PublicKey, p.SignedCert, s.clock(), p.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("insert issued cert: %w", err)
	}
	return &IssuedCert{
		ID: id, TenantID: p.TenantID, CAID: p.CAID,
		SubjectUser: p.SubjectUser, Principals: p.Principals,
		Serial: p.Serial, PublicKey: p.PublicKey, SignedCert: p.SignedCert,
		ExpiresAt: p.ExpiresAt, IssuedAt: time.Now(),
	}, nil
}

// NextCertSerial returns a per-CA monotonic serial. Uses SELECT COUNT as a
// cheap monotonic source — fine for audit/ordering purposes; for uniqueness
// the DB enforces UNIQUE (ca_id, serial).
func (s *Store) NextCertSerial(ctx context.Context, caID uuid.UUID) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(serial), 0) + 1 FROM issued_certs WHERE ca_id = $1`, caID).Scan(&n)
	return n, err
}

func (s *Store) ListIssuedCerts(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]IssuedCert, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issued_certs WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, access_request_id, ca_id, subject_user, principals, serial, public_key, signed_cert, issued_at, expires_at, revoked_at, revoked_reason
		FROM issued_certs WHERE tenant_id = $1 ORDER BY issued_at DESC LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []IssuedCert
	for rows.Next() {
		var c IssuedCert
		if err := rows.Scan(&c.ID, &c.TenantID, &c.AccessRequestID, &c.CAID, &c.SubjectUser, pq.Array(&c.Principals), &c.Serial, &c.PublicKey, &c.SignedCert, &c.IssuedAt, &c.ExpiresAt, &c.RevokedAt, &c.RevokedReason); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// ---------- command_acl ----------

type CommandACL struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Name              string
	Role              string
	NodeLabelSelector map[string]any
	AllowCommands     []string
	DenyCommands      []string
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateCommandACLParams struct {
	TenantID          uuid.UUID
	Name              string
	Role              string
	NodeLabelSelector map[string]any
	AllowCommands     []string
	DenyCommands      []string
	Enabled           bool
}

func (s *Store) CreateCommandACL(ctx context.Context, p CreateCommandACLParams) (*CommandACL, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Role) == "" {
		return nil, errors.New("tenant_id, name, role required")
	}
	selector, err := marshalJSONBMap(p.NodeLabelSelector)
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO command_acl (id, tenant_id, name, role, node_label_selector, allow_commands, deny_commands, enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
	`, id, p.TenantID, p.Name, p.Role, selector, pq.Array(p.AllowCommands), pq.Array(p.DenyCommands), p.Enabled, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert command acl: %w", err)
	}
	return s.GetCommandACL(ctx, id)
}

func (s *Store) GetCommandACL(ctx context.Context, id uuid.UUID) (*CommandACL, error) {
	row := s.db.QueryRowContext(ctx, commandACLSelectSQL+` WHERE id = $1`, id)
	return scanCommandACL(row)
}

func (s *Store) ListCommandACLs(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]CommandACL, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM command_acl WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, commandACLSelectSQL+` WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []CommandACL
	for rows.Next() {
		a, err := scanCommandACL(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

func (s *Store) DeleteCommandACL(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM command_acl WHERE id = $1`, id)
	return err
}

const commandACLSelectSQL = `
	SELECT id, tenant_id, name, role, node_label_selector, allow_commands, deny_commands, enabled, created_at, updated_at
	FROM command_acl
`

func scanCommandACL(sc scanner) (*CommandACL, error) {
	var a CommandACL
	var selRaw []byte
	if err := sc.Scan(&a.ID, &a.TenantID, &a.Name, &a.Role, &selRaw, pq.Array(&a.AllowCommands), pq.Array(&a.DenyCommands), &a.Enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m, err := decodeJSONBMap(selRaw)
	if err != nil {
		return nil, err
	}
	a.NodeLabelSelector = m
	return &a, nil
}
