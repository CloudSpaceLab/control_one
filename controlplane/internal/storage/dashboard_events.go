package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------- security_events ----------

type SecurityEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	NodeID    uuid.NullUUID
	EventType string
	Severity  string
	Source    string
	Details   map[string]any
	DedupKey  sql.NullString
	FiredAt   time.Time
}

type CreateSecurityEventParams struct {
	TenantID  uuid.UUID
	NodeID    *uuid.UUID
	EventType string
	Severity  string
	Source    string
	Details   map[string]any
	DedupKey  string
}

func (s *Store) CreateSecurityEvent(ctx context.Context, p CreateSecurityEventParams) (*SecurityEvent, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	if strings.TrimSpace(p.EventType) == "" || strings.TrimSpace(p.Source) == "" {
		return nil, errors.New("event_type and source required")
	}
	if p.Severity == "" {
		p.Severity = "medium"
	}
	detailsJSON, err := marshalJSONBMap(p.Details)
	if err != nil {
		return nil, err
	}
	var nodeID any
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nodeID = *p.NodeID
	}
	var dedup any
	if strings.TrimSpace(p.DedupKey) != "" {
		dedup = p.DedupKey
	}
	id := uuid.New()
	now := s.clock()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO security_events (id, tenant_id, node_id, event_type, severity, source, details, dedup_key, fired_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, id, p.TenantID, nodeID, p.EventType, p.Severity, p.Source, detailsJSON, dedup, now)
	if err != nil {
		return nil, fmt.Errorf("insert security event: %w", err)
	}
	return s.GetSecurityEvent(ctx, id)
}

func (s *Store) GetSecurityEvent(ctx context.Context, id uuid.UUID) (*SecurityEvent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, event_type, severity, source, details, dedup_key, fired_at
		FROM security_events WHERE id = $1
	`, id)
	var e SecurityEvent
	var raw []byte
	if err := row.Scan(&e.ID, &e.TenantID, &e.NodeID, &e.EventType, &e.Severity, &e.Source, &raw, &e.DedupKey, &e.FiredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	details, err := decodeJSONBMap(raw)
	if err != nil {
		return nil, err
	}
	e.Details = details
	return &e, nil
}

type SecurityEventFilter struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Severity string
	Since    *time.Time
}

type SecurityEventCounts struct {
	Critical int
	High     int
	Medium   int
	Low      int
	Total    int
}

func (s *Store) CountSecurityEvents(ctx context.Context, f SecurityEventFilter) (SecurityEventCounts, error) {
	var c SecurityEventCounts
	if s.db == nil {
		return c, errors.New("store database not initialized")
	}
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if f.TenantID != uuid.Nil {
		where = append(where, fmt.Sprintf("tenant_id = $%d", idx))
		args = append(args, f.TenantID)
		idx++
	}
	if f.NodeID != uuid.Nil {
		where = append(where, fmt.Sprintf("node_id = $%d", idx))
		args = append(args, f.NodeID)
		idx++
	}
	if f.Since != nil {
		where = append(where, fmt.Sprintf("fired_at >= $%d", idx))
		args = append(args, *f.Since)
		idx++
	}
	q := `
		SELECT
			COUNT(*) FILTER (WHERE severity = 'critical') AS critical,
			COUNT(*) FILTER (WHERE severity = 'high') AS high,
			COUNT(*) FILTER (WHERE severity = 'medium') AS medium,
			COUNT(*) FILTER (WHERE severity = 'low') AS low,
			COUNT(*) AS total
		FROM security_events WHERE ` + strings.Join(where, " AND ")
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&c.Critical, &c.High, &c.Medium, &c.Low, &c.Total); err != nil {
		return c, err
	}
	return c, nil
}

func (s *Store) ListSecurityEvents(ctx context.Context, f SecurityEventFilter, limit, offset int) ([]SecurityEvent, int, error) {
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
	if f.NodeID != uuid.Nil {
		where = append(where, fmt.Sprintf("node_id = $%d", idx))
		args = append(args, f.NodeID)
		idx++
	}
	if strings.TrimSpace(f.Severity) != "" {
		where = append(where, fmt.Sprintf("severity = $%d", idx))
		args = append(args, f.Severity)
		idx++
	}
	if f.Since != nil {
		where = append(where, fmt.Sprintf("fired_at >= $%d", idx))
		args = append(args, *f.Since)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_events WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	q := `
		SELECT id, tenant_id, node_id, event_type, severity, source, details, dedup_key, fired_at
		FROM security_events WHERE ` + whereSQL +
		fmt.Sprintf(` ORDER BY fired_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []SecurityEvent
	for rows.Next() {
		var e SecurityEvent
		var raw []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.NodeID, &e.EventType, &e.Severity, &e.Source, &raw, &e.DedupKey, &e.FiredAt); err != nil {
			return nil, 0, err
		}
		details, err := decodeJSONBMap(raw)
		if err != nil {
			return nil, 0, err
		}
		e.Details = details
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ---------- health_incidents ----------

type HealthIncident struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	NodeID       uuid.NullUUID
	IncidentType string
	Severity     string
	Details      map[string]any
	DedupKey     sql.NullString
	OpenedAt     time.Time
	ResolvedAt   sql.NullTime
}

