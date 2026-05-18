package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type ipBehaviorRollupStore interface {
	IncrementIPBehaviorRollups(context.Context, []storage.IPBehaviorObservation) error
}

type ipBehaviorFindingStore interface {
	UpsertIPBehaviorFinding(context.Context, storage.UpsertIPBehaviorFindingParams) (*storage.IPBehaviorFinding, error)
}

type ipBehaviorQueryStore interface {
	ListIPBehaviorCountries(context.Context, uuid.UUID, time.Time, string) ([]storage.IPBehaviorCountrySummary, error)
	GetIPBehaviorIPProfile(context.Context, uuid.UUID, string, time.Time) (*storage.IPBehaviorIPProfile, error)
	ListIPBehaviorBaselines(context.Context, uuid.UUID, string, int, int) ([]storage.IPBehaviorBaseline, int, error)
}

type ipBehaviorASNSourceStore interface {
	ListIPBehaviorSourceIPsByASN(context.Context, uuid.UUID, string, time.Time, int) ([]storage.IPBehaviorASNSource, error)
}

type ipBehaviorBaselineLookupStore interface {
	GetBehavioralBaseline(context.Context, uuid.UUID, *uuid.UUID, string, string) (*storage.BehavioralBaseline, error)
}

type ipBlockProposalStore interface {
	CreateIPBlocklistEntry(context.Context, storage.CreateIPBlocklistEntryParams) (*storage.IPBlocklistEntry, error)
	GetIPBlocklistEntry(context.Context, uuid.UUID) (*storage.IPBlocklistEntry, error)
	SetIPBlocklistEntryEntityAction(context.Context, uuid.UUID, uuid.UUID) (*storage.IPBlocklistEntry, error)
	UpdateIPBlocklistEntryStatus(context.Context, uuid.UUID, string, *uuid.UUID, string) (*storage.IPBlocklistEntry, error)
}

type ipBlockProposalQueryStore interface {
	ListIPBlocklistEntries(context.Context, storage.IPBlocklistEntryFilter, int, int) ([]storage.IPBlocklistEntry, int, error)
}

type ipBlockProposalEntityActionStore interface {
	GetIPBlocklistEntryByEntityAction(context.Context, uuid.UUID) (*storage.IPBlocklistEntry, error)
	UpdateIPBlocklistEntryStatus(context.Context, uuid.UUID, string, *uuid.UUID, string) (*storage.IPBlocklistEntry, error)
}

type ipBlockSafetyStore interface {
	CountRecentIPBlocklistEntries(context.Context, uuid.UUID, time.Time) (int, error)
	CountRecentIPBlocklistEntriesForServerGroup(context.Context, uuid.UUID, string, time.Time) (int, error)
	CountRecentIPBlocklistEntriesGlobal(context.Context, time.Time) (int, error)
}

type ipBlockExpiryStore interface {
	ListExpiredIPBlocklistEntries(context.Context, time.Time, int) ([]storage.IPBlocklistEntry, error)
	ListActiveIPBlocklistEntriesForNode(context.Context, uuid.UUID, uuid.UUID, time.Time, int) ([]storage.IPBlocklistEntry, error)
	SetIPBlocklistEntryEntityAction(context.Context, uuid.UUID, uuid.UUID) (*storage.IPBlocklistEntry, error)
	UpdateIPBlocklistEntryStatus(context.Context, uuid.UUID, string, *uuid.UUID, string) (*storage.IPBlocklistEntry, error)
}

type nodeFirewallRemovalStore interface {
	ListNodeFirewallRulesForEntityAction(context.Context, uuid.UUID) ([]storage.NodeFirewallRule, error)
	QueueNodeFirewallRuleRemoval(context.Context, uuid.UUID, uuid.UUID) error
}

type webserverBlockEntryActionStore interface {
	ListWebserverConfigActionsForBlockEntry(context.Context, uuid.UUID, uuid.UUID) ([]storage.WebserverConfigAction, error)
}

const networkBlockCircuitRuleID = "network.block_proposals"

func (s *Server) recordIPBehaviorRollups(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) {
	store, ok := s.store.(ipBehaviorRollupStore)
	if !ok || store == nil {
		return
	}
	obs := make([]storage.IPBehaviorObservation, 0)
	for i := range events {
		ev := &events[i]
		if ev.Type != "web.request" && ev.Type != "web.error" {
			continue
		}
		if net.ParseIP(ev.SrcIP) == nil {
			continue
		}
		obs = append(obs, storage.IPBehaviorObservation{
			TenantID:    tenantID,
			NodeID:      nodeID,
			Timestamp:   ev.TS,
			ServerGroup: detailsString(ev.Details, "server_group", ""),
			App:         firstNonEmptyIPBehavior(detailsString(ev.Details, "app", ""), detailsString(ev.Details, "vhost", ""), detailsString(ev.Details, "webserver_kind", "")),
			CountryCode: detailsString(ev.Details, "country_code", ""),
			Country:     detailsString(ev.Details, "country", ""),
			ASN:         detailsString(ev.Details, "asn", ""),
			ISP:         detailsString(ev.Details, "isp", ""),
			SourceIP:    ev.SrcIP,
			StatusCode:  int(detailsInt(ev.Details, "status_code")),
			BytesOut:    ev.BytesOut,
		})
	}
	if len(obs) == 0 {
		return
	}
	if err := store.IncrementIPBehaviorRollups(ctx, obs); err != nil {
		s.logger.Warn("record ip behavior rollups", zap.Error(err), zap.Int("rows", len(obs)))
	}
}

type ipBehaviorBucket struct {
	key                     string
	srcIP                   string
	countryCode             string
	country                 string
	asn                     string
	app                     string
	serverGroup             string
	windowLabel             string
	windowSize              time.Duration
	firstTS                 time.Time
	lastTS                  time.Time
	count                   int
	uniquePaths             int
	uniqueSourceIPs         int
	bytesIn                 int64
	bytesOut                int64
	threatScore             int
	sensitiveHits           int
	processSpawns           int
	fileWrites              int
	dbBulkQueries           int
	outboundConnections     int
	authSuccessAfterFailure int
	trustedPartner          bool
	partnerID               string
	statuses                map[int]int
	paths                   map[string]int
	evidenceRefs            []map[string]any
}

type ipBehaviorCurrentWindowStore interface {
	addAndRollup(context.Context, uuid.UUID, uuid.UUID, []IngestedEvent) []*ipBehaviorBucket
}

type ipBehaviorWindowAccumulator struct {
	mu        sync.Mutex
	retention time.Duration
	buckets   map[string]map[int64]*ipBehaviorBucket
}

var ipBehaviorCurrentWindows = []struct {
	label string
	size  time.Duration
}{
	{label: "1m", size: time.Minute},
	{label: "5m", size: 5 * time.Minute},
	{label: "15m", size: 15 * time.Minute},
	{label: "1h", size: time.Hour},
}

var ipBehaviorReasonSourcePattern = regexp.MustCompile(`\bfrom\s+([^\s]+)\s+scored\b`)

const ipBehaviorWindowResolution = time.Minute

func newIPBehaviorWindowAccumulator(retention time.Duration) *ipBehaviorWindowAccumulator {
	if retention <= 0 {
		retention = time.Hour
	}
	return &ipBehaviorWindowAccumulator{
		retention: retention,
		buckets:   map[string]map[int64]*ipBehaviorBucket{},
	}
}

func (s *Server) ipBehaviorCurrentWindowStore() ipBehaviorCurrentWindowStore {
	if s == nil {
		return nil
	}
	s.ipBehaviorWindowsOnce.Do(func() {
		s.ipBehaviorWindows = s.newIPBehaviorCurrentWindowStore()
	})
	return s.ipBehaviorWindows
}

func (s *Server) newIPBehaviorCurrentWindowStore() ipBehaviorCurrentWindowStore {
	if s != nil && s.cfg != nil && strings.EqualFold(strings.TrimSpace(s.cfg.IPBehavior.Counters.Backend), "redis") {
		addr := strings.TrimSpace(s.cfg.IPBehavior.Counters.RedisAddress)
		if addr != "" {
			return newRedisIPBehaviorWindowStore(redis.NewClient(&redis.Options{
				Addr:     addr,
				DB:       s.cfg.IPBehavior.Counters.RedisDB,
				Password: s.cfg.IPBehavior.Counters.RedisPassword,
			}), time.Hour, s.logger)
		}
	}
	return newIPBehaviorWindowAccumulator(time.Hour)
}

func (a *ipBehaviorWindowAccumulator) addAndRollup(_ context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) []*ipBehaviorBucket {
	if a == nil || len(events) == 0 {
		return nil
	}
	now := latestIPBehaviorEventTime(events)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	touched := map[string]struct{}{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range events {
		ev := &events[i]
		if !isIPBehaviorRequestEvent(ev) {
			continue
		}
		ts := ev.TS.UTC()
		if ts.IsZero() {
			ts = now
		}
		accKey := ipBehaviorAccumulatorKey(tenantID, nodeID, ev)
		if accKey == "" {
			continue
		}
		slot := ts.Truncate(ipBehaviorWindowResolution).Unix()
		bySlot := a.buckets[accKey]
		if bySlot == nil {
			bySlot = map[int64]*ipBehaviorBucket{}
			a.buckets[accKey] = bySlot
		}
		b := bySlot[slot]
		if b == nil {
			b = newIPBehaviorBucketFromEvent(ev)
			bySlot[slot] = b
		}
		addIPBehaviorEventToBucket(b, ev, ts)
		touched[accKey] = struct{}{}
	}
	a.pruneLocked(now)
	return a.rollupsLocked(now, touched)
}

func (a *ipBehaviorWindowAccumulator) pruneLocked(now time.Time) {
	cutoff := now.UTC().Add(-a.retention - ipBehaviorWindowResolution).Unix()
	for key, bySlot := range a.buckets {
		for slot := range bySlot {
			if slot < cutoff {
				delete(bySlot, slot)
			}
		}
		if len(bySlot) == 0 {
			delete(a.buckets, key)
		}
	}
}

func (a *ipBehaviorWindowAccumulator) rollupsLocked(now time.Time, touched map[string]struct{}) []*ipBehaviorBucket {
	if len(touched) == 0 {
		return nil
	}
	out := make([]*ipBehaviorBucket, 0, len(touched)*len(ipBehaviorCurrentWindows))
	for key := range touched {
		bySlot := a.buckets[key]
		if len(bySlot) == 0 {
			continue
		}
		for _, window := range ipBehaviorCurrentWindows {
			from := now.UTC().Add(-window.size).Truncate(ipBehaviorWindowResolution).Unix()
			var rolled *ipBehaviorBucket
			for slot, b := range bySlot {
				if slot < from {
					continue
				}
				if rolled == nil {
					rolled = cloneIPBehaviorBucketShape(b)
				}
				mergeIPBehaviorBucket(rolled, b)
			}
			if rolled == nil || rolled.count == 0 {
				continue
			}
			rolled.windowLabel = window.label
			rolled.windowSize = window.size
			out = append(out, rolled)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].key != out[j].key {
			return out[i].key < out[j].key
		}
		return out[i].windowSize < out[j].windowSize
	})
	return out
}

func latestIPBehaviorEventTime(events []IngestedEvent) time.Time {
	var latest time.Time
	for i := range events {
		ev := &events[i]
		if !isIPBehaviorRequestEvent(ev) || ev.TS.IsZero() {
			continue
		}
		ts := ev.TS.UTC()
		if latest.IsZero() || ts.After(latest) {
			latest = ts
		}
	}
	return latest
}

func isIPBehaviorRequestEvent(ev *IngestedEvent) bool {
	if ev == nil || (ev.Type != "web.request" && ev.Type != "web.error") {
		return false
	}
	return net.ParseIP(ev.SrcIP) != nil
}

func ipBehaviorAccumulatorKey(tenantID, nodeID uuid.UUID, ev *IngestedEvent) string {
	if ev == nil {
		return ""
	}
	b := newIPBehaviorBucketFromEvent(ev)
	if b == nil {
		return ""
	}
	return strings.Join([]string{
		tenantID.String(),
		nodeID.String(),
		b.srcIP,
		b.countryCode,
		b.asn,
		b.app,
		b.serverGroup,
	}, "|")
}

func newIPBehaviorBucketFromEvent(ev *IngestedEvent) *ipBehaviorBucket {
	if ev == nil {
		return nil
	}
	app := firstNonEmptyIPBehavior(detailsString(ev.Details, "app", ""), detailsString(ev.Details, "vhost", ""), detailsString(ev.Details, "webserver_kind", "web"))
	cc := strings.ToUpper(detailsString(ev.Details, "country_code", ""))
	asn := detailsString(ev.Details, "asn", "")
	serverGroup := detailsString(ev.Details, "server_group", "")
	key := strings.Join([]string{ev.SrcIP, cc, asn, app, serverGroup}, "|")
	return &ipBehaviorBucket{
		key:         key,
		srcIP:       ev.SrcIP,
		countryCode: cc,
		country:     detailsString(ev.Details, "country", ""),
		asn:         asn,
		app:         app,
		serverGroup: serverGroup,
		statuses:    map[int]int{},
		paths:       map[string]int{},
	}
}

func addIPBehaviorEventToBucket(b *ipBehaviorBucket, ev *IngestedEvent, ts time.Time) {
	if b == nil || ev == nil {
		return
	}
	if b.firstTS.IsZero() || ts.Before(b.firstTS) {
		b.firstTS = ts
	}
	if ts.After(b.lastTS) {
		b.lastTS = ts
	}
	b.count++
	if b.uniqueSourceIPs == 0 && ev.SrcIP != "" {
		b.uniqueSourceIPs = 1
	}
	b.bytesIn += ev.BytesIn
	b.bytesOut += ev.BytesOut
	if ev.ThreatScore > b.threatScore {
		b.threatScore = ev.ThreatScore
	}
	status := int(detailsInt(ev.Details, "status_code"))
	if status > 0 {
		b.statuses[status]++
		if status >= 200 && status < 300 && (b.statuses[401]+b.statuses[403]) > 0 {
			b.authSuccessAfterFailure++
		}
	}
	path := detailsString(ev.Details, "path_template", "")
	if path == "" {
		path = detailsString(ev.Details, "path", "")
	}
	if path != "" {
		b.paths[path]++
		b.uniquePaths = len(b.paths)
		if isSuspiciousIPBehaviorPath(path) {
			b.sensitiveHits++
		}
	}
	b.processSpawns += ipBehaviorSignalCount(ev.Details, []string{"process_spawned", "process_spawn", "new_process"}, []string{"process_spawn_count", "process_spawn_events"})
	b.fileWrites += ipBehaviorSignalCount(ev.Details, []string{"file_write", "web_root_file_write", "webroot_file_write"}, []string{"file_write_count", "web_root_file_write_count", "webroot_file_write_count"})
	b.dbBulkQueries += ipBehaviorSignalCount(ev.Details, []string{"db_bulk_query", "db_export_query"}, []string{"db_bulk_query_count", "db_export_query_count"})
	b.outboundConnections += ipBehaviorSignalCount(ev.Details, []string{"new_outbound_connection", "outbound_callback"}, []string{"outbound_connection_count", "callback_connection_count"})
	b.authSuccessAfterFailure += ipBehaviorSignalCount(ev.Details, []string{"auth_failure_then_success", "new_success_after_failure"}, []string{"auth_success_after_failure_count"})
	if uniqueSources := ipBehaviorMaxDetailInt(ev.Details, "unique_source_ips", "source_ip_count", "distributed_source_count"); uniqueSources > b.uniqueSourceIPs {
		b.uniqueSourceIPs = uniqueSources
	}
	if ipBehaviorDetailBool(ev.Details, "trusted_partner") {
		b.trustedPartner = true
	}
	if partnerID := firstNonEmptyIPBehavior(detailsString(ev.Details, "partner_id", ""), detailsString(ev.Details, "partner_name", "")); partnerID != "" && b.partnerID == "" {
		b.partnerID = partnerID
	}
	appendIPBehaviorEvidenceRef(b, ev, path, status)
}

