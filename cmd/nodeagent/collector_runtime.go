package main

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

type collectorRuntime struct {
	ctx context.Context
	log *zap.Logger

	mu         sync.Mutex
	collectors map[string]*collectorHandle
}

type collectorHandle struct {
	name          string
	state         string
	backoffReason string
	startedAt     time.Time
	cancel        context.CancelFunc
	run           func(context.Context)
	backend       func() string
	generation    uint64
}

func newCollectorRuntime(ctx context.Context, log *zap.Logger) *collectorRuntime {
	return &collectorRuntime{
		ctx:        ctx,
		log:        log.Named("collectors"),
		collectors: map[string]*collectorHandle{},
	}
}

func (r *collectorRuntime) Register(name string, run func(context.Context), backend func() string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.collectors[name]; ok {
		return
	}
	r.collectors[name] = &collectorHandle{name: name, state: "stopped", run: run, backend: backend}
}

func (r *collectorRuntime) MarkDisabled(name, reason string, backend func() string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors[name] = &collectorHandle{name: name, state: "disabled", backoffReason: reason, backend: backend}
}

func (r *collectorRuntime) SetEnabled(name string, enabled bool, reason string) {
	if enabled {
		r.Start(name)
		return
	}
	r.Stop(name, reason)
}

func (r *collectorRuntime) Start(name string) {
	r.mu.Lock()
	h := r.collectors[name]
	if h == nil || h.run == nil || h.state == "running" {
		r.mu.Unlock()
		return
	}
	childCtx, cancel := context.WithCancel(r.ctx)
	h.cancel = cancel
	h.state = "running"
	h.backoffReason = ""
	h.startedAt = time.Now().UTC()
	h.generation++
	generation := h.generation
	run := h.run
	r.mu.Unlock()

	r.log.Info("collector started", zap.String("collector", name))
	go func() {
		run(childCtx)
		r.mu.Lock()
		if cur := r.collectors[name]; cur != nil && cur.generation == generation {
			cur.cancel = nil
			if r.ctx.Err() != nil {
				cur.state = "stopped"
			} else {
				cur.state = "stopped"
				cur.backoffReason = "collector exited"
			}
		}
		r.mu.Unlock()
	}()
}

func (r *collectorRuntime) Stop(name, reason string) {
	r.mu.Lock()
	h := r.collectors[name]
	if h == nil || h.state != "running" {
		if h != nil && reason != "" {
			h.backoffReason = reason
		}
		r.mu.Unlock()
		return
	}
	cancel := h.cancel
	h.cancel = nil
	h.state = "stopped"
	h.backoffReason = reason
	h.generation++
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	r.log.Info("collector stopped", zap.String("collector", name), zap.String("reason", reason))
}

func (r *collectorRuntime) Snapshot() []collectorStateReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]collectorStateReport, 0, len(r.collectors))
	for _, h := range r.collectors {
		state := collectorStateReport{
			Name:          h.name,
			State:         h.state,
			BackoffReason: h.backoffReason,
		}
		if h.backend != nil {
			state.Backend = h.backend()
		}
		if !h.startedAt.IsZero() && h.state == "running" {
			state.StartedAt = h.startedAt.Format(time.RFC3339Nano)
		}
		out = append(out, state)
	}
	return out
}
