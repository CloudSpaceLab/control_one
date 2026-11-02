package patchgovernance

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAutomationExactScopeAndActivation(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour)
	node := uuid.NewString()
	window := uuid.NewString()
	a := AutomationPolicy{Enabled: true, Mode: "automatic", NodeIDs: []string{node}, CanaryNodeIDs: []string{node}, Packages: []string{"liba"}, Exclusions: []string{"kernel"}, WindowID: window, Transport: "direct", HealthProfile: AutomationHealthProfile, ObservationMinutes: 60, ValidUntil: &end}
	require.NoError(t, a.ValidateActivation())
	require.NoError(t, MatchAutomationApproval(a, node, "direct", window, []string{"liba"}, []string{"liba", "dependency"}, now))
	for _, change := range []func(*AutomationPolicy){func(p *AutomationPolicy) { p.Enabled = false }, func(p *AutomationPolicy) { p.ValidUntil = &now }, func(p *AutomationPolicy) { p.Transport = "airgapped" }, func(p *AutomationPolicy) { p.ObservationMinutes = 0 }, func(p *AutomationPolicy) { p.HealthProfile = "unqualified" }, func(p *AutomationPolicy) { p.NodeIDs = []string{uuid.NewString()} }, func(p *AutomationPolicy) { p.WindowID = uuid.NewString() }, func(p *AutomationPolicy) { p.Exclusions = []string{"dependency"} }} {
		copy := a
		change(&copy)
		require.Error(t, MatchAutomationApproval(copy, node, "direct", window, []string{"liba"}, []string{"liba", "dependency"}, now))
	}
	require.Error(t, MatchAutomationApproval(a, node, "direct", window, []string{"outside"}, nil, now))
}
