package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
)

// IPIntelCache implements ipintel.Cache against Postgres.
type IPIntelCache struct {
	db *sql.DB
}

// NewIPIntelCache wraps the storage handle.
func NewIPIntelCache(db *sql.DB) *IPIntelCache { return &IPIntelCache{db: db} }

// Get returns a cached Enrichment if one exists and has not expired.
func (c *IPIntelCache) Get(ctx context.Context, ip string) (*ipintel.Enrichment, bool, error) {
	if c == nil || c.db == nil {
		return nil, false, nil
	}
	var (
		payload []byte
		expires time.Time
	)
	err := c.db.QueryRowContext(ctx, `
		SELECT payload, expires_at
		FROM ip_enrichment_cache
		WHERE addr = $1 AND expires_at > NOW()
	`, ip).Scan(&payload, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var out ipintel.Enrichment
	if err := json.Unmarshal(payload, &out); err != nil {
		// Treat malformed cache as a miss; the caller will refresh.
		return nil, false, nil
	}
	return &out, true, nil
}

// Put inserts (or replaces) a cache entry with a TTL.
func (c *IPIntelCache) Put(ctx context.Context, ip string, e *ipintel.Enrichment, ttl time.Duration) error {
	if c == nil || c.db == nil || e == nil || ttl <= 0 {
		return nil
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO ip_enrichment_cache (addr, payload, source, fetched_at, expires_at)
		VALUES ($1, $2, $3, NOW(), NOW() + $4::interval)
		ON CONFLICT (addr) DO UPDATE SET
			payload = EXCLUDED.payload,
			source  = EXCLUDED.source,
			fetched_at = NOW(),
			expires_at = EXCLUDED.expires_at
	`, ip, payload, e.Source, ttl.String())
	return err
}

// Sweep removes expired rows. Call periodically from a background tick.
func (c *IPIntelCache) Sweep(ctx context.Context) error {
	if c == nil || c.db == nil {
		return nil
	}
	_, err := c.db.ExecContext(ctx, `DELETE FROM ip_enrichment_cache WHERE expires_at <= NOW()`)
	return err
}
