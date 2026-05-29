package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

type coverageMatrixResponse struct {
	CatalogVersion string                     `json:"catalog_version"`
	Scope          string                     `json:"scope"`
	TenantID       string                     `json:"tenant_id,omitempty"`
	GeneratedAt    string                     `json:"generated_at,omitempty"`
	Domains        []coverageDomainDefinition `json:"domains"`
	Legend         coverageLegend             `json:"legend"`
	Matrix         []coverageMatrixRow        `json:"matrix"`
}

type coverageExplainResponse struct {
	CatalogVersion string                     `json:"catalog_version"`
	Scope          string                     `json:"scope"`
	TenantID       string                     `json:"tenant_id,omitempty"`
	Domains        []coverageDomainDefinition `json:"domains"`
	Legend         coverageLegend             `json:"legend"`
	Explanations   []coverageExplanation      `json:"explanations"`
}

func (s *Server) handleCoverageSubroutes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/coverage/matrix":
		s.handleCoverageMatrix(w, r)
	case "/api/v1/coverage/explain":
		s.handleCoverageExplain(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCoverageMatrix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.coverageTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	resp := newCoverageMatrixResponse(tenantID)
	if tenantID != uuid.Nil {
		now := time.Now().UTC()
		var live bool
		resp.Matrix, live = appendTenantCoverageOverlays(r.Context(), s.store, tenantID, resp.Matrix, now)
		if live {
			resp.GeneratedAt = now.Format(time.RFC3339)
		}
	}
	resp.Matrix = filterCoverageMatrixByDomain(resp.Matrix, r.URL.Query().Get("domain"))
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCoverageExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.coverageTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCoverageExplainResponse(tenantID))
}

func (s *Server) coverageTenantFromQuery(w http.ResponseWriter, r *http.Request, principal *auth.Principal) (uuid.UUID, bool) {
	tenantID, ok := parseTenantQuery(w, r)
	if !ok || tenantID == uuid.Nil {
		return tenantID, ok
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
		return uuid.Nil, false
	}
	return tenantID, true
}

func newCoverageMatrixResponse(tenantID uuid.UUID) coverageMatrixResponse {
	return coverageMatrixResponse{
		CatalogVersion: coverageCatalogVersion,
		Scope:          coverageScope(tenantID),
		TenantID:       coverageTenantIDString(tenantID),
		Domains:        cloneCoverageDomains(),
		Legend:         buildCoverageLegend(),
		Matrix:         cloneCoverageMatrix(),
	}
}

type coverageNodeLister interface {
	ListNodes(context.Context, uuid.UUID, string, int, int) ([]storage.Node, int, error)
}

type coverageEdgeCollectorLister interface {
	ListContentPackEdgeCollectors(context.Context, uuid.UUID, int, int) ([]storage.ContentPackEdgeCollector, int, error)
}

type coverageSourceProposalLister interface {
	ListContentPackSourceProposals(context.Context, uuid.UUID, int, int) ([]storage.ContentPackSourceProposalRecord, int, error)
}

const coverageHeartbeatFreshnessWindow = 5 * time.Minute

func appendTenantCoverageOverlays(ctx context.Context, store any, tenantID uuid.UUID, rows []coverageMatrixRow, now time.Time) ([]coverageMatrixRow, bool) {
	if tenantID == uuid.Nil || store == nil {
		return rows, false
	}
	live := false
	nodeStore, ok := store.(coverageNodeLister)
	if ok {
		nodes, total, err := nodeStore.ListNodes(ctx, tenantID, "", 500, 0)
		if err != nil {
			row := tenantHeartbeatCoverageRow(coverageStateStale, []string{"node heartbeat inventory unavailable"}, []string{err.Error()})
			rows = append(rows, row)
		} else {
			rows = append(rows, tenantHeartbeatCoverageFromNodes(nodes, total, now))
		}
		live = true
	}
	if snapshotStore, ok := store.(contentPackRegistrySnapshotReader); ok {
		record, err := snapshotStore.ActiveContentPackRegistrySnapshot(ctx, tenantID)
		if err != nil {
			rows = append(rows, tenantContentPackCoverageRow(coverageStateStale, []string{"content pack registry snapshot unavailable"}, []string{err.Error()}))
		} else {
			rows = append(rows, tenantContentPackCoverageFromSnapshot(record))
		}
		live = true
	}
	if proposalStore, ok := store.(coverageSourceProposalLister); ok {
		if summaryStore, ok := store.(contentPackSourceProposalSummaryStore); ok && summaryStore != nil {
			summary, err := summaryStore.ContentPackSourceProposalSummaryFiltered(ctx, tenantID, storage.ContentPackSourceProposalFilter{})
			if err != nil {
				rows = append(rows, tenantSourceProposalCoverageRow(coverageStateStale, []string{"source proposal inventory unavailable"}, []string{err.Error()}))
			} else {
				rows = append(rows, tenantSourceProposalCoverageFromSummary(summary))
			}
		} else {
			proposals, total, err := proposalStore.ListContentPackSourceProposals(ctx, tenantID, 500, 0)
			if err != nil {
				rows = append(rows, tenantSourceProposalCoverageRow(coverageStateStale, []string{"source proposal inventory unavailable"}, []string{err.Error()}))
			} else {
				rows = append(rows, tenantSourceProposalCoverageFromRows(proposals, total))
			}
		}
		live = true
	}
	sourceRuntimeRowsAppended := false
	if sourceStore, ok := store.(contentPackSourceRuntimeStateStore); ok {
		sourceRows, total, err := sourceStore.ListContentPackSourceRuntimeStates(ctx, tenantID, 500, 0)
		if err != nil {
			rows = append(rows, tenantSourceHealthCoverageRow(coverageStateStale, []string{"source runtime state unavailable"}, []string{err.Error()}))
		} else if len(sourceRows) > 0 || total > 0 {
			rows = append(rows, tenantSourceHealthCoverageFromRuntimeStates(sourceRows, total, now))
			sourceRuntimeRowsAppended = true
		}
		live = true
	}
	if collectorStore, ok := store.(coverageEdgeCollectorLister); ok {
		collectors, total, err := collectorStore.ListContentPackEdgeCollectors(ctx, tenantID, 500, 0)
		if err != nil {
			rows = append(rows, tenantEdgeCollectorCoverageRow(coverageStateStale, []string{"edge collector inventory unavailable"}, []string{err.Error()}))
		} else {
			rows = append(rows, tenantEdgeCollectorCoverageFromCollectors(collectors, total, now))
			if !sourceRuntimeRowsAppended {
				rows = append(rows, tenantSourceHealthCoverageFromCollectors(collectors, total, now))
			}
		}
		live = true
	}
	return rows, live
}