func cloneIPBehaviorBucketShape(b *ipBehaviorBucket) *ipBehaviorBucket {
	if b == nil {
		return nil
	}
	return &ipBehaviorBucket{
		key:         b.key,
		srcIP:       b.srcIP,
		countryCode: b.countryCode,
		country:     b.country,
		asn:         b.asn,
		app:         b.app,
		serverGroup: b.serverGroup,
		partnerID:   b.partnerID,
		statuses:    map[int]int{},
		paths:       map[string]int{},
	}
}

func mergeIPBehaviorBucket(dst, src *ipBehaviorBucket) {
	if dst == nil || src == nil {
		return
	}
	if dst.firstTS.IsZero() || (!src.firstTS.IsZero() && src.firstTS.Before(dst.firstTS)) {
		dst.firstTS = src.firstTS
	}
	if src.lastTS.After(dst.lastTS) {
		dst.lastTS = src.lastTS
	}
	dst.count += src.count
	dst.bytesIn += src.bytesIn
	dst.bytesOut += src.bytesOut
	if src.uniquePaths > dst.uniquePaths {
		dst.uniquePaths = src.uniquePaths
	}
	if src.uniqueSourceIPs > dst.uniqueSourceIPs {
		dst.uniqueSourceIPs = src.uniqueSourceIPs
	}
	if src.threatScore > dst.threatScore {
		dst.threatScore = src.threatScore
	}
	dst.sensitiveHits += src.sensitiveHits
	dst.processSpawns += src.processSpawns
	dst.fileWrites += src.fileWrites
	dst.dbBulkQueries += src.dbBulkQueries
	dst.outboundConnections += src.outboundConnections
	dst.authSuccessAfterFailure += src.authSuccessAfterFailure
	if src.trustedPartner {
		dst.trustedPartner = true
	}
	if dst.partnerID == "" {
		dst.partnerID = src.partnerID
	}
	for status, count := range src.statuses {
		dst.statuses[status] += count
	}
	for path, count := range src.paths {
		dst.paths[path] += count
	}
	dst.evidenceRefs = appendIPBehaviorEvidenceRefs(dst.evidenceRefs, src.evidenceRefs, 16)
}

type redisIPBehaviorWindowStore struct {
	client    *redis.Client
	retention time.Duration
	fallback  *ipBehaviorWindowAccumulator
	log       *zap.Logger
}

type redisIPBehaviorSourceRef struct {
	tenantID uuid.UUID
	nodeID   uuid.UUID
	bucket   *ipBehaviorBucket
}

func newRedisIPBehaviorWindowStore(client *redis.Client, retention time.Duration, log *zap.Logger) *redisIPBehaviorWindowStore {
	if retention <= 0 {
		retention = time.Hour
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &redisIPBehaviorWindowStore{
		client:    client,
		retention: retention,
		fallback:  newIPBehaviorWindowAccumulator(retention),
		log:       log.Named("ip-behavior-counters"),
	}
}

func (r *redisIPBehaviorWindowStore) addAndRollup(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) []*ipBehaviorBucket {
	buckets, err := r.addAndRollupRedis(ctx, tenantID, nodeID, events)
	if err == nil {
		return buckets
	}
	if r.log != nil {
		r.log.Warn("redis ip behavior counters unavailable; using in-process fallback", zap.Error(err))
	}
	return r.fallback.addAndRollup(ctx, tenantID, nodeID, events)
}

func (r *redisIPBehaviorWindowStore) addAndRollupRedis(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) ([]*ipBehaviorBucket, error) {
	if r == nil || r.client == nil || len(events) == 0 {
		return nil, nil
	}
	now := latestIPBehaviorEventTime(events)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := r.retention + 2*ipBehaviorWindowResolution
	touched := map[string]redisIPBehaviorSourceRef{}
	pipe := r.client.Pipeline()
	for i := range events {
		ev := &events[i]
		if !isIPBehaviorRequestEvent(ev) {
			continue
		}
		ts := ev.TS.UTC()
		if ts.IsZero() {
			ts = now
		}
		b := newIPBehaviorBucketFromEvent(ev)
		if b == nil {
			continue
		}
		slot := ts.Truncate(ipBehaviorWindowResolution).Unix()
		for _, scope := range redisIPBehaviorCounterScopes(b) {
			prefix, ok := redisIPBehaviorCounterPrefix(scope, tenantID, nodeID, b)
			if !ok {
				continue
			}
			key := redisIPBehaviorSlotKey(prefix, slot)
			redisIncrementIPBehaviorCounter(ctx, pipe, key, scope, b, ev, ts, ttl)
			if scope != "source_ip" && ev.SrcIP != "" {
				uniqueKey := redisIPBehaviorUniqueIPKey(prefix, slot)
				pipe.PFAdd(ctx, uniqueKey, ev.SrcIP)
				pipe.Expire(ctx, uniqueKey, ttl)
			}
			if scope == "source_ip" {
				path := detailsString(ev.Details, "path_template", "")
				if path == "" {
					path = detailsString(ev.Details, "path", "")
				}
				if strings.TrimSpace(path) != "" {
					pathKey := redisIPBehaviorPathKey(prefix, slot)
					pipe.PFAdd(ctx, pathKey, hashString(path))
					pipe.Expire(ctx, pathKey, ttl)
				}
				touched[prefix] = redisIPBehaviorSourceRef{tenantID: tenantID, nodeID: nodeID, bucket: b}
			}
		}
	}
	if len(touched) == 0 {
		return nil, nil
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]*ipBehaviorBucket, 0, len(touched)*len(ipBehaviorCurrentWindows))
	for prefix, ref := range touched {
		for _, window := range ipBehaviorCurrentWindows {
			rolled, err := r.readRedisSourceWindow(ctx, prefix, ref.bucket, now, window.label, window.size)
			if err != nil {
				return nil, err
			}
			if rolled != nil && rolled.count > 0 {
				out = append(out, rolled)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].key != out[j].key {
			return out[i].key < out[j].key
		}
		return out[i].windowSize < out[j].windowSize
	})
	return out, nil
}

func redisIncrementIPBehaviorCounter(ctx context.Context, pipe redis.Pipeliner, key, scope string, b *ipBehaviorBucket, ev *IngestedEvent, ts time.Time, ttl time.Duration) {
	pipe.HSet(ctx, key, map[string]any{
		"scope":        scope,
		"src_ip":       b.srcIP,
		"country_code": b.countryCode,
		"country":      b.country,
		"asn":          b.asn,
		"app":          b.app,
		"server_group": b.serverGroup,
		"last_ts":      ts.UnixNano(),
	})
	pipe.HIncrBy(ctx, key, "request_count", 1)
	if ev.BytesIn != 0 {
		pipe.HIncrBy(ctx, key, "bytes_in", ev.BytesIn)
	}
	if ev.BytesOut != 0 {
		pipe.HIncrBy(ctx, key, "bytes_out", ev.BytesOut)
	}
	if ev.ThreatScore > 0 {
		pipe.HSet(ctx, key, "threat_score", ev.ThreatScore)
	}
	status := int(detailsInt(ev.Details, "status_code"))
	if status > 0 {
		pipe.HIncrBy(ctx, key, fmt.Sprintf("status_%d", status), 1)
		if status >= 100 && status <= 599 {
			pipe.HIncrBy(ctx, key, fmt.Sprintf("status_%dxx", status/100), 1)
		}
	}
	path := detailsString(ev.Details, "path_template", "")
	if path == "" {
		path = detailsString(ev.Details, "path", "")
	}
	if isSuspiciousIPBehaviorPath(path) {
		pipe.HIncrBy(ctx, key, "sensitive_path_hits", 1)
	}
	if n := ipBehaviorSignalCount(ev.Details, []string{"process_spawned", "process_spawn", "new_process"}, []string{"process_spawn_count", "process_spawn_events"}); n > 0 {
		pipe.HIncrBy(ctx, key, "process_spawns", int64(n))
	}
	if n := ipBehaviorSignalCount(ev.Details, []string{"file_write", "web_root_file_write", "webroot_file_write"}, []string{"file_write_count", "web_root_file_write_count", "webroot_file_write_count"}); n > 0 {
		pipe.HIncrBy(ctx, key, "file_writes", int64(n))
	}
	if n := ipBehaviorSignalCount(ev.Details, []string{"db_bulk_query", "db_export_query"}, []string{"db_bulk_query_count", "db_export_query_count"}); n > 0 {
		pipe.HIncrBy(ctx, key, "db_bulk_queries", int64(n))
	}
	if n := ipBehaviorSignalCount(ev.Details, []string{"new_outbound_connection", "outbound_callback"}, []string{"outbound_connection_count", "callback_connection_count"}); n > 0 {
		pipe.HIncrBy(ctx, key, "outbound_connections", int64(n))
	}
	if n := ipBehaviorSignalCount(ev.Details, []string{"auth_failure_then_success", "new_success_after_failure"}, []string{"auth_success_after_failure_count"}); n > 0 {
		pipe.HIncrBy(ctx, key, "auth_success_after_failure", int64(n))
	}
	if n := ipBehaviorMaxDetailInt(ev.Details, "unique_source_ips", "source_ip_count", "distributed_source_count"); n > 0 {
		pipe.HSet(ctx, key, "unique_source_ips", n)
	}
	if ipBehaviorDetailBool(ev.Details, "trusted_partner") {
		pipe.HSet(ctx, key, "trusted_partner", "true")
	}
	if partnerID := firstNonEmptyIPBehavior(detailsString(ev.Details, "partner_id", ""), detailsString(ev.Details, "partner_name", "")); partnerID != "" {
		pipe.HSet(ctx, key, "partner_id", partnerID)
	}
	pipe.Expire(ctx, key, ttl)
}

func (r *redisIPBehaviorWindowStore) readRedisSourceWindow(ctx context.Context, prefix string, shape *ipBehaviorBucket, now time.Time, label string, window time.Duration) (*ipBehaviorBucket, error) {
	if shape == nil {
		return nil, nil
	}
	slots := redisIPBehaviorSlots(now, window)
	if len(slots) == 0 {
		return nil, nil
	}
	pipe := r.client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, 0, len(slots))
	pathKeys := make([]string, 0, len(slots))
	for _, slot := range slots {
		cmds = append(cmds, pipe.HGetAll(ctx, redisIPBehaviorSlotKey(prefix, slot)))
		pathKeys = append(pathKeys, redisIPBehaviorPathKey(prefix, slot))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	rolled := cloneIPBehaviorBucketShape(shape)
	for _, cmd := range cmds {
		fields, err := cmd.Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		if len(fields) == 0 {
			continue
		}
		mergeRedisIPBehaviorFields(rolled, fields)
	}
	if rolled.count == 0 {
		return nil, nil
	}
	if n, err := r.client.PFCount(ctx, pathKeys...).Result(); err == nil && n > int64(rolled.uniquePaths) {
		rolled.uniquePaths = int(n)
	}
	rolled.windowLabel = label
	rolled.windowSize = window
	return rolled, nil
}

func mergeRedisIPBehaviorFields(b *ipBehaviorBucket, fields map[string]string) {
	if b == nil || len(fields) == 0 {
		return
	}
	if b.country == "" {
		b.country = fields["country"]
	}
	if b.serverGroup == "" {
		b.serverGroup = fields["server_group"]
	}
	b.count += int(parseRedisInt(fields["request_count"]))
	b.bytesIn += parseRedisInt(fields["bytes_in"])
	b.bytesOut += parseRedisInt(fields["bytes_out"])
	if ts := parseRedisInt(fields["last_ts"]); ts > 0 {
		t := time.Unix(0, ts).UTC()
		if b.firstTS.IsZero() || t.Before(b.firstTS) {
			b.firstTS = t
		}
		if t.After(b.lastTS) {
			b.lastTS = t
		}
	}
	if threat := int(parseRedisInt(fields["threat_score"])); threat > b.threatScore {
		b.threatScore = threat
	}
	b.sensitiveHits += int(parseRedisInt(fields["sensitive_path_hits"]))
	b.processSpawns += int(parseRedisInt(fields["process_spawns"]))
	b.fileWrites += int(parseRedisInt(fields["file_writes"]))
	b.dbBulkQueries += int(parseRedisInt(fields["db_bulk_queries"]))
	b.outboundConnections += int(parseRedisInt(fields["outbound_connections"]))
	b.authSuccessAfterFailure += int(parseRedisInt(fields["auth_success_after_failure"]))
	if uniqueSources := int(parseRedisInt(fields["unique_source_ips"])); uniqueSources > b.uniqueSourceIPs {
		b.uniqueSourceIPs = uniqueSources
	}
	if strings.EqualFold(fields["trusted_partner"], "true") {
		b.trustedPartner = true
	}
	if b.partnerID == "" {
		b.partnerID = fields["partner_id"]
	}
	if b.statuses == nil {
		b.statuses = map[int]int{}
	}
	for k, v := range fields {
		if !strings.HasPrefix(k, "status_") || strings.HasSuffix(k, "xx") {
			continue
		}
		status, err := strconv.Atoi(strings.TrimPrefix(k, "status_"))
		if err != nil {
			continue
		}
		b.statuses[status] += int(parseRedisInt(v))
	}
}

func parseRedisInt(value string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n
}

func redisIPBehaviorCounterScopes(b *ipBehaviorBucket) []string {
	scopes := []string{"source_ip"}
	if b.countryCode != "" && b.app != "" {
		scopes = append(scopes, "country_app")
		if b.serverGroup != "" {
			scopes = append(scopes, "server_group_country_app")
		}
		scopes = append(scopes, "node_country_app")
	}
	if b.asn != "" && b.app != "" {
		scopes = append(scopes, "asn_app")
	}
	return scopes
}

func redisIPBehaviorCounterPrefix(scope string, tenantID, nodeID uuid.UUID, b *ipBehaviorBucket) (string, bool) {
	if b == nil {
		return "", false
	}
	parts := []string{tenantID.String(), scope}
	switch scope {
	case "source_ip":
		parts = append(parts, nodeID.String(), b.serverGroup, b.app, b.countryCode, b.asn, b.srcIP)
	case "country_app":
		parts = append(parts, b.serverGroup, b.app, b.countryCode)
	case "server_group_country_app":
		parts = append(parts, b.serverGroup, b.app, b.countryCode)
	case "node_country_app":
		parts = append(parts, nodeID.String(), b.app, b.countryCode)
	case "asn_app":
		parts = append(parts, b.app, b.asn)
	default:
		return "", false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", false
		}
	}
	return "c1:ipb:v1:" + scope + ":" + hashString(strings.Join(parts, "|"))[:32], true
}

func redisIPBehaviorSlotKey(prefix string, slot int64) string {
	return fmt.Sprintf("%s:slot:%d", prefix, slot)
}

func redisIPBehaviorPathKey(prefix string, slot int64) string {
	return fmt.Sprintf("%s:paths:%d", prefix, slot)
}

func redisIPBehaviorUniqueIPKey(prefix string, slot int64) string {
	return fmt.Sprintf("%s:uniq_ips:%d", prefix, slot)
}

func redisIPBehaviorSlots(now time.Time, window time.Duration) []int64 {
	nowSlot := now.UTC().Truncate(ipBehaviorWindowResolution)
	from := now.UTC().Add(-window).Truncate(ipBehaviorWindowResolution)
	out := make([]int64, 0, int(window/ipBehaviorWindowResolution)+2)
	for t := from; !t.After(nowSlot); t = t.Add(ipBehaviorWindowResolution) {
		out = append(out, t.Unix())
	}
	return out
}

