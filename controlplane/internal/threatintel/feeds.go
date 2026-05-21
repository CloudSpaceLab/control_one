// Package threatintel pulls bad-IP / abuse-source lists from public feeds
// and exposes them as a unified set the auto-block pipeline can consume.
//
// Built-in sources (no API key required):
//   - Spamhaus DROP        (well-known hijacked / malicious netblocks)
//   - Spamhaus EDROP       (extended DROP list)
//   - FireHOL Level 1      (curated aggregate of community blocklists)
//   - Tor exit nodes       (dan.me.uk public list)
//   - DShield top-attackers (https://www.dshield.org/feeds/)
//
// Optional with API key:
//   - AbuseIPDB blocklist
//   - AlienVault OTX pulses
//
// All feeds are fetched on a configurable interval, parsed into a normalized
// IndicatorSet, and cached with a TTL. The pipeline subscribes via Subscribe()
// to receive a snapshot whenever the cache rolls.
package threatintel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Indicator represents one bad IP / CIDR with provenance.
type Indicator struct {
	TenantID  string    `json:"tenant_id,omitempty"`  // empty means global / built-in source
	IP        string    `json:"ip,omitempty"`         // single IP if no /n
	CIDR      string    `json:"cidr,omitempty"`       // canonical CIDR string when source supplied a netblock
	Feed      string    `json:"feed"`                 // feed identifier
	Category  string    `json:"category,omitempty"`   // optional taxonomy ("scanner", "tor-exit", "malware", ...)
	Score     int       `json:"score"`                // 0-100 confidence
	FirstSeen time.Time `json:"first_seen,omitempty"` // when the source first observed this indicator
	Evidence  string    `json:"evidence,omitempty"`
}

// IndicatorSet is the in-memory view used for fast lookup. Keyed by canonical
// CIDR or single-IP-as-/32; LookupIP walks the set on miss but the hit ratio
// for small feed pulls (~tens of thousands) is fine for inline checks.
type IndicatorSet struct {
	Updated time.Time
	cidrs   []*net.IPNet
	hits    map[string][]Indicator // CIDR string -> matching indicators
}

// LookupIP returns the matching Indicator if the IP falls in any feed CIDR.
func (s *IndicatorSet) LookupIP(ip net.IP) (Indicator, bool) {
	matches := s.LookupIPAll(ip, "")
	if len(matches) == 0 {
		return Indicator{}, false
	}
	return matches[0], true
}

// LookupIPAll returns every indicator that applies to ip. tenantID filters
// tenant-owned feeds while still allowing global built-in sources.
func (s *IndicatorSet) LookupIPAll(ip net.IP, tenantID string) []Indicator {
	if s == nil || ip == nil {
		return nil
	}
	if !isPublicRoutableIP(ip) {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	out := []Indicator{}
	seen := map[string]struct{}{}
	for _, n := range s.cidrs {
		if n.Contains(ip) {
			for _, ind := range s.hits[n.String()] {
				indTenant := strings.TrimSpace(ind.TenantID)
				if indTenant != "" && tenantID != "" && indTenant != tenantID {
					continue
				}
				if indTenant != "" && tenantID == "" {
					continue
				}
				dedupe := strings.Join([]string{indTenant, ind.Feed, ind.CIDR, ind.IP}, "|")
				if _, ok := seen[dedupe]; ok {
					continue
				}
				seen[dedupe] = struct{}{}
				out = append(out, ind)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Feed < out[j].Feed
	})
	return out
}

// isPublicRoutableIP excludes private, loopback, documentation, multicast,
// carrier-grade NAT, and other non-internet ranges from blacklist matching.
// Some edge-oriented feeds include those bogon networks intentionally; Control
// One should not label host-local traffic as malicious because of them.
func isPublicRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		a, b, c := ip4[0], ip4[1], ip4[2]
		switch {
		case a == 0 || a == 127 || a == 255 || a >= 224:
			return false
		case a == 100 && b >= 64 && b <= 127:
			return false
		case a == 192 && b == 0 && c == 2:
			return false
		case a == 198 && (b == 18 || b == 19):
			return false
		case a == 198 && b == 51 && c == 100:
			return false
		case a == 203 && b == 0 && c == 113:
			return false
		}
		return ip.IsGlobalUnicast()
	}
	if len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8 {
		return false
	}
	return ip.IsGlobalUnicast()
}

// All returns every indicator (caller must not mutate).
func (s *IndicatorSet) All() []Indicator {
	if s == nil {
		return nil
	}
	out := make([]Indicator, 0, len(s.hits))
	for _, inds := range s.hits {
		out = append(out, inds...)
	}
	return out
}

