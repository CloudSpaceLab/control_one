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

// HypervisorHost represents a single managed hypervisor or cloud endpoint a
// tenant can provision against. Multiple hosts per tenant are supported so a
// customer can operate across datacenters and accounts.
type HypervisorHost struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Provider        string
	Name            string
	EndpointURL     string
	CredentialID    uuid.NullUUID
	Datacenter      sql.NullString
	Labels          map[string]any
	HealthStatus    string
	HealthMessage   sql.NullString
	LastVerifiedAt  sql.NullTime
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateHypervisorHostParams defines input for inserting a host.
type CreateHypervisorHostParams struct {
	TenantID     uuid.UUID
	Provider     string
	Name         string
	EndpointURL  string
	CredentialID *uuid.UUID
	Datacenter   string
	Labels       map[string]any
}

// UpdateHypervisorHostParams captures patchable fields on a host.
type UpdateHypervisorHostParams struct {
	Name              *string
	EndpointURL       *string
	CredentialID      *uuid.UUID
	ClearCredentialID bool
	Datacenter        *string
	Labels            *map[string]any
}

// CreateHypervisorHost inserts a new hypervisor host row.
func (s *Store) CreateHypervisorHost(ctx context.Context, params CreateHypervisorHostParams) (*HypervisorHost, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	provider := strings.TrimSpace(params.Provider)
	name := strings.TrimSpace(params.Name)
	endpoint := strings.TrimSpace(params.EndpointURL)
	if provider == "" {
		return nil, errors.New("provider is required")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if endpoint == "" {
		return nil, errors.New("endpoint_url is required")
	}

	labelsJSON, err := marshalJSONBMap(params.Labels)
	if err != nil {
		return nil, fmt.Errorf("encode labels: %w", err)
	}

	var credID any
	if params.CredentialID != nil && *params.CredentialID != uuid.Nil {
		credID = *params.CredentialID
	}

	dc := strings.TrimSpace(params.Datacenter)
	var dcArg any
	if dc != "" {
		dcArg = dc
	}

	id := uuid.New()
	now := s.clock()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO hypervisor_hosts (
			id, tenant_id, provider, name, endpoint_url, credential_id, datacenter,
			labels, health_status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'unknown', $9, $9)
		RETURNING id, tenant_id, provider, name, endpoint_url, credential_id, datacenter,
		          labels, health_status, health_message, last_verified_at, created_at, updated_at
	`, id, params.TenantID, provider, name, endpoint, credID, dcArg, labelsJSON, now)

	return scanHypervisorHostRow(row)
}

// GetHypervisorHost returns a host row by id.
func (s *Store) GetHypervisorHost(ctx context.Context, id uuid.UUID) (*HypervisorHost, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, provider, name, endpoint_url, credential_id, datacenter,
		       labels, health_status, health_message, last_verified_at, created_at, updated_at
		FROM hypervisor_hosts
		WHERE id = $1
	`, id)
	return scanHypervisorHostRow(row)
}

