// Package sanctions wraps the outbound Moov Watchman / OFAC sanctions screening
// API. It is the in-tree replacement for the legacy hardcoded URL
//
//	http://178.79.176.19/moov-watchman-aml
//
// which used to live in upstream connectors and pinned to a plaintext IP. That
// pattern is unacceptable for a regulated-industry product: sanctions queries
// carry KYC PII (full name, BVN/NIN, DOB, address) and the response decides
// whether a customer is allowed onto the system. Plain HTTP between
// controlplane and screening provider is a P0 compliance failure (bugs §4 #2).
//
// Design rules baked into this package:
//
//  1. The base URL is **never** a compile-time constant. Callers must supply a
//     [Config] whose BaseURL is sourced from per-tenant configuration. Different
//     tenants can therefore route to different screening providers (Moov
//     Watchman, OFAC SDN direct, a regional AML aggregator, etc.).
//  2. The base URL **must** be HTTPS. NewClient rejects non-HTTPS schemes and
//     bare IP literals unless the tenant config explicitly opts in to insecure
//     transport via [Config.AllowInsecure]. Even then, the constructor logs a
//     P0-severity warning naming the tenant.
//  3. TLS verification is on by default. [Config.InsecureSkipVerify] can flip
//     it off — again with a logged warning — but never by default and never
//     without an explicit tenant flag.
//  4. The default screening provider is Moov Watchman. The path-shape of the
//     legacy URL (`/moov-watchman-aml`) is preserved so the screening route
//     can land later without reshaping client wiring.
package sanctions

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Config captures the per-tenant routing for outbound sanctions queries. It is
// populated by handlers from the tenant's screening config row before
// constructing a [Client].
//
// The zero value is intentionally invalid (BaseURL empty) so the constructor
// fails loud rather than silently defaulting to a global.
type Config struct {
	// TenantID identifies the tenant for log lines and error messages. Not
	// used as a transport parameter — the URL is the routing key.
	TenantID string

	// BaseURL is the absolute base URL for the screening provider. Must be a
	// fully-qualified `https://host[:port][/path]` URL. Trailing slash is
	// optional. The legacy plaintext IP form is rejected unconditionally
	// unless AllowInsecure is set; even then a warning fires.
	BaseURL string

	// AllowInsecure must be set to true for the tenant before NewClient will
	// accept a `http://` BaseURL. Off by default. Production tenants must
	// never have this set.
	AllowInsecure bool

	// InsecureSkipVerify disables TLS chain verification on the outbound
	// HTTPS request. Off by default. Should only be flipped on for
	// development/staging where the screening provider exposes a
	// self-signed cert; flipping it on always logs a warning.
	InsecureSkipVerify bool

	// Timeout caps the total outbound request time. Zero falls back to
	// DefaultTimeout. Sanctions APIs can be slow on first call (cold cache
	// of the OFAC list) so DefaultTimeout is generous.
	Timeout time.Duration
}

// DefaultTimeout is the fallback total-request timeout when [Config.Timeout]
// is zero. Sized for cold-cache OFAC list lookups.
const DefaultTimeout = 15 * time.Second

// ErrInsecureURL is returned when [Config.BaseURL] is not HTTPS and the tenant
// has not opted in via [Config.AllowInsecure]. The error message names the
// scheme it saw so operator dashboards can distinguish "missing config" from
// "config present but plaintext".
var ErrInsecureURL = errors.New("sanctions: BaseURL must use https:// (set AllowInsecure on the tenant config to override)")

// ErrIPLiteralURL is returned when [Config.BaseURL] points at a bare IP
// literal. The legacy production deployment used `http://178.79.176.19/...`;
// rejecting raw IPs ensures that whatever URL a tenant configures is at least
// going through DNS so cert chains can be validated.
var ErrIPLiteralURL = errors.New("sanctions: BaseURL must use a hostname, not an IP literal (set AllowInsecure to override)")

// ErrEmptyBaseURL is returned when BaseURL is whitespace. Distinct from
// ErrInsecureURL so handlers can render "tenant has not configured a sanctions
// provider yet" rather than "tenant misconfigured a sanctions provider".
var ErrEmptyBaseURL = errors.New("sanctions: BaseURL is required (configure a screening provider for this tenant)")