// Source is one feed implementation.
type Source interface {
	Name() string
	Fetch(ctx context.Context, client *http.Client) ([]Indicator, error)
}

// Config controls Manager behaviour.
type Config struct {
	RefreshInterval time.Duration // default 1h
	HTTPTimeout     time.Duration // default 30s
	// SnapshotDir stores the last successful pull for each source. Runtime
	// lookups use the in-memory IndicatorSet built from these snapshots, so
	// investigate pages never need to spend provider API quota.
	SnapshotDir string
	// Sources is the static fallback list. Operators primarily add feeds via
	// the threat_feeds table, but the static list still works for the no-DB
	// dev case and for "always-on" baseline feeds shipped with the product.
	Sources []Source
	// Provider returns the live set of operator-managed feeds. When set, the
	// Manager prefers Provider sources over the static Sources list and
	// re-evaluates the list on every refresh tick.
	Provider SourceProvider
}

// SourceProvider returns the current set of operator-defined feeds plus a
// callback the Manager invokes after each fetch so the provider can persist
// the success/failure status. Returning a stable feed_id lets the provider
// find the row when calling OnRefresh.
type SourceProvider interface {
	Sources(ctx context.Context) ([]ProvidedSource, error)
	OnRefresh(ctx context.Context, feedID string, status, errMsg string, count int)
}

// ProvidedSource pairs a Source with the database id of the row that defined
// it so refresh outcomes can be written back.
type ProvidedSource struct {
	ID       string
	TenantID string
	Source   Source
}

// Manager orchestrates feed pulls + caching.
type Manager struct {
	cfg         Config
	log         *zap.Logger
	mu          sync.RWMutex
	current     *IndicatorSet
	subscribers []chan *IndicatorSet
	httpClient  *http.Client
}

// New returns an unstarted Manager. Call Start to kick off the refresh loop.
func New(cfg Config, log *zap.Logger) *Manager {
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = time.Hour
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	return &Manager{
		cfg:        cfg,
		log:        log,
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
	}
}

// Start runs the refresh loop until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.refreshOnce(ctx)
	t := time.NewTicker(m.cfg.RefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.refreshOnce(ctx)
		}
	}
}

// Current returns the most recent snapshot. Nil if no successful fetch yet.
func (m *Manager) Current() *IndicatorSet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Subscribe returns a channel that receives every successful refresh.
func (m *Manager) Subscribe() <-chan *IndicatorSet {
	ch := make(chan *IndicatorSet, 1)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	if m.current != nil {
		select {
		case ch <- m.current:
		default:
		}
	}
	m.mu.Unlock()
	return ch
}

func (m *Manager) refreshOnce(ctx context.Context) {
	all := []Indicator{}

	// Operator-managed feeds first; fall back to static Sources when no
	// provider is wired in.
	if m.cfg.Provider != nil {
		provided, err := m.cfg.Provider.Sources(ctx)
		if err != nil && m.log != nil {
			m.log.Warn("threat feed provider list", zap.Error(err))
		}
		for _, ps := range provided {
			fetchCtx, cancel := context.WithTimeout(ctx, m.cfg.HTTPTimeout)
			inds, status, errMsg := m.fetchSource(fetchCtx, ps.ID, ps.Source)
			cancel()
			if len(inds) == 0 && status == "error" {
				m.cfg.Provider.OnRefresh(ctx, ps.ID, "error", errMsg, 0)
				continue
			}
			if tenantID := strings.TrimSpace(ps.TenantID); tenantID != "" {
				for i := range inds {
					inds[i].TenantID = tenantID
				}
			}
			all = append(all, inds...)
			m.cfg.Provider.OnRefresh(ctx, ps.ID, status, errMsg, len(inds))
		}
	}

	for _, src := range m.cfg.Sources {
		fetchCtx, cancel := context.WithTimeout(ctx, m.cfg.HTTPTimeout)
		inds, _, _ := m.fetchSource(fetchCtx, "static", src)
		cancel()
		all = append(all, inds...)
	}
	if len(all) == 0 {
		return
	}
	set := buildSet(all)
	m.mu.Lock()
	m.current = set
	subs := append([]chan *IndicatorSet(nil), m.subscribers...)
	m.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- set:
		default:
		}
	}
}