func (r *redisIPBehaviorWindowStore) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (s *Server) detectIPBehaviorBatch(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) []IngestedEvent {
	if s == nil {
		return nil
	}
	store := s.ipBehaviorCurrentWindowStore()
	if store == nil {
		return nil
	}
	buckets := store.addAndRollup(ctx, tenantID, nodeID, events)
	type scoredBucket struct {
		b             *ipBehaviorBucket
		score         int
		category      string
		reasons       []string
		corroborating int
		baselines     []storage.BehavioralBaseline
	}
	best := map[string]scoredBucket{}
	out := make([]IngestedEvent, 0)
	for _, b := range buckets {
		baselines := s.lookupIPBehaviorBaselines(ctx, tenantID, nodeID, b)
		score, category, reasons, corroborating := scoreIPBehaviorBucketWithBaselines(b, baselines)
		if score < 50 || corroborating == 0 {
			continue
		}
		key := strings.Join([]string{b.srcIP, b.countryCode, b.asn, b.app, b.serverGroup, category}, "|")
		if prev, ok := best[key]; ok && prev.score >= score {
			continue
		}
		best[key] = scoredBucket{b: b, score: score, category: category, reasons: reasons, corroborating: corroborating, baselines: baselines}
	}
	keys := make([]string, 0, len(best))
	for key := range best {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		scored := best[key]
		b := scored.b
		score := scored.score
		category := scored.category
		reasons := scored.reasons
		corroborating := scored.corroborating
		sev := severityFromIPBehaviorScore(score)
		if b.lastTS.IsZero() {
			b.lastTS = time.Now().UTC()
		}
		details := map[string]any{
			"score":                score,
			"category":             category,
			"reasons":              reasons,
			"request_count":        b.count,
			"unique_source_ips":    bucketDistributedSourceCount(b),
			"bytes_in":             b.bytesIn,
			"bytes_out":            b.bytesOut,
			"status_counts":        statusMapString(b.statuses),
			"country_code":         b.countryCode,
			"country":              b.country,
			"asn":                  b.asn,
			"app":                  b.app,
			"server_group":         b.serverGroup,
			"corroborating":        corroborating,
			"window":               firstNonEmptyIPBehavior(b.windowLabel, "current"),
			"window_seconds":       int(b.windowSize.Seconds()),
			"window_started":       b.firstTS.UTC().Format(time.RFC3339),
			"window_ended":         b.lastTS.UTC().Format(time.RFC3339),
			"dedup_window_seconds": 300,
			"dedup_scope":          []string{"tenant", "src_ip", "country", "asn", "app", "server_group", "category", "5m_window"},
		}
		if topPaths := ipBehaviorTopPaths(b.paths, 10); len(topPaths) > 0 {
			details["top_paths"] = topPaths
		}
		if hostCorrelation := ipBehaviorHostCorrelationEvidence(b); len(hostCorrelation) > 0 {
			details["host_correlation"] = hostCorrelation
		}
		if len(b.evidenceRefs) > 0 {
			details["evidence_refs"] = b.evidenceRefs
		}
		if b.trustedPartner {
			details["trusted_partner"] = true
			if b.partnerID != "" {
				details["partner_id"] = b.partnerID
			}
		}
		if len(scored.baselines) > 0 {
			details["baselines"] = ipBehaviorBaselineEvidence(scored.baselines)
		}
		dedup := fmt.Sprintf("anomaly.ip_behavior:%s:%s:%s:%s:%s:%d", tenantID, b.srcIP, b.countryCode, category, firstNonEmptyIPBehavior(b.windowLabel, "current"), b.lastTS.Unix()/300)
		msg := fmt.Sprintf("%s behavior from %s scored %d: %s", category, b.srcIP, score, strings.Join(reasons, "; "))
		out = append(out, IngestedEvent{
			Type:          "anomaly.ip_behavior",
			TS:            b.lastTS,
			NodeID:        nodeID.String(),
			TenantID:      tenantID.String(),
			Severity:      sev,
			CorrelationID: dedup,
			SrcIP:         b.srcIP,
			BytesOut:      b.bytesOut,
			Message:       msg,
			Details:       details,
			DedupKey:      dedup,
		})
		s.recordIPBehaviorFinding(ctx, tenantID, nodeID, dedup, b, score, sev, category, msg, details)
		if score >= 100 {
			s.openIPBehaviorConfidenceAlert(ctx, tenantID, nodeID, dedup, b, score, sev, category, msg, details)
		}
	}
	return out
}

func scoreIPBehaviorBucket(b *ipBehaviorBucket) (int, string, []string, int) {
	return scoreIPBehaviorBucketWithBaselines(b, nil)
}

func scoreIPBehaviorBucketWithBaselines(b *ipBehaviorBucket, baselines []storage.BehavioralBaseline) (int, string, []string, int) {
	score := 0
	corroborating := 0
	reasons := []string{}
	if b.countryCode != "" {
		score += 20
		reasons = append(reasons, "country/country-app behavior is being evaluated as a baseline dimension")
	}
	if b.asn != "" {
		score += 10
		reasons = append(reasons, "ASN/app behavior is being evaluated as a baseline dimension")
	}
	hour := b.lastTS.Hour()
	weekday := b.lastTS.Weekday()
	windowLabel := ipBehaviorBucketWindowLabel(b)
	if hour < 5 || hour >= 22 || weekday == time.Saturday || weekday == time.Sunday {
		score += 15
		reasons = append(reasons, "traffic occurred outside typical business hours")
	}
	if b.count >= 10 {
		score += 15
		corroborating++
		reasons = append(reasons, fmt.Sprintf("request burst: %d requests in %s", b.count, windowLabel))
	}
	authFailures := b.statuses[401] + b.statuses[403]
	if authFailures >= 5 || (b.count >= 5 && float64(authFailures)/float64(b.count) >= 0.40) {
		score += 20
		corroborating++
		reasons = append(reasons, fmt.Sprintf("auth failure spike: %d 401/403 responses", authFailures))
	}
	serverErrors := b.statuses[500] + b.statuses[502] + b.statuses[503]
	if serverErrors >= 3 {
		score += 15
		corroborating++
		reasons = append(reasons, fmt.Sprintf("server error spike: %d 500/502/503 responses", serverErrors))
	}
	if b.bytesOut >= 50*1024*1024 {
		score += 25
		corroborating++
		reasons = append(reasons, fmt.Sprintf("large outbound transfer: %d MiB", b.bytesOut/(1024*1024)))
	}
	if b.threatScore > 0 {
		if b.threatScore >= 90 && b.threatScore > score {
			score = b.threatScore
		} else {
			score += 25
		}
		corroborating++
		reasons = append(reasons, fmt.Sprintf("known threat intelligence confidence %d", b.threatScore))
	}
	if bucketSuspiciousPathHits(b) >= 3 {
		score += 10
		corroborating++
		reasons = append(reasons, "sensitive/admin path probing")
	}
	probeResponses := b.statuses[301] + b.statuses[403] + b.statuses[404] + b.statuses[429]
	if probeResponses >= 10 && bucketUniquePathCount(b) >= 5 {
		score += 10
		corroborating++
		reasons = append(reasons, "scanner-like path/status diversity")
	}
	if b.authSuccessAfterFailure > 0 {
		score += 10
		corroborating++
		reasons = append(reasons, "successful request followed prior auth failures in the same behavior window")
	}
	if distributed := bucketDistributedSourceCount(b); distributed >= 20 && b.count >= distributed {
		score += 20
		corroborating++
		reasons = append(reasons, fmt.Sprintf("slow distributed pattern: %d source IPs share the same country/ASN/app behavior", distributed))
	}
	if hostSignals := ipBehaviorHostCorrelationSignalCount(b); hostSignals > 0 {
		bonus := 10 + (hostSignals-1)*5
		if bonus > 25 {
			bonus = 25
		}
		score += bonus
		corroborating++
		reasons = append(reasons, fmt.Sprintf("host/app correlation after web traffic: %s", strings.Join(ipBehaviorHostCorrelationLabels(b), ", ")))
	}
	baselineScore, baselineReasons, baselineCorroborating, baselineCategory := scoreIPBehaviorBaselines(b, baselines)
	score += baselineScore
	corroborating += baselineCorroborating
	reasons = append(reasons, baselineReasons...)
	category := "ip_behavior"
	switch {
	case ipBehaviorWebshellCorrelation(b):
		category = "webshell_callback"
	case b.threatScore >= 90:
		category = "known_malicious_source"
	case b.trustedPartner && (baselineCategory != "" || baselineScore > 0):
		category = "partner_drift"
	case authFailures >= 5 || b.authSuccessAfterFailure > 0:
		category = "credential_attack"
	case b.bytesOut >= 50*1024*1024:
		category = "exfiltration_risk"
	case serverErrors >= 3:
		category = "exploit_attempt"
	case probeResponses >= 10 && bucketUniquePathCount(b) >= 5:
		category = "scanner_probe"
	case bucketDistributedSourceCount(b) >= 20:
		category = "slow_distributed_attack"
	}
	if baselineCategory != "" && category == "ip_behavior" {
		category = baselineCategory
	}
	if score > 100 {
		score = 100
	}
	return score, category, reasons, corroborating
}

func (s *Server) lookupIPBehaviorBaselines(ctx context.Context, tenantID, nodeID uuid.UUID, b *ipBehaviorBucket) []storage.BehavioralBaseline {
	store, ok := s.store.(ipBehaviorBaselineLookupStore)
	if !ok || store == nil || b == nil {
		return nil
	}
	type candidate struct {
		nodeID     *uuid.UUID
		signalType string
		dimension  string
	}
	app := cleanIPBehaviorDim(firstNonEmptyIPBehavior(b.app, "web"))
	candidates := []candidate{}
	if b.countryCode != "" {
		if nodeID != uuid.Nil {
			nid := nodeID
			candidates = append(candidates, candidate{
				nodeID:     &nid,
				signalType: "ip_behavior.node_country_app",
				dimension:  ipBehaviorDimensionKey(app, b.countryCode),
			})
		}
		candidates = append(candidates, candidate{
			signalType: "ip_behavior.country_app",
			dimension:  ipBehaviorDimensionKey(cleanIPBehaviorDim(b.serverGroup), app, b.countryCode),
		})
	}
	if b.asn != "" {
		candidates = append(candidates, candidate{
			signalType: "ip_behavior.asn_app",
			dimension:  ipBehaviorDimensionKey(app, b.asn),
		})
	}
	if b.srcIP != "" {
		candidates = append(candidates, candidate{
			signalType: "ip_behavior.source_ip_app",
			dimension:  ipBehaviorDimensionKey(app, b.srcIP),
		})
	}
	out := make([]storage.BehavioralBaseline, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if c.dimension == "" {
			continue
		}
		key := c.signalType + "|" + c.dimension
		if c.nodeID != nil {
			key += "|" + c.nodeID.String()
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		base, err := store.GetBehavioralBaseline(ctx, tenantID, c.nodeID, c.signalType, c.dimension)
		if err != nil {
			s.logger.Debug("ip behavior baseline lookup", zap.Error(err), zap.String("signal_type", c.signalType), zap.String("dimension", c.dimension))
			continue
		}
		if base != nil {
			out = append(out, *base)
		}
	}
	return out
}

func scoreIPBehaviorBaselines(b *ipBehaviorBucket, baselines []storage.BehavioralBaseline) (int, []string, int, string) {
	if b == nil || len(baselines) == 0 {
		return 0, nil, 0, ""
	}
	score := 0
	corroborating := 0
	reasons := []string{}
	category := ""
	authFailures := b.statuses[401] + b.statuses[403]
	serverErrors := b.statuses[500] + b.statuses[502] + b.statuses[503]
	authRatio := ratioInt(authFailures, b.count)
	serverErrRatio := ratioInt(serverErrors, b.count)
	for _, base := range baselines {
		if baselineSampleCount(base.Baseline) < 5 {
			continue
		}
		label := strings.TrimPrefix(base.SignalType, "ip_behavior.")
		requestP99 := nestedFloat(base.Baseline, "request_count", "p99")
		requestPeak := nestedFloat(base.Baseline, "request_count", "peak")
		if requestP99 > 0 && float64(b.count) > requestP99*1.5 && float64(b.count) >= requestP99+5 {
			score += 15
			corroborating++
			reasons = append(reasons, fmt.Sprintf("%s request rate %.0f exceeded learned p99 %.0f", label, float64(b.count), requestP99))
		} else if requestPeak > 0 && float64(b.count) > requestPeak*1.2 && float64(b.count) >= requestPeak+5 {
			score += 15
			corroborating++
			reasons = append(reasons, fmt.Sprintf("%s request rate %.0f exceeded learned peak %.0f", label, float64(b.count), requestPeak))
		}
		bytesP99 := nestedFloat(base.Baseline, "bytes_out", "p99")
		bytesPeak := nestedFloat(base.Baseline, "bytes_out", "peak")
		if bytesP99 > 0 && float64(b.bytesOut) > bytesP99*1.5 && b.bytesOut >= int64(bytesP99)+1024*1024 {
			score += 25
			corroborating++
			category = "exfiltration_risk"
			reasons = append(reasons, fmt.Sprintf("%s bytes out %.0f exceeded learned p99 %.0f", label, float64(b.bytesOut), bytesP99))
		} else if bytesPeak > 0 && float64(b.bytesOut) > bytesPeak*1.2 && b.bytesOut >= int64(bytesPeak)+1024*1024 {
			score += 25
			corroborating++
			category = "exfiltration_risk"
			reasons = append(reasons, fmt.Sprintf("%s bytes out %.0f exceeded learned peak %.0f", label, float64(b.bytesOut), bytesPeak))
		}
		baseAuthRatio := floatFromAny(base.Baseline["auth_fail_ratio"])
		if authFailures >= 3 && authRatio > 0.20 && (baseAuthRatio == 0 || authRatio > baseAuthRatio*3) {
			score += 20
			corroborating++
			category = "credential_attack"
			reasons = append(reasons, fmt.Sprintf("%s auth failure ratio %.0f%% exceeded learned %.0f%%", label, authRatio*100, baseAuthRatio*100))
		}
		baseErrRatio := floatFromAny(base.Baseline["server_err_ratio"])
		if serverErrors >= 2 && serverErrRatio > 0.10 && (baseErrRatio == 0 || serverErrRatio > baseErrRatio*3) {
			score += 15
			corroborating++
			if category == "" {
				category = "exploit_attempt"
			}
			reasons = append(reasons, fmt.Sprintf("%s server-error ratio %.0f%% exceeded learned %.0f%%", label, serverErrRatio*100, baseErrRatio*100))
		}
		if !int64SliceContains(anyInt64Slice(base.Baseline["active_hours"]), int64(b.lastTS.Hour())) {
			score += 10
			reasons = append(reasons, fmt.Sprintf("%s traffic arrived in a previously inactive hour", label))
		}
		if !int64SliceContains(anyInt64Slice(base.Baseline["active_weekdays"]), int64(b.lastTS.Weekday())) {
			score += 5
			reasons = append(reasons, fmt.Sprintf("%s traffic arrived on a previously inactive weekday", label))
		}
	}
	if score > 50 {
		score = 50
	}
	return score, reasons, corroborating, category
}

func severityFromIPBehaviorScore(score int) string {
	switch {
	case score >= 85:
		return "critical"
	case score >= 70:
		return "high"
	case score >= 50:
		return "medium"
	default:
		return "low"
	}
}

func ipBehaviorBucketWindowLabel(b *ipBehaviorBucket) string {
	if b == nil || strings.TrimSpace(b.windowLabel) == "" {
		return "the current window"
	}
	return b.windowLabel
}

func (s *Server) recordIPBehaviorFinding(ctx context.Context, tenantID, nodeID uuid.UUID, dedup string, b *ipBehaviorBucket, score int, severity, category, reason string, evidence map[string]any) {
	store, ok := s.store.(ipBehaviorFindingStore)
	if !ok || store == nil || b == nil {
		return
	}
	_, err := store.UpsertIPBehaviorFinding(ctx, storage.UpsertIPBehaviorFindingParams{
		TenantID:    tenantID,
		NodeID:      &nodeID,
		DedupKey:    dedup,
		SourceIP:    b.srcIP,
		CountryCode: b.countryCode,
		ASN:         b.asn,
		Category:    category,
		Severity:    severity,
		Score:       score,
		Reason:      reason,
		Evidence:    evidence,
		SeenAt:      b.lastTS,
	})
	if err != nil {
		s.logger.Warn("record ip behavior finding", zap.Error(err))
	}
}

func (s *Server) openIPBehaviorConfidenceAlert(ctx context.Context, tenantID, nodeID uuid.UUID, findingDedup string, b *ipBehaviorBucket, score int, severity, category, reason string, evidence map[string]any) {
	if s == nil || s.store == nil || b == nil || score < 100 {
		return
	}
	if severity == "" {
		severity = "critical"
	}
	alertDedup := fmt.Sprintf("anomaly.ip_behavior.auto:%s:%s:%s", nodeID.String(), b.srcIP, category)
	var nodeArg *uuid.UUID
	if nodeID != uuid.Nil {
		nodeArg = &nodeID
	}
	contextPayload := map[string]any{
		"event_type":           "anomaly.ip_behavior",
		"finding_dedup_key":    findingDedup,
		"score":                score,
		"confidence":           score,
		"category":             category,
		"src_ip":               b.srcIP,
		"country_code":         b.countryCode,
		"country":              b.country,
		"asn":                  b.asn,
		"app":                  b.app,
		"server_group":         b.serverGroup,
		"auto_alert_threshold": 100,
	}
	for key, value := range evidence {
		if _, exists := contextPayload[key]; !exists {
			contextPayload[key] = value
		}
	}
	title := fmt.Sprintf("100%% confidence %s from %s", ipBehaviorAlertCategoryLabel(category), firstNonEmptyIPBehavior(b.srcIP, "unknown source"))
	summary := ipBehaviorAlertSummary(category, b, score, reason, evidence)
	alert, err := s.store.CreateAlert(ctx, storage.CreateAlertParams{
		TenantID: tenantID,
		NodeID:   nodeArg,
		Source:   "ip_behavior",
		Severity: severity,
		Title:    title,
		Summary:  summary,
		DedupKey: alertDedup,
		Context:  contextPayload,
	})
	if err != nil {
		if errors.Is(err, storage.ErrAlertDeduped) {
			s.ensureAutoBlockForConfidenceAlert(ctx, tenantID, nodeID, b, score, category, summary)
			return
		}
		if s.logger != nil {
			s.logger.Warn("open ip behavior confidence alert", zap.Error(err), zap.String("src_ip", b.srcIP), zap.Int("score", score))
		}
		return
	}
	if alert == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"alert_id":   alert.ID.String(),
		"severity":   alert.Severity,
		"source":     alert.Source,
		"title":      alert.Title,
		"confidence": score,
		"src_ip":     b.srcIP,
		"category":   category,
	})
	s.publishEvent(eventbus.Event{
		Topic:    eventbus.TopicAlertOpened,
		TenantID: tenantID,
		NodeID:   nodeArg,
		Payload:  payload,
	})
	s.ensureAutoBlockForConfidenceAlert(ctx, tenantID, nodeID, b, score, category, summary)
}

