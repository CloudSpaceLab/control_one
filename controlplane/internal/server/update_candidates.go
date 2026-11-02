package server

import (
	"context"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/contentlock"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
	"time"
)

func (s *Server) nodeUpdateCandidates(ctx context.Context, node *storage.Node, packages []storage.NodePackage) ([]offlinebundle.UpdateCandidateView, error) {
	reader, ok := s.store.(interface {
		GetNodeUpdateCandidates(context.Context, uuid.UUID) ([]packageinventory.CandidateObservation, error)
		GetNativePackageSnapshot(context.Context, uuid.UUID) (*packageinventory.Snapshot, error)
	})
	if !ok {
		return nil, nil
	}
	release, err := s.acquireVulnerabilityPublication(ctx, false)
	if err != nil {
		return nil, err
	}
	defer release()
	releaseContent, lockErr := contentlock.Acquire(ctx, s.offlineContentRootDir(), false)
	if lockErr != nil {
		return nil, lockErr
	}
	defer releaseContent()
	s.vulnerabilityContentMu.RLock()
	defer s.vulnerabilityContentMu.RUnlock()
	observations, err := reader.GetNodeUpdateCandidates(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	snapshot, err := reader.GetNativePackageSnapshot(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	views, report, _ := offlinebundle.AssessUpdateCandidates(s.offlineContentRootDir(), nodeAssessmentInventory(*node, packages, nil), snapshot, observations, time.Now().UTC())
	if published, ok := s.store.(interface {
		CheckVulnerabilityPublication(context.Context, string) error
	}); ok {
		if err = published.CheckVulnerabilityPublication(ctx, report.FeedGeneration); err != nil {
			for i := range views {
				views[i].SecurityFixApplicable = false
				views[i].Reason = "published_advisory_generation_mismatch"
			}
		}
	}
	for i := range views {
		review := offlinebundle.ExplainUpdateCandidate(views[i], time.Now().UTC())
		views[i].Review = &review
	}
	return views, nil
}
