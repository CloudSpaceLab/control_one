package server

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type complianceEvidenceQuery struct {
	TenantID     uuid.UUID
	NodeID       uuid.UUID
	RuleID       string
	Framework    string
	ControlRef   string
	EvidenceType string
	Passed       *bool
	Since        time.Time
	Until        time.Time
	Limit        int
	Offset       int
}

type complianceEvidenceQueryResponse struct {
	TenantID          string                                `json:"tenant_id"`
	Since             time.Time                             `json:"since"`
	Until             time.Time                             `json:"until"`
	Summary           complianceEvidenceQuerySummary        `json:"summary"`
	EvaluatorEvidence []complianceEvaluatorEvidenceResponse `json:"evaluator_evidence"`
	UploadedEvidence  []complianceUploadedEvidenceResponse  `json:"uploaded_evidence"`
	Citations         []complianceEvidenceCitation          `json:"citations"`
	Guardrails        []string                              `json:"guardrails"`
}

type complianceEvidenceQuerySummary struct {
	Total             int            `json:"total"`
	EvaluatorEvidence int            `json:"evaluator_evidence"`
	UploadedEvidence  int            `json:"uploaded_evidence"`
	Redacted          int            `json:"redacted"`
	Fresh             int            `json:"fresh"`
	Aging             int            `json:"aging"`
	Stale             int            `json:"stale"`
	Expired           int            `json:"expired"`
	ByFramework       map[string]int `json:"by_framework,omitempty"`
	ByControl         map[string]int `json:"by_control,omitempty"`
}

type complianceEvaluatorEvidenceResponse struct {
	ResultID    string         `json:"result_id"`
	JobID       string         `json:"job_id"`
	TenantID    string         `json:"tenant_id,omitempty"`
	NodeID      string         `json:"node_id,omitempty"`
	ScanID      string         `json:"scan_id,omitempty"`
	RuleID      string         `json:"rule_id"`
	Framework   string         `json:"framework,omitempty"`
	ControlRef  string         `json:"control_ref,omitempty"`
	Passed      bool           `json:"passed"`
	Severity    string         `json:"severity,omitempty"`
	Details     string         `json:"details,omitempty"`
	CheckedAt   string         `json:"checked_at,omitempty"`
	Evidence    map[string]any `json:"evidence,omitempty"`
	Redacted    bool           `json:"redacted"`
	AgeSeconds  int64          `json:"age_seconds,omitempty"`
	Freshness   string         `json:"freshness_status,omitempty"`
	CitationIDs []string       `json:"citation_ids"`
}

type complianceUploadedEvidenceResponse struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id,omitempty"`
	EvidenceType  string   `json:"evidence_type"`
	Framework     string   `json:"framework,omitempty"`
	ControlRef    string   `json:"control_ref,omitempty"`
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	FileSizeBytes *int64   `json:"file_size_bytes,omitempty"`
	MimeType      string   `json:"mime_type,omitempty"`
	Checksum      string   `json:"checksum,omitempty"`
	UploadedBy    string   `json:"uploaded_by,omitempty"`
	UploadedAt    string   `json:"uploaded_at"`
	ExpiresAt     string   `json:"expires_at,omitempty"`
	AgeSeconds    int64    `json:"age_seconds,omitempty"`
	Freshness     string   `json:"freshness_status,omitempty"`
	CitationIDs   []string `json:"citation_ids"`
}

type complianceEvidenceCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
}

func (s *Server) buildComplianceEvidenceQuery(ctx context.Context, q complianceEvidenceQuery, guardrails []string) (complianceEvidenceQueryResponse, error) {
	if q.Limit <= 0 || q.Limit > maxListLimit {
		q.Limit = 100
	}
	resp := complianceEvidenceQueryResponse{
		TenantID:   q.TenantID.String(),
		Since:      q.Since,
		Until:      q.Until,
		Guardrails: append([]string{"tenant_scoped", "read_only", "no_file_contents", "credentials_redacted", "source_row_citations"}, guardrails...),
		Summary: complianceEvidenceQuerySummary{
			ByFramework: map[string]int{},
			ByControl:   map[string]int{},
		},
	}
	if s == nil || s.store == nil {
		return resp, nil
	}

	results, _, err := s.store.ListComplianceResultsFiltered(ctx, storage.ComplianceResultFilter{
		TenantID: q.TenantID,
		NodeID:   q.NodeID,
		RuleID:   q.RuleID,
		Passed:   q.Passed,
		Since:    &q.Since,
		Until:    &q.Until,
	}, q.Limit, q.Offset)
	if err != nil {
		return resp, err
	}
	for _, result := range results {
		item, citation, ok := complianceEvaluatorEvidenceItem(result, q)
		if !ok {
			continue
		}
		resp.EvaluatorEvidence = append(resp.EvaluatorEvidence, item)
		resp.Citations = append(resp.Citations, citation)
	}

	uploaded, _, err := s.listComplianceEvidenceFiltered(ctx, q, q.Limit, q.Offset)
	if err != nil {
		return resp, err
	}
	for _, evidence := range uploaded {
		if !complianceUploadedEvidenceMatches(evidence, q) {
			continue
		}
		item, citation := complianceUploadedEvidenceItem(evidence, q.Until)
		resp.UploadedEvidence = append(resp.UploadedEvidence, item)
		resp.Citations = append(resp.Citations, citation)
	}

	resp.Summary = summarizeComplianceEvidenceQuery(resp)
	return resp, nil
}

