package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// CustomDashboard is a user-authored dashboard. Each one belongs to a
// tenant and is owned by a single user; shared=true grants read access to
// any user in the same tenant with dashboards.read.
type CustomDashboard struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	OwnerID     uuid.UUID       `json:"owner_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Layout      json.RawMessage `json:"layout"`
	Shared      bool            `json:"shared"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Widgets     []DashboardWidget `json:"widgets,omitempty"`
}

// DashboardWidget is one card on a dashboard.
type DashboardWidget struct {
	ID             uuid.UUID       `json:"id"`
	DashboardID    uuid.UUID       `json:"dashboard_id"`
	Title          string          `json:"title"`
	WidgetType     string          `json:"widget_type"`
	Spec           json.RawMessage `json:"spec"`
	NodeIDs        []uuid.UUID     `json:"node_ids"`
	RefreshSeconds int             `json:"refresh_seconds"`
	SortOrder      int             `json:"sort_order"`
}

// CreateDashboard inserts a new dashboard. Returns the populated row.
func (s *Store) CreateDashboard(ctx context.Context, tenantID, ownerID uuid.UUID, name, description string, shared bool) (*CustomDashboard, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("dashboard name required")
	}
	d := CustomDashboard{
		ID: uuid.New(), TenantID: tenantID, OwnerID: ownerID,
		Name: name, Description: description, Shared: shared,
		Layout: json.RawMessage("{}"),
	}
	err := s.db.QueryRowContext(ctx, `
INSERT INTO custom_dashboards (id, tenant_id, owner_id, name, description, layout, shared)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
RETURNING created_at, updated_at`,
		d.ID, d.TenantID, d.OwnerID, d.Name, d.Description, string(d.Layout), d.Shared,
	).Scan(&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDashboardsForUser returns dashboards the user can see — owned OR
// shared in the same tenant.
func (s *Store) ListDashboardsForUser(ctx context.Context, tenantID, userID uuid.UUID) ([]CustomDashboard, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, owner_id, name, COALESCE(description,''),
       layout::text, shared, created_at, updated_at
FROM custom_dashboards
WHERE tenant_id = $1 AND (owner_id = $2 OR shared = true)
ORDER BY updated_at DESC`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CustomDashboard, 0, 8)
	for rows.Next() {
		var d CustomDashboard
		var layoutTxt string
		if err := rows.Scan(&d.ID, &d.TenantID, &d.OwnerID, &d.Name, &d.Description,
			&layoutTxt, &d.Shared, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Layout = json.RawMessage(layoutTxt)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDashboard returns a single dashboard with all widgets joined. Returns
// nil + nil when the row doesn't exist or the user can't read it.
func (s *Store) GetDashboard(ctx context.Context, dashboardID, userID uuid.UUID) (*CustomDashboard, error) {
	const dq = `
SELECT id, tenant_id, owner_id, name, COALESCE(description,''),
       layout::text, shared, created_at, updated_at
FROM custom_dashboards
WHERE id = $1 AND (owner_id = $2 OR shared = true)
LIMIT 1`
	var d CustomDashboard
	var layoutTxt string
	err := s.db.QueryRowContext(ctx, dq, dashboardID, userID).Scan(
		&d.ID, &d.TenantID, &d.OwnerID, &d.Name, &d.Description,
		&layoutTxt, &d.Shared, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Layout = json.RawMessage(layoutTxt)

	wrows, err := s.db.QueryContext(ctx, `
SELECT id, dashboard_id, title, widget_type, spec::text, node_ids,
       refresh_seconds, sort_order
FROM custom_dashboard_widgets
WHERE dashboard_id = $1
ORDER BY sort_order`, dashboardID)
	if err != nil {
		return &d, err
	}
	defer wrows.Close()
	for wrows.Next() {
		var w DashboardWidget
		var specTxt string
		var nodeUUIDs pq.StringArray
		if err := wrows.Scan(&w.ID, &w.DashboardID, &w.Title, &w.WidgetType,
			&specTxt, &nodeUUIDs, &w.RefreshSeconds, &w.SortOrder); err != nil {
			return &d, err
		}
		w.Spec = json.RawMessage(specTxt)
		for _, sid := range nodeUUIDs {
			if u, err := uuid.Parse(sid); err == nil {
				w.NodeIDs = append(w.NodeIDs, u)
			}
		}
		d.Widgets = append(d.Widgets, w)
	}
	return &d, nil
}

// UpdateDashboard updates name + description + shared + layout. Owner-only.
func (s *Store) UpdateDashboard(ctx context.Context, dashboardID, ownerID uuid.UUID, name, description string, shared bool, layout json.RawMessage) error {
	if len(layout) == 0 {
		layout = json.RawMessage("{}")
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE custom_dashboards
   SET name = $1, description = $2, shared = $3, layout = $4::jsonb, updated_at = NOW()
 WHERE id = $5 AND owner_id = $6`,
		name, description, shared, string(layout), dashboardID, ownerID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("dashboard not found or not owned by user")
	}
	return nil
}