func (m *Manager) fetchSource(ctx context.Context, sourceID string, src Source) ([]Indicator, string, string) {
	if src == nil {
		return nil, "error", "source unavailable"
	}
	inds, err := src.Fetch(ctx, m.httpClient)
	if err == nil {
		if len(inds) > 0 {
			if saveErr := saveSnapshot(m.cfg.SnapshotDir, sourceID, src.Name(), inds); saveErr != nil && m.log != nil {
				m.log.Warn("save threat feed snapshot", zap.String("feed", src.Name()), zap.Error(saveErr))
			}
		}
		return inds, "ok", ""
	}
	if m.log != nil {
		m.log.Warn("threat feed fetch", zap.String("feed", src.Name()), zap.Error(err))
	}
	snap, snapErr := loadSnapshot(m.cfg.SnapshotDir, sourceID, src.Name())
	if snapErr == nil && len(snap.Indicators) > 0 {
		if m.log != nil {
			m.log.Warn("using stale threat feed snapshot",
				zap.String("feed", src.Name()),
				zap.Time("saved_at", snap.SavedAt),
				zap.Error(err))
		}
		return snap.Indicators, "stale", err.Error()
	}
	if snapErr != nil && !os.IsNotExist(snapErr) && m.log != nil {
		m.log.Warn("load threat feed snapshot", zap.String("feed", src.Name()), zap.Error(snapErr))
	}
	return nil, "error", err.Error()
}

func buildSet(inds []Indicator) *IndicatorSet {
	hits := make(map[string][]Indicator, len(inds))
	cidrs := make([]*net.IPNet, 0, len(inds))
	cidrSeen := map[string]struct{}{}
	for _, ind := range inds {
		canonical := ind.CIDR
		if canonical == "" && ind.IP != "" {
			ip := net.ParseIP(ind.IP)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				canonical = ind.IP + "/32"
			} else {
				canonical = ind.IP + "/128"
			}
		}
		_, n, err := net.ParseCIDR(canonical)
		if err != nil {
			continue
		}
		key := n.String()
		ind.CIDR = key
		merged := false
		for i, existing := range hits[key] {
			if existing.TenantID == ind.TenantID && existing.Feed == ind.Feed && existing.Category == ind.Category {
				if ind.Score > existing.Score || existing.FirstSeen.IsZero() || (!ind.FirstSeen.IsZero() && ind.FirstSeen.Before(existing.FirstSeen)) {
					hits[key][i] = ind
				}
				merged = true
				break
			}
		}
		if !merged {
			hits[key] = append(hits[key], ind)
		}
		if _, ok := cidrSeen[key]; ok {
			continue
		}
		cidrSeen[key] = struct{}{}
		cidrs = append(cidrs, n)
	}
	return &IndicatorSet{Updated: time.Now().UTC(), cidrs: cidrs, hits: hits}
}

type sourceSnapshot struct {
	SourceID   string      `json:"source_id"`
	SourceName string      `json:"source_name"`
	SavedAt    time.Time   `json:"saved_at"`
	Indicators []Indicator `json:"indicators"`
}

func saveSnapshot(dir, sourceID, sourceName string, inds []Indicator) error {
	path := snapshotPath(dir, sourceID, sourceName)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	payload := sourceSnapshot{
		SourceID:   strings.TrimSpace(sourceID),
		SourceName: sourceName,
		SavedAt:    time.Now().UTC(),
		Indicators: inds,
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	encErr := json.NewEncoder(f).Encode(payload)
	closeErr := f.Close()
	if encErr != nil {
		_ = os.Remove(tmp)
		return encErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			_ = os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, path)
	}
	return nil
}

