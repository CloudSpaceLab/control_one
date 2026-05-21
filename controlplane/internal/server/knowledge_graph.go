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
)

// nodeServicesRequest is the agent's payload when reporting an inventory cycle.
// Empty `services` means "no listening services discovered" (clears table).
type nodeServicesRequest struct {
	Services []nodeServiceItem `json:"services"`
}

type nodeServiceItem struct {
	PID              int      `json:"pid"`
	Process          string   `json:"process"`
	BinaryPath       string   `json:"binary_path"`
	WorkingDir       string   `json:"working_dir,omitempty"`
	CommandLine      string   `json:"command_line,omitempty"`
	ListenAddr       string   `json:"listen_addr"`
	Port             int      `json:"port"`
	ServiceKind      string   `json:"service_kind"`
	ProbeStatus      *int     `json:"probe_status,omitempty"`
	ProbeServer      *string  `json:"probe_server,omitempty"`
	ProbeTitle       *string  `json:"probe_title,omitempty"`
	ProbeContentType *string  `json:"probe_content_type,omitempty"`
	AppRoot          string   `json:"app_root,omitempty"`
	AppProfileID     string   `json:"app_profile_id,omitempty"`
	AppName          string   `json:"app_name,omitempty"`
	AppConfidence    int      `json:"app_confidence,omitempty"`
	AppEvidence      []string `json:"app_evidence,omitempty"`
}

type nodeServiceResponse struct {
	ID               string   `json:"id"`
	NodeID           string   `json:"node_id"`
	TenantID         string   `json:"tenant_id"`
	PID              int      `json:"pid"`
	Process          string   `json:"process"`
	BinaryPath       string   `json:"binary_path"`
	WorkingDir       string   `json:"working_dir,omitempty"`
	CommandLine      string   `json:"command_line,omitempty"`
	ListenAddr       string   `json:"listen_addr"`
	Port             int      `json:"port"`
	ServiceKind      string   `json:"service_kind"`
	ProbeStatus      *int     `json:"probe_status,omitempty"`
	ProbeServer      *string  `json:"probe_server,omitempty"`
	ProbeTitle       *string  `json:"probe_title,omitempty"`
	ProbeContentType *string  `json:"probe_content_type,omitempty"`
	AppRoot          string   `json:"app_root,omitempty"`
	AppProfileID     string   `json:"app_profile_id,omitempty"`
	AppName          string   `json:"app_name,omitempty"`
	AppConfidence    int      `json:"app_confidence,omitempty"`
	AppEvidence      []string `json:"app_evidence,omitempty"`
	ObservedAt       string   `json:"observed_at"`
}

