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

// ThreatFeed describes one operator-configured threat-intel data source.
type ThreatFeed struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Name               string
	FeedType           string
	URL                sql.NullString
	APIKeySealed       []byte
	Nonce              []byte
	ScoreFloor         int
	RefreshSeconds     int
	Category           sql.NullString
	Enabled            bool
	LastStatus         sql.NullString
	LastError          sql.NullString
	LastIndicatorCount int
	LastRefreshedAt    sql.NullTime
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateThreatFeedParams struct {
	TenantID       uuid.UUID
	Name           string
	FeedType       string
	URL            string
	APIKeySealed   []byte
	Nonce          []byte
	ScoreFloor     int
	RefreshSeconds int
	Category       string
	Enabled        bool
}

type UpdateThreatFeedParams struct {
	Name           *string
	URL            *string
	APIKeySealed   []byte
	Nonce          []byte
	ClearAPIKey    bool
	ScoreFloor     *int
	RefreshSeconds *int
	Category       *string
	Enabled        *bool
}

var validFeedTypes = map[string]bool{
	"spamhaus_drop":   true,
	"spamhaus_edrop":  true,
	"firehol_l1":      true,
	"tor_exit":        true,
	"abuseipdb":       true,
	"otx":             true,
	"custom_lines":    true,
	"custom_spamhaus": true,
}

func (s *Store) CreateThreatFeed(ctx context.Context, p CreateThreatFeedParams) (*ThreatFeed, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.Name) == "" {
		return nil, errors.New("tenant_id and name required")
	}
	if !validFeedTypes[p.FeedType] {
		return nil, fmt.Errorf("invalid feed_type %q", p.FeedType)
	}
	if p.ScoreFloor < 0 || p.ScoreFloor > 100 {
		return nil, errors.New("score_floor must be 0-100")
	}
	if p.RefreshSeconds < 60 {
		p.RefreshSeconds = 3600
	}
	id := uuid.New()
	var urlArg, catArg any
	if strings.TrimSpace(p.URL) != "" {
		urlArg = p.URL
	}
	if strings.TrimSpace(p.Category) != "" {
		catArg = p.Category
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO threat_feeds (id, tenant_id, name, feed_type, url, api_key_sealed, nonce,
			score_floor, refresh_seconds, category, enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
	`, id, p.TenantID, p.Name, p.FeedType, urlArg, p.APIKeySealed, p.Nonce,
		p.ScoreFloor, p.RefreshSeconds, catArg, p.Enabled, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert threat feed: %w", err)
	}
	return s.GetThreatFeed(ctx, id)
}

func (s *Store) GetThreatFeed(ctx context.Context, id uuid.UUID) (*ThreatFeed, error) {
	row := s.db.QueryRowContext(ctx, threatFeedSelectSQL+` WHERE id = $1`, id)
	return scanThreatFeed(row)
}

type ThreatFeedFilter struct {
	TenantID uuid.UUID
	Enabled  *bool
}

func (s *Store) ListThreatFeeds(ctx context.Context, f ThreatFeedFilter) ([]ThreatFeed, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if f.TenantID != uuid.Nil {
		where = append(where, fmt.Sprintf("tenant_id = $%d", idx))
		args = append(args, f.TenantID)
		idx++
	}
	if f.Enabled != nil {
		where = append(where, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *f.Enabled)
	}
	q := threatFeedSelectSQL + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ThreatFeed
	for rows.Next() {
		f, err := scanThreatFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) UpdateThreatFeed(ctx context.Context, id uuid.UUID, p UpdateThreatFeedParams) (*ThreatFeed, error) {
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
	if p.URL != nil {
		sets = append(sets, fmt.Sprintf("url = $%d", idx))
		if strings.TrimSpace(*p.URL) == "" {
			args = append(args, nil)
		} else {
			args = append(args, *p.URL)
		}
		idx++
	}
	if p.ClearAPIKey {
		sets = append(sets, "api_key_sealed = NULL", "nonce = NULL")
	} else if len(p.APIKeySealed) > 0 {
		sets = append(sets, fmt.Sprintf("api_key_sealed = $%d", idx))
		args = append(args, p.APIKeySealed)
		idx++
		sets = append(sets, fmt.Sprintf("nonce = $%d", idx))
		args = append(args, p.Nonce)
		idx++
	}
	if p.ScoreFloor != nil {
		if *p.ScoreFloor < 0 || *p.ScoreFloor > 100 {
			return nil, errors.New("score_floor must be 0-100")
		}
		sets = append(sets, fmt.Sprintf("score_floor = $%d", idx))
		args = append(args, *p.ScoreFloor)
		idx++
	}
	if p.RefreshSeconds != nil {
		if *p.RefreshSeconds < 60 {
			return nil, errors.New("refresh_seconds must be >= 60")
		}
		sets = append(sets, fmt.Sprintf("refresh_seconds = $%d", idx))
		args = append(args, *p.RefreshSeconds)
		idx++
	}
	if p.Category != nil {
		sets = append(sets, fmt.Sprintf("category = $%d", idx))
		if strings.TrimSpace(*p.Category) == "" {
			args = append(args, nil)
		} else {
			args = append(args, *p.Category)
		}
		idx++
	}
	if p.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", idx))
		args = append(args, *p.Enabled)
		idx++
	}
	args = append(args, id)
	q := `UPDATE threat_feeds SET ` + strings.Join(sets, ", ") + fmt.Sprintf(` WHERE id = $%d`, idx)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("update threat feed: %w", err)
	}
	return s.GetThreatFeed(ctx, id)
}

func (s *Store) DeleteThreatFeed(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM threat_feeds WHERE id = $1`, id)
	return err
}

func (s *Store) RecordThreatFeedRefresh(ctx context.Context, id uuid.UUID, status, errMsg string, count int) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var errArg any
	if errMsg != "" {
		errArg = errMsg
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE threat_feeds
		   SET last_status = $2, last_error = $3, last_indicator_count = $4,
		       last_refreshed_at = NOW(), updated_at = NOW()
		 WHERE id = $1
	`, id, status, errArg, count)
	return err
}

const threatFeedSelectSQL = `
	SELECT id, tenant_id, name, feed_type, url, api_key_sealed, nonce,
		score_floor, refresh_seconds, category, enabled,
		last_status, last_error, last_indicator_count, last_refreshed_at,
		created_at, updated_at
	FROM threat_feeds
`

func scanThreatFeed(sc scanner) (*ThreatFeed, error) {
	var f ThreatFeed
	if err := sc.Scan(
		&f.ID, &f.TenantID, &f.Name, &f.FeedType, &f.URL, &f.APIKeySealed, &f.Nonce,
		&f.ScoreFloor, &f.RefreshSeconds, &f.Category, &f.Enabled,
		&f.LastStatus, &f.LastError, &f.LastIndicatorCount, &f.LastRefreshedAt,
		&f.CreatedAt, &f.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}