func tenantHeartbeatCoverageFromNodes(nodes []storage.Node, total int, now time.Time) coverageMatrixRow {
	if total == 0 {
		return tenantHeartbeatCoverageRow(
			coverageStateNotApplicable,
			[]string{"no enrolled nodes for this tenant"},
			[]string{"enroll at least one node before heartbeat freshness can be assessed"},
		)
	}
	var fresh, stale, missing int
	var newest *time.Time
	for _, node := range nodes {
		if node.LastSeenAt == nil {
			missing++
			continue
		}
		lastSeen := node.LastSeenAt.UTC()
		if newest == nil || lastSeen.After(*newest) {
			copyTime := lastSeen
			newest = &copyTime
		}
		if now.Sub(lastSeen) <= coverageHeartbeatFreshnessWindow {
			fresh++
		} else {
			stale++
		}
	}
	signals := []string{
		fmt.Sprintf("nodes_total=%d", total),
		fmt.Sprintf("nodes_sampled=%d", len(nodes)),
		fmt.Sprintf("fresh=%d", fresh),
		fmt.Sprintf("stale=%d", stale),
		fmt.Sprintf("missing=%d", missing),
	}
	if newest != nil {
		signals = append(signals, "last_seen_at="+newest.UTC().Format(time.RFC3339))
	}
	if total > len(nodes) {
		signals = append(signals, "node_count_truncated=true")
	}
	if stale > 0 || missing > 0 || total > len(nodes) {
		gaps := []string{}
		if stale > 0 {
			gaps = append(gaps, fmt.Sprintf("%d nodes have heartbeats older than %s", stale, coverageHeartbeatFreshnessWindow))
		}
		if missing > 0 {
			gaps = append(gaps, fmt.Sprintf("%d nodes have no heartbeat timestamp", missing))
		}
		if total > len(nodes) {
			gaps = append(gaps, "tenant has more than 500 nodes; heartbeat overlay is sampled until pagination is expanded")
		}
		return tenantHeartbeatCoverageRow(coverageStateStale, signals, gaps)
	}
	return tenantHeartbeatCoverageRow(coverageStateSupported, signals, nil)
}

func tenantHeartbeatCoverageRow(state coverageSupportState, signals []string, gaps []string) coverageMatrixRow {
	return coverageMatrixRow{
		Domain:  "telemetry",
		Title:   "Tenant heartbeat freshness",
		State:   state,
		Quality: []coverageQualityState{coverageQualityProductionTested},
		Signals: append([]string{
			"nodes.last_seen_at",
			"freshness_window=5m",
		}, signals...),
		Evidence: []string{
			"controlplane/internal/storage/nodes.go",
			"controlplane/internal/server/heartbeat.go",
		},
		Gaps: gaps,
	}
}

func tenantContentPackCoverageFromSnapshot(record *storage.ContentPackRegistrySnapshotRecord) coverageMatrixRow {
	if record == nil {
		return tenantContentPackCoverageRow(
			coverageStateRawOnly,
			[]string{"active_content_pack_snapshot=false"},
			[]string{"no active SIEM content-pack registry snapshot exists for this tenant"},
		)
	}
	var enabled, quarantined, disabled, sources, parsers, detections, samples int
	for _, pack := range record.Snapshot.Packs {
		sources += len(pack.Manifest.Sources)
		parsers += pack.ParserCount
		detections += pack.DetectionCount
		samples += pack.SampleCount
		switch pack.Status {
		case contentpacks.PackStatusEnabled:
			enabled++
		case contentpacks.PackStatusQuarantined:
			quarantined++
		case contentpacks.PackStatusDisabled, contentpacks.PackStatusDeprecated:
			disabled++
		}
	}
	signals := []string{
		"active_content_pack_snapshot=true",
		fmt.Sprintf("snapshot_id=%s", record.ID),
		fmt.Sprintf("snapshot_source=%s", record.Source),
		fmt.Sprintf("snapshot_created_at=%s", record.CreatedAt.UTC().Format(time.RFC3339)),
		fmt.Sprintf("packs_total=%d", len(record.Snapshot.Packs)),
		fmt.Sprintf("packs_enabled=%d", enabled),
		fmt.Sprintf("packs_quarantined=%d", quarantined),
		fmt.Sprintf("packs_disabled=%d", disabled),
		fmt.Sprintf("sources_declared=%d", sources),
		fmt.Sprintf("parsers_declared=%d", parsers),
		fmt.Sprintf("detections_declared=%d", detections),
		fmt.Sprintf("samples_declared=%d", samples),
	}
	gaps := []string{
		"content-pack enablement proves parser/content availability, not live source collection or parser health",
	}
	state := coverageStatePartial
	if len(record.Snapshot.Packs) == 0 || enabled == 0 {
		state = coverageStateRawOnly
		gaps = append(gaps, "no enabled SIEM content packs are active for this tenant")
	}
	if quarantined > 0 {
		if enabled == 0 {
			state = coverageStateException
		}
		gaps = append(gaps, fmt.Sprintf("%d SIEM content pack(s) are quarantined", quarantined))
	}
	return tenantContentPackCoverageRow(state, signals, gaps)
}

func tenantSourceProposalCoverageFromRows(rows []storage.ContentPackSourceProposalRecord, total int) coverageMatrixRow {
	if total == 0 {
		return tenantSourceProposalCoverageRow(
			coverageStateRawOnly,
			[]string{"source_proposals_total=0"},
			[]string{"no durable SIEM source proposals exist for this tenant"},
		)
	}
	var proposed, autoEligible, approvalRequired, approved, rejected, privacyBlocked, stale, unknown int
	var newest *time.Time
	for _, row := range rows {
		switch row.Status {
		case storage.ContentPackSourceProposalStatusProposed:
			proposed++
		case storage.ContentPackSourceProposalStatusAutoEligible:
			autoEligible++
		case storage.ContentPackSourceProposalStatusApprovalRequired:
			approvalRequired++
		case storage.ContentPackSourceProposalStatusApproved:
			approved++
		case storage.ContentPackSourceProposalStatusRejected:
			rejected++
		case storage.ContentPackSourceProposalStatusPrivacyBlocked:
			privacyBlocked++
		case storage.ContentPackSourceProposalStatusStale:
			stale++
		default:
			unknown++
		}
		newest = newestTime(newest, &row.LastSeenAt)
	}
	signals := []string{
		fmt.Sprintf("source_proposals_total=%d", total),
		fmt.Sprintf("source_proposals_sampled=%d", len(rows)),
		fmt.Sprintf("proposed=%d", proposed),
		fmt.Sprintf("auto_eligible=%d", autoEligible),
		fmt.Sprintf("approval_required=%d", approvalRequired),
		fmt.Sprintf("approved=%d", approved),
		fmt.Sprintf("rejected=%d", rejected),
		fmt.Sprintf("privacy_blocked=%d", privacyBlocked),
		fmt.Sprintf("stale=%d", stale),
		fmt.Sprintf("unknown=%d", unknown),
	}
	if newest != nil {
		signals = append(signals, "last_proposal_seen_at="+newest.UTC().Format(time.RFC3339))
	}
	if total > len(rows) {
		signals = append(signals, "source_proposal_count_truncated=true")
	}
	gaps := []string{}
	if proposed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are awaiting policy/operator triage", proposed))
	}
	if autoEligible > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are auto-eligible but not yet proven deployed/healthy", autoEligible))
	}
	if approvalRequired > 0 {
		gaps = append(gaps, fmt.Sprintf("%d high-risk or sensitive source proposal(s) require explicit approval", approvalRequired))
	}
	if approved > 0 {
		gaps = append(gaps, fmt.Sprintf("%d approved source proposal(s) still need config render/deploy/runtime health proof", approved))
	}
	if rejected > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) were rejected by an operator", rejected))
	}
	if privacyBlocked > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are privacy-blocked by policy", privacyBlocked))
	}
	if stale > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are stale", stale))
	}
	if unknown > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) have an unrecognized status", unknown))
	}
	if total > len(rows) {
		gaps = append(gaps, "tenant has more than 500 source proposals; overlay is sampled until pagination is expanded")
	}
	state := coverageStatePartial
	if stale == total {
		state = coverageStateStale
	} else if proposed+autoEligible+approvalRequired+approved+stale+unknown == 0 && rejected+privacyBlocked > 0 {
		state = coverageStateException
	}
	return tenantSourceProposalCoverageRow(state, signals, gaps)
}

