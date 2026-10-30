package patchsupport

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPublishedOfflineEvidence(t *testing.T) {
	plan := OfflineContent{Format: 2, ManifestSHA256: strings.Repeat("a", 64), InstalledStatusSHA256: strings.Repeat("b", 64), KeyringSHA256: strings.Repeat("c", 64), Suite: "noble-updates", Component: "main", Architecture: "amd64", Requested: []ExactPackage{{Name: "libudev1", Version: "1.2"}}, Resolved: []ExactPackage{{Name: "libudev1", Version: "1.2"}}}
	now := time.Now().UTC()
	ready := PublishedPlanEvidence{Report: PublishedPlanReport{Version: 1, Status: "ready", Plans: []OfflineContent{plan}}, ObservedAt: now}
	require.NoError(t, ready.Require(plan.ManifestSHA256, &plan, now))
	for _, scenario := range []string{"stale", "future", "empty", "unavailable", "changed", "oversized", "version", "nil"} {
		t.Run(scenario, func(t *testing.T) {
			e := ready
			e.Report.Plans = append([]OfflineContent(nil), ready.Report.Plans...)
			content := &plan
			switch scenario {
			case "stale":
				e.ObservedAt = now.Add(-5*time.Minute - time.Nanosecond)
			case "future":
				e.ObservedAt = now.Add(time.Second)
			case "empty":
				e.Report.Status = "empty"
				e.Report.Plans = nil
			case "unavailable":
				e.Report.Status = "unavailable"
				e.Report.Plans = nil
			case "changed":
				e.Report.Plans[0].ManifestSHA256 = strings.Repeat("d", 64)
			case "oversized":
				e.Report.Plans = append(e.Report.Plans, plan)
			case "version":
				e.Report.Version = 2
			case "nil":
				content = nil
			}
			require.Error(t, e.Require(plan.ManifestSHA256, content, now))
		})
	}
}
