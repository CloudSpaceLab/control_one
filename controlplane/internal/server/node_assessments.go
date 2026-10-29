package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
)

type nodeAssessmentStore interface {
	RecordNodeAssessment(context.Context, storage.NodeAssessment) error
	GetLatestNodeAssessment(context.Context, uuid.UUID, uuid.UUID) (*storage.NodeAssessment, error)
}

type nodeAssessmentEvidence struct {
	offlinebundle.AssessmentEvidence
	InventoryCompleteness      string                            `json:"inventory_completeness"`
	InventoryLastFullSync      *time.Time                        `json:"inventory_last_full_sync,omitempty"`
	InventoryReportedHash      string                            `json:"inventory_reported_hash,omitempty"`
	InventoryReportedCount     *int                              `json:"inventory_reported_count,omitempty"`
	ApplicationDependencyCount int                               `json:"application_dependency_count"`
	FindingsUpdated            bool                              `json:"findings_updated"`
	NativeSnapshot             *packageinventory.Snapshot        `json:"native_snapshot,omitempty"`
	VerifiedFixedScopes        []offlinebundle.VerificationScope `json:"verified_fixed_scopes,omitempty"`
	InventoryRevision          *int64                            `json:"inventory_revision,omitempty"`
}

type nodeAssessmentView struct {
	*storage.NodeAssessment
	CurrentStatus  string   `json:"current_status"`
	CurrentReasons []string `json:"current_reasons"`
}

func (s *Server) latestNodeAssessment(ctx context.Context, node *storage.Node) (*nodeAssessmentView, error) {
	store, ok := s.store.(nodeAssessmentStore)
	if !ok {
		return nil, nil
	}
	a, err := store.GetLatestNodeAssessment(ctx, node.TenantID, node.ID)
	if err != nil || a == nil {
		return nil, err
	}
	view := &nodeAssessmentView{NodeAssessment: a, CurrentStatus: a.Status, CurrentReasons: []string{}}
	var evidence nodeAssessmentEvidence
	if json.Unmarshal(a.Evidence, &evidence) != nil {
		view.CurrentStatus = "unknown"
		view.CurrentReasons = append(view.CurrentReasons, "stored_evidence_invalid")
		return view, nil
	}
	if evidence.InventoryRevision != nil {
		if revisions, ok := s.store.(interface {
			GetNodeInventoryRevision(context.Context, uuid.UUID) (int64, error)
		}); ok {
			current, err := revisions.GetNodeInventoryRevision(ctx, node.ID)
			if err != nil {
				return nil, err
			}
			if current != *evidence.InventoryRevision {
				view.CurrentStatus = "unknown"
				view.CurrentReasons = append(view.CurrentReasons, "inventory_revision_changed")
			}
		}
	}
	_, _, current, _ := offlinebundle.AssessActiveVulnerabilityFeedsWithEvidence(s.offlineContentRootDir(), nil)
	if current.FeedGeneration != evidence.FeedGeneration {
		view.CurrentStatus = "unknown"
		view.CurrentReasons = append(view.CurrentReasons, "feed_generation_changed")
	}
	health := offlinebundle.ActiveVulnerabilityAssessmentStatus(s.offlineContentRootDir())
	if health.Status == "expired" || health.Status == "unavailable" {
		view.CurrentStatus = health.Status
		view.CurrentReasons = append(view.CurrentReasons, "current_feed_"+health.Status)
	}
	if time.Since(a.CompletedAt) > 24*time.Hour {
		view.CurrentStatus = "unknown"
		view.CurrentReasons = append(view.CurrentReasons, "assessment_stale")
	}
	if evidence.NativeSnapshot != nil {
		if snapshots, ok := s.store.(interface {
			GetNativePackageSnapshot(context.Context, uuid.UUID) (*packageinventory.Snapshot, error)
		}); ok {
			current, err := snapshots.GetNativePackageSnapshot(ctx, node.ID)
			if err != nil {
				return nil, err
			}
			if current == nil || current.SHA256 != evidence.NativeSnapshot.SHA256 || time.Since(current.ObservedAt) > 24*time.Hour {
				view.CurrentStatus = "unknown"
				view.CurrentReasons = append(view.CurrentReasons, "native_inventory_completeness_no_longer_current")
			}
		}
	}
	if inventoryStore, ok := s.store.(vulnerabilityFeedImportStore); ok {
		packages, err := inventoryStore.ListNodePackages(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		deps, err := inventoryStore.ListNodeAppDependencies(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		if offlinebundle.VulnerabilityInventoryGeneration(nodeAssessmentInventory(*node, packages, deps)) != a.InventoryGeneration {
			view.CurrentStatus = "unknown"
			view.CurrentReasons = append(view.CurrentReasons, "inventory_generation_changed")
		}
	}
	return view, nil
}

func nodeAssessmentInventory(node storage.Node, packages []storage.NodePackage, deps []storage.NodeAppDependency) []offlinebundle.PackageInventory {
	inventory := make([]offlinebundle.PackageInventory, 0, len(packages)+len(deps))
	for _, pkg := range packages {
		arch := ""
		if pkg.Arch != nil {
			arch = *pkg.Arch
		}
		inventory = append(inventory, offlinebundle.PackageInventory{Platform: node.OS.String, Name: pkg.Name, Version: pkg.Version, Source: pkg.Source, Arch: arch})
	}
	for _, dep := range deps {
		inventory = append(inventory, offlinebundle.PackageInventory{Platform: node.OS.String, Name: dep.Name, Version: dep.Version, Source: dep.Ecosystem, PURL: dep.PURL, CPE: dep.CPE})
	}
	return inventory
}
