package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/policy"
)

// ScannerAdapter defines the interface for compliance scanner implementations
type ScannerAdapter interface {
	Name() string
	Scan(ctx context.Context, rules []policy.Rule) ([]Result, error)
	IsAvailable() bool
}

// ScannerRegistry manages available scanner adapters
type ScannerRegistry struct {
	adapters map[string]ScannerAdapter
	log      *zap.Logger
}

// NewScannerRegistry creates a new scanner registry
func NewScannerRegistry(log *zap.Logger) *ScannerRegistry {
	registry := &ScannerRegistry{
		adapters: make(map[string]ScannerAdapter),
		log:      log,
	}
	registry.registerDefaultAdapters()
	return registry
}

func (r *ScannerRegistry) registerDefaultAdapters() {
	r.Register("openscap", NewOpenSCAPAdapter(r.log))
	r.Register("inspec", NewInSpecAdapter(r.log))
	r.Register("ansible", NewAnsibleAdapter(r.log))
	r.Register("trivy", NewTrivyAdapter(r.log))
	builtinScanner := NewBuiltinScanner(r.log, Options{
		Timeout:       30 * time.Second,
		MaxConcurrent: 4,
	})
	r.Register("builtin", &builtinAdapter{scanner: builtinScanner})
}

// Register adds a scanner adapter to the registry
func (r *ScannerRegistry) Register(name string, adapter ScannerAdapter) {
	r.adapters[name] = adapter
}

// Get retrieves a scanner adapter by name
func (r *ScannerRegistry) Get(name string) (ScannerAdapter, bool) {
	adapter, ok := r.adapters[name]
	return adapter, ok
}

// List returns all registered scanner names
func (r *ScannerRegistry) List() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

// GetAvailable returns all available (installed) scanners
func (r *ScannerRegistry) GetAvailable() []ScannerAdapter {
	var available []ScannerAdapter
	for _, adapter := range r.adapters {
		if adapter.IsAvailable() {
			available = append(available, adapter)
		}
	}
	return available
}

// OpenSCAPAdapter implements OpenSCAP scanner integration
type OpenSCAPAdapter struct {
	log        *zap.Logger
	oscapPath  string
	profileDir string
}

// NewOpenSCAPAdapter creates a new OpenSCAP adapter
func NewOpenSCAPAdapter(log *zap.Logger) *OpenSCAPAdapter {
	adapter := &OpenSCAPAdapter{log: log}
	adapter.oscapPath = adapter.findOpenSCAP()
	adapter.profileDir = "/usr/share/xml/scap"
	return adapter
}

func (a *OpenSCAPAdapter) Name() string {
	return "openscap"
}

func (a *OpenSCAPAdapter) IsAvailable() bool {
	return a.oscapPath != ""
}

func (a *OpenSCAPAdapter) findOpenSCAP() string {
	paths := []string{
		"oscap",
		"/usr/bin/oscap",
		"/usr/local/bin/oscap",
	}
	for _, path := range paths {
		if _, err := exec.LookPath(path); err == nil {
			return path
		}
	}
	return ""
}

func (a *OpenSCAPAdapter) Scan(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	if !a.IsAvailable() {
		return nil, fmt.Errorf("openscap not available")
	}

	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		result := Result{
			RuleID:    rule.ID,
			CheckedAt: time.Now().UTC(),
			Metadata: map[string]string{
				"severity": rule.Severity,
				"version":  rule.Version,
				"scanner":  "openscap",
			},
		}

		profile := a.extractProfile(rule)
		if profile == "" {
			result.Status = StatusError
			result.Details = "no OpenSCAP profile specified in rule"
			results = append(results, result)
			continue
		}

		scanResult, err := a.runOpenSCAP(ctx, profile, rule)
		if err != nil {
			result.Status = StatusError
			result.Details = fmt.Sprintf("openscap scan failed: %v", err)
			results = append(results, result)
			continue
		}

		result.Status = scanResult.Status
		result.Details = scanResult.Details
		if scanResult.Metadata != nil {
			for k, v := range scanResult.Metadata {
				result.Metadata[k] = v
			}
		}
		results = append(results, result)
	}

	return results, nil
}

