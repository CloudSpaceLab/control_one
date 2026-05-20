package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type postureDriftExplainQuery struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

type postureDriftExplainResponse struct {
	TenantID   string                        `json:"tenant_id"`
	NodeID     string                        `json:"node_id,omitempty"`
	Since      time.Time                     `json:"since"`
	Until      time.Time                     `json:"until"`
	Desired    *postureDesiredStateResponse  `json:"desired,omitempty"`
	Summary    postureDriftSummary           `json:"summary"`
	Receipts   []postureDriftReceiptResponse `json:"receipts"`
	Citations  []postureDriftCitation        `json:"citations"`
	Guardrails []string                      `json:"guardrails"`
}

type postureDesiredStateResponse struct {
	DesiredStateID      string   `json:"desired_state_id"`
	SchemaVersion       string   `json:"schema_version"`
	Mode                string   `json:"mode"`
	Active              bool     `json:"active"`
	LocalOnly           bool     `json:"local_only"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	SourceRefs          []string `json:"source_refs,omitempty"`
	TemplateErrors      []string `json:"template_errors,omitempty"`
	UnsupportedControls []string `json:"unsupported_controls,omitempty"`
}

type postureDriftSummary struct {
	Total                  int            `json:"total"`
	WithDrift              int            `json:"with_drift"`
	MissingControlReceipts int            `json:"missing_control_receipts"`
	RollbackAvailable      int            `json:"rollback_available"`
	ByStatus               map[string]int `json:"by_status,omitempty"`
	ByMissingControl       map[string]int `json:"by_missing_control,omitempty"`
	ByDrift                map[string]int `json:"by_drift,omitempty"`
}

type postureDriftReceiptResponse struct {
	AuditID           string         `json:"audit_id"`
	NodeID            string         `json:"node_id"`
	DesiredStateID    string         `json:"desired_state_id"`
	SchemaVersion     string         `json:"schema_version,omitempty"`
	Mode              string         `json:"mode,omitempty"`
	Status            string         `json:"status"`
	Backend           string         `json:"backend,omitempty"`
	DryRun            bool           `json:"dry_run"`
	PlannedRules      int            `json:"planned_rules"`
	AppliedRules      int            `json:"applied_rules"`
	RemovedRules      int            `json:"removed_rules,omitempty"`
	MissingControls   []string       `json:"missing_controls,omitempty"`
	Drift             []string       `json:"drift,omitempty"`
	Error             string         `json:"error,omitempty"`
	SignaturePresent  bool           `json:"signature_present"`
	SignatureValid    bool           `json:"signature_valid"`
	SignatureKeyID    string         `json:"signature_key_id,omitempty"`
	ObservedAt        string         `json:"observed_at,omitempty"`
	RollbackAvailable bool           `json:"rollback_available"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CitationIDs       []string       `json:"citation_ids"`
}

type postureDriftCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
}

func (s *Server) buildPostureDriftExplain(ctx context.Context, q postureDriftExplainQuery, guardrails []string) (postureDriftExplainResponse, error) {
	if q.Limit <= 0 || q.Limit > maxListLimit {
		q.Limit = 100
	}
	resp := postureDriftExplainResponse{
		TenantID:   q.TenantID.String(),
		Since:      q.Since,
		Until:      q.Until,
		Guardrails: append([]string{"tenant_scoped", "read_only", "audit_log_citations", "proposal_only_ai"}, guardrails...),
		Summary: postureDriftSummary{
			ByStatus:         map[string]int{},
			ByMissingControl: map[string]int{},
			ByDrift:          map[string]int{},
		},
	}
	if q.NodeID != uuid.Nil {
		resp.NodeID = q.NodeID.String()
	}
	if s == nil || s.store == nil {
		return resp, nil
	}

	if q.NodeID != uuid.Nil {
		node, err := s.ensureNodeInTenant(ctx, q.TenantID, q.NodeID)
		if err != nil {
			return resp, err
		}
		resp.Desired = postureDesiredStateFromPolicy(s.compileNodeNetworkPolicy(ctx, *node, q.Until))
	}

	resourceID := ""
	if q.NodeID != uuid.Nil {
		resourceID = q.NodeID.String()
	}
	logs, _, err := s.store.ListAuditLogs(ctx, storage.AuditLogFilter{
		TenantID:     q.TenantID,
		Action:       "network_policy.receipt",
		ResourceType: "node",
		ResourceID:   resourceID,
		Since:        &q.Since,
		Until:        &q.Until,
	}, q.Limit, q.Offset)
	if err != nil {
		return resp, err
	}
	for _, log := range logs {
		item, citation := postureDriftReceiptFromAudit(log)
		resp.Receipts = append(resp.Receipts, item)
		resp.Citations = append(resp.Citations, citation)
	}
	resp.Summary = summarizePostureDriftReceipts(resp.Receipts)
	return resp, nil
}

