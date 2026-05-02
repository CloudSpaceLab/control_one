package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// NodePatchConfig is the per-node patch-mode setting introduced in Wave C.
// mode is one of "direct" (apt/dnf/winget on the node), "proxy" (route
// through a managed Squid), or "airgapped" (read pre-staged repo path).
type NodePatchConfig struct {
	NodeID    uuid.UUID
	Mode      string
	ProxyID   *uuid.UUID
	WindowID  *uuid.UUID
	UpdatedAt time.Time
}

// MaintenanceWindow is an operator-scheduled change window. Status moves
// scheduled → open → closing → closed (or aborted on force-close failure).
// allow_repos is the host list that gets opened in the firewall while the
// window is in the open state.
type MaintenanceWindow struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	NodeIDs       []uuid.UUID
	OpensAt       time.Time
	ClosesAt      time.Time
	AllowRepos    []string
	Status        string
	OpenedBy      *uuid.UUID
	ForceClosedAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SquidProxy is one managed Squid instance. host:port is unique per tenant.
// whitelist is a JSON array of regexes/hostnames the proxy permits.
type SquidProxy struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Host            string
	Port            int
	Status          string
	Whitelist       []string
	LastValidatedAt *time.Time
	LastError       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ── node_patch_config ─────────────────────────────────────────────────────

// GetNodePatchConfig returns the per-node config or nil when the node has
// never been configured (caller should treat the absence as mode=direct).
func (s *Store) GetNodePatchConfig(ctx context.Context, nodeID uuid.UUID) (*NodePatchConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT node_id, mode, proxy_id, window_id, updated_at
		FROM node_patch_config WHERE node_id = $1
	`, nodeID)
	var c NodePatchConfig
	if err := row.Scan(&c.NodeID, &c.Mode, &c.ProxyID, &c.WindowID, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get node patch config: %w", err)
	}
	return &c, nil
}

// UpsertNodePatchConfig inserts or updates the row. Mode validation is left
// to the CHECK constraint — caller-supplied bad modes return a SQL error.
func (s *Store) UpsertNodePatchConfig(ctx context.Context, in NodePatchConfig) (*NodePatchConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	mode := in.Mode
	if mode == "" {
		mode = "direct"
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO node_patch_config (node_id, mode, proxy_id, window_id, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			mode       = EXCLUDED.mode,
			proxy_id   = EXCLUDED.proxy_id,
			window_id  = EXCLUDED.window_id,
			updated_at = NOW()
		RETURNING node_id, mode, proxy_id, window_id, updated_at
	`, in.NodeID, mode, in.ProxyID, in.WindowID)
	var c NodePatchConfig
	if err := row.Scan(&c.NodeID, &c.Mode, &c.ProxyID, &c.WindowID, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("upsert node patch config: %w", err)
	}
	return &c, nil
}

// ── maintenance_windows ───────────────────────────────────────────────────