// Client is the outbound sanctions screening client for one tenant.
//
// One Client per tenant per process is fine — the embedded *http.Client owns
// connection pooling. Callers may freely store a Client on a server struct
// keyed by tenant ID.
type Client struct {
	cfg     Config
	baseURL *url.URL
	httpc   *http.Client
	logger  *zap.Logger
}

// NewClient builds a screening client from the tenant config. It validates the
// URL up front and refuses to construct an insecure client unless the tenant
// has opted in. Logger may be nil; a no-op logger is substituted.
//
// Validation order:
//  1. BaseURL non-empty                       → ErrEmptyBaseURL
//  2. BaseURL parses as absolute URL          → wrapped parse error
//  3. Scheme is https                         → ErrInsecureURL (unless AllowInsecure)
//  4. Host is a DNS name, not an IP literal   → ErrIPLiteralURL (unless AllowInsecure)
//
// All "AllowInsecure overrides" emit a warn log naming the tenant.
func NewClient(cfg Config, logger *zap.Logger) (*Client, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	raw := strings.TrimSpace(cfg.BaseURL)
	if raw == "" {
		return nil, ErrEmptyBaseURL
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("sanctions: parse BaseURL: %w", err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return nil, fmt.Errorf("sanctions: BaseURL must be absolute (scheme://host/...): %q", raw)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		// good
	case "http":
		if !cfg.AllowInsecure {
			return nil, ErrInsecureURL
		}
		logger.Warn("sanctions: tenant configured plaintext HTTP screening URL; PII transits unencrypted",
			zap.String("tenant_id", cfg.TenantID),
			zap.String("base_url", parsed.Redacted()),
		)
	default:
		return nil, fmt.Errorf("sanctions: BaseURL scheme must be http or https, got %q", parsed.Scheme)
	}

	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !cfg.AllowInsecure {
			return nil, ErrIPLiteralURL
		}
		logger.Warn("sanctions: tenant configured IP-literal screening URL; cert pinning impossible",
			zap.String("tenant_id", cfg.TenantID),
			zap.String("host", host),
		)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if cfg.InsecureSkipVerify {
		// Only honoured when the tenant has opted in. Logging at warn so
		// operators see this in audit trails. Note: this still goes over
		// HTTPS — the only thing skipped is certificate validation.
		tlsCfg.InsecureSkipVerify = true
		logger.Warn("sanctions: tenant disabled TLS verification for screening provider",
			zap.String("tenant_id", cfg.TenantID),
			zap.String("base_url", parsed.Redacted()),
		)
	}

	httpc := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:       tlsCfg,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}

	return &Client{
		cfg:     cfg,
		baseURL: parsed,
		httpc:   httpc,
		logger:  logger,
	}, nil
}

// BaseURL returns the validated base URL the client is bound to. Useful for
// logging and for handlers that want to surface the configured provider in an
// admin response.
func (c *Client) BaseURL() *url.URL {
	if c == nil || c.baseURL == nil {
		return nil
	}
	cp := *c.baseURL
	return &cp
}

// TenantID returns the tenant this client was constructed for. Convenience for
// log-context wiring.
func (c *Client) TenantID() string {
	if c == nil {
		return ""
	}
	return c.cfg.TenantID
}

// HTTPClient exposes the underlying *http.Client. Screening request methods
// (search, scan, etc.) will be added by follow-up Sprint 4 worktrees
// (`c1-aml-auth-fix`, `c1-sanctions-dob-refuse`); the constructor is the
// security-critical surface and is what this package locks down today.
func (c *Client) HTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.httpc
}

// resolve returns baseURL joined with the supplied path. Returns the cloned
// base URL when path is empty. Used by follow-up screening methods.
func (c *Client) resolve(path string) (*url.URL, error) {
	if c == nil || c.baseURL == nil {
		return nil, errors.New("sanctions: client not initialised")
	}
	if path == "" {
		out := *c.baseURL
		return &out, nil
	}
	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("sanctions: parse path %q: %w", path, err)
	}
	return c.baseURL.ResolveReference(rel), nil
}

// newRequest builds a Request with the Accept header set, ready for a follow-up
// screening method to attach query params or a body. Exported for forward
// compatibility — initial Sprint-4 row only ships the constructor + URL-from-
// config plumbing, but the resolve+request seam is in place so the next row
// (`c1-sanctions-dob-refuse`) can land without a refactor.
func (c *Client) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	u, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("sanctions: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "control_one-sanctions/1.0")
	return req, nil
}
