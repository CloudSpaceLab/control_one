// Package ipintel orchestrates IP enrichment lookups (geolocation, ASN,
// reputation) for the Investigate surface. It supports two providers:
//
//   - akyriako/ipquery — self-hosted REST service that bundles geo + ASN +
//     AbuseIPDB risk into one response. Used when Config.IpqueryBaseURL is
//     set; preferred since it gives the richest payload in one round-trip.
//   - AbuseIPDB direct — used standalone when IpqueryBaseURL is empty but
//     AbuseIPDBKey is set. Returns risk-only (no geo / ASN).
//
// The Service caches successful lookups in Postgres (ip_enrichment_cache)
// for Config.CacheTTL so repeat investigations don't burn through external
// rate limits. Failures are not cached.
package ipintel

import (
	"net/http"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// Enrichment is the canonical shape returned to the API. JSON tags match
// the wire format consumed by the UI's IpEnrichment type.
type Enrichment struct {
	Addr             string             `json:"addr"`
	Geo              GeoInfo            `json:"geo"`
	ThreatFeeds      []ThreatFeedHit    `json:"threat_feeds,omitempty"`
	ReputationScore  int                `json:"reputation_score"`
	UsageType        string             `json:"usage_type,omitempty"`
	IsTor            bool               `json:"is_tor,omitempty"`
	TotalReports     int                `json:"total_reports,omitempty"`
	LastReportedAt   string             `json:"last_reported_at,omitempty"`
	Source           string             `json:"source,omitempty"` // "ipquery" | "abuseipdb" | "cache"
	FetchedAt        time.Time          `json:"fetched_at"`
}

// GeoInfo captures geolocation + network-owner facts.
type GeoInfo struct {
	Country     string  `json:"country,omitempty"`
	CountryCode string  `json:"country_code,omitempty"`
	City        string  `json:"city,omitempty"`
	Region      string  `json:"region,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	Timezone    string  `json:"timezone,omitempty"`
	ASN         string  `json:"asn,omitempty"`
	Org         string  `json:"org,omitempty"`
	ISP         string  `json:"isp,omitempty"`
}

// ThreatFeedHit names a feed that flagged the address.
type ThreatFeedHit struct {
	Feed      string `json:"feed"`
	Severity  string `json:"severity,omitempty"`
	FirstSeen string `json:"first_seen,omitempty"`
}

// Provider abstracts a lookup backend.
type Provider interface {
	Name() string
	Lookup(ctx Ctx, ip string) (*Enrichment, error)
}

// Ctx is a small alias so callers don't have to import context just for
// the package surface; the actual implementations accept context.Context.
type Ctx = providerContext

// httpDoer is satisfied by *http.Client; declared as an interface so tests
// can inject a stub round-tripper without rewriting the providers.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Validate reports whether the supplied config has at least one provider
// usable. Used by the server during boot to log a warning if Investigate
// IP enrichment is disabled.
func Validate(cfg config.IPIntelConfig) (enabled bool, primary string) {
	if !cfg.Enabled {
		return false, ""
	}
	if cfg.IpqueryBaseURL != "" {
		return true, "ipquery"
	}
	if cfg.AbuseIPDBKey != "" {
		return true, "abuseipdb"
	}
	return false, ""
}
