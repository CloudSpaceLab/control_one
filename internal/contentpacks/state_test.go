package contentpacks

import (
	"testing"
	"time"
)

func TestCoverageTransitionHappyPath(t *testing.T) {
	path := []string{
		CoverageDiscovered,
		CoverageProposed,
		CoverageApprovalRequired,
		CoverageApproved,
		CoverageConfigRendered,
		CoverageDeployed,
		CoverageCollecting,
		CoverageParserHealthy,
	}
	for i := 1; i < len(path); i++ {
		from, to := path[i-1], path[i]
		if !CanTransitionCoverage(from, to) {
			t.Fatalf("CanTransitionCoverage(%q, %q) = false", from, to)
		}
		if err := ValidateCoverageTransition(from, to); err != nil {
			t.Fatalf("ValidateCoverageTransition(%q, %q) error = %v", from, to, err)
		}
	}
}

func TestCoverageTransitionRejectsSkippingRuntimeProof(t *testing.T) {
	if CanTransitionCoverage(CoverageProposed, CoverageParserHealthy) {
		t.Fatal("proposed source should not jump directly to parser_healthy")
	}
	if err := ValidateCoverageTransition(CoverageApproved, CoverageParserHealthy); err == nil {
		t.Fatal("approved source should not jump directly to parser_healthy")
	}
}

func TestCoverageStateClassifiers(t *testing.T) {
	if !CoverageStateIsHealthy(CoverageParserHealthy) {
		t.Fatal("parser_healthy should be healthy")
	}
	if !CoverageStateIsDegraded(CoverageParserFailed) {
		t.Fatal("parser_failed should be degraded")
	}
	if !CoverageStateRequiresCollector(CoverageDeployed) {
		t.Fatal("deployed should require collector identity")
	}
	if !CoverageStateRequiresParser(CoverageCollecting) {
		t.Fatal("collecting should require parser identity")
	}
	if !CoverageStateIsDegraded(CoverageCollectionConflict) {
		t.Fatal("collection_conflict should be degraded")
	}
	if !CoverageStateRequiresCollector(CoverageCollectionConflict) {
		t.Fatal("collection_conflict should require collector identity")
	}
	if CoverageStateRequiresParser(CoverageCollectionConflict) {
		t.Fatal("collection_conflict should not require parser identity")
	}
	if CoverageStateRequiresApprovalRef(CoverageCollectionConflict) {
		t.Fatal("collection_conflict should not require approval reference")
	}
}

func TestNewSourceRuntimeStateDefaultsFromManifestAndSource(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[0], "", now)
	if state.SourceInstanceID != "controlone.nginx.nginx.access" {
		t.Fatalf("SourceInstanceID = %q", state.SourceInstanceID)
	}
	if state.CoverageState != CoverageState(CoverageDiscovered) {
		t.Fatalf("CoverageState = %q", state.CoverageState)
	}
	if state.CollectorMode != CollectorNodeFileLog {
		t.Fatalf("CollectorMode = %q", state.CollectorMode)
	}
	if state.ParserID != "nginx.access.combined" {
		t.Fatalf("ParserID = %q", state.ParserID)
	}
	if !state.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", state.UpdatedAt, now)
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSourceRuntimeStateValidateRequiresApprovalForSensitiveActiveSource(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[1], "nginx.error.node1", time.Now())
	state.CoverageState = CoverageState(CoverageCollecting)
	state.CollectorID = "collector.node1"
	err := state.Validate()
	assertIssue(t, err, "approval_id")
}

func TestSourceRuntimeStateValidateRequiresCollectorAndParserForActiveStates(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[0], "nginx.access.node1", time.Now())
	state.CoverageState = CoverageState(CoverageCollecting)
	state.CollectorID = ""
	state.ParserID = ""
	err := state.Validate()
	assertIssue(t, err, "collector_id")
	assertIssue(t, err, "parser_id")
}

func TestSourceRuntimeStateValidateCollectionConflictNeedsCollectorAndErrorOnly(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[0], "nginx.access.node1", time.Now())
	state.CoverageState = CoverageState(CoverageCollectionConflict)
	state.CollectorID = ""
	state.ParserID = ""
	state.ApprovalRequired = true
	state.ApprovalID = ""
	state.LastError = ""
	err := state.Validate()
	assertIssue(t, err, "collector_id")
	assertIssue(t, err, "last_error")

	state.CollectorID = "collector.node1"
	state.LastError = "duplicate collection owners"
	if err := state.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSourceRuntimeStateValidateRequiresParserHealthEvidence(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[0], "nginx.access.node1", time.Now())
	state.CoverageState = CoverageState(CoverageParserHealthy)
	state.CollectorID = "collector.node1"
	err := state.Validate()
	assertIssue(t, err, "last_parsed_at")
}

func TestSourceRuntimeStateTransitionUpdatesStateAndTimestamp(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[0], "nginx.access.node1", time.Now())
	at := time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC)
	next, err := state.Transition(CoverageProposed, at)
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if next.CoverageState != CoverageState(CoverageProposed) {
		t.Fatalf("CoverageState = %q", next.CoverageState)
	}
	if !next.UpdatedAt.Equal(at) {
		t.Fatalf("UpdatedAt = %s, want %s", next.UpdatedAt, at)
	}
	if state.CoverageState != CoverageState(CoverageDiscovered) {
		t.Fatalf("original state mutated to %q", state.CoverageState)
	}
}

func TestSourceRuntimeMetricsRejectImpossibleCounts(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	state := NewSourceRuntimeState(*manifest, manifest.Sources[0], "nginx.access.node1", time.Now())
	state.Metrics.EventsReceived = 10
	state.Metrics.EventsParsed = 11
	err := state.Validate()
	assertIssue(t, err, "metrics.events_parsed")
}