func tenantSourceProposalCoverageFromSummary(summary storage.ContentPackSourceProposalSummary) coverageMatrixRow {
	total := summary.Total
	if total == 0 {
		return tenantSourceProposalCoverageRow(
			coverageStateRawOnly,
			[]string{"source_proposals_total=0", "source_proposals_summary=true"},
			[]string{"no durable SIEM source proposals exist for this tenant"},
		)
	}
	byStatus := summary.ByStatus
	proposed := byStatus[storage.ContentPackSourceProposalStatusProposed]
	autoEligible := byStatus[storage.ContentPackSourceProposalStatusAutoEligible]
	approvalRequired := byStatus[storage.ContentPackSourceProposalStatusApprovalRequired]
	approved := byStatus[storage.ContentPackSourceProposalStatusApproved]
	rejected := byStatus[storage.ContentPackSourceProposalStatusRejected]
	privacyBlocked := byStatus[storage.ContentPackSourceProposalStatusPrivacyBlocked]
	stale := byStatus[storage.ContentPackSourceProposalStatusStale]
	known := proposed + autoEligible + approvalRequired + approved + rejected + privacyBlocked + stale
	unknown := total - known
	if unknown < 0 {
		unknown = 0
	}
	signals := []string{
		"source_proposals_summary=true",
		fmt.Sprintf("source_proposals_total=%d", total),
		fmt.Sprintf("proposed=%d", proposed),
		fmt.Sprintf("auto_eligible=%d", autoEligible),
		fmt.Sprintf("approval_required=%d", approvalRequired),
		fmt.Sprintf("approved=%d", approved),
		fmt.Sprintf("rejected=%d", rejected),
		fmt.Sprintf("privacy_blocked=%d", privacyBlocked),
		fmt.Sprintf("stale=%d", stale),
		fmt.Sprintf("unknown=%d", unknown),
	}
	return tenantSourceProposalCoverageFromCounts(total, proposed, autoEligible, approvalRequired, approved, rejected, privacyBlocked, stale, unknown, signals)
}

func tenantSourceProposalCoverageFromCounts(total, proposed, autoEligible, approvalRequired, approved, rejected, privacyBlocked, stale, unknown int, signals []string) coverageMatrixRow {
	gaps := []string{}
	if proposed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are awaiting policy/operator triage", proposed))
	}
	if autoEligible > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are auto-eligible but not yet proven deployed/healthy", autoEligible))
	}
	if approvalRequired > 0 {
		gaps = append(gaps, fmt.Sprintf("%d high-risk or sensitive source proposal(s) require explicit approval", approvalRequired))
	}
	if approved > 0 {
		gaps = append(gaps, fmt.Sprintf("%d approved source proposal(s) still need config render/deploy/runtime health proof", approved))
	}
	if rejected > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) were rejected by an operator", rejected))
	}
	if privacyBlocked > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are privacy-blocked by policy", privacyBlocked))
	}
	if stale > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) are stale", stale))
	}
	if unknown > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source proposal(s) have an unrecognized status", unknown))
	}
	state := coverageStatePartial
	if stale == total {
		state = coverageStateStale
	} else if proposed+autoEligible+approvalRequired+approved+stale+unknown == 0 && rejected+privacyBlocked > 0 {
		state = coverageStateException
	}
	return tenantSourceProposalCoverageRow(state, signals, gaps)
}

func tenantSourceProposalCoverageRow(state coverageSupportState, signals []string, gaps []string) coverageMatrixRow {
	return coverageMatrixRow{
		Domain:  "parser",
		Title:   "Tenant SIEM source proposals",
		State:   state,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: append([]string{
			"content_pack_source_proposals",
			"pre_collection_truth=true",
		}, signals...),
		Evidence: []string{
			"internal/connectordiscovery",
			"controlplane/internal/storage/content_pack_source_proposal.go",
			"controlplane/internal/server/knowledge_graph.go",
			"controlplane/internal/server/content_packs.go",
		},
		Gaps: gaps,
	}
}

func tenantContentPackCoverageRow(state coverageSupportState, signals []string, gaps []string) coverageMatrixRow {
	return coverageMatrixRow{
		Domain:  "parser",
		Title:   "Tenant SIEM content-pack registry",
		State:   state,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: signals,
		Evidence: []string{
			"internal/contentpacks",
			"controlplane/internal/offlinebundle/content_pack.go",
			"controlplane/internal/storage/content_pack_registry.go",
			"controlplane/internal/server/content_packs.go",
		},
		Gaps: gaps,
	}
}

