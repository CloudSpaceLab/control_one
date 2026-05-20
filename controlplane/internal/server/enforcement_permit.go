package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type firewallCompletionReceipt struct {
	Contract           string `json:"contract"`
	JobID              string `json:"job_id"`
	NodeFirewallRuleID string `json:"node_firewall_rule_id"`
	EntityActionID     string `json:"entity_action_id"`
	Action             string `json:"action"`
	Status             string `json:"status"`
	Backend            string `json:"backend"`
	RuleFingerprint    string `json:"rule_fingerprint"`
	Source             string `json:"source,omitempty"`
	Dest               string `json:"dest,omitempty"`
	Port               int    `json:"port,omitempty"`
	Protocol           string `json:"protocol,omitempty"`
	Direction          string `json:"direction"`
	RuleAction         string `json:"rule_action"`
	Tag                string `json:"tag"`
	ObservedAt         string `json:"observed_at"`
}

func validateFirewallReceipt(rule *storage.NodeFirewallRule, jobID uuid.UUID, action string, metadata map[string]any) error {
	if rule == nil {
		return errors.New("firewall rule unavailable")
	}
	var receipt firewallCompletionReceipt
	raw, ok := metadata["receipt"]
	if !ok || raw == nil {
		return errors.New("required firewall receipt missing")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal firewall receipt: %w", err)
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		return fmt.Errorf("decode firewall receipt: %w", err)
	}
	if receipt.Contract != "firewall.receipt.v1" {
		return errors.New("invalid firewall receipt contract")
	}
	if !strings.EqualFold(receipt.JobID, jobID.String()) {
		return errors.New("firewall receipt job_id mismatch")
	}
	if !strings.EqualFold(receipt.NodeFirewallRuleID, rule.ID.String()) {
		return errors.New("firewall receipt rule_id mismatch")
	}
	if !strings.EqualFold(receipt.EntityActionID, rule.EntityActionID.String()) {
		return errors.New("firewall receipt entity_action_id mismatch")
	}
	if receipt.Action != action {
		return errors.New("firewall receipt action mismatch")
	}
	if receipt.Status != "succeeded" {
		return errors.New("firewall receipt status must be succeeded")
	}
	if strings.TrimSpace(receipt.Backend) == "" {
		return errors.New("firewall receipt backend required")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(receipt.ObservedAt)); err != nil {
		return errors.New("firewall receipt observed_at must be RFC3339")
	}
	expected := firewallReceiptShapeFromRule(rule, action)
	if receipt.Source != expected.Source || receipt.Dest != expected.Dest ||
		receipt.Port != expected.Port || receipt.Protocol != expected.Protocol ||
		receipt.Direction != expected.Direction || receipt.RuleAction != expected.RuleAction ||
		receipt.Tag != expected.Tag {
		return errors.New("firewall receipt rule shape mismatch")
	}
	if receipt.RuleFingerprint != firewallReceiptFingerprint(expected) {
		return errors.New("firewall receipt fingerprint mismatch")
	}
	return nil
}

func firewallReceiptShapeFromRule(rule *storage.NodeFirewallRule, action string) firewallCompletionReceipt {
	source := stringFromPtr(rule.Source)
	dest := stringFromPtr(rule.Dest)
	port := intFromPtr(rule.Port)
	protocol := stringFromPtr(rule.Protocol)
	ruleAction := "block"
	if action != JobTypeFirewallRuleDelete && strings.EqualFold(rule.Action, "allow") {
		ruleAction = "allow"
	}
	direction := rule.Direction
	if strings.TrimSpace(direction) == "" {
		direction = "in"
	}
	return firewallCompletionReceipt{
		Source:     source,
		Dest:       dest,
		Port:       port,
		Protocol:   protocol,
		Direction:  direction,
		RuleAction: ruleAction,
		Tag:        rule.Tag,
	}
}

func firewallReceiptFingerprint(receipt firewallCompletionReceipt) string {
	payload := struct {
		Source    string `json:"source,omitempty"`
		Dest      string `json:"dest,omitempty"`
		Port      int    `json:"port,omitempty"`
		Protocol  string `json:"protocol,omitempty"`
		Direction string `json:"direction"`
		Action    string `json:"action"`
		Tag       string `json:"tag"`
	}{
		Source:    receipt.Source,
		Dest:      receipt.Dest,
		Port:      receipt.Port,
		Protocol:  receipt.Protocol,
		Direction: receipt.Direction,
		Action:    receipt.RuleAction,
		Tag:       receipt.Tag,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intFromPtr(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
