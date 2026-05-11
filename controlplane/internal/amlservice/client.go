package amlservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultTimeout = 10 * time.Second

var (
	ErrEmptyBaseURL = errors.New("amlservice: base_url is required")
	ErrInsecureURL  = errors.New("amlservice: base_url must use https:// unless allow_insecure is set")
	ErrIPLiteral    = errors.New("amlservice: base_url must use a hostname unless allow_insecure is set")
)

type Config struct {
	BaseURL            string
	APIKey             string
	Timeout            time.Duration
	AllowInsecure      bool
	InsecureSkipVerify bool
}

type Client struct {
	baseURL *url.URL
	apiKey  string
	httpc   *http.Client
}

type ScreeningRequest struct {
	Name                string `json:"name"`
	AccountNo           string `json:"account_no,omitempty"`
	Country             string `json:"country,omitempty"`
	BirthDate           string `json:"birth_date,omitempty"`
	IDNumber            string `json:"id_number,omitempty"`
	EntityType          string `json:"entity_type,omitempty"`
	IncludePEP          bool   `json:"include_pep"`
	IncludeSanctions    bool   `json:"include_sanctions"`
	IncludeAdverseMedia bool   `json:"include_adverse_media"`
	IncludeRegistry     bool   `json:"include_registry"`
}

type ScreeningResult struct {
	RequestID    string          `json:"request_id"`
	Timestamp    *time.Time      `json:"timestamp,omitempty"`
	Duration     string          `json:"duration,omitempty"`
	Sanctions    json.RawMessage `json:"sanctions,omitempty"`
	PEP          json.RawMessage `json:"pep,omitempty"`
	AdverseMedia json.RawMessage `json:"adverse_media,omitempty"`
	Registry     json.RawMessage `json:"registry,omitempty"`
	OverallRisk  float64         `json:"overall_risk"`
	RiskLevel    string          `json:"risk_level"`
}

func NewClient(cfg Config) (*Client, error) {
	raw := strings.TrimSpace(cfg.BaseURL)
	if raw == "" {
		return nil, ErrEmptyBaseURL
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("amlservice: parse base_url: %w", err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return nil, fmt.Errorf("amlservice: base_url must be absolute: %q", raw)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !cfg.AllowInsecure {
			return nil, ErrInsecureURL
		}
	default:
		return nil, fmt.Errorf("amlservice: base_url scheme must be http or https, got %q", parsed.Scheme)
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !cfg.AllowInsecure {
		return nil, ErrIPLiteral
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Client{
		baseURL: parsed,
		apiKey:  cfg.APIKey,
		httpc: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: cfg.InsecureSkipVerify,
				},
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          16,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: timeout,
			},
		},
	}, nil
}

func (c *Client) Screen(ctx context.Context, req ScreeningRequest) (*ScreeningResult, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("amlservice: encode screen request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolve("/api/v1/screen"), &body)
	if err != nil {
		return nil, fmt.Errorf("amlservice: build screen request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.apiKey) != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("amlservice: screen request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("amlservice: screen returned %d", resp.StatusCode)
	}

	var out ScreeningResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("amlservice: decode screen response: %w", err)
	}
	return &out, nil
}

func (c *Client) resolve(path string) string {
	u := *c.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	relPath := strings.TrimLeft(path, "/")
	u.Path = basePath + "/" + relPath
	return u.String()
}
