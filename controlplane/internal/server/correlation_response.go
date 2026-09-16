package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

// handleCorrelationResponse converts a correlation decision into the existing
// governed block-proposal lifecycle. All enforcement therefore shares the
// same protected-address, rate-limit, receipt, expiry, rollback, and audit
// controls as an operator-created proposal.
func (s *Server) handleCorrelationResponse(ctx context.Context, rule storage.CorrelationRule, alert *storage.Alert, ev eventbus.Event) error {
	if s == nil || s.store == nil || alert == nil || rule.ResponseMode == "" || rule.ResponseMode == "alert_only" {
		return nil
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		return fmt.Errorf("block proposal store unavailable")
	}
	srcIP := correlationResponseSourceIP(ev)
	if net.ParseIP(srcIP) == nil {
		return s.auditCorrelationResponseSkipped(ctx, rule, alert, "matching event has no valid source IP")
	}
	cidr := exactCIDRForIP(srcIP)
	if protected := s.protectedIPBlockReason(ctx, rule.TenantID, cidr); protected != "" {
		return s.auditCorrelationResponseSkipped(ctx, rule, alert, "protected target: "+protected)
	}
	if status, reason := s.blockProposalSafetyViolation(ctx, rule.TenantID, ""); status != 0 {
		return s.auditCorrelationResponseSkipped(ctx, rule, alert, reason)
	}
	if query, ok := s.store.(ipBlockProposalQueryStore); ok {
		rows, _, err := query.ListIPBlocklistEntries(ctx, storage.IPBlocklistEntryFilter{TenantID: rule.TenantID, IPCIDR: cidr}, 20, 0)
		if err == nil {
			for _, row := range rows {
				switch strings.ToLower(strings.TrimSpace(row.Status)) {
				case "proposed", "approved", "canary", "dispatching", "active":
					return nil
				}
			}
		}
	}
	ttl := rule.ResponseTTLSeconds
	if !correlationResponseTTLs[ttl] {
		ttl = 3600
	}
	expiresAt := time.Now().UTC().Add(time.Duration(ttl) * time.Second)
	var targetID *uuid.UUID
	targetType := "tenant"
	if rule.ResponseScope == "affected" && ev.NodeID != nil && *ev.NodeID != uuid.Nil {
		targetType = "node"
		targetID = ev.NodeID
	}
	correlationID := ""
	if alert.DedupKey.Valid {
		correlationID = alert.DedupKey.String
	}
	reason := fmt.Sprintf("Correlation response: rule=%s; alert_id=%s; correlation_id=%s; mode=%s", rule.Name, alert.ID, correlationID, rule.ResponseMode)
	entry, err := store.CreateIPBlocklistEntry(ctx, storage.CreateIPBlocklistEntryParams{
		TenantID: rule.TenantID, IPCIDR: cidr, Scope: rule.ResponseScope,
		TargetType: targetType, TargetID: targetID, Enforcement: rule.ResponseEnforcement,
		Reason: reason, Score: correlationResponseScore(rule.Severity), ExpiresAt: &expiresAt,
	})
	if err != nil {
		return err
	}
	s.recordAudit(ctx, s.systemActor(), rule.TenantID, "correlation.response.proposal_created", "ip_blocklist_entry", entry.ID.String(), map[string]any{
		"alert_id": alert.ID.String(), "rule_id": rule.ID.String(), "correlation_id": correlationID,
		"ip_cidr": cidr, "response_mode": rule.ResponseMode, "expires_at": expiresAt.Format(time.RFC3339),
	})
	if rule.ResponseMode != "auto_temporary_block" {
		return nil
	}
	if targetID == nil {
		_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, "automatic response requires an affected node")
		return fmt.Errorf("automatic correlation response requires an affected node")
	}
	now := time.Now().UTC()
	action, err := s.recordBlockProposalEntityAction(ctx, entry, nil, now)
	if err != nil {
		_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, err.Error())
		return err
	}
	if _, err = store.SetIPBlocklistEntryEntityAction(ctx, entry.ID, action.ID); err != nil {
		return err
	}
	if _, err = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "dispatching", nil, ""); err != nil {
		return err
	}
	dispatched, err := s.dispatchBlockProposalToNode(ctx, entry, action.ID, *targetID)
	if err != nil || dispatched == 0 {
		message := "no enforcement target dispatched"
		if err != nil {
			message = err.Error()
		}
		_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, message)
		return fmt.Errorf("automatic correlation response: %s", message)
	}
	s.recordAudit(ctx, s.systemActor(), rule.TenantID, "correlation.response.auto_dispatched", "ip_blocklist_entry", entry.ID.String(), map[string]any{
		"alert_id": alert.ID.String(), "rule_id": rule.ID.String(), "node_id": targetID.String(),
		"ip_cidr": cidr, "dispatches": dispatched, "expires_at": expiresAt.Format(time.RFC3339),
	})
	return nil
}

func correlationResponseSourceIP(ev eventbus.Event) string {
	payload := map[string]any{}
	if json.Unmarshal(ev.Payload, &payload) != nil {
		return ""
	}
	for _, candidate := range []map[string]any{payload, nestedCorrelationResponseMap(payload, "details"), nestedCorrelationResponseMap(payload, "event")} {
		for _, key := range []string{"src_ip", "source_ip"} {
			if value := strings.TrimSpace(fmt.Sprint(candidate[key])); value != "" && value != "<nil>" {
				return value
			}
		}
	}
	return ""
}

func nestedCorrelationResponseMap(payload map[string]any, key string) map[string]any {
	if nested, ok := payload[key].(map[string]any); ok {
		return nested
	}
	return map[string]any{}
}

func correlationResponseScore(severity string) int {
	if strings.EqualFold(severity, "critical") {
		return 100
	}
	if strings.EqualFold(severity, "high") {
		return 80
	}
	return 50
}

func (s *Server) auditCorrelationResponseSkipped(ctx context.Context, rule storage.CorrelationRule, alert *storage.Alert, reason string) error {
	s.recordAudit(ctx, s.systemActor(), rule.TenantID, "correlation.response.skipped", "alert", alert.ID.String(), map[string]any{
		"rule_id": rule.ID.String(), "correlation_id": nullableStringValue(alert.DedupKey), "response_mode": rule.ResponseMode, "reason": reason,
	})
	return fmt.Errorf("correlation response skipped: %s", reason)
}

func nullableStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
