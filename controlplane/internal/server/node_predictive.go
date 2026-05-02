package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Job type constants for the predictive server downtime pipeline (Use
// Case 5). Both jobs are scheduler-driven, hourly. Validate hooks are
// registered in job_types.go init().
const (
	// JobTypeHealthBaselines runs hourly EWMA over telemetry to keep
	// behavioral_baselines fresh for each (signal_type='health.<metric>',
	// dimension='<metric_name>'). Fast α=0.3 for CPU/RAM/IO; slow α=0.05
	// for SMART metrics.
	JobTypeHealthBaselines = "health.compute_baselines"
	// JobTypeHealthPredict runs hourly. Computes a 0..100 score per node
	// from telemetry + baselines, applies hysteresis + cold-start gating,
	// upserts node_health_scores, and (when criteria are met) opens a
	// HealthIncident with dedupe key (node_id, primary_component).
	JobTypeHealthPredict = "health.predict"
)

// Risk band thresholds — see migration 0084 doc comment.
const (
	healthScoreLowThreshold      = 75 // >=75 → low
	healthScoreMediumThreshold   = 50 // 50..74 → medium
	healthScoreHighThreshold     = 25 // 25..49 → high; <25 → critical
	healthCalibrationMinSamples  = 24 // require ≥24 samples per metric
	healthIncidentDedupeCooldown = 6 * time.Hour
	healthHysteresisDuration     = 30 * time.Minute
)

// healthSignal names the telemetry metric that drives a single penalty.
// We treat absence as "no penalty" — agent rollouts deliver these
// metrics over time. A node with no telemetry remains at score=100.
type healthSignal struct {
	metricName string
	penalty    int
	// trigger reports whether the latest sample warrants the penalty.
	// For sustained checks (iowait_pct > 30) it inspects the recent
	// window, not just one sample.
	trigger func(samples []storage.TelemetryMetric) bool
	// primaryKey is the components-map key for incident dedupe.
	primaryKey string
}

// healthSignalsCatalog returns the deterministic list of signals
// considered by the predict job. Order matters only for stable
// tie-breaking when two signals have identical penalties.
func healthSignalsCatalog() []healthSignal {
	return []healthSignal{
		{
			metricName: "smart.reallocated_sector_count",
			penalty:    35,
			primaryKey: "smart_reallocated",
			trigger:    triggerLatestPositive,
		},
		{
			metricName: "smart.uncorrectable_errors",
			penalty:    30,
			primaryKey: "smart_uncorrectable",
			trigger:    triggerLatestPositive,
		},
		{
			metricName: "host.oom_events_count",
			penalty:    25,
			primaryKey: "oom_events",
			trigger:    triggerLastHourPositive,
		},
		{
			metricName: "host.swap_used_pct",
			penalty:    15,
			primaryKey: "swap_used",
			trigger:    triggerLatestAbove(80),
		},
		{
			metricName: "host.iowait_pct",
			penalty:    15,
			primaryKey: "iowait_sustained",
			trigger:    triggerSustainedAbove(30, 6),
		},
		{
			metricName: "host.load_avg_ratio",
			penalty:    10,
			primaryKey: "load_avg_high",
			trigger:    triggerLatestAbove(2),
		},
		{
			metricName: "net.packet_loss_pct",
			penalty:    10,
			primaryKey: "packet_loss",
			trigger:    triggerLatestAbove(5),
		},
		// icmp_latency p99 baseline-relative is handled separately by
		// scoreLatencyVsBaseline so it can compare against the EWMA
		// baseline — it doesn't fit the static-threshold trigger shape.
	}
}

func triggerLatestPositive(samples []storage.TelemetryMetric) bool {
	if len(samples) == 0 {
		return false
	}
	return samples[0].MetricValue > 0
}

func triggerLastHourPositive(samples []storage.TelemetryMetric) bool {
	cutoff := time.Now().Add(-time.Hour)
	for _, s := range samples {
		if s.Timestamp.After(cutoff) && s.MetricValue > 0 {
			return true
		}
	}
	return false
}

func triggerLatestAbove(threshold float64) func([]storage.TelemetryMetric) bool {
	return func(samples []storage.TelemetryMetric) bool {
		if len(samples) == 0 {
			return false
		}
		return samples[0].MetricValue > threshold
	}
}

// triggerSustainedAbove fires only when at least minSamples consecutive
// recent samples are above the threshold — avoids one-shot CPU spikes
// from tripping the iowait penalty.
func triggerSustainedAbove(threshold float64, minSamples int) func([]storage.TelemetryMetric) bool {
	return func(samples []storage.TelemetryMetric) bool {
		if len(samples) < minSamples {
			return false
		}
		// samples are ordered DESC by timestamp; check the most recent
		// minSamples in a row.
		for i := 0; i < minSamples; i++ {
			if samples[i].MetricValue <= threshold {
				return false
			}
		}
		return true
	}
}

// ---------- HTTP handlers ----------

type nodeHealthScoreResponse struct {
	NodeID     string         `json:"node_id"`
	Score      int            `json:"score"`
	RiskLevel  string         `json:"risk_level"`
	Components map[string]any `json:"components"`
	ComputedAt *string        `json:"computed_at,omitempty"`
}

func newNodeHealthScoreResponse(s storage.NodeHealthScore) nodeHealthScoreResponse {
	out := nodeHealthScoreResponse{
		NodeID:     s.NodeID.String(),
		Score:      s.Score,
		RiskLevel:  s.RiskLevel,
		Components: s.Components,
	}
	if out.Components == nil {
		out.Components = map[string]any{}
	}
	if !s.ComputedAt.IsZero() {
		t := s.ComputedAt.UTC().Format(time.RFC3339)
		out.ComputedAt = &t
	}
	return out
}

// handleNodeHealth implements GET /api/v1/nodes/:id/health.
// Routed from handleNodeResource — the segments[1]=="health" dispatch is
// added there. Returns the latest score row, or a synthesized
// "calibrating" response when no row exists yet (node hasn't been
// scored — the predict job hasn't run for it).
func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	score, err := s.store.GetNodeHealthScore(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node health score", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if score == nil {
		// Synthesize a clean calibrating response — no penalties have
		// been applied because no scoring run has happened yet.
		writeJSON(w, http.StatusOK, nodeHealthScoreResponse{
			NodeID:    nodeID.String(),
			Score:     100,
			RiskLevel: "calibrating",
			Components: map[string]any{
				"calibrating_samples": 0,
				"reason":              "no scoring run yet",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, newNodeHealthScoreResponse(*score))
}

type atRiskNodeResponse struct {
	NodeID     string         `json:"node_id"`
	TenantID   string         `json:"tenant_id"`
	Hostname   string         `json:"hostname"`
	Score      int            `json:"score"`
	RiskLevel  string         `json:"risk_level"`
	Components map[string]any `json:"components"`
	ComputedAt string         `json:"computed_at"`
}

type atRiskFleetResponse struct {
	Data       []atRiskNodeResponse `json:"data"`
	TotalCount int                  `json:"total_count"`
	Critical   int                  `json:"critical"`
	High       int                  `json:"high"`
}

// handleAtRiskFleet implements GET /api/v1/health/at-risk?tenant_id=.
// Reports every node currently in HIGH or CRIT band. tenant_id is
// optional — when omitted, the roll-up spans every tenant the caller
// can see (RBAC is role-based here; cross-tenant scoping is enforced
// upstream by viewer access being granted per-tenant).
func (s *Server) handleAtRiskFleet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = id
	}
	rows, err := s.store.ListAtRiskNodes(r.Context(), tenantID, healthScoreMediumThreshold-1)
	if err != nil {
		s.logger.Error("list at-risk nodes", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := atRiskFleetResponse{Data: make([]atRiskNodeResponse, 0, len(rows))}
	for _, row := range rows {
		comp := row.Components
		if comp == nil {
			comp = map[string]any{}
		}
		resp.Data = append(resp.Data, atRiskNodeResponse{
			NodeID:     row.NodeID.String(),
			TenantID:   row.TenantID.String(),
			Hostname:   row.Hostname,
			Score:      row.Score,
			RiskLevel:  row.RiskLevel,
			Components: comp,
			ComputedAt: row.ComputedAt.UTC().Format(time.RFC3339),
		})
		switch row.RiskLevel {
		case "critical":
			resp.Critical++
		case "high":
			resp.High++
		}
	}
	resp.TotalCount = len(resp.Data)
	writeJSON(w, http.StatusOK, resp)
}

// ---------- Job handlers ----------

// handleHealthBaselinesJob is the hourly EWMA pass. Per (tenant, node,
// metric), it folds the latest hour of telemetry into an EWMA stored
// under signal_type='health.<metric>', dimension='<metric_name>'. Two
// alphas: fast (0.3) for volatile signals (CPU/RAM/IO), slow (0.05) for
// SMART (we don't want a single bad day to mask the long-term trend).
//
// The job is a roll-up — it does not care about completion: scheduler
// calls it on a fixed interval. The structured outcome is a
// {succeeded[], failed[]} slice in metadata so multi-tenant runs are
// observable.
func (s *Server) handleHealthBaselinesJob(ctx context.Context, job *storage.Job) error {
	if s == nil || s.store == nil {
		return errors.New("store unavailable")
	}
	if job == nil {
		return errors.New("nil job")
	}
	tenants, _, err := s.store.ListTenants(ctx, "", 1000, 0)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	type result struct {
		TenantID string `json:"tenant_id"`
		Updated  int    `json:"updated"`
		Error    string `json:"error,omitempty"`
	}
	var succeeded, failed []result
	signals := healthSignalsCatalog()
	now := time.Now()
	for _, t := range tenants {
		count, err := s.computeBaselinesForTenant(ctx, t.ID, signals, now)
		if err != nil {
			failed = append(failed, result{TenantID: t.ID.String(), Error: err.Error()})
			s.logger.Warn("baselines tenant failed", zap.String("tenant_id", t.ID.String()), zap.Error(err))
			continue
		}
		succeeded = append(succeeded, result{TenantID: t.ID.String(), Updated: count})
	}
	s.logger.Info("health baselines run",
		zap.Int("tenants_succeeded", len(succeeded)),
		zap.Int("tenants_failed", len(failed)),
	)
	return nil
}

// computeBaselinesForTenant scans nodes for a tenant and updates the
// EWMA baseline for each (node, metric). Returns the count of upserts.
func (s *Server) computeBaselinesForTenant(
	ctx context.Context,
	tenantID uuid.UUID,
	signals []healthSignal,
	now time.Time,
) (int, error) {
	nodes, _, err := s.store.ListNodes(ctx, tenantID, "", 1000, 0)
	if err != nil {
		return 0, fmt.Errorf("list nodes: %w", err)
	}
	since := now.Add(-time.Hour)
	count := 0
	for i := range nodes {
		node := nodes[i]
		nodeID := node.ID
		// Pull existing baselines for the node so we can fold into them.
		existingBaselines, _ := s.store.ListBehavioralBaselines(ctx, tenantID, nodeID)
		existingMap := make(map[string]storage.BehavioralBaseline, len(existingBaselines))
		for _, b := range existingBaselines {
			if strings.HasPrefix(b.SignalType, "health.") {
				existingMap[b.Dimension] = b
			}
		}
		// icmp_latency baseline lives alongside the static-threshold
		// signals; include it explicitly so we keep p99 fresh.
		metricNames := append([]string{"net.icmp_latency_p99"}, func() []string {
			out := make([]string, 0, len(signals))
			for _, sig := range signals {
				out = append(out, sig.metricName)
			}
			return out
		}()...)
		for _, metric := range metricNames {
			samples, _, err := s.store.ListTelemetryMetrics(ctx, storage.TelemetryMetricFilter{
				TenantID:   tenantID,
				NodeID:     nodeID,
				MetricName: metric,
				Since:      &since,
			}, 256, 0)
			if err != nil {
				s.logger.Warn("list telemetry for baseline", zap.Error(err), zap.String("metric", metric))
				continue
			}
			if len(samples) == 0 {
				continue
			}
			alpha := alphaForMetric(metric)
			prev := 0.0
			samplesSeen := 0
			if b, ok := existingMap[metric]; ok {
				if v, ok := b.Baseline["ewma"].(float64); ok {
					prev = v
				}
				if v, ok := b.Baseline["samples"].(float64); ok {
					samplesSeen = int(v)
				}
			}
			ewma := prev
			for _, sample := range samples {
				if samplesSeen == 0 {
					ewma = sample.MetricValue
				} else {
					ewma = alpha*sample.MetricValue + (1-alpha)*ewma
				}
				samplesSeen++
			}
			baseline := map[string]any{
				"ewma":    ewma,
				"alpha":   alpha,
				"samples": samplesSeen,
				"updated": now.UTC().Format(time.RFC3339),
			}
			id := nodeID
			if err := s.store.UpsertBehavioralBaseline(ctx, tenantID, &id, "health."+metric, metric, baseline, 30); err != nil {
				return count, fmt.Errorf("upsert baseline %s: %w", metric, err)
			}
			count++
		}
	}
	return count, nil
}

// alphaForMetric selects the EWMA smoothing factor. Slow (0.05) for
// long-trend SMART counters; fast (0.3) for volatile host/net signals.
func alphaForMetric(metric string) float64 {
	if strings.HasPrefix(metric, "smart.") {
		return 0.05
	}
	return 0.3
}

// handleHealthPredictJob is the hourly scoring pass. Walks every node,
// computes a 0..100 score against the static thresholds + the
// baseline-relative ICMP latency check, and upserts into
// node_health_scores. Hysteresis-gated incident creation lives here.
func (s *Server) handleHealthPredictJob(ctx context.Context, job *storage.Job) error {
	if s == nil || s.store == nil {
		return errors.New("store unavailable")
	}
	if job == nil {
		return errors.New("nil job")
	}
	tenants, _, err := s.store.ListTenants(ctx, "", 1000, 0)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	type result struct {
		TenantID string `json:"tenant_id"`
		Scored   int    `json:"scored"`
		Error    string `json:"error,omitempty"`
	}
	var succeeded, failed []result
	signals := healthSignalsCatalog()
	for _, t := range tenants {
		scored, err := s.scorePredictForTenant(ctx, t.ID, signals)
		if err != nil {
			failed = append(failed, result{TenantID: t.ID.String(), Error: err.Error()})
			s.logger.Warn("predict tenant failed", zap.String("tenant_id", t.ID.String()), zap.Error(err))
			continue
		}
		succeeded = append(succeeded, result{TenantID: t.ID.String(), Scored: scored})
	}
	s.logger.Info("health predict run",
		zap.Int("tenants_succeeded", len(succeeded)),
		zap.Int("tenants_failed", len(failed)),
	)
	return nil
}

// scorePredictForTenant computes scores for every node in the tenant.
func (s *Server) scorePredictForTenant(ctx context.Context, tenantID uuid.UUID, signals []healthSignal) (int, error) {
	nodes, _, err := s.store.ListNodes(ctx, tenantID, "", 1000, 0)
	if err != nil {
		return 0, fmt.Errorf("list nodes: %w", err)
	}
	scored := 0
	since := time.Now().Add(-2 * time.Hour)
	for i := range nodes {
		node := nodes[i]
		nodeID := node.ID
		// Pull samples per signal and the baselines (for icmp).
		samplesByMetric := make(map[string][]storage.TelemetryMetric, len(signals)+1)
		minSamples := math.MaxInt
		for _, sig := range signals {
			samples, _, err := s.store.ListTelemetryMetrics(ctx, storage.TelemetryMetricFilter{
				TenantID:   tenantID,
				NodeID:     nodeID,
				MetricName: sig.metricName,
				Since:      &since,
			}, 256, 0)
			if err != nil {
				continue
			}
			samplesByMetric[sig.metricName] = samples
			if len(samples) < minSamples {
				minSamples = len(samples)
			}
		}
		// icmp latency
		latencySamples, _, _ := s.store.ListTelemetryMetrics(ctx, storage.TelemetryMetricFilter{
			TenantID:   tenantID,
			NodeID:     nodeID,
			MetricName: "net.icmp_latency_p99",
			Since:      &since,
		}, 256, 0)
		samplesByMetric["net.icmp_latency_p99"] = latencySamples
		if len(latencySamples) < minSamples {
			minSamples = len(latencySamples)
		}
		// Cold-start gate.
		if minSamples == math.MaxInt {
			minSamples = 0
		}
		if minSamples < healthCalibrationMinSamples {
			components := map[string]any{
				"calibrating_samples": minSamples,
				"reason":              fmt.Sprintf("need %d samples per metric", healthCalibrationMinSamples),
			}
			if _, err := s.store.UpsertNodeHealthScore(ctx, storage.UpsertNodeHealthScoreParams{
				NodeID:     nodeID,
				Score:      100,
				RiskLevel:  "calibrating",
				Components: components,
			}); err != nil {
				return scored, fmt.Errorf("upsert calibrating score: %w", err)
			}
			scored++
			continue
		}
		// Static-threshold signals.
		score := 100
		breakdown := map[string]int{}
		for _, sig := range signals {
			samples := samplesByMetric[sig.metricName]
			if sig.trigger != nil && sig.trigger(samples) {
				score -= sig.penalty
				breakdown[sig.primaryKey] = -sig.penalty
			}
		}
		// Latency-vs-baseline.
		if latencyPenalty, key := s.scoreLatencyVsBaseline(ctx, tenantID, nodeID, latencySamples); latencyPenalty > 0 {
			score -= latencyPenalty
			breakdown[key] = -latencyPenalty
		}
		if score < 0 {
			score = 0
		}
		risk := riskLevelForScore(score)
		primary := largestPenalty(breakdown)
		components := map[string]any{
			"breakdown":         breakdown,
			"primary_component": primary,
		}
		if _, err := s.store.UpsertNodeHealthScore(ctx, storage.UpsertNodeHealthScoreParams{
			NodeID:     nodeID,
			Score:      score,
			RiskLevel:  risk,
			Components: components,
		}); err != nil {
			return scored, fmt.Errorf("upsert score: %w", err)
		}
		scored++
		// Hysteresis-gated incident: only when score crosses 50 AND
		// remains below for healthHysteresisDuration. We approximate
		// "crossed" by checking that the prior stored score was >=50;
		// "remained" by checking computed_at delta is at least
		// hysteresis. Final dedupe + cooldown is enforced via
		// (node_id, primary_component) DedupKey on the incident insert.
		if score < healthScoreMediumThreshold && primary != "" {
			s.maybeOpenHealthIncident(ctx, node.TenantID, nodeID, score, risk, primary, breakdown)
		}
	}
	return scored, nil
}

// scoreLatencyVsBaseline penalizes ICMP p99 latency exceeding 3× the
// EWMA baseline. Returns 0 when no baseline exists yet (cold start).
func (s *Server) scoreLatencyVsBaseline(
	ctx context.Context,
	tenantID, nodeID uuid.UUID,
	samples []storage.TelemetryMetric,
) (int, string) {
	if len(samples) == 0 {
		return 0, ""
	}
	baselines, err := s.store.ListBehavioralBaselines(ctx, tenantID, nodeID)
	if err != nil {
		return 0, ""
	}
	var baselineEWMA float64
	for _, b := range baselines {
		if b.SignalType == "health.net.icmp_latency_p99" {
			if v, ok := b.Baseline["ewma"].(float64); ok {
				baselineEWMA = v
				break
			}
		}
	}
	if baselineEWMA <= 0 {
		return 0, ""
	}
	if samples[0].MetricValue > baselineEWMA*3 {
		return 10, "icmp_latency_spike"
	}
	return 0, ""
}

// riskLevelForScore maps the numeric score onto the named bands.
func riskLevelForScore(score int) string {
	switch {
	case score >= healthScoreLowThreshold:
		return "low"
	case score >= healthScoreMediumThreshold:
		return "medium"
	case score >= healthScoreHighThreshold:
		return "high"
	default:
		return "critical"
	}
}

// largestPenalty returns the breakdown key with the most negative
// penalty (i.e. largest absolute contribution). Stable on ties via key
// alpha-sort.
func largestPenalty(breakdown map[string]int) string {
	if len(breakdown) == 0 {
		return ""
	}
	keys := make([]string, 0, len(breakdown))
	for k := range breakdown {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	primary := keys[0]
	for _, k := range keys {
		if breakdown[k] < breakdown[primary] {
			primary = k
		}
	}
	return primary
}

// maybeOpenHealthIncident enforces hysteresis (score below threshold
// for ≥ healthHysteresisDuration) plus dedupe via (node_id,
// primary_component) cooldown. Manual close — we never auto-resolve on
// recovery.
func (s *Server) maybeOpenHealthIncident(
	ctx context.Context,
	tenantID uuid.UUID,
	nodeID uuid.UUID,
	score int,
	risk string,
	primary string,
	breakdown map[string]int,
) {
	if s.store == nil {
		return
	}
	// Hysteresis: require that the previous stored score was ALSO
	// below threshold and that computed_at is older than the hysteresis
	// window. This approximates "remained <50 for 30 min" without a
	// dedicated state-transition log: if the prior reading was already
	// below 50 and was taken ≥30min ago, the score has been low across
	// at least one full period.
	prev, err := s.store.GetNodeHealthScore(ctx, nodeID)
	if err != nil || prev == nil {
		return
	}
	if prev.Score >= healthScoreMediumThreshold {
		// First time crossing; let the next run open the incident.
		return
	}
	if time.Since(prev.ComputedAt) < healthHysteresisDuration {
		return
	}
	dedupKey := fmt.Sprintf("health:%s:%s", nodeID.String(), primary)
	details := map[string]any{
		"score":             score,
		"risk_level":        risk,
		"primary_component": primary,
		"breakdown":         breakdown,
	}
	nodeIDCopy := nodeID
	if _, err := s.store.CreateHealthIncident(ctx, storage.CreateHealthIncidentParams{
		TenantID:     tenantID,
		NodeID:       &nodeIDCopy,
		IncidentType: "predictive.downtime",
		Severity:     mapRiskToSeverity(risk),
		Details:      details,
		DedupKey:     dedupKey,
	}); err != nil {
		// Cooldown is enforced via the unique-on-dedup-key constraint;
		// duplicate insert returns an error which we treat as "already
		// dispatched" and swallow.
		s.logger.Debug("health incident dedupe or insert error",
			zap.Error(err),
			zap.String("dedup_key", dedupKey),
		)
	}
}

// mapRiskToSeverity surfaces the band as a HealthIncident severity
// string. critical→critical, high→high, anything else→medium (we
// shouldn't ever open an incident for medium/low here, but be safe).
func mapRiskToSeverity(risk string) string {
	switch risk {
	case "critical":
		return "critical"
	case "high":
		return "high"
	default:
		return "medium"
	}
}

// decodeHealthJobPayload — the predict + baselines jobs take no payload
// today. Reserved for future "scope to tenant_id" or "scope to node_id"
// invocations.
func decodeHealthJobPayload(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return map[string]any{}, nil
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid health job payload: %w", err)
	}
	return p, nil
}
