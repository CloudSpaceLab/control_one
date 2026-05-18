package ipintel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// Cache abstracts the persistence layer. The Postgres-backed implementation
// lives in storage/ipintel_cache.go; tests use the in-memory variant below.
type Cache interface {
	Get(ctx context.Context, ip string) (*Enrichment, bool, error)
	Put(ctx context.Context, ip string, e *Enrichment, ttl time.Duration) error
}

// Service is the unified entry point used by the HTTP handler. It picks a
// provider based on config, applies cache + single-flight, and returns a
// canonical Enrichment.
type Service struct {
	cfg       config.IPIntelConfig
	primary   Provider // ipquery or abuseipdb
	secondary Provider // optional: abuseipdb when ipquery is primary
	cache     Cache
	inflight  singleflight
}

// New constructs a Service from config. When cfg.Enabled is false or no
// provider is configured the returned Service.Lookup will return an
// errDisabled sentinel so the caller can degrade gracefully.
func New(cfg config.IPIntelConfig, cache Cache) *Service {
	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	if cfg.HTTPTimeout <= 0 {
		httpClient.Timeout = 5 * time.Second
	}
	s := &Service{cfg: cfg, cache: cache}
	if !cfg.Enabled {
		return s
	}
	if cfg.IpqueryBaseURL != "" {
		s.primary = NewIpqueryProvider(cfg.IpqueryBaseURL, httpClient)
	}
	if cfg.AbuseIPDBKey != "" {
		ab := NewAbuseIPDBProvider(cfg.AbuseIPDBKey, httpClient)
		if s.primary == nil {
			s.primary = ab
		} else {
			s.secondary = ab
		}
	}
	return s
}

// Enabled reports whether at least one provider is wired.
func (s *Service) Enabled() bool { return s.primary != nil }

// ErrDisabled is returned when no provider is configured. Callers should
// surface a friendly empty enrichment, not a 500.
var ErrDisabled = errors.New("ipintel: disabled (no provider configured)")

// Lookup returns enrichment for ip, hitting cache first, then the primary
// provider, falling back to the secondary on hard errors. Successful
// lookups are cached for cfg.CacheTTL.
func (s *Service) Lookup(ctx context.Context, ip string) (*Enrichment, error) {
	if parsed := net.ParseIP(ip); parsed == nil {
		return nil, errors.New("ipintel: invalid ip")
	}
	if s.primary == nil {
		return nil, ErrDisabled
	}

	if s.cache != nil && s.cfg.CacheTTL > 0 {
		if hit, ok, err := s.cache.Get(ctx, ip); err == nil && ok {
			hit.Source = "cache"
			if s.secondary != nil && shouldCheckSecondary(hit, s.AbuseScoreCutoff()) {
				if secondary, secErr := s.secondary.Lookup(ctx, ip); secErr == nil && secondary != nil {
					hit = mergeEnrichment(hit, secondary)
					_ = s.cache.Put(ctx, ip, hit, s.cfg.CacheTTL)
				}
			}
			return hit, nil
		}
	}

	v, err := s.inflight.Do(ip, func() (*Enrichment, error) {
		out, err := s.primary.Lookup(ctx, ip)
		if err != nil && s.secondary != nil {
			out, err = s.secondary.Lookup(ctx, ip)
		} else if err == nil && s.secondary != nil && shouldCheckSecondary(out, s.AbuseScoreCutoff()) {
			if secondary, secErr := s.secondary.Lookup(ctx, ip); secErr == nil && secondary != nil {
				out = mergeEnrichment(out, secondary)
			}
		}
		return out, err
	})
	if err != nil {
		return nil, err
	}
	if v != nil && s.cache != nil && s.cfg.CacheTTL > 0 {
		_ = s.cache.Put(ctx, ip, v, s.cfg.CacheTTL)
	}
	return v, nil
}

func shouldCheckSecondary(e *Enrichment, cutoff int) bool {
	if e == nil {
		return true
	}
	if e.ReputationScore <= 0 {
		return true
	}
	return e.ReputationScore < cutoff && len(e.ThreatFeeds) == 0
}

