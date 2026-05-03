package server

import (
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

	knowledgeGraphCache.invalidate(node.TenantID)

	w.WriteHeader(http.StatusNoContent)
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

	if cached, ok := knowledgeGraphCache.get(tenantID); ok {
		writeMarkdown(w, cached)
		return
	}

	doc, err := s.buildKnowledgeGraph(r, tenantID)
	if err != nil {
		s.logger.Error("build knowledge graph", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	knowledgeGraphCache.put(tenantID, doc)
	writeMarkdown(w, doc)
}

func writeMarkdown(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (s *Server) buildKnowledgeGraph(r *http.Request, tenantID uuid.UUID) (string, error) {
	tenant, err := s.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		return "", fmt.Errorf("get tenant: %w", err)
	}
	if tenant == nil {
		return "", fmt.Errorf("tenant not found")
	}

	nodes, _, err := s.store.ListNodes(r.Context(), tenantID, "", 1000, 0)
	if err != nil {
		return "", fmt.Errorf("list nodes: %w", err)
	}

	services, err := s.store.ListNodeServicesForTenant(r.Context(), tenantID)
	if err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}

	servicesByNode := make(map[uuid.UUID][]storage.NodeService, len(nodes))
	for _, svc := range services {
		servicesByNode[svc.NodeID] = append(servicesByNode[svc.NodeID], svc)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Knowledge graph — %s\n\n", tenant.Name)
	fmt.Fprintf(&b, "_Generated %s · %d nodes · %d listening services._\n\n",
		time.Now().UTC().Format(time.RFC3339), len(nodes), len(services))
	b.WriteString("This document describes every node enrolled under this tenant: " +
		"OS, agent version, listening services and their detected URLs, and " +
		"firewall posture. It is generated server-side from the agent " +
		"telemetry pipeline and is intended as grounded context for the " +
		"natural-language Ask surface.\n\n")

	if len(nodes) == 0 {
		b.WriteString("> No nodes are currently enrolled under this tenant.\n")
		return b.String(), nil
	}

	// Stable ordering — nodes by hostname, services by port.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Hostname < nodes[j].Hostname })

	for _, n := range nodes {
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

		nodeServices := servicesByNode[n.ID]
		sort.Slice(nodeServices, func(i, j int) bool { return nodeServices[i].Port < nodeServices[j].Port })
		if len(nodeServices) == 0 {
			b.WriteString("\n_No listening services discovered yet._\n\n")
			continue
		}
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

	return b.String(), nil
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
	scheme := "http"
	if svc.Port == 443 || svc.Port == 8443 || strings.Contains(strings.ToLower(svc.ServiceKind), "https") {
		scheme = "https"
	}
	host := n.PublicIP.String
	if host == "" {
		host = nodeDisplayName(n)
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
type knowledgeGraphCacheImpl struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[uuid.UUID]knowledgeGraphCacheEntry
}

type knowledgeGraphCacheEntry struct {
	body      string
	expiresAt time.Time
}

var knowledgeGraphCache = &knowledgeGraphCacheImpl{
	ttl: 5 * time.Minute,
	m:   make(map[uuid.UUID]knowledgeGraphCacheEntry),
}

func (c *knowledgeGraphCacheImpl) get(id uuid.UUID) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.m[id]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.body, true
}

func (c *knowledgeGraphCacheImpl) put(id uuid.UUID, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[id] = knowledgeGraphCacheEntry{body: body, expiresAt: time.Now().Add(c.ttl)}
}

func (c *knowledgeGraphCacheImpl) invalidate(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, id)
}
