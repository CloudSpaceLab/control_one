package patchsupport

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func onlineFixture(now time.Time) OnlinePlan {
	p := OnlinePlan{Version: 1, NodeID: uuid.NewString(), Mode: "direct", TransportContext: strings.Repeat("a", 64), InstalledStateSHA256: strings.Repeat("b", 64), InputsSHA256: strings.Repeat("c", 64), ManifestSHA256: strings.Repeat("d", 64), Requested: []string{"liba"}, Resolved: []ExactPackage{{Name: "liba", Version: "2"}}, Review: OfflineReview{PreparedAt: now.Add(-time.Minute), ExpiresAt: now.Add(59 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []PlanChange{{Name: "liba", InstalledVersion: "1", ProposedVersion: "2", Role: "requested"}}}}
	p.SHA256 = p.Digest()
	return p
}
func TestOnlinePlanBindsEveryApprovalField(t *testing.T) {
	now := time.Now().UTC()
	original := onlineFixture(now)
	require.NoError(t, original.Validate(now))
	for _, change := range []string{"node", "mode", "proxy", "context", "state", "inputs", "manifest", "scope", "version", "before", "expiry"} {
		t.Run(change, func(t *testing.T) {
			p := original
			p.Requested = append([]string(nil), p.Requested...)
			p.Resolved = append([]ExactPackage(nil), p.Resolved...)
			p.Review.Changes = append([]PlanChange(nil), p.Review.Changes...)
			switch change {
			case "node":
				p.NodeID = uuid.NewString()
			case "mode":
				p.Mode = "proxy"
			case "proxy":
				p.ProxyURL = "http://proxy.local:3128"
			case "context":
				p.TransportContext = strings.Repeat("e", 64)
			case "state":
				p.InstalledStateSHA256 = strings.Repeat("e", 64)
			case "inputs":
				p.InputsSHA256 = strings.Repeat("e", 64)
			case "manifest":
				p.ManifestSHA256 = strings.Repeat("e", 64)
			case "scope":
				p.Requested = []string{"other"}
			case "version":
				p.Resolved[0].Version = "3"
			case "before":
				p.Review.Changes[0].InstalledVersion = "0"
			case "expiry":
				p.Review.ExpiresAt = p.Review.ExpiresAt.Add(-time.Minute)
			}
			require.NotEqual(t, original.SHA256, p.Digest())
			require.Error(t, p.Validate(now), "altered public plan must not inherit approval identity")
		})
	}
	require.ErrorContains(t, original.Validate(original.Review.ExpiresAt), "expired")
}
func TestOnlinePlanRejectsUnsafeScopeAndTransport(t *testing.T) {
	now := time.Now().UTC()
	for _, scenario := range []string{"empty scope", "unsorted", "duplicate", "shell", "extra dependency", "missing review", "future", "long expiry", "proxy credentials", "direct proxy", "service claim", "reboot claim"} {
		t.Run(scenario, func(t *testing.T) {
			p := onlineFixture(now)
			switch scenario {
			case "empty scope":
				p.Requested = nil
			case "unsorted":
				p.Requested = []string{"zlib1g", "liba"}
			case "duplicate":
				p.Requested = []string{"liba", "liba"}
			case "shell":
				p.Requested = []string{"liba;echo"}
			case "extra dependency":
				p.Resolved = append(p.Resolved, ExactPackage{Name: "libb", Version: "3"})
			case "missing review":
				p.Review.Changes = nil
			case "future":
				p.Review.PreparedAt = now.Add(time.Minute)
			case "long expiry":
				p.Review.ExpiresAt = now.Add(time.Hour)
			case "proxy credentials":
				p.Mode = "proxy"
				p.ProxyURL = "http://user:secret@proxy.local:3128"
			case "direct proxy":
				p.ProxyURL = "http://proxy.local:3128"
			case "service claim":
				p.Review.ServiceImpact = "healthy"
			case "reboot claim":
				p.Review.Reboot = "not_required"
			}
			p.SHA256 = p.Digest()
			require.Error(t, p.Validate(now))
		})
	}
}