func tenantEdgeCollectorCoverageFromCollectors(collectors []storage.ContentPackEdgeCollector, total int, now time.Time) coverageMatrixRow {
	if total == 0 {
		return tenantEdgeCollectorCoverageRow(
			coverageStateRawOnly,
			[]string{"edge_collectors_total=0"},
			[]string{"no SIEM edge collectors are registered for this tenant"},
		)
	}
	var healthy, degraded, registered, disabled, staleStatus, missingHeartbeat, staleHeartbeat, configMismatch int
	var newest *time.Time
	for _, collector := range collectors {
		switch collector.Status {
		case storage.ContentPackEdgeCollectorStatusHealthy:
			healthy++
		case storage.ContentPackEdgeCollectorStatusDegraded:
			degraded++
		case storage.ContentPackEdgeCollectorStatusRegistered:
			registered++
		case storage.ContentPackEdgeCollectorStatusDisabled:
			disabled++
		case storage.ContentPackEdgeCollectorStatusStale:
			staleStatus++
		}
		if collector.LastHeartbeatAt == nil {
			missingHeartbeat++
		} else {
			lastHeartbeat := collector.LastHeartbeatAt.UTC()
			if newest == nil || lastHeartbeat.After(*newest) {
				copyTime := lastHeartbeat
				newest = &copyTime
			}
			if now.Sub(lastHeartbeat) > coverageHeartbeatFreshnessWindow {
				staleHeartbeat++
			}
		}
		if strings.TrimSpace(collector.DesiredConfigVersion) != "" &&
			strings.TrimSpace(collector.RunningConfigVersion) != "" &&
			strings.TrimSpace(collector.DesiredConfigVersion) != strings.TrimSpace(collector.RunningConfigVersion) {
			configMismatch++
		}
	}
	signals := []string{
		fmt.Sprintf("edge_collectors_total=%d", total),
		fmt.Sprintf("edge_collectors_sampled=%d", len(collectors)),
		fmt.Sprintf("healthy=%d", healthy),
		fmt.Sprintf("degraded=%d", degraded),
		fmt.Sprintf("registered=%d", registered),
		fmt.Sprintf("disabled=%d", disabled),
		fmt.Sprintf("stale_status=%d", staleStatus),
		fmt.Sprintf("missing_heartbeat=%d", missingHeartbeat),
		fmt.Sprintf("stale_heartbeat=%d", staleHeartbeat),
		fmt.Sprintf("config_mismatch=%d", configMismatch),
	}
	if newest != nil {
		signals = append(signals, "last_heartbeat_at="+newest.UTC().Format(time.RFC3339))
	}
	if total > len(collectors) {
		signals = append(signals, "edge_collector_count_truncated=true")
	}
	gaps := []string{}
	if registered > 0 {
		gaps = append(gaps, fmt.Sprintf("%d edge collector(s) are registered but have not reported healthy apply/heartbeat state", registered))
	}
	if degraded > 0 {
		gaps = append(gaps, fmt.Sprintf("%d edge collector(s) are degraded", degraded))
	}
	if staleStatus > 0 || staleHeartbeat > 0 || missingHeartbeat > 0 {
		if staleStatus > 0 {
			gaps = append(gaps, fmt.Sprintf("%d edge collector(s) reported stale status", staleStatus))
		}
		if staleHeartbeat > 0 {
			gaps = append(gaps, fmt.Sprintf("%d edge collector heartbeat(s) are older than %s", staleHeartbeat, coverageHeartbeatFreshnessWindow))
		}
		if missingHeartbeat > 0 {
			gaps = append(gaps, fmt.Sprintf("%d edge collector(s) have no heartbeat timestamp", missingHeartbeat))
		}
		return tenantEdgeCollectorCoverageRow(coverageStateStale, signals, gaps)
	}
	if configMismatch > 0 {
		gaps = append(gaps, fmt.Sprintf("%d edge collector(s) are not running their desired config version", configMismatch))
	}
	if disabled > 0 {
		gaps = append(gaps, fmt.Sprintf("%d edge collector(s) are disabled", disabled))
	}
	if total > len(collectors) {
		gaps = append(gaps, "tenant has more than 500 edge collectors; overlay is sampled until pagination is expanded")
	}
	if degraded > 0 || registered > 0 || disabled > 0 || configMismatch > 0 || total > len(collectors) {
		return tenantEdgeCollectorCoverageRow(coverageStatePartial, signals, gaps)
	}
	return tenantEdgeCollectorCoverageRow(coverageStateSupported, signals, nil)
}

func tenantEdgeCollectorCoverageRow(state coverageSupportState, signals []string, gaps []string) coverageMatrixRow {
	return coverageMatrixRow{
		Domain:  "telemetry",
		Title:   "Tenant SIEM edge collectors",
		State:   state,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: append([]string{
			"content_pack_edge_collectors",
			"freshness_window=5m",
		}, signals...),
		Evidence: []string{
			"controlplane/internal/storage/content_pack_edge_collector.go",
			"controlplane/internal/storage/content_pack_collector_config.go",
			"controlplane/internal/server/content_packs.go",
		},
		Gaps: gaps,
	}
}

type tenantSourceHealthEvidence struct {
	RuntimeStateID   string
	SourceInstanceID string
	CollectorID      string
	SourceID         string
	ReceiverID       string
	NodeID           string
	PackID           string
	PackVersion      string
	ParserID         string
	CollectorMode    string
	ConfigVersion    string
	ContentVersion   string
	DisplayName      string
	State            contentpacks.CoverageState
	ApprovalRequired bool
	ApprovalID       string
	Metrics          contentpacks.SourceRuntimeMetrics
	LastEventAt      *time.Time
	LastParsedAt     *time.Time
	LastHealthAt     *time.Time
	LastError        string
	Labels           map[string]string
}

func tenantSourceHealthCoverageFromCollectors(collectors []storage.ContentPackEdgeCollector, total int, now time.Time) coverageMatrixRow {
	evidence := sourceHealthEvidenceFromCollectors(collectors)
	row := tenantSourceHealthCoverageFromEvidence(evidence, total, len(collectors), now, "edge collectors are not reporting per-source receiver/parser health yet")
	if total > len(collectors) {
		row.Signals = append(row.Signals, "edge_collector_count_truncated=true")
		row.Gaps = append(row.Gaps, "tenant has more than 500 edge collectors; source health overlay is sampled until pagination is expanded")
	}
	return row
}

