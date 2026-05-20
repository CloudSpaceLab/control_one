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

type Alert struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	RuleID     uuid.NullUUID
	NodeID     uuid.NullUUID
	Source     string
	Severity   string
	Title      string
	Summary    sql.NullString
	State      string
	DedupKey   sql.NullString
	Context    map[string]any
	OpenedAt   time.Time
	AckedAt    sql.NullTime
	AckedBy    uuid.NullUUID
	ResolvedAt sql.NullTime
	ResolvedBy uuid.NullUUID
}

type CreateAlertParams struct {
	TenantID uuid.UUID
	RuleID   *uuid.UUID
	NodeID   *uuid.UUID
	Source   string
	Severity string
	Title    string
	Summary  string
	DedupKey string
	Context  map[string]any
}

type UpdateAlertDispositionParams struct {
	Disposition   string
	Reason        string
	SuppressUntil *time.Time
	By            uuid.UUID
	At            time.Time
}

// CreateAlert inserts a new alert. If DedupKey is set and an open alert with
// the same (tenant, dedup_key) exists, ErrAlertDeduped is returned along with
// the existing alert — callers treat this as idempotent.
var ErrAlertDeduped = errors.New("alert deduplicated")

func (s *Store) CreateAlert(ctx context.Context, p CreateAlertParams) (*Alert, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.Title) == "" {
		return nil, errors.New("tenant_id and title required")
	}
	if p.Severity == "" {
		p.Severity = "medium"
	}
	if p.Source == "" {
		p.Source = "system"
	}
	ctxJSON, err := marshalJSONBMap(p.Context)
	if err != nil {
		return nil, err
	}
	var ruleID, nodeID any
	if p.RuleID != nil && *p.RuleID != uuid.Nil {
		ruleID = *p.RuleID
	}
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nodeID = *p.NodeID
	}
	var dedupArg any
	if strings.TrimSpace(p.DedupKey) != "" {
		dedupArg = p.DedupKey
		// Fast-path: return existing open alert if it's already open.
		existing, err := s.findOpenAlertByDedup(ctx, p.TenantID, p.DedupKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, ErrAlertDeduped
		}
	}
	var summaryArg any
	if strings.TrimSpace(p.Summary) != "" {
		summaryArg = p.Summary
	}

	id := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO alerts (id, tenant_id, rule_id, node_id, source, severity, title, summary, state, dedup_key, context, opened_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'open',$9,$10,$11)
	`, id, p.TenantID, ruleID, nodeID, p.Source, p.Severity, p.Title, summaryArg, dedupArg, ctxJSON, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert alert: %w", err)
	}
	return s.GetAlert(ctx, id)
}

func (s *Store) findOpenAlertByDedup(ctx context.Context, tenant uuid.UUID, key string) (*Alert, error) {
	row := s.db.QueryRowContext(ctx, alertSelectSQL+` WHERE tenant_id = $1 AND dedup_key = $2 AND state = 'open' LIMIT 1`, tenant, key)
	return scanAlert(row)
}

func (s *Store) GetAlert(ctx context.Context, id uuid.UUID) (*Alert, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, alertSelectSQL+` WHERE id = $1`, id)
	return scanAlert(row)
}

type AlertFilter struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	State    string
	Severity string
	Since    *time.Time
}

func (s *Store) ListAlerts(ctx context.Context, f AlertFilter, limit, offset int) ([]Alert, int, error) {
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
	if strings.TrimSpace(f.State) != "" {
		where = append(where, fmt.Sprintf("state = $%d", idx))
		args = append(args, f.State)
		idx++
	}
	if strings.TrimSpace(f.Severity) != "" {
		where = append(where, fmt.Sprintf("severity = $%d", idx))
		args = append(args, f.Severity)
		idx++
	}
	if f.Since != nil {
		where = append(where, fmt.Sprintf("opened_at >= $%d", idx))
		args = append(args, *f.Since)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, offset)
	q := alertSelectSQL + ` WHERE ` + whereSQL + fmt.Sprintf(` ORDER BY opened_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

