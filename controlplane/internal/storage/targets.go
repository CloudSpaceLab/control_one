package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Target is estate identity. NodeID is present only for agent-managed compute.
// Collection readiness is independent of lifecycle and device reachability.
type Target struct {
	ID                         uuid.UUID            `json:"id"`
	TenantID                   uuid.UUID            `json:"tenant_id"`
	NodeID                     *uuid.UUID           `json:"node_id,omitempty"`
	Family                     string               `json:"family"`
	Type                       string               `json:"type"`
	Subtype                    string               `json:"subtype"`
	Hostname                   string               `json:"hostname"`
	DisplayName                string               `json:"display_name"`
	Site                       string               `json:"site"`
	Group                      string               `json:"group"`
	Vendor                     string               `json:"vendor"`
	Model                      string               `json:"model"`
	Platform                   string               `json:"platform"`
	Firmware                   string               `json:"firmware"`
	Serial                     string               `json:"serial"`
	LifecycleState             string               `json:"lifecycle_state"`
	ReachabilityState          string               `json:"reachability_state"`
	ReachabilityMode           string               `json:"reachability_mode"`
	CollectionState            string               `json:"collection_state"`
	ManagementModes            []string             `json:"management_modes"`
	Capabilities               []string             `json:"capabilities"`
	Classification             TargetClassification `json:"classification"`
	Addresses                  []TargetAddress      `json:"addresses"`
	LastObservedAt             *time.Time           `json:"last_observed_at,omitempty"`
	LastSuccessfulCollectionAt *time.Time           `json:"last_successful_collection_at,omitempty"`
	CreatedAt                  time.Time            `json:"created_at"`
	UpdatedAt                  time.Time            `json:"updated_at"`
}

type TargetAddress struct {
	Purpose     string    `json:"purpose"`
	Address     string    `json:"address"`
	Source      string    `json:"source"`
	Confidence  int       `json:"confidence"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Current     bool      `json:"current"`
}

// NetworkTargetParams deliberately excludes secrets and collected facts.
// Creation records operator assertions, not verified inventory or readiness.
type NetworkTargetParams struct {
	TenantID            uuid.UUID `json:"tenant_id"`
	Type                string    `json:"type"`
	Subtype             string    `json:"subtype"`
	Hostname            string    `json:"hostname"`
	DisplayName         string    `json:"display_name"`
	Site                string    `json:"site"`
	Group               string    `json:"group"`
	ManagementAddresses []string  `json:"management_addresses"`
}

func (p *NetworkTargetParams) Validate() error {
	if p.TenantID == uuid.Nil {
		return errors.New("tenant_id is required")
	}
	p.Type = strings.TrimSpace(p.Type)
	switch p.Type {
	case "router", "switch", "firewall", "load_balancer", "waf", "vpn_gateway", "wireless_controller", "access_point", "ids_ips", "network_appliance":
	default:
		return errors.New("invalid network device type")
	}
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	p.Hostname = strings.TrimSpace(p.Hostname)
	p.Site = strings.TrimSpace(p.Site)
	p.Group = strings.TrimSpace(p.Group)
	p.Subtype = strings.TrimSpace(p.Subtype)
	if p.DisplayName == "" {
		return errors.New("display_name is required")
	}
	for _, value := range []string{p.DisplayName, p.Hostname, p.Site, p.Group, p.Subtype} {
		if len(value) > 255 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("identity fields must be single-line and at most 255 bytes")
		}
	}
	if len(p.ManagementAddresses) == 0 || len(p.ManagementAddresses) > 16 {
		return errors.New("provide 1 to 16 management addresses")
	}
	seen := map[string]bool{}
	addresses := make([]string, 0, len(p.ManagementAddresses))
	for _, raw := range p.ManagementAddresses {
		address := strings.TrimSpace(raw)
		if ip := net.ParseIP(address); ip != nil {
			address = ip.String()
		} else {
			address = strings.ToLower(strings.TrimSuffix(address, "."))
			if !validTargetDNS(address) {
				return errors.New("management address must be an IP address or DNS name")
			}
		}
		if !seen[address] {
			addresses = append(addresses, address)
			seen[address] = true
		}
	}
	p.ManagementAddresses = addresses
	return nil
}

func validTargetDNS(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// TargetAccess is always tied to a persisted user. Scoped roles and expiration
// are checked in SQL before counting, paginating, reading or creating targets.
type TargetAccess struct {
	UserID     uuid.UUID
	Permission string
}

type TargetFilter struct {
	TenantID                                                     uuid.UUID
	Family, Type, Site, Group, Vendor, Model, Platform, Firmware string
	ReachabilityState, CollectionState, LifecycleState, Search   string
	Limit, Offset                                                int
}

func targetAccessPredicate(tenantExpr string, userArg, permissionArg int) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id = ur.role_id
 WHERE ur.user_id = $%d AND rp.permission_name = $%d
 AND (ur.tenant_id IS NULL OR ur.tenant_id = %s)
 AND (ur.expires_at IS NULL OR ur.expires_at > NOW()))`, userArg, permissionArg, tenantExpr)
}