func tenantSourceHealthCoverageFromEvidence(evidence map[string]tenantSourceHealthEvidence, total, sampled int, now time.Time, noEvidenceGap string) coverageMatrixRow {
	evidence = sourceHealthEvidenceWithCollectionConflicts(evidence)
	reportingCollectors := sourceHealthReportingCollectors(evidence)
	if len(evidence) == 0 {
		signals := []string{
			"source_health_sources_total=0",
			fmt.Sprintf("source_health_collectors_reporting=%d", len(reportingCollectors)),
			fmt.Sprintf("source_health_scope_total=%d", total),
			fmt.Sprintf("source_health_scope_sampled=%d", sampled),
		}
		return tenantSourceHealthCoverageRow(
			coverageStateRawOnly,
			signals,
			[]string{noEvidenceGap},
		)
	}

	var parserHealthy, collecting, parserFailed, silent, backpressured, collectionConflict, stale, deployed, approved, approvalRequired, proposed, privacyBlocked, unsupported, unknown int
	var eventsReceived, eventsParsed, parseFailures, eventsDropped, queueDepth int64
	var newestEvent, newestParsed, newestHealth *time.Time
	for _, item := range evidence {
		item = sourceHealthEvidenceApplyFreshness(item, now)
		state := item.State
		switch state {
		case contentpacks.CoverageState(contentpacks.CoverageParserHealthy):
			parserHealthy++
		case contentpacks.CoverageState(contentpacks.CoverageCollecting):
			collecting++
		case contentpacks.CoverageState(contentpacks.CoverageParserFailed):
			parserFailed++
		case contentpacks.CoverageState(contentpacks.CoverageSilent):
			silent++
		case contentpacks.CoverageState(contentpacks.CoverageBackpressured):
			backpressured++
		case contentpacks.CoverageState(contentpacks.CoverageCollectionConflict):
			collectionConflict++
		case contentpacks.CoverageState(contentpacks.CoverageStale):
			stale++
		case contentpacks.CoverageState(contentpacks.CoverageDeployed):
			deployed++
		case contentpacks.CoverageState(contentpacks.CoverageApproved):
			approved++
		case contentpacks.CoverageState(contentpacks.CoverageApprovalRequired):
			approvalRequired++
		case contentpacks.CoverageState(contentpacks.CoverageProposed), contentpacks.CoverageState(contentpacks.CoverageDiscovered):
			proposed++
		case contentpacks.CoverageState(contentpacks.CoveragePrivacyBlocked):
			privacyBlocked++
		case contentpacks.CoverageState(contentpacks.CoverageUnsupported):
			unsupported++
		default:
			unknown++
		}
		eventsReceived += item.Metrics.EventsReceived
		eventsParsed += item.Metrics.EventsParsed
		parseFailures += item.Metrics.ParseFailures
		eventsDropped += item.Metrics.EventsDropped
		queueDepth += item.Metrics.QueueDepth
		newestEvent = newestTime(newestEvent, item.LastEventAt)
		newestParsed = newestTime(newestParsed, item.LastParsedAt)
		newestHealth = newestTime(newestHealth, item.LastHealthAt)
	}

	signals := []string{
		fmt.Sprintf("source_health_sources_total=%d", len(evidence)),
		fmt.Sprintf("source_health_collectors_reporting=%d", len(reportingCollectors)),
		fmt.Sprintf("source_health_scope_total=%d", total),
		fmt.Sprintf("source_health_scope_sampled=%d", sampled),
		fmt.Sprintf("parser_healthy=%d", parserHealthy),
		fmt.Sprintf("collecting=%d", collecting),
		fmt.Sprintf("parser_failed=%d", parserFailed),
		fmt.Sprintf("silent=%d", silent),
		fmt.Sprintf("backpressured=%d", backpressured),
		fmt.Sprintf("collection_conflict=%d", collectionConflict),
		fmt.Sprintf("stale=%d", stale),
		fmt.Sprintf("deployed=%d", deployed),
		fmt.Sprintf("approved=%d", approved),
		fmt.Sprintf("approval_required=%d", approvalRequired),
		fmt.Sprintf("proposed=%d", proposed),
		fmt.Sprintf("privacy_blocked=%d", privacyBlocked),
		fmt.Sprintf("unsupported=%d", unsupported),
		fmt.Sprintf("unknown=%d", unknown),
		fmt.Sprintf("events_received=%d", eventsReceived),
		fmt.Sprintf("events_parsed=%d", eventsParsed),
		fmt.Sprintf("parse_failures=%d", parseFailures),
		fmt.Sprintf("events_dropped=%d", eventsDropped),
		fmt.Sprintf("queue_depth=%d", queueDepth),
	}
	if newestEvent != nil {
		signals = append(signals, "last_event_at="+newestEvent.UTC().Format(time.RFC3339))
	}
	if newestParsed != nil {
		signals = append(signals, "last_parsed_at="+newestParsed.UTC().Format(time.RFC3339))
	}
	if newestHealth != nil {
		signals = append(signals, "last_source_health_at="+newestHealth.UTC().Format(time.RFC3339))
	}

	gaps := []string{}
	if parserFailed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) report parser failures", parserFailed))
	}
	if backpressured > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) report queue, lag, retry, or drop pressure", backpressured))
	}
	if collectionConflict > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) have duplicate collection owners without approved migration dual-write", collectionConflict))
	}
	if silent > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are deployed but silent", silent))
	}
	if stale > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source health signal(s) are older than %s", stale, coverageHeartbeatFreshnessWindow))
	}
	if collecting > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are collecting but parser health is not yet proven", collecting))
	}
	if deployed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are deployed but event flow is not yet proven", deployed))
	}
	if approved > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are approved but not yet rendered/deployed", approved))
	}
	if approvalRequired > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) require explicit approval before collection", approvalRequired))
	}
	if proposed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are proposed/discovered but not approved for collection", proposed))
	}
	if privacyBlocked > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are privacy-blocked by policy", privacyBlocked))
	}
	if unsupported > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are unsupported or rejected for collection", unsupported))
	}
	if unknown > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source health signal(s) have an unrecognized state", unknown))
	}

	state := coverageStateSupported
	if stale > 0 && parserHealthy+collecting == 0 {
		state = coverageStateStale
	} else if parserHealthy+collecting+parserFailed+backpressured+collectionConflict+silent+deployed+approved+approvalRequired+proposed == 0 && privacyBlocked+unsupported > 0 {
		state = coverageStateException
	} else if parserFailed > 0 || backpressured > 0 || collectionConflict > 0 || silent > 0 || stale > 0 || collecting > 0 || deployed > 0 || approved > 0 || approvalRequired > 0 || proposed > 0 || privacyBlocked > 0 || unsupported > 0 || unknown > 0 || total > sampled {
		state = coverageStatePartial
	}
	return tenantSourceHealthCoverageRow(state, signals, gaps)
}

