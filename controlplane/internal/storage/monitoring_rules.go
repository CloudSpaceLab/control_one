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

// PortMonitoringRule represents a port-state rule evaluated by the nodeagent
// port scanner. expected_state is "open" or "closed".
type PortMonitoringRule struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	PolicyID      uuid.NullUUID
	Name          string
	Port          int
	Protocol      string
	ExpectedState string
	TargetLabels  map[string]any
	Severity      string
	Action        string
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreatePortRuleParams struct {
	TenantID      uuid.UUID
	PolicyID      *uuid.UUID
	Name          string
	Port          int
	Protocol      string
	ExpectedState string
	TargetLabels  map[string]any
	Severity      string
	Action        string
	Enabled       bool
}

type UpdatePortRuleParams struct {
	Name          *string
	Port          *int
	Protocol      *string
	ExpectedState *string
	TargetLabels  *map[string]any
	Severity      *string
	Action        *string
	Enabled       *bool
}

func (s *Store) CreatePortRule(ctx context.Context, params CreatePortRuleParams) (*PortMonitoringRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, errors.New("name required")
	}
	if params.Port < 1 || params.Port > 65535 {
		return nil, errors.New("port must be 1..65535")
	}
	protocol := strings.ToLower(strings.TrimSpace(params.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" && protocol != "udp" {
		return nil, errors.New("protocol must be tcp or udp")
	}
	expected := strings.ToLower(strings.TrimSpace(params.ExpectedState))
	if expected != "open" && expected != "closed" {
		return nil, errors.New("expected_state must be open or closed")
	}

	labels, err := marshalJSONBMap(params.TargetLabels)
	if err != nil {
		return nil, err
	}
	severity := strings.TrimSpace(params.Severity)
	if severity == "" {
		severity = "medium"
	}
	action := strings.TrimSpace(params.Action)
	if action == "" {
		action = "notify"
	}

	id := uuid.New()
	now := s.clock()
	var policyID any
	if params.PolicyID != nil && *params.PolicyID != uuid.Nil {
		policyID = *params.PolicyID
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO port_monitoring_rules (
			id, tenant_id, policy_id, name, port, protocol, expected_state,
			target_labels, severity, action, enabled, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
	`, id, params.TenantID, policyID, name, params.Port, protocol, expected,
		labels, severity, action, params.Enabled, now)
	if err != nil {
		return nil, fmt.Errorf("insert port rule: %w", err)
	}
	return s.GetPortRule(ctx, id)
}

func (s *Store) GetPortRule(ctx context.Context, id uuid.UUID) (*PortMonitoringRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, portRuleSelectSQL+` WHERE id = $1`, id)
	return scanPortRule(row)
}

type PortRuleFilter struct {
	TenantID uuid.UUID
	PolicyID uuid.UUID
	Enabled  *bool
}

func (s *Store) ListPortRules(ctx context.Context, filter PortRuleFilter, limit, offset int) ([]PortMonitoringRule, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if filter.TenantID != uuid.Nil {
		where = append(where, fmt.Sprintf("tenant_id = $%d", idx))
		args = append(args, filter.TenantID)
		idx++
	}
	if filter.PolicyID != uuid.Nil {
		where = append(where, fmt.Sprintf("policy_id = $%d", idx))
		args = append(args, filter.PolicyID)
		idx++
	}
	if filter.Enabled != nil {
		where = append(where, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *filter.Enabled)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM port_monitoring_rules WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	q := portRuleSelectSQL + ` WHERE ` + whereSQL + fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []PortMonitoringRule
	for rows.Next() {
		r, err := scanPortRule(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdatePortRule(ctx context.Context, id uuid.UUID, p UpdatePortRuleParams) (*PortMonitoringRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	sets := []string{"updated_at = $1"}
	args := []any{s.clock()}
	idx := 2
	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", idx))
		args = append(args, strings.TrimSpace(*p.Name))
		idx++
	}
	if p.Port != nil {
		if *p.Port < 1 || *p.Port > 65535 {
			return nil, errors.New("port must be 1..65535")
		}
		sets = append(sets, fmt.Sprintf("port = $%d", idx))
		args = append(args, *p.Port)
		idx++
	}
	if p.Protocol != nil {
		sets = append(sets, fmt.Sprintf("protocol = $%d", idx))
		args = append(args, strings.ToLower(*p.Protocol))
		idx++
	}
	if p.ExpectedState != nil {
		sets = append(sets, fmt.Sprintf("expected_state = $%d", idx))
		args = append(args, strings.ToLower(*p.ExpectedState))
		idx++
	}
	if p.TargetLabels != nil {
		b, err := marshalJSONBMap(*p.TargetLabels)
		if err != nil {
			return nil, err
		}
		sets = append(sets, fmt.Sprintf("target_labels = $%d", idx))
		args = append(args, b)
		idx++
	}
	if p.Severity != nil {
		sets = append(sets, fmt.Sprintf("severity = $%d", idx))
		args = append(args, *p.Severity)
		idx++
	}
	if p.Action != nil {
		sets = append(sets, fmt.Sprintf("action = $%d", idx))
		args = append(args, *p.Action)
		idx++
	}
	if p.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *p.Enabled)
		idx++
	}
	args = append(args, id)
	q := `UPDATE port_monitoring_rules SET ` + strings.Join(sets, ", ") + fmt.Sprintf(` WHERE id = $%d`, idx)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("update port rule: %w", err)
	}
	return s.GetPortRule(ctx, id)
}

func (s *Store) DeletePortRule(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM port_monitoring_rules WHERE id = $1`, id)
	return err
}

const portRuleSelectSQL = `
	SELECT id, tenant_id, policy_id, name, port, protocol, expected_state,
		target_labels, severity, action, enabled, created_at, updated_at
	FROM port_monitoring_rules
`

type scanner interface {
	Scan(dest ...any) error
}

func scanPortRule(sc scanner) (*PortMonitoringRule, error) {
	var r PortMonitoringRule
	var labelsRaw []byte
	if err := sc.Scan(
		&r.ID, &r.TenantID, &r.PolicyID, &r.Name, &r.Port, &r.Protocol, &r.ExpectedState,
		&labelsRaw, &r.Severity, &r.Action, &r.Enabled, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	labels, err := decodeJSONBMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	r.TargetLabels = labels
	return &r, nil
}

// ---- log_monitoring_rules ----

type LogMonitoringRule struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	PolicyID      uuid.NullUUID
	Name          string
	LogSource     string
	Pattern       string
	Severity      string
	WindowSeconds int
	Threshold     int
	Action        string
	TargetLabels  map[string]any
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateLogRuleParams struct {
	TenantID      uuid.UUID
	PolicyID      *uuid.UUID
	Name          string
	LogSource     string
	Pattern       string
	Severity      string
	WindowSeconds int
	Threshold     int
	Action        string
	TargetLabels  map[string]any
	Enabled       bool
}

type UpdateLogRuleParams struct {
	Name          *string
	LogSource     *string
	Pattern       *string
	Severity      *string
	WindowSeconds *int
	Threshold     *int
	Action        *string
	TargetLabels  *map[string]any
	Enabled       *bool
}

func (s *Store) CreateLogRule(ctx context.Context, p CreateLogRuleParams) (*LogMonitoringRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.LogSource) == "" || strings.TrimSpace(p.Pattern) == "" {
		return nil, errors.New("name, log_source, pattern required")
	}
	if p.WindowSeconds <= 0 {
		p.WindowSeconds = 60
	}
	if p.Threshold <= 0 {
		p.Threshold = 1
	}
	if p.Severity == "" {
		p.Severity = "medium"
	}
	if p.Action == "" {
		p.Action = "notify"
	}
	labels, err := marshalJSONBMap(p.TargetLabels)
	if err != nil {
		return nil, err
	}
	var policyID any
	if p.PolicyID != nil && *p.PolicyID != uuid.Nil {
		policyID = *p.PolicyID
	}
	id := uuid.New()
	now := s.clock()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO log_monitoring_rules (
			id, tenant_id, policy_id, name, log_source, pattern, severity,
			window_seconds, threshold, action, target_labels, enabled, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
	`, id, p.TenantID, policyID, p.Name, p.LogSource, p.Pattern, p.Severity,
		p.WindowSeconds, p.Threshold, p.Action, labels, p.Enabled, now)
	if err != nil {
		return nil, fmt.Errorf("insert log rule: %w", err)
	}
	return s.GetLogRule(ctx, id)
}

func (s *Store) GetLogRule(ctx context.Context, id uuid.UUID) (*LogMonitoringRule, error) {
	row := s.db.QueryRowContext(ctx, logRuleSelectSQL+` WHERE id = $1`, id)
	return scanLogRule(row)
}

type LogRuleFilter struct {
	TenantID  uuid.UUID
	PolicyID  uuid.UUID
	LogSource string
	Enabled   *bool
}

func (s *Store) ListLogRules(ctx context.Context, f LogRuleFilter, limit, offset int) ([]LogMonitoringRule, int, error) {
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
	if f.PolicyID != uuid.Nil {
		where = append(where, fmt.Sprintf("policy_id = $%d", idx))
		args = append(args, f.PolicyID)
		idx++
	}
	if strings.TrimSpace(f.LogSource) != "" {
		where = append(where, fmt.Sprintf("log_source = $%d", idx))
		args = append(args, f.LogSource)
		idx++
	}
	if f.Enabled != nil {
		where = append(where, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *f.Enabled)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM log_monitoring_rules WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	q := logRuleSelectSQL + ` WHERE ` + whereSQL + fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []LogMonitoringRule
	for rows.Next() {
		r, err := scanLogRule(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateLogRule(ctx context.Context, id uuid.UUID, p UpdateLogRuleParams) (*LogMonitoringRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	sets := []string{"updated_at = $1"}
	args := []any{s.clock()}
	idx := 2
	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", idx))
		args = append(args, *p.Name)
		idx++
	}
	if p.LogSource != nil {
		sets = append(sets, fmt.Sprintf("log_source = $%d", idx))
		args = append(args, *p.LogSource)
		idx++
	}
	if p.Pattern != nil {
		sets = append(sets, fmt.Sprintf("pattern = $%d", idx))
		args = append(args, *p.Pattern)
		idx++
	}
	if p.Severity != nil {
		sets = append(sets, fmt.Sprintf("severity = $%d", idx))
		args = append(args, *p.Severity)
		idx++
	}
	if p.WindowSeconds != nil {
		if *p.WindowSeconds <= 0 {
			return nil, errors.New("window_seconds must be > 0")
		}
		sets = append(sets, fmt.Sprintf("window_seconds = $%d", idx))
		args = append(args, *p.WindowSeconds)
		idx++
	}
	if p.Threshold != nil {
		if *p.Threshold <= 0 {
			return nil, errors.New("threshold must be > 0")
		}
		sets = append(sets, fmt.Sprintf("threshold = $%d", idx))
		args = append(args, *p.Threshold)
		idx++
	}
	if p.Action != nil {
		sets = append(sets, fmt.Sprintf("action = $%d", idx))
		args = append(args, *p.Action)
		idx++
	}
	if p.TargetLabels != nil {
		b, err := marshalJSONBMap(*p.TargetLabels)
		if err != nil {
			return nil, err
		}
		sets = append(sets, fmt.Sprintf("target_labels = $%d", idx))
		args = append(args, b)
		idx++
	}
	if p.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *p.Enabled)
		idx++
	}
	args = append(args, id)
	q := `UPDATE log_monitoring_rules SET ` + strings.Join(sets, ", ") + fmt.Sprintf(` WHERE id = $%d`, idx)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("update log rule: %w", err)
	}
	return s.GetLogRule(ctx, id)
}

func (s *Store) DeleteLogRule(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM log_monitoring_rules WHERE id = $1`, id)
	return err
}

const logRuleSelectSQL = `
	SELECT id, tenant_id, policy_id, name, log_source, pattern, severity,
		window_seconds, threshold, action, target_labels, enabled, created_at, updated_at
	FROM log_monitoring_rules
`

func scanLogRule(sc scanner) (*LogMonitoringRule, error) {
	var r LogMonitoringRule
	var labelsRaw []byte
	if err := sc.Scan(
		&r.ID, &r.TenantID, &r.PolicyID, &r.Name, &r.LogSource, &r.Pattern, &r.Severity,
		&r.WindowSeconds, &r.Threshold, &r.Action, &labelsRaw, &r.Enabled, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	labels, err := decodeJSONBMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	r.TargetLabels = labels
	return &r, nil
}
