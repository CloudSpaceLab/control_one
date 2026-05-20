package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type nodeVulnerabilityResponse struct {
	ID               string         `json:"id"`
	TenantID         string         `json:"tenant_id"`
	NodeID           string         `json:"node_id"`
	PackageName      string         `json:"package_name"`
	InstalledVersion string         `json:"installed_version"`
	PackageSource    string         `json:"package_source,omitempty"`
	Arch             string         `json:"arch,omitempty"`
	CVEID            string         `json:"cve_id"`
	Severity         string         `json:"severity"`
	CVSSScore        *float64       `json:"cvss_score,omitempty"`
	EPSSScore        *float64       `json:"epss_score,omitempty"`
	KEV              bool           `json:"kev"`
	FixedVersion     string         `json:"fixed_version,omitempty"`
	AdvisoryURL      string         `json:"advisory_url,omitempty"`
	EvidenceSource   string         `json:"evidence_source"`
	References       []string       `json:"references,omitempty"`
	VEXStatus        string         `json:"vex_status,omitempty"`
	ExceptionStatus  string         `json:"exception_status,omitempty"`
	Evidence         map[string]any `json:"evidence,omitempty"`
	FirstSeenAt      string         `json:"first_seen_at"`
	LastSeenAt       string         `json:"last_seen_at"`
	ResolvedAt       *string        `json:"resolved_at,omitempty"`
	CitationID       string         `json:"citation_id"`
}

type nodeVulnerabilitySummary struct {
	Total       int            `json:"total"`
	Active      int            `json:"active"`
	Resolved    int            `json:"resolved"`
	KEV         int            `json:"kev"`
	BySeverity  map[string]int `json:"by_severity"`
	WithFix     int            `json:"with_fix"`
	WithoutFix  int            `json:"without_fix"`
	EvidenceSet []string       `json:"evidence_sources,omitempty"`
}

func (s *Server) handleNodeVulnerabilities(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for vulnerabilities", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, node.TenantID, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.VulnerabilityFindingFilter{
		TenantID:        node.TenantID,
		NodeID:          node.ID,
		CVEID:           strings.TrimSpace(r.URL.Query().Get("cve_id")),
		PackageName:     strings.TrimSpace(r.URL.Query().Get("package")),
		Severity:        strings.TrimSpace(r.URL.Query().Get("severity")),
		IncludeResolved: parseBoolQueryParam(r, "include_resolved"),
		KEVOnly:         parseBoolQueryParam(r, "kev_only"),
	}
	findings, total, err := s.store.ListNodeVulnerabilityFindings(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list node vulnerability findings", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]nodeVulnerabilityResponse, 0, len(findings))
	for _, finding := range findings {
		out = append(out, newNodeVulnerabilityResponse(finding))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       out,
		"pagination": newPaginationMeta(total, limit, offset, len(out)),
		"summary":    summarizeNodeVulnerabilities(findings, total),
		"guardrails": []string{"tenant_scoped", "read_only", "package_cve_fixed_version_evidence", "source_row_citations"},
	})
}

func newNodeVulnerabilityResponse(f storage.VulnerabilityFinding) nodeVulnerabilityResponse {
	out := nodeVulnerabilityResponse{
		ID:               f.ID.String(),
		TenantID:         f.TenantID.String(),
		NodeID:           f.NodeID.String(),
		PackageName:      f.PackageName,
		InstalledVersion: f.InstalledVersion,
		PackageSource:    f.PackageSource,
		Arch:             f.Arch,
		CVEID:            f.CVEID,
		Severity:         f.Severity,
		CVSSScore:        f.CVSSScore,
		EPSSScore:        f.EPSSScore,
		KEV:              f.KEV,
		FixedVersion:     f.FixedVersion,
		AdvisoryURL:      f.AdvisoryURL,
		EvidenceSource:   f.EvidenceSource,
		References:       append([]string(nil), f.References...),
		VEXStatus:        f.VEXStatus,
		ExceptionStatus:  f.ExceptionStatus,
		Evidence:         f.Evidence,
		FirstSeenAt:      f.FirstSeenAt.UTC().Format(time.RFC3339),
		LastSeenAt:       f.LastSeenAt.UTC().Format(time.RFC3339),
		CitationID:       citationID("node_vulnerability_findings", f.ID.String()),
	}
	if f.ResolvedAt != nil {
		resolved := f.ResolvedAt.UTC().Format(time.RFC3339)
		out.ResolvedAt = &resolved
	}
	return out
}

func (s *Server) runNodeVulnerabilitiesTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	limit := intFromToolInput(input, "limit")
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := intFromToolInput(input, "offset")
	if offset < 0 {
		offset = 0
	}
	findings, total, err := s.store.ListNodeVulnerabilityFindings(ctx, storage.VulnerabilityFindingFilter{
		TenantID:        tc.TenantID,
		NodeID:          nodeID,
		CVEID:           strings.TrimSpace(stringFromToolInput(input, "cve_id")),
		PackageName:     strings.TrimSpace(stringFromToolInput(input, "package")),
		Severity:        strings.TrimSpace(stringFromToolInput(input, "severity")),
		IncludeResolved: boolFromToolInput(input, "include_resolved"),
		KEVOnly:         boolFromToolInput(input, "kev_only"),
	}, limit, offset)
	if err != nil {
		return aiToolExecution{}, err
	}
	out := make([]nodeVulnerabilityResponse, 0, len(findings))
	for _, finding := range findings {
		out = append(out, newNodeVulnerabilityResponse(finding))
	}
	payload := map[string]any{
		"data":       out,
		"pagination": newPaginationMeta(total, limit, offset, len(out)),
		"summary":    summarizeNodeVulnerabilities(findings, total),
		"guardrails": []string{"tenant_scoped", "read_only", "package_cve_fixed_version_evidence", "source_row_citations", "no_patch_execution"},
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "node_vulnerabilities", Label: "vulnerability findings", Detail: fmt.Sprintf("%d findings", len(out))},
		Payload:  payload,
	}, nil
}

func summarizeNodeVulnerabilities(findings []storage.VulnerabilityFinding, total int) nodeVulnerabilitySummary {
	out := nodeVulnerabilitySummary{
		Total:      total,
		BySeverity: map[string]int{},
	}
	evidence := map[string]struct{}{}
	for _, finding := range findings {
		if finding.ResolvedAt != nil {
			out.Resolved++
		} else {
			out.Active++
		}
		if finding.KEV {
			out.KEV++
		}
		if strings.TrimSpace(finding.FixedVersion) != "" {
			out.WithFix++
		} else {
			out.WithoutFix++
		}
		incrementStringCount(out.BySeverity, firstNonEmptyString(finding.Severity, "unknown"))
		if src := strings.TrimSpace(finding.EvidenceSource); src != "" {
			evidence[src] = struct{}{}
		}
	}
	for src := range evidence {
		out.EvidenceSet = append(out.EvidenceSet, src)
	}
	return out
}

func boolFromToolInput(input map[string]any, key string) bool {
	if input == nil {
		return false
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && parsed
	default:
		return false
	}
}

func parseBoolQueryParam(r *http.Request, key string) bool {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

func vulnerabilitySeverityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}
