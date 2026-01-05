package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"go.uber.org/zap"
)

// Client is the subset of the API client used by provisioning adapters.
type Client interface {
	Do(ctx context.Context, method, path string, body []byte) (*http.Response, error)
}

// Adapter coordinates provider-specific provisioning behavior.
type Adapter interface {
	Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error)
	RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error)
}

// ApplyResult describes the outcome of a provisioning apply call.
type ApplyResult struct {
	OperationID string
}

// BaselineResult describes the outcome of a baseline run.
type BaselineResult struct {
	Notes string
}

func newAdapter(provider string, log *zap.Logger, client Client) Adapter {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	switch normalized {
	case "mock":
		return &mockAdapter{log: log}
	case "aws":
		return &awsAdapter{httpAdapter: newHTTPAdapter(log, client)}
	case "azure":
		return &azureAdapter{httpAdapter: newHTTPAdapter(log, client)}
	case "vmware":
		return &vmwareAdapter{httpAdapter: newHTTPAdapter(log, client)}
	case "libvirt":
		return &libvirtAdapter{httpAdapter: newHTTPAdapter(log, client)}
	case "gcp":
		return newHTTPAdapter(log, client)
	default:
		return newHTTPAdapter(log, client)
	}
}

type httpAdapter struct {
	log    *zap.Logger
	client Client
}

func newHTTPAdapter(log *zap.Logger, client Client) Adapter {
	return &httpAdapter{log: log, client: client}
}

func (h *httpAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	if opts.Template == "" {
		return &ApplyResult{OperationID: "noop"}, nil
	}

	payload := map[string]any{
		"node_id":          nodeID,
		"template":         opts.Template,
		"provider":         opts.Provider,
		"metadata":         metadata,
		"auto_remediation": opts.AutoRemediation,
		"baselines":        opts.Baselines,
	}

	if h.client == nil {
		h.log.Info("provisioning client unavailable; simulating template apply", zap.String("node_id", nodeID))
		return &ApplyResult{OperationID: "local-simulated"}, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal provisioning payload: %w", err)
	}

	resp, err := h.client.Do(ctx, http.MethodPost, "/api/v1/provisioning/apply", body)
	if err != nil {
		return nil, fmt.Errorf("provisioning apply request failed: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("provisioning apply rejected: status %d", resp.StatusCode)
	}

	var result struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode provisioning response: %w", err)
	}
	if strings.TrimSpace(result.OperationID) == "" {
		result.OperationID = "pending"
	}
	return &ApplyResult{OperationID: result.OperationID}, nil
}

func (h *httpAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	if len(opts.Baselines) == 0 {
		return nil, nil
	}

	if h.client == nil {
		h.log.Info("baseline run skipped; provisioning client unavailable", zap.String("node_id", nodeID))
		return &BaselineResult{Notes: "simulated"}, nil
	}

	payload := map[string]any{
		"node_id":   nodeID,
		"baselines": opts.Baselines,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal baseline payload: %w", err)
	}

	resp, err := h.client.Do(ctx, http.MethodPost, "/api/v1/provisioning/baselines", body)
	if err != nil {
		return nil, fmt.Errorf("baseline run request failed: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("baseline run rejected: status %d", resp.StatusCode)
	}

	var result struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode baseline response: %w", err)
	}

	return &BaselineResult{Notes: result.Notes}, nil
}

type mockAdapter struct {
	log *zap.Logger
}

func (m *mockAdapter) Apply(_ context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	plan := metadata["plan_id"]
	if plan == "" {
		plan = opts.Template
	}
	m.log.Debug("mock provisioning apply", zap.String("node_id", nodeID), zap.String("plan", plan))
	return &ApplyResult{OperationID: fmt.Sprintf("mock-%s", nodeID)}, nil
}

func (m *mockAdapter) RunBaselines(_ context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	if len(opts.Baselines) == 0 {
		return nil, nil
	}
	m.log.Debug("mock baselines", zap.String("node_id", nodeID), zap.Int("count", len(opts.Baselines)))
	return &BaselineResult{Notes: "mock remediation complete"}, nil
}

type awsAdapter struct {
	httpAdapter Adapter
}

func (a *awsAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureAWSMetadata(metadata)
	return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
}

func (a *awsAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return a.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

func ensureAWSMetadata(metadata map[string]string) {
	if metadata == nil {
		return
	}
	if region := strings.TrimSpace(metadata["region"]); region == "" {
		if env := strings.TrimSpace(os.Getenv("AWS_REGION")); env != "" {
			metadata["region"] = env
		} else if env := strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION")); env != "" {
			metadata["region"] = env
		} else {
			metadata["region"] = "us-east-1"
		}
	}
}

func drainAndClose(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
