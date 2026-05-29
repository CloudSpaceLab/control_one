package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

type privateAccessStore interface {
	ListNodeServicesForTenant(context.Context, uuid.UUID) ([]storage.NodeService, error)
	UpsertPrivateAccessSnapshot(context.Context, uuid.UUID, privateaccess.Snapshot) (*storage.PrivateAccessSnapshotRecord, error)
	ListPrivateAccessSnapshots(context.Context, uuid.UUID) ([]storage.PrivateAccessSnapshotRecord, error)
	ReplacePrivateAccessExposureFindings(context.Context, uuid.UUID, []privateaccess.ExposureFinding, time.Time) ([]storage.PrivateAccessExposureFindingRecord, error)
	ListPrivateAccessExposureFindings(context.Context, uuid.UUID, bool, int, int) ([]storage.PrivateAccessExposureFindingRecord, error)
}

type privateAccessNodeLister interface {
	ListNodes(context.Context, uuid.UUID, string, int, int) ([]storage.Node, int, error)
}

type privateAccessFirewallGetter interface {
	GetNodeFirewallState(context.Context, uuid.UUID) (*storage.NodeFirewallState, error)
}

type privateAccessLoadBalancerLister interface {
	ListClusterLBRegistrationsForNode(context.Context, uuid.UUID) ([]storage.ClusterLBRegistration, error)
}

type privateAccessExposureFindingGetter interface {
	GetPrivateAccessExposureFinding(context.Context, uuid.UUID, uuid.UUID) (*storage.PrivateAccessExposureFindingRecord, error)
}

type privateAccessExposureContext struct {
	Nodes            []storage.Node
	Firewalls        map[uuid.UUID]*storage.NodeFirewallState
	LoadBalancers    map[uuid.UUID][]storage.ClusterLBRegistration
	NodeContextKnown bool
	Now              time.Time
}