func (a *OpenSCAPAdapter) extractProfile(rule policy.Rule) string {
	if strings.Contains(rule.Check, "xccdf") || strings.Contains(rule.Check, "ds:") {
		return rule.Check
	}
	if strings.Contains(rule.ID, "xccdf") {
		return rule.ID
	}
	return ""
}

func (a *OpenSCAPAdapter) runOpenSCAP(ctx context.Context, profile string, rule policy.Rule) (*Result, error) {
	cmd := exec.CommandContext(ctx, a.oscapPath, "xccdf", "eval",
		"--profile", profile,
		"--results", "/tmp/oscap-results.xml",
		profile)

	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		if strings.Contains(outputStr, "pass") {
			return &Result{Status: StatusCompliant, Details: outputStr}, nil
		}
		return &Result{Status: StatusNonCompliant, Details: outputStr}, nil
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "pass") && !strings.Contains(outputStr, "fail") {
		return &Result{Status: StatusCompliant, Details: outputStr}, nil
	}
	return &Result{Status: StatusNonCompliant, Details: outputStr}, nil
}

// InSpecAdapter implements Chef InSpec scanner integration
type InSpecAdapter struct {
	log      *zap.Logger
	inspecPath string
}

// NewInSpecAdapter creates a new InSpec adapter
func NewInSpecAdapter(log *zap.Logger) *InSpecAdapter {
	adapter := &InSpecAdapter{log: log}
	adapter.inspecPath = adapter.findInSpec()
	return adapter
}

func (a *InSpecAdapter) Name() string {
	return "inspec"
}

func (a *InSpecAdapter) IsAvailable() bool {
	return a.inspecPath != ""
}

func (a *InSpecAdapter) findInSpec() string {
	paths := []string{
		"inspec",
		"/usr/bin/inspec",
		"/usr/local/bin/inspec",
		"/opt/chef-workstation/bin/inspec",
	}
	for _, path := range paths {
		if _, err := exec.LookPath(path); err == nil {
			return path
		}
	}
	return ""
}

func (a *InSpecAdapter) Scan(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	if !a.IsAvailable() {
		return nil, fmt.Errorf("inspec not available")
	}

	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		result := Result{
			RuleID:    rule.ID,
			CheckedAt: time.Now().UTC(),
			Metadata: map[string]string{
				"severity": rule.Severity,
				"version":  rule.Version,
				"scanner":  "inspec",
			},
		}

		profile := a.extractProfile(rule)
		if profile == "" {
			result.Status = StatusError
			result.Details = "no InSpec profile specified in rule"
			results = append(results, result)
			continue
		}

		scanResult, err := a.runInSpec(ctx, profile, rule)
		if err != nil {
			result.Status = StatusError
			result.Details = fmt.Sprintf("inspec scan failed: %v", err)
			results = append(results, result)
			continue
		}

		result.Status = scanResult.Status
		result.Details = scanResult.Details
		if scanResult.Metadata != nil {
			for k, v := range scanResult.Metadata {
				result.Metadata[k] = v
			}
		}
		results = append(results, result)
	}

	return results, nil
}

func (a *InSpecAdapter) extractProfile(rule policy.Rule) string {
	if strings.HasPrefix(rule.Check, "inspec://") || strings.HasPrefix(rule.Check, "profile://") {
		return strings.TrimPrefix(strings.TrimPrefix(rule.Check, "inspec://"), "profile://")
	}
	if filepath.Ext(rule.Check) == ".rb" || strings.Contains(rule.Check, "inspec") {
		return rule.Check
	}
	return ""
}