func targetWhere(f TargetFilter, access TargetAccess) (string, []any, error) {
	if access.UserID == uuid.Nil || access.Permission != "targets.read" {
		return "", nil, errors.New("target read access is required")
	}
	if f.Limit < 1 || f.Limit > 500 || f.Offset < 0 {
		return "", nil, errors.New("limit must be 1 to 500 and offset non-negative")
	}
	args := []any{access.UserID, access.Permission}
	clauses := []string{targetAccessPredicate("t.tenant_id", 1, 2)}
	add := func(column string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("t.%s = $%d", column, len(args)))
	}
	if f.TenantID != uuid.Nil {
		add("tenant_id", f.TenantID)
	}
	for _, field := range []struct{ column, value string }{
		{"family", f.Family}, {"type", f.Type}, {"site", f.Site}, {"device_group", f.Group},
		{"vendor", f.Vendor}, {"model", f.Model}, {"platform", f.Platform}, {"firmware", f.Firmware},
		{"reachability_state", f.ReachabilityState}, {"collection_state", f.CollectionState}, {"lifecycle_state", f.LifecycleState},
	} {
		if field.value != "" {
			add(field.column, field.value)
		}
	}
	if f.Search != "" {
		args = append(args, "%"+escapeLike(strings.ToLower(f.Search))+"%")
		arg := len(args)
		clauses = append(clauses, fmt.Sprintf(`(LOWER(t.display_name) LIKE $%[1]d OR LOWER(t.hostname) LIKE $%[1]d
   OR LOWER(t.serial) LIKE $%[1]d OR EXISTS (SELECT 1 FROM target_addresses a
   WHERE a.target_id = t.id AND a.current AND LOWER(a.address) LIKE $%[1]d))`, arg))
	}
	return strings.Join(clauses, " AND "), args, nil
}

const targetSelect = `SELECT t.id, t.tenant_id, t.node_id, t.family, t.type, t.subtype, t.hostname,
 t.display_name, t.site, t.device_group, t.vendor, t.model, t.platform, t.firmware, t.serial,
 t.lifecycle_state, t.reachability_state, t.reachability_mode, t.collection_state,
 t.management_modes, t.capabilities, t.classification, t.last_observed_at,
 t.last_successful_collection_at, t.created_at, t.updated_at,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('purpose', a.purpose, 'address', a.address, 'source', a.source,
 'confidence', a.confidence, 'first_seen_at', a.first_seen_at, 'last_seen_at', a.last_seen_at, 'current', a.current)
 ORDER BY a.purpose, a.address, a.source) FROM target_addresses a WHERE a.target_id = t.id), '[]')
 FROM targets t`

