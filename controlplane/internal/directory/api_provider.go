package directory

import (
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

// APIProvider provides access to external directory services via REST API.
type APIProvider struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	log        *zap.Logger
}

// Config configures the API provider.
type Config struct {
	Endpoint   string
	APIKey     string
	Timeout    time.Duration
	SkipVerify bool
}

// NewAPIProvider creates a new API directory provider.
func NewAPIProvider(log *zap.Logger, cfg Config) (*APIProvider, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("api endpoint is required")
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	endpoint = strings.TrimSuffix(endpoint, "/")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.SkipVerify,
		},
	}

	return &APIProvider{
		baseURL: endpoint,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		apiKey: strings.TrimSpace(cfg.APIKey),
		log:    log,
	}, nil
}

// GetUsers retrieves users from the external directory service.
func (p *APIProvider) GetUsers(ctx context.Context) ([]User, error) {
	url := fmt.Sprintf("%s/users", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Users []User `json:"users"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}

	return result.Users, nil
}

// GetGroups retrieves groups from the external directory service.
func (p *APIProvider) GetGroups(ctx context.Context) ([]Group, error) {
	url := fmt.Sprintf("%s/groups", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Groups []Group `json:"groups"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}

	return result.Groups, nil
}

// GetUserGroups retrieves groups for a specific user.
func (p *APIProvider) GetUserGroups(ctx context.Context, userID string) ([]Group, error) {
	url := fmt.Sprintf("%s/users/%s/groups", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api request failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Groups []Group `json:"groups"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}

	return result.Groups, nil
}

func (p *APIProvider) setHeaders(req *http.Request) {
	if p.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// User represents a user from the directory service.
type User struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Groups      []string `json:"groups,omitempty"`
	Role        string   `json:"role,omitempty"`
}

// Group represents a group from the directory service.
type Group struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members,omitempty"`
}