// ListHypervisorHosts lists hosts filtered by tenant/provider.
func (s *Store) ListHypervisorHosts(ctx context.Context, tenantID uuid.UUID, provider string, limit, offset int) ([]HypervisorHost, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if tenantID != uuid.Nil {
		args = append(args, tenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if p := strings.TrimSpace(provider); p != "" {
		args = append(args, p)
		clauses = append(clauses, fmt.Sprintf("provider = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM hypervisor_hosts WHERE %s`, strings.Join(clauses, " AND "))
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count hypervisor hosts: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, provider, name, endpoint_url, credential_id, datacenter,
		       labels, health_status, health_message, last_verified_at, created_at, updated_at
		FROM hypervisor_hosts
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query hypervisor hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hosts []HypervisorHost
	for rows.Next() {
		host, err := scanHypervisorHostRow(rows)
		if err != nil {
			return nil, 0, err
		}
		hosts = append(hosts, *host)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate hypervisor hosts: %w", err)
	}
	return hosts, total, nil
}

// UpdateHypervisorHost applies patchable fields.
func (s *Store) UpdateHypervisorHost(ctx context.Context, id uuid.UUID, params UpdateHypervisorHostParams) (*HypervisorHost, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}

	setFragments := []string{}
	args := []any{id}
	idx := 2

	if params.Name != nil {
		setFragments = append(setFragments, fmt.Sprintf("name = $%d", idx))
		args = append(args, strings.TrimSpace(*params.Name))
		idx++
	}
	if params.EndpointURL != nil {
		setFragments = append(setFragments, fmt.Sprintf("endpoint_url = $%d", idx))
		args = append(args, strings.TrimSpace(*params.EndpointURL))
		idx++
	}
	if params.ClearCredentialID {
		setFragments = append(setFragments, "credential_id = NULL")
	} else if params.CredentialID != nil {
		setFragments = append(setFragments, fmt.Sprintf("credential_id = $%d", idx))
		args = append(args, *params.CredentialID)
		idx++
	}
	if params.Datacenter != nil {
		setFragments = append(setFragments, fmt.Sprintf("datacenter = $%d", idx))
		dc := strings.TrimSpace(*params.Datacenter)
		if dc == "" {
			args = append(args, nil)
		} else {
			args = append(args, dc)
		}
		idx++
	}
	if params.Labels != nil {
		encoded, err := marshalJSONBMap(*params.Labels)
		if err != nil {
			return nil, fmt.Errorf("encode labels: %w", err)
		}
		setFragments = append(setFragments, fmt.Sprintf("labels = $%d", idx))
		args = append(args, encoded)
		idx++
	}

	if len(setFragments) == 0 {
		return s.GetHypervisorHost(ctx, id)
	}

	setFragments = append(setFragments, fmt.Sprintf("updated_at = $%d", idx))
	args = append(args, s.clock())

	query := fmt.Sprintf(`
		UPDATE hypervisor_hosts
		SET %s
		WHERE id = $1
		RETURNING id, tenant_id, provider, name, endpoint_url, credential_id, datacenter,
		          labels, health_status, health_message, last_verified_at, created_at, updated_at
	`, strings.Join(setFragments, ", "))

	row := s.db.QueryRowContext(ctx, query, args...)
	return scanHypervisorHostRow(row)
}

// RecordHypervisorHostHealth updates the health status after a verify call.
func (s *Store) RecordHypervisorHostHealth(ctx context.Context, id uuid.UUID, status, message string) (*HypervisorHost, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	var msgArg any
	if strings.TrimSpace(message) != "" {
		msgArg = message
	}
	now := s.clock()
	row := s.db.QueryRowContext(ctx, `
		UPDATE hypervisor_hosts
		SET health_status = $2, health_message = $3, last_verified_at = $4, updated_at = $4
		WHERE id = $1
		RETURNING id, tenant_id, provider, name, endpoint_url, credential_id, datacenter,
		          labels, health_status, health_message, last_verified_at, created_at, updated_at
	`, id, status, msgArg, now)
	return scanHypervisorHostRow(row)
}

// DeleteHypervisorHost removes a host. Clusters that reference it have their
// hypervisor_host_id set NULL via ON DELETE SET NULL.
func (s *Store) DeleteHypervisorHost(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("id is required")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM hypervisor_hosts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete hypervisor host: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete hypervisor host rows affected: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanHypervisorHostRow(row rowScanner) (*HypervisorHost, error) {
	var (
		host        HypervisorHost
		labelsBytes []byte
	)
	if err := row.Scan(
		&host.ID,
		&host.TenantID,
		&host.Provider,
		&host.Name,
		&host.EndpointURL,
		&host.CredentialID,
		&host.Datacenter,
		&labelsBytes,
		&host.HealthStatus,
		&host.HealthMessage,
		&host.LastVerifiedAt,
		&host.CreatedAt,
		&host.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan hypervisor host: %w", err)
	}
	if len(labelsBytes) > 0 {
		if err := json.Unmarshal(labelsBytes, &host.Labels); err != nil {
			return nil, fmt.Errorf("unmarshal labels: %w", err)
		}
	}
	if host.Labels == nil {
		host.Labels = map[string]any{}
	}
	return &host, nil
}
