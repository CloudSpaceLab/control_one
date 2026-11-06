// Package executionauthority implements the explicit, deployment-configured
// current-authority contract. OIDC discovery and a valid JWT are not this contract.
package executionauthority

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

type Config struct {
	Enabled        bool   `mapstructure:"enabled"`
	Endpoint       string `mapstructure:"endpoint"`
	CredentialFile string `mapstructure:"credential_file"`
	CAFile         string `mapstructure:"ca_file"`
}

type Request struct {
	Version   int    `json:"version"`
	Nonce     string `json:"nonce"`
	Authority string `json:"authority"`
	Subject   string `json:"subject"`
	UserID    string `json:"user_id"`
	TenantID  string `json:"tenant_id"`
	Boundary  string `json:"boundary"`
	Purpose   string `json:"purpose"`
	Reference string `json:"reference"`
}
type Decision struct {
	Request
	DecisionID string    `json:"decision_id"`
	Active     bool      `json:"active"`
	Allowed    bool      `json:"allowed"`
	Roles      []string  `json:"roles"`
	ObservedAt time.Time `json:"observed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type Client struct {
	endpoint, credential, authority string
	http                            *http.Client
}

func New(cfg Config, issuer string) (*Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || issuer == "" || strings.ContainsAny(issuer, "\r\n") {
		return nil, errors.New("execution authority requires an HTTPS endpoint and the configured OIDC issuer")
	}
	secret, err := os.ReadFile(cfg.CredentialFile)
	if err != nil {
		return nil, errors.New("execution authority credential file is unreadable")
	}
	credential := strings.TrimSpace(string(secret))
	if len(credential) < 32 || len(credential) > 4096 || strings.ContainsAny(credential, "\r\n") {
		return nil, errors.New("invalid execution authority service credential")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, errors.New("execution authority CA file is unreadable")
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("execution authority CA file has no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	return &Client{endpoint: u.String(), credential: credential, authority: "oidc:" + issuer, http: &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig, Proxy: nil, MaxIdleConnsPerHost: 2, ResponseHeaderTimeout: 2 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("execution authority redirects are forbidden")
		},
	}}, nil
}

// Check never reuses a decision. The nonce and all execution coordinates must
// match, and provider roles only restrict the caller's separately checked grants.
func (c *Client) Check(ctx context.Context, request Request) (Decision, error) {
	var denied Decision
	if c == nil || request.Authority != c.authority || request.Subject == "" || len(request.Subject) > 512 || request.UserID == "" || request.TenantID == "" || request.Reference == "" || len(request.Reference) > 512 || !slices.Contains([]string{"approval", "schedule", "dispatch", "start"}, request.Boundary) || !slices.Contains([]string{"organization_owner", "organization_approver", "individual_approval", "tenant_policy"}, request.Purpose) {
		return denied, errors.New("current provider execution authority is unavailable")
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return denied, err
	}
	request.Version = 1
	request.Nonce = hex.EncodeToString(nonce)
	body, err := json.Marshal(request)
	if err != nil {
		return denied, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return denied, err
	}
	r.Header.Set("Authorization", "Bearer "+c.credential)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	response, err := c.http.Do(r)
	if err != nil {
		return denied, errors.New("current provider execution authority could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return denied, errors.New("current provider execution authority refused the check")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return denied, errors.New("invalid execution authority response")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decision Decision
	if decoder.Decode(&decision) != nil || decoder.Decode(new(any)) != io.EOF || decision.Request != request || decision.DecisionID == "" || len(decision.DecisionID) > 256 {
		return denied, errors.New("execution authority response binding mismatch")
	}
	now := time.Now().UTC()
	if decision.ObservedAt.Before(now.Add(-5*time.Second)) || decision.ObservedAt.After(now.Add(time.Second)) || !decision.ExpiresAt.After(now) || !decision.ExpiresAt.After(decision.ObservedAt) || decision.ExpiresAt.After(now.Add(10*time.Second)) || decision.ExpiresAt.After(decision.ObservedAt.Add(10*time.Second)) {
		return denied, errors.New("execution authority response is stale or has an invalid lifetime")
	}
	allowedRoles := []string{"admin"}
	if request.Purpose == "organization_approver" {
		allowedRoles = append(allowedRoles, "ciso")
	}
	if request.Purpose == "individual_approval" {
		allowedRoles = append(allowedRoles, "operator")
	}
	if !decision.Active || !decision.Allowed {
		return decision, errors.New("current provider execution authority is revoked or denied")
	}
	for _, role := range allowedRoles {
		if slices.Contains(decision.Roles, role) {
			return decision, nil
		}
	}
	return decision, errors.New("current provider role does not authorize this execution purpose")
}
