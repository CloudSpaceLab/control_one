package main

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/firewall"
)

func TestFirewallRuleFromActionDeleteRemovesOriginalBlockShape(t *testing.T) {
	t.Parallel()

	rule := firewallRuleFromAction("firewall.rule_delete", firewallActionDetail{
		Action:    "allow",
		Direction: "in",
		Source:    "203.0.113.10",
		Tag:       "c1-original",
		Reason:    "ttl expired",
	})

	if rule.Action != firewall.ActionBlock {
		t.Fatalf("delete job rule action = %q, want original block action", rule.Action)
	}
	if rule.Source != "203.0.113.10" || rule.Tag != "c1-original" {
		t.Fatalf("unexpected rule shape: %#v", rule)
	}
}

func TestFirewallRuleFromActionAddCanStillAllow(t *testing.T) {
	t.Parallel()

	rule := firewallRuleFromAction("firewall.rule_add", firewallActionDetail{
		Action:    "allow",
		Direction: "out",
		Dest:      "198.51.100.7",
	})

	if rule.Action != firewall.ActionAllow {
		t.Fatalf("add job rule action = %q, want allow", rule.Action)
	}
	if rule.Direction != firewall.DirectionOut {
		t.Fatalf("direction = %q, want out", rule.Direction)
	}
}
