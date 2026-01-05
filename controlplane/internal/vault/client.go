package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Client provides access to HashiCorp Vault.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	log        *zap.Logger
}

// Config configures the Vault client.
type Config struct {
	Address    string
	Token      string
	Timeout    time.Duration
	SkipVerify bool
	Namespace  string
}

// NewClient creates a new Vault client.
func NewClient(log *zap.Logger, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, fmt.Errorf("vault address is required")
	}

	address := strings.TrimSpace(cfg.Address)
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "https://" + address
	}
	address = strings.TrimSuffix(address, "/")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.SkipVerify,
		},
	}

	return &Client{
		baseURL: address,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		token:     strings.TrimSpace(cfg.Token),
		log:       log,
	}, nil
}

// ReadSecret reads a secret from Vault at the given path.
func (c *Client) ReadSecret(ctx context.Context, path string) (map[string]interface{}, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("secret path is required")
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, strings.TrimPrefix(path, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode vault response: %w", err)
	}

	return result.Data.Data, nil
}

// ListSecrets lists secrets at the given path.
func (c *Client) ListSecrets(ctx context.Context, path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("secret path is required")
	}

	url := fmt.Sprintf("%s/v1/%s?list=true", c.baseURL, strings.TrimPrefix(path, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode vault response: %w", err)
	}

	return result.Data.Keys, nil
}

// WriteSecret writes a secret to Vault at the given path.
func (c *Client) WriteSecret(ctx context.Context, path string, data map[string]interface{}) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("secret path is required")
	}

	payload := map[string]interface{}{
		"data": data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, strings.TrimPrefix(path, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetSecretVersion retrieves the version metadata for a secret (KV v2).
func (c *Client) GetSecretVersion(ctx context.Context, path string) (int, error) {
	if strings.TrimSpace(path) == "" {
		return 0, fmt.Errorf("secret path is required")
	}

	url := fmt.Sprintf("%s/v1/%s?version=0", c.baseURL, strings.TrimPrefix(path, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("vault request failed: status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode vault response: %w", err)
	}

	return result.Data.Metadata.Version, nil
}

func (c *Client) setHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("X-Vault-Token", c.token)
	}
	req.Header.Set("X-Vault-Request-ID", fmt.Sprintf("%d", time.Now().UnixNano()))
}

// Health checks Vault health status.
func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/sys/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("vault is unhealthy: status %d", resp.StatusCode)
	}

	return nil
}


