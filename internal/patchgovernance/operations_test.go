package patchgovernance

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOperationsAutomationDraftSafety(t *testing.T) {
	now := time.Now().UTC()
	o := DefaultOperations()
	o.Owner = "Lab"
	o.Reason = "Review configuration"
	require.NoError(t, o.Validate(now))
	o.Notifications.Enabled = true
	require.ErrorContains(t, o.Validate(now), "recipients")
	o.Notifications.Recipients = []string{uuid.NewString()}
	require.NoError(t, o.Validate(now))
	end := now.Add(time.Hour)
	target := uuid.NewString()
	o.Automation = AutomationPolicy{Mode: "approved_scheduled", NodeIDs: []string{target}, CanaryNodeIDs: []string{target}, Packages: []string{"liblzma5"}, Exclusions: []string{}, WindowID: uuid.NewString(), Transport: "direct", MaxWaveSize: 1, HealthProfile: "Lab probes", ValidUntil: &end, RequireApproval: true, RequireExactPlan: true, RequireFreshAssessment: true, RequireHealth: true, RequireRebootVerification: true, StopOnFailure: true}
	require.NoError(t, o.Validate(now))
	o.Automation.Enabled = true
	require.ErrorContains(t, o.Validate(now), "qualified")
	o.Automation.Enabled = false
	o.Automation.RequireApproval = false
	require.Error(t, o.Validate(now))
	o.Automation.Mode = "automatic"
	require.NoError(t, o.Validate(now))
	o.Automation.RequireHealth = false
	require.Error(t, o.Validate(now))
	o.Automation.RequireHealth = true
	o.Automation.Exclusions = []string{"liblzma5"}
	require.Error(t, o.Validate(now))
	o.Automation.Exclusions = nil
	o.Automation.CanaryNodeIDs = []string{uuid.NewString()}
	require.ErrorContains(t, o.Validate(now), "canary")
}
