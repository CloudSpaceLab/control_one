package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// InvestigateStore is the optional storage interface backing the SIEM
// Investigate endpoints. It is intentionally separate from the main
// Store interface so existing fakes don't have to satisfy it — handlers
// type-assert at request time and return 503 when the backend doesn't
// implement it.
type InvestigateStore interface {
	CreateSavedSearch(ctx context.Context, in storage.SavedSearch) (*storage.SavedSearch, error)
	ListSavedSearches(ctx context.Context, tenantID, userID uuid.UUID, limit, offset int) ([]storage.SavedSearch, int, error)
	GetSavedSearch(ctx context.Context, id uuid.UUID) (*storage.SavedSearch, error)
	UpdateSavedSearch(ctx context.Context, id uuid.UUID, in storage.SavedSearch) (*storage.SavedSearch, error)
	DeleteSavedSearch(ctx context.Context, id uuid.UUID) error
	AddEntityTag(ctx context.Context, t storage.EntityTag) (*storage.EntityTag, error)
	RemoveEntityTag(ctx context.Context, tenantID uuid.UUID, entityType, entityID, tag string) error
	ListEntityTags(ctx context.Context, tenantID uuid.UUID, entityType, entityID string) ([]storage.EntityTag, error)
	RecordEntityAction(ctx context.Context, a storage.EntityAction) (*storage.EntityAction, error)
	ListAssetCIDRs(ctx context.Context, tenantID uuid.UUID) ([]net.IPNet, error)
	EntityLifecycle(ctx context.Context, f storage.LifecycleFilter, limit int) ([]storage.LifecycleItem, error)
	EntitySummary(ctx context.Context, tenantID uuid.UUID, entityType, entityID string) (*storage.EntitySummary, error)
}

// investigateBackend extracts the InvestigateStore implementation from
// the server's main store. Returns nil when not available.
func (s *Server) investigateBackend() InvestigateStore {
	if s == nil || s.store == nil {
		return nil
	}
	if v, ok := s.store.(InvestigateStore); ok {
		return v
	}
	return nil
}

// ===== Search =====

type searchFacet struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type searchItem struct {
	Type           string               `json:"type"`
	ID             string               `json:"id"`
	Score          float64              `json:"score"`
	Snippet        string               `json:"snippet,omitempty"`
	Classification []ClassificationChip `json:"classification,omitempty"`
}

