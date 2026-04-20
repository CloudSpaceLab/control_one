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
	// Destroy tears down the cloud resource associated with nodeID. For http-
	// backed adapters this forwards to DELETE /nodes/:id; adapters that need
	// native SDK access override this method. Missing backend implementations
	// degrade to log-WARN + nil for backwards compatibility.
	Destroy(ctx context.Context, nodeID string) error
	// RegisterLB attaches nodeID to whatever load-balancer endpoint is
	// encoded in clusterMeta (e.g. lb_target_group_arn for AWS, lb_backend_pool_id
	// for Azure, lb_pool for VMware/NSX). clusterMeta is opaque to the
	// abstraction — each adapter cherry-picks keys it understands.
	RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error
	// DeregisterLB is the inverse of RegisterLB. Invoked during shrink and
	// cluster teardown drain loops before Destroy.
	DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error
}

// ApplyResult describes the outcome of a provisioning apply call.
type ApplyResult struct {
	OperationID string
}

// BaselineResult describes the outcome of a baseline run.
type BaselineResult struct {
	Notes string
}

// NewAdapter is the exported constructor callers (e.g., the clusters API) use
// to obtain a provisioning.Adapter for a given provider. It's a thin wrapper
// around the package-private newAdapter and exists so server code can build an
// adapter without going through the heavier Engine.
func NewAdapter(provider string, log *zap.Logger, client Client) Adapter {
	return newAdapter(provider, log, client)
}