func tenantSourceHealthCoverageFromRuntimeSummary(summary storage.ContentPackSourceRuntimeStateSummary) coverageMatrixRow {
	if summary.Total == 0 {
		return tenantSourceHealthCoverageRow(
			coverageStateRawOnly,
			[]string{
				"source_runtime_persisted=true",
				"source_runtime_summary=true",
				"source_health_sources_total=0",
				"source_health_collectors_reporting=0",
				"source_health_scope_total=0",
				"source_health_scope_sampled=0",
			},
			[]string{"no durable SIEM source runtime states exist for this tenant"},
		)
	}
	byState := summary.ByState
	parserHealthy := byState[contentpacks.CoverageParserHealthy]
	collecting := byState[contentpacks.CoverageCollecting]
	parserFailed := byState[contentpacks.CoverageParserFailed]
	silent := byState[contentpacks.CoverageSilent]
	backpressured := byState[contentpacks.CoverageBackpressured]
	collectionConflict := byState[contentpacks.CoverageCollectionConflict]
	stale := byState[contentpacks.CoverageStale]
	deployed := byState[contentpacks.CoverageDeployed]
	approved := byState[contentpacks.CoverageApproved]
	approvalRequired := byState[contentpacks.CoverageApprovalRequired]
	proposed := byState[contentpacks.CoverageProposed] + byState[contentpacks.CoverageDiscovered]
	privacyBlocked := byState[contentpacks.CoveragePrivacyBlocked]
	unsupported := byState[contentpacks.CoverageUnsupported]
	known := parserHealthy + collecting + parserFailed + silent + backpressured + collectionConflict + stale + deployed + approved + approvalRequired + proposed + privacyBlocked + unsupported
	unknown := summary.Total - known
	if unknown < 0 {
		unknown = 0
	}
	metrics := summary.Metrics
	signals := []string{
		"source_runtime_persisted=true",
		"source_runtime_summary=true",
		fmt.Sprintf("source_health_sources_total=%d", summary.Total),
		fmt.Sprintf("source_health_collectors_reporting=%d", summary.CollectorsReporting),
		fmt.Sprintf("source_health_scope_total=%d", summary.Total),
		fmt.Sprintf("source_health_scope_sampled=%d", summary.Total),
		fmt.Sprintf("parser_healthy=%d", parserHealthy),
		fmt.Sprintf("collecting=%d", collecting),
		fmt.Sprintf("parser_failed=%d", parserFailed),
		fmt.Sprintf("silent=%d", silent),
		fmt.Sprintf("backpressured=%d", backpressured),
		fmt.Sprintf("collection_conflict=%d", collectionConflict),
		fmt.Sprintf("stale=%d", stale),
		fmt.Sprintf("deployed=%d", deployed),
		fmt.Sprintf("approved=%d", approved),
		fmt.Sprintf("approval_required=%d", approvalRequired),
		fmt.Sprintf("proposed=%d", proposed),
		fmt.Sprintf("privacy_blocked=%d", privacyBlocked),
		fmt.Sprintf("unsupported=%d", unsupported),
		fmt.Sprintf("unknown=%d", unknown),
		fmt.Sprintf("events_received=%d", metrics.EventsReceived),
		fmt.Sprintf("events_parsed=%d", metrics.EventsParsed),
		fmt.Sprintf("parse_failures=%d", metrics.ParseFailures),
		fmt.Sprintf("events_dropped=%d", metrics.EventsDropped),
		fmt.Sprintf("queue_depth=%d", metrics.QueueDepth),
	}

	gaps := []string{}
	if parserFailed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) report parser failures", parserFailed))
	}
	if backpressured > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) report queue, lag, retry, or drop pressure", backpressured))
	}
	if collectionConflict > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) have duplicate collection owners without approved migration dual-write", collectionConflict))
	}
	if silent > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are deployed but silent", silent))
	}
	if stale > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source health signal(s) are older than %s", stale, coverageHeartbeatFreshnessWindow))
	}
	if collecting > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are collecting but parser health is not yet proven", collecting))
	}
	if deployed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are deployed but event flow is not yet proven", deployed))
	}
	if approved > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are approved but not yet rendered/deployed", approved))
	}
	if approvalRequired > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) require explicit approval before collection", approvalRequired))
	}
	if proposed > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are proposed/discovered but not approved for collection", proposed))
	}
	if privacyBlocked > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are privacy-blocked by policy", privacyBlocked))
	}
	if unsupported > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source(s) are unsupported or rejected for collection", unsupported))
	}
	if unknown > 0 {
		gaps = append(gaps, fmt.Sprintf("%d source health signal(s) have an unrecognized state", unknown))
	}

	state := coverageStateSupported
	if stale > 0 && parserHealthy+collecting == 0 {
		state = coverageStateStale
	} else if parserHealthy+collecting+parserFailed+backpressured+collectionConflict+silent+deployed+approved+approvalRequired+proposed == 0 && privacyBlocked+unsupported > 0 {
		state = coverageStateException
	} else if parserFailed > 0 || backpressured > 0 || collectionConflict > 0 || silent > 0 || stale > 0 || collecting > 0 || deployed > 0 || approved > 0 || approvalRequired > 0 || proposed > 0 || privacyBlocked > 0 || unsupported > 0 || unknown > 0 {
		state = coverageStatePartial
	}
	return tenantSourceHealthCoverageRow(state, signals, gaps)
}

func tenantSourceHealthCoverageFromRuntimeStates(rows []storage.ContentPackSourceRuntimeStateRecord, total int, now time.Time) coverageMatrixRow {
	evidence := make(map[string]tenantSourceHealthEvidence, len(rows))
	for _, row := range rows {
		item := tenantSourceHealthEvidenceFromRuntimeState(row.State)
		if item.SourceID == "" {
			continue
		}
		key := item.CollectorID + "/" + item.SourceID
		if key == "/" {
			key = item.SourceID
		}
		evidence[key] = item
	}
	row := tenantSourceHealthCoverageFromEvidence(evidence, total, len(rows), now, "no durable SIEM source runtime states exist for this tenant")
	row.Signals = append(row.Signals, "source_runtime_persisted=true")
	if total > len(rows) {
		row.Signals = append(row.Signals, "source_runtime_count_truncated=true")
		row.Gaps = append(row.Gaps, "tenant has more than 500 source runtime states; overlay is sampled until pagination is expanded")
	}
	return row
}

func sourceHealthEvidenceFromCollectors(collectors []storage.ContentPackEdgeCollector) map[string]tenantSourceHealthEvidence {
	evidence := map[string]tenantSourceHealthEvidence{}
	for _, collector := range collectors {
		collectSourceHealthEvidence(evidence, collector, "sources")
		collectSourceHealthEvidence(evidence, collector, "source_health")
		collectReceiverHealthEvidence(evidence, collector, "receivers")
		collectReceiverHealthEvidence(evidence, collector, "receiver_health")
	}
	return evidence
}

func sourceHealthReportingCollectors(evidence map[string]tenantSourceHealthEvidence) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range evidence {
		if strings.TrimSpace(item.CollectorID) != "" {
			out[item.CollectorID] = struct{}{}
		}
	}
	return out
}

func tenantSourceHealthCoverageRow(state coverageSupportState, signals []string, gaps []string) coverageMatrixRow {
	return coverageMatrixRow{
		Domain:  "parser",
		Title:   "Tenant SIEM source health",
		State:   state,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: append([]string{
			"content_pack_edge_collectors.health.sources",
			"freshness_window=5m",
		}, signals...),
		Evidence: []string{
			"controlplane/internal/server/coverage_handlers.go",
			"controlplane/internal/server/content_packs.go",
			"controlplane/internal/storage/content_pack_edge_collector.go",
			"internal/contentpacks/state.go",
		},
		Gaps: gaps,
	}
}

func collectSourceHealthEvidence(out map[string]tenantSourceHealthEvidence, collector storage.ContentPackEdgeCollector, key string) {
	value, ok := healthMapValue(collector.Health, key)
	if !ok {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for sourceID, raw := range typed {
			if item, ok := sourceHealthEvidenceFromValue(collector, sourceID, "", raw); ok {
				mergeSourceHealthEvidence(out, item)
			}
		}
	case []any:
		for _, raw := range typed {
			if item, ok := sourceHealthEvidenceFromValue(collector, "", "", raw); ok {
				mergeSourceHealthEvidence(out, item)
			}
		}
	}
}

func collectReceiverHealthEvidence(out map[string]tenantSourceHealthEvidence, collector storage.ContentPackEdgeCollector, key string) {
	value, ok := healthMapValue(collector.Health, key)
	if !ok {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for receiverID, raw := range typed {
			sourceID := sourceIDFromOTelReceiverID(receiverID)
			if item, ok := sourceHealthEvidenceFromValue(collector, sourceID, receiverID, raw); ok {
				mergeSourceHealthEvidence(out, item)
			}
		}
	case []any:
		for _, raw := range typed {
			if item, ok := sourceHealthEvidenceFromValue(collector, "", "", raw); ok {
				mergeSourceHealthEvidence(out, item)
			}
		}
	}
}

