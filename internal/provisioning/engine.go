package provisioning

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

type Options struct {
	Template        string
	Provider        string
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
	log     *zap.Logger
	opts    Options
	adapter Adapter
	mu      sync.RWMutex
	status  map[string]Status
}

func NewEngine(log *zap.Logger, client Client, opts Options) *Engine {
	return &Engine{
		log:     log,
		opts:    opts,
		adapter: newAdapter(opts.Provider, log, client),
		status:  make(map[string]Status),
	}
}

func (e *Engine) ApplyTemplate(ctx context.Context, nodeID string, metadata map[string]string) error {
	if e.opts.Template == "" {
		e.log.Debug("no provisioning template configured")
		return nil
	}

	e.setStatus(nodeID, StatusRunning)
	result, err := e.adapter.Apply(ctx, nodeID, e.opts, metadata)
	if err != nil {
		e.setStatus(nodeID, StatusFailed)
		return err
	}

	if planID := metadata["plan_id"]; planID != "" {
		e.log.Info("provisioning template applied", zap.String("node_id", nodeID), zap.String("plan_id", planID), zap.String("operation_id", result.OperationID))
	} else {
		e.log.Info("provisioning template applied", zap.String("node_id", nodeID), zap.String("operation_id", result.OperationID))
	}
	e.setStatus(nodeID, StatusSuccess)
	return nil
}

func (e *Engine) RunBaselines(ctx context.Context, nodeID string) error {
	if len(e.opts.Baselines) == 0 {
		return nil
	}

	result, err := e.adapter.RunBaselines(ctx, nodeID, e.opts)
	if err != nil {
		return err
	}

	if e.opts.AutoRemediation && result != nil && result.Notes != "" {
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
