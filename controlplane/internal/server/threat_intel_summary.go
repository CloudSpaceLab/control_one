package server

import (
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/threatintel"
)

type threatIntelSummaryResponse struct {
	Available        bool                       `json:"available"`
	GeneratedAt      string                     `json:"generated_at,omitempty"`
	TotalIndicators  int                        `json:"total_indicators"`
	GlobalIndicators int                        `json:"global_indicators"`
	TenantIndicators int                        `json:"tenant_indicators"`
	Sources          []threatIntelSourceSummary `json:"sources"`
	Lookup           *threatIntelLookupResult   `json:"lookup,omitempty"`
}

type threatIntelSourceSummary struct {
	Feed       string                       `json:"feed"`
	Category   string                       `json:"category,omitempty"`
	Scope      string                       `json:"scope"`
	TenantID   string                       `json:"tenant_id,omitempty"`
	Indicators int                          `json:"indicators"`
	MaxScore   int                          `json:"max_score"`
	Sample     []threatIntelIndicatorSample `json:"sample,omitempty"`
}

type threatIntelLookupResult struct {
	IP      string                       `json:"ip"`
	Listed  bool                         `json:"listed"`
	Score   int                          `json:"score"`
	Feeds   []string                     `json:"feeds"`
	Matches []threatIntelIndicatorSample `json:"matches"`
}

type threatIntelIndicatorSample struct {
	Feed      string `json:"feed,omitempty"`
	Category  string `json:"category,omitempty"`
	IP        string `json:"ip,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	Score     int    `json:"score"`
	FirstSeen string `json:"first_seen,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
}

func (s *Server) handleThreatIntelSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := threatIntelSummaryResponse{Sources: []threatIntelSourceSummary{}}
	if s == nil || s.threatIntel == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	current := s.threatIntel.Current()
	if current == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Available = true
	resp.GeneratedAt = formatTime(current.Updated)
	tenant := tenantID.String()
	bySource := map[string]*threatIntelSourceSummary{}
	for _, ind := range current.All() {
		indTenant := strings.TrimSpace(ind.TenantID)
		if indTenant != "" && indTenant != tenant {
			continue
		}
		resp.TotalIndicators++
		scope := "global"
		if indTenant != "" {
			scope = "tenant"
			resp.TenantIndicators++
		} else {
			resp.GlobalIndicators++
		}
		feed := strings.TrimSpace(ind.Feed)
		if feed == "" {
			feed = "unknown"
		}
		key := strings.Join([]string{scope, indTenant, feed, strings.TrimSpace(ind.Category)}, "|")
		src := bySource[key]
		if src == nil {
			src = &threatIntelSourceSummary{
				Feed:     feed,
				Category: strings.TrimSpace(ind.Category),
				Scope:    scope,
				TenantID: indTenant,
			}
			bySource[key] = src
		}
		src.Indicators++
		if ind.Score > src.MaxScore {
			src.MaxScore = ind.Score
		}
		if len(src.Sample) < 5 {
			src.Sample = append(src.Sample, newThreatIntelIndicatorSample(ind))
		}
	}
	for _, src := range bySource {
		resp.Sources = append(resp.Sources, *src)
	}
	sort.SliceStable(resp.Sources, func(i, j int) bool {
		if resp.Sources[i].Indicators != resp.Sources[j].Indicators {
			return resp.Sources[i].Indicators > resp.Sources[j].Indicators
		}
		if resp.Sources[i].MaxScore != resp.Sources[j].MaxScore {
			return resp.Sources[i].MaxScore > resp.Sources[j].MaxScore
		}
		return resp.Sources[i].Feed < resp.Sources[j].Feed
	})

	if rawIP := strings.TrimSpace(r.URL.Query().Get("ip")); rawIP != "" {
		ip := net.ParseIP(rawIP)
		if ip == nil {
			http.Error(w, "invalid ip", http.StatusBadRequest)
			return
		}
		matches := current.LookupIPAll(ip, tenant)
		lookup := threatIntelLookupResult{
			IP:      rawIP,
			Listed:  len(matches) > 0,
			Matches: make([]threatIntelIndicatorSample, 0, len(matches)),
		}
		seenFeeds := map[string]struct{}{}
		for _, ind := range matches {
			if ind.Score > lookup.Score {
				lookup.Score = ind.Score
			}
			feed := strings.TrimSpace(ind.Feed)
			if feed != "" {
				if _, ok := seenFeeds[feed]; !ok {
					seenFeeds[feed] = struct{}{}
					lookup.Feeds = append(lookup.Feeds, feed)
				}
			}
			lookup.Matches = append(lookup.Matches, newThreatIntelIndicatorSample(ind))
		}
		sort.Strings(lookup.Feeds)
		resp.Lookup = &lookup
	}

	writeJSON(w, http.StatusOK, resp)
}

func newThreatIntelIndicatorSample(ind threatintel.Indicator) threatIntelIndicatorSample {
	sample := threatIntelIndicatorSample{
		Feed:     strings.TrimSpace(ind.Feed),
		Category: strings.TrimSpace(ind.Category),
		IP:       strings.TrimSpace(ind.IP),
		CIDR:     strings.TrimSpace(ind.CIDR),
		Score:    ind.Score,
		Evidence: strings.TrimSpace(ind.Evidence),
	}
	if !ind.FirstSeen.IsZero() {
		sample.FirstSeen = formatTime(ind.FirstSeen)
	}
	return sample
}
