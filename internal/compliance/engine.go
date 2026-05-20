package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type Options struct {
	Region         string
	RuleSets       []string
	Certifications []string
	AutoApply      bool
}

type Result struct {
	RuleID      string         `json:"rule_id"`
	Passed      bool           `json:"passed"`
	Severity    string         `json:"severity"`
	Details     string         `json:"details"`
	CheckedAt   time.Time      `json:"checked_at"`
	Remediation string         `json:"remediation,omitempty"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

type Engine struct {
	log    *zap.Logger
	client *api.Client
	opts   Options
	mu     sync.RWMutex
	latest []Result
}

func NewEngine(log *zap.Logger, client *api.Client, opts Options) *Engine {
	return &Engine{log: log, client: client, opts: opts}
}

func (e *Engine) Evaluate(ctx context.Context, nodeID string, policies map[string]string) ([]Result, error) {
	if len(e.opts.RuleSets) == 0 {
		e.log.Debug("no compliance rules configured")
		return nil, nil
	}

	if e.client == nil {
		e.log.Warn("compliance client unavailable; returning cached results")
		e.mu.RLock()
		defer e.mu.RUnlock()
		out := make([]Result, len(e.latest))
		copy(out, e.latest)
		return out, nil
	}

	useRealScan := false
	if policies != nil {
		if val, ok := policies["use_real_scan"]; ok && strings.ToLower(val) == "true" {
			useRealScan = true
		}
	}

	payload := map[string]any{
		"node_id":        nodeID,
		"region":         e.opts.Region,
		"rulesets":       e.opts.RuleSets,
		"certifications": e.opts.Certifications,
		"policies":       policies,
		"auto_apply":     e.opts.AutoApply,
		"use_real_scan":  useRealScan,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal compliance payload: %w", err)
	}

	resp, err := e.client.Do(ctx, "POST", "/api/v1/compliance/evaluate", body)
	if err != nil {
		return nil, fmt.Errorf("compliance evaluate request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("compliance evaluate rejected: status %d", resp.StatusCode)
	}

	var result struct {
		Results []Result `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode compliance results: %w", err)
	}

	e.mu.Lock()
	e.latest = result.Results
	e.mu.Unlock()

	return result.Results, nil
}

func (e *Engine) Latest() []Result {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Result, len(e.latest))
	copy(out, e.latest)
	return out
}