func (s *Store) AckAlert(ctx context.Context, id uuid.UUID, by uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var byArg any
	if by != uuid.Nil {
		byArg = by
	}
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET state = 'acked', acked_at = NOW(), acked_by = $1 WHERE id = $2 AND state = 'open'`, byArg, id)
	return err
}

func (s *Store) ResolveAlert(ctx context.Context, id uuid.UUID, by uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var byArg any
	if by != uuid.Nil {
		byArg = by
	}
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = NOW(), resolved_by = $1 WHERE id = $2 AND state != 'resolved'`, byArg, id)
	return err
}

func (s *Store) UpdateAlertDisposition(ctx context.Context, id uuid.UUID, p UpdateAlertDispositionParams) (*Alert, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	disposition := strings.ToLower(strings.TrimSpace(p.Disposition))
	if disposition == "" {
		return nil, errors.New("disposition is required")
	}
	alert, err := s.GetAlert(ctx, id)
	if err != nil {
		return nil, err
	}
	if alert == nil {
		return nil, sql.ErrNoRows
	}
	at := p.At.UTC()
	if at.IsZero() {
		at = s.clock().UTC()
	}
	contextMap := alert.Context
	if contextMap == nil {
		contextMap = map[string]any{}
	}
	entry := map[string]any{
		"value":      disposition,
		"updated_at": at.Format(time.RFC3339),
	}
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		entry["reason"] = reason
	}
	if p.By != uuid.Nil {
		entry["updated_by"] = p.By.String()
	}
	if p.SuppressUntil != nil && !p.SuppressUntil.IsZero() {
		entry["suppress_until"] = p.SuppressUntil.UTC().Format(time.RFC3339)
	}
	contextMap["disposition"] = entry
	contextMap["disposition_history"] = appendJSONHistory(contextMap["disposition_history"], entry)
	ctxJSON, err := marshalJSONBMap(contextMap)
	if err != nil {
		return nil, err
	}
	state := alertStateForDisposition(disposition, alert.State)
	var byArg any
	if p.By != uuid.Nil {
		byArg = p.By
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE alerts
		   SET context = $1,
		       state = $2,
		       acked_at = CASE WHEN $2 = 'acked' AND acked_at IS NULL THEN $3 ELSE acked_at END,
		       acked_by = CASE WHEN $2 = 'acked' AND acked_by IS NULL THEN $4::uuid ELSE acked_by END,
		       resolved_at = CASE WHEN $2 = 'resolved' THEN COALESCE(resolved_at, $3) ELSE resolved_at END,
		       resolved_by = CASE WHEN $2 = 'resolved' THEN COALESCE(resolved_by, $4::uuid) ELSE resolved_by END
		 WHERE id = $5
	`, ctxJSON, state, at, byArg, id)
	if err != nil {
		return nil, fmt.Errorf("update alert disposition: %w", err)
	}
	return s.GetAlert(ctx, id)
}

func alertStateForDisposition(disposition, current string) string {
	switch strings.ToLower(strings.TrimSpace(disposition)) {
	case "true_positive":
		if strings.EqualFold(strings.TrimSpace(current), "resolved") {
			return "resolved"
		}
		return "acked"
	case "false_positive", "benign_positive", "accepted_risk", "suppressed", "resolved":
		return "resolved"
	default:
		return nonEmptyString(current, "open")
	}
}

func appendJSONHistory(existing any, entry map[string]any) []any {
	history := []any{}
	switch values := existing.(type) {
	case []any:
		history = append(history, values...)
	case []map[string]any:
		for _, value := range values {
			history = append(history, value)
		}
	}
	history = append(history, entry)
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	return history
}

func nonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const alertSelectSQL = `
	SELECT id, tenant_id, rule_id, node_id, source, severity, title, summary, state, dedup_key,
		context, opened_at, acked_at, acked_by, resolved_at, resolved_by
	FROM alerts
`

func scanAlert(sc scanner) (*Alert, error) {
	var a Alert
	var ctxRaw []byte
	if err := sc.Scan(
		&a.ID, &a.TenantID, &a.RuleID, &a.NodeID, &a.Source, &a.Severity, &a.Title, &a.Summary,
		&a.State, &a.DedupKey, &ctxRaw, &a.OpenedAt, &a.AckedAt, &a.AckedBy, &a.ResolvedAt, &a.ResolvedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m, err := decodeJSONBMap(ctxRaw)
	if err != nil {
		return nil, err
	}
	a.Context = m
	return &a, nil
}
