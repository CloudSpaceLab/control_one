package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client wraps http.Client with mTLS configuration and helper methods.
type Client struct {
	baseURL string
	http    *http.Client
	token   string
}

// NewClient constructs an mTLS-enabled API client. If certFile/keyFile are empty, TLS client auth is skipped.
func NewClient(baseURL, certFile, keyFile, caFile, token string) (*Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load x509 key pair: %w", err)
		}
		transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}

	if caFile != "" {
		caf, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM(caf); !ok {
			return nil, fmt.Errorf("append ca cert failed")
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Transport: transport,
			Timeout:   45 * time.Second,
		},
		token: token,
	}, nil
}

// Do sends a JSON HTTP request with optional body bytes.
func (c *Client) Do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	return c.http.Do(req)
}
