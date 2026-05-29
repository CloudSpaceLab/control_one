package privateaccess

import "time"

type ProviderKind string

const (
	ProviderNetBird   ProviderKind = "netbird"
	ProviderHeadscale ProviderKind = "headscale"
	ProviderOpenZiti  ProviderKind = "openziti"
)

func ValidProvider(provider ProviderKind) bool {
	switch provider {
	case ProviderNetBird, ProviderHeadscale, ProviderOpenZiti:
		return true
	default:
		return false
	}
}

type Snapshot struct {
	Provider    ProviderKind `json:"provider"`
	AccountID   string       `json:"account_id,omitempty"`
	CollectedAt time.Time    `json:"collected_at,omitempty"`

	Peers           []Peer            `json:"peers,omitempty"`
	Groups          []Group           `json:"groups,omitempty"`
	Policies        []Policy          `json:"policies,omitempty"`
	Routes          []Route           `json:"routes,omitempty"`
	Services        []Service         `json:"services,omitempty"`
	ConnectorHealth []ConnectorHealth `json:"connector_health,omitempty"`
	AuditEvents     []AuditEvent      `json:"audit_events,omitempty"`
}

type Peer struct {
	ID         string            `json:"id"`
	Name       string            `json:"name,omitempty"`
	NodeID     string            `json:"node_id,omitempty"`
	Address    string            `json:"address,omitempty"`
	Status     string            `json:"status,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	LastSeenAt time.Time         `json:"last_seen_at,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Group struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	PeerIDs []string `json:"peer_ids,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

type Policy struct {
	ID        string           `json:"id"`
	Name      string           `json:"name,omitempty"`
	Enabled   bool             `json:"enabled"`
	Action    string           `json:"action,omitempty"`
	Sources   []PolicySubject  `json:"sources,omitempty"`
	Resources []PolicyResource `json:"resources,omitempty"`
}

type PolicySubject struct {
	PeerID  string `json:"peer_id,omitempty"`
	GroupID string `json:"group_id,omitempty"`
	Tag     string `json:"tag,omitempty"`
}

type PolicyResource struct {
	ServiceIDs []string `json:"service_ids,omitempty"`
	RouteIDs   []string `json:"route_ids,omitempty"`
	CIDRs      []string `json:"cidrs,omitempty"`
	Protocol   string   `json:"protocol,omitempty"`
	Ports      []int    `json:"ports,omitempty"`
}

type Route struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	PeerID      string `json:"peer_id,omitempty"`
	CIDR        string `json:"cidr,omitempty"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

type Service struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	NodeID   string   `json:"node_id,omitempty"`
	Host     string   `json:"host,omitempty"`
	Protocol string   `json:"protocol,omitempty"`
	Ports    []int    `json:"ports,omitempty"`
	RouteIDs []string `json:"route_ids,omitempty"`
	Enabled  bool     `json:"enabled"`
}

type ConnectorHealth struct {
	ID        string            `json:"id"`
	Name      string            `json:"name,omitempty"`
	Status    string            `json:"status,omitempty"`
	CheckedAt time.Time         `json:"checked_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type AuditEvent struct {
	ID         string            `json:"id"`
	Type       string            `json:"type,omitempty"`
	Actor      string            `json:"actor,omitempty"`
	Target     string            `json:"target,omitempty"`
	ObservedAt time.Time         `json:"observed_at,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ExposureObservation struct {
	NodeID            string            `json:"node_id,omitempty"`
	ServiceID         string            `json:"service_id,omitempty"`
	Name              string            `json:"name,omitempty"`
	Address           string            `json:"address,omitempty"`
	Protocol          string            `json:"protocol,omitempty"`
	Port              int               `json:"port,omitempty"`
	PubliclyReachable bool              `json:"publicly_reachable"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

type ReconcileOptions struct {
	Now              time.Time
	MaxPeerStaleness time.Duration
}

type ExposureFinding struct {
	Type      string       `json:"type"`
	Severity  string       `json:"severity"`
	Provider  ProviderKind `json:"provider,omitempty"`
	NodeID    string       `json:"node_id,omitempty"`
	ServiceID string       `json:"service_id,omitempty"`
	Detail    string       `json:"detail,omitempty"`
	Evidence  []string     `json:"evidence,omitempty"`
}

const (
	FindingPubliclyExposed   = "publicly_exposed"
	FindingPrivateAccessOnly = "private_access_only"
	FindingUnmanaged         = "unmanaged_private_service"
	FindingPolicyDrift       = "policy_drift"
)