func newAdapter(provider string, log *zap.Logger, client Client) Adapter {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	switch normalized {
	case "mock":
		return newMockAdapter(log)
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

// Destroy forwards to the provisioning service. If the service isn't wired up
// (no client) or returns 404/501, we log WARN and return nil so callers (shrink,
// teardown) can still drain cluster_members and label rows without a hard fail.
func (h *httpAdapter) Destroy(ctx context.Context, nodeID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("destroy: node id is required")
	}
	if h.client == nil {
		h.log.Warn("provisioning client unavailable; destroy is a no-op",
			zap.String("node_id", nodeID))
		return nil
	}
	resp, err := h.client.Do(ctx, http.MethodDelete, "/api/v1/provisioning/nodes/"+nodeID, nil)
	if err != nil {
		return fmt.Errorf("destroy request failed: %w", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		h.log.Warn("provisioning backend did not implement destroy; proceeding",
			zap.String("node_id", nodeID), zap.Int("status", resp.StatusCode))
		return nil
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("destroy rejected: status %d", resp.StatusCode)
	}
	return nil
}

// RegisterLB forwards to POST /api/v1/provisioning/lb/register. If the backend
// returns 404/501, we treat it as a no-op (log WARN). This keeps parity with
// Destroy semantics so the cluster join path doesn't fail on backends that
// haven't been upgraded yet.
func (h *httpAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return h.lbForward(ctx, "/api/v1/provisioning/lb/register", nodeID, clusterMeta)
}

// DeregisterLB forwards to POST /api/v1/provisioning/lb/deregister.
func (h *httpAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return h.lbForward(ctx, "/api/v1/provisioning/lb/deregister", nodeID, clusterMeta)
}

func (h *httpAdapter) lbForward(ctx context.Context, path, nodeID string, clusterMeta map[string]any) error {
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("lb: node id is required")
	}
	if h.client == nil {
		h.log.Warn("provisioning client unavailable; lb forward is a no-op",
			zap.String("node_id", nodeID), zap.String("path", path))
		return nil
	}
	payload := map[string]any{
		"node_id":      nodeID,
		"cluster_meta": clusterMeta,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal lb payload: %w", err)
	}
	resp, err := h.client.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("lb forward failed: %w", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		h.log.Warn("provisioning backend did not implement lb endpoint; proceeding",
			zap.String("node_id", nodeID), zap.String("path", path), zap.Int("status", resp.StatusCode))
		return nil
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("lb forward rejected: status %d", resp.StatusCode)
	}
	return nil
}

// MockAdapterCall captures a single call recorded against the mock adapter.
// Tests inspect the slice to assert the provisioner contract without spinning
// up a real backend.
type MockAdapterCall struct {
	Method      string
	NodeID      string
	Options     Options
	Metadata    map[string]string
	ClusterMeta map[string]any
}

type mockAdapter struct {
	log   *zap.Logger
	calls []MockAdapterCall
}

func newMockAdapter(log *zap.Logger) *mockAdapter {
	return &mockAdapter{log: log}
}

// Calls returns a copy of the recorded call list. Safe for test assertions.
func (m *mockAdapter) Calls() []MockAdapterCall {
	out := make([]MockAdapterCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// Reset clears the recorded calls — useful between test phases.
func (m *mockAdapter) Reset() {
	m.calls = nil
}

func (m *mockAdapter) Apply(_ context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	plan := metadata["plan_id"]
	if plan == "" {
		plan = opts.Template
	}
	m.log.Debug("mock provisioning apply", zap.String("node_id", nodeID), zap.String("plan", plan))
	m.calls = append(m.calls, MockAdapterCall{
		Method:   "Apply",
		NodeID:   nodeID,
		Options:  opts,
		Metadata: cloneStringMap(metadata),
	})
	return &ApplyResult{OperationID: fmt.Sprintf("mock-%s", nodeID)}, nil
}

func (m *mockAdapter) RunBaselines(_ context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	m.calls = append(m.calls, MockAdapterCall{
		Method:  "RunBaselines",
		NodeID:  nodeID,
		Options: opts,
	})
	if len(opts.Baselines) == 0 {
		return nil, nil
	}
	m.log.Debug("mock baselines", zap.String("node_id", nodeID), zap.Int("count", len(opts.Baselines)))
	return &BaselineResult{Notes: "mock remediation complete"}, nil
}

func (m *mockAdapter) Destroy(_ context.Context, nodeID string) error {
	m.log.Debug("mock destroy", zap.String("node_id", nodeID))
	m.calls = append(m.calls, MockAdapterCall{
		Method: "Destroy",
		NodeID: nodeID,
	})
	return nil
}

func (m *mockAdapter) RegisterLB(_ context.Context, nodeID string, clusterMeta map[string]any) error {
	m.log.Debug("mock register lb", zap.String("node_id", nodeID))
	m.calls = append(m.calls, MockAdapterCall{
		Method:      "RegisterLB",
		NodeID:      nodeID,
		ClusterMeta: cloneAnyMap(clusterMeta),
	})
	return nil
}

func (m *mockAdapter) DeregisterLB(_ context.Context, nodeID string, clusterMeta map[string]any) error {
	m.log.Debug("mock deregister lb", zap.String("node_id", nodeID))
	m.calls = append(m.calls, MockAdapterCall{
		Method:      "DeregisterLB",
		NodeID:      nodeID,
		ClusterMeta: cloneAnyMap(clusterMeta),
	})
	return nil
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

// Destroy for AWS delegates to the provisioning backend which maps nodeID to
// an EC2 instance id and issues TerminateInstances via aws-sdk-go-v2/service/ec2.
// SDK wiring is intentionally left to the provisioning service so adapters here
// stay thin and test-friendly. If the backend is missing, the underlying http
// adapter logs WARN + returns nil.
func (a *awsAdapter) Destroy(ctx context.Context, nodeID string) error {
	return a.httpAdapter.Destroy(ctx, nodeID)
}

// RegisterLB for AWS maps to ElasticLoadBalancingV2 RegisterTargets against the
// target group specified by clusterMeta["lb_target_group_arn"]. When no TG is
// configured, the call degrades to a no-op forward (the backend decides).
func (a *awsAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return a.httpAdapter.RegisterLB(ctx, nodeID, clusterMeta)
}

// DeregisterLB for AWS maps to ElasticLoadBalancingV2 DeregisterTargets.
func (a *awsAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return a.httpAdapter.DeregisterLB(ctx, nodeID, clusterMeta)
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

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