// CreateMaintenanceWindow inserts a scheduled window.
func (s *Store) CreateMaintenanceWindow(ctx context.Context, in MaintenanceWindow) (*MaintenanceWindow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	nodeIDs := make([]string, 0, len(in.NodeIDs))
	for _, n := range in.NodeIDs {
		nodeIDs = append(nodeIDs, n.String())
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO maintenance_windows
			(tenant_id, name, node_ids, opens_at, closes_at, allow_repos, status)
		VALUES ($1, $2, $3::uuid[], $4, $5, $6, 'scheduled')
		RETURNING id, tenant_id, name, node_ids::text[], opens_at, closes_at,
		          allow_repos, status, opened_by, force_closed_at,
		          created_at, updated_at
	`, in.TenantID, in.Name, pq.Array(nodeIDs), in.OpensAt, in.ClosesAt, pq.Array(in.AllowRepos))
	return scanMaintenanceWindow(row)
}

// GetMaintenanceWindow fetches one window or returns nil when not found.
func (s *Store) GetMaintenanceWindow(ctx context.Context, id uuid.UUID) (*MaintenanceWindow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, node_ids::text[], opens_at, closes_at,
		       allow_repos, status, opened_by, force_closed_at,
		       created_at, updated_at
		FROM maintenance_windows WHERE id = $1
	`, id)
	w, err := scanMaintenanceWindow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// ListMaintenanceWindows returns up-to-200 windows for a tenant in
// reverse-chronological order. Filtering by status is left to the caller.
func (s *Store) ListMaintenanceWindows(ctx context.Context, tenantID uuid.UUID) ([]MaintenanceWindow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, node_ids::text[], opens_at, closes_at,
		       allow_repos, status, opened_by, force_closed_at,
		       created_at, updated_at
		FROM maintenance_windows
		WHERE tenant_id = $1
		ORDER BY opens_at DESC
		LIMIT 200
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MaintenanceWindow
	for rows.Next() {
		w, err := scanMaintenanceWindowRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// MarkMaintenanceWindowOpen flips status → 'open' + records the opener.
func (s *Store) MarkMaintenanceWindowOpen(ctx context.Context, id uuid.UUID, openedBy *uuid.UUID) error {
	return s.setMaintenanceWindowStatus(ctx, id, "open", openedBy, false)
}

// MarkMaintenanceWindowClosing flips status → 'closing' (firewall close in
// flight). Used while we wait for delete jobs to confirm.
func (s *Store) MarkMaintenanceWindowClosing(ctx context.Context, id uuid.UUID) error {
	return s.setMaintenanceWindowStatus(ctx, id, "closing", nil, false)
}

// MarkMaintenanceWindowClosed flips status → 'closed'.
func (s *Store) MarkMaintenanceWindowClosed(ctx context.Context, id uuid.UUID) error {
	return s.setMaintenanceWindowStatus(ctx, id, "closed", nil, false)
}

// MarkMaintenanceWindowAborted flips status → 'aborted' for windows that
// failed to open / close cleanly.
func (s *Store) MarkMaintenanceWindowAborted(ctx context.Context, id uuid.UUID) error {
	return s.setMaintenanceWindowStatus(ctx, id, "aborted", nil, false)
}

// ForceCloseMaintenanceWindow flips an open/closing window to 'closed' and
// stamps force_closed_at. Used by the operator panic button.
func (s *Store) ForceCloseMaintenanceWindow(ctx context.Context, id uuid.UUID) error {
	return s.setMaintenanceWindowStatus(ctx, id, "closed", nil, true)
}

func (s *Store) setMaintenanceWindowStatus(ctx context.Context, id uuid.UUID, status string, openedBy *uuid.UUID, force bool) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if force {
		_, err := s.db.ExecContext(ctx, `
			UPDATE maintenance_windows
			SET status = $2,
			    force_closed_at = NOW(),
			    updated_at = NOW()
			WHERE id = $1
		`, id, status)
		return err
	}
	if openedBy != nil {
		_, err := s.db.ExecContext(ctx, `
			UPDATE maintenance_windows
			SET status = $2,
			    opened_by = $3,
			    updated_at = NOW()
			WHERE id = $1
		`, id, status, openedBy)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE maintenance_windows
		SET status = $2, updated_at = NOW()
		WHERE id = $1
	`, id, status)
	return err
}

// ── squid_proxies ─────────────────────────────────────────────────────────

// CreateSquidProxy inserts a fresh proxy row in 'installing' state.
func (s *Store) CreateSquidProxy(ctx context.Context, in SquidProxy) (*SquidProxy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if in.Whitelist == nil {
		in.Whitelist = []string{}
	}
	whitelistBytes, err := json.Marshal(in.Whitelist)
	if err != nil {
		return nil, fmt.Errorf("marshal whitelist: %w", err)
	}
	port := in.Port
	if port == 0 {
		port = 3128
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO squid_proxies (tenant_id, host, port, status, whitelist)
		VALUES ($1, $2, $3, 'installing', $4)
		RETURNING id, tenant_id, host, port, status, whitelist,
		          last_validated_at, last_error, created_at, updated_at
	`, in.TenantID, in.Host, port, whitelistBytes)
	return scanSquidProxy(row)
}

// GetSquidProxy fetches one proxy by id.
func (s *Store) GetSquidProxy(ctx context.Context, id uuid.UUID) (*SquidProxy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, host, port, status, whitelist,
		       last_validated_at, last_error, created_at, updated_at
		FROM squid_proxies WHERE id = $1
	`, id)
	p, err := scanSquidProxy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ListSquidProxies returns proxies for a tenant in creation order.
func (s *Store) ListSquidProxies(ctx context.Context, tenantID uuid.UUID) ([]SquidProxy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, host, port, status, whitelist,
		       last_validated_at, last_error, created_at, updated_at
		FROM squid_proxies
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT 200
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list squid proxies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SquidProxy
	for rows.Next() {
		p, err := scanSquidProxyRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// UpdateSquidProxyStatus flips the status column with optional last_error.
// Use lastError = "" to clear; a non-empty string is recorded verbatim.
func (s *Store) UpdateSquidProxyStatus(ctx context.Context, id uuid.UUID, status, lastError string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var errPtr *string
	if lastError != "" {
		errPtr = &lastError
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE squid_proxies
		SET status = $2,
		    last_error = $3,
		    last_validated_at = CASE WHEN $2 = 'healthy' THEN NOW() ELSE last_validated_at END,
		    updated_at = NOW()
		WHERE id = $1
	`, id, status, errPtr)
	return err
}

// UpdateSquidProxyWhitelist replaces the whitelist body. Validation is the
// caller's responsibility (the controlplane runs squid -k parse before
// dispatching to the agent).
func (s *Store) UpdateSquidProxyWhitelist(ctx context.Context, id uuid.UUID, whitelist []string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if whitelist == nil {
		whitelist = []string{}
	}
	whitelistBytes, err := json.Marshal(whitelist)
	if err != nil {
		return fmt.Errorf("marshal whitelist: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE squid_proxies
		SET whitelist = $2, updated_at = NOW()
		WHERE id = $1
	`, id, whitelistBytes)
	return err
}

// ── scanners ──────────────────────────────────────────────────────────────

func scanMaintenanceWindow(row *sql.Row) (*MaintenanceWindow, error) {
	w := &MaintenanceWindow{}
	var nodeIDStrs []string
	var allowRepos []string
	if err := row.Scan(
		&w.ID, &w.TenantID, &w.Name, pq.Array(&nodeIDStrs), &w.OpensAt, &w.ClosesAt,
		pq.Array(&allowRepos), &w.Status, &w.OpenedBy, &w.ForceClosedAt,
		&w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return nil, err
	}
	w.NodeIDs = make([]uuid.UUID, 0, len(nodeIDStrs))
	for _, s := range nodeIDStrs {
		if id, err := uuid.Parse(s); err == nil {
			w.NodeIDs = append(w.NodeIDs, id)
		}
	}
	w.AllowRepos = allowRepos
	return w, nil
}

func scanMaintenanceWindowRow(rows *sql.Rows) (*MaintenanceWindow, error) {
	w := &MaintenanceWindow{}
	var nodeIDStrs []string
	var allowRepos []string
	if err := rows.Scan(
		&w.ID, &w.TenantID, &w.Name, pq.Array(&nodeIDStrs), &w.OpensAt, &w.ClosesAt,
		pq.Array(&allowRepos), &w.Status, &w.OpenedBy, &w.ForceClosedAt,
		&w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan maintenance window: %w", err)
	}
	w.NodeIDs = make([]uuid.UUID, 0, len(nodeIDStrs))
	for _, s := range nodeIDStrs {
		if id, err := uuid.Parse(s); err == nil {
			w.NodeIDs = append(w.NodeIDs, id)
		}
	}
	w.AllowRepos = allowRepos
	return w, nil
}

func scanSquidProxy(row *sql.Row) (*SquidProxy, error) {
	p := &SquidProxy{}
	var whitelistBytes []byte
	if err := row.Scan(
		&p.ID, &p.TenantID, &p.Host, &p.Port, &p.Status, &whitelistBytes,
		&p.LastValidatedAt, &p.LastError, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(whitelistBytes) > 0 {
		_ = json.Unmarshal(whitelistBytes, &p.Whitelist)
	}
	if p.Whitelist == nil {
		p.Whitelist = []string{}
	}
	return p, nil
}

func scanSquidProxyRow(rows *sql.Rows) (*SquidProxy, error) {
	p := &SquidProxy{}
	var whitelistBytes []byte
	if err := rows.Scan(
		&p.ID, &p.TenantID, &p.Host, &p.Port, &p.Status, &whitelistBytes,
		&p.LastValidatedAt, &p.LastError, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan squid proxy: %w", err)
	}
	if len(whitelistBytes) > 0 {
		_ = json.Unmarshal(whitelistBytes, &p.Whitelist)
	}
	if p.Whitelist == nil {
		p.Whitelist = []string{}
	}
	return p, nil
}
