package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"go.uber.org/zap"
)

// capturingClient records every Do invocation so tests can assert path + body
// without spinning up an HTTP server.
type capturingClient struct {
	calls []capturedCall
	resp  *http.Response
}

type capturedCall struct {
	Method string
	Path   string
	Body   []byte
}

func (c *capturingClient) Do(_ context.Context, method, path string, body []byte) (*http.Response, error) {
	c.calls = append(c.calls, capturedCall{Method: method, Path: path, Body: append([]byte(nil), body...)})
	if c.resp != nil {
		return c.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
	}, nil
}

func TestAWSAdapterDestroyForwardsToHTTP(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &awsAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	if err := a.Destroy(context.Background(), "node-ec2"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", len(client.calls))
	}
	call := client.calls[0]
	if call.Method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", call.Method)
	}
	if call.Path != "/api/v1/provisioning/nodes/node-ec2" {
		t.Fatalf("unexpected path: %s", call.Path)
	}
}

func TestAWSAdapterRegisterLBForwardsTargetGroupARN(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &awsAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	meta := map[string]any{"lb_target_group_arn": "arn:aws:elasticloadbalancing:us-east-1:000000000000:targetgroup/foo/abcd"}
	if err := a.RegisterLB(context.Background(), "node-1", meta); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := a.DeregisterLB(context.Background(), "node-1", meta); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(client.calls))
	}
	if client.calls[0].Path != "/api/v1/provisioning/lb/register" {
		t.Fatalf("unexpected register path: %s", client.calls[0].Path)
	}
	if client.calls[1].Path != "/api/v1/provisioning/lb/deregister" {
		t.Fatalf("unexpected deregister path: %s", client.calls[1].Path)
	}

	var payload struct {
		NodeID      string         `json:"node_id"`
		ClusterMeta map[string]any `json:"cluster_meta"`
	}
	if err := json.Unmarshal(client.calls[0].Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.NodeID != "node-1" {
		t.Fatalf("expected node_id propagated, got %q", payload.NodeID)
	}
	if payload.ClusterMeta["lb_target_group_arn"] != meta["lb_target_group_arn"] {
		t.Fatalf("expected target group ARN propagated, got %+v", payload.ClusterMeta)
	}
}

// TestAWSAdapterDestroy501DegradesToNil verifies the WARN+nil back-compat
// contract: if the backend returns 501/404, cluster teardown proceeds.
func TestAWSAdapterDestroy501DegradesToNil(t *testing.T) {
	t.Parallel()
	client := &capturingClient{
		resp: &http.Response{
			StatusCode: http.StatusNotImplemented,
			Body:       io.NopCloser(bytes.NewReader(nil)),
		},
	}
	a := &awsAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	if err := a.Destroy(context.Background(), "node-x"); err != nil {
		t.Fatalf("expected graceful nil on 501, got %v", err)
	}
}