func postureDesiredStateFromPolicy(policy *nodeNetworkPolicy) *postureDesiredStateResponse {
	if policy == nil {
		return nil
	}
	out := &postureDesiredStateResponse{
		DesiredStateID: policy.DesiredStateID,
		SchemaVersion:  policy.SchemaVersion,
		Mode:           policy.Mode,
		Active:         policy.Active,
		LocalOnly:      policy.LocalOnly,
		Reason:         policy.Reason,
		SourceRefs:     append([]string(nil), policy.SourceRefs...),
		TemplateErrors: append([]string(nil), policy.TemplateErrors...),
	}
	if policy.ExpiresAt != nil {
		out.ExpiresAt = *policy.ExpiresAt
	}
	out.UnsupportedControls = append(out.UnsupportedControls, policy.Enforcement.UnsupportedControls...)
	return out
}

func postureDriftReceiptFromAudit(log storage.AuditLog) (postureDriftReceiptResponse, postureDriftCitation) {
	citationID := citationID("audit_logs", log.ID.String())
	item := postureDriftReceiptResponse{
		AuditID:           log.ID.String(),
		DesiredStateID:    strings.TrimSpace(detailsString(log.Metadata, "desired_state_id", "")),
		SchemaVersion:     strings.TrimSpace(detailsString(log.Metadata, "schema_version", "")),
		Mode:              strings.TrimSpace(detailsString(log.Metadata, "mode", "")),
		Status:            firstNonEmptyString(detailsString(log.Metadata, "status", ""), "unknown"),
		Backend:           strings.TrimSpace(detailsString(log.Metadata, "backend", "")),
		DryRun:            boolFromMap(log.Metadata, "dry_run"),
		PlannedRules:      intFromMetadata(log.Metadata, "planned_rules"),
		AppliedRules:      intFromMetadata(log.Metadata, "applied_rules"),
		RemovedRules:      intFromMetadata(log.Metadata, "removed_rules"),
		MissingControls:   stringsFromMetadata(log.Metadata, "missing_controls"),
		Drift:             stringsFromMetadata(log.Metadata, "drift"),
		Error:             strings.TrimSpace(detailsString(log.Metadata, "error", "")),
		SignaturePresent:  boolFromMap(log.Metadata, "signature_present"),
		SignatureValid:    boolFromMap(log.Metadata, "signature_valid"),
		SignatureKeyID:    strings.TrimSpace(detailsString(log.Metadata, "signature_key_id", "")),
		ObservedAt:        strings.TrimSpace(detailsString(log.Metadata, "observed_at", "")),
		RollbackAvailable: boolFromMap(log.Metadata, "rollback_available"),
		Metadata:          log.Metadata,
		CitationIDs:       []string{citationID},
	}
	if log.ResourceID != nil {
		item.NodeID = strings.TrimSpace(*log.ResourceID)
	}
	return item, postureDriftCitation{ID: citationID, Kind: "audit_log", Table: "audit_logs", SourceRecordID: "audit_logs:" + log.ID.String()}
}

func summarizePostureDriftReceipts(receipts []postureDriftReceiptResponse) postureDriftSummary {
	out := postureDriftSummary{
		Total:            len(receipts),
		ByStatus:         map[string]int{},
		ByMissingControl: map[string]int{},
		ByDrift:          map[string]int{},
	}
	for _, receipt := range receipts {
		incrementStringCount(out.ByStatus, firstNonEmptyString(receipt.Status, "unknown"))
		if len(receipt.Drift) > 0 {
			out.WithDrift++
			for _, drift := range receipt.Drift {
				incrementStringCount(out.ByDrift, drift)
			}
		}
		if len(receipt.MissingControls) > 0 {
			out.MissingControlReceipts++
			for _, control := range receipt.MissingControls {
				incrementStringCount(out.ByMissingControl, control)
			}
		}
		if receipt.RollbackAvailable {
			out.RollbackAvailable++
		}
	}
	if len(out.ByStatus) == 0 {
		out.ByStatus = nil
	}
	if len(out.ByMissingControl) == 0 {
		out.ByMissingControl = nil
	}
	if len(out.ByDrift) == 0 {
		out.ByDrift = nil
	}
	return out
}

func intFromMetadata(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		i, _ := value.Int64()
		return int(i)
	default:
		return 0
	}
}

func stringsFromMetadata(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	switch raw := values[key].(type) {
	case []string:
		return sanitizeStringSlice(raw, 50)
	case []any:
		out := make([]string, 0, len(raw))
		for _, value := range raw {
			if text := strings.TrimSpace(detailsString(map[string]any{"value": value}, "value", "")); text != "" {
				out = append(out, text)
			}
		}
		return sanitizeStringSlice(out, 50)
	default:
		return nil
	}
}