func (s *Server) ensureAutoBlockForConfidenceAlert(ctx context.Context, tenantID, nodeID uuid.UUID, b *ipBehaviorBucket, score int, category, summary string) {
	if s == nil || s.store == nil || tenantID == uuid.Nil || nodeID == uuid.Nil || b == nil || score < 100 {
		return
	}
	if strings.TrimSpace(b.srcIP) == "" || net.ParseIP(b.srcIP) == nil {
		return
	}
	if !strings.EqualFold(category, "known_malicious_source") && b.threatScore < 100 {
		return
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		return
	}
	cidr := exactCIDRForIP(b.srcIP)
	if cidr == "" {
		return
	}
	if protected := s.protectedIPBlockReason(ctx, tenantID, cidr); protected != "" {
		if s.logger != nil {
			s.logger.Warn("skip automatic threat-intel block for protected target", zap.String("ip_cidr", cidr), zap.String("reason", protected))
		}
		return
	}
	if status, msg := s.blockProposalSafetyViolation(ctx, tenantID, b.serverGroup); status != 0 {
		if s.logger != nil {
			s.logger.Warn("skip automatic threat-intel block: safety gate", zap.Int("status", status), zap.String("reason", msg), zap.String("ip_cidr", cidr))
		}
		return
	}
	if query, ok := s.store.(ipBlockProposalQueryStore); ok {
		rows, _, err := query.ListIPBlocklistEntries(ctx, storage.IPBlocklistEntryFilter{
			TenantID:   tenantID,
			IPCIDR:     cidr,
			TargetType: "node",
			TargetID:   nodeID,
		}, 20, 0)
		if err == nil {
			for _, row := range rows {
				switch strings.ToLower(strings.TrimSpace(row.Status)) {
				case "proposed", "approved", "canary", "dispatching", "active":
					return
				}
			}
		}
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	reason := strings.TrimSpace(summary)
	if reason == "" {
		reason = fmt.Sprintf("100%% confidence known malicious source %s", cidr)
	}
	entry, err := store.CreateIPBlocklistEntry(ctx, storage.CreateIPBlocklistEntryParams{
		TenantID:    tenantID,
		IPCIDR:      cidr,
		Scope:       "node",
		TargetType:  "node",
		TargetID:    &nodeID,
		ServerGroup: b.serverGroup,
		App:         b.app,
		Enforcement: "firewall",
		Reason:      "Auto-block: " + reason,
		Score:       score,
		ExpiresAt:   &expiresAt,
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("create automatic threat-intel block", zap.String("ip_cidr", cidr), zap.Error(err))
		}
		return
	}
	action, err := s.recordBlockProposalEntityAction(ctx, entry, nil, now)
	if err != nil {
		_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, "record entity action: "+err.Error())
		return
	}
	if _, err := store.SetIPBlocklistEntryEntityAction(ctx, entry.ID, action.ID); err != nil && s.logger != nil {
		s.logger.Warn("link automatic threat-intel block action", zap.String("block_entry_id", entry.ID.String()), zap.Error(err))
	}
	if _, err := store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "dispatching", nil, ""); err != nil && s.logger != nil {
		s.logger.Warn("mark automatic threat-intel block dispatching", zap.String("block_entry_id", entry.ID.String()), zap.Error(err))
	}
	if dispatched, err := s.dispatchBlockProposalToNode(ctx, entry, action.ID, nodeID); err != nil {
		_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, err.Error())
		if s.logger != nil {
			s.logger.Warn("dispatch automatic threat-intel block", zap.String("ip_cidr", cidr), zap.Error(err))
		}
	} else if dispatched == 0 {
		_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, "no firewall dispatch target")
	} else {
		s.recordAudit(ctx, s.systemActor(), tenantID, "network.block_proposal.auto_dispatched", "ip_blocklist_entry", entry.ID.String(), map[string]any{
			"ip_cidr":      cidr,
			"node_id":      nodeID.String(),
			"score":        score,
			"category":     category,
			"expires_at":   expiresAt.Format(time.RFC3339),
			"dispatches":   dispatched,
			"threat_score": b.threatScore,
		})
	}
}

func (s *Server) backfillIPBehaviorConfidenceAlerts(ctx context.Context, findings []storage.IPBehaviorFinding) {
	for _, finding := range findings {
		s.openIPBehaviorFindingConfidenceAlert(ctx, finding)
	}
}

func (s *Server) openIPBehaviorFindingConfidenceAlert(ctx context.Context, finding storage.IPBehaviorFinding) {
	if finding.Score < 100 {
		return
	}
	if finding.Status != "" && !strings.EqualFold(finding.Status, "open") {
		return
	}
	nodeID := uuid.Nil
	if finding.NodeID.Valid {
		nodeID = finding.NodeID.UUID
	}
	b := ipBehaviorBucketFromFinding(finding)
	if b.srcIP == "" {
		return
	}
	s.openIPBehaviorConfidenceAlert(
		ctx,
		finding.TenantID,
		nodeID,
		finding.DedupKey,
		b,
		finding.Score,
		firstNonEmptyIPBehavior(finding.Severity, severityFromIPBehaviorScore(finding.Score)),
		firstNonEmptyIPBehavior(finding.Category, "ip_behavior"),
		finding.Reason,
		finding.Evidence,
	)
}

func ipBehaviorBucketFromFinding(finding storage.IPBehaviorFinding) *ipBehaviorBucket {
	evidence := finding.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	b := &ipBehaviorBucket{
		srcIP:       "",
		countryCode: finding.CountryCode,
		country:     stringFromMapAny(evidence, "country"),
		asn:         firstNonEmptyIPBehavior(finding.ASN, stringFromMapAny(evidence, "asn")),
		app:         stringFromMapAny(evidence, "app"),
		serverGroup: stringFromMapAny(evidence, "server_group"),
		windowLabel: firstNonEmptyIPBehavior(stringFromMapAny(evidence, "window"), "current window"),
		lastTS:      finding.LastSeenAt,
		count:       int(int64FromServerAny(evidence["request_count"])),
		bytesIn:     int64FromServerAny(evidence["bytes_in"]),
		bytesOut:    int64FromServerAny(evidence["bytes_out"]),
		statuses:    map[int]int{},
		paths:       map[string]int{},
	}
	if finding.SourceIP.Valid {
		b.srcIP = normalizeIPBehaviorAlertSource(finding.SourceIP.String)
	}
	if b.srcIP == "" {
		b.srcIP = normalizeIPBehaviorAlertSource(firstNonEmptyIPBehavior(stringFromMapAny(evidence, "src_ip"), sourceIPFromIPBehaviorReason(finding.Reason)))
	}
	if b.country == "" {
		b.country = firstNonEmptyIPBehavior(stringFromMapAny(evidence, "country_name"), b.countryCode)
	}
	if rawStatuses, ok := evidence["status_counts"].(map[string]any); ok {
		for rawCode, rawCount := range rawStatuses {
			code, err := strconv.Atoi(rawCode)
			if err != nil {
				continue
			}
			b.statuses[code] = int(int64FromServerAny(rawCount))
		}
	}
	return b
}

func sourceIPFromIPBehaviorReason(reason string) string {
	match := ipBehaviorReasonSourcePattern.FindStringSubmatch(reason)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func normalizeIPBehaviorAlertSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	ip, ipNet, err := net.ParseCIDR(value)
	if err != nil {
		return value
	}
	ones, bits := ipNet.Mask.Size()
	if ones == bits {
		return ip.String()
	}
	return ipNet.String()
}

func ipBehaviorAlertSummary(category string, b *ipBehaviorBucket, score int, reason string, evidence map[string]any) string {
	signals := ipBehaviorAlertSignals(reason, evidence)
	if len(signals) == 0 {
		signals = append(signals, "behavior exceeded learned network baselines")
	}
	if len(signals) > 3 {
		signals = signals[:3]
	}
	location := firstNonEmptyIPBehavior(b.country, b.countryCode, "unknown country")
	app := firstNonEmptyIPBehavior(b.app, b.serverGroup, "web traffic")
	return fmt.Sprintf("%s in %s for %s reached %d%% confidence: %s.", ipBehaviorAlertCategoryLabel(category), location, app, score, strings.Join(signals, ", "))
}