// DeleteDashboard removes a dashboard + cascades to widgets. Owner-only.
func (s *Store) DeleteDashboard(ctx context.Context, dashboardID, ownerID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM custom_dashboards WHERE id = $1 AND owner_id = $2`,
		dashboardID, ownerID)
	return err
}

// CreateWidget appends a widget to a dashboard. Validates widget_type.
func (s *Store) CreateWidget(ctx context.Context, w DashboardWidget) (*DashboardWidget, error) {
	switch w.WidgetType {
	case "db_query", "sys_resources", "log_size", "network_bytes":
	default:
		return nil, errors.New("invalid widget_type")
	}
	if w.RefreshSeconds <= 0 {
		w.RefreshSeconds = 30
	}
	if w.RefreshSeconds < 5 {
		w.RefreshSeconds = 5 // floor — the agent heartbeat is 60s anyway
	}
	if len(w.Spec) == 0 {
		w.Spec = json.RawMessage("{}")
	}
	w.ID = uuid.New()
	nodeStrs := make([]string, len(w.NodeIDs))
	for i, n := range w.NodeIDs {
		nodeStrs[i] = n.String()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO custom_dashboard_widgets
    (id, dashboard_id, title, widget_type, spec, node_ids, refresh_seconds, sort_order)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)`,
		w.ID, w.DashboardID, w.Title, w.WidgetType,
		string(w.Spec), pq.Array(nodeStrs), w.RefreshSeconds, w.SortOrder)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// UpdateWidget replaces title + spec + node_ids + refresh + sort_order.
func (s *Store) UpdateWidget(ctx context.Context, w DashboardWidget) error {
	switch w.WidgetType {
	case "db_query", "sys_resources", "log_size", "network_bytes":
	default:
		return errors.New("invalid widget_type")
	}
	if w.RefreshSeconds < 5 {
		w.RefreshSeconds = 5
	}
	if len(w.Spec) == 0 {
		w.Spec = json.RawMessage("{}")
	}
	nodeStrs := make([]string, len(w.NodeIDs))
	for i, n := range w.NodeIDs {
		nodeStrs[i] = n.String()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE custom_dashboard_widgets
   SET title = $1, widget_type = $2, spec = $3::jsonb, node_ids = $4,
       refresh_seconds = $5, sort_order = $6, updated_at = NOW()
 WHERE id = $7`,
		w.Title, w.WidgetType, string(w.Spec), pq.Array(nodeStrs),
		w.RefreshSeconds, w.SortOrder, w.ID)
	return err
}

// DeleteWidget removes a widget. Caller must verify ownership of the
// parent dashboard.
func (s *Store) DeleteWidget(ctx context.Context, widgetID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM custom_dashboard_widgets WHERE id = $1`, widgetID)
	return err
}
