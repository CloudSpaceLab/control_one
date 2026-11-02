package patchsupport

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

const PrepareOnlineJob = "patch.prepare_online"

type OnlinePlanReport struct {
	Status string      `json:"status"`
	Plan   *OnlinePlan `json:"plan,omitempty"`
}

// OnlineTransport is shared by request checks and the transactional dispatcher.
// Runtime/liveness timestamps are excluded; repository and managed policy changes
// are included. The digest is not a continuous-health claim.
type OnlineTransport struct {
	TenantID        string   `json:"tenant_id"`
	NodeID          string   `json:"node_id"`
	Mode            string   `json:"mode"`
	ProxyID         string   `json:"proxy_id,omitempty"`
	ProxyURL        string   `json:"proxy_url,omitempty"`
	RepositoryHosts []string `json:"repository_hosts,omitempty"`
	ProxyNodeID     string   `json:"proxy_node_id,omitempty"`
	Revision        int64    `json:"revision,omitempty"`
	ClientCIDRs     []string `json:"client_cidrs,omitempty"`
	ProbeURLs       []string `json:"probe_urls,omitempty"`
}

func (t OnlineTransport) Digest() string {
	body, _ := json.Marshal(t)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

// OnlinePreparation permits read-only resolution/acquisition, never installation.
// TransportContext pins the server's proxy identity/configuration snapshot.
type OnlinePreparation struct {
	RepositoryHosts  []string  `json:"repository_hosts,omitempty"`
	NodeID           string    `json:"node_id"`
	Mode             string    `json:"mode"`
	ProxyURL         string    `json:"proxy_url,omitempty"`
	TransportContext string    `json:"transport_context"`
	Packages         []string  `json:"packages"`
	ExpiresAt        time.Time `json:"expires_at"`
	RequestedBy      string    `json:"requested_by"`
}

func (p OnlinePreparation) Validate(now time.Time) error {
	id, err := uuid.Parse(p.NodeID)
	if err != nil || id == uuid.Nil || id.String() != p.NodeID || (p.Mode != "direct" && p.Mode != "proxy") || !offlineDigest.MatchString(p.TransportContext) {
		return fmt.Errorf("invalid online preparation identity")
	}
	if (p.Mode == "direct" && (p.ProxyURL != "" || len(p.RepositoryHosts) > 0)) || (p.Mode == "proxy" && ProxyURL(p.ProxyURL) != nil) {
		return fmt.Errorf("invalid online preparation transport")
	}
	if p.Mode == "proxy" {
		if len(p.RepositoryHosts) == 0 || len(p.RepositoryHosts) > 256 {
			return fmt.Errorf("explicit repository host policy required for proxy preparation")
		}
		for _, host := range p.RepositoryHosts {
			if !offlineSelector.MatchString(host) || strings.ContainsAny(host, "_:") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
				return fmt.Errorf("proxy exact plans require exact repository hostnames")
			}
		}
	}
	if len(p.Packages) == 0 || len(p.Packages) > 100 {
		return fmt.Errorf("select 1–100 explicit installed packages for exact online planning")
	}
	for i, name := range p.Packages {
		if !offlinePackage.MatchString(name) || len(name) > 256 || (i > 0 && p.Packages[i-1] >= name) {
			return fmt.Errorf("online package scope must be unique and sorted")
		}
	}
	if !p.ExpiresAt.After(now) || p.ExpiresAt.After(now.Add(11*time.Minute)) {
		return fmt.Errorf("online preparation expired or invalid")
	}
	return nil
}

// OnlinePlan is an approval identity over immutable acquired bytes, complete
// installed state, exact dependency operations and the reviewed transport.
// Reboot/service compatibility are explicitly not inferred from APT resolution.
type OnlinePlan struct {
	ArtifactsSHA256      string         `json:"artifacts_sha256,omitempty"`
	RepositoryHosts      []string       `json:"repository_hosts,omitempty"`
	Version              int            `json:"version"`
	SHA256               string         `json:"sha256"`
	NodeID               string         `json:"node_id"`
	Mode                 string         `json:"mode"`
	ProxyURL             string         `json:"proxy_url,omitempty"`
	TransportContext     string         `json:"transport_context"`
	InstalledStateSHA256 string         `json:"installed_state_sha256"`
	InputsSHA256         string         `json:"inputs_sha256"`
	ManifestSHA256       string         `json:"manifest_sha256"`
	Requested            []string       `json:"requested"`
	Resolved             []ExactPackage `json:"resolved"`
	Review               OfflineReview  `json:"review"`
}

func (p OnlinePlan) Digest() string {
	p.SHA256 = ""
	body, _ := json.Marshal(p)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

func (p OnlinePlan) Validate(now time.Time) error {
	if p.ArtifactsSHA256 != "" && !offlineDigest.MatchString(p.ArtifactsSHA256) {
		return fmt.Errorf("invalid acquired archive-set identity")
	}
	prep := OnlinePreparation{RepositoryHosts: p.RepositoryHosts, NodeID: p.NodeID, Mode: p.Mode, ProxyURL: p.ProxyURL, TransportContext: p.TransportContext, Packages: p.Requested, ExpiresAt: now.Add(time.Minute)}
	if err := prep.Validate(now); err != nil {
		return err
	}
	if p.Version != 1 || !offlineDigest.MatchString(p.SHA256) || p.SHA256 != p.Digest() || !offlineDigest.MatchString(p.InstalledStateSHA256) || !offlineDigest.MatchString(p.InputsSHA256) || !offlineDigest.MatchString(p.ManifestSHA256) {
		return fmt.Errorf("online plan identity mismatch")
	}
	r := p.Review
	if r.PreparedAt.IsZero() || r.PreparedAt.After(now) || !r.ExpiresAt.After(now) || !r.ExpiresAt.After(r.PreparedAt) || r.ExpiresAt.Sub(r.PreparedAt) > time.Hour {
		return fmt.Errorf("online plan expired or invalid; prepare and approve a fresh plan")
	}
	if p.Resolved == nil || r.Changes == nil || len(p.Resolved) > 100 || len(r.Changes) != len(p.Resolved) || r.Reboot != "unknown" || r.ServiceImpact != "requires_operator_review" {
		return fmt.Errorf("invalid exact online review")
	}
	for i, pkg := range p.Resolved {
		if !offlinePackage.MatchString(pkg.Name) || !offlineVersion.MatchString(pkg.Version) || len(pkg.Version) > 256 || (i > 0 && p.Resolved[i-1].Name >= pkg.Name) {
			return fmt.Errorf("invalid resolved online package scope")
		}
		c := r.Changes[i]
		role := "dependency"
		for _, name := range p.Requested {
			if name == pkg.Name {
				role = "requested"
			}
		}
		if c.Name != pkg.Name || c.ProposedVersion != pkg.Version || c.Role != role || (c.InstalledVersion != "" && (!offlineVersion.MatchString(c.InstalledVersion) || len(c.InstalledVersion) > 256)) {
			return fmt.Errorf("online review differs from exact operations")
		}
	}
	return nil
}

func (p OnlinePlan) Matches(prep OnlinePreparation) bool {
	return reflect.DeepEqual(p.RepositoryHosts, prep.RepositoryHosts) && p.NodeID == prep.NodeID && p.Mode == prep.Mode && p.ProxyURL == prep.ProxyURL && p.TransportContext == prep.TransportContext && reflect.DeepEqual(p.Requested, prep.Packages)
}
