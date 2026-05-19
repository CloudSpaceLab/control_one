package ipintel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

type stubDoer struct {
	resp *http.Response
	err  error
	last *http.Request
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	s.last = req
	return s.resp, s.err
}

func makeJSONResp(status int, body any) *http.Response {
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(b)),
		Header:     make(http.Header),
	}
}

func TestIpqueryProviderHappyPath(t *testing.T) {
	body := map[string]any{
		"ip":  "1.2.3.4",
		"isp": map[string]any{"asn": "AS15169", "org": "Google", "isp": "Google LLC"},
		"location": map[string]any{
			"country": "United States", "country_code": "US", "city": "Mountain View",
			"latitude": 37.42, "longitude": -122.08,
		},
		"risk": map[string]any{
			"abuse_confidence_score": 80, "is_tor": false, "total_reports": 12,
			"usage_type": "Data Center", "last_reported_at": "2026-04-20T00:00:00Z",
		},
	}
	doer := &stubDoer{resp: makeJSONResp(200, body)}
	p := NewIpqueryProvider("https://ipq.example/", doer)
	got, err := p.Lookup(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Geo.CountryCode != "US" {
		t.Errorf("country_code: %q", got.Geo.CountryCode)
	}
	if got.Geo.ASN != "AS15169" {
		t.Errorf("asn: %q", got.Geo.ASN)
	}
	if got.ReputationScore != 80 {
		t.Errorf("score: %d", got.ReputationScore)
	}
	if len(got.ThreatFeeds) != 1 || got.ThreatFeeds[0].Severity != "critical" {
		t.Errorf("threat feeds: %+v", got.ThreatFeeds)
	}
	if !strings.Contains(doer.last.URL.Path, "/lookup/1.2.3.4") {
		t.Errorf("path: %s", doer.last.URL.Path)
	}
}

func TestAbuseIPDBProviderHappyPath(t *testing.T) {
	body := map[string]any{
		"data": map[string]any{
			"ipAddress":            "1.2.3.4",
			"abuseConfidenceScore": 60,
			"countryCode":          "US",
			"countryName":          "United States",
			"isp":                  "Cloudflare",
			"totalReports":         5,
			"lastReportedAt":       "2026-04-20T12:00:00Z",
			"usageType":            "Content Delivery",
		},
	}
	doer := &stubDoer{resp: makeJSONResp(200, body)}
	p := NewAbuseIPDBProvider("test-key", doer)
	got, err := p.Lookup(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ReputationScore != 60 {
		t.Errorf("score: %d", got.ReputationScore)
	}
	if got.Geo.CountryCode != "US" || got.Geo.ISP != "Cloudflare" {
		t.Errorf("geo: %+v", got.Geo)
	}
	if doer.last.Header.Get("Key") != "test-key" {
		t.Errorf("auth header missing")
	}
}

func TestAbuseIPDBRateLimited(t *testing.T) {
	doer := &stubDoer{resp: &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(""))}}
	p := NewAbuseIPDBProvider("k", doer)
	if _, err := p.Lookup(context.Background(), "1.2.3.4"); err == nil {
		t.Fatal("expected rate limit error")
	}
}

func TestServiceCachesSuccess(t *testing.T) {
	body := map[string]any{
		"ip":       "8.8.8.8",
		"isp":      map[string]any{"asn": "AS15169"},
		"location": map[string]any{"country_code": "US"},
		"risk":     map[string]any{"abuse_confidence_score": 0},
	}
	doer := &stubDoer{resp: makeJSONResp(200, body)}
	cfg := config.IPIntelConfig{
		Enabled:        true,
		IpqueryBaseURL: "https://ipq.example",
		CacheTTL:       time.Minute,
		HTTPTimeout:    time.Second,
	}
	svc := New(cfg, NewMemCache())
	// Replace the auto-built primary with one using the stub doer so we
	// can observe call counts. The constructor uses a real http.Client
	// so we patch by re-assigning.
	svc.primary = NewIpqueryProvider(cfg.IpqueryBaseURL, doer)

	first, err := svc.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Source != "ipquery" {
		t.Errorf("source: %s", first.Source)
	}
	// Second call should hit cache; the stubDoer would still return the
	// same body so we check Source flips to "cache".
	doer.resp = makeJSONResp(500, map[string]any{}) // ensure provider would fail if called
	second, err := svc.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Source != "cache" {
		t.Errorf("expected cache hit, got source %s", second.Source)
	}
}

