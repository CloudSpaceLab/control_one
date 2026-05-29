package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
)

// nodeServicesRequest is the agent's payload when reporting an inventory cycle.
// Empty `services` means "no listening services discovered" (clears table).
type nodeServicesRequest struct {
	Services           []nodeServiceItem             `json:"services"`
	ConnectorProposals []connectordiscovery.Proposal `json:"connector_proposals,omitempty"`
}

type nodeServiceItem struct {
	PID              int     `json:"pid"`
	Process          string  `json:"process"`
	BinaryPath       string  `json:"binary_path"`
	ListenAddr       string  `json:"listen_addr"`
	Port             int     `json:"port"`
	ServiceKind      string  `json:"service_kind"`
	ProbeStatus      *int    `json:"probe_status,omitempty"`
	ProbeServer      *string `json:"probe_server,omitempty"`
	ProbeTitle       *string `json:"probe_title,omitempty"`
	ProbeContentType *string `json:"probe_content_type,omitempty"`
}

type nodeServiceResponse struct {
	ID               string  `json:"id"`
	NodeID           string  `json:"node_id"`
	TenantID         string  `json:"tenant_id"`
	PID              int     `json:"pid"`
	Process          string  `json:"process"`
	BinaryPath       string  `json:"binary_path"`
	ListenAddr       string  `json:"listen_addr"`
	Port             int     `json:"port"`
	ServiceKind      string  `json:"service_kind"`
	ProbeStatus      *int    `json:"probe_status,omitempty"`
	ProbeServer      *string `json:"probe_server,omitempty"`
	ProbeTitle       *string `json:"probe_title,omitempty"`
	ProbeContentType *string `json:"probe_content_type,omitempty"`
	ObservedAt       string  `json:"observed_at"`
}

type nodeApprovedLogSourcesResponse struct {
	NodeID      string                  `json:"node_id"`
	GeneratedAt string                  `json:"generated_at"`
	Sources     []nodeApprovedLogSource `json:"sources"`
}

