package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type nodeDocumentationResponse struct {
	Node           nodeResponse                 `json:"node"`
	Services       []nodeServiceResponse        `json:"services"`
	Firewall       *nodeFirewallStateResponse   `json:"firewall,omitempty"`
	Health         *nodeHealthScoreResponse     `json:"health,omitempty"`
	RecentAlerts   []alertResponse              `json:"recent_alerts"`
	TopConnections []connectionDocumentationRow `json:"top_connections"`
}

type nodeFirewallStateResponse struct {
	NodeID       string                 `json:"node_id"`
	FirewallType string                 `json:"firewall_type"`
	Enabled      bool                   `json:"enabled"`
	Rules        []storage.FirewallRule `json:"rules"`
	Zones        []storage.FirewallZone `json:"zones"`
	ObservedAt   string                 `json:"observed_at,omitempty"`
}

type connectionDocumentationRow struct {
	StartedAt   string `json:"started_at,omitempty"`
	Direction   string `json:"direction,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	SrcIP       string `json:"src_ip,omitempty"`
	SrcPort     int    `json:"src_port,omitempty"`
	DstIP       string `json:"dst_ip,omitempty"`
	DstPort     int    `json:"dst_port,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	BytesIn     int64  `json:"bytes_in,omitempty"`
	BytesOut    int64  `json:"bytes_out,omitempty"`
	ThreatMatch bool   `json:"threat_match,omitempty"`
}

func (s *Server) handleNodeDocumentation(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID, markdown bool) {
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

	doc, err := s.buildNodeDocumentation(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("build node documentation", zap.Error(err), zap.String("node_id", nodeID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if doc == nil {
		http.NotFound(w, r)
		return
	}
	if markdown {
		writeMarkdown(w, renderNodeDocumentationMarkdown(*doc))
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) buildNodeDocumentation(ctx context.Context, nodeID uuid.UUID) (*nodeDocumentationResponse, error) {
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	if node == nil {
		return nil, nil
	}

	services, err := s.store.ListNodeServicesForNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node services: %w", err)
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Port < services[j].Port })

	out := &nodeDocumentationResponse{
		Node:           nodeResponseFromModel(*node),
		Services:       make([]nodeServiceResponse, 0, len(services)),
		RecentAlerts:   []alertResponse{},
		TopConnections: []connectionDocumentationRow{},
	}
	for _, svc := range services {
		out.Services = append(out.Services, newNodeServiceResponse(svc))
	}

	firewall, err := s.store.GetNodeFirewallState(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get firewall state: %w", err)
	}
	if firewall != nil {
		out.Firewall = newNodeFirewallStateResponse(*firewall)
	}

	health, err := s.store.GetNodeHealthScore(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get health score: %w", err)
	}
	if health != nil {
		h := newNodeHealthScoreResponse(*health)
		out.Health = &h
	}

	alerts, _, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: node.TenantID, NodeID: nodeID}, 5, 0)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	for _, alert := range alerts {
		out.RecentAlerts = append(out.RecentAlerts, newAlertResponse(alert))
	}

	if s.dorisClient != nil {
		until := time.Now().UTC()
		since := until.Add(-24 * time.Hour)
		rows, err := s.dorisClient.ListConnectionsForNode(ctx, node.TenantID.String(), nodeID.String(), since, until, 10, false)
		if err != nil {
			s.logger.Warn("list node documentation connections", zap.Error(err), zap.String("node_id", nodeID.String()))
		} else {
			out.TopConnections = make([]connectionDocumentationRow, 0, len(rows))
			for _, row := range rows {
				out.TopConnections = append(out.TopConnections, newConnectionDocumentationRow(row))
			}
		}
	}

	return out, nil
}

func newNodeFirewallStateResponse(st storage.NodeFirewallState) *nodeFirewallStateResponse {
	out := &nodeFirewallStateResponse{
		NodeID:       st.NodeID.String(),
		FirewallType: st.FirewallType,
		Enabled:      st.Enabled,
		Rules:        st.Rules,
		Zones:        st.Zones,
	}
	if !st.ObservedAt.IsZero() {
		out.ObservedAt = st.ObservedAt.UTC().Format(time.RFC3339)
	}
	if out.Rules == nil {
		out.Rules = []storage.FirewallRule{}
	}
	if out.Zones == nil {
		out.Zones = []storage.FirewallZone{}
	}
	return out
}

func newConnectionDocumentationRow(row doris.ConnectionRow) connectionDocumentationRow {
	out := connectionDocumentationRow{
		Direction:   row.Direction,
		ProcessName: row.ProcessName,
		SrcIP:       row.SrcIP,
		SrcPort:     row.SrcPort,
		DstIP:       row.DstIP,
		DstPort:     row.DstPort,
		Protocol:    row.Protocol,
		BytesIn:     row.BytesIn,
		BytesOut:    row.BytesOut,
		ThreatMatch: row.ThreatMatch,
	}
	if !row.StartedAt.IsZero() {
		out.StartedAt = row.StartedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func renderNodeDocumentationMarkdown(doc nodeDocumentationResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Node documentation - %s\n\n", doc.Node.Hostname)
	fmt.Fprintf(&b, "- **id:** `%s`\n", doc.Node.ID)
	fmt.Fprintf(&b, "- **state:** %s\n", doc.Node.State)
	if doc.Node.OS != nil {
		fmt.Fprintf(&b, "- **os:** %s\n", *doc.Node.OS)
	}
	if doc.Node.PublicIP != nil {
		fmt.Fprintf(&b, "- **public ip:** %s\n", *doc.Node.PublicIP)
	}
	if doc.Node.AgentVersion != nil {
		fmt.Fprintf(&b, "- **agent:** %s\n", *doc.Node.AgentVersion)
	}

	b.WriteString("\n## Firewall\n\n")
	if doc.Firewall == nil {
		b.WriteString("No firewall snapshot reported.\n")
	} else {
		fmt.Fprintf(&b, "- **type:** %s\n- **enabled:** %t\n- **rules:** %d\n", doc.Firewall.FirewallType, doc.Firewall.Enabled, len(doc.Firewall.Rules))
	}

	b.WriteString("\n## Health\n\n")
	if doc.Health == nil {
		b.WriteString("No health score computed.\n")
	} else {
		fmt.Fprintf(&b, "- **score:** %d\n- **risk:** %s\n", doc.Health.Score, doc.Health.RiskLevel)
	}

	b.WriteString("\n## Listening services\n\n")
	if len(doc.Services) == 0 {
		b.WriteString("No listening services discovered.\n")
	} else {
		b.WriteString("| Port | Process | Kind |\n|---:|---|---|\n")
		for _, svc := range doc.Services {
			fmt.Fprintf(&b, "| %d | %s | %s |\n", svc.Port, strOrDash(svc.Process), strOrDash(svc.ServiceKind))
		}
	}

	b.WriteString("\n## Recent alerts\n\n")
	if len(doc.RecentAlerts) == 0 {
		b.WriteString("No recent alerts.\n")
	} else {
		for _, alert := range doc.RecentAlerts {
			fmt.Fprintf(&b, "- **%s:** %s (%s)\n", alert.Severity, alert.Title, alert.State)
		}
	}

	return b.String()
}