func complianceEvaluatorEvidenceItem(result storage.ComplianceResult, q complianceEvidenceQuery) (complianceEvaluatorEvidenceResponse, complianceEvidenceCitation, bool) {
	evidence := complianceEvidenceFromMetadata(result.Metadata)
	if len(evidence) == 0 {
		return complianceEvaluatorEvidenceResponse{}, complianceEvidenceCitation{}, false
	}
	framework := firstNonEmptyString(mapString(evidence, "framework"), mapString(result.Metadata, "framework"))
	control := firstNonEmptyString(mapString(evidence, "control"), mapString(result.Metadata, "control_ref"))
	if q.Framework != "" && !strings.EqualFold(q.Framework, framework) {
		return complianceEvaluatorEvidenceResponse{}, complianceEvidenceCitation{}, false
	}
	if q.ControlRef != "" && !strings.EqualFold(q.ControlRef, control) {
		return complianceEvaluatorEvidenceResponse{}, complianceEvidenceCitation{}, false
	}
	citationID := citationID("compliance_results", result.ID.String())
	item := complianceEvaluatorEvidenceResponse{
		ResultID:    result.ID.String(),
		JobID:       result.JobID.String(),
		RuleID:      result.RuleID,
		Framework:   framework,
		ControlRef:  control,
		Passed:      result.Passed,
		Evidence:    evidence,
		Redacted:    boolFromMap(result.Metadata, "evidence_redacted"),
		CitationIDs: []string{citationID},
	}
	if result.TenantID != uuid.Nil {
		item.TenantID = result.TenantID.String()
	}
	if result.NodeID != uuid.Nil {
		item.NodeID = result.NodeID.String()
	}
	if result.ScanID != nil {
		item.ScanID = strings.TrimSpace(*result.ScanID)
	}
	if result.Severity != nil {
		item.Severity = strings.TrimSpace(*result.Severity)
	}
	if result.Details != nil {
		item.Details = strings.TrimSpace(*result.Details)
	}
	if result.CheckedAt != nil {
		item.CheckedAt = result.CheckedAt.UTC().Format(time.RFC3339)
		item.AgeSeconds = complianceEvidenceAgeSeconds(*result.CheckedAt, q.Until)
		item.Freshness = complianceEvidenceFreshness(*result.CheckedAt, nil, q.Until)
	}
	return item, complianceEvidenceCitation{ID: citationID, Kind: "compliance_result", Table: "compliance_results", SourceRecordID: "compliance_results:" + result.ID.String()}, true
}

func complianceUploadedEvidenceMatches(evidence storage.ComplianceEvidence, q complianceEvidenceQuery) bool {
	if evidence.TenantID != q.TenantID {
		return false
	}
	if q.Framework != "" && !strings.EqualFold(q.Framework, complianceStringPtrValue(evidence.Framework)) {
		return false
	}
	if q.EvidenceType != "" && !strings.EqualFold(q.EvidenceType, evidence.EvidenceType) {
		return false
	}
	if q.ControlRef != "" && !strings.EqualFold(q.ControlRef, complianceStringPtrValue(evidence.ControlRef)) {
		return false
	}
	if !q.Since.IsZero() && evidence.UploadedAt.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && evidence.UploadedAt.After(q.Until) {
		return false
	}
	if evidence.ExpiresAt != nil && !evidence.ExpiresAt.After(complianceEvidenceReferenceTime(q.Until)) {
		return false
	}
	return true
}

