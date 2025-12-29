package provisioning

import (
	"context"
	"strings"
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

	// providerMeta holds metadata detected locally (e.g., region, subscription).
	// It is merged into ApplyTemplate metadata to enrich downstream requests.
	providerMeta map[string]string
}

func NewEngine(log *zap.Logger, client Client, opts Options) *Engine {
	detectedProvider := strings.TrimSpace(opts.Provider)
	providerMeta := map[string]string{}
	if detectedProvider == "" || detectedProvider == "auto" {
		if p, md := DetectProvider(); p != "" && p != "unknown" {
			detectedProvider = p
			providerMeta = md
			log.Info("provisioning provider auto-detected", zap.String("provider", p), zap.Any("metadata", md))
		} else {
			log.Warn("provisioning provider not specified; defaulting to generic adapter")
		}
	}
	opts.Provider = detectedProvider

	return &Engine{
		log:          log,
		opts:         opts,
		adapter:      newAdapter(opts.Provider, log, client),
		status:       make(map[string]Status),
		providerMeta: providerMeta,
	}
}

func (e *Engine) ApplyTemplate(ctx context.Context, nodeID string, metadata map[string]string) error {
	if e.opts.Template == "" {
		e.log.Debug("no provisioning template configured")
		return nil
	}

	mergedMeta := make(map[string]string, len(metadata)+len(e.providerMeta))
	for k, v := range e.providerMeta {
		mergedMeta[k] = v
	}
	for k, v := range metadata {
		mergedMeta[k] = v
	}

	e.setStatus(nodeID, StatusRunning)
	result, err := e.adapter.Apply(ctx, nodeID, e.opts, mergedMeta)
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