func (s *Server) handlePrivateAccessSnapshots(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(privateAccessStore)
	if !ok {
		http.Error(w, "private access store unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		snapshots, err := store.ListPrivateAccessSnapshots(r.Context(), tenantID)
		if err != nil {
			http.Error(w, "list private access snapshots", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": snapshots})
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleOperator, roleAdmin)
		if !ok {
			return
		}
		var body struct {
			Snapshot privateaccess.Snapshot `json:"snapshot"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		record, err := store.UpsertPrivateAccessSnapshot(r.Context(), tenantID, body.Snapshot)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, record)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePrivateAccessExposureFindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	store, ok := s.store.(privateAccessStore)
	if !ok {
		http.Error(w, "private access store unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	openOnly := strings.TrimSpace(r.URL.Query().Get("open_only")) != "false"
	limit := parsePrivateAccessIntQuery(r, "limit", 100)
	offset := parsePrivateAccessIntQuery(r, "offset", 0)
	findings, err := store.ListPrivateAccessExposureFindings(r.Context(), tenantID, openOnly, limit, offset)
	if err != nil {
		http.Error(w, "list private access findings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": findings})
}

func (s *Server) handlePrivateAccessExposureFindingSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/private-access/exposure/findings/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 2 && parts[1] == "soc-case" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		id, err := uuid.Parse(parts[0])
		if err != nil {
			http.Error(w, "invalid finding id", http.StatusBadRequest)
			return
		}
		s.handlePrivateAccessExposureFindingSOCCase(w, r, id)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handlePrivateAccessExposureFindingSOCCase(w http.ResponseWriter, r *http.Request, findingID uuid.UUID) {
	getter, ok := s.store.(privateAccessExposureFindingGetter)
	if !ok {
		http.Error(w, "private access finding store unavailable", http.StatusServiceUnavailable)
		return
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "case store unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	finding, err := getter.GetPrivateAccessExposureFinding(r.Context(), tenantID, findingID)
	if err != nil {
		http.Error(w, "get private access finding", http.StatusInternalServerError)
		return
	}
	if finding == nil {
		http.Error(w, "private access finding not found", http.StatusNotFound)
		return
	}
	evidence, err := json.Marshal(privateAccessFindingSOCCaseEvidence(*finding))
	if err != nil {
		http.Error(w, "marshal private access finding evidence", http.StatusInternalServerError)
		return
	}
	nodeID := uuid.Nil
	if finding.NodeID != nil {
		nodeID = *finding.NodeID
	}
	row, err := backend.CreateAIInvestigation(r.Context(), storage.CreateAIInvestigationParams{
		TenantID:         tenantID,
		NodeID:           nodeID,
		TriggerType:      "private_access_exposure",
		TriggerEventType: "private_access_exposure." + strings.TrimSpace(finding.Type),
		TriggerDedupKey:  "private_access_exposure:" + finding.ID.String(),
		Severity:         firstNonEmptyString(finding.Severity, "medium"),
		Summary:          privateAccessFindingSOCCaseSummary(*finding),
		Evidence:         evidence,
		Status:           storage.AIInvestigationStatusOpen,
	})
	if err != nil {
		http.Error(w, "create private access SOC case", http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "private_access.exposure.soc_case_opened", "private_access_exposure_finding", finding.ID.String(), map[string]any{
		"case_id":      row.ID.String(),
		"finding_type": finding.Type,
		"severity":     finding.Severity,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"case":    newSOCCaseResponse(*row),
		"finding": finding,
	})
}

func (s *Server) handlePrivateAccessExposureReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	store, ok := s.store.(privateAccessStore)
	if !ok {
		http.Error(w, "private access store unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleOperator, roleAdmin)
	if !ok {
		return
	}
	services, err := store.ListNodeServicesForTenant(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "list node services", http.StatusInternalServerError)
		return
	}
	snapshots, err := store.ListPrivateAccessSnapshots(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "list private access snapshots", http.StatusInternalServerError)
		return
	}
	exposureContext, err := loadPrivateAccessExposureContext(r.Context(), store, tenantID)
	if err != nil {
		http.Error(w, "list private access exposure context", http.StatusInternalServerError)
		return
	}
	models := make([]privateaccess.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		models = append(models, snapshot.Snapshot)
	}
	now := time.Now().UTC()
	exposureContext.Now = now
	observations := privateAccessObservationsFromNodeServicesWithContext(services, exposureContext)
	findings := privateaccess.ReconcileExposure(observations, models, privateaccess.ReconcileOptions{Now: now})
	records, err := store.ReplacePrivateAccessExposureFindings(r.Context(), tenantID, findings, now)
	if err != nil {
		http.Error(w, "store private access findings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"observations": len(observations),
		"snapshots":    len(snapshots),
		"findings":     records,
	})
}

func privateAccessObservationsFromNodeServices(services []storage.NodeService) []privateaccess.ExposureObservation {
	return privateAccessObservationsFromNodeServicesWithContext(services, privateAccessExposureContext{})
}

func privateAccessObservationsFromNodeServicesWithContext(services []storage.NodeService, exposureContext privateAccessExposureContext) []privateaccess.ExposureObservation {
	nodesByID := make(map[uuid.UUID]storage.Node, len(exposureContext.Nodes))
	for _, node := range exposureContext.Nodes {
		nodesByID[node.ID] = node
	}
	out := make([]privateaccess.ExposureObservation, 0, len(services))
	for _, svc := range services {
		protocol := "tcp"
		addr := normalizePrivateAccessListenAddr(svc.ListenAddr)
		name := strings.TrimSpace(svc.Process)
		if name == "" {
			name = strings.TrimSpace(svc.ServiceKind)
		}
		nodePublicIP := ""
		var nodeLabels map[string]any
		if node, ok := nodesByID[svc.NodeID]; ok && node.PublicIP.Valid {
			nodePublicIP = strings.TrimSpace(node.PublicIP.String)
			nodeLabels = node.Labels
		} else if node, ok := nodesByID[svc.NodeID]; ok {
			nodeLabels = node.Labels
		}
		firewallState := exposureContext.Firewalls[svc.NodeID]
		publiclyReachable, metadata := privateAccessServiceExposureMetadata(svc, nodePublicIP, exposureContext.NodeContextKnown, firewallState, exposureContext.Now, nodeLabels, exposureContext.LoadBalancers[svc.NodeID])
		metadata["process"] = svc.Process
		metadata["service_kind"] = svc.ServiceKind
		metadata["listen_addr"] = svc.ListenAddr
		out = append(out, privateaccess.ExposureObservation{
			NodeID:            svc.NodeID.String(),
			ServiceID:         svc.ID.String(),
			Name:              name,
			Address:           addr,
			Protocol:          protocol,
			Port:              svc.Port,
			PubliclyReachable: publiclyReachable,
			Metadata:          metadata,
		})
	}
	return out
}

func loadPrivateAccessExposureContext(ctx context.Context, store privateAccessStore, tenantID uuid.UUID) (privateAccessExposureContext, error) {
	var out privateAccessExposureContext
	lister, ok := store.(privateAccessNodeLister)
	if !ok {
		return out, nil
	}
	out.NodeContextKnown = true
	const pageSize = 500
	for offset := 0; ; offset += pageSize {
		nodes, total, err := lister.ListNodes(ctx, tenantID, "", pageSize, offset)
		if err != nil {
			return out, err
		}
		out.Nodes = append(out.Nodes, nodes...)
		if len(nodes) == 0 || len(nodes) < pageSize || len(out.Nodes) >= total {
			break
		}
	}
	getter, ok := store.(privateAccessFirewallGetter)
	if ok && len(out.Nodes) > 0 {
		out.Firewalls = make(map[uuid.UUID]*storage.NodeFirewallState, len(out.Nodes))
		for _, node := range out.Nodes {
			state, err := getter.GetNodeFirewallState(ctx, node.ID)
			if err != nil {
				return out, err
			}
			if state != nil {
				out.Firewalls[node.ID] = state
			}
		}
	}
	lbLister, ok := store.(privateAccessLoadBalancerLister)
	if ok && len(out.Nodes) > 0 {
		out.LoadBalancers = make(map[uuid.UUID][]storage.ClusterLBRegistration, len(out.Nodes))
		for _, node := range out.Nodes {
			rows, err := lbLister.ListClusterLBRegistrationsForNode(ctx, node.ID)
			if err != nil {
				return out, err
			}
			if len(rows) > 0 {
				out.LoadBalancers[node.ID] = rows
			}
		}
	}
	return out, nil
}

func privateAccessServiceExposureMetadata(svc storage.NodeService, nodePublicIP string, nodeContextKnown bool, firewallState *storage.NodeFirewallState, now time.Time, nodeLabels map[string]any, loadBalancers []storage.ClusterLBRegistration) (bool, map[string]string) {
	metadata := map[string]string{}
	nodePublicIP = strings.TrimSpace(nodePublicIP)
	if nodePublicIP != "" {
		metadata["node_public_ip"] = nodePublicIP
	}
	if path, ok := privateAccessPublicPath(svc.ListenAddr, nodePublicIP, nodeContextKnown); ok {
		metadata["public_path"] = path
	}
	if infraPath, infraMetadata := privateAccessInfrastructureExposureMetadata(nodeLabels, loadBalancers, svc.Port); len(infraMetadata) > 0 {
		for key, value := range infraMetadata {
			metadata[key] = value
		}
		if infraPath != "" {
			if metadata["public_path"] == "" {
				metadata["public_path"] = infraPath
			} else {
				metadata["infrastructure_public_path"] = infraPath
			}
		}
	}
	posture, blocksPublic, firewallEvidence := privateAccessFirewallExposurePosture(firewallState, now, "tcp", svc.Port)
	if posture != "" {
		metadata["firewall_posture"] = posture
	}
	if firewallEvidence != "" {
		metadata["firewall_evidence"] = firewallEvidence
	}
	return metadata["public_path"] != "" && !blocksPublic, metadata
}

func privateAccessPublicPath(listenAddr, nodePublicIP string, nodeContextKnown bool) (string, bool) {
	addr := normalizePrivateAccessListenAddr(listenAddr)
	if privateAccessIPCanBePublic(addr) {
		return "listener_public_ip", true
	}
	if nodePublicIP != "" && privateAccessIPCanBePublic(nodePublicIP) {
		if isPublicListener(listenAddr) {
			return "node_public_ip_wildcard_listener", true
		}
		if addr != "" && strings.EqualFold(addr, normalizePrivateAccessListenAddr(nodePublicIP)) {
			return "node_public_ip_bound_listener", true
		}
	}
	if !nodeContextKnown && isPublicListener(listenAddr) {
		return "wildcard_listener_node_context_unavailable", true
	}
	return "", false
}

func privateAccessIPCanBePublic(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), "[]")
	if value == "" {
		return false
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast()
}

func privateAccessFirewallExposurePosture(state *storage.NodeFirewallState, now time.Time, protocol string, port int) (string, bool, string) {
	if state == nil {
		return "unknown", false, ""
	}
	stale := state.ObservedAt.IsZero() || (!now.IsZero() && now.Sub(state.ObservedAt.UTC()) > 30*time.Minute)
	if !state.Enabled {
		if stale {
			return "stale_disabled", false, ""
		}
		return "disabled", false, ""
	}
	defaultDeny := firewallStateDefaultDeny(*state)
	if !defaultDeny {
		if stale {
			return "stale_default_allow", false, ""
		}
		return "enabled_default_allow", false, ""
	}
	if allowed, evidence := privateAccessFirewallAllowsPublicPort(*state, protocol, port); allowed {
		if stale {
			return "stale_default_deny_public_allow", false, evidence
		}
		return "default_deny_public_allow", false, evidence
	}
	if stale {
		return "stale_default_deny", false, ""
	}
	return "default_deny", true, ""
}

func privateAccessFirewallAllowsPublicPort(state storage.NodeFirewallState, protocol string, port int) (bool, string) {
	for _, rule := range state.Rules {
		structured := strings.TrimSpace(rule.Action+rule.Direction+rule.Protocol+rule.Port+rule.Source) != ""
		if !privateAccessFirewallRuleAllows(rule) || !privateAccessFirewallRuleInbound(rule) {
			continue
		}
		if !privateAccessFirewallRuleProtocolMatches(rule.Protocol, protocol) {
			continue
		}
		if structured {
			if !privateAccessFirewallRulePortMatches(rule.Port, port) {
				continue
			}
		} else if !privateAccessFirewallRawMentionsPort(rule.Raw, port) {
			continue
		}
		if !privateAccessFirewallSourceCanBePublic(rule.Source) {
			continue
		}
		return true, privateAccessFirewallRuleEvidence(rule)
	}
	return false, ""
}

func privateAccessFirewallRuleAllows(rule storage.FirewallRule) bool {
	action := strings.ToLower(strings.TrimSpace(rule.Action))
	if action != "" {
		return action == "allow" || action == "accept" || action == "pass"
	}
	raw := strings.ToLower(rule.Raw)
	return strings.Contains(raw, "allow") || strings.Contains(raw, "accept") || strings.Contains(raw, "pass")
}

func privateAccessFirewallRuleInbound(rule storage.FirewallRule) bool {
	direction := strings.ToLower(strings.TrimSpace(rule.Direction))
	switch direction {
	case "", "in", "input", "inbound", "incoming":
	default:
		return false
	}
	if direction != "" || rule.Raw == "" {
		return true
	}
	raw := " " + strings.ToLower(rule.Raw) + " "
	hasInbound := strings.Contains(raw, " in ") || strings.Contains(raw, "input") || strings.Contains(raw, "incoming")
	hasOutbound := strings.Contains(raw, " out ") || strings.Contains(raw, "output") || strings.Contains(raw, "outgoing")
	return hasInbound || !hasOutbound
}

func privateAccessFirewallRuleProtocolMatches(ruleProtocol, protocol string) bool {
	ruleProtocol = strings.ToLower(strings.TrimSpace(ruleProtocol))
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	return ruleProtocol == "" || ruleProtocol == "any" || protocol == "" || ruleProtocol == protocol
}

func privateAccessFirewallRulePortMatches(portSpec string, port int) bool {
	portSpec = strings.TrimSpace(portSpec)
	if portSpec == "" || port <= 0 {
		return true
	}
	for _, field := range strings.FieldsFunc(portSpec, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		if privateAccessFirewallPortFieldMatches(field, port) {
			return true
		}
	}
	return false
}

func privateAccessFirewallPortFieldMatches(field string, port int) bool {
	field = strings.TrimSpace(strings.ToLower(field))
	field = strings.TrimPrefix(strings.TrimPrefix(field, "tcp/"), "udp/")
	field = strings.TrimSuffix(strings.TrimSuffix(field, "/tcp"), "/udp")
	if field == "" {
		return false
	}
	if value, err := strconv.Atoi(field); err == nil {
		return value == port
	}
	var sep string
	if strings.Contains(field, ":") {
		sep = ":"
	} else if strings.Contains(field, "-") {
		sep = "-"
	}
	if sep == "" {
		return false
	}
	parts := strings.SplitN(field, sep, 2)
	if len(parts) != 2 {
		return false
	}
	start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	return startErr == nil && endErr == nil && port >= start && port <= end
}

func privateAccessFirewallRawMentionsPort(raw string, port int) bool {
	if port <= 0 {
		return true
	}
	want := strconv.Itoa(port)
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) {
		if field == want {
			return true
		}
	}
	return false
}

func privateAccessFirewallSourceCanBePublic(source string) bool {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		return true
	}
	switch source {
	case "*", "all", "any", "anywhere", "0.0.0.0/0", "::/0":
		return true
	}
	for _, field := range strings.FieldsFunc(source, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		field = strings.Trim(strings.TrimSpace(field), "[]")
		if field == "" || field == "(v6)" {
			continue
		}
		if prefix, err := netip.ParsePrefix(field); err == nil {
			addr := prefix.Addr().Unmap()
			if prefix.Bits() == 0 || (addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast()) {
				return true
			}
			continue
		}
		if privateAccessIPCanBePublic(field) {
			return true
		}
		if net.ParseIP(field) == nil {
			return true
		}
	}
	return false
}

func privateAccessFirewallRuleEvidence(rule storage.FirewallRule) string {
	if value := strings.TrimSpace(rule.Comment); value != "" {
		return compactPrivateAccessEvidence(value)
	}
	if value := strings.TrimSpace(rule.Raw); value != "" {
		return compactPrivateAccessEvidence(value)
	}
	parts := make([]string, 0, 5)
	for _, value := range []string{rule.Action, rule.Direction, rule.Protocol, rule.Port, rule.Source} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return compactPrivateAccessEvidence(strings.Join(parts, " "))
}

func compactPrivateAccessEvidence(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 160 {
		return value[:160]
	}
	return value
}

func privateAccessInfrastructureExposureMetadata(labels map[string]any, loadBalancers []storage.ClusterLBRegistration, port int) (string, map[string]string) {
	metadata := map[string]string{}
	activeLBs := privateAccessActiveLoadBalancerEvidence(loadBalancers)
	if len(activeLBs) > 0 {
		metadata["load_balancer_evidence"] = strings.Join(activeLBs, ";")
	}
	if cloudIP := privateAccessLabelString(labels,
		"control_one.cloud_public_ip",
		"cloud.public_ip",
		"cloud_public_ip",
		"aws.public_ip",
		"azure.public_ip",
		"gcp.public_ip",
	); privateAccessIPCanBePublic(cloudIP) && privateAccessPortAllowedByLabels(labels, port,
		"control_one.public_ports",
		"exposure.public_ports",
		"cloud.public_ports",
		"public_ports",
	) {
		metadata["cloud_public_ip"] = cloudIP
		return "cloud_public_ip_label", metadata
	}
	if privateAccessLabelBool(labels,
		"control_one.load_balancer_public",
		"load_balancer.public",
		"load_balancer_public",
		"lb.public",
		"cloud.load_balancer_public",
		"exposure.load_balancer_public",
	) && len(activeLBs) > 0 && privateAccessPortAllowedByLabels(labels, port,
		"control_one.load_balancer_public_ports",
		"load_balancer.public_ports",
		"lb.public_ports",
		"exposure.public_ports",
		"public_ports",
	) {
		if dns := privateAccessLabelString(labels, "load_balancer.public_dns", "lb.public_dns", "load_balancer.dns", "lb.dns"); dns != "" {
			metadata["load_balancer_public_dns"] = dns
		}
		return "public_load_balancer", metadata
	}
	if privateAccessLabelBool(labels,
		"control_one.nat_public_ingress",
		"nat.public_ingress",
		"nat_public_ingress",
		"cloud.nat_public_ingress",
		"exposure.nat_public_ingress",
	) && privateAccessPortAllowedByLabels(labels, port,
		"control_one.nat_public_ports",
		"nat.public_ports",
		"exposure.public_ports",
		"public_ports",
	) {
		if natID := privateAccessLabelString(labels, "nat.gateway_id", "nat_gateway_id", "cloud.nat_gateway_id"); natID != "" {
			metadata["nat_gateway_id"] = natID
		}
		if natIP := privateAccessLabelString(labels, "nat.public_ip", "nat_public_ip", "cloud.nat_public_ip"); natIP != "" {
			metadata["nat_public_ip"] = natIP
		}
		return "public_nat_ingress_label", metadata
	}
	return "", metadata
}

func privateAccessActiveLoadBalancerEvidence(loadBalancers []storage.ClusterLBRegistration) []string {
	if len(loadBalancers) == 0 {
		return nil
	}
	out := make([]string, 0, len(loadBalancers))
	for _, lb := range loadBalancers {
		if lb.DeregisteredAt != nil {
			continue
		}
		provider := strings.TrimSpace(lb.Provider)
		identifier := strings.TrimSpace(lb.LBIdentifier)
		if identifier == "" {
			continue
		}
		if provider != "" {
			out = append(out, provider+":"+identifier)
		} else {
			out = append(out, identifier)
		}
	}
	sort.Strings(out)
	return out
}

func privateAccessPortAllowedByLabels(labels map[string]any, port int, keys ...string) bool {
	values, ok := privateAccessLabelStrings(labels, keys...)
	if !ok || len(values) == 0 || port <= 0 {
		return true
	}
	for _, value := range values {
		if privateAccessFirewallPortFieldMatches(value, port) {
			return true
		}
	}
	return false
}

func privateAccessLabelString(labels map[string]any, keys ...string) string {
	value, ok := privateAccessLabelValue(labels, keys...)
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		out := strings.TrimSpace(fmt.Sprint(value))
		if out == "<nil>" {
			return ""
		}
		return out
	}
}

func privateAccessLabelBool(labels map[string]any, keys ...string) bool {
	value, ok := privateAccessLabelValue(labels, keys...)
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "y", "public", "internet-facing", "internet_facing":
			return true
		default:
			return false
		}
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return false
	}
}

func privateAccessLabelStrings(labels map[string]any, keys ...string) ([]string, bool) {
	value, ok := privateAccessLabelValue(labels, keys...)
	if !ok {
		return nil, false
	}
	out := privateAccessStringsFromAny(value)
	return out, len(out) > 0
}

func privateAccessStringsFromAny(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, privateAccessStringsFromAny(item)...)
		}
		return out
	case string:
		out := []string{}
		for _, item := range strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
		}) {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return nil
		}
		return []string{text}
	}
}

func privateAccessLabelValue(labels map[string]any, keys ...string) (any, bool) {
	if len(labels) == 0 {
		return nil, false
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key = privateAccessLabelKey(key); key != "" {
			wanted[key] = struct{}{}
		}
	}
	for key, value := range labels {
		if _, ok := wanted[privateAccessLabelKey(key)]; ok {
			return value, true
		}
	}
	return nil, false
}

func privateAccessLabelKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	return value
}

func privateAccessFindingSOCCaseSummary(finding storage.PrivateAccessExposureFindingRecord) string {
	detail := strings.TrimSpace(finding.Detail)
	if detail == "" {
		detail = strings.TrimSpace(finding.Type)
	}
	if detail == "" {
		detail = "private access exposure finding"
	}
	return detail
}

func privateAccessFindingSOCCaseEvidence(finding storage.PrivateAccessExposureFindingRecord) map[string]any {
	citation := map[string]any{
		"kind": "private_access_exposure_finding",
		"id":   "private_access_exposure_findings:" + finding.ID.String(),
	}
	out := map[string]any{
		"citations": []any{citation},
		"finding": map[string]any{
			"id":          finding.ID.String(),
			"type":        finding.Type,
			"severity":    finding.Severity,
			"provider":    finding.Provider,
			"detail":      finding.Detail,
			"evidence":    finding.Evidence,
			"observed_at": finding.ObservedAt,
		},
	}
	if finding.NodeID != nil {
		out["node_id"] = finding.NodeID.String()
	}
	if finding.ServiceID != nil {
		out["service_id"] = finding.ServiceID.String()
	}
	return out
}

func normalizePrivateAccessListenAddr(value string) string {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if zone, _, ok := strings.Cut(value, "%"); ok {
		value = zone
	}
	return value
}

func parsePrivateAccessIntQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
