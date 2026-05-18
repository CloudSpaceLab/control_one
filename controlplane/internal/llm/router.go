package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type ProviderConfig struct {
	Provider  string
	Model     string
	BaseURL   string
	APIKey    string
	Fallbacks []ProviderConfig
}

type ProviderError struct {
	Provider   string
	StatusCode int
	Message    string
	Body       string
}

func (e ProviderError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s status %d: %s", e.Provider, e.StatusCode, msg)
	}
	return fmt.Sprintf("%s error: %s", e.Provider, msg)
}

func NewClient(cfg ProviderConfig) (Client, error) {
	primary, err := newProviderClient(cfg)
	if err != nil {
		return nil, err
	}
	if len(cfg.Fallbacks) == 0 {
		return primary, nil
	}
	clients := []Client{primary}
	for _, fallback := range cfg.Fallbacks {
		client, err := newProviderClient(fallback)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return &FallbackClient{Clients: clients}, nil
}

func newProviderClient(cfg ProviderConfig) (Client, error) {
	switch normalizeProvider(cfg.Provider) {
	case "anthropic":
		return &AnthropicClient{Config: cfg, HTTPClient: &http.Client{Timeout: 60 * time.Second}}, nil
	case "openai":
		return &OpenAIClient{Config: cfg, HTTPClient: &http.Client{Timeout: 60 * time.Second}}, nil
	case "google":
		return &GeminiClient{Config: cfg, HTTPClient: &http.Client{Timeout: 60 * time.Second}}, nil
	default:
		return nil, ErrUnsupportedProvider
	}
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "anthropic", "claude":
		return "anthropic"
	case "openai", "gpt":
		return "openai"
	case "google", "gemini":
		return "google"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

type FallbackClient struct {
	Clients []Client
}

func (c *FallbackClient) Generate(ctx context.Context, req Request) (Response, error) {
	if c == nil || len(c.Clients) == 0 {
		return Response{}, ErrUnsupportedProvider
	}
	var lastErr error
	for i, client := range c.Clients {
		resp, err := client.Generate(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i == len(c.Clients)-1 || !IsRetryableError(err) {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var providerErr ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode == http.StatusTooManyRequests || providerErr.StatusCode >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
