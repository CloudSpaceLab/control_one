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

// SavedSearch mirrors a row in the saved_searches table.
type SavedSearch struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	OwnerUserID  uuid.UUID       `json:"owner_user_id"`
	Name         string          `json:"name"`
	Query        string          `json:"query"`
	EntityType   string          `json:"entity_type,omitempty"`
	Filters      json.RawMessage `json:"filters,omitempty"`
	Shared       bool            `json:"shared"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// EntityTag mirrors a row in the entity_tags table.
type EntityTag struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	EntityType string     `json:"entity_type"`
	EntityID   string     `json:"entity_id"`
	Tag        string     `json:"tag"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// EntityAction captures an operator-driven block / allow / quarantine
// decision recorded from the Investigate UI.
type EntityAction struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	EntityType string     `json:"entity_type"`
	EntityID   string     `json:"entity_id"`
	Action     string     `json:"action"`
	Reason     string     `json:"reason,omitempty"`
	TTLSeconds *int       `json:"ttl_seconds,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// AssetCIDR is a tenant-declared IP range used by the IP classifier.
type AssetCIDR struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	CIDR      string    `json:"cidr"`
	Name      string    `json:"name,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// LifecycleItem is one row in a unified per-entity timeline. It merges
// rows from security_events / alerts / audit_logs / session_recordings /
// entity_actions so the UI can show a single time-ordered feed.
type LifecycleItem struct {
	Timestamp  time.Time      `json:"ts"`
	Source     string         `json:"source"` // event|alert|audit|session|remediation|action
	Severity   string         `json:"severity,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	Target     string         `json:"target,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	RawID      string         `json:"raw_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// EntitySummary returns counts + first/last-seen for an entity. The
// counts are best-effort: when a backing table doesn't exist, the field
// is left at 0.
type EntitySummary struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	FirstSeen *time.Time     `json:"first_seen,omitempty"`
	LastSeen  *time.Time     `json:"last_seen,omitempty"`
	Counts    map[string]int `json:"counts"`
	Meta      map[string]any `json:"meta,omitempty"`
}

// LifecycleFilter scopes a lifecycle query to a tenant + window + sources.
type LifecycleFilter struct {
	TenantID   uuid.UUID
	EntityType string
	EntityID   string
	Since      *time.Time
	Until      *time.Time
	Sources    []string
}

// ===== Saved searches =====

// CreateSavedSearch inserts a new saved search and returns the row.
func (s *Store) CreateSavedSearch(ctx context.Context, in SavedSearch) (*SavedSearch, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if len(in.Filters) == 0 {
		in.Filters = json.RawMessage(`{}`)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO saved_searches (id, tenant_id, owner_user_id, name, query, entity_type, filters, shared)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7::jsonb, $8)
		RETURNING id, tenant_id, owner_user_id, name, query,
		          COALESCE(entity_type, ''), filters, shared, created_at, updated_at
	`, in.ID, in.TenantID, in.OwnerUserID, in.Name, in.Query, in.EntityType, []byte(in.Filters), in.Shared)
	return scanSavedSearch(row)
}

// ListSavedSearches returns saved searches the user can see — their own
// plus tenant-shared ones.
func (s *Store) ListSavedSearches(ctx context.Context, tenantID, userID uuid.UUID, limit, offset int) ([]SavedSearch, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, owner_user_id, name, query,
		       COALESCE(entity_type, ''), filters, shared, created_at, updated_at
		FROM saved_searches
		WHERE tenant_id = $1 AND (owner_user_id = $2 OR shared = true)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, tenantID, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list saved searches: %w", err)
	}
	defer rows.Close()

	out := make([]SavedSearch, 0, limit)
	for rows.Next() {
		ss, err := scanSavedSearch(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *ss)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	_ = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM saved_searches
		WHERE tenant_id = $1 AND (owner_user_id = $2 OR shared = true)
	`, tenantID, userID).Scan(&total)

	return out, total, nil
}

// GetSavedSearch returns a single saved search by id.
func (s *Store) GetSavedSearch(ctx context.Context, id uuid.UUID) (*SavedSearch, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, owner_user_id, name, query,
		       COALESCE(entity_type, ''), filters, shared, created_at, updated_at
		FROM saved_searches WHERE id = $1
	`, id)
	return scanSavedSearch(row)
}

// UpdateSavedSearch overwrites name/query/filters/shared.
func (s *Store) UpdateSavedSearch(ctx context.Context, id uuid.UUID, in SavedSearch) (*SavedSearch, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if len(in.Filters) == 0 {
		in.Filters = json.RawMessage(`{}`)
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE saved_searches
		   SET name = $2, query = $3, entity_type = NULLIF($4,''),
		       filters = $5::jsonb, shared = $6, updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, tenant_id, owner_user_id, name, query,
		           COALESCE(entity_type, ''), filters, shared, created_at, updated_at
	`, id, in.Name, in.Query, in.EntityType, []byte(in.Filters), in.Shared)
	return scanSavedSearch(row)
}

