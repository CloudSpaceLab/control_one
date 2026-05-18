package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// DLPScanResult represents a single PII finding from a file scan.
type DLPScanResult struct {
	Path       string    `json:"path"`
	LineNumber int       `json:"line_number"`
	Match      string    `json:"match"`
	PIIType    string    `json:"pii_type"`
	Severity   string    `json:"severity"`
	RuleID     string    `json:"rule_id"`
	DetectedAt time.Time `json:"detected_at"`
}

// DLPScanReport aggregates findings from a scan run.
type DLPScanReport struct {
	NodeID       string          `json:"node_id"`
	TenantID     string          `json:"tenant_id"`
	ScanPath     string          `json:"scan_path"`
	ScannedAt    time.Time       `json:"scanned_at"`
	FilesScanned int             `json:"files_scanned"`
	Findings     []DLPScanResult `json:"findings"`
}

// DLPRule represents a classification rule fetched from control plane.
type DLPRule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PIIType  string `json:"pii_type"`
	Regex    string `json:"regex"`
	Severity string `json:"severity"`
	Enabled  bool   `json:"enabled"`
}

// DLPScanner scans files for PII based on rules from control plane.
type DLPScanner struct {
	client    *api.Client
	log       *zap.Logger
	nodeID    string
	tenantID  string
	scanPaths []string
	rules     []DLPRule
	ruleRegex map[string]*regexp.Regexp
}

// NewDLPScanner creates a new DLP scanner instance.
func NewDLPScanner(client *api.Client, log *zap.Logger, nodeID, tenantID string, scanPaths []string) *DLPScanner {
	if len(scanPaths) == 0 {
		scanPaths = []string{"/var/log", "/etc", "/home"}
	}
	return &DLPScanner{
		client:    client,
		log:       log,
		nodeID:    nodeID,
		tenantID:  tenantID,
		scanPaths: scanPaths,
		ruleRegex: make(map[string]*regexp.Regexp),
	}
}

// FetchRules retrieves DLP rules from the control plane.
func (s *DLPScanner) FetchRules(ctx context.Context) error {
	path := fmt.Sprintf("/api/v1/dlp/rules?tenant_id=%s", s.tenantID)
	resp, err := s.client.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("fetch dlp rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fetch dlp rules: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []DLPRule `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dlp rules: %w", err)
	}

	s.rules = nil
	s.ruleRegex = make(map[string]*regexp.Regexp)
	for _, rule := range result.Data {
		if !rule.Enabled {
			continue
		}
		re, err := regexp.Compile(rule.Regex)
		if err != nil {
			s.log.Warn("invalid dlp rule regex", zap.String("rule_id", rule.ID), zap.Error(err))
			continue
		}
		s.rules = append(s.rules, rule)
		s.ruleRegex[rule.ID] = re
	}

	s.log.Info("fetched dlp rules", zap.Int("count", len(s.rules)))
	return nil
}

