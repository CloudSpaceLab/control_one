package provisioning

import (
	"context"
	"testing"

	"go.uber.org/zap/zaptest"
)

type stubAdapter struct {
	applyCalled bool
	runCalled   bool
	metadata    map[string]string
}

func (s *stubAdapter) Apply(_ context.Context, _ string, _ Options, metadata map[string]string) (*ApplyResult, error) {
	s.applyCalled = true
	s.metadata = metadata
	return &ApplyResult{OperationID: "stub-op"}, nil
}

func (s *stubAdapter) RunBaselines(_ context.Context, _ string, _ Options) (*BaselineResult, error) {
	s.runCalled = true
	return &BaselineResult{Notes: "stub"}, nil
}

func TestEngineApplyTemplateMergesMetadata(t *testing.T) {
	log := zaptest.NewLogger(t)
	engine := NewEngine(log, nil, Options{Template: "demo", Provider: "mock"})
	capture := &stubAdapter{}
	engine.adapter = capture
	engine.providerMeta = map[string]string{"detected": "true"}

	metadata := map[string]string{"caller": "api"}
	if err := engine.ApplyTemplate(context.Background(), "node-1", metadata); err != nil {
		t.Fatalf("apply template failed: %v", err)
	}

	if !capture.applyCalled {
		t.Fatalf("expected adapter apply to be called")
	}
	if capture.metadata["detected"] != "true" {
		t.Fatalf("expected provider metadata merged, got %v", capture.metadata)
	}
	if capture.metadata["caller"] != "api" {
		t.Fatalf("expected caller metadata merged, got %v", capture.metadata)
	}
	if got := engine.Status("node-1"); got != StatusSuccess {
		t.Fatalf("expected status success, got %s", got)
	}
}

func TestEngineRunBaselinesDelegatesToAdapter(t *testing.T) {
	log := zaptest.NewLogger(t)
	engine := NewEngine(log, nil, Options{Baselines: []string{"cis"}, Provider: "mock"})
	capture := &stubAdapter{}
	engine.adapter = capture

	if err := engine.RunBaselines(context.Background(), "node-1"); err != nil {
		t.Fatalf("run baselines failed: %v", err)
	}
	if !capture.runCalled {
		t.Fatalf("expected adapter RunBaselines to be called")
	}
}

func TestAWSAdapterEnsuresRegionMetadata(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	capture := &stubAdapter{}
	adapter := &awsAdapter{httpAdapter: capture}

	metadata := map[string]string{}
	if _, err := adapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, metadata); err != nil {
		t.Fatalf("aws adapter apply failed: %v", err)
	}

	if capture.metadata["region"] != "us-west-2" {
		t.Fatalf("expected aws region metadata, got %v", capture.metadata)
	}
}