func mergeEnrichment(primary, secondary *Enrichment) *Enrichment {
	if primary == nil {
		return secondary
	}
	if secondary == nil {
		return primary
	}
	out := *primary
	if out.Addr == "" {
		out.Addr = secondary.Addr
	}
	if out.Geo.Country == "" {
		out.Geo.Country = secondary.Geo.Country
	}
	if out.Geo.CountryCode == "" {
		out.Geo.CountryCode = secondary.Geo.CountryCode
	}
	if out.Geo.City == "" {
		out.Geo.City = secondary.Geo.City
	}
	if out.Geo.Region == "" {
		out.Geo.Region = secondary.Geo.Region
	}
	if out.Geo.Latitude == 0 {
		out.Geo.Latitude = secondary.Geo.Latitude
	}
	if out.Geo.Longitude == 0 {
		out.Geo.Longitude = secondary.Geo.Longitude
	}
	if out.Geo.Timezone == "" {
		out.Geo.Timezone = secondary.Geo.Timezone
	}
	if out.Geo.ASN == "" {
		out.Geo.ASN = secondary.Geo.ASN
	}
	if out.Geo.Org == "" {
		out.Geo.Org = secondary.Geo.Org
	}
	if out.Geo.ISP == "" {
		out.Geo.ISP = secondary.Geo.ISP
	}
	if secondary.ReputationScore > out.ReputationScore {
		out.ReputationScore = secondary.ReputationScore
	}
	if out.UsageType == "" {
		out.UsageType = secondary.UsageType
	}
	out.IsTor = out.IsTor || secondary.IsTor
	if secondary.TotalReports > out.TotalReports {
		out.TotalReports = secondary.TotalReports
	}
	if out.LastReportedAt == "" {
		out.LastReportedAt = secondary.LastReportedAt
	}
	out.ThreatFeeds = mergeThreatFeedHits(out.ThreatFeeds, secondary.ThreatFeeds)
	if primary.Source != "" && secondary.Source != "" && primary.Source != secondary.Source {
		out.Source = primary.Source + "+" + secondary.Source
	} else if out.Source == "" {
		out.Source = secondary.Source
	}
	if out.FetchedAt.IsZero() || (!secondary.FetchedAt.IsZero() && secondary.FetchedAt.After(out.FetchedAt)) {
		out.FetchedAt = secondary.FetchedAt
	}
	return &out
}

func mergeThreatFeedHits(a, b []ThreatFeedHit) []ThreatFeedHit {
	if len(a) == 0 {
		return append([]ThreatFeedHit(nil), b...)
	}
	out := append([]ThreatFeedHit(nil), a...)
	seen := map[string]int{}
	for i, hit := range out {
		seen[hit.Feed] = i
	}
	for _, hit := range b {
		if hit.Feed == "" {
			continue
		}
		if i, ok := seen[hit.Feed]; ok {
			if out[i].Severity == "" {
				out[i].Severity = hit.Severity
			}
			if out[i].FirstSeen == "" {
				out[i].FirstSeen = hit.FirstSeen
			}
			continue
		}
		seen[hit.Feed] = len(out)
		out = append(out, hit)
	}
	return out
}

// AbuseScoreCutoff returns the threshold the UI should treat as "flag".
func (s *Service) AbuseScoreCutoff() int {
	if s.cfg.AbuseScoreCutoff > 0 {
		return s.cfg.AbuseScoreCutoff
	}
	return 25
}

// ─── singleflight (collapse concurrent lookups for the same IP) ──────

type sfCall struct {
	wg  sync.WaitGroup
	val *Enrichment
	err error
}

type singleflight struct {
	mu sync.Mutex
	m  map[string]*sfCall
}

func (g *singleflight) Do(key string, fn func() (*Enrichment, error)) (*Enrichment, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*sfCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &sfCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return c.val, c.err
}

// ─── In-memory Cache (used by tests + offline mode) ────────────────────

type memCache struct {
	mu    sync.RWMutex
	items map[string]memEntry
}

type memEntry struct {
	exp time.Time
	val Enrichment
}

// NewMemCache returns a process-local TTL cache. Suitable for tests; in
// production prefer a Postgres-backed Cache.
func NewMemCache() Cache { return &memCache{items: make(map[string]memEntry)} }

func (m *memCache) Get(_ context.Context, ip string) (*Enrichment, bool, error) {
	m.mu.RLock()
	e, ok := m.items[ip]
	m.mu.RUnlock()
	if !ok || time.Now().After(e.exp) {
		return nil, false, nil
	}
	out := e.val
	return &out, true, nil
}

func (m *memCache) Put(_ context.Context, ip string, v *Enrichment, ttl time.Duration) error {
	if v == nil {
		return nil
	}
	m.mu.Lock()
	m.items[ip] = memEntry{exp: time.Now().Add(ttl), val: *v}
	m.mu.Unlock()
	return nil
}