func loadSnapshot(dir, sourceID, sourceName string) (*sourceSnapshot, error) {
	path := snapshotPath(dir, sourceID, sourceName)
	if path == "" {
		return nil, os.ErrNotExist
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var snap sourceSnapshot
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func SnapshotExists(dir, sourceID, sourceName string) bool {
	path := snapshotPath(dir, sourceID, sourceName)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func snapshotPath(dir, sourceID, sourceName string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	sourceID = cleanSnapshotName(sourceID)
	if sourceID == "" {
		sourceID = "static"
	}
	sourceName = cleanSnapshotName(sourceName)
	if sourceName == "" {
		sourceName = "source"
	}
	return filepath.Join(dir, sourceID+"-"+sourceName+".json")
}

func cleanSnapshotName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- built-in sources ----------------------------------------------------

// SpamhausDROP fetches the Spamhaus DROP list (textual /CIDR per line).
type SpamhausDROP struct {
	URL string // default https://www.spamhaus.org/drop/drop.txt
}

func (s SpamhausDROP) Name() string { return "spamhaus-drop" }

func (s SpamhausDROP) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	url := s.URL
	if url == "" {
		url = "https://www.spamhaus.org/drop/drop.txt"
	}
	body, err := fetchBody(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return parseSpamhaus(body, "spamhaus-drop"), nil
}

// SpamhausEDROP — extended DROP.
type SpamhausEDROP struct{ URL string }

func (s SpamhausEDROP) Name() string { return "spamhaus-edrop" }

func (s SpamhausEDROP) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	url := s.URL
	if url == "" {
		url = "https://www.spamhaus.org/drop/edrop.txt"
	}
	body, err := fetchBody(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return parseSpamhaus(body, "spamhaus-edrop"), nil
}

// FireHOLLevel1 — aggregate of well-vetted community blocklists.
type FireHOLLevel1 struct{ URL string }

func (f FireHOLLevel1) Name() string { return "firehol-level1" }

func (f FireHOLLevel1) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	url := f.URL
	if url == "" {
		url = "https://iplists.firehol.org/files/firehol_level1.netset"
	}
	body, err := fetchBody(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return parseLineList(body, "firehol-level1", "aggregate", 80), nil
}

// TorExitNodes — Tor exit node list. Useful as a separate signal (not always
// "bad", but always worth flagging for sensitive endpoints).
type TorExitNodes struct{ URL string }

func (t TorExitNodes) Name() string { return "tor-exit" }

func (t TorExitNodes) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	url := t.URL
	if url == "" {
		url = "https://www.dan.me.uk/torlist/?exit"
	}
	body, err := fetchBody(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return parseLineList(body, "tor-exit", "tor", 50), nil
}

// AbuseIPDBBlocklist — requires API key in cfg. Returns confidence-scored IPs.
type AbuseIPDBBlocklist struct {
	APIKey        string
	ConfidenceMin int // default 75
}

func (a AbuseIPDBBlocklist) Name() string { return "abuseipdb" }

func (a AbuseIPDBBlocklist) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	if strings.TrimSpace(a.APIKey) == "" {
		return nil, fmt.Errorf("abuseipdb api key required")
	}
	min := a.ConfidenceMin
	if min <= 0 {
		min = 75
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.abuseipdb.com/api/v2/blacklist?confidenceMinimum=%d&plaintext", min), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Key", a.APIKey)
	req.Header.Set("Accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("abuseipdb status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseLineList(body, "abuseipdb", "abuse", 90), nil
}

// AlienVaultOTX — pull pulse-based indicators from Open Threat Exchange.
type AlienVaultOTX struct {
	APIKey string
}

func (o AlienVaultOTX) Name() string { return "otx" }

type otxResponse struct {
	Results []struct {
		Indicators []struct {
			Indicator string `json:"indicator"`
			Type      string `json:"type"`
			Created   string `json:"created"`
		} `json:"indicators"`
		Tags []string `json:"tags"`
	} `json:"results"`
}

func (o AlienVaultOTX) Fetch(ctx context.Context, client *http.Client) ([]Indicator, error) {
	if strings.TrimSpace(o.APIKey) == "" {
		return nil, fmt.Errorf("otx api key required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://otx.alienvault.com/api/v1/pulses/subscribed?limit=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-OTX-API-KEY", o.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("otx status %d", resp.StatusCode)
	}
	var parsed otxResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	var out []Indicator
	now := time.Now().UTC()
	for _, p := range parsed.Results {
		category := strings.Join(p.Tags, ",")
		for _, ind := range p.Indicators {
			if ind.Type != "IPv4" && ind.Type != "IPv6" {
				continue
			}
			out = append(out, Indicator{
				IP: ind.Indicator, Feed: "otx", Category: category, Score: 70, FirstSeen: now,
			})
		}
	}
	return out, nil
}

// --- helpers -------------------------------------------------------------

func fetchBody(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// parseSpamhaus accepts the textual DROP/EDROP formats: "<cidr> ; SBL12345".
func parseSpamhaus(body []byte, feed string) []Indicator {
	out := []Indicator{}
	now := time.Now().UTC()
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, ";", 2)
		cidr := strings.TrimSpace(parts[0])
		if cidr == "" {
			continue
		}
		ev := ""
		if len(parts) == 2 {
			ev = strings.TrimSpace(parts[1])
		}
		out = append(out, Indicator{CIDR: cidr, Feed: feed, Category: "drop", Score: 100, FirstSeen: now, Evidence: ev})
	}
	return out
}

// parseLineList accepts one IP or CIDR per line, ignoring '#' / ';' comments.
func parseLineList(body []byte, feed, category string, score int) []Indicator {
	out := []Indicator{}
	now := time.Now().UTC()
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		// Strip inline comments.
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		ind := Indicator{Feed: feed, Category: category, Score: score, FirstSeen: now}
		if strings.Contains(line, "/") {
			ind.CIDR = line
		} else {
			ind.IP = line
		}
		out = append(out, ind)
	}
	return out
}
