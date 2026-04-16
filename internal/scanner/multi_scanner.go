package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/policy"
)

// MultiScanner routes rules to appropriate scanner adapters based on rule metadata
type MultiScanner struct {
	log      *zap.Logger
	registry *ScannerRegistry
	fallback Runner
	mu       sync.RWMutex
}

// NewMultiScanner creates a new multi-scanner that routes to appropriate adapters
func NewMultiScanner(log *zap.Logger, fallback Runner) *MultiScanner {
	registry := NewScannerRegistry(log)
	return &MultiScanner{
		log:      log,
		registry: registry,
		fallback: fallback,
	}
}

// Run executes scans using appropriate scanner adapters based on rule metadata
func (m *MultiScanner) Run(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	results := make([]Result, 0, len(rules))

	for _, rule := range rules {
		scannerName := m.determineScanner(rule)

		adapter, ok := m.registry.Get(scannerName)
		if !ok || !adapter.IsAvailable() {
			if m.fallback != nil {
				fallbackResults, err := m.fallback.Run(ctx, []policy.Rule{rule})
				if err != nil {
					m.log.Warn("fallback scanner failed",
						zap.String("rule_id", rule.ID),
						zap.String("scanner", scannerName),
						zap.Error(err))
					results = append(results, Result{
						RuleID:    rule.ID,
						Status:    StatusError,
						Details:   fmt.Sprintf("scanner %s not available and fallback failed: %v", scannerName, err),
						CheckedAt: fallbackResults[0].CheckedAt,
					})
					continue
				}
				results = append(results, fallbackResults[0])
				continue
			}

			results = append(results, Result{
				RuleID:  rule.ID,
				Status:  StatusError,
				Details: fmt.Sprintf("scanner %s not available and no fallback", scannerName),
			})
			continue
		}

		adapterResults, err := adapter.Scan(ctx, []policy.Rule{rule})
		if err != nil {
			m.log.Warn("adapter scan failed",
				zap.String("rule_id", rule.ID),
				zap.String("scanner", scannerName),
				zap.Error(err))

			if m.fallback != nil {
				fallbackResults, fallbackErr := m.fallback.Run(ctx, []policy.Rule{rule})
				if fallbackErr == nil && len(fallbackResults) > 0 {
					results = append(results, fallbackResults[0])
					continue
				}
			}

			results = append(results, Result{
				RuleID:  rule.ID,
				Status:  StatusError,
				Details: fmt.Sprintf("scanner %s failed: %v", scannerName, err),
			})
			continue
		}

		if len(adapterResults) > 0 {
			results = append(results, adapterResults[0])
		}
	}

	return results, nil
}

// determineScanner determines which scanner to use based on rule metadata
func (m *MultiScanner) determineScanner(rule policy.Rule) string {
	check := strings.ToLower(rule.Check)

	if strings.Contains(check, "openscap") || strings.Contains(check, "xccdf") || strings.Contains(check, "ds:") {
		return "openscap"
	}
	if strings.Contains(check, "inspec") || strings.Contains(check, "profile://") {
		return "inspec"
	}
	if strings.Contains(check, "ansible") || strings.HasSuffix(check, ".yml") || strings.HasSuffix(check, ".yaml") {
		return "ansible"
	}
	if strings.Contains(check, "trivy") || strings.HasPrefix(check, "fs:") || strings.HasPrefix(check, "image:") {
		return "trivy"
	}

	// Check rule ID or check content for scanner hints
	if strings.Contains(rule.ID, "openscap") || strings.Contains(rule.ID, "inspec") || strings.Contains(rule.ID, "ansible") || strings.Contains(rule.ID, "trivy") {
		if strings.Contains(rule.ID, "openscap") {
			return "openscap"
		}
		if strings.Contains(rule.ID, "inspec") {
			return "inspec"
		}
		if strings.Contains(rule.ID, "ansible") {
			return "ansible"
		}
		if strings.Contains(rule.ID, "trivy") {
			return "trivy"
		}
	}

	return "builtin"
}

// RegisterScanner adds a custom scanner adapter
func (m *MultiScanner) RegisterScanner(name string, adapter ScannerAdapter) {
	m.registry.Register(name, adapter)
}

// ListAvailableScanners returns list of available scanner names
func (m *MultiScanner) ListAvailableScanners() []string {
	return m.registry.List()
}

// GetAdapter retrieves a scanner adapter by name
func (m *MultiScanner) GetAdapter(name string) (ScannerAdapter, bool) {
	return m.registry.Get(name)
}