// DeleteSavedSearch removes a saved search by id.
func (s *Store) DeleteSavedSearch(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM saved_searches WHERE id = $1`, id)
	return err
}

func scanSavedSearch(row interface {
	Scan(dest ...any) error
}) (*SavedSearch, error) {
	var (
		ss      SavedSearch
		filters []byte
	)
	if err := row.Scan(
		&ss.ID, &ss.TenantID, &ss.OwnerUserID, &ss.Name, &ss.Query,
		&ss.EntityType, &filters, &ss.Shared, &ss.CreatedAt, &ss.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(filters) > 0 {
		ss.Filters = json.RawMessage(filters)
	}
	return &ss, nil
}

// ===== Entity tags =====

// AddEntityTag inserts a (entity_type, entity_id, tag) triple. Duplicate
// inserts are silently ignored.
func (s *Store) AddEntityTag(ctx context.Context, t EntityTag) (*EntityTag, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO entity_tags (id, tenant_id, entity_type, entity_id, tag, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, entity_type, entity_id, tag)
		DO UPDATE SET tag = EXCLUDED.tag
		RETURNING id, tenant_id, entity_type, entity_id, tag, created_by, created_at
	`, t.ID, t.TenantID, t.EntityType, t.EntityID, t.Tag, t.CreatedBy)
	out := EntityTag{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.EntityType, &out.EntityID, &out.Tag, &out.CreatedBy, &out.CreatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveEntityTag deletes a single tag.
func (s *Store) RemoveEntityTag(ctx context.Context, tenantID uuid.UUID, entityType, entityID, tag string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM entity_tags
		WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3 AND tag = $4
	`, tenantID, entityType, entityID, tag)
	return err
}

// ListEntityTags returns every tag attached to a given entity.
func (s *Store) ListEntityTags(ctx context.Context, tenantID uuid.UUID, entityType, entityID string) ([]EntityTag, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, entity_type, entity_id, tag, created_by, created_at
		FROM entity_tags
		WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3
		ORDER BY created_at DESC
	`, tenantID, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityTag
	for rows.Next() {
		var t EntityTag
		if err := rows.Scan(&t.ID, &t.TenantID, &t.EntityType, &t.EntityID, &t.Tag, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ===== Entity actions =====

// RecordEntityAction inserts a block / allow / quarantine action. The
// caller is responsible for also writing an audit_log row.
func (s *Store) RecordEntityAction(ctx context.Context, a EntityAction) (*EntityAction, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	var ttl sql.NullInt64
	if a.TTLSeconds != nil {
		ttl = sql.NullInt64{Int64: int64(*a.TTLSeconds), Valid: true}
	}
	var expires sql.NullTime
	if a.ExpiresAt != nil {
		expires = sql.NullTime{Time: *a.ExpiresAt, Valid: true}
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO entity_actions
		    (id, tenant_id, entity_type, entity_id, action, reason, ttl_seconds, expires_at, created_by)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9)
		RETURNING id, tenant_id, entity_type, entity_id, action,
		          COALESCE(reason,''), ttl_seconds, expires_at, created_by, created_at
	`, a.ID, a.TenantID, a.EntityType, a.EntityID, a.Action, a.Reason, ttl, expires, a.CreatedBy)
	var (
		out      EntityAction
		ttlScan  sql.NullInt64
		expScan  sql.NullTime
	)
	if err := row.Scan(
		&out.ID, &out.TenantID, &out.EntityType, &out.EntityID, &out.Action,
		&out.Reason, &ttlScan, &expScan, &out.CreatedBy, &out.CreatedAt,
	); err != nil {
		return nil, err
	}
	if ttlScan.Valid {
		v := int(ttlScan.Int64)
		out.TTLSeconds = &v
	}
	if expScan.Valid {
		t := expScan.Time
		out.ExpiresAt = &t
	}
	return &out, nil
}

// ===== Asset CIDRs =====

// ListAssetCIDRs returns the IP ranges declared by tenantID. Used by the
// IP classifier — kept light so the API can call it once per request.
func (s *Store) ListAssetCIDRs(ctx context.Context, tenantID uuid.UUID) ([]net.IPNet, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cidr::text FROM asset_cidrs WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []net.IPNet
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		_, n, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// ===== Lifecycle =====

// EntityLifecycle merges rows from security_events, alerts, audit_logs,
// session_recordings and entity_actions, time-ordered and limited.
//
// TODO(investigate): once Doris is the lifecycle source for raw events,
// query Doris for the events portion and merge in-process here.
func (s *Store) EntityLifecycle(ctx context.Context, f LifecycleFilter, limit int) ([]LifecycleItem, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 100
	}

	since := f.Since
	until := f.Until
	if since == nil {
		t := time.Now().UTC().AddDate(0, 0, -30)
		since = &t
	}
	if until == nil {
		t := time.Now().UTC().Add(time.Minute)
		until = &t
	}

	wantSource := func(name string) bool {
		if len(f.Sources) == 0 {
			return true
		}
		for _, s := range f.Sources {
			if strings.EqualFold(s, name) {
				return true
			}
		}
		return false
	}

	out := make([]LifecycleItem, 0, limit)

	// Pattern matched against JSONB columns. Treat entity ID as a literal
	// substring to keep the query simple — the JSONB GIN index from 0066
	// keeps this responsive at moderate cardinality.
	jsonNeedle := "%" + escapeLike(f.EntityID) + "%"

	if wantSource("event") {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id::text, severity, source, fired_at, details
			FROM security_events
			WHERE tenant_id = $1
			  AND fired_at BETWEEN $2 AND $3
			  AND (
			        details::text ILIKE $4
			     OR id::text = $5
			  )
			ORDER BY fired_at DESC
			LIMIT $6
		`, f.TenantID, *since, *until, jsonNeedle, f.EntityID, limit)
		if err == nil {
			for rows.Next() {
				var (
					id, severity, source string
					ts                   time.Time
					details              []byte
				)
				if err := rows.Scan(&id, &severity, &source, &ts, &details); err != nil {
					continue
				}
				meta := map[string]any{}
				_ = json.Unmarshal(details, &meta)
				out = append(out, LifecycleItem{
					Timestamp: ts,
					Source:    "event",
					Severity:  severity,
					Summary:   source,
					RawID:     id,
					Metadata:  meta,
				})
			}
			rows.Close()
		}
	}

	if wantSource("alert") {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id::text, severity, title, COALESCE(summary,''), opened_at, context
			FROM alerts
			WHERE tenant_id = $1
			  AND opened_at BETWEEN $2 AND $3
			  AND (
			        context::text ILIKE $4
			     OR id::text = $5
			  )
			ORDER BY opened_at DESC
			LIMIT $6
		`, f.TenantID, *since, *until, jsonNeedle, f.EntityID, limit)
		if err == nil {
			for rows.Next() {
				var (
					id, severity, title, summary string
					ts                           time.Time
					context                      []byte
				)
				if err := rows.Scan(&id, &severity, &title, &summary, &ts, &context); err != nil {
					continue
				}
				meta := map[string]any{}
				_ = json.Unmarshal(context, &meta)
				body := title
				if summary != "" {
					body = title + " — " + summary
				}
				out = append(out, LifecycleItem{
					Timestamp: ts,
					Source:    "alert",
					Severity:  severity,
					Summary:   body,
					RawID:     id,
					Metadata:  meta,
				})
			}
			rows.Close()
		}
	}

	if wantSource("audit") {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id::text, COALESCE(actor_type,''), action, resource_type,
			       COALESCE(resource_id, ''), created_at, metadata
			FROM audit_logs
			WHERE tenant_id = $1
			  AND created_at BETWEEN $2 AND $3
			  AND (
			        resource_id = $5
			     OR metadata::text ILIKE $4
			  )
			ORDER BY created_at DESC
			LIMIT $6
		`, f.TenantID, *since, *until, jsonNeedle, f.EntityID, limit)
		if err == nil {
			for rows.Next() {
				var (
					id, actor, action, rType, rID string
					ts                            time.Time
					metadata                      []byte
				)
				if err := rows.Scan(&id, &actor, &action, &rType, &rID, &ts, &metadata); err != nil {
					continue
				}
				meta := map[string]any{}
				_ = json.Unmarshal(metadata, &meta)
				out = append(out, LifecycleItem{
					Timestamp: ts,
					Source:    "audit",
					Actor:     actor,
					Target:    rType + ":" + rID,
					Summary:   action,
					RawID:     id,
					Metadata:  meta,
				})
			}
			rows.Close()
		}
	}

	if wantSource("session") {
		// Sessions have a text user_id and a uuid node_id; we accept a
		// match against either.
		rows, err := s.db.QueryContext(ctx, `
			SELECT id::text, COALESCE(user_id,''), session_type, status, started_at
			FROM session_recordings
			WHERE started_at BETWEEN $1 AND $2
			  AND (user_id = $3 OR node_id::text = $3 OR id::text = $3)
			ORDER BY started_at DESC
			LIMIT $4
		`, *since, *until, f.EntityID, limit)
		if err == nil {
			for rows.Next() {
				var (
					id, user, sType, status string
					ts                      time.Time
				)
				if err := rows.Scan(&id, &user, &sType, &status, &ts); err != nil {
					continue
				}
				out = append(out, LifecycleItem{
					Timestamp: ts,
					Source:    "session",
					Actor:     user,
					Summary:   sType + " (" + status + ")",
					RawID:     id,
				})
			}
			rows.Close()
		}
	}

	if wantSource("action") || wantSource("remediation") {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id::text, action, COALESCE(reason,''), created_at
			FROM entity_actions
			WHERE tenant_id = $1
			  AND entity_type = $2
			  AND entity_id = $3
			  AND created_at BETWEEN $4 AND $5
			ORDER BY created_at DESC
			LIMIT $6
		`, f.TenantID, f.EntityType, f.EntityID, *since, *until, limit)
		if err == nil {
			for rows.Next() {
				var (
					id, action, reason string
					ts                 time.Time
				)
				if err := rows.Scan(&id, &action, &reason, &ts); err != nil {
					continue
				}
				out = append(out, LifecycleItem{
					Timestamp: ts,
					Source:    "action",
					Summary:   action,
					Target:    reason,
					RawID:     id,
				})
			}
			rows.Close()
		}
	}

	// Time-order DESC across all merged sources.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Timestamp.After(out[i].Timestamp) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// EntitySummary returns counts + first/last-seen across all merged sources.
func (s *Store) EntitySummary(ctx context.Context, tenantID uuid.UUID, entityType, entityID string) (*EntitySummary, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	sum := &EntitySummary{
		Type:   entityType,
		ID:     entityID,
		Counts: map[string]int{},
	}

	jsonNeedle := "%" + escapeLike(entityID) + "%"

	type rowCount struct {
		key   string
		query string
		args  []any
	}
	queries := []rowCount{
		{
			key:   "events",
			query: `SELECT COUNT(*), MIN(fired_at), MAX(fired_at) FROM security_events WHERE tenant_id = $1 AND (details::text ILIKE $2 OR id::text = $3)`,
			args:  []any{tenantID, jsonNeedle, entityID},
		},
		{
			key:   "alerts",
			query: `SELECT COUNT(*), MIN(opened_at), MAX(opened_at) FROM alerts WHERE tenant_id = $1 AND (context::text ILIKE $2 OR id::text = $3)`,
			args:  []any{tenantID, jsonNeedle, entityID},
		},
		{
			key:   "audit",
			query: `SELECT COUNT(*), MIN(created_at), MAX(created_at) FROM audit_logs WHERE tenant_id = $1 AND (resource_id = $3 OR metadata::text ILIKE $2)`,
			args:  []any{tenantID, jsonNeedle, entityID},
		},
		{
			key:   "sessions",
			query: `SELECT COUNT(*), MIN(started_at), MAX(started_at) FROM session_recordings WHERE user_id = $1 OR node_id::text = $1 OR id::text = $1`,
			args:  []any{entityID},
		},
		{
			key:   "remediations",
			query: `SELECT COUNT(*), MIN(created_at), MAX(created_at) FROM entity_actions WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3`,
			args:  []any{tenantID, entityType, entityID},
		},
	}

	for _, q := range queries {
		var (
			c             int
			firstNT, lastNT sql.NullTime
		)
		if err := s.db.QueryRowContext(ctx, q.query, q.args...).Scan(&c, &firstNT, &lastNT); err != nil {
			continue
		}
		sum.Counts[q.key] = c
		if firstNT.Valid {
			t := firstNT.Time
			if sum.FirstSeen == nil || t.Before(*sum.FirstSeen) {
				sum.FirstSeen = &t
			}
		}
		if lastNT.Valid {
			t := lastNT.Time
			if sum.LastSeen == nil || t.After(*sum.LastSeen) {
				sum.LastSeen = &t
			}
		}
	}

	return sum, nil
}

// escapeLike escapes wildcard chars before composing an ILIKE pattern.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
