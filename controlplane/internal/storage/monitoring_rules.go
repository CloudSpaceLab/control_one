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

// ListEnabledRulesForNode returns enabled port and log monitoring rules for
// the given tenant that match the node's labels. Rules with empty
// target_labels apply to all nodes.
func (s *Store) ListEnabledRulesForNode(ctx context.Context, tenantID uuid.UUID, nodeLabels map[string]any) ([]PortMonitoringRule, []LogMonitoringRule, error) {
	if s.db == nil {
		return nil, nil, errors.New("store database not initialized")
	}

	enabled := true
	portFilter := PortRuleFilter{TenantID: tenantID, Enabled: &enabled}
	portRules, _, err := s.ListPortRules(ctx, portFilter, 500, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("list port rules: %w", err)
	}

	logFilter := LogRuleFilter{TenantID: tenantID, Enabled: &enabled}
	logRules, _, err := s.ListLogRules(ctx, logFilter, 500, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("list log rules: %w", err)
	}

	matchedPorts := filterRulesByLabels(portRules, nodeLabels)
	matchedLogs := filterRulesByLabels(logRules, nodeLabels)
	return matchedPorts, matchedLogs, nil
}

// filterRulesByLabels returns rules whose target_labels are a subset of the
// node's labels. Rules with empty/nil target_labels always match.
func filterRulesByLabels[T interface{ GetTargetLabels() map[string]any }](rules []T, nodeLabels map[string]any) []T {
	var out []T
	for _, r := range rules {
		tl := r.GetTargetLabels()
		if len(tl) == 0 {
			out = append(out, r)
			continue
		}
		if labelsMatch(nodeLabels, tl) {
			out = append(out, r)
		}
	}
	return out
}

func labelsMatch(nodeLabels, ruleLabels map[string]any) bool {
	if len(ruleLabels) == 0 {
		return true
	}
	if len(nodeLabels) == 0 {
		return false
	}
	for k, v := range ruleLabels {
		nv, ok := nodeLabels[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", nv) != fmt.Sprintf("%v", v) {
			return false
		}
	}
	return true
}

func (r PortMonitoringRule) GetTargetLabels() map[string]any { return r.TargetLabels }
func (r LogMonitoringRule) GetTargetLabels() map[string]any  { return r.TargetLabels }

// ---- metric_threshold_rules ----

type MetricThresholdRule struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	MetricName    string
	Operator      string
	Threshold     float64
	WindowSeconds int
	Severity      string
	Action        string
	TargetNodeID  *uuid.UUID
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateMetricThresholdRuleParams struct {
	TenantID      uuid.UUID
	Name          string
	MetricName    string
	Operator      string
	Threshold     float64
	WindowSeconds int
	Severity      string
	Action        string
	TargetNodeID  *uuid.UUID
	Enabled       bool
}

type UpdateMetricThresholdRuleParams struct {
	Name          *string
	MetricName    *string
	Operator      *string
	Threshold     *float64
	WindowSeconds *int
	Severity      *string
	Action        *string
	TargetNodeID  *uuid.UUID
	Enabled       *bool
}

var validOperators = map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true, "eq": true}