func sourceHealthEvidenceFromValue(collector storage.ContentPackEdgeCollector, sourceID, receiverID string, raw any) (tenantSourceHealthEvidence, bool) {
	item := tenantSourceHealthEvidence{
		CollectorID:   strings.TrimSpace(collector.CollectorID),
		SourceID:      strings.TrimSpace(sourceID),
		ReceiverID:    strings.TrimSpace(receiverID),
		ConfigVersion: strings.TrimSpace(collector.RunningConfigVersion),
		State:         contentpacks.CoverageState(contentpacks.CoverageDeployed),
	}
	if collector.LastHeartbeatAt != nil {
		t := collector.LastHeartbeatAt.UTC()
		item.LastHealthAt = &t
	}
	switch typed := raw.(type) {
	case string:
		item.State = sourceHealthStateFromStatus(typed, item.Metrics)
	case bool:
		if typed {
			item.State = contentpacks.CoverageState(contentpacks.CoverageCollecting)
		}
	case map[string]any:
		item.SourceInstanceID = firstNonEmptyContentPack(
			healthStringField(typed, "source_instance_id"),
			healthStringField(typed, "instance_id"),
		)
		item.SourceID = firstNonEmptyContentPack(
			healthStringField(typed, "source_id"),
			healthStringField(typed, "source"),
			item.SourceID,
		)
		item.NodeID = healthStringField(typed, "node_id")
		item.PackID = healthStringField(typed, "pack_id")
		item.PackVersion = healthStringField(typed, "pack_version")
		item.ParserID = healthStringField(typed, "parser_id")
		item.CollectorMode = healthStringField(typed, "collector_mode")
		item.ConfigVersion = firstNonEmptyContentPack(healthStringField(typed, "config_version"), item.ConfigVersion)
		item.ContentVersion = healthStringField(typed, "content_version")
		item.DisplayName = healthStringField(typed, "display_name")
		item.ApprovalRequired = healthBoolField(typed, "approval_required", "requires_approval")
		item.ApprovalID = firstNonEmptyContentPack(
			healthStringField(typed, "approval_id"),
			healthStringField(typed, "approval_ref"),
			healthStringField(typed, "source_proposal_id"),
		)
		item.ReceiverID = firstNonEmptyContentPack(
			healthStringField(typed, "receiver_id"),
			healthStringField(typed, "receiver"),
			item.ReceiverID,
		)
		item.Metrics = contentpacks.SourceRuntimeMetrics{
			EventsReceived:  healthInt64Field(typed, "events_received", "received", "accepted"),
			EventsParsed:    healthInt64Field(typed, "events_parsed", "parsed"),
			EventsDropped:   healthInt64Field(typed, "events_dropped", "dropped"),
			ParseFailures:   healthInt64Field(typed, "parse_failures", "parser_failures", "failed_parse_count"),
			LagMillis:       healthInt64Field(typed, "lag_millis", "lag_ms"),
			QueueDepth:      healthInt64Field(typed, "queue_depth", "exporter_queue_depth"),
			CursorAgeMillis: healthInt64Field(typed, "cursor_age_millis", "cursor_age_ms"),
			RetryCount:      healthInt64Field(typed, "retry_count", "retries"),
		}
		item.LastEventAt = healthTimeField(typed, "last_event_at", "last_received_at")
		item.LastParsedAt = healthTimeField(typed, "last_parsed_at")
		if lastHealthAt := healthTimeField(typed, "last_health_at", "updated_at"); lastHealthAt != nil {
			item.LastHealthAt = lastHealthAt
		}
		item.LastError = firstNonEmptyContentPack(healthStringField(typed, "last_error"), healthStringField(typed, "error"))
		item.Labels = healthStringMapField(typed, "labels")
		item.State = sourceHealthStateFromStatus(firstNonEmptyContentPack(
			healthStringField(typed, "coverage_state"),
			healthStringField(typed, "state"),
			healthStringField(typed, "status"),
		), item.Metrics)
	default:
		return tenantSourceHealthEvidence{}, false
	}
	if item.SourceID == "" && item.ReceiverID != "" {
		item.SourceID = sourceIDFromOTelReceiverID(item.ReceiverID)
	}
	if item.SourceID == "" {
		return tenantSourceHealthEvidence{}, false
	}
	if item.SourceInstanceID == "" {
		item.SourceInstanceID = contentPackSourceInstanceID(item.CollectorID, item.SourceID)
	}
	return item, true
}

func sourceHealthEvidenceApplyFreshness(item tenantSourceHealthEvidence, now time.Time) tenantSourceHealthEvidence {
	if item.LastHealthAt != nil && now.Sub(item.LastHealthAt.UTC()) > coverageHeartbeatFreshnessWindow {
		item.State = contentpacks.CoverageState(contentpacks.CoverageStale)
	}
	return item
}

func mergeSourceHealthEvidence(out map[string]tenantSourceHealthEvidence, next tenantSourceHealthEvidence) {
	key := next.CollectorID + "/" + next.SourceID
	current, ok := out[key]
	if !ok {
		out[key] = next
		return
	}
	if sourceHealthSeverity(next.State) >= sourceHealthSeverity(current.State) {
		current.State = next.State
		if strings.TrimSpace(next.LastError) != "" {
			current.LastError = next.LastError
		}
	}
	current.ReceiverID = firstNonEmptyContentPack(current.ReceiverID, next.ReceiverID)
	current.SourceInstanceID = firstNonEmptyContentPack(current.SourceInstanceID, next.SourceInstanceID)
	current.NodeID = firstNonEmptyContentPack(current.NodeID, next.NodeID)
	current.PackID = firstNonEmptyContentPack(current.PackID, next.PackID)
	current.PackVersion = firstNonEmptyContentPack(current.PackVersion, next.PackVersion)
	current.ParserID = firstNonEmptyContentPack(current.ParserID, next.ParserID)
	current.CollectorMode = firstNonEmptyContentPack(current.CollectorMode, next.CollectorMode)
	current.ConfigVersion = firstNonEmptyContentPack(current.ConfigVersion, next.ConfigVersion)
	current.ContentVersion = firstNonEmptyContentPack(current.ContentVersion, next.ContentVersion)
	current.DisplayName = firstNonEmptyContentPack(current.DisplayName, next.DisplayName)
	current.ApprovalRequired = current.ApprovalRequired || next.ApprovalRequired
	current.ApprovalID = firstNonEmptyContentPack(current.ApprovalID, next.ApprovalID)
	current.Labels = mergeHealthLabels(current.Labels, next.Labels)
	current.Metrics.EventsReceived = maxInt64(current.Metrics.EventsReceived, next.Metrics.EventsReceived)
	current.Metrics.EventsParsed = maxInt64(current.Metrics.EventsParsed, next.Metrics.EventsParsed)
	current.Metrics.EventsDropped = maxInt64(current.Metrics.EventsDropped, next.Metrics.EventsDropped)
	current.Metrics.ParseFailures = maxInt64(current.Metrics.ParseFailures, next.Metrics.ParseFailures)
	current.Metrics.LagMillis = maxInt64(current.Metrics.LagMillis, next.Metrics.LagMillis)
	current.Metrics.QueueDepth = maxInt64(current.Metrics.QueueDepth, next.Metrics.QueueDepth)
	current.Metrics.CursorAgeMillis = maxInt64(current.Metrics.CursorAgeMillis, next.Metrics.CursorAgeMillis)
	current.Metrics.RetryCount = maxInt64(current.Metrics.RetryCount, next.Metrics.RetryCount)
	current.LastEventAt = newestTime(current.LastEventAt, next.LastEventAt)
	current.LastParsedAt = newestTime(current.LastParsedAt, next.LastParsedAt)
	current.LastHealthAt = newestTime(current.LastHealthAt, next.LastHealthAt)
	out[key] = current
}

