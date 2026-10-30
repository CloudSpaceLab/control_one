package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

func preservePatchEvidence(existing, requested map[string]any) map[string]any {
	labels := map[string]any{}
	for key, value := range requested {
		if !strings.HasPrefix(key, "patch.") {
			labels[key] = value
		}
	}
	for key, value := range existing {
		if strings.HasPrefix(key, "patch.") {
			labels[key] = value
		}
	}
	return labels
}

func (s *Server) updatePublishedOfflinePlans(ctx context.Context, node *storage.Node, report *patchsupport.PublishedPlanReport) (*storage.Node, error) {
	// Missing or malformed reports revoke prior discovery evidence, including
	// after an agent downgrade. Never renew old plans from an empty heartbeat.
	value := patchsupport.PublishedPlanReport{Version: 1, Status: "unavailable", Plans: []patchsupport.OfflineContent{}}
	if report != nil && report.Validate() == nil {
		value = *report
	}
	labels := map[string]any{}
	for k, v := range node.Labels {
		labels[k] = v
	}
	labels["patch.offline_plans"] = patchsupport.PublishedPlanEvidence{Report: value, ObservedAt: time.Now().UTC()}
	if err := s.store.UpdateNodeLabels(ctx, node.ID, labels); err != nil {
		return node, err
	}
	copy := *node
	copy.Labels = labels
	return &copy, nil
}

func validatePublishedOfflinePlan(node *storage.Node, policy patchPackagePolicy, now time.Time) error {
	if policy.PublishedOfflinePlanSHA256 == "" {
		return nil
	}
	if node == nil {
		return fmt.Errorf("offline plan target unavailable")
	}
	var evidence patchsupport.PublishedPlanEvidence
	body, err := json.Marshal(node.Labels["patch.offline_plans"])
	if err != nil || json.Unmarshal(body, &evidence) != nil {
		return fmt.Errorf("agent-published offline plan unavailable")
	}
	return evidence.Require(policy.PublishedOfflinePlanSHA256, policy.OfflineContent, now)
}