func (s *Store) CreateMetricThresholdRule(ctx context.Context, p CreateMetricThresholdRuleParams) (*MetricThresholdRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, errors.New("name required")
	}
	metricName := strings.TrimSpace(p.MetricName)
	if metricName == "" {
		return nil, errors.New("metric_name required")
	}
	op := strings.ToLower(strings.TrimSpace(p.Operator))
	if !validOperators[op] {
		return nil, errors.New("operator must be gt, gte, lt, lte, or eq")
	}
	if p.WindowSeconds <= 0 {
		p.WindowSeconds = 300
	}
	severity := strings.TrimSpace(p.Severity)
	if severity == "" {
		severity = "medium"
	}
	action := strings.TrimSpace(p.Action)
	if action == "" {
		action = "notify"
	}

	id := uuid.New()
	now := s.clock()
	var targetNodeArg any
	if p.TargetNodeID != nil && *p.TargetNodeID != uuid.Nil {
		targetNodeArg = *p.TargetNodeID
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metric_threshold_rules (
			id, tenant_id, name, metric_name, operator, threshold,
			window_seconds, severity, action, target_node_id, enabled, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
	`, id, p.TenantID, name, metricName, op, p.Threshold,
		p.WindowSeconds, severity, action, targetNodeArg, p.Enabled, now)
	if err != nil {
		return nil, fmt.Errorf("insert metric threshold rule: %w", err)
	}
	return s.GetMetricThresholdRule(ctx, id)
}

func (s *Store) GetMetricThresholdRule(ctx context.Context, id uuid.UUID) (*MetricThresholdRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, metricThresholdRuleSelectSQL+` WHERE id = $1`, id)
	return scanMetricThresholdRule(row)
}

func (s *Store) ListEnabledMetricThresholdRules(ctx context.Context, tenantID uuid.UUID) ([]MetricThresholdRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, metricThresholdRuleSelectSQL+` WHERE tenant_id = $1 AND enabled = true`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MetricThresholdRule
	for rows.Next() {
		r, err := scanMetricThresholdRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

type MetricThresholdRuleFilter struct {
	TenantID uuid.UUID
	Enabled  *bool
}

func (s *Store) ListMetricThresholdRules(ctx context.Context, filter MetricThresholdRuleFilter, limit, offset int) ([]MetricThresholdRule, int, error) {
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
	if filter.Enabled != nil {
		where = append(where, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *filter.Enabled)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metric_threshold_rules WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	q := metricThresholdRuleSelectSQL + ` WHERE ` + whereSQL + fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []MetricThresholdRule
	for rows.Next() {
		r, err := scanMetricThresholdRule(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateMetricThresholdRule(ctx context.Context, id uuid.UUID, p UpdateMetricThresholdRuleParams) (*MetricThresholdRule, error) {
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
	if p.MetricName != nil {
		sets = append(sets, fmt.Sprintf("metric_name = $%d", idx))
		args = append(args, strings.TrimSpace(*p.MetricName))
		idx++
	}
	if p.Operator != nil {
		op := strings.ToLower(strings.TrimSpace(*p.Operator))
		if !validOperators[op] {
			return nil, errors.New("operator must be gt, gte, lt, lte, or eq")
		}
		sets = append(sets, fmt.Sprintf("operator = $%d", idx))
		args = append(args, op)
		idx++
	}
	if p.Threshold != nil {
		sets = append(sets, fmt.Sprintf("threshold = $%d", idx))
		args = append(args, *p.Threshold)
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
	if p.TargetNodeID != nil {
		sets = append(sets, fmt.Sprintf("target_node_id = $%d", idx))
		args = append(args, *p.TargetNodeID)
		idx++
	}
	if p.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *p.Enabled)
		idx++
	}
	args = append(args, id)
	q := `UPDATE metric_threshold_rules SET ` + strings.Join(sets, ", ") + fmt.Sprintf(` WHERE id = $%d`, idx)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("update metric threshold rule: %w", err)
	}
	return s.GetMetricThresholdRule(ctx, id)
}

func (s *Store) DeleteMetricThresholdRule(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM metric_threshold_rules WHERE id = $1`, id)
	return err
}

func (s *Store) CountMetricValueInWindow(ctx context.Context, tenantID, nodeID uuid.UUID, metricName, operator string, threshold, windowSeconds float64) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var comparison string
	switch operator {
	case "gt":
		comparison = ">"
	case "gte":
		comparison = ">="
	case "lt":
		comparison = "<"
	case "lte":
		comparison = "<="
	case "eq":
		comparison = "="
	default:
		return 0, fmt.Errorf("invalid operator: %s", operator)
	}
	var count int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM telemetry_metrics
		WHERE tenant_id = $1 AND node_id = $2 AND metric_name = $3
		AND timestamp > NOW() - INTERVAL '1 second' * $4
		AND metric_value %s $5
	`, comparison), tenantID, nodeID, metricName, windowSeconds, threshold).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count metric values in window: %w", err)
	}
	return count, nil
}

const metricThresholdRuleSelectSQL = `
	SELECT id, tenant_id, name, metric_name, operator, threshold,
		window_seconds, severity, action, target_node_id, enabled, created_at, updated_at
	FROM metric_threshold_rules
`

func scanMetricThresholdRule(sc scanner) (*MetricThresholdRule, error) {
	var r MetricThresholdRule
	if err := sc.Scan(
		&r.ID, &r.TenantID, &r.Name, &r.MetricName, &r.Operator, &r.Threshold,
		&r.WindowSeconds, &r.Severity, &r.Action, &r.TargetNodeID, &r.Enabled, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}