func sourceHealthStateFromStatus(status string, metrics contentpacks.SourceRuntimeMetrics) contentpacks.CoverageState {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case contentpacks.CoverageParserHealthy, "healthy", "parser-ok", "parser_ok":
		return contentpacks.CoverageState(contentpacks.CoverageParserHealthy)
	case contentpacks.CoverageCollecting, "ok", "active", "running":
		return contentpacks.CoverageState(contentpacks.CoverageCollecting)
	case contentpacks.CoverageParserFailed, "parse_failed", "parser-failed", "failed":
		return contentpacks.CoverageState(contentpacks.CoverageParserFailed)
	case contentpacks.CoverageSilent:
		return contentpacks.CoverageState(contentpacks.CoverageSilent)
	case contentpacks.CoverageBackpressured, "backpressure", "degraded":
		return contentpacks.CoverageState(contentpacks.CoverageBackpressured)
	case contentpacks.CoverageCollectionConflict, "duplicate_collection", "collection-conflict":
		return contentpacks.CoverageState(contentpacks.CoverageCollectionConflict)
	case contentpacks.CoverageStale:
		return contentpacks.CoverageState(contentpacks.CoverageStale)
	case contentpacks.CoverageDeployed:
		return contentpacks.CoverageState(contentpacks.CoverageDeployed)
	}
	if normalized != "" {
		return contentpacks.CoverageState(normalized)
	}
	if metrics.ParseFailures > 0 {
		return contentpacks.CoverageState(contentpacks.CoverageParserFailed)
	}
	if metrics.EventsDropped > 0 || metrics.QueueDepth > 0 || metrics.RetryCount > 0 || metrics.LagMillis > 0 {
		return contentpacks.CoverageState(contentpacks.CoverageBackpressured)
	}
	if metrics.EventsReceived > 0 && metrics.EventsParsed > 0 && metrics.EventsParsed >= metrics.EventsReceived {
		return contentpacks.CoverageState(contentpacks.CoverageParserHealthy)
	}
	if metrics.EventsReceived > 0 {
		return contentpacks.CoverageState(contentpacks.CoverageCollecting)
	}
	return contentpacks.CoverageState(contentpacks.CoverageDeployed)
}

func sourceHealthSeverity(state contentpacks.CoverageState) int {
	switch state {
	case contentpacks.CoverageState(contentpacks.CoverageCollectionConflict):
		return 8
	case contentpacks.CoverageState(contentpacks.CoverageParserFailed):
		return 7
	case contentpacks.CoverageState(contentpacks.CoverageBackpressured):
		return 6
	case contentpacks.CoverageState(contentpacks.CoverageSilent):
		return 5
	case contentpacks.CoverageState(contentpacks.CoverageStale):
		return 4
	case contentpacks.CoverageState(contentpacks.CoverageDeployed):
		return 3
	case contentpacks.CoverageState(contentpacks.CoverageCollecting):
		return 2
	case contentpacks.CoverageState(contentpacks.CoverageParserHealthy):
		return 1
	default:
		return 0
	}
}

func healthMapValue(values map[string]any, key string) (any, bool) {
	for k, value := range values {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return value, true
		}
	}
	return nil, false
}

func healthStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := healthMapValue(values, key)
		if !ok || value == nil {
			continue
		}
		if str, ok := value.(string); ok {
			return strings.TrimSpace(str)
		}
	}
	return ""
}

func healthStringMapField(values map[string]any, keys ...string) map[string]string {
	for _, key := range keys {
		value, ok := healthMapValue(values, key)
		if !ok || value == nil {
			continue
		}
		out := map[string]string{}
		switch typed := value.(type) {
		case map[string]string:
			for labelKey, labelValue := range typed {
				labelKey = strings.TrimSpace(labelKey)
				labelValue = strings.TrimSpace(labelValue)
				if labelKey != "" && labelValue != "" {
					out[labelKey] = labelValue
				}
			}
		case map[string]any:
			for labelKey, rawLabelValue := range typed {
				labelKey = strings.TrimSpace(labelKey)
				labelValue := strings.TrimSpace(fmt.Sprint(rawLabelValue))
				if labelKey != "" && labelValue != "" && labelValue != "<nil>" {
					out[labelKey] = labelValue
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func healthBoolField(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := healthMapValue(values, key)
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			normalized := strings.ToLower(strings.TrimSpace(typed))
			return normalized == "true" || normalized == "yes" || normalized == "1"
		}
	}
	return false
}

func mergeHealthLabels(current, next map[string]string) map[string]string {
	if len(current) == 0 && len(next) == 0 {
		return nil
	}
	out := cloneStringMapContentPack(current)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range next {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func healthInt64Field(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := healthMapValue(values, key)
		if !ok {
			continue
		}
		if parsed, ok := healthInt64(value); ok {
			return parsed
		}
	}
	return 0
}

func healthInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case float32:
		return int64(typed), true
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return parsed, true
		}
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			return int64(parsed), true
		}
	}
	return 0, false
}

func healthTimeField(values map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		value, ok := healthMapValue(values, key)
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case time.Time:
			t := typed.UTC()
			return &t
		case string:
			raw := strings.TrimSpace(typed)
			if raw == "" {
				continue
			}
			if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				t := parsed.UTC()
				return &t
			}
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				t := parsed.UTC()
				return &t
			}
		}
	}
	return nil
}

func sourceIDFromOTelReceiverID(receiverID string) string {
	receiverID = strings.TrimSpace(receiverID)
	if receiverID == "" {
		return ""
	}
	const marker = "/controlone."
	idx := strings.Index(strings.ToLower(receiverID), marker)
	if idx < 0 {
		return receiverID
	}
	return strings.TrimSpace(receiverID[idx+len(marker):])
}

func newestTime(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	value := candidate.UTC()
	if current == nil || value.After(current.UTC()) {
		return &value
	}
	return current
}

func maxInt64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func filterCoverageMatrixByDomain(rows []coverageMatrixRow, domain string) []coverageMatrixRow {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return rows
	}
	filtered := make([]coverageMatrixRow, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(row.Domain, domain) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func newCoverageExplainResponse(tenantID uuid.UUID) coverageExplainResponse {
	return coverageExplainResponse{
		CatalogVersion: coverageCatalogVersion,
		Scope:          coverageScope(tenantID),
		TenantID:       coverageTenantIDString(tenantID),
		Domains:        cloneCoverageDomains(),
		Legend:         buildCoverageLegend(),
		Explanations:   cloneCoverageExplanations(),
	}
}

func coverageScope(tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return "global"
	}
	return "tenant"
}

func coverageTenantIDString(tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return ""
	}
	return tenantID.String()
}
