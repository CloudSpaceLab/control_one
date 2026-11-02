package patchgovernance

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestUrgencyEvidenceAndPolicyBoundaries(t *testing.T) {
	now := time.Now().UTC()
	p := Policy{Name: "Tenant example", Owner: "Lab operations", Mode: "recommend_only", RetentionDays: 30, Rules: []Rule{{Name: "Exploited exposed", Severity: "high", RequireKEV: true, Exposure: "exposed", Importance: "any", Priority: "urgent", DueHours: 24}, {Name: "Critical", Severity: "critical", Exposure: "any", Importance: "any", Priority: "high", DueHours: 72}}}
	in := Input{Severity: "critical", KEV: true, Exposure: "unknown", Importance: "unknown", Compatibility: "unknown", EvidenceCurrent: true, FirstSeen: now.Add(-100 * time.Hour)}
	d := Evaluate(p, in, now)
	require.Equal(t, "Critical", d.Rule)
	require.True(t, d.Overdue)
	require.False(t, d.InstallationAuthorized)
	in.Exposure = "exposed"
	d = Evaluate(p, in, now)
	require.Equal(t, "Exploited exposed", d.Rule)
	require.Equal(t, in.FirstSeen.Add(24*time.Hour), *d.DueAt)
	in.EvidenceCurrent = false
	d = Evaluate(p, in, now)
	require.Equal(t, "review_required", d.Priority)
	require.Nil(t, d.DueAt)
	in.EvidenceCurrent = true
	in.Severity = "unknown"
	d = Evaluate(p, in, now)
	require.Equal(t, "review_required", d.Priority)
	require.Contains(t, d.Reasons[0], "Severity is unknown")
	p.Mode = "automatic"
	require.ErrorContains(t, p.Validate(), "unavailable")
	require.Equal(t, "review_required", Evaluate(p, in, now).Priority)
}
func TestTrackingExceptionBounds(t *testing.T) {
	now := time.Now().UTC()
	v := Tracking{Owner: "Lab", EscalateTo: "Review queue", Exposure: "unknown", Importance: "unknown", Compatibility: "unknown", Reason: "Review"}
	require.NoError(t, v.Validate(now))
	until := now.Add(-time.Second)
	v.ExceptionUntil = &until
	require.Error(t, v.Validate(now))
	until = now.Add(91 * 24 * time.Hour)
	require.Error(t, v.Validate(now))
	until = now.Add(time.Hour)
	require.NoError(t, v.Validate(now))
}