func scanTarget(row interface{ Scan(...any) error }) (*Target, error) {
	var t Target
	var nodeID uuid.NullUUID
	var modes, capabilities, classification, addresses []byte
	if err := row.Scan(&t.ID, &t.TenantID, &nodeID, &t.Family, &t.Type, &t.Subtype, &t.Hostname,
		&t.DisplayName, &t.Site, &t.Group, &t.Vendor, &t.Model, &t.Platform, &t.Firmware, &t.Serial,
		&t.LifecycleState, &t.ReachabilityState, &t.ReachabilityMode, &t.CollectionState,
		&modes, &capabilities, &classification, &t.LastObservedAt, &t.LastSuccessfulCollectionAt,
		&t.CreatedAt, &t.UpdatedAt, &addresses); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if nodeID.Valid {
		t.NodeID = &nodeID.UUID
	}
	for _, item := range []struct {
		raw  []byte
		dest any
	}{{modes, &t.ManagementModes}, {capabilities, &t.Capabilities}, {classification, &t.Classification}, {addresses, &t.Addresses}} {
		if err := json.Unmarshal(item.raw, item.dest); err != nil {
			return nil, fmt.Errorf("decode target evidence: %w", err)
		}
	}
	return &t, nil
}

func (s *Store) ListTargets(ctx context.Context, f TargetFilter, access TargetAccess) ([]Target, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	where, args, err := targetWhere(f, access)
	if err != nil {
		return nil, 0, err
	}
	// Count and page use one snapshot, including authorization, under concurrent writes.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM targets t WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := tx.QueryContext(ctx, targetSelect+" WHERE "+where+fmt.Sprintf(" ORDER BY t.display_name, t.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	targets := make([]Target, 0)
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, 0, err
		}
		targets = append(targets, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return targets, total, nil
}

func (s *Store) GetTarget(ctx context.Context, id uuid.UUID, access TargetAccess) (*Target, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	where, args, err := targetWhere(TargetFilter{Limit: 1}, access)
	if err != nil {
		return nil, err
	}
	args = append(args, id)
	return scanTarget(s.db.QueryRowContext(ctx, targetSelect+" WHERE "+where+fmt.Sprintf(" AND t.id = $%d", len(args)), args...))
}

func (s *Store) CreateNetworkTarget(ctx context.Context, p NetworkTargetParams, access TargetAccess) (*Target, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if access.UserID == uuid.Nil || access.Permission != "targets.write" {
		return nil, errors.New("target write access is required")
	}
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	target, err := createNetworkTargetTx(ctx, tx, p, access, s.clock().UTC())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return target, nil
}

func createNetworkTargetTx(ctx context.Context, tx *sql.Tx, p NetworkTargetParams, access TargetAccess, now time.Time) (*Target, error) {
	id := uuid.New()
	classification := TargetClassification{Source: "operator", Confidence: 100, Evidence: []string{"operator-selected device type; not protocol verified"}}
	raw, err := json.Marshal(classification)
	if err != nil {
		return nil, err
	}
	// INSERT SELECT refuses creation outside the user's authorized tenant.
	err = tx.QueryRowContext(ctx, `INSERT INTO targets (id, tenant_id, family, type, subtype, hostname, display_name, site, device_group, classification, created_at, updated_at)
 SELECT $1, tenant.id, 'network_security', $3, $4, $5, $6, $7, $8, $9, $10, $10 FROM tenants tenant
 WHERE tenant.id = $2 AND `+targetAccessPredicate("tenant.id", 11, 12)+` RETURNING id`,
		id, p.TenantID, p.Type, p.Subtype, p.Hostname, p.DisplayName, p.Site, p.Group, raw, now, access.UserID, access.Permission).Scan(&id)
	if err != nil {
		return nil, err
	}
	for _, address := range p.ManagementAddresses {
		if _, err := tx.ExecContext(ctx, `INSERT INTO target_addresses (target_id, tenant_id, purpose, address, source, confidence, first_seen_at, last_seen_at)
   VALUES ($1, $2, 'management', $3, 'operator', 100, $4, $4)`, id, p.TenantID, address, now); err != nil {
			return nil, err
		}
	}
	// Return inside the transaction; write-only operators can see their creation receipt.
	target, err := scanTarget(tx.QueryRowContext(ctx, targetSelect+" WHERE t.id = $1", id))
	if err != nil {
		return nil, err
	}
	return target, nil
}