func (a *InSpecAdapter) runInSpec(ctx context.Context, profile string, rule policy.Rule) (*Result, error) {
	outputFile := fmt.Sprintf("/tmp/inspec-%s.json", rule.ID)
	cmd := exec.CommandContext(ctx, a.inspecPath, "exec", profile,
		"--format", "json",
		"--output", outputFile)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &Result{Status: StatusError, Details: string(output)}, err
	}

	var report struct {
		Profiles []struct {
			Controls []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"controls"`
		} `json:"profiles"`
	}

	if reportData, readErr := readJSONFile(outputFile); readErr == nil {
		if jsonErr := json.Unmarshal(reportData, &report); jsonErr == nil {
			for _, profile := range report.Profiles {
				for _, control := range profile.Controls {
					if control.ID == rule.ID {
						status := StatusNonCompliant
						if control.Status == "passed" {
							status = StatusCompliant
						}
						return &Result{Status: status, Details: string(output)}, nil
					}
				}
			}
		}
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "0 failures") {
		return &Result{Status: StatusCompliant, Details: outputStr}, nil
	}
	return &Result{Status: StatusNonCompliant, Details: outputStr}, nil
}

// AnsibleAdapter implements Ansible playbook execution for compliance
type AnsibleAdapter struct {
	log         *zap.Logger
	ansiblePath string
}

// NewAnsibleAdapter creates a new Ansible adapter
func NewAnsibleAdapter(log *zap.Logger) *AnsibleAdapter {
	adapter := &AnsibleAdapter{log: log}
	adapter.ansiblePath = adapter.findAnsible()
	return adapter
}

func (a *AnsibleAdapter) Name() string {
	return "ansible"
}

func (a *AnsibleAdapter) IsAvailable() bool {
	return a.ansiblePath != ""
}

func (a *AnsibleAdapter) findAnsible() string {
	paths := []string{
		"ansible-playbook",
		"/usr/bin/ansible-playbook",
		"/usr/local/bin/ansible-playbook",
	}
	for _, path := range paths {
		if _, err := exec.LookPath(path); err == nil {
			return path
		}
	}
	return ""
}

func (a *AnsibleAdapter) Scan(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	if !a.IsAvailable() {
		return nil, fmt.Errorf("ansible not available")
	}

	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		result := Result{
			RuleID:    rule.ID,
			CheckedAt: time.Now().UTC(),
			Metadata: map[string]string{
				"severity": rule.Severity,
				"version":  rule.Version,
				"scanner":  "ansible",
			},
		}

		playbook := a.extractPlaybook(rule)
		if playbook == "" {
			result.Status = StatusError
			result.Details = "no Ansible playbook specified in rule"
			results = append(results, result)
			continue
		}

		scanResult, err := a.runAnsible(ctx, playbook, rule)
		if err != nil {
			result.Status = StatusError
			result.Details = fmt.Sprintf("ansible execution failed: %v", err)
			results = append(results, result)
			continue
		}

		result.Status = scanResult.Status
		result.Details = scanResult.Details
		if scanResult.Metadata != nil {
			for k, v := range scanResult.Metadata {
				result.Metadata[k] = v
			}
		}
		results = append(results, result)
	}

	return results, nil
}

func (a *AnsibleAdapter) extractPlaybook(rule policy.Rule) string {
	if strings.HasSuffix(rule.Check, ".yml") || strings.HasSuffix(rule.Check, ".yaml") {
		return rule.Check
	}
	if strings.HasPrefix(rule.Check, "ansible://") {
		return strings.TrimPrefix(rule.Check, "ansible://")
	}
	return ""
}

func (a *AnsibleAdapter) runAnsible(ctx context.Context, playbook string, rule policy.Rule) (*Result, error) {
	cmd := exec.CommandContext(ctx, a.ansiblePath, playbook,
		"--check",
		"--diff",
		"-e", fmt.Sprintf("rule_id=%s", rule.ID))

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if strings.Contains(outputStr, "changed=0") && strings.Contains(outputStr, "failed=0") {
			return &Result{Status: StatusCompliant, Details: outputStr}, nil
		}
		return &Result{Status: StatusNonCompliant, Details: outputStr}, nil
	}

	if strings.Contains(outputStr, "changed=0") && strings.Contains(outputStr, "failed=0") {
		return &Result{Status: StatusCompliant, Details: outputStr}, nil
	}
	return &Result{Status: StatusNonCompliant, Details: outputStr}, nil
}

// TrivyAdapter implements Trivy vulnerability scanner for continuous monitoring
type TrivyAdapter struct {
	log       *zap.Logger
	trivyPath string
}

// NewTrivyAdapter creates a new Trivy adapter
func NewTrivyAdapter(log *zap.Logger) *TrivyAdapter {
	adapter := &TrivyAdapter{log: log}
	adapter.trivyPath = adapter.findTrivy()
	return adapter
}

func (a *TrivyAdapter) Name() string {
	return "trivy"
}

func (a *TrivyAdapter) IsAvailable() bool {
	return a.trivyPath != ""
}

func (a *TrivyAdapter) findTrivy() string {
	paths := []string{
		"trivy",
		"/usr/local/bin/trivy",
		"/usr/bin/trivy",
	}
	for _, path := range paths {
		if _, err := exec.LookPath(path); err == nil {
			return path
		}
	}
	return ""
}

func (a *TrivyAdapter) Scan(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	if !a.IsAvailable() {
		return nil, fmt.Errorf("trivy not available")
	}

	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		result := Result{
			RuleID:    rule.ID,
			CheckedAt: time.Now().UTC(),
			Metadata: map[string]string{
				"severity": rule.Severity,
				"version":  rule.Version,
				"scanner":  "trivy",
			},
		}

		target := a.extractTarget(rule)
		if target == "" {
			target = "fs:/"
		}

		scanResult, err := a.runTrivy(ctx, target, rule)
		if err != nil {
			result.Status = StatusError
			result.Details = fmt.Sprintf("trivy scan failed: %v", err)
			results = append(results, result)
			continue
		}

		result.Status = scanResult.Status
		result.Details = scanResult.Details
		if scanResult.Metadata != nil {
			for k, v := range scanResult.Metadata {
				result.Metadata[k] = v
			}
		}
		results = append(results, result)
	}

	return results, nil
}

func (a *TrivyAdapter) extractTarget(rule policy.Rule) string {
	if strings.HasPrefix(rule.Check, "trivy://") {
		return strings.TrimPrefix(rule.Check, "trivy://")
	}
	if strings.HasPrefix(rule.Check, "fs:") || strings.HasPrefix(rule.Check, "image:") {
		return rule.Check
	}
	return ""
}

func (a *TrivyAdapter) runTrivy(ctx context.Context, target string, rule policy.Rule) (*Result, error) {
	outputFile := fmt.Sprintf("/tmp/trivy-%s.json", rule.ID)
	cmd := exec.CommandContext(ctx, a.trivyPath, target,
		"--format", "json",
		"--output", outputFile,
		"--severity", "CRITICAL,HIGH",
		"--exit-code", "0")

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	var report struct {
		Results []struct {
			Vulnerabilities []struct {
				Severity string `json:"Severity"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}

	if reportData, readErr := readJSONFile(outputFile); readErr == nil {
		if jsonErr := json.Unmarshal(reportData, &report); jsonErr == nil {
			criticalCount := 0
			highCount := 0
			for _, result := range report.Results {
				for _, vuln := range result.Vulnerabilities {
					if vuln.Severity == "CRITICAL" {
						criticalCount++
					} else if vuln.Severity == "HIGH" {
						highCount++
					}
				}
			}

			if criticalCount == 0 && highCount == 0 {
				return &Result{
					Status: StatusCompliant,
					Details: outputStr,
					Metadata: map[string]string{
						"critical_vulns": "0",
						"high_vulns":      "0",
					},
				}, nil
			}

			return &Result{
				Status: StatusNonCompliant,
				Details: fmt.Sprintf("Found %d critical and %d high severity vulnerabilities", criticalCount, highCount),
				Metadata: map[string]string{
					"critical_vulns": fmt.Sprintf("%d", criticalCount),
					"high_vulns":      fmt.Sprintf("%d", highCount),
				},
			}, nil
		}
	}

	if err != nil {
		return &Result{Status: StatusError, Details: outputStr}, err
	}

	if strings.Contains(outputStr, "Total: 0") {
		return &Result{Status: StatusCompliant, Details: outputStr}, nil
	}
	return &Result{Status: StatusNonCompliant, Details: outputStr}, nil
}

func readJSONFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// builtinAdapter wraps BuiltinScanner to implement ScannerAdapter
type builtinAdapter struct {
	scanner *BuiltinScanner
}

func (b *builtinAdapter) Name() string {
	return "builtin"
}

func (b *builtinAdapter) IsAvailable() bool {
	return true
}

func (b *builtinAdapter) Scan(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	return b.scanner.Run(ctx, rules)
}