func ipBehaviorAlertSignals(reason string, evidence map[string]any) []string {
	out := make([]string, 0, 6)
	if evidence != nil {
		if count := int(int64FromServerAny(evidence["request_count"])); count > 0 {
			window := firstNonEmptyIPBehavior(stringFromMapAny(evidence, "window"), "current window")
			out = append(out, fmt.Sprintf("%d requests in %s", count, window))
		}
		if statuses, ok := evidence["status_counts"].(map[string]any); ok {
			auth := int(int64FromServerAny(statuses["401"]) + int64FromServerAny(statuses["403"]))
			serverErrors := int(int64FromServerAny(statuses["500"]) + int64FromServerAny(statuses["502"]) + int64FromServerAny(statuses["503"]) + int64FromServerAny(statuses["5xx"]))
			if auth > 0 {
				out = append(out, fmt.Sprintf("%d auth failures", auth))
			}
			if serverErrors > 0 {
				out = append(out, fmt.Sprintf("%d server errors", serverErrors))
			}
		}
		if bytesOut := int(int64FromServerAny(evidence["bytes_out"])); bytesOut >= 1024*1024 {
			out = append(out, fmt.Sprintf("%d MiB outbound", bytesOut/(1024*1024)))
		}
	}
	for _, raw := range strings.Split(ipBehaviorReasonBody(reason), ";") {
		signal := ipBehaviorHumanAlertReason(strings.TrimSpace(raw))
		if signal == "" || stringSliceContainsFold(out, signal) {
			continue
		}
		out = append(out, signal)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func ipBehaviorReasonBody(reason string) string {
	if idx := strings.Index(reason, ":"); idx >= 0 {
		return reason[idx+1:]
	}
	return reason
}

func ipBehaviorHumanAlertReason(reason string) string {
	if reason == "" {
		return ""
	}
	lower := strings.ToLower(reason)
	if strings.Contains(lower, "behavior is being evaluated as a baseline dimension") {
		return ""
	}
	if strings.HasPrefix(lower, "request burst:") {
		return strings.TrimSpace(strings.TrimPrefix(reason, "request burst:"))
	}
	if strings.HasPrefix(lower, "auth failure spike:") {
		return strings.TrimSpace(strings.TrimPrefix(reason, "auth failure spike:"))
	}
	if strings.HasPrefix(lower, "server error spike:") {
		return strings.TrimSpace(strings.TrimPrefix(reason, "server error spike:"))
	}
	if strings.Contains(lower, "sensitive/admin path probing") {
		return "admin path probing"
	}
	if strings.Contains(lower, "previously inactive hour") {
		return "inactive-hour traffic"
	}
	if strings.Contains(lower, "previously inactive weekday") {
		return "inactive weekday traffic"
	}
	if strings.Contains(lower, "auth failure ratio") {
		return "auth failures exceeded learned baseline"
	}
	if strings.Contains(lower, "server-error ratio") {
		return "server errors exceeded learned baseline"
	}
	if strings.Contains(lower, "request rate") {
		return "request rate exceeded learned baseline"
	}
	if strings.Contains(lower, "bytes out") {
		return "bytes out exceeded learned baseline"
	}
	return reason
}

func ipBehaviorAlertCategoryLabel(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "credential_attack":
		return "credential attack"
	case "exploit_attempt":
		return "exploit attempt"
	case "exfiltration_risk":
		return "exfiltration risk"
	case "scanner_probe":
		return "scanner probe"
	case "slow_distributed_attack":
		return "distributed attack"
	case "webshell_callback":
		return "webshell callback"
	case "partner_drift":
		return "partner drift"
	case "known_malicious_source":
		return "known malicious source"
	default:
		return "IP behavior anomaly"
	}
}

func stringFromMapAny(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (s *Server) handleIPBehaviorOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(ipBehaviorQueryStore)
	if !ok {
		http.Error(w, "ip behavior store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, since, ok := parseIPBehaviorTenantSince(w, r, time.Hour)
	if !ok {
		return
	}
	countries, err := store.ListIPBehaviorCountries(r.Context(), tenantID, since, "")
	if err != nil {
		s.logger.Warn("list ip behavior overview", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	var totalReq, totalBytes int64
	statusTotals := map[string]int64{}
	for _, c := range countries {
		totalReq += c.RequestCount
		totalBytes += c.BytesOut
		for k, v := range c.StatusCounts {
			statusTotals[k] += v
		}
	}
	if len(countries) > 10 {
		countries = countries[:10]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":     tenantID.String(),
		"since":         since.UTC().Format(time.RFC3339),
		"request_count": totalReq,
		"bytes_out":     totalBytes,
		"status_counts": statusTotals,
		"top_countries": countries,
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleIPBehaviorCountries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(ipBehaviorQueryStore)
	if !ok {
		http.Error(w, "ip behavior store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, since, ok := parseIPBehaviorTenantSince(w, r, 24*time.Hour)
	if !ok {
		return
	}
	countries, err := store.ListIPBehaviorCountries(r.Context(), tenantID, since, "")
	if err != nil {
		s.logger.Warn("list ip behavior countries", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"countries": countries, "since": since.UTC().Format(time.RFC3339)})
}

func (s *Server) handleIPBehaviorCountryDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(ipBehaviorQueryStore)
	if !ok {
		http.Error(w, "ip behavior store unavailable", http.StatusServiceUnavailable)
		return
	}
	code := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/ip-behavior/countries/"), "/")
	if code == "" {
		http.NotFound(w, r)
		return
	}
	tenantID, since, ok := parseIPBehaviorTenantSince(w, r, 24*time.Hour)
	if !ok {
		return
	}
	countries, err := store.ListIPBehaviorCountries(r.Context(), tenantID, since, code)
	if err != nil {
		s.logger.Warn("get ip behavior country", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if len(countries) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, countries[0])
}

func (s *Server) handleIPBehaviorIPProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(ipBehaviorQueryStore)
	if !ok {
		http.Error(w, "ip behavior store unavailable", http.StatusServiceUnavailable)
		return
	}
	ip := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/ip-behavior/ips/"), "/")
	if net.ParseIP(ip) == nil {
		http.Error(w, "invalid ip", http.StatusBadRequest)
		return
	}
	tenantID, since, ok := parseIPBehaviorTenantSince(w, r, 24*time.Hour)
	if !ok {
		return
	}
	profile, err := store.GetIPBehaviorIPProfile(r.Context(), tenantID, ip, since)
	if err != nil {
		s.logger.Warn("get ip behavior ip profile", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if profile == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleIPBehaviorBaselines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(ipBehaviorQueryStore)
	if !ok {
		http.Error(w, "ip behavior store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	baselines, total, err := store.ListIPBehaviorBaselines(r.Context(), tenantID, r.URL.Query().Get("dimension"), limit, offset)
	if err != nil {
		s.logger.Warn("list ip behavior baselines", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       baselines,
		"pagination": newPaginationMeta(total, limit, offset, len(baselines)),
	})
}

type createBlockProposalRequest struct {
	TenantID                string  `json:"tenant_id"`
	FindingID               *string `json:"finding_id"`
	IPCIDR                  string  `json:"ip_cidr"`
	Scope                   string  `json:"scope"`
	TargetType              string  `json:"target_type"`
	TargetID                *string `json:"target_id"`
	ServerGroup             string  `json:"server_group"`
	App                     string  `json:"app"`
	VHost                   string  `json:"vhost"`
	Enforcement             string  `json:"enforcement"`
	Reason                  string  `json:"reason"`
	Score                   int     `json:"score"`
	TTLSeconds              int     `json:"ttl_seconds"`
	ProtectedOverride       bool    `json:"protected_override"`
	ProtectedOverrideReason string  `json:"protected_override_reason"`
}

type createASNBlockProposalsRequest struct {
	TenantID                string `json:"tenant_id"`
	ASN                     string `json:"asn"`
	Since                   string `json:"since"`
	Limit                   int    `json:"limit"`
	Scope                   string `json:"scope"`
	TargetType              string `json:"target_type"`
	ServerGroup             string `json:"server_group"`
	App                     string `json:"app"`
	VHost                   string `json:"vhost"`
	Enforcement             string `json:"enforcement"`
	Reason                  string `json:"reason"`
	Score                   int    `json:"score"`
	TTLSeconds              int    `json:"ttl_seconds"`
	ProtectedOverride       bool   `json:"protected_override"`
	ProtectedOverrideReason string `json:"protected_override_reason"`
}

type asnBlockProposalSkipped struct {
	SourceIP string `json:"source_ip"`
	Reason   string `json:"reason"`
}

type asnBlockProposalsResponse struct {
	ASN             string                    `json:"asn"`
	TotalCandidates int                       `json:"total_candidates"`
	Created         []blockProposalResponse   `json:"created"`
	Skipped         []asnBlockProposalSkipped `json:"skipped"`
	Limit           int                       `json:"limit"`
	GeneratedAt     string                    `json:"generated_at"`
}

type blockProposalLifecycleRequest struct {
	Reason string `json:"reason"`
}

type blockProposalResponse struct {
	ID                      string  `json:"id"`
	TenantID                string  `json:"tenant_id"`
	FindingID               *string `json:"finding_id,omitempty"`
	EntityActionID          *string `json:"entity_action_id,omitempty"`
	IPCIDR                  string  `json:"ip_cidr"`
	Scope                   string  `json:"scope"`
	TargetType              string  `json:"target_type"`
	TargetID                *string `json:"target_id,omitempty"`
	ServerGroup             string  `json:"server_group"`
	App                     string  `json:"app"`
	VHost                   string  `json:"vhost"`
	Enforcement             string  `json:"enforcement"`
	Status                  string  `json:"status"`
	Reason                  string  `json:"reason"`
	Score                   int     `json:"score"`
	ExpiresAt               *string `json:"expires_at,omitempty"`
	ApprovedBy              *string `json:"approved_by,omitempty"`
	ApprovedAt              *string `json:"approved_at,omitempty"`
	LastError               *string `json:"last_error,omitempty"`
	ProtectedOverride       bool    `json:"protected_override"`
	ProtectedOverrideReason string  `json:"protected_override_reason,omitempty"`
	CreatedAt               string  `json:"created_at"`
	UpdatedAt               string  `json:"updated_at"`
}

func newBlockProposalResponse(entry *storage.IPBlocklistEntry) blockProposalResponse {
	if entry == nil {
		return blockProposalResponse{}
	}
	resp := blockProposalResponse{
		ID:                      entry.ID.String(),
		TenantID:                entry.TenantID.String(),
		IPCIDR:                  entry.IPCIDR,
		Scope:                   entry.Scope,
		TargetType:              entry.TargetType,
		ServerGroup:             entry.ServerGroup,
		App:                     entry.App,
		VHost:                   entry.VHost,
		Enforcement:             entry.Enforcement,
		Status:                  entry.Status,
		Reason:                  entry.Reason,
		Score:                   entry.Score,
		ExpiresAt:               formatNullTime(entry.ExpiresAt),
		ApprovedAt:              formatNullTime(entry.ApprovedAt),
		ProtectedOverride:       entry.ProtectedOverride,
		ProtectedOverrideReason: entry.ProtectedOverrideReason,
		CreatedAt:               formatTime(entry.CreatedAt),
		UpdatedAt:               formatTime(entry.UpdatedAt),
	}
	if entry.FindingID.Valid {
		v := entry.FindingID.UUID.String()
		resp.FindingID = &v
	}
	if entry.EntityActionID.Valid {
		v := entry.EntityActionID.UUID.String()
		resp.EntityActionID = &v
	}
	if entry.TargetID.Valid {
		v := entry.TargetID.UUID.String()
		resp.TargetID = &v
	}
	if entry.ApprovedBy.Valid {
		v := entry.ApprovedBy.UUID.String()
		resp.ApprovedBy = &v
	}
	if entry.LastError.Valid {
		v := entry.LastError.String
		resp.LastError = &v
	}
	return resp
}

func (s *Server) handleBlockProposals(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListBlockProposals(w, r)
	case http.MethodPost:
		s.handleCreateBlockProposal(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) handleListBlockProposals(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	store, ok := s.store.(ipBlockProposalQueryStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.IPBlocklistEntryFilter{
		TenantID:    tenantID,
		IPCIDR:      strings.TrimSpace(r.URL.Query().Get("ip_cidr")),
		Status:      strings.TrimSpace(r.URL.Query().Get("status")),
		ServerGroup: strings.TrimSpace(r.URL.Query().Get("server_group")),
		TargetType:  strings.TrimSpace(r.URL.Query().Get("target_type")),
		App:         strings.TrimSpace(r.URL.Query().Get("app")),
		VHost:       strings.TrimSpace(r.URL.Query().Get("vhost")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("finding_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid finding_id", http.StatusBadRequest)
			return
		}
		filter.FindingID = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("target_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid target_id", http.StatusBadRequest)
			return
		}
		filter.TargetID = parsed
	}
	rows, total, err := store.ListIPBlocklistEntries(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Warn("list block proposals", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]blockProposalResponse, 0, len(rows))
	for i := range rows {
		resp = append(resp, newBlockProposalResponse(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       resp,
		"pagination": newPaginationMeta(total, limit, offset, len(resp)),
	})
}

func (s *Server) handleCreateBlockProposal(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createBlockProposalRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if !validIPOrCIDR(req.IPCIDR) {
		http.Error(w, "ip_cidr must be an IP or CIDR", http.StatusBadRequest)
		return
	}
	if err := validateBlockProposalTTL(req.TTLSeconds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	protectedOverride := false
	protectedOverrideReason := strings.TrimSpace(req.ProtectedOverrideReason)
	if protected := s.protectedIPBlockReason(r.Context(), tenantID, req.IPCIDR); protected != "" {
		if !req.ProtectedOverride {
			s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "ip", strings.TrimSpace(req.IPCIDR), map[string]any{
				"reason": "protected target: " + protected,
				"stage":  "create",
			})
			http.Error(w, "ip_cidr overlaps protected tenant range: "+protected, http.StatusConflict)
			return
		}
		if !hasRole(principal, roleAdmin) {
			s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "ip", strings.TrimSpace(req.IPCIDR), map[string]any{
				"reason": "protected override requires admin",
				"stage":  "create",
			})
			http.Error(w, "protected CIDR override requires admin", http.StatusForbidden)
			return
		}
		if protectedOverrideReason == "" {
			s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "ip", strings.TrimSpace(req.IPCIDR), map[string]any{
				"reason": "protected override reason required",
				"stage":  "create",
			})
			http.Error(w, "protected_override_reason is required", http.StatusBadRequest)
			return
		}
		protectedOverride = true
		s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.protected_override", "ip", strings.TrimSpace(req.IPCIDR), map[string]any{
			"protected_range": protected,
			"reason":          protectedOverrideReason,
			"stage":           "create",
		})
	}
	serverGroup := strings.TrimSpace(req.ServerGroup)
	if status, msg := s.blockProposalSafetyViolation(r.Context(), tenantID, serverGroup); status != 0 {
		s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "ip", strings.TrimSpace(req.IPCIDR), map[string]any{
			"reason": msg,
			"stage":  "create",
		})
		http.Error(w, msg, status)
		return
	}
	var findingID, targetID *uuid.UUID
	if req.FindingID != nil && strings.TrimSpace(*req.FindingID) != "" {
		parsed, err := uuid.Parse(*req.FindingID)
		if err != nil {
			http.Error(w, "invalid finding_id", http.StatusBadRequest)
			return
		}
		findingID = &parsed
	}
	if req.TargetID != nil && strings.TrimSpace(*req.TargetID) != "" {
		parsed, err := uuid.Parse(*req.TargetID)
		if err != nil {
			http.Error(w, "invalid target_id", http.StatusBadRequest)
			return
		}
		targetID = &parsed
	}
	var expiresAt *time.Time
	if req.TTLSeconds > 0 {
		t := time.Now().UTC().Add(time.Duration(req.TTLSeconds) * time.Second)
		expiresAt = &t
	}
	entry, err := store.CreateIPBlocklistEntry(r.Context(), storage.CreateIPBlocklistEntryParams{
		TenantID:                tenantID,
		FindingID:               findingID,
		IPCIDR:                  req.IPCIDR,
		Scope:                   req.Scope,
		TargetType:              req.TargetType,
		TargetID:                targetID,
		ServerGroup:             serverGroup,
		App:                     strings.TrimSpace(req.App),
		VHost:                   strings.TrimSpace(req.VHost),
		Enforcement:             req.Enforcement,
		Reason:                  req.Reason,
		Score:                   req.Score,
		ExpiresAt:               expiresAt,
		ProtectedOverride:       protectedOverride,
		ProtectedOverrideReason: protectedOverrideReason,
	})
	if err != nil {
		s.logger.Warn("create block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.created", "ip_blocklist_entry", entry.ID.String(), map[string]any{
		"ip_cidr":            entry.IPCIDR,
		"scope":              entry.Scope,
		"server_group":       entry.ServerGroup,
		"protected_override": entry.ProtectedOverride,
	})
	writeJSON(w, http.StatusAccepted, newBlockProposalResponse(entry))
}

func (s *Server) handleCreateASNBlockProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	proposalStore, ok := s.store.(ipBlockProposalStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	sourceStore, ok := s.store.(ipBehaviorASNSourceStore)
	if !ok {
		http.Error(w, "ip behavior ASN source store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createASNBlockProposalsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	asn := strings.TrimSpace(req.ASN)
	if asn == "" {
		http.Error(w, "asn is required", http.StatusBadRequest)
		return
	}
	if err := validateBlockProposalTTL(req.TTLSeconds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	if raw := strings.TrimSpace(req.Since); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		since = parsed
	}
	serverGroup := strings.TrimSpace(req.ServerGroup)
	if status, msg := s.blockProposalSafetyViolation(r.Context(), tenantID, serverGroup); status != 0 {
		s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "asn", asn, map[string]any{
			"reason": msg,
			"stage":  "asn_create",
		})
		http.Error(w, msg, status)
		return
	}
	sources, err := sourceStore.ListIPBehaviorSourceIPsByASN(r.Context(), tenantID, asn, since, limit)
	if err != nil {
		s.logger.Warn("list asn source ips", zap.String("asn", asn), zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if len(sources) == 0 {
		http.Error(w, "no observed source IPs for ASN in selected window", http.StatusNotFound)
		return
	}
	scope := firstNonEmptyIPBehavior(strings.TrimSpace(req.Scope), "tenant")
	targetType := firstNonEmptyIPBehavior(strings.TrimSpace(req.TargetType), "tenant")
	enforcement := firstNonEmptyIPBehavior(strings.TrimSpace(req.Enforcement), "firewall")
	protectedOverrideReason := strings.TrimSpace(req.ProtectedOverrideReason)
	now := time.Now().UTC()
	var expiresAt *time.Time
	if req.TTLSeconds > 0 {
		t := now.Add(time.Duration(req.TTLSeconds) * time.Second)
		expiresAt = &t
	}
	created := make([]blockProposalResponse, 0, len(sources))
	skipped := make([]asnBlockProposalSkipped, 0)
	for _, source := range sources {
		cidr := exactCIDRForIP(source.SourceIP)
		if cidr == "" {
			skipped = append(skipped, asnBlockProposalSkipped{SourceIP: source.SourceIP, Reason: "invalid source IP"})
			continue
		}
		protectedOverride := false
		if protected := s.protectedIPBlockReason(r.Context(), tenantID, cidr); protected != "" {
			if !req.ProtectedOverride {
				skipped = append(skipped, asnBlockProposalSkipped{SourceIP: source.SourceIP, Reason: "protected target: " + protected})
				continue
			}
			if !hasRole(principal, roleAdmin) {
				s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "asn", asn, map[string]any{
					"source_ip": source.SourceIP,
					"reason":    "protected override requires admin",
					"stage":     "asn_create",
				})
				http.Error(w, "protected CIDR override requires admin", http.StatusForbidden)
				return
			}
			if protectedOverrideReason == "" {
				http.Error(w, "protected_override_reason is required", http.StatusBadRequest)
				return
			}
			protectedOverride = true
		}
		reason := asnBlockProposalReason(req.Reason, asn, source)
		entry, err := proposalStore.CreateIPBlocklistEntry(r.Context(), storage.CreateIPBlocklistEntryParams{
			TenantID:                tenantID,
			IPCIDR:                  cidr,
			Scope:                   scope,
			TargetType:              targetType,
			ServerGroup:             serverGroup,
			App:                     strings.TrimSpace(req.App),
			VHost:                   strings.TrimSpace(req.VHost),
			Enforcement:             enforcement,
			Reason:                  reason,
			Score:                   req.Score,
			ExpiresAt:               expiresAt,
			ProtectedOverride:       protectedOverride,
			ProtectedOverrideReason: protectedOverrideReason,
		})
		if err != nil {
			s.logger.Warn("create asn block proposal", zap.String("asn", asn), zap.String("source_ip", source.SourceIP), zap.Error(err))
			skipped = append(skipped, asnBlockProposalSkipped{SourceIP: source.SourceIP, Reason: "create failed"})
			continue
		}
		created = append(created, newBlockProposalResponse(entry))
	}
	if len(created) == 0 {
		s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.rejected", "asn", asn, map[string]any{
			"reason":        "no eligible source IPs",
			"skipped_count": len(skipped),
			"stage":         "asn_create",
		})
		http.Error(w, "no eligible source IPs for ASN block proposal", http.StatusConflict)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "network.block_proposal.asn_created", "asn", asn, map[string]any{
		"created_count": len(created),
		"skipped_count": len(skipped),
		"scope":         scope,
		"server_group":  serverGroup,
		"enforcement":   enforcement,
	})
	writeJSON(w, http.StatusAccepted, asnBlockProposalsResponse{
		ASN:             asn,
		TotalCandidates: len(sources),
		Created:         created,
		Skipped:         skipped,
		Limit:           limit,
		GeneratedAt:     formatTime(now),
	})
}

func validateBlockProposalTTL(ttlSeconds int) error {
	switch ttlSeconds {
	case 0, 900, 3600, 86400:
		return nil
	default:
		return fmt.Errorf("ttl_seconds must be 900, 3600, 86400, or 0 for permanent approval")
	}
}

func (s *Server) handleBlockProposalSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/network/block-proposals/"), "/")
	segments := strings.Split(trimmed, "/")
	if len(segments) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid block proposal id", http.StatusBadRequest)
		return
	}
	switch segments[1] {
	case "approve":
		s.handleApproveBlockProposal(w, r, id)
	case "promote":
		s.handlePromoteBlockProposal(w, r, id)
	case "reject":
		s.handleRejectBlockProposal(w, r, id)
	case "rollback":
		s.handleRollbackBlockProposal(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleApproveBlockProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	entry, err := store.GetIPBlocklistEntry(r.Context(), id)
	if err != nil {
		s.logger.Warn("get block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.NotFound(w, r)
		return
	}
	if protected := s.protectedIPBlockReason(r.Context(), entry.TenantID, entry.IPCIDR); protected != "" {
		if !entry.ProtectedOverride {
			updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "protected target: "+protected)
			s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.rejected", "ip_blocklist_entry", id.String(), map[string]any{
				"ip_cidr": entry.IPCIDR,
				"reason":  "protected target: " + protected,
				"stage":   "approve",
			})
			writeJSON(w, http.StatusConflict, newBlockProposalResponse(updated))
			return
		}
		if !hasRole(principal, roleAdmin) {
			s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.rejected", "ip_blocklist_entry", id.String(), map[string]any{
				"ip_cidr": entry.IPCIDR,
				"reason":  "protected override approval requires admin",
				"stage":   "approve",
			})
			http.Error(w, "protected CIDR override approval requires admin", http.StatusForbidden)
			return
		}
		if strings.TrimSpace(entry.ProtectedOverrideReason) == "" {
			updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "protected override reason missing")
			writeJSON(w, http.StatusConflict, newBlockProposalResponse(updated))
			return
		}
		s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.protected_override_approved", "ip_blocklist_entry", id.String(), map[string]any{
			"ip_cidr":         entry.IPCIDR,
			"protected_range": protected,
			"override_reason": entry.ProtectedOverrideReason,
			"stage":           "approve",
		})
	}
	if !strings.EqualFold(entry.Status, "proposed") {
		http.Error(w, "block proposal is not pending approval", http.StatusConflict)
		return
	}
	if status, msg := s.blockProposalSafetyViolation(r.Context(), entry.TenantID, entry.ServerGroup); status != 0 {
		s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.rejected", "ip_blocklist_entry", id.String(), map[string]any{
			"ip_cidr": entry.IPCIDR,
			"reason":  msg,
			"stage":   "approve",
		})
		http.Error(w, msg, status)
		return
	}
	now := time.Now().UTC()
	if entry.ExpiresAt.Valid && !entry.ExpiresAt.Time.After(now) {
		updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "expired", nil, "proposal expired before approval")
		writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
		return
	}
	approverID := principalUserID(s, r.Context(), principal)
	var approverPtr *uuid.UUID
	if approverID != uuid.Nil {
		approverPtr = &approverID
	}
	blockAction, err := s.recordBlockProposalEntityAction(r.Context(), entry, approverPtr, now)
	if err != nil {
		s.logger.Warn("record block proposal entity action", zap.Error(err), zap.String("block_entry_id", id.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if _, linkErr := store.SetIPBlocklistEntryEntityAction(r.Context(), id, blockAction.ID); linkErr != nil {
		s.logger.Warn("link block proposal entity action", zap.Error(linkErr), zap.String("block_entry_id", id.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	updated, err := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "approved", approverPtr, "")
	if err != nil {
		s.logger.Warn("approve block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	entry = updated
	dispatched := 0
	if strings.EqualFold(entry.TargetType, "node") && entry.TargetID.Valid {
		if !enforcementWantsFirewall(entry.Enforcement) && !enforcementWantsWebserver(entry.Enforcement) {
			updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, fmt.Sprintf("unsupported enforcement target %q", entry.Enforcement))
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
			return
		}
		n, ferr := s.dispatchBlockProposalToNode(r.Context(), entry, blockAction.ID, entry.TargetID.UUID)
		if ferr != nil {
			updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, ferr.Error())
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
			return
		}
		dispatched += n
		if dispatched == 0 {
			updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "no eligible enforcement targets for node-scoped block")
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
			return
		}
		updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "dispatching", nil, "")
	} else if strings.EqualFold(entry.TargetType, "tenant") || strings.EqualFold(entry.TargetType, "fleet") || strings.EqualFold(entry.Scope, "tenant") || strings.EqualFold(entry.Scope, "fleet") {
		if s.blockProposalCanaryEnabled() {
			var groups []string
			dispatched, groups, err = s.dispatchBlockProposalCanaryToTenantNodes(r.Context(), entry, blockAction.ID)
			if err != nil {
				updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, err.Error())
				writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
				return
			}
			if dispatched == 0 {
				updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "no active nodes available for tenant-scoped canary")
				writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
				return
			}
			updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "canary", nil, "")
			s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.canary_started", "ip_blocklist_entry", entry.ID.String(), map[string]any{
				"ip_cidr":                 entry.IPCIDR,
				"entity_action_id":        blockAction.ID.String(),
				"dispatched":              dispatched,
				"server_groups":           groups,
				"nodes_per_server_group":  s.blockCanaryNodesPerServerGroup(),
				"requires_manual_promote": true,
			})
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
			return
		}
		dispatched, ferr := s.dispatchBlockProposalToTenantNodes(r.Context(), entry, blockAction.ID)
		if ferr != nil {
			updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, ferr.Error())
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
			return
		}
		if dispatched == 0 {
			updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "no active nodes available for tenant-scoped block")
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
			return
		}
		updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "dispatching", nil, "")
	} else {
		updated, _ = store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, fmt.Sprintf("unsupported target type %q", entry.TargetType))
		writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
		return
	}
	s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.approved", "ip_blocklist_entry", entry.ID.String(), map[string]any{
		"ip_cidr":          entry.IPCIDR,
		"target":           entry.TargetType,
		"entity_action_id": blockAction.ID.String(),
	})
	writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
}

