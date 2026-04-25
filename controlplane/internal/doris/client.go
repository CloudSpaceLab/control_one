// Package doris is the analytic storage backbone for Control One. Doris
// speaks the MySQL wire protocol so we connect via the standard go-sql-driver,
// but the schemas, write patterns, and tuning differ from a row store: bulk
// inserts go through Stream Load (HTTP), analytic queries use HLL/BITMAP for
// uniques, and partitions are managed per-day for fast TTL pruning.
//
// Today the package covers:
//
//   - Connection lifecycle (`Client`)
//   - DDL bootstrap for telemetry_logs, security_events, rule_trigger_log,
//     telemetry_metrics, and a unique-counter table backed by BITMAP.
//   - Bulk writer (`Writer`) that batches by table and flushes on size/time.
//   - Reader queries used by dashboards and recommendations.
//
// Postgres remains the source of truth for transactional data (tenants,
// nodes, policies, RBAC); Doris is the OLAP partner for events and metrics.
package doris

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Config controls how the client talks to a Doris frontend (FE).
type Config struct {
	// DSN is the MySQL DSN for the Doris query port (default 9030). Example:
	//   admin:secret@tcp(doris-fe:9030)/controlone?parseTime=true
	DSN string

	// HTTPEndpoint is the Doris FE HTTP base used for Stream Load. Example:
	//   http://doris-fe:8030
	HTTPEndpoint string

	// User + Password authenticate Stream Load HTTP requests. Mirror the
	// MySQL credentials in production.
	User     string
	Password string

	// Database is the Doris database (logical schema) we operate inside.
	Database string

	// MaxOpenConns / MaxIdleConns tune the SQL pool used for analytic reads.
	MaxOpenConns int
	MaxIdleConns int

	// QueryTimeout caps individual analytic queries.
	QueryTimeout time.Duration
}

// Client wraps a database/sql handle for analytic queries plus an HTTP client
// for Stream Load bulk writes.
type Client struct {
	cfg  Config
	db   *sql.DB
	http *http.Client
}

// New returns a connected Client. Caller owns Close().
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("doris dsn required")
	}
	if cfg.Database == "" {
		return nil, errors.New("doris database required")
	}
	if cfg.MaxOpenConns <= 0 {
		cfg.MaxOpenConns = 16
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 4
	}
	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = 30 * time.Second
	}
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open doris: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(15 * time.Minute)

	return &Client{
		cfg:  cfg,
		db:   db,
		http: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// Close releases connections.
func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Ping verifies connectivity. Use during startup.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

// DB exposes the underlying *sql.DB for ad-hoc queries. Prefer the typed
// helpers in this package; reach for DB() only when scaling out new readers.
func (c *Client) DB() *sql.DB { return c.db }

// HTTPEndpoint returns the configured FE HTTP base.
func (c *Client) HTTPEndpoint() string { return strings.TrimRight(c.cfg.HTTPEndpoint, "/") }

// Database returns the configured database name.
func (c *Client) Database() string { return c.cfg.Database }

// streamLoadURL composes the Stream Load endpoint for a given table.
func (c *Client) streamLoadURL(table string) string {
	if c.cfg.HTTPEndpoint == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/%s/%s/_stream_load",
		c.HTTPEndpoint(), url.PathEscape(c.cfg.Database), url.PathEscape(table))
}