type CreateHealthIncidentParams struct {
	TenantID     uuid.UUID
	NodeID       *uuid.UUID
	IncidentType string
	Severity     string
	Details      map[string]any
	DedupKey     string
}

func (s *Store) CreateHealthIncident(ctx context.Context, p CreateHealthIncidentParams) (*HealthIncident, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.IncidentType) == "" {
		return nil, errors.New("tenant_id and incident_type required")
	}
	if p.Severity == "" {
		p.Severity = "medium"
	}
	detailsJSON, err := marshalJSONBMap(p.Details)
	if err != nil {
		return nil, err
	}
	var nodeID any
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nodeID = *p.NodeID
	}
	var dedup any
	if strings.TrimSpace(p.DedupKey) != "" {
		dedup = p.DedupKey
	}
	id := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO health_incidents (id, tenant_id, node_id, incident_type, severity, details, dedup_key, opened_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, id, p.TenantID, nodeID, p.IncidentType, p.Severity, detailsJSON, dedup, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert health incident: %w", err)
	}
	return s.GetHealthIncident(ctx, id)
}

func (s *Store) GetHealthIncident(ctx context.Context, id uuid.UUID) (*HealthIncident, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, incident_type, severity, details, dedup_key, opened_at, resolved_at
		FROM health_incidents WHERE id = $1
	`, id)
	var h HealthIncident
	var raw []byte
	if err := row.Scan(&h.ID, &h.TenantID, &h.NodeID, &h.IncidentType, &h.Severity, &raw, &h.DedupKey, &h.OpenedAt, &h.ResolvedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m, err := decodeJSONBMap(raw)
	if err != nil {
		return nil, err
	}
	h.Details = m
	return &h, nil
}

func (s *Store) ResolveHealthIncident(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE health_incidents SET resolved_at = NOW() WHERE id = $1 AND resolved_at IS NULL`, id)
	return err
}

// CountOpenHealthIncidents returns currently unresolved incidents grouped by severity.
func (s *Store) CountOpenHealthIncidents(ctx context.Context, tenantID uuid.UUID) (SecurityEventCounts, error) {
	var c SecurityEventCounts
	if s.db == nil {
		return c, errors.New("store database not initialized")
	}
	where := []string{"resolved_at IS NULL"}
	args := []any{}
	if tenantID != uuid.Nil {
		where = append(where, "tenant_id = $1")
		args = append(args, tenantID)
	}
	q := `
		SELECT
			COUNT(*) FILTER (WHERE severity = 'critical'),
			COUNT(*) FILTER (WHERE severity = 'high'),
			COUNT(*) FILTER (WHERE severity = 'medium'),
			COUNT(*) FILTER (WHERE severity = 'low'),
			COUNT(*)
		FROM health_incidents WHERE ` + strings.Join(where, " AND ")
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&c.Critical, &c.High, &c.Medium, &c.Low, &c.Total); err != nil {
		return c, err
	}
	return c, nil
}

// ---------- rule_trigger_log ----------

type RuleTrigger struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	RuleID      uuid.UUID
	RuleType    string
	NodeID      uuid.NullUUID
	Severity    string
	Details     map[string]any
	TriggeredAt time.Time
}

type CreateRuleTriggerParams struct {
	TenantID uuid.UUID
	RuleID   uuid.UUID
	RuleType string
	NodeID   *uuid.UUID
	Severity string
	Details  map[string]any
}

func (s *Store) CreateRuleTrigger(ctx context.Context, p CreateRuleTriggerParams) (*RuleTrigger, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || p.RuleID == uuid.Nil {
		return nil, errors.New("tenant_id and rule_id required")
	}
	ruleType := strings.TrimSpace(p.RuleType)
	if ruleType != "port" && ruleType != "log" && ruleType != "compliance" {
		return nil, errors.New("rule_type must be port, log, or compliance")
	}
	if p.Severity == "" {
		p.Severity = "medium"
	}
	detailsJSON, err := marshalJSONBMap(p.Details)
	if err != nil {
		return nil, err
	}
	var nodeID any
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nodeID = *p.NodeID
	}
	id := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO rule_trigger_log (id, tenant_id, rule_id, rule_type, node_id, severity, details, triggered_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, id, p.TenantID, p.RuleID, ruleType, nodeID, p.Severity, detailsJSON, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert rule trigger: %w", err)
	}
	return &RuleTrigger{ID: id, TenantID: p.TenantID, RuleID: p.RuleID, RuleType: ruleType, Severity: p.Severity}, nil
}

// CountRuleTriggersSince returns total rule triggers grouped by rule_type since t.
func (s *Store) CountRuleTriggersSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (map[string]int, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	where := []string{"triggered_at >= $1"}
	args := []any{since}
	idx := 2
	if tenantID != uuid.Nil {
		where = append(where, fmt.Sprintf("tenant_id = $%d", idx))
		args = append(args, tenantID)
	}
	q := `SELECT rule_type, COUNT(*) FROM rule_trigger_log WHERE ` + strings.Join(where, " AND ") + ` GROUP BY rule_type`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}
