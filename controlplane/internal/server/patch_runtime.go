package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

type patchRuntimeEvidence = patchsupport.Evidence

func nodePatchRuntime(node *storage.Node, now time.Time) (patchsupport.Runtime, error) {
	var evidence patchRuntimeEvidence
	if node == nil {
		return evidence.Runtime, fmt.Errorf("patch runtime evidence unavailable")
	}
	raw, err := json.Marshal(node.Labels["patch.runtime"])
	if err != nil || json.Unmarshal(raw, &evidence) != nil || evidence.ObservedAt.IsZero() || evidence.ObservedAt.After(now) || now.Sub(evidence.ObservedAt) > 5*time.Minute {
		return evidence.Runtime, fmt.Errorf("current agent patch runtime required; update the agent and wait for a heartbeat")
	}
	return evidence.Runtime, nil
}

func (s *Server) updatePatchRuntime(ctx context.Context, node *storage.Node, report *patchsupport.Runtime) (*storage.Node, error) {
	if report == nil {
		return node, nil
	}
	labels := map[string]any{}
	for k, v := range node.Labels {
		labels[k] = v
	}
	labels["patch.runtime"] = patchRuntimeEvidence{Runtime: *report, ObservedAt: time.Now().UTC()}
	if err := s.store.UpdateNodeLabels(ctx, node.ID, labels); err != nil {
		return node, err
	}
	copy := *node
	copy.Labels = labels
	return &copy, nil
}

func validateRuntimePackageScope(r patchsupport.Runtime, policy patchPackagePolicy) error {
	if policy.OnlinePlan != nil && r.OnlinePlanVersion != 1 {
		return fmt.Errorf("current exact online planning agent required")
	}
	if policy.OfflineContent != nil && policy.OfflineContent.Format == 3 && r.OfflinePlanFormat < 3 {
		return fmt.Errorf("agent does not support time-bounded format-3 plans; update it and wait for a current heartbeat")
	}
	if r.OS == "windows" && (len(policy.Allowlist) > 0 || len(policy.Denylist) > 0) {
		return fmt.Errorf("Windows package-scoped patching is unavailable; winget allowlists/exclusions are not implemented")
	}
	return nil
}
