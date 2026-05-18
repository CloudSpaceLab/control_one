package server

import (
	"context"
	"database/sql"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/threatintel"
)

const threatIntelRefreshInterval = time.Hour

func (s *Server) startThreatIntelManager() {
	if s == nil || s.threatIntel != nil {
		return
	}
	provider := &serverThreatFeedProvider{store: s.store, sealer: s.sealer, log: s.logger}
	mgr := threatintel.New(threatintel.Config{
		RefreshInterval: threatIntelRefreshInterval,
		HTTPTimeout:     30 * time.Second,
		Sources:         s.staticThreatIntelSources(),
		Provider:        provider,
	}, s.logger)
	ctx, cancel := context.WithCancel(context.Background())
	s.threatIntel = mgr
	s.threatIntelStop = cancel
	go mgr.Start(ctx)
}

func (s *Server) staticThreatIntelSources() []threatintel.Source {
	sources := []threatintel.Source{
		threatintel.SpamhausDROP{},
		threatintel.SpamhausEDROP{},
		threatintel.FireHOLLevel1{},
		threatintel.TorExitNodes{},
	}
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.IPIntel.AbuseIPDBKey) != "" {
		sources = append(sources, threatintel.AbuseIPDBBlocklist{APIKey: s.cfg.IPIntel.AbuseIPDBKey})
	}
	return sources
}

type serverThreatFeedProvider struct {
	store  Store
	sealer *secretbox.Sealer
	log    *zap.Logger
}

func (p *serverThreatFeedProvider) Sources(ctx context.Context) ([]threatintel.ProvidedSource, error) {
	if p == nil || p.store == nil {
		return nil, nil
	}
	enabled := true
	feeds, err := p.store.ListThreatFeeds(ctx, storage.ThreatFeedFilter{Enabled: &enabled})
	if err != nil {
		return nil, err
	}
	out := make([]threatintel.ProvidedSource, 0, len(feeds))
	for _, feed := range feeds {
		apiKey, ok := p.apiKeyForFeed(feed)
		if !ok {
			continue
		}
		src, err := threatintel.SourceFromConfig(
			feed.FeedType,
			nullStringValue(feed.URL),
			apiKey,
			nullStringValue(feed.Category),
		)
		if err != nil {
			if p.log != nil {
				p.log.Warn("build threat feed source",
					zap.String("feed_id", feed.ID.String()),
					zap.String("feed_type", feed.FeedType),
					zap.Error(err))
			}
			p.store.RecordThreatFeedRefresh(ctx, feed.ID, "error", err.Error(), 0)
			continue
		}
		out = append(out, threatintel.ProvidedSource{
			ID:       feed.ID.String(),
			TenantID: feed.TenantID.String(),
			Source:   src,
		})
	}
	return out, nil
}

func (p *serverThreatFeedProvider) apiKeyForFeed(feed storage.ThreatFeed) (string, bool) {
	if len(feed.APIKeySealed) == 0 {
		return "", true
	}
	if p == nil || p.sealer == nil {
		if p != nil && p.log != nil {
			p.log.Warn("threat feed api key unavailable: secrets sealer not configured",
				zap.String("feed_id", feed.ID.String()),
				zap.String("feed_type", feed.FeedType))
		}
		return "", false
	}
	plain, err := p.sealer.Open(feed.APIKeySealed, feed.Nonce)
	if err != nil {
		if p.log != nil {
			p.log.Warn("open threat feed api key",
				zap.String("feed_id", feed.ID.String()),
				zap.Error(err))
		}
		return "", false
	}
	return strings.TrimSpace(string(plain)), true
}

func (p *serverThreatFeedProvider) OnRefresh(ctx context.Context, feedID string, status, errMsg string, count int) {
	if p == nil || p.store == nil {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(feedID))
	if err != nil {
		return
	}
	if err := p.store.RecordThreatFeedRefresh(ctx, id, status, errMsg, count); err != nil && p.log != nil {
		p.log.Warn("record threat feed refresh", zap.String("feed_id", feedID), zap.Error(err))
	}
}

func nullStringValue(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
}

func (s *Server) threatIntelIPMatches(tenantID uuid.UUID, ip net.IP) []threatintel.Indicator {
	if s == nil || s.threatIntel == nil || ip == nil {
		return nil
	}
	current := s.threatIntel.Current()
	if current == nil {
		return nil
	}
	tenant := ""
	if tenantID != uuid.Nil {
		tenant = tenantID.String()
	}
	return current.LookupIPAll(ip, tenant)
}

func threatIndicatorsToEnrichment(inds []threatintel.Indicator) ([]ipintel.ThreatFeedHit, []TFRow, int) {
	if len(inds) == 0 {
		return nil, nil, 0
	}
	sort.SliceStable(inds, func(i, j int) bool {
		if inds[i].Score != inds[j].Score {
			return inds[i].Score > inds[j].Score
		}
		return inds[i].Feed < inds[j].Feed
	})
	hits := make([]ipintel.ThreatFeedHit, 0, len(inds))
	rows := make([]TFRow, 0, len(inds))
	maxScore := 0
	seen := map[string]struct{}{}
	for _, ind := range inds {
		feed := strings.TrimSpace(ind.Feed)
		if feed == "" {
			continue
		}
		if _, ok := seen[feed]; ok {
			continue
		}
		seen[feed] = struct{}{}
		if ind.Score > maxScore {
			maxScore = ind.Score
		}
		firstSeen := ""
		if !ind.FirstSeen.IsZero() {
			firstSeen = ind.FirstSeen.UTC().Format(time.RFC3339)
		}
		severity := severityForScore(ind.Score)
		hits = append(hits, ipintel.ThreatFeedHit{
			Feed:      feed,
			Severity:  severity,
			FirstSeen: firstSeen,
		})
		rows = append(rows, TFRow{Feed: feed, Severity: severity, FirstSeen: ind.FirstSeen})
	}
	return hits, rows, maxScore
}

func mergeIPThreatFeedHits(existing, extra []ipintel.ThreatFeedHit) []ipintel.ThreatFeedHit {
	if len(existing) == 0 {
		return append([]ipintel.ThreatFeedHit(nil), extra...)
	}
	out := append([]ipintel.ThreatFeedHit(nil), existing...)
	seen := map[string]int{}
	for i, hit := range out {
		seen[strings.ToLower(strings.TrimSpace(hit.Feed))] = i
	}
	for _, hit := range extra {
		key := strings.ToLower(strings.TrimSpace(hit.Feed))
		if key == "" {
			continue
		}
		if idx, ok := seen[key]; ok {
			if out[idx].Severity == "" {
				out[idx].Severity = hit.Severity
			}
			if out[idx].FirstSeen == "" {
				out[idx].FirstSeen = hit.FirstSeen
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, hit)
	}
	return out
}