type nodeApprovedLogSource struct {
	ProposalRecordID string            `json:"proposal_record_id"`
	ProposalID       string            `json:"proposal_id"`
	SourceID         string            `json:"source_id,omitempty"`
	Program          string            `json:"program"`
	Type             string            `json:"type"`
	CollectMode      string            `json:"collect_mode,omitempty"`
	Paths            []string          `json:"paths"`
	Formatter        string            `json:"formatter,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

type nodeApprovedLogSourceStore interface {
	ListApprovedContentPackSourceProposalsForNode(context.Context, uuid.UUID, uuid.UUID, int) ([]storage.ContentPackSourceProposalRecord, error)
}

func newNodeServiceResponse(svc storage.NodeService) nodeServiceResponse {
	return nodeServiceResponse{
		ID:               svc.ID.String(),
		NodeID:           svc.NodeID.String(),
		TenantID:         svc.TenantID.String(),
		PID:              svc.PID,
		Process:          svc.Process,
		BinaryPath:       svc.BinaryPath,
		ListenAddr:       svc.ListenAddr,
		Port:             svc.Port,
		ServiceKind:      svc.ServiceKind,
		ProbeStatus:      svc.ProbeStatus,
		ProbeServer:      svc.ProbeServer,
		ProbeTitle:       svc.ProbeTitle,
		ProbeContentType: svc.ProbeContentType,
		ObservedAt:       svc.ObservedAt.UTC().Format(time.RFC3339),
	}
}

// handleNodeServices is dispatched from handleNodeResource for paths like
// /api/v1/nodes/<id>/services. POST is the agent's inventory upload (mTLS,
// CN must match node id); GET returns the latest snapshot to operator UIs.
func (s *Server) handleNodeServices(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleNodeServicesIngest(w, r, nodeID)
	case http.MethodGet:
		s.handleNodeServicesList(w, r, nodeID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeServicesIngest(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if principal.Type != "agent" {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	cn := strings.TrimSpace(principal.Name)
	if cn == "" || !strings.EqualFold(cn, nodeID.String()) {
		http.Error(w, "client cert CN does not match node id", http.StatusForbidden)
		return
	}

	var body nodeServicesRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for services ingest", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	services := make([]storage.NodeService, 0, len(body.Services))
	for _, svc := range body.Services {
		if svc.Port <= 0 || svc.Port > 65535 {
			continue
		}
		services = append(services, storage.NodeService{
			NodeID:           nodeID,
			TenantID:         node.TenantID,
			PID:              svc.PID,
			Process:          svc.Process,
			BinaryPath:       svc.BinaryPath,
			ListenAddr:       svc.ListenAddr,
			Port:             svc.Port,
			ServiceKind:      svc.ServiceKind,
			ProbeStatus:      svc.ProbeStatus,
			ProbeServer:      svc.ProbeServer,
			ProbeTitle:       svc.ProbeTitle,
			ProbeContentType: svc.ProbeContentType,
		})
	}

	if err := s.store.ReplaceNodeServices(r.Context(), nodeID, node.TenantID, services); err != nil {
		s.logger.Error("replace node services", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if len(body.ConnectorProposals) > 0 {
		if _, err := s.updateNodeConnectorProposals(r.Context(), node, body.ConnectorProposals); err != nil {
			s.logger.Warn("update node connector proposals", zap.Error(err))
		}
		s.persistNodeConnectorProposals(r.Context(), node, body.ConnectorProposals)
	}

	// Bridge node_services -> port_observations (bugs §1.3).
	//
	// Until this bridge existed the table had zero writers in the codebase:
	// the agent only POSTs `node_services`, never raw port observations,
	// so AggregatePortObservations always returned [] and the
	// Recommendations tab was permanently empty for every tenant. Each
	// listening service is, by definition, an observation that the port
	// was open at this tick; we persist one row per service per ingest
	// so the existing aggregator (50-sample / 95%-dominant) has input.
	//
	// Best-effort: a partial failure here MUST NOT fail the agent's ingest
	// (services are already committed). We log and continue.
	s.bridgePortObservations(r.Context(), node.TenantID, nodeID, services)

	knowledgeGraphCache.invalidate(node.TenantID)

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNodeApprovedLogSources(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if principal.Type != "agent" {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	cn := strings.TrimSpace(principal.Name)
	if cn == "" || !strings.EqualFold(cn, nodeID.String()) {
		http.Error(w, "client cert CN does not match node id", http.StatusForbidden)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for approved log sources", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}
	sources, err := s.listNodeApprovedLogSources(r.Context(), node, 128)
	if err != nil {
		s.logger.Error("list approved log source proposals", zap.Error(err), zap.String("node_id", nodeID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, nodeApprovedLogSourcesResponse{
		NodeID:      nodeID.String(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Sources:     sources,
	})
}

func (s *Server) listNodeApprovedLogSources(ctx context.Context, node *storage.Node, limit int) ([]nodeApprovedLogSource, error) {
	if s == nil || s.store == nil || node == nil || node.ID == uuid.Nil || node.TenantID == uuid.Nil {
		return nil, nil
	}
	store, ok := s.store.(nodeApprovedLogSourceStore)
	if !ok || store == nil {
		return []nodeApprovedLogSource{}, nil
	}
	proposals, err := store.ListApprovedContentPackSourceProposalsForNode(ctx, node.TenantID, node.ID, limit)
	if err != nil {
		return nil, err
	}
	sources := make([]nodeApprovedLogSource, 0, len(proposals))
	for _, proposal := range proposals {
		source, ok := nodeApprovedLogSourceFromProposal(proposal)
		if ok {
			sources = append(sources, source)
		}
	}
	return sources, nil
}

func nodeApprovedLogSourceFromProposal(proposal storage.ContentPackSourceProposalRecord) (nodeApprovedLogSource, bool) {
	if proposal.Status != storage.ContentPackSourceProposalStatusApproved {
		return nodeApprovedLogSource{}, false
	}
	if !storage.ContentPackSourceProposalCollectModeDeploysNodeAgent(proposal.CollectMode) {
		return nodeApprovedLogSource{}, false
	}
	kind := strings.ToLower(strings.TrimSpace(proposal.Kind))
	if kind != connectordiscovery.KindLocalLog {
		return nodeApprovedLogSource{}, false
	}
	collectorType := strings.ToLower(strings.TrimSpace(proposal.CollectorType))
	if collectorType == "" {
		collectorType = connectordiscovery.CollectorTypeFile
	}
	if collectorType != connectordiscovery.CollectorTypeFile {
		return nodeApprovedLogSource{}, false
	}
	program := strings.ToLower(strings.TrimSpace(proposal.Program))
	if program == "" {
		return nodeApprovedLogSource{}, false
	}
	paths := sanitizeStringSlice(proposal.Paths, 64)
	if len(paths) == 0 {
		return nodeApprovedLogSource{}, false
	}
	collectMode := strings.ToLower(strings.TrimSpace(proposal.CollectMode))
	if collectMode == "" {
		collectMode = storage.ContentPackSourceProposalCollectModeCollectRaw
	}
	labels := sanitizeConnectorProposalLabels(proposal.Labels)
	labels["control_one.source_proposal_id"] = proposal.ID.String()
	labels["control_one.source_proposal_external_id"] = proposal.ProposalID
	labels["control_one.connector_decision"] = storage.ContentPackSourceProposalStatusApproved
	labels["control_one.collect_mode"] = collectMode
	if proposal.SourceID != "" {
		labels["control_one.content_pack_source_id"] = proposal.SourceID
	}
	if labels["discovery_source"] == "" {
		labels["discovery_source"] = "local"
	}
	formatter := strings.ToLower(strings.TrimSpace(proposal.Formatter))
	if formatter == "" {
		formatter = "generic"
	}
	return nodeApprovedLogSource{
		ProposalRecordID: proposal.ID.String(),
		ProposalID:       proposal.ProposalID,
		SourceID:         proposal.SourceID,
		Program:          program,
		Type:             collectorType,
		CollectMode:      collectMode,
		Paths:            paths,
		Formatter:        formatter,
		Labels:           labels,
	}, true
}

func (s *Server) updateNodeConnectorProposals(ctx context.Context, node *storage.Node, proposals []connectordiscovery.Proposal) (*storage.Node, error) {
	if s == nil || s.store == nil || node == nil || node.ID == uuid.Nil {
		return node, nil
	}
	normalized := normalizeConnectorProposals(proposals)
	if len(normalized) == 0 {
		return node, nil
	}
	labels := map[string]any{}
	for k, v := range node.Labels {
		labels[k] = v
	}
	labels["agent.connector_proposals"] = normalized
	labels["agent.connector_proposal_count"] = len(normalized)
	labels["agent.connector_auto_eligible_count"] = countAutoEligibleConnectorProposals(proposals)
	if err := s.store.UpdateNodeLabels(ctx, node.ID, labels); err != nil {
		return node, err
	}
	updated := *node
	updated.Labels = labels
	return &updated, nil
}

func (s *Server) persistNodeConnectorProposals(ctx context.Context, node *storage.Node, proposals []connectordiscovery.Proposal) {
	if s == nil || s.store == nil || node == nil || node.ID == uuid.Nil || node.TenantID == uuid.Nil || len(proposals) == 0 {
		return
	}
	store, ok := s.store.(contentPackSourceProposalStore)
	if !ok || store == nil {
		return
	}
	records, err := store.UpsertContentPackSourceProposals(ctx, storage.UpsertContentPackSourceProposalsParams{
		TenantID:  node.TenantID,
		NodeID:    node.ID,
		Proposals: proposals,
	})
	if err != nil {
		s.logger.Warn("persist connector source proposals", zap.Error(err), zap.String("node_id", node.ID.String()))
		return
	}
	s.persistContentPackSourceRuntimeStatesFromProposals(ctx, records)
}

func normalizeConnectorProposals(proposals []connectordiscovery.Proposal) []map[string]any {
	out := make([]map[string]any, 0, len(proposals))
	seen := map[string]struct{}{}
	for _, proposal := range proposals {
		program := strings.ToLower(strings.TrimSpace(proposal.Program))
		if program == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(proposal.ID))
		if key == "" {
			key = proposal.Kind + ":" + program
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		confidence := proposal.Confidence
		if confidence < 0 {
			confidence = 0
		}
		if confidence > 100 {
			confidence = 100
		}
		item := map[string]any{
			"id":                    key,
			"kind":                  strings.ToLower(strings.TrimSpace(proposal.Kind)),
			"program":               program,
			"collector_type":        strings.ToLower(strings.TrimSpace(proposal.CollectorType)),
			"formatter":             strings.ToLower(strings.TrimSpace(proposal.Formatter)),
			"confidence":            confidence,
			"risk":                  strings.ToLower(strings.TrimSpace(proposal.Risk)),
			"auto_connect_eligible": proposal.AutoConnectEligible,
			"requires_approval":     proposal.RequiresApproval,
			"reason":                strings.TrimSpace(proposal.Reason),
			"evidence":              sanitizeStringSlice(proposal.Evidence, 12),
			"paths":                 sanitizeStringSlice(proposal.Paths, 12),
			"labels":                sanitizeConnectorProposalLabels(proposal.Labels),
		}
		out = append(out, item)
		if len(out) >= 32 {
			break
		}
	}
	return out
}

func countAutoEligibleConnectorProposals(proposals []connectordiscovery.Proposal) int {
	count := 0
	for _, proposal := range proposals {
		if proposal.AutoConnectEligible {
			count++
		}
	}
	return count
}

func sanitizeConnectorProposalLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
		if len(out) >= 16 {
			break
		}
	}
	return out
}

// bridgePortObservations writes one port_observations row per listening
// service so the recommendations generator (`handleRecommendations`) has
// non-empty input. See bugs §1.3.
func (s *Server) bridgePortObservations(ctx context.Context, tenantID, nodeID uuid.UUID, services []storage.NodeService) {
	if s.store == nil || len(services) == 0 {
		return
	}
	nodeRef := nodeID
	for _, svc := range services {
		if svc.Port <= 0 || svc.Port > 65535 {
			continue
		}
		params := storage.CreatePortObservationParams{
			TenantID: tenantID,
			NodeID:   &nodeRef,
			Port:     svc.Port,
			Protocol: protocolFromServiceKind(svc.ServiceKind),
			State:    portStateFromService(svc),
		}
		if err := s.store.CreatePortObservation(ctx, params); err != nil {
			s.logger.Warn("create port observation",
				zap.String("tenant", tenantID.String()),
				zap.String("node", nodeID.String()),
				zap.Int("port", svc.Port),
				zap.Error(err))
		}
	}
}

// protocolFromServiceKind maps the agent's service_kind heuristic to the
// transport protocol stored in port_observations. Today every kind comes
// from a TCP listening socket; udp will be added when the agent grows
// udp inventory.
func protocolFromServiceKind(kind string) string {
	_ = strings.ToLower(strings.TrimSpace(kind))
	return "tcp"
}

// portStateFromService classifies a listening service into the
// observation states the recommendations aggregator understands.
// Anything in node_services is, by definition, in LISTEN -> "open".
// 5xx (or zero) HTTP probe statuses are treated as "filtered" so
// misbehaving services don't drown out healthy ones in dominant-state.
func portStateFromService(svc storage.NodeService) string {
	if svc.ProbeStatus != nil {
		code := *svc.ProbeStatus
		if code >= 500 || code == 0 {
			return "filtered"
		}
	}
	return "open"
}

func (s *Server) handleNodeServicesList(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	services, err := s.store.ListNodeServicesForNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("list node services", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]nodeServiceResponse, 0, len(services))
	for _, svc := range services {
		out = append(out, newNodeServiceResponse(svc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// handleKnowledgeGraph returns a per-tenant markdown document the LLM-Ask
// surface can ground answers against. URL: /api/v1/knowledge-graph/<tenant>.md
// Cached in-process for 5 minutes; invalidated when an agent uploads a fresh
// services payload.
func (s *Server) handleKnowledgeGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/knowledge-graph/")
	rest = strings.TrimSuffix(rest, ".md")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		http.Error(w, "tenant id required", http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(rest)
	if err != nil {
		http.Error(w, "invalid tenant id", http.StatusBadRequest)
		return
	}

	sections, err := s.getCachedKGSections(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("build knowledge graph", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeMarkdown(w, renderKGSections(sections))
}

// getCachedKGSections returns the cached []kgSection for a tenant or
// rebuilds + caches it on miss. The cache stores the section slice (NOT
// the rendered string) so the per-request compressor can re-pack
// sections for each question without re-running the storage queries.
func (s *Server) getCachedKGSections(ctx context.Context, tenantID uuid.UUID) ([]kgSection, error) {
	if cached, ok := knowledgeGraphCache.get(tenantID); ok {
		return cached, nil
	}
	sections, err := s.buildKGSections(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	knowledgeGraphCache.put(tenantID, sections)
	return sections, nil
}

func writeMarkdown(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// kgSectionKind classifies a section so the per-request compressor (see
// kg_compress.go) can apply force-include rules. Baseline + majority
// groups are summary aggregates; node sections describe one machine.
type kgSectionKind int

const (
	kgSectionBaseline kgSectionKind = iota
	kgSectionMajority
	kgSectionNode
	kgSectionLookup
)

// kgSection is the canonical building block emitted by buildKGSections.
// We render the markdown body once at build-time so the compressor can
// cheaply re-pack sections for each user question; the slice itself is
// what the 5-minute cache holds.
type kgSection struct {
	Kind kgSectionKind
	// Identifiers used by the compressor's force-include + scoring
	// passes. Hostname / NodeID may be empty on aggregate sections.
	Hostname string
	NodeID   string
	PublicIP string
	// Tokens is the pre-tokenized body used for scoring against the
	// question. Lowercased alphanumeric word split, no stopwords.
	Tokens []string
	// Markdown is the rendered body, ready to concatenate.
	Markdown string
}

// buildKGSections is the build-time half of the two-stage compression
// pipeline. It dedupes per-node sections by (os, arch, agent, state):
// when ≥ 5 nodes share the same tuple AND ≥ 80% of them share one state
// the group collapses into a single majority summary section; outliers
// (or anything under the threshold) keep full per-node sections.
//
// The output is deliberately small for homogeneous fleets (a 1000-node
// tenant where everything is Linux/amd64/agent-1.2.3/active collapses
// to one majority section) which is what makes the per-request
// compressor's 8K-token budget achievable.
func (s *Server) buildKGSections(ctx context.Context, tenantID uuid.UUID) ([]kgSection, error) {
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	if tenant == nil {
		return nil, fmt.Errorf("tenant not found")
	}

	nodes, _, err := s.store.ListNodes(ctx, tenantID, "", 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	services, err := s.store.ListNodeServicesForTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	servicesByNode := make(map[uuid.UUID][]storage.NodeService, len(nodes))
	for _, svc := range services {
		servicesByNode[svc.NodeID] = append(servicesByNode[svc.NodeID], svc)
	}

	// Stable ordering — nodes by hostname for deterministic output.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Hostname < nodes[j].Hostname })

	sections := make([]kgSection, 0, len(nodes)+1)
	sections = append(sections, baselineSection(tenant.Name, nodes, services))

	if len(nodes) == 0 {
		return sections, nil
	}

	// Group nodes by (os, arch, agent) — state participates in the
	// majority test inside the group rather than the key, so a group
	// with mostly-active nodes can still summarize even when a handful
	// are pending.
	type groupKey struct {
		os, arch, agent string
	}
	groups := make(map[groupKey][]storage.Node, 8)
	order := make([]groupKey, 0, 8)
	for _, n := range nodes {
		k := groupKey{os: n.OS.String, arch: n.Arch.String, agent: n.AgentVersion.String}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], n)
	}

	const majorityMin = 5
	const majorityFrac = 0.80

	for _, k := range order {
		members := groups[k]

		// Find the dominant state inside this group.
		stateCount := make(map[string]int, 4)
		for _, n := range members {
			stateCount[n.State]++
		}
		var domState string
		var domCount int
		for st, c := range stateCount {
			if c > domCount {
				domState, domCount = st, c
			}
		}

		canSummarize := len(members) >= majorityMin && float64(domCount)/float64(len(members)) >= majorityFrac
		if !canSummarize {
			for _, n := range members {
				sections = append(sections, fullNodeSection(n, servicesByNode[n.ID]))
			}
			continue
		}

		// Majority group: emit one summary + one full section per outlier.
		majority := make([]storage.Node, 0, domCount)
		outliers := make([]storage.Node, 0, len(members)-domCount)
		for _, n := range members {
			if n.State == domState {
				majority = append(majority, n)
			} else {
				outliers = append(outliers, n)
			}
		}
		sections = append(sections, majoritySection(k.os, k.arch, k.agent, domState, majority))
		for _, n := range majority {
			sections = append(sections, lookupNodeSection(n))
		}
		for _, n := range outliers {
			sections = append(sections, fullNodeSection(n, servicesByNode[n.ID]))
		}
	}

	return sections, nil
}

// baselineSection is the always-included fleet header. It carries the
// tenant name, node count, service count, and a build timestamp.
func baselineSection(tenantName string, nodes []storage.Node, services []storage.NodeService) kgSection {
	var b strings.Builder
	fmt.Fprintf(&b, "# Knowledge graph — %s\n\n", tenantName)
	fmt.Fprintf(&b, "_Generated %s · %d nodes · %d listening services._\n\n",
		time.Now().UTC().Format(time.RFC3339), len(nodes), len(services))
	b.WriteString("This document describes every node enrolled under this tenant: " +
		"OS, agent version, listening services and their detected URLs, and " +
		"firewall posture. It is generated server-side from the agent " +
		"telemetry pipeline and is intended as grounded context for the " +
		"natural-language Ask surface.\n\n")
	if len(nodes) == 0 {
		b.WriteString("> No nodes are currently enrolled under this tenant.\n")
	}
	md := b.String()
	return kgSection{
		Kind:     kgSectionBaseline,
		Tokens:   tokenizeForKG(md),
		Markdown: md,
	}
}

// majoritySection collapses a homogeneous group into one line. The
// node-name list is included so an operator can still page through
// individual hostnames; if the list is huge we cap it.
func majoritySection(os, arch, agent, state string, members []storage.Node) kgSection {
	const maxNames = 25
	names := make([]string, 0, len(members))
	for _, n := range members {
		names = append(names, nodeDisplayName(n))
	}
	sort.Strings(names)
	shown := names
	tail := ""
	if len(names) > maxNames {
		shown = names[:maxNames]
		tail = fmt.Sprintf(", … (+%d more)", len(names)-maxNames)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Fleet baseline (%d nodes)\n\n", len(members))
	fmt.Fprintf(&b, "%d of %d nodes match: %s/%s/%s/%s.\n\n",
		len(members), len(members),
		strOrUnknown(os), strOrUnknown(arch), strOrUnknown(agent), strOrUnknown(state))
	fmt.Fprintf(&b, "Nodes: %s%s.\n\n", strings.Join(shown, ", "), tail)
	md := b.String()
	return kgSection{
		Kind:     kgSectionMajority,
		Tokens:   tokenizeForKG(md),
		Markdown: md,
	}
}

// lookupNodeSection is a compact exact-match index for nodes represented by a
// majority summary. It is skipped during normal packing and only included when
// a question names the node by UUID, hostname, or public IP.
func lookupNodeSection(n storage.Node) kgSection {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", nodeDisplayName(n))
	fmt.Fprintf(&b, "- **id:** `%s`\n", n.ID.String())
	fmt.Fprintf(&b, "- **state:** %s\n", n.State)
	if v := n.PublicIP.String; v != "" {
		fmt.Fprintf(&b, "- **public ip:** %s\n", v)
	}
	md := b.String()
	return kgSection{
		Kind:     kgSectionLookup,
		Hostname: n.Hostname,
		NodeID:   n.ID.String(),
		PublicIP: n.PublicIP.String,
		Tokens:   tokenizeForKG(md),
		Markdown: md,
	}
}

// fullNodeSection renders one node + its listening services as the
// original per-node block. This is the bloated shape we're trying to
// avoid emitting too many of.
func fullNodeSection(n storage.Node, nodeServices []storage.NodeService) kgSection {
	sort.Slice(nodeServices, func(i, j int) bool { return nodeServices[i].Port < nodeServices[j].Port })

	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", nodeDisplayName(n))
	fmt.Fprintf(&b, "- **id:** `%s`\n", n.ID.String())
	fmt.Fprintf(&b, "- **state:** %s\n", n.State)
	if v := n.OS.String; v != "" {
		fmt.Fprintf(&b, "- **os:** %s", v)
		if a := n.Arch.String; a != "" {
			fmt.Fprintf(&b, " (%s)", a)
		}
		b.WriteString("\n")
	}
	if v := n.AgentVersion.String; v != "" {
		fmt.Fprintf(&b, "- **agent:** %s\n", v)
	}
	if v := n.PublicIP.String; v != "" {
		fmt.Fprintf(&b, "- **public ip:** %s\n", v)
	}
	if n.LastSeenAt != nil {
		fmt.Fprintf(&b, "- **last seen:** %s\n", n.LastSeenAt.UTC().Format(time.RFC3339))
	}

	if len(nodeServices) == 0 {
		b.WriteString("\n_No listening services discovered yet._\n\n")
	} else {
		b.WriteString("\n| Port | Process | Kind | Server | URL |\n")
		b.WriteString("|---:|---|---|---|---|\n")
		for _, svc := range nodeServices {
			url := serviceURL(svc, n)
			server := ""
			if svc.ProbeServer != nil {
				server = *svc.ProbeServer
			}
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n",
				svc.Port,
				strOrDash(svc.Process),
				strOrDash(svc.ServiceKind),
				strOrDash(server),
				strOrDash(url),
			)
		}
		b.WriteString("\n")
	}

	md := b.String()
	return kgSection{
		Kind:     kgSectionNode,
		Hostname: n.Hostname,
		NodeID:   n.ID.String(),
		PublicIP: n.PublicIP.String,
		Tokens:   tokenizeForKG(md),
		Markdown: md,
	}
}

// renderKGSections concatenates every section's pre-rendered markdown.
// The full-KG markdown endpoint uses this; the per-request compressor
// uses compressForQuery instead (which picks a subset).
func renderKGSections(sections []kgSection) string {
	var b strings.Builder
	for _, s := range sections {
		b.WriteString(s.Markdown)
	}
	return b.String()
}

func strOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func nodeDisplayName(n storage.Node) string {
	if n.Hostname != "" {
		return n.Hostname
	}
	return n.ID.String()
}

func serviceURL(svc storage.NodeService, n storage.Node) string {
	if svc.ProbeStatus == nil {
		return ""
	}
	// listen_addr tells us whether the service is reachable off-host.
	// Loopback-only binds were probed locally by the agent; surfacing a
	// public URL for them would lie. Operators only see clickable URLs
	// for services that are actually exposed.
	bind := svc.ListenAddr
	if bind == "127.0.0.1" || bind == "::1" || strings.HasPrefix(bind, "127.") {
		return ""
	}
	host := n.PublicIP.String
	if host == "" {
		host = nodeDisplayName(n)
	}
	if host == "" {
		return ""
	}
	scheme := "http"
	if svc.Port == 443 || svc.Port == 8443 || strings.Contains(strings.ToLower(svc.ServiceKind), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d/", scheme, host, svc.Port)
}

func strOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// knowledgeGraphCacheImpl is a small per-tenant in-process cache. 5-minute
// TTL plus an explicit invalidate hook called on services upload.
//
// The cached value is the dedupped []kgSection slice rather than the
// rendered string: the per-request /ai/ask path needs to re-pack
// sections against the user's question and would otherwise have to
// reparse markdown. The full-KG endpoint just calls renderKGSections.
type knowledgeGraphCacheImpl struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[uuid.UUID]knowledgeGraphCacheEntry
}

type knowledgeGraphCacheEntry struct {
	sections  []kgSection
	expiresAt time.Time
}

var knowledgeGraphCache = &knowledgeGraphCacheImpl{
	ttl: 5 * time.Minute,
	m:   make(map[uuid.UUID]knowledgeGraphCacheEntry),
}

func (c *knowledgeGraphCacheImpl) get(id uuid.UUID) ([]kgSection, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.m[id]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.sections, true
}

func (c *knowledgeGraphCacheImpl) put(id uuid.UUID, sections []kgSection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[id] = knowledgeGraphCacheEntry{sections: sections, expiresAt: time.Now().Add(c.ttl)}
}

func (c *knowledgeGraphCacheImpl) invalidate(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, id)
}
