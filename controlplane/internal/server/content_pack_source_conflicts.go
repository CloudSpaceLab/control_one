package server

import (
	"sort"
	"strings"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

const (
	contentPackCollectionOwnerLabel        = "control_one.collection_owner"
	contentPackCollectionIdentityLabel     = "control_one.collection_identity"
	contentPackCollectionConflictPeerLabel = "control_one.collection_conflict_peers"
	contentPackCollectionOwnerNodeAgent    = "node_agent"
	contentPackCollectionOwnerOTelEdge     = "otel_edge"
	contentPackCollectionOwnerProposal     = "proposal"
	contentPackCollectionOwnerDualWrite    = "migration_dual_write"
)

func sourceHealthEvidenceWithCollectionConflicts(evidence map[string]tenantSourceHealthEvidence) map[string]tenantSourceHealthEvidence {
	if len(evidence) < 2 {
		return evidence
	}
	groups := map[string][]string{}
	for key, item := range evidence {
		if !contentPackSourceHealthIsActiveCollectionState(item.State) {
			continue
		}
		identity := contentPackSourceCollectionIdentity(item)
		if identity == "" {
			continue
		}
		groups[identity] = append(groups[identity], key)
	}
	if len(groups) == 0 {
		return evidence
	}
	out := cloneSourceHealthEvidenceMap(evidence)
	for identity, keys := range groups {
		if len(keys) < 2 {
			continue
		}
		sort.Strings(keys)
		if contentPackSourceHealthConflictGroupAllowsDualWrite(out, keys) {
			continue
		}
		owners := contentPackSourceHealthConflictOwners(out, keys)
		if len(owners) < 2 {
			continue
		}
		peerIDs := contentPackSourceHealthConflictPeerIDs(out, keys)
		for _, key := range keys {
			item := out[key]
			item.State = contentpacks.CoverageState(contentpacks.CoverageCollectionConflict)
			item.LastError = "duplicate source collection owner conflict: " + strings.Join(owners, ", ")
			if item.Labels == nil {
				item.Labels = map[string]string{}
			} else {
				item.Labels = cloneStringMapContentPack(item.Labels)
			}
			item.Labels[contentPackCollectionIdentityLabel] = identity
			item.Labels[contentPackCollectionConflictPeerLabel] = strings.Join(peerIDs, ",")
			out[key] = item
		}
	}
	return out
}

func cloneSourceHealthEvidenceMap(input map[string]tenantSourceHealthEvidence) map[string]tenantSourceHealthEvidence {
	out := make(map[string]tenantSourceHealthEvidence, len(input))
	for key, item := range input {
		item.Labels = cloneStringMapContentPack(item.Labels)
		out[key] = item
	}
	return out
}

func contentPackSourceHealthIsActiveCollectionState(state contentpacks.CoverageState) bool {
	switch contentpacks.NormalizeCoverageState(string(state)) {
	case contentpacks.CoverageState(contentpacks.CoverageConfigRendered),
		contentpacks.CoverageState(contentpacks.CoverageDeployed),
		contentpacks.CoverageState(contentpacks.CoverageCollecting),
		contentpacks.CoverageState(contentpacks.CoverageParserHealthy),
		contentpacks.CoverageState(contentpacks.CoverageParserFailed),
		contentpacks.CoverageState(contentpacks.CoverageSilent),
		contentpacks.CoverageState(contentpacks.CoverageBackpressured),
		contentpacks.CoverageState(contentpacks.CoverageCollectionConflict):
		return true
	default:
		return false
	}
}

func contentPackSourceCollectionIdentity(item tenantSourceHealthEvidence) string {
	for _, key := range []string{contentPackCollectionIdentityLabel, "collection_identity"} {
		if value := strings.TrimSpace(item.Labels[key]); value != "" {
			return value
		}
	}
	if approvalID := strings.TrimSpace(item.ApprovalID); approvalID != "" {
		return "proposal:" + approvalID
	}
	if proposalID := strings.TrimSpace(item.Labels["control_one.source_proposal_id"]); proposalID != "" {
		return "proposal:" + proposalID
	}
	if nodeID := strings.TrimSpace(item.NodeID); nodeID != "" && strings.TrimSpace(item.SourceID) != "" {
		return "node:" + nodeID + "/" + strings.TrimSpace(item.SourceID)
	}
	if strings.TrimSpace(item.SourceInstanceID) != "" {
		return "instance:" + strings.TrimSpace(item.SourceInstanceID)
	}
	return ""
}

func contentPackSourceCollectionOwner(item tenantSourceHealthEvidence) string {
	for _, key := range []string{contentPackCollectionOwnerLabel, "collection_owner"} {
		if value := strings.TrimSpace(item.Labels[key]); value != "" {
			return strings.ToLower(value)
		}
	}
	switch strings.TrimSpace(item.CollectorMode) {
	case contentpacks.CollectorNodeFileLog, contentpacks.CollectorControlOneNode:
		return contentPackCollectionOwnerNodeAgent
	case "":
		return "unknown"
	default:
		return contentPackCollectionOwnerOTelEdge
	}
}

func contentPackSourceHealthConflictGroupAllowsDualWrite(evidence map[string]tenantSourceHealthEvidence, keys []string) bool {
	for _, key := range keys {
		item := evidence[key]
		for _, label := range []string{
			"control_one.dual_write",
			"control_one.allow_duplicate_collection",
			"allow_duplicate_collection",
			"migration_dual_write",
		} {
			if strings.EqualFold(strings.TrimSpace(item.Labels[label]), "true") || strings.EqualFold(strings.TrimSpace(item.Labels[label]), "yes") {
				return true
			}
		}
		if contentPackSourceCollectionOwner(item) == contentPackCollectionOwnerDualWrite {
			return true
		}
	}
	return false
}

func contentPackSourceHealthConflictOwners(evidence map[string]tenantSourceHealthEvidence, keys []string) []string {
	seen := map[string]struct{}{}
	for _, key := range keys {
		owner := contentPackSourceCollectionOwner(evidence[key])
		if owner == "" {
			owner = "unknown"
		}
		seen[owner] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for owner := range seen {
		out = append(out, owner)
	}
	sort.Strings(out)
	return out
}

func contentPackSourceHealthConflictPeerIDs(evidence map[string]tenantSourceHealthEvidence, keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		item := evidence[key]
		out = append(out, firstNonEmptyContentPack(item.SourceInstanceID, item.CollectorID+"/"+item.SourceID, key))
	}
	sort.Strings(out)
	return out
}