func TestServiceLookupCachedDoesNotCallProvider(t *testing.T) {
	cache := NewMemCache()
	if err := cache.Put(context.Background(), "8.8.8.8", &Enrichment{
		Addr:            "8.8.8.8",
		ReputationScore: 77,
		Source:          "abuseipdb",
	}, time.Minute); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	doer := &stubDoer{resp: makeJSONResp(500, map[string]any{})}
	cfg := config.IPIntelConfig{
		Enabled:        true,
		IpqueryBaseURL: "https://ipq.example",
		CacheTTL:       time.Minute,
		HTTPTimeout:    time.Second,
	}
	svc := New(cfg, cache)
	svc.primary = NewIpqueryProvider(cfg.IpqueryBaseURL, doer)

	got, ok, err := svc.LookupCached(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("cached lookup: %v", err)
	}
	if !ok || got == nil {
		t.Fatal("expected cached enrichment")
	}
	if got.Source != "cache" || got.ReputationScore != 77 {
		t.Fatalf("unexpected cached enrichment: %+v", got)
	}
	if doer.last != nil {
		t.Fatalf("LookupCached called provider: %s", doer.last.URL.String())
	}
}

func TestServiceDisabledWithoutProvider(t *testing.T) {
	svc := New(config.IPIntelConfig{Enabled: true}, nil)
	if svc.Enabled() {
		t.Fatal("expected disabled")
	}
	if _, err := svc.Lookup(context.Background(), "1.2.3.4"); err == nil {
		t.Fatal("expected ErrDisabled")
	}
}

func TestServiceFallsBackToSecondary(t *testing.T) {
	cfg := config.IPIntelConfig{
		Enabled:        true,
		IpqueryBaseURL: "https://ipq.example",
		AbuseIPDBKey:   "k",
		CacheTTL:       0,
		HTTPTimeout:    time.Second,
	}
	svc := New(cfg, NewMemCache())
	primaryDoer := &stubDoer{resp: &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}}
	secondaryDoer := &stubDoer{resp: makeJSONResp(200, map[string]any{
		"data": map[string]any{
			"ipAddress":            "1.1.1.1",
			"abuseConfidenceScore": 10,
			"countryCode":          "US",
		},
	})}
	svc.primary = NewIpqueryProvider(cfg.IpqueryBaseURL, primaryDoer)
	svc.secondary = NewAbuseIPDBProvider(cfg.AbuseIPDBKey, secondaryDoer)

	got, err := svc.Lookup(context.Background(), "1.1.1.1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Source != "abuseipdb" {
		t.Errorf("expected fallback source, got %s", got.Source)
	}
}

func TestServiceMergesWeakPrimaryWithSecondary(t *testing.T) {
	cfg := config.IPIntelConfig{
		Enabled:        true,
		IpqueryBaseURL: "https://ipq.example",
		AbuseIPDBKey:   "k",
		CacheTTL:       0,
		HTTPTimeout:    time.Second,
	}
	svc := New(cfg, NewMemCache())
	primaryDoer := &stubDoer{resp: makeJSONResp(200, map[string]any{
		"ip":       "45.135.193.156",
		"isp":      map[string]any{"asn": "AS51396", "org": "Pfcloud UG"},
		"location": map[string]any{"country": "Germany", "country_code": "DE", "city": "Langen"},
		"risk":     map[string]any{"abuse_confidence_score": 0, "total_reports": 0},
	})}
	secondaryDoer := &stubDoer{resp: makeJSONResp(200, map[string]any{
		"data": map[string]any{
			"ipAddress":            "45.135.193.156",
			"abuseConfidenceScore": 100,
			"countryCode":          "DE",
			"totalReports":         1083,
			"lastReportedAt":       "2026-05-18T18:00:02Z",
		},
	})}
	svc.primary = NewIpqueryProvider(cfg.IpqueryBaseURL, primaryDoer)
	svc.secondary = NewAbuseIPDBProvider(cfg.AbuseIPDBKey, secondaryDoer)

	got, err := svc.Lookup(context.Background(), "45.135.193.156")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Geo.City != "Langen" || got.Geo.ASN != "AS51396" {
		t.Fatalf("primary geo was not preserved: %+v", got.Geo)
	}
	if got.ReputationScore != 100 || got.TotalReports != 1083 {
		t.Fatalf("secondary abuse score not merged: %+v", got)
	}
	if len(got.ThreatFeeds) != 1 || got.ThreatFeeds[0].Feed != "abuseipdb" {
		t.Fatalf("secondary threat feed not merged: %+v", got.ThreatFeeds)
	}
	if got.Source != "ipquery+abuseipdb" {
		t.Fatalf("source = %q", got.Source)
	}
}

func TestSeverityFromScore(t *testing.T) {
	cases := []struct {
		s int
		w string
	}{
		{100, "critical"}, {80, "critical"}, {60, "high"}, {30, "warning"}, {10, "info"},
	}
	for _, c := range cases {
		if got := severityFromScore(c.s); got != c.w {
			t.Errorf("severity(%d) = %s, want %s", c.s, got, c.w)
		}
	}
}
