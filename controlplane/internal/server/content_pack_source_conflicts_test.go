package server

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

func TestSourceHealthEvidenceWithCollectionConflictsMarksDuplicateOwners(t *testing.T) {
	identity := "proposal:source-approval-1"
	evidence := map[string]tenantSourceHealthEvidence{
		"node/linux.auth": {
			SourceInstanceID: "node/linux.auth",
			SourceID:         "linux.auth",
			CollectorID:      "node-1",
			CollectorMode:    contentpacks.CollectorNodeFileLog,
			State:            contentpacks.CoverageState(contentpacks.CoverageCollecting),
			Labels: map[string]string{
				contentPackCollectionOwnerLabel:    contentPackCollectionOwnerNodeAgent,
				contentPackCollectionIdentityLabel: identity,
			},
		},
		"edge/linux.auth": {
			SourceInstanceID: "edge/linux.auth",
			SourceID:         "linux.auth",
			CollectorID:      "edge-1",
			CollectorMode:    contentpacks.CollectorSyslog,
			State:            contentpacks.CoverageState(contentpacks.CoverageConfigRendered),
			Labels: map[string]string{
				contentPackCollectionOwnerLabel:    contentPackCollectionOwnerOTelEdge,
				contentPackCollectionIdentityLabel: identity,
			},
		},
	}

	got := sourceHealthEvidenceWithCollectionConflicts(evidence)
	for key, item := range got {
		if item.State != contentpacks.CoverageState(contentpacks.CoverageCollectionConflict) {
			t.Fatalf("%s state = %q, want collection_conflict", key, item.State)
		}
		if item.LastError == "" {
			t.Fatalf("%s missing conflict error", key)
		}
		if item.Labels[contentPackCollectionConflictPeerLabel] == "" {
			t.Fatalf("%s missing conflict peer labels", key)
		}
	}
	if evidence["node/linux.auth"].State != contentpacks.CoverageState(contentpacks.CoverageCollecting) {
		t.Fatal("sourceHealthEvidenceWithCollectionConflicts mutated input map")
	}
}

func TestSourceHealthEvidenceWithCollectionConflictsHonorsDualWrite(t *testing.T) {
	identity := "proposal:source-approval-2"
	evidence := map[string]tenantSourceHealthEvidence{
		"node/linux.auth": {
			SourceID:      "linux.auth",
			CollectorID:   "node-1",
			CollectorMode: contentpacks.CollectorNodeFileLog,
			State:         contentpacks.CoverageState(contentpacks.CoverageCollecting),
			Labels: map[string]string{
				contentPackCollectionOwnerLabel:    contentPackCollectionOwnerNodeAgent,
				contentPackCollectionIdentityLabel: identity,
				"control_one.dual_write":           "true",
			},
		},
		"edge/linux.auth": {
			SourceID:      "linux.auth",
			CollectorID:   "edge-1",
			CollectorMode: contentpacks.CollectorSyslog,
			State:         contentpacks.CoverageState(contentpacks.CoverageConfigRendered),
			Labels: map[string]string{
				contentPackCollectionOwnerLabel:    contentPackCollectionOwnerOTelEdge,
				contentPackCollectionIdentityLabel: identity,
			},
		},
	}

	got := sourceHealthEvidenceWithCollectionConflicts(evidence)
	if got["node/linux.auth"].State == contentpacks.CoverageState(contentpacks.CoverageCollectionConflict) ||
		got["edge/linux.auth"].State == contentpacks.CoverageState(contentpacks.CoverageCollectionConflict) {
		t.Fatalf("dual-write group should not be marked as a collection conflict: %+v", got)
	}
}

func TestSourceHealthEvidenceWithCollectionConflictsIgnoresSameOwner(t *testing.T) {
	identity := "collector:edge-1/linux.auth"
	evidence := map[string]tenantSourceHealthEvidence{
		"edge/linux.auth": {
			SourceID:      "linux.auth",
			CollectorID:   "edge-1",
			CollectorMode: contentpacks.CollectorSyslog,
			State:         contentpacks.CoverageState(contentpacks.CoverageCollecting),
			Labels: map[string]string{
				contentPackCollectionOwnerLabel:    contentPackCollectionOwnerOTelEdge,
				contentPackCollectionIdentityLabel: identity,
			},
		},
		"edge/linux.auth.receiver": {
			SourceID:      "linux.auth",
			CollectorID:   "edge-1",
			ReceiverID:    "syslog/linux.auth",
			CollectorMode: contentpacks.CollectorSyslog,
			State:         contentpacks.CoverageState(contentpacks.CoverageConfigRendered),
			Labels: map[string]string{
				contentPackCollectionOwnerLabel:    contentPackCollectionOwnerOTelEdge,
				contentPackCollectionIdentityLabel: identity,
			},
		},
	}

	got := sourceHealthEvidenceWithCollectionConflicts(evidence)
	for key, item := range got {
		if item.State == contentpacks.CoverageState(contentpacks.CoverageCollectionConflict) {
			t.Fatalf("%s should not be marked as a collection conflict for same-owner duplicate evidence", key)
		}
	}
}

func TestMergeSourceHealthEvidenceKeepsSameSourceSeparatePerNode(t *testing.T) {
	evidence := map[string]tenantSourceHealthEvidence{}
	mergeSourceHealthEvidence(evidence, tenantSourceHealthEvidence{
		CollectorID: "otelcol", NodeID: "node-a", SourceID: "windows.security",
		State: contentpacks.CoverageState(contentpacks.CoverageCollecting), ApprovalID: "approved-a",
	})
	mergeSourceHealthEvidence(evidence, tenantSourceHealthEvidence{
		CollectorID: "otelcol", NodeID: "node-b", SourceID: "windows.security",
		State: contentpacks.CoverageState(contentpacks.CoverageApprovalRequired), ApprovalRequired: true,
	})

	if len(evidence) != 2 {
		t.Fatalf("source health records merged across nodes: %+v", evidence)
	}
	for _, item := range evidence {
		if item.NodeID == "node-a" && (item.ApprovalRequired || item.ApprovalID != "approved-a") {
			t.Fatalf("node-a approval evidence was overwritten: %+v", item)
		}
		if item.NodeID == "node-b" && (!item.ApprovalRequired || item.ApprovalID != "") {
			t.Fatalf("node-b approval evidence was overwritten: %+v", item)
		}
	}
}

func TestContentPackSourceRuntimeStateFromEvidenceScopesFallbackInstanceToNode(t *testing.T) {
	base := tenantSourceHealthEvidence{
		CollectorID: "otelcol", SourceID: "windows.security",
		State: contentpacks.CoverageState(contentpacks.CoverageCollecting),
	}
	first, ok := contentPackSourceRuntimeStateFromEvidence(tenantSourceHealthEvidence{
		NodeID: "node-a", CollectorID: base.CollectorID, SourceID: base.SourceID, State: base.State,
	})
	if !ok {
		t.Fatal("expected node-a runtime state")
	}
	second, ok := contentPackSourceRuntimeStateFromEvidence(tenantSourceHealthEvidence{
		NodeID: "node-b", CollectorID: base.CollectorID, SourceID: base.SourceID, State: base.State,
	})
	if !ok {
		t.Fatal("expected node-b runtime state")
	}
	if first.SourceInstanceID == second.SourceInstanceID {
		t.Fatalf("fallback runtime IDs collide across nodes: %q", first.SourceInstanceID)
	}
}
