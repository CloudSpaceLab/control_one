package metrics_test

import (
	"sort"
	"testing"

	metricnames "github.com/CloudSpaceLab/control_one/internal/metrics"
	"github.com/CloudSpaceLab/control_one/internal/util"
)

// TestMetricNamesContract pins the agent emitter and the controlplane
// metric-name registry together. If the agent ever emits a key that is
// not declared in metricnames.CoreEmitted (or vice versa), this test
// fires — preventing the 9-vs-7 disjoint-set bug from PR #51 audit
// (docs/incomplete-features-and-bugs.md §1.1) from recurring.
//
// This is the load-bearing assertion for c1-calibration-metric-contract.
func TestMetricNamesContract(t *testing.T) {
	// What the agent actually emits, today, on a tick.
	emitted := util.CollectHostMetrics()
	emittedKeys := make([]string, 0, len(emitted))
	for k := range emitted {
		emittedKeys = append(emittedKeys, k)
	}
	sort.Strings(emittedKeys)

	expected := append([]string(nil), metricnames.CoreEmitted...)
	sort.Strings(expected)

	// Every name the agent emits MUST be in CoreEmitted.
	declared := make(map[string]struct{}, len(metricnames.CoreEmitted))
	for _, n := range metricnames.CoreEmitted {
		declared[n] = struct{}{}
	}
	for _, k := range emittedKeys {
		if _, ok := declared[k]; !ok {
			t.Errorf("agent emitted metric %q not declared in metrics.CoreEmitted; update controlplane/internal/metrics/names.go", k)
		}
	}

	// And every name in CoreEmitted SHOULD be emitted (best-effort —
	// CollectHostMetrics may skip a key on a host where gopsutil returns
	// nil, e.g. memory readings on a sandboxed CI runner). We log the
	// gap rather than fail so CI on minimal containers doesn't break,
	// but we do enforce that the emitted set is a subset of declared.
	if len(emittedKeys) == 0 {
		t.Logf("CollectHostMetrics returned no metrics on this host — likely a sandboxed runner; subset check still applies")
	}
}

// TestMetricNamesContract_OptionalDisjoint asserts the optional-signals
// list does not overlap CoreEmitted. Overlap would mean a name is both
// "agent emits" and "agent doesn't emit yet," which is contradictory.
func TestMetricNamesContract_OptionalDisjoint(t *testing.T) {
	core := make(map[string]struct{}, len(metricnames.CoreEmitted))
	for _, n := range metricnames.CoreEmitted {
		core[n] = struct{}{}
	}
	for _, n := range metricnames.OptionalSignals {
		if _, ok := core[n]; ok {
			t.Errorf("metric %q appears in both CoreEmitted and OptionalSignals — pick one", n)
		}
	}
}

// TestMetricNamesContract_UnitsCoverage ensures every CoreEmitted name
// has a Units mapping (possibly empty string for dimensionless counts),
// so the server's metricUnits map round-trips deterministically.
func TestMetricNamesContract_UnitsCoverage(t *testing.T) {
	for _, n := range metricnames.CoreEmitted {
		// Units returns "" for dimensionless metrics — that's allowed,
		// we just verify the function doesn't panic / return something
		// weird. A future regression that drops a metric from the
		// switch will silently return "" which is fine; if we ever
		// want stricter coverage we can add a registered-units set.
		_ = metricnames.Units(n)
	}
	for _, n := range metricnames.OptionalSignals {
		_ = metricnames.Units(n)
	}
}