func complianceUploadedEvidenceItem(evidence storage.ComplianceEvidence, reference time.Time) (complianceUploadedEvidenceResponse, complianceEvidenceCitation) {
	citationID := citationID("compliance_evidence", evidence.ID.String())
	item := complianceUploadedEvidenceResponse{
		ID:           evidence.ID.String(),
		TenantID:     evidence.TenantID.String(),
		EvidenceType: evidence.EvidenceType,
		Framework:    complianceStringPtrValue(evidence.Framework),
		ControlRef:   complianceStringPtrValue(evidence.ControlRef),
		Title:        evidence.Title,
		Description:  complianceStringPtrValue(evidence.Description),
		UploadedAt:   evidence.UploadedAt.UTC().Format(time.RFC3339),
		AgeSeconds:   complianceEvidenceAgeSeconds(evidence.UploadedAt, reference),
		Freshness:    complianceEvidenceFreshness(evidence.UploadedAt, evidence.ExpiresAt, reference),
		CitationIDs:  []string{citationID},
	}
	if evidence.UploadedBy != uuid.Nil {
		item.UploadedBy = evidence.UploadedBy.String()
	}
	if evidence.FileSizeBytes != nil {
		item.FileSizeBytes = evidence.FileSizeBytes
	}
	if evidence.MimeType != nil {
		item.MimeType = strings.TrimSpace(*evidence.MimeType)
	}
	if evidence.Checksum != nil {
		item.Checksum = strings.TrimSpace(*evidence.Checksum)
	}
	if evidence.ExpiresAt != nil {
		item.ExpiresAt = evidence.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return item, complianceEvidenceCitation{ID: citationID, Kind: "compliance_evidence", Table: "compliance_evidence", SourceRecordID: "compliance_evidence:" + evidence.ID.String()}
}

type complianceEvidenceFilteredStore interface {
	ListComplianceEvidenceFiltered(context.Context, storage.ComplianceEvidenceFilter, int, int) ([]storage.ComplianceEvidence, int, error)
}

func (s *Server) listComplianceEvidenceFiltered(ctx context.Context, q complianceEvidenceQuery, limit, offset int) ([]storage.ComplianceEvidence, int, error) {
	filter := storage.ComplianceEvidenceFilter{
		TenantID:            q.TenantID,
		Framework:           q.Framework,
		ControlRef:          q.ControlRef,
		EvidenceType:        q.EvidenceType,
		UploadedSince:       timePtrIfSet(q.Since),
		UploadedUntil:       timePtrIfSet(q.Until),
		ExpirationReference: timePtrIfSet(complianceEvidenceReferenceTime(q.Until)),
		IncludeExpired:      false,
	}
	if store, ok := s.store.(complianceEvidenceFilteredStore); ok {
		return store.ListComplianceEvidenceFiltered(ctx, filter, limit, offset)
	}
	items, _, err := s.store.ListComplianceEvidence(ctx, q.TenantID, q.Framework, q.EvidenceType, maxListLimit, 0)
	if err != nil {
		return nil, 0, err
	}
	out := make([]storage.ComplianceEvidence, 0, len(items))
	for _, item := range items {
		if complianceUploadedEvidenceMatches(item, q) {
			out = append(out, item)
		}
	}
	total := len(out)
	if offset > len(out) {
		return []storage.ComplianceEvidence{}, total, nil
	}
	if offset > 0 {
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func timePtrIfSet(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func summarizeComplianceEvidenceQuery(resp complianceEvidenceQueryResponse) complianceEvidenceQuerySummary {
	out := complianceEvidenceQuerySummary{
		Total:             len(resp.EvaluatorEvidence) + len(resp.UploadedEvidence),
		EvaluatorEvidence: len(resp.EvaluatorEvidence),
		UploadedEvidence:  len(resp.UploadedEvidence),
		ByFramework:       map[string]int{},
		ByControl:         map[string]int{},
	}
	for _, item := range resp.EvaluatorEvidence {
		if item.Redacted {
			out.Redacted++
		}
		incrementComplianceFreshness(&out, item.Freshness)
		incrementStringCount(out.ByFramework, item.Framework)
		incrementStringCount(out.ByControl, item.ControlRef)
	}
	for _, item := range resp.UploadedEvidence {
		incrementComplianceFreshness(&out, item.Freshness)
		incrementStringCount(out.ByFramework, item.Framework)
		incrementStringCount(out.ByControl, item.ControlRef)
	}
	if len(out.ByFramework) == 0 {
		out.ByFramework = nil
	}
	if len(out.ByControl) == 0 {
		out.ByControl = nil
	}
	return out
}

func incrementComplianceFreshness(out *complianceEvidenceQuerySummary, status string) {
	switch status {
	case "fresh":
		out.Fresh++
	case "aging":
		out.Aging++
	case "stale":
		out.Stale++
	case "expired":
		out.Expired++
	}
}

func complianceEvidenceReferenceTime(reference time.Time) time.Time {
	if reference.IsZero() {
		return time.Now().UTC()
	}
	return reference.UTC()
}

func complianceEvidenceAgeSeconds(observedAt, reference time.Time) int64 {
	if observedAt.IsZero() {
		return 0
	}
	age := complianceEvidenceReferenceTime(reference).Sub(observedAt.UTC())
	if age < 0 {
		return 0
	}
	return int64(age.Seconds())
}

func complianceEvidenceFreshness(observedAt time.Time, expiresAt *time.Time, reference time.Time) string {
	ref := complianceEvidenceReferenceTime(reference)
	if expiresAt != nil && !expiresAt.After(ref) {
		return "expired"
	}
	age := ref.Sub(observedAt.UTC())
	switch {
	case age < 0:
		return "fresh"
	case age <= 7*24*time.Hour:
		return "fresh"
	case age <= 30*24*time.Hour:
		return "aging"
	default:
		return "stale"
	}
}

func incrementStringCount(values map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key != "" {
		values[key]++
	}
}

func mapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return strings.TrimSpace(detailsString(values, key, ""))
}

func boolFromMap(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func complianceStringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