// ScanFile scans a single file for PII matches.
func (s *DLPScanner) ScanFile(path string) ([]DLPScanResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	// Skip binary files based on extension
	ext := strings.ToLower(filepath.Ext(path))
	if isBinaryExtension(ext) {
		return nil, nil
	}

	var findings []DLPScanResult
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line size

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip very long lines (likely binary)
		if len(line) > 10000 {
			continue
		}

		for _, rule := range s.rules {
			re := s.ruleRegex[rule.ID]
			if re == nil {
				continue
			}

			matches := re.FindAllString(line, -1)
			for _, match := range matches {
				// Truncate match for display
				displayMatch := match
				if len(displayMatch) > 100 {
					displayMatch = displayMatch[:97] + "..."
				}

				findings = append(findings, DLPScanResult{
					Path:       path,
					LineNumber: lineNum,
					Match:      displayMatch,
					PIIType:    rule.PIIType,
					Severity:   rule.Severity,
					RuleID:     rule.ID,
					DetectedAt: time.Now().UTC(),
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return findings, err
	}

	return findings, nil
}

// isBinaryExtension returns true for common binary file extensions.
func isBinaryExtension(ext string) bool {
	binaryExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".bin": true, ".dat": true, ".db": true, ".sqlite": true,
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
		".zip": true, ".tar": true, ".gz": true, ".rar": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true,
	}
	return binaryExts[ext]
}

// shouldScanFile returns true if the file should be scanned based on extension.
func shouldScanFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	scanExts := map[string]bool{
		".txt": true, ".log": true, ".csv": true, ".json": true,
		".xml": true, ".yaml": true, ".yml": true, ".conf": true,
		".config": true, ".ini": true, ".properties": true,
		".sql": true, ".sh": true, ".bash": true, ".py": true,
		".js": true, ".ts": true, ".go": true, ".java": true,
		".cpp": true, ".c": true, ".h": true, ".rs": true,
		".rb": true, ".php": true, ".pl": true, ".pm": true,
		"": true, // files without extension
	}
	return scanExts[ext]
}

// ScanPaths scans configured paths and returns all findings.
func (s *DLPScanner) ScanPaths(ctx context.Context) (*DLPScanReport, error) {
	report := &DLPScanReport{
		NodeID:    s.nodeID,
		TenantID:  s.tenantID,
		ScannedAt: time.Now().UTC(),
		Findings:  []DLPScanResult{},
	}

	for _, basePath := range s.scanPaths {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}

		info, err := os.Stat(basePath)
		if err != nil {
			s.log.Warn("scan path not accessible", zap.String("path", basePath), zap.Error(err))
			continue
		}

		if !info.IsDir() {
			if shouldScanFile(basePath) {
				findings, err := s.ScanFile(basePath)
				if err != nil {
					s.log.Warn("failed to scan file", zap.String("path", basePath), zap.Error(err))
				} else {
					report.Findings = append(report.Findings, findings...)
					report.FilesScanned++
				}
			}
			continue
		}

		// Walk directory
		err = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				s.log.Debug("walk error", zap.String("path", path), zap.Error(err))
				return nil // continue walking
			}
			if info.IsDir() {
				return nil
			}
			if !shouldScanFile(path) {
				return nil
			}

			findings, err := s.ScanFile(path)
			if err != nil {
				s.log.Debug("failed to scan file", zap.String("path", path), zap.Error(err))
				return nil // continue walking
			}

			report.Findings = append(report.Findings, findings...)
			report.FilesScanned++
			return nil
		})

		if err != nil {
			s.log.Warn("directory scan error", zap.String("path", basePath), zap.Error(err))
		}
	}

	return report, nil
}

// ReportFindings sends findings to the control plane.
func (s *DLPScanner) ReportFindings(ctx context.Context, report *DLPScanReport) error {
	if len(report.Findings) == 0 {
		s.log.Info("no dlp findings to report")
		return nil
	}

	payload := struct {
		NodeID       string          `json:"node_id"`
		TenantID     string          `json:"tenant_id"`
		FilesScanned int             `json:"files_scanned"`
		Findings     []DLPScanResult `json:"findings"`
		ScannedAt    time.Time       `json:"scanned_at"`
	}{
		NodeID:       report.NodeID,
		TenantID:     report.TenantID,
		FilesScanned: report.FilesScanned,
		Findings:     report.Findings,
		ScannedAt:    report.ScannedAt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal findings: %w", err)
	}

	resp, err := s.client.Do(ctx, http.MethodPost, "/api/v1/dlp/findings", body)
	if err != nil {
		return fmt.Errorf("send findings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send findings: status=%d body=%s", resp.StatusCode, string(body))
	}

	s.log.Info("reported dlp findings", zap.Int("count", len(report.Findings)), zap.Int("files", report.FilesScanned))
	return nil
}

// Run executes a full DLP scan cycle: fetch rules, scan paths, report findings.
func (s *DLPScanner) Run(ctx context.Context) error {
	if err := s.FetchRules(ctx); err != nil {
		return fmt.Errorf("fetch rules: %w", err)
	}

	if len(s.rules) == 0 {
		s.log.Info("no dlp rules configured, skipping scan")
		return nil
	}

	report, err := s.ScanPaths(ctx)
	if err != nil {
		return fmt.Errorf("scan paths: %w", err)
	}

	if err := s.ReportFindings(ctx, report); err != nil {
		return fmt.Errorf("report findings: %w", err)
	}

	return nil
}

// RunAsync runs the scanner asynchronously.
func (s *DLPScanner) RunAsync(ctx context.Context) {
	go func() {
		if err := s.Run(ctx); err != nil {
			s.log.Error("dlp scan failed", zap.Error(err))
		}
	}()
}