type searchResponse struct {
	Query      string        `json:"query"`
	Detected   string        `json:"detected_type,omitempty"`
	Facets     []searchFacet `json:"facets"`
	Items      []searchItem  `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (s *Server) handleInvestigateSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}

	// Detect entity type from the query, unless caller pinned it.
	detected, _ := ClassifyValue(q)
	requestedTypes := splitCSV(r.URL.Query().Get("types"))

	tenantID := tenantFromQuery(r)

	resp := searchResponse{Query: q, Detected: detected, Facets: []searchFacet{}, Items: []searchItem{}}

	// If we know the type, surface a single primary item for the entity
	// itself plus a facet so the UI can pivot.
	if detected != "" && (len(requestedTypes) == 0 || hasString(requestedTypes, detected)) {
		item := searchItem{Type: detected, ID: q, Score: 1.0, Snippet: q}
		if detected == EntityTypeIP {
			if ib := s.investigateBackend(); ib != nil && tenantID != uuid.Nil {
				assets, _ := ib.ListAssetCIDRs(r.Context(), tenantID)
				item.Classification = ClassifyIP(q, assets, nil)
			} else {
				item.Classification = ClassifyIP(q, nil, nil)
			}
			// Overlay a NODE chip when the IP belongs to a registered agent.
			// Searches across all tenants — no tenant filter needed because
			// IP ownership is unambiguous and the caller is already authed.
			if s.store != nil {
				if nodes, err := s.store.FindNodesByPublicIP(r.Context(), q); err == nil && len(nodes) > 0 {
					n := nodes[0]
					label := "NODE"
					if n.Hostname != "" {
						label = "NODE:" + n.Hostname
					}
					item.Classification = append(
						[]ClassificationChip{{Label: label, Severity: "healthy"}},
						item.Classification...,
					)
				}
			}
		}
		resp.Items = append(resp.Items, item)
		resp.Facets = append(resp.Facets, searchFacet{Type: detected, Count: 1})
	}

	// Free-text search: entity_tags first, then Doris telemetry_logs.
	if detected == "" {
		if ib := s.investigateBackend(); ib != nil && tenantID != uuid.Nil {
			tags, _ := ib.ListEntityTags(r.Context(), tenantID, "", q)
			for _, tag := range tags {
				resp.Items = append(resp.Items, searchItem{
					Type:    tag.EntityType,
					ID:      tag.EntityID,
					Score:   0.9,
					Snippet: tag.Tag,
				})
			}
			if len(tags) > 0 {
				resp.Facets = append(resp.Facets, searchFacet{Type: "tag", Count: len(tags)})
			}
		}

		// Doris telemetry log search for broader text matching.
		if s.dorisClient != nil && tenantID != uuid.Nil {
			logs, _, err := s.dorisClient.SearchLogs(r.Context(), doris.LogSearchParams{
				TenantID: tenantID.String(),
				Search:   q,
				Since:    time.Now().UTC().Add(-30 * 24 * time.Hour),
				Until:    time.Now().UTC(),
				Limit:    10,
			})
			if err != nil {
				s.logger.Warn("investigate log search", zap.Error(err))
			} else {
				for _, lr := range logs {
					snippet := lr.Message
					if len(snippet) > 200 {
						snippet = snippet[:200] + "…"
					}
					resp.Items = append(resp.Items, searchItem{
						Type:    "log",
						ID:      lr.NodeID + ":" + lr.Source,
						Score:   0.7,
						Snippet: snippet,
					})
				}
				if len(logs) > 0 {
					resp.Facets = append(resp.Facets, searchFacet{Type: "log", Count: len(logs)})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ===== Entity overview =====

func (s *Server) handleEntityOverview(w http.ResponseWriter, r *http.Request, entityType, entityID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		writeJSON(w, http.StatusOK, &storage.EntitySummary{Type: entityType, ID: entityID, Counts: map[string]int{}})
		return
	}

	tenantID := tenantFromQuery(r)
	summary, err := ib.EntitySummary(r.Context(), tenantID, entityType, entityID)
	if err != nil {
		s.logger.Warn("entity summary", zap.Error(err))
		writeJSON(w, http.StatusOK, &storage.EntitySummary{Type: entityType, ID: entityID, Counts: map[string]int{}})
		return
	}

	// Tack on classification chips for IPs.
	if entityType == EntityTypeIP {
		assets, _ := ib.ListAssetCIDRs(r.Context(), tenantID)
		chips := ClassifyIP(entityID, assets, nil)
		// Prepend NODE chip when IP is a registered agent.
		if s.store != nil {
			if nodes, err := s.store.FindNodesByPublicIP(r.Context(), entityID); err == nil && len(nodes) > 0 {
				n := nodes[0]
				label := "NODE"
				if n.Hostname != "" {
					label = "NODE:" + n.Hostname
				}
				chips = append([]ClassificationChip{{Label: label, Severity: "healthy"}}, chips...)
			}
		}
		summary.Meta = map[string]any{"classification": chips}
	}

	writeJSON(w, http.StatusOK, summary)
}

// ===== Lifecycle =====

type lifecycleResponse struct {
	Items      []storage.LifecycleItem `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

// lifecycleCursor encodes the (timestamp, raw_id) of the last delivered
// row so a follow-up request can resume after it. Opaque base64 — never
// exposed to the client semantically.
type lifecycleCursor struct {
	TS time.Time `json:"ts"`
	ID string    `json:"id"`
}

func encodeCursor(c lifecycleCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (lifecycleCursor, error) {
	var c lifecycleCursor
	if s == "" {
		return c, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

func (s *Server) handleEntityLifecycle(w http.ResponseWriter, r *http.Request, entityType, entityID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		writeJSON(w, http.StatusOK, lifecycleResponse{Items: []storage.LifecycleItem{}})
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= maxListLimit {
			limit = parsed
		}
	}

	tenantID := tenantFromQuery(r)
	filter := storage.LifecycleFilter{
		TenantID:   tenantID,
		EntityType: entityType,
		EntityID:   entityID,
		Sources:    splitCSV(r.URL.Query().Get("sources")),
	}
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Since = &t
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("until")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Until = &t
		}
	}
	if cur := strings.TrimSpace(r.URL.Query().Get("cursor")); cur != "" {
		if c, err := decodeCursor(cur); err == nil && !c.TS.IsZero() {
			until := c.TS
			filter.Until = &until
		}
	}

	items, err := ib.EntityLifecycle(r.Context(), filter, limit+1)
	if err != nil {
		s.logger.Warn("entity lifecycle", zap.Error(err))
		writeJSON(w, http.StatusOK, lifecycleResponse{Items: []storage.LifecycleItem{}})
		return
	}

	resp := lifecycleResponse{}
	if len(items) > limit {
		last := items[limit-1]
		resp.NextCursor = encodeCursor(lifecycleCursor{TS: last.Timestamp, ID: last.RawID})
		items = items[:limit]
	}
	resp.Items = items
	writeJSON(w, http.StatusOK, resp)
}

// ===== Related =====

type relatedItem struct {
	Type          string  `json:"type"`
	ID            string  `json:"id"`
	Score         float64 `json:"score"`
	CoOccurrences int     `json:"co_occurrences"`
}

type relatedResponse struct {
	Related []relatedItem `json:"related"`
}

func (s *Server) handleEntityRelated(w http.ResponseWriter, r *http.Request, entityType, entityID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	// TODO(investigate): compute true co-occurrences from Doris event store.
	writeJSON(w, http.StatusOK, relatedResponse{Related: []relatedItem{}})
}

// ===== IP enrich =====

type ipGeoBlock struct {
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

type ipEnrichResponse struct {
	Address         string                  `json:"addr"`
	Classification  []ClassificationChip    `json:"classification"`
	Geo             ipGeoBlock              `json:"geo"`
	ThreatFeeds     []ipintel.ThreatFeedHit `json:"threat_feeds"`
	ReputationScore int                     `json:"reputation_score"`
	UsageType       string                  `json:"usage_type,omitempty"`
	IsTor           bool                    `json:"is_tor,omitempty"`
	TotalReports    int                     `json:"total_reports,omitempty"`
	LastReportedAt  string                  `json:"last_reported_at,omitempty"`
	Source          string                  `json:"source,omitempty"`
}

func (s *Server) handleIPEnrich(w http.ResponseWriter, r *http.Request, addr string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	ip := net.ParseIP(addr)
	if ip == nil {
		http.Error(w, "invalid ip", http.StatusBadRequest)
		return
	}

	tenantID := tenantFromQuery(r)
	var assets []net.IPNet
	if ib := s.investigateBackend(); ib != nil && tenantID != uuid.Nil {
		assets, _ = ib.ListAssetCIDRs(r.Context(), tenantID)
	}

	resp := ipEnrichResponse{
		Address:     addr,
		Geo:         ipGeoBlock{},
		ThreatFeeds: []ipintel.ThreatFeedHit{},
	}

	// Live enrichment via ipquery / AbuseIPDB. Failures degrade silently —
	// classification (RFC1918 / asset / threat-feed catalog) still works.
	var threatFeedRows []TFRow
	if s.ipIntel != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		if e, err := s.ipIntel.Lookup(ctx, addr); err == nil && e != nil {
			resp.Geo = ipGeoBlock{
				Country:     e.Geo.Country,
				CountryCode: e.Geo.CountryCode,
				City:        e.Geo.City,
				Region:      e.Geo.Region,
				Latitude:    e.Geo.Latitude,
				Longitude:   e.Geo.Longitude,
				Timezone:    e.Geo.Timezone,
				ASN:         e.Geo.ASN,
				Org:         e.Geo.Org,
				ISP:         e.Geo.ISP,
			}
			resp.ThreatFeeds = e.ThreatFeeds
			resp.ReputationScore = e.ReputationScore
			resp.UsageType = e.UsageType
			resp.IsTor = e.IsTor
			resp.TotalReports = e.TotalReports
			resp.LastReportedAt = e.LastReportedAt
			resp.Source = e.Source
			// Convert threat-feed hits into the legacy TFRow shape consumed
			// by ClassifyIP so the chip set reflects external feeds too.
			for _, t := range e.ThreatFeeds {
				threatFeedRows = append(threatFeedRows, TFRow{Feed: t.Feed, Severity: t.Severity})
			}
		} else if err != nil && !errors.Is(err, ipintel.ErrDisabled) {
			s.logger.Warn("ipintel lookup failed",
				zap.String("addr", addr),
				zap.Error(err))
		}
	}

	resp.Classification = ClassifyIP(addr, assets, threatFeedRows)
	if resp.IsTor {
		resp.Classification = append(resp.Classification, ClassificationChip{Label: "TOR", Severity: "critical"})
	}
	if cutoff := func() int {
		if s.ipIntel != nil {
			return s.ipIntel.AbuseScoreCutoff()
		}
		return 25
	}(); resp.ReputationScore >= cutoff && resp.ReputationScore > 0 {
		resp.Classification = append(resp.Classification, ClassificationChip{
			Label:    fmt.Sprintf("ABUSE %d/100", resp.ReputationScore),
			Severity: severityForScore(resp.ReputationScore),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// severityForScore maps an AbuseIPDB confidence score (0-100) to the UI
// tone vocabulary. Mirrors ipintel.severityFromScore but kept private to
// the server package to avoid widening the ipintel API surface.
func severityForScore(score int) string {
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

// ===== Process tree =====

type processNode struct {
	Name      string         `json:"name"`
	Hash      string         `json:"hash,omitempty"`
	Signature string         `json:"signature,omitempty"`
	Parent    *processNode   `json:"parent,omitempty"`
	Children  []*processNode `json:"children,omitempty"`
}

type processTreeResponse struct {
	Root  processNode `json:"root"`
	Hosts []string    `json:"hosts"`
}

func (s *Server) handleProcessTree(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	// TODO(investigate): when a process_tree table or Doris view exists,
	// hydrate parent + children. For now return the single queried node.
	writeJSON(w, http.StatusOK, processTreeResponse{
		Root:  processNode{Name: key, Children: []*processNode{}},
		Hosts: []string{},
	})
}

// ===== Saved searches =====

type savedSearchPayload struct {
	Name       string          `json:"name"`
	Query      string          `json:"query"`
	EntityType string          `json:"entity_type"`
	Filters    json.RawMessage `json:"filters,omitempty"`
	Shared     bool            `json:"shared"`
}

func (s *Server) handleSavedSearchesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.savedSearchesList(w, r)
	case http.MethodPost:
		s.savedSearchesCreate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) savedSearchesList(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		writeJSON(w, http.StatusOK, paginatedResponse[storage.SavedSearch]{Data: []storage.SavedSearch{}})
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tenantID := tenantFromQuery(r)
	userID := principalUserID(s, r.Context(), principal)
	items, total, err := ib.ListSavedSearches(r.Context(), tenantID, userID, limit, offset)
	if err != nil {
		s.logger.Warn("list saved searches", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, paginatedResponse[storage.SavedSearch]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) savedSearchesCreate(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		http.Error(w, "investigate store unavailable", http.StatusServiceUnavailable)
		return
	}
	var p savedSearchPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	tenantID := tenantFromQuery(r)
	userID := principalUserID(s, r.Context(), principal)
	row, err := ib.CreateSavedSearch(r.Context(), storage.SavedSearch{
		TenantID:    tenantID,
		OwnerUserID: userID,
		Name:        strings.TrimSpace(p.Name),
		Query:       p.Query,
		EntityType:  strings.TrimSpace(p.EntityType),
		Filters:     p.Filters,
		Shared:      p.Shared,
	})
	if err != nil {
		s.logger.Warn("create saved search", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (s *Server) handleSavedSearchSubroute(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/api/v1/saved-searches/")
	id, err := uuid.Parse(strings.TrimSuffix(tail, "/"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.savedSearchesUpdate(w, r, id)
	case http.MethodDelete:
		s.savedSearchesDelete(w, r, id)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) savedSearchesUpdate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		http.Error(w, "investigate store unavailable", http.StatusServiceUnavailable)
		return
	}
	existing, err := ib.GetSavedSearch(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	userID := principalUserID(s, r.Context(), principal)
	if existing.OwnerUserID != userID && !(existing.Shared && hasRole(principal, roleAdmin)) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	var p savedSearchPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	row, err := ib.UpdateSavedSearch(r.Context(), id, storage.SavedSearch{
		Name:       strings.TrimSpace(p.Name),
		Query:      p.Query,
		EntityType: strings.TrimSpace(p.EntityType),
		Filters:    p.Filters,
		Shared:     p.Shared,
	})
	if err != nil {
		s.logger.Warn("update saved search", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) savedSearchesDelete(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		http.Error(w, "investigate store unavailable", http.StatusServiceUnavailable)
		return
	}
	existing, err := ib.GetSavedSearch(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	userID := principalUserID(s, r.Context(), principal)
	if existing.OwnerUserID != userID && !(existing.Shared && hasRole(principal, roleAdmin)) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err := ib.DeleteSavedSearch(r.Context(), id); err != nil {
		s.logger.Warn("delete saved search", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===== Entity subroute dispatcher =====

// handleEntitySubroutes dispatches /api/v1/entities/{type}/{id}[/...] to
// the appropriate per-resource handler.
func (s *Server) handleEntitySubroutes(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/api/v1/entities/")
	parts := strings.Split(tail, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	entityType := parts[0]
	entityID := parts[1]
	rest := parts[2:]

	if len(rest) == 0 {
		s.handleEntityOverview(w, r, entityType, entityID)
		return
	}

	switch rest[0] {
	case "lifecycle":
		s.handleEntityLifecycle(w, r, entityType, entityID)
	case "related":
		s.handleEntityRelated(w, r, entityType, entityID)
	case "enrich":
		if entityType != EntityTypeIP {
			http.Error(w, "enrich only supported for ip", http.StatusBadRequest)
			return
		}
		s.handleIPEnrich(w, r, entityID)
	case "tree":
		if entityType != EntityTypeProcess {
			http.Error(w, "tree only supported for process", http.StatusBadRequest)
			return
		}
		s.handleProcessTree(w, r, entityID)
	case "tags":
		if len(rest) == 1 {
			s.handleEntityTagsCollection(w, r, entityType, entityID)
			return
		}
		s.handleEntityTagDelete(w, r, entityType, entityID, rest[1])
	case "actions":
		s.handleEntityActions(w, r, entityType, entityID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// ===== Tags =====

func (s *Server) handleEntityTagsCollection(w http.ResponseWriter, r *http.Request, entityType, entityID string) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		ib := s.investigateBackend()
		if ib == nil {
			writeJSON(w, http.StatusOK, []storage.EntityTag{})
			return
		}
		items, err := ib.ListEntityTags(r.Context(), tenantFromQuery(r), entityType, entityID)
		if err != nil {
			s.logger.Warn("list entity tags", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if items == nil {
			items = []storage.EntityTag{}
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		ib := s.investigateBackend()
		if ib == nil {
			http.Error(w, "investigate store unavailable", http.StatusServiceUnavailable)
			return
		}
		var p struct {
			Tag string `json:"tag"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		tag := strings.TrimSpace(p.Tag)
		if tag == "" {
			http.Error(w, "tag required", http.StatusBadRequest)
			return
		}
		userID := principalUserID(s, r.Context(), principal)
		var creator *uuid.UUID
		if userID != uuid.Nil {
			id := userID
			creator = &id
		}
		row, err := ib.AddEntityTag(r.Context(), storage.EntityTag{
			TenantID:   tenantFromQuery(r),
			EntityType: entityType,
			EntityID:   entityID,
			Tag:        tag,
			CreatedBy:  creator,
		})
		if err != nil {
			s.logger.Warn("add entity tag", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, row)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEntityTagDelete(w http.ResponseWriter, r *http.Request, entityType, entityID, tag string) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		http.Error(w, "investigate store unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := ib.RemoveEntityTag(r.Context(), tenantFromQuery(r), entityType, entityID, tag); err != nil {
		s.logger.Warn("remove entity tag", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===== Actions =====

type entityActionPayload struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
	TTL    int    `json:"ttl"`
	// Scope controls how widely a block/allow is enforced when entity_type=ip:
	//   - "" or "affected" (default): nodes that have seen traffic to/from the
	//     IP in the last 7 days, per process_connections in Doris.
	//   - "fleet": every enrolled node in the tenant.
	// Other entity types ignore this field.
	Scope string `json:"scope,omitempty"`
}

// entityActionResponse extends the stored EntityAction row with the rolled-up
// fan-out result for ip blocks. Counts are zero for non-ip entity types.
type entityActionResponse struct {
	*storage.EntityAction
	NodesDispatched int      `json:"nodes_dispatched"`
	NodeIDs         []string `json:"node_ids,omitempty"`
	Scope           string   `json:"scope,omitempty"`
}

func (s *Server) handleEntityActions(w http.ResponseWriter, r *http.Request, entityType, entityID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	ib := s.investigateBackend()
	if ib == nil {
		http.Error(w, "investigate store unavailable", http.StatusServiceUnavailable)
		return
	}
	var p entityActionPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	switch action {
	case "block", "allow", "quarantine":
	default:
		http.Error(w, "action must be block|allow|quarantine", http.StatusBadRequest)
		return
	}

	tenantID := tenantFromQuery(r)
	userID := principalUserID(s, r.Context(), principal)
	var creator *uuid.UUID
	if userID != uuid.Nil {
		id := userID
		creator = &id
	}

	ea := storage.EntityAction{
		TenantID:   tenantID,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		Reason:     strings.TrimSpace(p.Reason),
		CreatedBy:  creator,
	}
	if p.TTL > 0 {
		ttl := p.TTL
		exp := time.Now().UTC().Add(time.Duration(ttl) * time.Second)
		ea.TTLSeconds = &ttl
		ea.ExpiresAt = &exp
	}
	row, err := ib.RecordEntityAction(r.Context(), ea)
	if err != nil {
		s.logger.Warn("record entity action", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Mirror to audit_logs so existing audit pipelines pick it up.
	s.recordAudit(r.Context(), principal, tenantID,
		"entity."+action, entityType, entityID,
		map[string]any{
			"reason": ea.Reason,
			"ttl":    ea.TTLSeconds,
			"scope":  p.Scope,
		},
	)

	// Enforcement fan-out: only IP blocks/allows actually push firewall jobs.
	// Other entity types (process, file, host, …) and quarantine remain
	// audit-only — no node enforcement defined for them yet.
	resp := entityActionResponse{EntityAction: row, Scope: p.Scope}
	if entityType == "ip" && (action == "block" || action == "allow") {
		nodes, scope, ferr := s.fanOutFirewallAction(r.Context(), tenantID, row, entityID, p.Scope, p.TTL)
		if ferr != nil {
			s.logger.Warn("fan out firewall action", zap.Error(ferr), zap.String("entity_id", entityID))
			// Don't fail the request — the entity_action is recorded; surface
			// the partial state via the response so the operator can retry.
		}
		resp.NodesDispatched = len(nodes)
		resp.Scope = scope
		for _, n := range nodes {
			resp.NodeIDs = append(resp.NodeIDs, n.String())
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// fanOutFirewallAction resolves the affected nodes for an IP entity_action
// and dispatches a firewall.rule_add (block) or firewall.rule_delete (allow)
// per node. Returns the list of node UUIDs that received a dispatch and the
// effective scope used (may differ from the requested scope when affected
// returns nothing and we fall back to fleet).
func (s *Server) fanOutFirewallAction(
	ctx context.Context,
	tenantID uuid.UUID,
	row *storage.EntityAction,
	ip, requestedScope string,
	ttlSeconds int,
) ([]uuid.UUID, string, error) {
	if row == nil {
		return nil, "", errors.New("nil entity action row")
	}
	scope := strings.ToLower(strings.TrimSpace(requestedScope))
	if scope != "fleet" && scope != "affected" {
		scope = "affected"
	}

	var nodes []uuid.UUID
	if scope == "affected" {
		affected, err := s.resolveAffectedNodesForIP(ctx, tenantID.String(), ip)
		if err != nil {
			return nil, scope, fmt.Errorf("resolve affected nodes: %w", err)
		}
		nodes = affected
		// If no historical hits and the operator didn't ask for fleet scope
		// explicitly, surface as a no-op rather than silently expanding to
		// the whole fleet (operators rarely want that by accident).
		if len(nodes) == 0 {
			return nil, scope, nil
		}
	} else {
		// scope == "fleet"
		all, _, err := s.store.ListNodes(ctx, tenantID, "", 1000, 0)
		if err != nil {
			return nil, scope, fmt.Errorf("list nodes: %w", err)
		}
		for i := range all {
			nodes = append(nodes, all[i].ID)
		}
	}

	var ttl *int
	if ttlSeconds > 0 {
		t := ttlSeconds
		ttl = &t
	}
	dispatched := make([]uuid.UUID, 0, len(nodes))
	action := row.Action
	for _, nid := range nodes {
		if _, _, err := s.dispatchFirewallRule(ctx, tenantID, row.ID, nid, action, ip, row.Reason, ttl); err != nil {
			s.logger.Warn("dispatch firewall rule",
				zap.Error(err),
				zap.String("node_id", nid.String()),
				zap.String("ip", ip),
			)
			continue
		}
		dispatched = append(dispatched, nid)
	}
	return dispatched, scope, nil
}

// ===== Helpers =====

func tenantFromQuery(r *http.Request) uuid.UUID {
	v := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if v == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func splitCSV(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}

func hasRole(p *auth.Principal, role string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if strings.EqualFold(strings.TrimSpace(r), role) {
			return true
		}
	}
	return false
}

// principalUserID resolves the storage user_id for a principal. Returns
// uuid.Nil when no matching user row is found — callers treat that as
// "anonymous owner" and tags / saved searches are stored without an
// attributable creator.
func principalUserID(s *Server, ctx context.Context, p *auth.Principal) uuid.UUID {
	if p == nil || s == nil || s.store == nil {
		return uuid.Nil
	}
	if strings.TrimSpace(p.Subject) == "" {
		return uuid.Nil
	}
	user, err := s.store.GetUserByExternalID(ctx, p.Subject)
	if err != nil || user == nil {
		return uuid.Nil
	}
	return user.ID
}
