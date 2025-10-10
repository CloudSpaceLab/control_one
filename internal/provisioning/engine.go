package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type Options struct {
	Template        string
	Baselines       []string
	AutoRemediation bool
}

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

type Engine struct {
	log    *zap.Logger
	client *api.Client
	opts   Options
	mu     sync.RWMutex
	status map[string]Status
}

func NewEngine(log *zap.Logger, client *api.Client, opts Options) *Engine {
	return &Engine{
		log:    log,
		client: client,
		opts:   opts,
		status: make(map[string]Status),
	}
}

func (e *Engine) ApplyTemplate(ctx context.Context, nodeID string, metadata map[string]string) error {
	if e.opts.Template == "" {
		e.log.Debug("no provisioning template configured")
		return nil
	}

	e.setStatus(nodeID, StatusRunning)

	payload := map[string]any{
		"node_id":          nodeID,
		"template":         e.opts.Template,
		"metadata":         metadata,
		"auto_remediation": e.opts.AutoRemediation,
		"baselines":        e.opts.Baselines,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		e.setStatus(nodeID, StatusFailed)
		return fmt.Errorf("marshal provisioning payload: %w", err)
	}

	if e.client == nil {
		e.log.Info("provisioning client unavailable; marking template as applied locally", zap.String("node_id", nodeID))
		e.setStatus(nodeID, StatusSuccess)
		return nil
	}

	resp, err := e.client.Do(ctx, "POST", "/api/v1/provisioning/apply", body)
	if err != nil {
		e.setStatus(nodeID, StatusFailed)
		return fmt.Errorf("provisioning apply request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		e.setStatus(nodeID, StatusFailed)
		return fmt.Errorf("provisioning apply rejected: status %d", resp.StatusCode)
	}

	var result struct {
		Status      string `json:"status"`
		OperationID string `json:"operation_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		e.setStatus(nodeID, StatusFailed)
		return fmt.Errorf("decode provisioning response: %w", err)
	}

	e.log.Info("provisioning template applied", zap.String("node_id", nodeID), zap.String("operation_id", result.OperationID))
	e.setStatus(nodeID, StatusSuccess)
	return nil
}

func (e *Engine) RunBaselines(ctx context.Context, nodeID string) error {
	if len(e.opts.Baselines) == 0 {
		return nil
	}

	if e.client == nil {
		e.log.Info("baseline run skipped; provisioning client unavailable", zap.String("node_id", nodeID))
		return nil
	}

	payload := map[string]any{
		"node_id":   nodeID,
		"baselines": e.opts.Baselines,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal baseline payload: %w", err)
	}

	resp, err := e.client.Do(ctx, "POST", "/api/v1/provisioning/baselines", body)
	if err != nil {
		return fmt.Errorf("baseline run request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("baseline run rejected: status %d", resp.StatusCode)
	}

	var result struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode baseline response: %w", err)
	}

	if e.opts.AutoRemediation && result.Notes != "" {
		e.log.Debug("baseline remediation details", zap.String("node_id", nodeID), zap.String("notes", result.Notes))
	}
	return nil
}

func (e *Engine) Status(nodeID string) Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if s, ok := e.status[nodeID]; ok {
		return s
	}
	return StatusPending
}

func (e *Engine) setStatus(nodeID string, s Status) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status[nodeID] = s
}