func (s *Server) handlePromoteBlockProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	entry, err := store.GetIPBlocklistEntry(r.Context(), id)
	if err != nil {
		s.logger.Warn("get block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.NotFound(w, r)
		return
	}
	if !strings.EqualFold(entry.Status, "canary") {
		http.Error(w, "block proposal is not awaiting canary promotion", http.StatusConflict)
		return
	}
	if !entry.EntityActionID.Valid {
		updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "canary block is missing entity action link")
		writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
		return
	}
	if protected := s.protectedIPBlockReason(r.Context(), entry.TenantID, entry.IPCIDR); protected != "" {
		if !entry.ProtectedOverride {
			updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "protected target: "+protected)
			s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.canary_stopped", "ip_blocklist_entry", id.String(), map[string]any{
				"ip_cidr": entry.IPCIDR,
				"reason":  "protected target: " + protected,
			})
			writeJSON(w, http.StatusConflict, newBlockProposalResponse(updated))
			return
		}
		if !hasRole(principal, roleAdmin) {
			s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.canary_stopped", "ip_blocklist_entry", id.String(), map[string]any{
				"ip_cidr": entry.IPCIDR,
				"reason":  "protected override promotion requires admin",
			})
			http.Error(w, "protected CIDR override promotion requires admin", http.StatusForbidden)
			return
		}
		s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.protected_override_promoted", "ip_blocklist_entry", id.String(), map[string]any{
			"ip_cidr":         entry.IPCIDR,
			"protected_range": protected,
			"override_reason": entry.ProtectedOverrideReason,
			"stage":           "promote",
		})
	}
	if status, msg := s.blockProposalSafetyViolation(r.Context(), entry.TenantID, entry.ServerGroup); status != 0 {
		s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.canary_stopped", "ip_blocklist_entry", id.String(), map[string]any{
			"ip_cidr": entry.IPCIDR,
			"reason":  msg,
		})
		http.Error(w, msg, status)
		return
	}
	if reason := s.blockProposalCanaryFailureReason(r.Context(), entry); reason != "" {
		updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, reason)
		s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.canary_stopped", "ip_blocklist_entry", id.String(), map[string]any{
			"ip_cidr": entry.IPCIDR,
			"reason":  reason,
		})
		writeJSON(w, http.StatusConflict, newBlockProposalResponse(updated))
		return
	}
	skip := s.blockProposalDispatchedNodeIDs(r.Context(), entry)
	dispatched, err := s.dispatchBlockProposalToTenantNodesExcluding(r.Context(), entry, entry.EntityActionID.UUID, skip)
	if err != nil {
		updated, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, err.Error())
		writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
		return
	}
	updated, err := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "dispatching", nil, "")
	if err != nil {
		s.logger.Warn("promote block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.canary_promoted", "ip_blocklist_entry", entry.ID.String(), map[string]any{
		"ip_cidr":       entry.IPCIDR,
		"dispatched":    dispatched,
		"skipped_nodes": len(skip),
	})
	writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
}

func (s *Server) handleRejectBlockProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	entry, err := store.GetIPBlocklistEntry(r.Context(), id)
	if err != nil {
		s.logger.Warn("get block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.NotFound(w, r)
		return
	}
	if !strings.EqualFold(entry.Status, "proposed") {
		http.Error(w, "only proposed block proposals can be rejected; use rollback for dispatched blocks", http.StatusConflict)
		return
	}
	reason := decodeLifecycleReason(r, "operator rejected proposal")
	updated, err := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "rejected", nil, reason)
	if err != nil {
		s.logger.Warn("reject block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.rejected", "ip_blocklist_entry", id.String(), map[string]any{
		"ip_cidr": entry.IPCIDR,
		"reason":  reason,
		"stage":   "operator",
	})
	writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
}

func (s *Server) handleRollbackBlockProposal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(ipBlockProposalStore)
	if !ok {
		http.Error(w, "block proposal store unavailable", http.StatusServiceUnavailable)
		return
	}
	entry, err := store.GetIPBlocklistEntry(r.Context(), id)
	if err != nil {
		s.logger.Warn("get block proposal", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.NotFound(w, r)
		return
	}
	if blockProposalStatusTerminal(entry.Status) {
		http.Error(w, "block proposal is already terminal", http.StatusConflict)
		return
	}
	reason := decodeLifecycleReason(r, "operator requested rollback")
	if !entry.EntityActionID.Valid {
		updated, err := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "rolled_back", nil, reason)
		if err != nil {
			s.logger.Warn("rollback pending block proposal", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.rolled_back", "ip_blocklist_entry", id.String(), map[string]any{
			"ip_cidr": entry.IPCIDR,
			"reason":  reason,
		})
		writeJSON(w, http.StatusAccepted, newBlockProposalResponse(updated))
		return
	}
	updated, err := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "rolled_back", nil, reason)
	if err != nil {
		s.logger.Warn("mark block proposal rollback", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	entry = updated
	firewallQueued := 0
	webserverQueued := 0
	if enforcementWantsFirewall(entry.Enforcement) {
		firewallQueued, err = s.queueFirewallRemovalForBlockEntry(r.Context(), entry, entry.EntityActionID.UUID)
		if err != nil {
			failed, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "rollback failed: "+err.Error())
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(failed))
			return
		}
	}
	if enforcementWantsWebserver(entry.Enforcement) {
		webserverQueued, err = s.refreshWebserverBlocklistsForRolledBackEntry(r.Context(), entry, time.Now().UTC(), reason)
		if err != nil {
			failed, _ := store.UpdateIPBlocklistEntryStatus(r.Context(), id, "failed", nil, "rollback failed: "+err.Error())
			writeJSON(w, http.StatusAccepted, newBlockProposalResponse(failed))
			return
		}
	}
	s.recordAudit(r.Context(), principal, entry.TenantID, "network.block_proposal.rolled_back", "ip_blocklist_entry", id.String(), map[string]any{
		"ip_cidr":          entry.IPCIDR,
		"reason":           reason,
		"entity_action_id": entry.EntityActionID.UUID.String(),
		"firewall_jobs":    firewallQueued,
		"webserver_jobs":   webserverQueued,
	})
	writeJSON(w, http.StatusAccepted, newBlockProposalResponse(entry))
}

func decodeLifecycleReason(r *http.Request, fallback string) string {
	reason := ""
	if r != nil && r.Body != nil {
		var req blockProposalLifecycleRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err == nil {
			reason = strings.TrimSpace(req.Reason)
		}
	}
	if reason == "" {
		reason = fallback
	}
	return reason
}

func (s *Server) recordBlockProposalEntityAction(ctx context.Context, entry *storage.IPBlocklistEntry, approverID *uuid.UUID, now time.Time) (*storage.EntityAction, error) {
	if s == nil || entry == nil {
		return nil, errors.New("block proposal unavailable")
	}
	ib := s.investigateBackend()
	if ib == nil {
		return nil, errors.New("investigate store unavailable")
	}
	action := storage.EntityAction{
		TenantID:   entry.TenantID,
		EntityType: "ip",
		EntityID:   entry.IPCIDR,
		Action:     "block",
		Reason:     entry.Reason,
		CreatedBy:  approverID,
	}
	if entry.ExpiresAt.Valid {
		expiresAt := entry.ExpiresAt.Time.UTC()
		if expiresAt.After(now) {
			ttl := int(expiresAt.Sub(now).Seconds())
			if ttl <= 0 {
				ttl = 1
			}
			action.TTLSeconds = &ttl
			action.ExpiresAt = &expiresAt
		}
	}
	row, err := ib.RecordEntityAction(ctx, action)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ID == uuid.Nil {
		return nil, errors.New("record entity action returned empty id")
	}
	return row, nil
}

func (s *Server) dispatchBlockProposalToTenantNodes(ctx context.Context, entry *storage.IPBlocklistEntry, entityActionID uuid.UUID) (int, error) {
	return s.dispatchBlockProposalToTenantNodesExcluding(ctx, entry, entityActionID, nil)
}

func (s *Server) dispatchBlockProposalToTenantNodesExcluding(ctx context.Context, entry *storage.IPBlocklistEntry, entityActionID uuid.UUID, skip map[uuid.UUID]struct{}) (int, error) {
	if s == nil || s.store == nil || entry == nil {
		return 0, errors.New("store unavailable")
	}
	if entityActionID == uuid.Nil {
		return 0, errors.New("entity action id required for block dispatch")
	}
	dispatched := 0
	for offset := 0; ; offset += 500 {
		nodes, _, err := s.store.ListNodes(ctx, entry.TenantID, "", 500, offset)
		if err != nil {
			return dispatched, fmt.Errorf("list tenant nodes: %w", err)
		}
		for _, node := range nodes {
			if node.State != storage.NodeStateActive {
				continue
			}
			if !blockProposalMatchesNodeServerGroup(entry, node) {
				continue
			}
			if _, ok := skip[node.ID]; ok {
				continue
			}
			n, err := s.dispatchBlockProposalToNode(ctx, entry, entityActionID, node.ID)
			if err != nil {
				return dispatched, err
			}
			dispatched += n
		}
		if len(nodes) < 500 {
			break
		}
	}
	return dispatched, nil
}

