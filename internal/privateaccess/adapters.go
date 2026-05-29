package privateaccess

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPImportTimeout = 30 * time.Second

// ImportSummary is a redacted receipt for a provider import. It deliberately
// carries counts and endpoint names, not credentials or raw provider rows.
type ImportSummary struct {
	Provider        ProviderKind `json:"provider"`
	AccountID       string       `json:"account_id,omitempty"`
	CollectedAt     time.Time    `json:"collected_at"`
	Peers           int          `json:"peers"`
	Groups          int          `json:"groups"`
	Policies        int          `json:"policies"`
	Routes          int          `json:"routes"`
	Services        int          `json:"services"`
	ConnectorHealth int          `json:"connector_health"`
	AuditEvents     int          `json:"audit_events"`
}

// HTTPImportConfig describes one live provider pull. Token material is expected
// to come from an encrypted provider credential; callers should not persist it
// in account configuration.
type HTTPImportConfig struct {
	Provider      ProviderKind
	AccountID     string
	BaseURL       string
	Token         string
	Authorization string
	Endpoints     map[string]string
	Timeout       time.Duration
	SkipTLSVerify bool
	Client        *http.Client
	Now           time.Time
}

// SnapshotFromProviderPayload normalizes provider-native JSON exports into the
// provider-neutral private-access snapshot model.
func SnapshotFromProviderPayload(provider ProviderKind, accountID string, payload map[string]json.RawMessage, now time.Time) (Snapshot, ImportSummary, error) {
	provider = ProviderKind(strings.ToLower(strings.TrimSpace(string(provider))))
	if !ValidProvider(provider) {
		return Snapshot{}, ImportSummary{}, fmt.Errorf("unsupported private-access provider %q", provider)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}

	var snapshot Snapshot
	var err error
	switch provider {
	case ProviderNetBird:
		snapshot, err = netBirdSnapshotFromPayload(payload)
	case ProviderHeadscale:
		snapshot, err = headscaleSnapshotFromPayload(payload)
	case ProviderOpenZiti:
		snapshot, err = openZitiSnapshotFromPayload(payload)
	}
	if err != nil {
		return Snapshot{}, ImportSummary{}, err
	}
	snapshot.Provider = provider
	snapshot.AccountID = strings.TrimSpace(accountID)
	if snapshot.AccountID == "" {
		snapshot.AccountID = "default"
	}
	if snapshot.CollectedAt.IsZero() {
		snapshot.CollectedAt = now
	} else {
		snapshot.CollectedAt = snapshot.CollectedAt.UTC()
	}
	return snapshot, summaryForSnapshot(snapshot), nil
}

// FetchSnapshot pulls a provider API using a configured endpoint map, then
// normalizes the collected resources into the shared snapshot model.
func FetchSnapshot(ctx context.Context, cfg HTTPImportConfig) (Snapshot, ImportSummary, error) {
	provider := ProviderKind(strings.ToLower(strings.TrimSpace(string(cfg.Provider))))
	if !ValidProvider(provider) {
		return Snapshot{}, ImportSummary{}, fmt.Errorf("unsupported private-access provider %q", provider)
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return Snapshot{}, ImportSummary{}, errors.New("base_url is required")
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return Snapshot{}, ImportSummary{}, fmt.Errorf("invalid base_url %q", baseURL)
	}
	client := cfg.Client
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultHTTPImportTimeout
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.SkipTLSVerify {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // operator-controlled private provider endpoint
		}
		client = &http.Client{Timeout: timeout, Transport: transport}
	}

	endpoints := providerEndpoints(provider, cfg.Endpoints)
	payload := make(map[string]json.RawMessage, len(endpoints))
	for key, path := range endpoints {
		raw, err := fetchProviderResource(ctx, client, parsedBase, path, providerAuthorization(provider, cfg))
		if err != nil {
			return Snapshot{}, ImportSummary{}, fmt.Errorf("fetch %s: %w", key, err)
		}
		payload[key] = raw
	}
	return SnapshotFromProviderPayload(provider, cfg.AccountID, payload, cfg.Now)
}