func newNodeServiceResponse(svc storage.NodeService) nodeServiceResponse {
	return nodeServiceResponse{
		ID:               svc.ID.String(),
		NodeID:           svc.NodeID.String(),
		TenantID:         svc.TenantID.String(),
		PID:              svc.PID,
		Process:          svc.Process,
		BinaryPath:       svc.BinaryPath,
		WorkingDir:       svc.WorkingDir,
		CommandLine:      svc.CommandLine,
		ListenAddr:       svc.ListenAddr,
		Port:             svc.Port,
		ServiceKind:      svc.ServiceKind,
		ProbeStatus:      svc.ProbeStatus,
		ProbeServer:      svc.ProbeServer,
		ProbeTitle:       svc.ProbeTitle,
		ProbeContentType: svc.ProbeContentType,
		AppRoot:          svc.AppRoot,
		AppProfileID:     svc.AppProfileID,
		AppName:          svc.AppName,
		AppConfidence:    svc.AppConfidence,
		AppEvidence:      svc.AppEvidence,
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
			WorkingDir:       svc.WorkingDir,
			CommandLine:      svc.CommandLine,
			ListenAddr:       svc.ListenAddr,
			Port:             svc.Port,
			ServiceKind:      svc.ServiceKind,
			ProbeStatus:      svc.ProbeStatus,
			ProbeServer:      svc.ProbeServer,
			ProbeTitle:       svc.ProbeTitle,
			ProbeContentType: svc.ProbeContentType,
			AppRoot:          svc.AppRoot,
			AppProfileID:     svc.AppProfileID,
			AppName:          svc.AppName,
			AppConfidence:    svc.AppConfidence,
			AppEvidence:      sanitizeStringSlice(svc.AppEvidence, 12),
		})
	}

	if err := s.store.ReplaceNodeServices(r.Context(), nodeID, node.TenantID, services); err != nil {
		s.logger.Error("replace node services", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
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
	webserversByNode := map[uuid.UUID][]storage.WebserverInstance{}
	if store, ok := s.store.(interface {
		ListWebserverInstances(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.WebserverInstance, int, error)
	}); ok {
		if webservers, _, err := store.ListWebserverInstances(ctx, tenantID, uuid.Nil, 1000, 0); err == nil {
			for _, web := range webservers {
				webserversByNode[web.NodeID] = append(webserversByNode[web.NodeID], web)
			}
		} else {
			s.logger.Warn("list webservers for knowledge graph", zap.Error(err))
		}
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
				sections = append(sections, fullNodeSection(n, servicesByNode[n.ID], webserversByNode[n.ID]))
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
			sections = append(sections, fullNodeSection(n, servicesByNode[n.ID], webserversByNode[n.ID]))
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
func fullNodeSection(n storage.Node, nodeServices []storage.NodeService, webservers []storage.WebserverInstance) kgSection {
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
		b.WriteString("\n| Port | Process | Kind | App root | App | Server | URL |\n")
		b.WriteString("|---:|---|---|---|---|---|---|\n")
		for _, svc := range nodeServices {
			url := serviceURL(svc, n)
			server := ""
			if svc.ProbeServer != nil {
				server = *svc.ProbeServer
			}
			appName := svc.AppName
			if appName == "" {
				appName = svc.AppProfileID
			}
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s |\n",
				svc.Port,
				strOrDash(svc.Process),
				strOrDash(svc.ServiceKind),
				strOrDash(svc.AppRoot),
				strOrDash(appName),
				strOrDash(server),
				strOrDash(url),
			)
		}
		b.WriteString("\n")
	}

	if roots := kgApplicationRoots(webservers); len(roots) > 0 {
		b.WriteString("\nApplication roots:\n")
		for _, root := range roots {
			fmt.Fprintf(&b, "- `%s`", root.path)
			if root.app != "" {
				fmt.Fprintf(&b, " â€” %s", root.app)
			}
			if root.source != "" {
				fmt.Fprintf(&b, " (%s)", root.source)
			}
			if root.vhost != "" {
				fmt.Fprintf(&b, " vhost=%s", root.vhost)
			}
			if root.evidence != "" {
				fmt.Fprintf(&b, " evidence=%s", root.evidence)
			}
			b.WriteString("\n")
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

type kgAppRoot struct {
	path     string
	app      string
	source   string
	vhost    string
	evidence string
}

func kgApplicationRoots(webservers []storage.WebserverInstance) []kgAppRoot {
	seen := map[string]struct{}{}
	var out []kgAppRoot
	add := func(path, app, source, vhost string, evidence []string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		key := strings.ToLower(path + "|" + vhost + "|" + source)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, kgAppRoot{
			path:     path,
			app:      app,
			source:   source,
			vhost:    vhost,
			evidence: strings.Join(sanitizeStringSlice(evidence, 4), ", "),
		})
	}
	for _, web := range webservers {
		for _, vhost := range web.VHosts {
			path := vhostValue(vhost, "document_root", "root", "web_root", "app_root", "path")
			if path == "" {
				continue
			}
			evidence := anyStringSlice(vhost["evidence"])
			evidence = append(evidence, anyStringSlice(vhost["detection_evidence"])...)
			source := "webserver_config"
			if vhostValue(vhost, "directive") == "filesystem_scan" || containsString(evidence, "webserver_config:filesystem_scan") {
				source = "filesystem_scan"
			}
			app := firstNonEmpty(vhostValue(vhost, "application_name", "app", "application"), vhostValue(vhost, "application_type", "profile_id"))
			add(path, app, source, vhostValue(vhost, "vhost", "server_name", "host", "name"), evidence)
		}
		if raw, ok := web.Capabilities["application_roots"]; ok {
			for _, item := range anyMapSlice(raw) {
				path := vhostValue(item, "path", "document_root", "root", "app_root")
				if path == "" {
					continue
				}
				evidence := anyStringSlice(item["evidence"])
				source := "webserver_config"
				if vhostValue(item, "directive") == "filesystem_scan" || containsString(evidence, "webserver_config:filesystem_scan") {
					source = "filesystem_scan"
				}
				app := firstNonEmpty(vhostValue(item, "application_name", "app", "application"), vhostValue(item, "application_type", "profile_id"))
				add(path, app, source, vhostValue(item, "vhost", "server_name", "host", "name"), evidence)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func anyMapSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]map[string]any); ok {
			return typed
		}
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
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