func (s *Server) dispatchBlockProposalToNode(ctx context.Context, entry *storage.IPBlocklistEntry, entityActionID uuid.UUID, nodeID uuid.UUID) (int, error) {
	if s == nil || s.store == nil || entry == nil || nodeID == uuid.Nil {
		return 0, errors.New("node block dispatch unavailable")
	}
	ttlSeconds := ttlSecondsFromBlockEntry(entry)
	dispatched := 0
	doFirewall := enforcementWantsFirewall(entry.Enforcement)
	doWebserver := enforcementWantsWebserver(entry.Enforcement)
	if !doFirewall && !doWebserver {
		return 0, fmt.Errorf("unsupported enforcement target %q", entry.Enforcement)
	}
	if doFirewall {
		if _, _, err := s.dispatchFirewallRule(ctx, entry.TenantID, entityActionID, nodeID, "block", entry.IPCIDR, entry.Reason, ttlSeconds); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	if doWebserver {
		n, err := s.dispatchBlockProposalToWebserversOnNode(ctx, entry, nodeID)
		if err != nil {
			return dispatched, err
		}
		dispatched += n
	}
	return dispatched, nil
}

func (s *Server) dispatchBlockProposalCanaryToTenantNodes(ctx context.Context, entry *storage.IPBlocklistEntry, entityActionID uuid.UUID) (int, []string, error) {
	if s == nil || s.store == nil || entry == nil {
		return 0, nil, errors.New("store unavailable")
	}
	nodes, err := s.activeTenantNodes(ctx, entry.TenantID)
	if err != nil {
		return 0, nil, err
	}
	nodes = filterNodesForBlockProposalServerGroup(nodes, entry)
	selected := selectCanaryNodesByServerGroup(nodes, s.blockCanaryNodesPerServerGroup())
	groups := make([]string, 0, len(selected))
	dispatched := 0
	for _, group := range sortedCanaryGroups(selected) {
		groups = append(groups, group)
		for _, node := range selected[group] {
			n, err := s.dispatchBlockProposalToNode(ctx, entry, entityActionID, node.ID)
			if err != nil {
				return dispatched, groups, err
			}
			dispatched += n
		}
	}
	return dispatched, groups, nil
}

func (s *Server) expireIPBlocklistEntries(ctx context.Context, now time.Time, limit int) (int, error) {
	store, ok := s.store.(ipBlockExpiryStore)
	if !ok {
		return 0, nil
	}
	entries, err := store.ListExpiredIPBlocklistEntries(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	expired := 0
	for i := range entries {
		entry := entries[i]
		if strings.EqualFold(entry.Status, "proposed") {
			if _, err := store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "expired", nil, "proposal expired before approval"); err != nil {
				return expired, err
			}
			expired++
			continue
		}
		if !entry.EntityActionID.Valid {
			if _, err := store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "expired", nil, "expired without enforcement action link"); err != nil {
				return expired, err
			}
			expired++
			continue
		}
		if enforcementWantsFirewall(entry.Enforcement) {
			if _, err := s.queueFirewallRemovalForBlockEntry(ctx, &entry, entry.EntityActionID.UUID); err != nil {
				_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, err.Error())
				return expired, err
			}
		}
		if enforcementWantsWebserver(entry.Enforcement) {
			if _, err := s.refreshWebserverBlocklistsForExpiredEntry(ctx, &entry, now); err != nil {
				_, _ = store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, err.Error())
				return expired, err
			}
		}
		if _, err := store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "expired", nil, "ttl expired; removal dispatched"); err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func (s *Server) queueFirewallRemovalForBlockEntry(ctx context.Context, entry *storage.IPBlocklistEntry, entityActionID uuid.UUID) (int, error) {
	if s == nil || s.store == nil || entry == nil {
		return 0, errors.New("firewall removal unavailable")
	}
	store, ok := s.store.(nodeFirewallRemovalStore)
	if !ok {
		return 0, errors.New("firewall removal store unavailable")
	}
	rules, err := store.ListNodeFirewallRulesForEntityAction(ctx, entityActionID)
	if err != nil {
		return 0, fmt.Errorf("list firewall rules for block entry: %w", err)
	}
	queued := 0
	for _, rule := range rules {
		if strings.EqualFold(rule.Status, "removed") {
			continue
		}
		payload := firewallJobPayload{
			NodeFirewallRuleID: rule.ID.String(),
			NodeID:             rule.NodeID.String(),
			EntityActionID:     entityActionID.String(),
			Action:             "block",
			Direction:          rule.Direction,
			Source:             stringPtrValue(rule.Source),
			Dest:               stringPtrValue(rule.Dest),
			Port:               intPtrValue(rule.Port),
			Protocol:           stringPtrValue(rule.Protocol),
			Tag:                rule.Tag,
			Reason:             strings.TrimSpace("ttl expired: " + entry.Reason),
		}
		payloadBytes, _ := json.Marshal(payload)
		job := &storage.Job{
			TenantID: entry.TenantID,
			Type:     JobTypeFirewallRuleDelete,
			Status:   storage.JobStatusQueued,
			Payload:  payloadBytes,
		}
		created, err := s.store.CreateJob(ctx, job, nil)
		if err != nil {
			return queued, fmt.Errorf("create firewall removal job: %w", err)
		}
		if err := store.QueueNodeFirewallRuleRemoval(ctx, rule.ID, created.ID); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func enforcementWantsFirewall(enforcement string) bool {
	enforcement = strings.ToLower(strings.TrimSpace(enforcement))
	switch enforcement {
	case "", "firewall", "host", "host_firewall", "both", "combined", "firewall+webserver", "webserver+firewall":
		return true
	default:
		return false
	}
}

func enforcementWantsWebserver(enforcement string) bool {
	enforcement = strings.ToLower(strings.TrimSpace(enforcement))
	switch enforcement {
	case "webserver", "web", "http", "both", "combined", "firewall+webserver", "webserver+firewall":
		return true
	default:
		return false
	}
}

func (s *Server) blockProposalCanaryEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Remediation.BlockCanaryEnabled
}

func (s *Server) blockCanaryNodesPerServerGroup() int {
	if s == nil || s.cfg == nil || s.cfg.Remediation.BlockCanaryNodesPerServerGroup <= 0 {
		return 1
	}
	return s.cfg.Remediation.BlockCanaryNodesPerServerGroup
}

func (s *Server) activeTenantNodes(ctx context.Context, tenantID uuid.UUID) ([]storage.Node, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("store unavailable")
	}
	var out []storage.Node
	for offset := 0; ; offset += 500 {
		nodes, _, err := s.store.ListNodes(ctx, tenantID, "", 500, offset)
		if err != nil {
			return nil, fmt.Errorf("list tenant nodes: %w", err)
		}
		for _, node := range nodes {
			if node.State == storage.NodeStateActive {
				out = append(out, node)
			}
		}
		if len(nodes) < 500 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		gi := nodeServerGroup(out[i])
		gj := nodeServerGroup(out[j])
		if gi != gj {
			return gi < gj
		}
		if out[i].Hostname != out[j].Hostname {
			return out[i].Hostname < out[j].Hostname
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}

func selectCanaryNodesByServerGroup(nodes []storage.Node, perGroup int) map[string][]storage.Node {
	if perGroup <= 0 {
		perGroup = 1
	}
	selected := map[string][]storage.Node{}
	for _, node := range nodes {
		group := nodeServerGroup(node)
		if len(selected[group]) >= perGroup {
			continue
		}
		selected[group] = append(selected[group], node)
	}
	return selected
}

func sortedCanaryGroups(nodes map[string][]storage.Node) []string {
	groups := make([]string, 0, len(nodes))
	for group := range nodes {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}

func nodeServerGroup(node storage.Node) string {
	for _, key := range []string{"server_group", "server_group_id", "serverGroup", "group", "cluster.name", "cluster"} {
		if v := labelValueString(node.Labels, key); v != "" {
			return v
		}
	}
	return "ungrouped"
}

func labelValueString(labels map[string]any, key string) string {
	if labels == nil {
		return ""
	}
	v, ok := labels[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func blockProposalMatchesNodeServerGroup(entry *storage.IPBlocklistEntry, node storage.Node) bool {
	if entry == nil || strings.TrimSpace(entry.ServerGroup) == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(entry.ServerGroup), nodeServerGroup(node))
}

func filterNodesForBlockProposalServerGroup(nodes []storage.Node, entry *storage.IPBlocklistEntry) []storage.Node {
	if entry == nil || strings.TrimSpace(entry.ServerGroup) == "" {
		return nodes
	}
	out := make([]storage.Node, 0, len(nodes))
	for _, node := range nodes {
		if blockProposalMatchesNodeServerGroup(entry, node) {
			out = append(out, node)
		}
	}
	return out
}

func (s *Server) blockProposalCanaryFailureReason(ctx context.Context, entry *storage.IPBlocklistEntry) string {
	if s == nil || s.store == nil || entry == nil {
		return "canary state unavailable"
	}
	if entry.EntityActionID.Valid {
		if store, ok := s.store.(nodeFirewallRemovalStore); ok {
			rules, err := store.ListNodeFirewallRulesForEntityAction(ctx, entry.EntityActionID.UUID)
			if err != nil {
				return "canary firewall state unavailable: " + err.Error()
			}
			for _, rule := range rules {
				if strings.EqualFold(rule.Status, "failed") {
					msg := strings.TrimSpace(stringPtrValue(rule.Error))
					if msg == "" {
						msg = "firewall enforcement failed"
					}
					return fmt.Sprintf("canary failed on node %s: %s", rule.NodeID, msg)
				}
			}
		}
	}
	if store, ok := s.store.(webserverBlockEntryActionStore); ok {
		actions, err := store.ListWebserverConfigActionsForBlockEntry(ctx, entry.TenantID, entry.ID)
		if err != nil {
			return "canary webserver state unavailable: " + err.Error()
		}
		for _, action := range actions {
			if strings.EqualFold(action.Status, "failed") {
				msg := ""
				if action.ErrorMessage.Valid {
					msg = strings.TrimSpace(action.ErrorMessage.String)
				}
				if msg == "" {
					msg = "webserver enforcement failed"
				}
				return fmt.Sprintf("canary failed on node %s: %s", action.NodeID, msg)
			}
			if reason := s.openEnforcementCircuitReason(ctx, entry.TenantID, webserverEnforcementCircuitRuleID(action.NodeID)); reason != "" {
				return fmt.Sprintf("canary circuit breaker open on node %s: %s", action.NodeID, reason)
			}
		}
	}
	return ""
}

func (s *Server) blockProposalDispatchedNodeIDs(ctx context.Context, entry *storage.IPBlocklistEntry) map[uuid.UUID]struct{} {
	out := map[uuid.UUID]struct{}{}
	if s == nil || s.store == nil || entry == nil {
		return out
	}
	if entry.EntityActionID.Valid {
		if store, ok := s.store.(nodeFirewallRemovalStore); ok {
			if rules, err := store.ListNodeFirewallRulesForEntityAction(ctx, entry.EntityActionID.UUID); err == nil {
				for _, rule := range rules {
					out[rule.NodeID] = struct{}{}
				}
			}
		}
	}
	if store, ok := s.store.(webserverBlockEntryActionStore); ok {
		if actions, err := store.ListWebserverConfigActionsForBlockEntry(ctx, entry.TenantID, entry.ID); err == nil {
			for _, action := range actions {
				out[action.NodeID] = struct{}{}
			}
		}
	}
	return out
}

type blockProposalEnforcementCounters struct {
	total    int
	applied  int
	pending  int
	failed   int
	errorMsg string
}

func (s *Server) refreshBlockProposalEnforcementStatusByEntityAction(ctx context.Context, entityActionID uuid.UUID) {
	if s == nil || s.store == nil || entityActionID == uuid.Nil {
		return
	}
	store, ok := s.store.(ipBlockProposalEntityActionStore)
	if !ok {
		return
	}
	entry, err := store.GetIPBlocklistEntryByEntityAction(ctx, entityActionID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("load block proposal by entity action", zap.String("entity_action_id", entityActionID.String()), zap.Error(err))
		}
		return
	}
	s.refreshBlockProposalEnforcementStatus(ctx, entry)
}

func (s *Server) refreshBlockProposalEnforcementStatus(ctx context.Context, entry *storage.IPBlocklistEntry) {
	if s == nil || s.store == nil || entry == nil || entry.ID == uuid.Nil || blockProposalStatusTerminal(entry.Status) {
		return
	}
	counters := blockProposalEnforcementCounters{}
	if enforcementWantsFirewall(entry.Enforcement) && entry.EntityActionID.Valid {
		s.collectFirewallBlockProposalCounters(ctx, entry.EntityActionID.UUID, &counters)
	}
	if enforcementWantsWebserver(entry.Enforcement) {
		s.collectWebserverBlockProposalCounters(ctx, entry, &counters)
	}
	if counters.total == 0 {
		return
	}
	store, ok := s.store.(interface {
		UpdateIPBlocklistEntryStatus(context.Context, uuid.UUID, string, *uuid.UUID, string) (*storage.IPBlocklistEntry, error)
	})
	if !ok {
		return
	}
	if counters.failed > 0 {
		errMsg := counters.errorMsg
		if strings.TrimSpace(errMsg) == "" {
			errMsg = "one or more enforcement layers failed"
		}
		if _, err := store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "failed", nil, errMsg); err != nil && s.logger != nil {
			s.logger.Warn("mark block proposal failed", zap.String("block_entry_id", entry.ID.String()), zap.Error(err))
		}
		return
	}
	if strings.EqualFold(entry.Status, "canary") {
		return
	}
	if counters.pending == 0 && counters.applied > 0 {
		if _, err := store.UpdateIPBlocklistEntryStatus(ctx, entry.ID, "active", nil, ""); err != nil && s.logger != nil {
			s.logger.Warn("mark block proposal active", zap.String("block_entry_id", entry.ID.String()), zap.Error(err))
		}
	}
}

func (s *Server) collectFirewallBlockProposalCounters(ctx context.Context, entityActionID uuid.UUID, counters *blockProposalEnforcementCounters) {
	if counters == nil || entityActionID == uuid.Nil {
		return
	}
	store, ok := s.store.(nodeFirewallRemovalStore)
	if !ok {
		return
	}
	rules, err := store.ListNodeFirewallRulesForEntityAction(ctx, entityActionID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("list firewall rules for block proposal status", zap.String("entity_action_id", entityActionID.String()), zap.Error(err))
		}
		return
	}
	for _, rule := range rules {
		counters.total++
		switch strings.ToLower(strings.TrimSpace(rule.Status)) {
		case "applied":
			counters.applied++
		case "failed":
			counters.failed++
			if counters.errorMsg == "" {
				counters.errorMsg = stringPtrValue(rule.Error)
			}
		case "removed":
			counters.applied++
		default:
			counters.pending++
		}
	}
}

func (s *Server) collectWebserverBlockProposalCounters(ctx context.Context, entry *storage.IPBlocklistEntry, counters *blockProposalEnforcementCounters) {
	if counters == nil || entry == nil {
		return
	}
	store, ok := s.store.(webserverBlockEntryActionStore)
	if !ok {
		return
	}
	actions, err := store.ListWebserverConfigActionsForBlockEntry(ctx, entry.TenantID, entry.ID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("list webserver actions for block proposal status", zap.String("block_entry_id", entry.ID.String()), zap.Error(err))
		}
		return
	}
	for _, action := range actions {
		counters.total++
		switch strings.ToLower(strings.TrimSpace(action.Status)) {
		case "succeeded":
			counters.applied++
		case "failed", "cancelled":
			counters.failed++
			if counters.errorMsg == "" && action.ErrorMessage.Valid {
				counters.errorMsg = action.ErrorMessage.String
			}
		default:
			counters.pending++
		}
	}
}

func blockProposalStatusTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "expired", "removed", "denied", "rejected", "rolled_back":
		return true
	default:
		return false
	}
}

func (s *Server) blockProposalSafetyViolation(ctx context.Context, tenantID uuid.UUID, serverGroup string) (int, string) {
	if reason := s.openEnforcementCircuitReason(ctx, tenantID, networkBlockCircuitRuleID); reason != "" {
		return http.StatusConflict, "network block enforcement circuit breaker is open: " + reason
	}
	limit := s.maxBlockChangesPerHour()
	if limit <= 0 || s == nil || s.store == nil {
		return 0, ""
	}
	store, ok := s.store.(ipBlockSafetyStore)
	if !ok {
		return 0, ""
	}
	since := time.Now().UTC().Add(-time.Hour)
	serverGroup = strings.TrimSpace(serverGroup)
	if globalLimit := s.maxGlobalBlockChangesPerHour(); globalLimit > 0 {
		count, err := store.CountRecentIPBlocklistEntriesGlobal(ctx, since)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("count recent global ip blocklist entries", zap.Error(err))
			}
			return http.StatusServiceUnavailable, "block proposal safety check unavailable"
		}
		if count >= globalLimit {
			return http.StatusTooManyRequests, fmt.Sprintf("global block proposal rate limit exceeded: %d changes in the last hour (limit %d)", count, globalLimit)
		}
	}
	if serverGroup != "" {
		groupLimit := s.maxBlockChangesPerServerGroupPerHour()
		if groupLimit > 0 {
			count, err := store.CountRecentIPBlocklistEntriesForServerGroup(ctx, tenantID, serverGroup, since)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("count recent server-group ip blocklist entries", zap.String("server_group", serverGroup), zap.Error(err))
				}
				return http.StatusServiceUnavailable, "block proposal safety check unavailable"
			}
			if count >= groupLimit {
				return http.StatusTooManyRequests, fmt.Sprintf("server group %q block proposal rate limit exceeded: %d changes in the last hour (limit %d)", serverGroup, count, groupLimit)
			}
		}
	}
	count, err := store.CountRecentIPBlocklistEntries(ctx, tenantID, since)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("count recent ip blocklist entries", zap.Error(err))
		}
		return http.StatusServiceUnavailable, "block proposal safety check unavailable"
	}
	if count >= limit {
		return http.StatusTooManyRequests, fmt.Sprintf("block proposal rate limit exceeded: %d changes in the last hour (limit %d)", count, limit)
	}
	return 0, ""
}

func (s *Server) maxBlockChangesPerHour() int {
	if s == nil || s.cfg == nil || s.cfg.Remediation.MaxBlockChangesPerHour <= 0 {
		return 100
	}
	return s.cfg.Remediation.MaxBlockChangesPerHour
}

func (s *Server) maxBlockChangesPerServerGroupPerHour() int {
	if s == nil || s.cfg == nil || s.cfg.Remediation.MaxBlockChangesPerServerGroupPerHour <= 0 {
		return 25
	}
	return s.cfg.Remediation.MaxBlockChangesPerServerGroupPerHour
}

func (s *Server) maxGlobalBlockChangesPerHour() int {
	if s == nil || s.cfg == nil || s.cfg.Remediation.MaxGlobalBlockChangesPerHour <= 0 {
		return 1000
	}
	return s.cfg.Remediation.MaxGlobalBlockChangesPerHour
}

func (s *Server) openEnforcementCircuitReason(ctx context.Context, tenantID uuid.UUID, ruleID string) string {
	if s == nil || s.store == nil || tenantID == uuid.Nil || strings.TrimSpace(ruleID) == "" {
		return ""
	}
	state, err := s.store.GetCircuitBreakerState(ctx, tenantID, ruleID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("get enforcement circuit breaker", zap.String("rule_id", ruleID), zap.Error(err))
		}
		return ""
	}
	if state == nil || state.AckedAt != nil {
		return ""
	}
	reason := strings.TrimSpace(state.TrippedReason)
	if reason == "" {
		reason = "unacknowledged enforcement safety breaker"
	}
	return reason
}

func (s *Server) protectedIPBlockReason(ctx context.Context, tenantID uuid.UUID, target string) string {
	targetNet, ok := parseIPOrCIDRNet(target)
	if s == nil || !ok {
		return ""
	}
	if s.store != nil {
		if filters, err := s.store.GetTenantEventFilters(ctx, tenantID); err == nil && filters != nil {
			if match := firstOverlappingCIDR(targetNet, filters.AllowlistCIDRs); match != "" {
				return "tenant allowlist " + match
			}
		}
	}
	if ib := s.investigateBackend(); ib != nil {
		if assets, err := ib.ListAssetCIDRs(ctx, tenantID); err == nil {
			for _, asset := range assets {
				assetCopy := asset
				if cidrNetsOverlap(targetNet, &assetCopy) {
					return "tenant asset CIDR " + asset.String()
				}
			}
		}
	}
	return ""
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func intPtrValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func ttlSecondsFromBlockEntry(entry *storage.IPBlocklistEntry) *int {
	if entry == nil || !entry.ExpiresAt.Valid {
		return nil
	}
	ttl := int(time.Until(entry.ExpiresAt.Time).Seconds())
	if ttl <= 0 {
		return nil
	}
	return &ttl
}

func parseIPBehaviorTenantSince(w http.ResponseWriter, r *http.Request, defaultWindow time.Duration) (uuid.UUID, time.Time, bool) {
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return uuid.Nil, time.Time{}, false
	}
	since := time.Now().UTC().Add(-defaultWindow)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return uuid.Nil, time.Time{}, false
		}
		since = parsed
	}
	return tenantID, since, true
}

func statusMapString(in map[int]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[strconv.Itoa(k)] = v
	}
	return out
}

func ipBehaviorSignalCount(details map[string]any, boolKeys, countKeys []string) int {
	for _, key := range countKeys {
		if n := int64FromServerAny(details[key]); n > 0 {
			return int(n)
		}
	}
	for _, key := range boolKeys {
		if ipBehaviorDetailBool(details, key) {
			return 1
		}
	}
	return 0
}

func ipBehaviorMaxDetailInt(details map[string]any, keys ...string) int {
	max := 0
	for _, key := range keys {
		if n := int(int64FromServerAny(details[key])); n > max {
			max = n
		}
	}
	return max
}

func ipBehaviorDetailBool(details map[string]any, key string) bool {
	if details == nil {
		return false
	}
	switch v := details[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "yes") || strings.TrimSpace(v) == "1"
	case int, int64, int32, float64, float32, json.Number:
		return int64FromServerAny(v) > 0
	default:
		return false
	}
}

func appendIPBehaviorEvidenceRef(b *ipBehaviorBucket, ev *IngestedEvent, path string, status int) {
	if b == nil || ev == nil {
		return
	}
	ref := map[string]any{
		"type":      firstNonEmptyIPBehavior(ev.Type, "web.request"),
		"timestamp": ev.TS.UTC().Format(time.RFC3339),
	}
	if ev.CorrelationID != "" {
		ref["correlation_id"] = ev.CorrelationID
	}
	if ev.DedupKey != "" {
		ref["dedup_key"] = ev.DedupKey
	}
	if ev.ProcessName != "" {
		ref["process"] = ev.ProcessName
	}
	if path != "" {
		ref["path"] = path
	}
	if status > 0 {
		ref["status_code"] = status
	}
	for _, key := range []string{
		"source_file", "parser_profile", "request_id", "response_request_id", "traceparent",
		"connection_id", "process_event_id", "file_event_id", "db_query_id", "patch_state_id", "exposure_id",
	} {
		if value := detailsString(ev.Details, key, ""); value != "" {
			ref[key] = value
		}
	}
	if len(ref) <= 2 && path == "" && status == 0 {
		return
	}
	b.evidenceRefs = appendIPBehaviorEvidenceRefs(b.evidenceRefs, []map[string]any{ref}, 16)
}

func appendIPBehaviorEvidenceRefs(existing []map[string]any, refs []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(refs) == 0 {
		return existing
	}
	seen := map[string]struct{}{}
	for _, ref := range existing {
		seen[ipBehaviorEvidenceRefKey(ref)] = struct{}{}
	}
	out := existing
	for _, ref := range refs {
		if len(out) >= limit {
			break
		}
		key := ipBehaviorEvidenceRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		copied := make(map[string]any, len(ref))
		for k, v := range ref {
			copied[k] = v
		}
		out = append(out, copied)
	}
	return out
}

func ipBehaviorEvidenceRefKey(ref map[string]any) string {
	if len(ref) == 0 {
		return ""
	}
	parts := []string{
		fmt.Sprint(ref["type"]),
		fmt.Sprint(ref["correlation_id"]),
		fmt.Sprint(ref["dedup_key"]),
		fmt.Sprint(ref["timestamp"]),
		fmt.Sprint(ref["path"]),
		fmt.Sprint(ref["status_code"]),
	}
	return strings.Join(parts, "|")
}

func ipBehaviorHostCorrelationSignalCount(b *ipBehaviorBucket) int {
	if b == nil {
		return 0
	}
	count := 0
	if b.processSpawns > 0 {
		count++
	}
	if b.fileWrites > 0 {
		count++
	}
	if b.dbBulkQueries > 0 {
		count++
	}
	if b.outboundConnections > 0 {
		count++
	}
	return count
}

func ipBehaviorHostCorrelationLabels(b *ipBehaviorBucket) []string {
	if b == nil {
		return nil
	}
	labels := []string{}
	if b.processSpawns > 0 {
		labels = append(labels, fmt.Sprintf("%d process spawn(s)", b.processSpawns))
	}
	if b.fileWrites > 0 {
		labels = append(labels, fmt.Sprintf("%d file write(s)", b.fileWrites))
	}
	if b.dbBulkQueries > 0 {
		labels = append(labels, fmt.Sprintf("%d DB bulk query signal(s)", b.dbBulkQueries))
	}
	if b.outboundConnections > 0 {
		labels = append(labels, fmt.Sprintf("%d outbound connection(s)", b.outboundConnections))
	}
	return labels
}

func ipBehaviorHostCorrelationEvidence(b *ipBehaviorBucket) map[string]any {
	if ipBehaviorHostCorrelationSignalCount(b) == 0 {
		return nil
	}
	return map[string]any{
		"process_spawns":       b.processSpawns,
		"file_writes":          b.fileWrites,
		"db_bulk_queries":      b.dbBulkQueries,
		"outbound_connections": b.outboundConnections,
		"signal_count":         ipBehaviorHostCorrelationSignalCount(b),
	}
}

func ipBehaviorWebshellCorrelation(b *ipBehaviorBucket) bool {
	return b != nil && b.fileWrites > 0 && b.processSpawns > 0 && b.outboundConnections > 0
}

func bucketDistributedSourceCount(b *ipBehaviorBucket) int {
	if b == nil {
		return 0
	}
	if b.uniqueSourceIPs > 0 {
		return b.uniqueSourceIPs
	}
	if b.srcIP != "" {
		return 1
	}
	return 0
}

func ipBehaviorTopPaths(paths map[string]int, limit int) []map[string]any {
	if len(paths) == 0 || limit <= 0 {
		return nil
	}
	type pathCount struct {
		path  string
		count int
	}
	rows := make([]pathCount, 0, len(paths))
	for path, count := range paths {
		rows = append(rows, pathCount{path: path, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].path < rows[j].path
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"path": row.path, "count": row.count})
	}
	return out
}

func suspiciousPathHits(paths map[string]int) int {
	total := 0
	for p, n := range paths {
		if isSuspiciousIPBehaviorPath(p) {
			total += n
		}
	}
	return total
}

func bucketSuspiciousPathHits(b *ipBehaviorBucket) int {
	if b == nil {
		return 0
	}
	if b.sensitiveHits > 0 {
		return b.sensitiveHits
	}
	return suspiciousPathHits(b.paths)
}

func bucketUniquePathCount(b *ipBehaviorBucket) int {
	if b == nil {
		return 0
	}
	if b.uniquePaths > 0 {
		return b.uniquePaths
	}
	return len(b.paths)
}

func isSuspiciousIPBehaviorPath(path string) bool {
	lp := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.Contains(lp, "admin"),
		strings.Contains(lp, "login"),
		strings.Contains(lp, "wp-login"),
		strings.Contains(lp, "api/auth"),
		strings.Contains(lp, "upload"),
		strings.Contains(lp, "export"),
		strings.Contains(lp, "download"):
		return true
	default:
		return false
	}
}

func firstNonEmptyIPBehavior(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func validIPOrCIDR(value string) bool {
	_, ok := parseIPOrCIDRNet(value)
	return ok
}

func exactCIDRForIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32"
	}
	return ip.String() + "/128"
}

func asnBlockProposalReason(baseReason, asn string, source storage.IPBehaviorASNSource) string {
	reason := strings.TrimSpace(baseReason)
	if reason == "" {
		reason = "behavioral ASN containment"
	}
	parts := []string{
		fmt.Sprintf("ASN %s: %s", strings.TrimSpace(asn), reason),
		fmt.Sprintf("source_ip=%s", strings.TrimSpace(source.SourceIP)),
		fmt.Sprintf("requests=%d", source.RequestCount),
		fmt.Sprintf("bytes_out=%d", source.BytesOut),
	}
	if len(source.Countries) > 0 {
		parts = append(parts, "countries="+strings.Join(source.Countries, ","))
	}
	if len(source.Apps) > 0 {
		parts = append(parts, "apps="+strings.Join(source.Apps, ","))
	}
	if len(source.ServerGroups) > 0 {
		parts = append(parts, "server_groups="+strings.Join(source.ServerGroups, ","))
	}
	return strings.Join(parts, "; ")
}

func parseIPOrCIDRNet(value string) (*net.IPNet, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	if ip := net.ParseIP(value); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, true
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, true
	}
	_, n, err := net.ParseCIDR(value)
	return n, err == nil
}

func firstOverlappingCIDR(target *net.IPNet, cidrs []string) string {
	for _, raw := range cidrs {
		other, ok := parseIPOrCIDRNet(raw)
		if !ok {
			continue
		}
		if cidrNetsOverlap(target, other) {
			return strings.TrimSpace(raw)
		}
	}
	return ""
}

func cidrNetsOverlap(a, b *net.IPNet) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Contains(b.IP) || b.Contains(a.IP)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ipBehaviorDimensionKey(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned = append(cleaned, cleanIPBehaviorDim(part))
	}
	return strings.Join(cleaned, "|")
}

func cleanIPBehaviorDim(value string) string {
	return strings.TrimSpace(value)
}

func baselineSampleCount(m map[string]any) int64 {
	return int64FromServerAny(m["sample_count"])
}

func nestedFloat(m map[string]any, key, child string) float64 {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case map[string]any:
		return floatFromAny(typed[child])
	case map[string]float64:
		return typed[child]
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return 0
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			return 0
		}
		return floatFromAny(out[child])
	}
}

func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func int64FromServerAny(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func ratioInt(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func anyInt64Slice(v any) []int64 {
	switch typed := v.(type) {
	case []int64:
		return typed
	case []int:
		out := make([]int64, 0, len(typed))
		for _, n := range typed {
			out = append(out, int64(n))
		}
		return out
	case []any:
		out := make([]int64, 0, len(typed))
		for _, n := range typed {
			out = append(out, int64FromServerAny(n))
		}
		return out
	default:
		return nil
	}
}

func int64SliceContains(values []int64, needle int64) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func ipBehaviorBaselineEvidence(baselines []storage.BehavioralBaseline) []map[string]any {
	out := make([]map[string]any, 0, len(baselines))
	for _, b := range baselines {
		out = append(out, map[string]any{
			"signal_type":  b.SignalType,
			"dimension":    b.Dimension,
			"window_days":  b.WindowDays,
			"sample_count": baselineSampleCount(b.Baseline),
			"computed_at":  b.ComputedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}
