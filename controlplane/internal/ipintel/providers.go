package ipintel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// providerContext is a tiny wrapper to keep the public Provider signature
// stable (we may want to inject deadlines/cancellation hooks later).
type providerContext = context.Context

// ─── ipquery (akyriako/ipquery) ────────────────────────────────────────

type ipqueryProvider struct {
	base   string
	client httpDoer
}

// NewIpqueryProvider returns a provider that calls a self-hosted
// akyriako/ipquery instance at base. The base URL must NOT include the
// /lookup path; it's appended automatically.
func NewIpqueryProvider(base string, client httpDoer) Provider {
	return &ipqueryProvider{base: strings.TrimRight(base, "/"), client: client}
}

func (p *ipqueryProvider) Name() string { return "ipquery" }

// ipqueryResponse mirrors the akyriako/ipquery JSON layout. Field names
// match the upstream README verbatim (see commit history of that repo).
type ipqueryResponse struct {
	IP  string `json:"ip"`
	ISP struct {
		ASN string `json:"asn"`
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"isp"`
	Location struct {
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		City        string  `json:"city"`
		State       string  `json:"state"`
		Zipcode     string  `json:"zipcode"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Timezone    string  `json:"timezone"`
		Localtime   string  `json:"localtime"`
	} `json:"location"`
	Risk struct {
		AbuseConfidenceScore  int    `json:"abuse_confidence_score"`
		UsageType             string `json:"usage_type"`
		IsTor                 bool   `json:"is_tor"`
		TotalReports          int    `json:"total_reports"`
		NumberOfUsersReported int    `json:"number_of_users_reported"`
		LastReportedAt        string `json:"last_reported_at"`
	} `json:"risk"`
}

func (p *ipqueryProvider) Lookup(ctx context.Context, ip string) (*Enrichment, error) {
	if p.base == "" {
		return nil, errors.New("ipquery base url not configured")
	}
	endpoint := p.base + "/lookup/" + url.PathEscape(ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		// Treat as a benign "no data" rather than a hard error.
		return &Enrichment{Addr: ip, Source: "ipquery", FetchedAt: time.Now().UTC()}, nil
	}
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("ipquery: %s: %s", res.Status, string(body))
	}
	var raw ipqueryResponse
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ipquery decode: %w", err)
	}

	out := &Enrichment{
		Addr: raw.IP,
		Geo: GeoInfo{
			Country:     raw.Location.Country,
			CountryCode: raw.Location.CountryCode,
			City:        raw.Location.City,
			Region:      raw.Location.State,
			Latitude:    raw.Location.Latitude,
			Longitude:   raw.Location.Longitude,
			Timezone:    raw.Location.Timezone,
			ASN:         raw.ISP.ASN,
			Org:         raw.ISP.Org,
			ISP:         raw.ISP.ISP,
		},
		ReputationScore: raw.Risk.AbuseConfidenceScore,
		UsageType:       raw.Risk.UsageType,
		IsTor:           raw.Risk.IsTor,
		TotalReports:    raw.Risk.TotalReports,
		LastReportedAt:  raw.Risk.LastReportedAt,
		Source:          "ipquery",
		FetchedAt:       time.Now().UTC(),
	}
	if raw.Risk.TotalReports > 0 {
		out.ThreatFeeds = append(out.ThreatFeeds, ThreatFeedHit{
			Feed:      "abuseipdb",
			Severity:  severityFromScore(raw.Risk.AbuseConfidenceScore),
			FirstSeen: raw.Risk.LastReportedAt,
		})
	}
	return out, nil
}

// ─── AbuseIPDB direct ─────────────────────────────────────────────────

type abuseIPDBProvider struct {
	apiKey string
	client httpDoer
}

// NewAbuseIPDBProvider returns a provider that queries the AbuseIPDB
// "check" endpoint directly. apiKey is required.
func NewAbuseIPDBProvider(apiKey string, client httpDoer) Provider {
	return &abuseIPDBProvider{apiKey: apiKey, client: client}
}

func (p *abuseIPDBProvider) Name() string { return "abuseipdb" }

type abuseIPDBResponse struct {
	Data struct {
		IPAddress            string   `json:"ipAddress"`
		IsPublic             bool     `json:"isPublic"`
		IPVersion            int      `json:"ipVersion"`
		IsWhitelisted        bool     `json:"isWhitelisted"`
		AbuseConfidenceScore int      `json:"abuseConfidenceScore"`
		CountryCode          string   `json:"countryCode"`
		CountryName          string   `json:"countryName"`
		UsageType            string   `json:"usageType"`
		ISP                  string   `json:"isp"`
		Domain               string   `json:"domain"`
		Hostnames            []string `json:"hostnames"`
		IsTor                bool     `json:"isTor"`
		TotalReports         int      `json:"totalReports"`
		NumDistinctUsers     int      `json:"numDistinctUsers"`
		LastReportedAt       string   `json:"lastReportedAt"`
	} `json:"data"`
	Errors []struct {
		Detail string `json:"detail"`
		Status int    `json:"status"`
	} `json:"errors"`
}

func (p *abuseIPDBProvider) Lookup(ctx context.Context, ip string) (*Enrichment, error) {
	if p.apiKey == "" {
		return nil, errors.New("abuseipdb api key not configured")
	}
	q := url.Values{}
	q.Set("ipAddress", ip)
	q.Set("maxAgeInDays", "90")
	endpoint := "https://api.abuseipdb.com/api/v2/check?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Key", p.apiKey)
	req.Header.Set("Accept", "application/json")
	res, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("abuseipdb: rate limited")
	}
	var raw abuseIPDBResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("abuseipdb decode (status %s): %w", res.Status, err)
	}
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("abuseipdb: %s", raw.Errors[0].Detail)
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("abuseipdb: %s", res.Status)
	}

	out := &Enrichment{
		Addr: raw.Data.IPAddress,
		Geo: GeoInfo{
			Country:     raw.Data.CountryName,
			CountryCode: raw.Data.CountryCode,
			ISP:         raw.Data.ISP,
		},
		ReputationScore: raw.Data.AbuseConfidenceScore,
		UsageType:       raw.Data.UsageType,
		IsTor:           raw.Data.IsTor,
		TotalReports:    raw.Data.TotalReports,
		LastReportedAt:  raw.Data.LastReportedAt,
		Source:          "abuseipdb",
		FetchedAt:       time.Now().UTC(),
	}
	if raw.Data.TotalReports > 0 {
		out.ThreatFeeds = append(out.ThreatFeeds, ThreatFeedHit{
			Feed:      "abuseipdb",
			Severity:  severityFromScore(raw.Data.AbuseConfidenceScore),
			FirstSeen: raw.Data.LastReportedAt,
		})
	}
	return out, nil
}

// severityFromScore maps an AbuseIPDB confidence score (0-100) to a
// severity tone consumable by the UI's StatusTag.
func severityFromScore(score int) string {
	switch {
	case score >= 75:
		return "critical"
	case score >= 50:
		return "high"
	case score >= 25:
		return "warning"
	default:
		return "info"
	}
}