func netBirdSnapshotFromPayload(payload map[string]json.RawMessage) (Snapshot, error) {
	peers, err := decodeObjects(firstPayload(payload, "peers", "peer"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode netbird peers: %w", err)
	}
	groups, err := decodeObjects(firstPayload(payload, "groups"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode netbird groups: %w", err)
	}
	routes, err := decodeObjects(firstPayload(payload, "routes", "networks"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode netbird routes: %w", err)
	}
	policies, err := decodeObjects(firstPayload(payload, "policies"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode netbird policies: %w", err)
	}
	events, err := decodeObjects(firstPayload(payload, "audit_events", "events", "activity"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode netbird audit events: %w", err)
	}

	out := Snapshot{
		Peers:       make([]Peer, 0, len(peers)),
		Groups:      make([]Group, 0, len(groups)),
		Routes:      make([]Route, 0, len(routes)),
		Policies:    make([]Policy, 0, len(policies)+len(routes)),
		AuditEvents: make([]AuditEvent, 0, len(events)),
	}
	for _, item := range peers {
		out.Peers = append(out.Peers, Peer{
			ID:         firstString(item, "id", "peer_id", "identifier"),
			Name:       firstString(item, "name", "hostname", "dns_label"),
			NodeID:     firstString(item, "node_id", "control_one_node_id"),
			Address:    firstString(item, "ip", "netbird_ip", "address", "ip_address"),
			Status:     firstString(item, "status", "connection_status", "connected"),
			Tags:       stringValues(item, "tags", "groups", "group_ids"),
			LastSeenAt: firstTime(item, "last_seen", "last_seen_at", "last_login", "last_connected"),
		})
	}
	for _, item := range groups {
		out.Groups = append(out.Groups, Group{
			ID:      firstString(item, "id", "group_id", "name"),
			Name:    firstString(item, "name", "display_name"),
			PeerIDs: nestedIDs(item, "peers", "peer_ids", "members"),
			Tags:    stringValues(item, "tags"),
		})
	}
	for _, item := range routes {
		route := Route{
			ID:          firstString(item, "id", "route_id", "network_id", "name"),
			Name:        firstString(item, "name", "network_id", "description"),
			PeerID:      firstString(item, "peer", "peer_id", "routing_peer_id"),
			CIDR:        firstString(item, "network", "cidr", "prefix"),
			Enabled:     firstBool(item, true, "enabled", "active"),
			Description: firstString(item, "description"),
		}
		out.Routes = append(out.Routes, route)
		if route.ID != "" && len(stringValues(item, "access_control_groups", "access_control_group_ids")) > 0 {
			out.Policies = append(out.Policies, Policy{
				ID:      "route:" + route.ID + ":access-control",
				Name:    firstNonEmpty(route.Name, route.ID) + " route access",
				Enabled: route.Enabled,
				Action:  "allow",
				Sources: groupSubjects(stringValues(item, "access_control_groups", "access_control_group_ids")),
				Resources: []PolicyResource{{
					RouteIDs: []string{route.ID},
					CIDRs:    nonEmptyStrings(route.CIDR),
				}},
			})
		}
	}
	for _, item := range policies {
		out.Policies = append(out.Policies, policyFromGenericMap(item))
	}
	for _, item := range events {
		out.AuditEvents = append(out.AuditEvents, auditEventFromMap(item))
	}
	return out, nil
}

func headscaleSnapshotFromPayload(payload map[string]json.RawMessage) (Snapshot, error) {
	nodes, err := decodeObjects(firstPayload(payload, "nodes", "machines", "peers"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode headscale nodes: %w", err)
	}
	routes, err := decodeObjects(firstPayload(payload, "routes"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode headscale routes: %w", err)
	}
	users, err := decodeObjects(firstPayload(payload, "users", "namespaces"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode headscale users: %w", err)
	}
	aclRules, err := decodeObjects(firstPayload(payload, "acls", "acl_rules", "acl"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode headscale ACLs: %w", err)
	}

	out := Snapshot{
		Peers:    make([]Peer, 0, len(nodes)),
		Groups:   make([]Group, 0, len(users)),
		Routes:   make([]Route, 0, len(routes)),
		Policies: make([]Policy, 0, len(aclRules)),
	}
	for _, item := range nodes {
		out.Peers = append(out.Peers, Peer{
			ID:         firstString(item, "id", "node_id", "machine_id", "name"),
			Name:       firstString(item, "name", "given_name", "hostname"),
			NodeID:     firstString(item, "control_one_node_id"),
			Address:    firstString(item, "ip_address", "address", "ipv4"),
			Status:     statusFromOnline(item),
			Tags:       stringValues(item, "tags", "valid_tags", "forced_tags"),
			LastSeenAt: firstTime(item, "last_seen", "last_seen_at"),
		})
	}
	for _, item := range users {
		out.Groups = append(out.Groups, Group{
			ID:   firstString(item, "id", "name", "username"),
			Name: firstString(item, "name", "username", "display_name"),
			Tags: stringValues(item, "tags"),
		})
	}
	for _, item := range routes {
		out.Routes = append(out.Routes, Route{
			ID:          firstString(item, "id", "route_id", "prefix"),
			Name:        firstString(item, "name", "prefix"),
			PeerID:      firstString(item, "node_id", "machine_id", "advertised_by"),
			CIDR:        firstString(item, "prefix", "route", "cidr"),
			Enabled:     firstBool(item, false, "enabled", "approved"),
			Description: firstString(item, "description"),
		})
	}
	for _, item := range aclRules {
		out.Policies = append(out.Policies, headscalePolicyFromACL(item))
	}
	return out, nil
}

func openZitiSnapshotFromPayload(payload map[string]json.RawMessage) (Snapshot, error) {
	identities, err := decodeObjects(firstPayload(payload, "identities", "peers"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode openziti identities: %w", err)
	}
	services, err := decodeObjects(firstPayload(payload, "services"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode openziti services: %w", err)
	}
	policies, err := decodeObjects(firstPayload(payload, "service_policies", "servicePolicies", "policies"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode openziti policies: %w", err)
	}
	routers, err := decodeObjects(firstPayload(payload, "connector_health", "edge_routers", "edgeRouters"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode openziti routers: %w", err)
	}
	events, err := decodeObjects(firstPayload(payload, "audit_events", "events"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode openziti audit events: %w", err)
	}

	out := Snapshot{
		Peers:           make([]Peer, 0, len(identities)),
		Services:        make([]Service, 0, len(services)),
		Policies:        make([]Policy, 0, len(policies)),
		ConnectorHealth: make([]ConnectorHealth, 0, len(routers)),
		AuditEvents:     make([]AuditEvent, 0, len(events)),
	}
	for _, item := range identities {
		out.Peers = append(out.Peers, Peer{
			ID:         firstString(item, "id", "identity_id", "name"),
			Name:       firstString(item, "name"),
			NodeID:     firstString(item, "control_one_node_id"),
			Status:     statusFromEnabled(item),
			Tags:       stringValues(item, "roleAttributes", "role_attributes", "tags"),
			LastSeenAt: firstTime(item, "last_seen_at", "updatedAt", "updated_at"),
		})
	}
	for _, item := range services {
		out.Services = append(out.Services, Service{
			ID:       firstString(item, "id", "service_id", "name"),
			Name:     firstString(item, "name"),
			NodeID:   firstString(item, "control_one_node_id"),
			Host:     firstString(item, "host", "hostname", "address"),
			Protocol: firstString(item, "protocol"),
			Ports:    intValues(item, "ports", "port"),
			Enabled:  firstBool(item, true, "enabled"),
		})
	}
	for _, item := range policies {
		out.Policies = append(out.Policies, openZitiPolicyFromMap(item))
	}
	for _, item := range routers {
		out.ConnectorHealth = append(out.ConnectorHealth, ConnectorHealth{
			ID:        firstString(item, "id", "router_id", "name"),
			Name:      firstString(item, "name"),
			Status:    firstString(item, "status", "ctrlStatus", "ctrl_status"),
			CheckedAt: firstTime(item, "checked_at", "updatedAt", "updated_at"),
		})
	}
	for _, item := range events {
		out.AuditEvents = append(out.AuditEvents, auditEventFromMap(item))
	}
	return out, nil
}

func providerEndpoints(provider ProviderKind, override map[string]string) map[string]string {
	defaults := map[ProviderKind]map[string]string{
		ProviderNetBird: {
			"peers":        "/api/peers",
			"groups":       "/api/groups",
			"policies":     "/api/policies",
			"routes":       "/api/routes",
			"audit_events": "/api/events",
		},
		ProviderHeadscale: {
			"nodes":  "/api/v1/node",
			"routes": "/api/v1/routes",
			"users":  "/api/v1/user",
		},
		ProviderOpenZiti: {
			"identities":       "/edge/management/v1/identities",
			"services":         "/edge/management/v1/services",
			"service_policies": "/edge/management/v1/service-policies",
			"connector_health": "/edge/management/v1/edge-routers",
		},
	}
	out := make(map[string]string, len(defaults[provider])+len(override))
	for key, path := range defaults[provider] {
		out[key] = path
	}
	for key, path := range override {
		key = strings.TrimSpace(key)
		path = strings.TrimSpace(path)
		if key != "" && path != "" {
			out[key] = path
		}
	}
	return out
}

func fetchProviderResource(ctx context.Context, client *http.Client, base *url.URL, endpoint string, authorization string) (json.RawMessage, error) {
	u := *base
	ref, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if ref.IsAbs() {
		u = *ref
	} else {
		u = *base.ResolveReference(ref)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authorization) != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if !json.Valid(body) {
		return nil, errors.New("provider returned invalid JSON")
	}
	return append(json.RawMessage(nil), body...), nil
}

func providerAuthorization(provider ProviderKind, cfg HTTPImportConfig) string {
	if value := strings.TrimSpace(cfg.Authorization); value != "" {
		return value
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") || strings.HasPrefix(strings.ToLower(token), "token ") {
		return token
	}
	if provider == ProviderNetBird {
		return "Token " + token
	}
	return "Bearer " + token
}

func summaryForSnapshot(snapshot Snapshot) ImportSummary {
	return ImportSummary{
		Provider:        snapshot.Provider,
		AccountID:       snapshot.AccountID,
		CollectedAt:     snapshot.CollectedAt,
		Peers:           len(snapshot.Peers),
		Groups:          len(snapshot.Groups),
		Policies:        len(snapshot.Policies),
		Routes:          len(snapshot.Routes),
		Services:        len(snapshot.Services),
		ConnectorHealth: len(snapshot.ConnectorHealth),
		AuditEvents:     len(snapshot.AuditEvents),
	}
}

func firstPayload(payload map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if raw, ok := payload[key]; ok && len(bytes.TrimSpace(raw)) > 0 {
			return raw
		}
	}
	return nil
}

func decodeObjects(raw json.RawMessage) ([]map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return objectsFromValue(value), nil
}

func objectsFromValue(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"data", "items", "resources", "nodes", "machines", "routes", "peers", "groups", "policies", "services"} {
			if nested, ok := typed[key]; ok {
				if out := objectsFromValue(nested); len(out) > 0 {
					return out
				}
			}
		}
		if len(typed) == 0 {
			return nil
		}
		return []map[string]any{typed}
	default:
		return nil
	}
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			if out := stringValue(value); out != "" {
				return out
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "connected"
		}
		return "disconnected"
	case map[string]any:
		return firstString(typed, "id", "name")
	default:
		return ""
	}
}

func firstBool(item map[string]any, fallback bool, keys ...string) bool {
	for _, key := range keys {
		value, ok := item[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "enabled", "active", "approved", "online", "connected":
				return true
			case "false", "disabled", "inactive", "denied", "offline", "disconnected":
				return false
			}
		case json.Number:
			i, _ := typed.Int64()
			return i != 0
		case float64:
			return typed != 0
		}
	}
	return fallback
}

func firstTime(item map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		raw := firstString(item, key)
		if raw == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if ts, err := time.Parse(layout, raw); err == nil {
				return ts.UTC()
			}
		}
	}
	return time.Time{}
}

func stringValues(item map[string]any, keys ...string) []string {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			if out := valuesAsStrings(value); len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func valuesAsStrings(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := stringValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return nonEmptyStrings(typed...)
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == ';' })
		return nonEmptyStrings(parts...)
	case map[string]any:
		return nonEmptyStrings(firstString(typed, "id", "name"))
	default:
		if s := stringValue(value); s != "" {
			return []string{s}
		}
		return nil
	}
}

func nestedIDs(item map[string]any, keys ...string) []string {
	values := stringValues(item, keys...)
	return nonEmptyStrings(values...)
}

func intValues(item map[string]any, keys ...string) []int {
	for _, key := range keys {
		value, ok := item[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []any:
			out := make([]int, 0, len(typed))
			for _, entry := range typed {
				if i, ok := asInt(entry); ok {
					out = append(out, i)
				}
			}
			return out
		default:
			if i, ok := asInt(value); ok {
				return []int{i}
			}
		}
	}
	return nil
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		i, err := typed.Int64()
		return int(i), err == nil
	case float64:
		return int(typed), typed == float64(int(typed))
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(typed))
		return i, err == nil
	default:
		return 0, false
	}
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func groupSubjects(ids []string) []PolicySubject {
	out := make([]PolicySubject, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out = append(out, PolicySubject{GroupID: strings.TrimSpace(id)})
		}
	}
	return out
}

func policyFromGenericMap(item map[string]any) Policy {
	policy := Policy{
		ID:        firstString(item, "id", "policy_id", "name"),
		Name:      firstString(item, "name", "description"),
		Enabled:   firstBool(item, true, "enabled", "active"),
		Action:    firstNonEmpty(firstString(item, "action"), "allow"),
		Sources:   groupSubjects(stringValues(item, "source_groups", "sources", "groups")),
		Resources: []PolicyResource{{}},
	}
	resource := &policy.Resources[0]
	resource.ServiceIDs = stringValues(item, "service_ids", "services")
	resource.RouteIDs = stringValues(item, "route_ids", "routes")
	resource.CIDRs = stringValues(item, "cidrs", "networks", "prefixes")
	resource.Protocol = firstString(item, "protocol")
	resource.Ports = intValues(item, "ports", "port")
	if len(resource.ServiceIDs) == 0 && len(resource.RouteIDs) == 0 && len(resource.CIDRs) == 0 && resource.Protocol == "" && len(resource.Ports) == 0 {
		policy.Resources = nil
	}
	return policy
}

func headscalePolicyFromACL(item map[string]any) Policy {
	action := firstNonEmpty(firstString(item, "action"), "accept")
	policy := Policy{
		ID:      firstString(item, "id", "name"),
		Name:    firstString(item, "name", "comment"),
		Enabled: !strings.EqualFold(action, "deny"),
		Action:  "allow",
	}
	if policy.ID == "" {
		policy.ID = "acl:" + strings.Join(stringValues(item, "src", "sources"), ",")
	}
	for _, src := range stringValues(item, "src", "sources") {
		src = strings.TrimSpace(strings.TrimPrefix(src, "tag:"))
		if src != "" {
			policy.Sources = append(policy.Sources, PolicySubject{Tag: src})
		}
	}
	for _, dst := range stringValues(item, "dst", "destinations") {
		resource := PolicyResource{}
		host, portText, ok := strings.Cut(dst, ":")
		if ok {
			if port, err := strconv.Atoi(strings.TrimSpace(portText)); err == nil {
				resource.Ports = []int{port}
			}
		} else {
			host = dst
		}
		host = strings.TrimPrefix(strings.TrimSpace(host), "tag:")
		if strings.Contains(host, "/") {
			resource.CIDRs = []string{host}
		} else if host != "" {
			resource.RouteIDs = []string{host}
		}
		policy.Resources = append(policy.Resources, resource)
	}
	return policy
}

func openZitiPolicyFromMap(item map[string]any) Policy {
	policy := Policy{
		ID:      firstString(item, "id", "policy_id", "name"),
		Name:    firstString(item, "name"),
		Enabled: firstBool(item, true, "enabled"),
		Action:  firstNonEmpty(firstString(item, "action", "policyType", "policy_type"), "allow"),
	}
	for _, role := range stringValues(item, "identityRoles", "identity_roles", "identities") {
		policy.Sources = append(policy.Sources, subjectFromOpenZitiRole(role))
	}
	resource := PolicyResource{}
	for _, role := range stringValues(item, "serviceRoles", "service_roles", "services") {
		role = strings.TrimSpace(role)
		switch {
		case strings.HasPrefix(role, "@"):
			resource.ServiceIDs = append(resource.ServiceIDs, strings.TrimPrefix(role, "@"))
		case strings.HasPrefix(role, "#"):
			resource.ServiceIDs = append(resource.ServiceIDs, strings.TrimPrefix(role, "#"))
		case role != "":
			resource.ServiceIDs = append(resource.ServiceIDs, role)
		}
	}
	resource.Ports = intValues(item, "ports", "port")
	resource.Protocol = firstString(item, "protocol")
	if len(resource.ServiceIDs) > 0 || len(resource.Ports) > 0 || resource.Protocol != "" {
		policy.Resources = []PolicyResource{resource}
	}
	return policy
}

func subjectFromOpenZitiRole(role string) PolicySubject {
	role = strings.TrimSpace(role)
	switch {
	case strings.HasPrefix(role, "@"):
		return PolicySubject{PeerID: strings.TrimPrefix(role, "@")}
	case strings.HasPrefix(role, "#"):
		return PolicySubject{Tag: strings.TrimPrefix(role, "#")}
	default:
		return PolicySubject{Tag: role}
	}
}

func auditEventFromMap(item map[string]any) AuditEvent {
	return AuditEvent{
		ID:         firstString(item, "id", "event_id", "trace_id"),
		Type:       firstString(item, "type", "event_type", "action"),
		Actor:      firstString(item, "actor", "user", "username"),
		Target:     firstString(item, "target", "resource", "object"),
		ObservedAt: firstTime(item, "observed_at", "timestamp", "time", "created_at"),
	}
}

func statusFromOnline(item map[string]any) string {
	if firstBool(item, false, "online", "connected") {
		return "connected"
	}
	return firstNonEmpty(firstString(item, "status"), "disconnected")
}

func statusFromEnabled(item map[string]any) string {
	if firstBool(item, true, "enabled") {
		return firstNonEmpty(firstString(item, "status"), "enabled")
	}
	return "disabled"
}
