// Package behavioral computes baseline statistics from observation tables so
// the recommender + correlation engines can flag anomalies against known-good
// behavior. The rollup runs periodically and writes into behavioral_baselines.
package behavioral

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Store is the narrow slice of storage.Store needed by the rollup. Kept as an
// interface so tests can pass fakes without pulling in the full Store.
type Store interface {
	AggregatePortObservations(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]storage.PortObservationStats, error)
	AggregateIPBehaviorBaselineStats(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]storage.IPBehaviorBaselineStats, error)
	UpsertBehavioralBaseline(ctx context.Context, tenantID uuid.UUID, nodeID *uuid.UUID, signalType, dimension string, baseline map[string]any, windowDays int) error
	ListTenants(ctx context.Context, namePrefix string, limit, offset int) ([]storage.Tenant, int, error)
}

// Rollup recomputes per-tenant baselines on a fixed interval.
type Rollup struct {
	store      Store
	log        *zap.Logger
	interval   time.Duration
	windowDays int
}

// NewRollup returns a ready-to-run rollup. interval defaults to 1h,
// windowDays to 30.
func NewRollup(store Store, log *zap.Logger, interval time.Duration, windowDays int) *Rollup {
	if interval <= 0 {
		interval = time.Hour
	}
	if windowDays <= 0 {
		windowDays = 30
	}
	return &Rollup{store: store, log: log, interval: interval, windowDays: windowDays}
}

// Run ticks every interval until ctx is cancelled. First tick fires immediately.
func (r *Rollup) Run(ctx context.Context) {
	if r == nil || r.store == nil {
		return
	}
	tick := time.NewTicker(r.interval)
	defer tick.Stop()
	r.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce walks every tenant and recomputes baselines for port observations.
// Errors per-tenant are logged, not returned; one tenant's DB failure must
// not stop the rollup for the rest.
func (r *Rollup) RunOnce(ctx context.Context) {
	tenants, _, err := r.store.ListTenants(ctx, "", 1000, 0)
	if err != nil {
		if r.log != nil {
			r.log.Warn("behavioral rollup list tenants", zap.Error(err))
		}
		return
	}
	since := time.Now().UTC().Add(-time.Duration(r.windowDays) * 24 * time.Hour)
	for _, t := range tenants {
		if err := r.rollupPorts(ctx, t.ID, since); err != nil && r.log != nil {
			r.log.Warn("behavioral port rollup tenant", zap.String("tenant_id", t.ID.String()), zap.Error(err))
		}
		if err := r.rollupIPBehavior(ctx, t.ID, since); err != nil && r.log != nil {
			r.log.Warn("behavioral ip rollup tenant", zap.String("tenant_id", t.ID.String()), zap.Error(err))
		}
	}
}

func (r *Rollup) rollupPorts(ctx context.Context, tenantID uuid.UUID, since time.Time) error {
	stats, err := r.store.AggregatePortObservations(ctx, tenantID, since)
	if err != nil {
		return err
	}
	if len(stats) == 0 {
		return nil
	}
	type portKey struct {
		Port     int
		Protocol string
	}
	byPort := map[portKey]map[string]int{}
	totals := map[portKey]int{}
	for _, s := range stats {
		k := portKey{Port: s.Port, Protocol: s.Protocol}
		if byPort[k] == nil {
			byPort[k] = map[string]int{}
		}
		byPort[k][s.State] = s.Count
		totals[k] += s.Count
	}
	for k, states := range byPort {
		dominant, ratio := dominantState(states)
		baseline := map[string]any{
			"port":            k.Port,
			"protocol":        k.Protocol,
			"states":          states,
			"total_samples":   totals[k],
			"dominant_state":  dominant,
			"dominant_ratio":  ratio,
			"computed_at_utc": time.Now().UTC().Format(time.RFC3339),
		}
		dim := k.Protocol + ":" + itoa10(k.Port)
		if err := r.store.UpsertBehavioralBaseline(ctx, tenantID, nil, "port_state", dim, baseline, r.windowDays); err != nil {
			return err
		}
	}
	return nil
}

func (r *Rollup) rollupIPBehavior(ctx context.Context, tenantID uuid.UUID, since time.Time) error {
	stats, err := r.store.AggregateIPBehaviorBaselineStats(ctx, tenantID, since)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, st := range stats {
		status := st.StatusCounts
		authFailures := status["401"] + status["403"]
		serverErrors := status["500"] + status["502"] + status["503"] + status["5xx"]
		authRatio := 0.0
		serverErrorRatio := 0.0
		if st.TotalRequests > 0 {
			authRatio = float64(authFailures) / float64(st.TotalRequests)
			serverErrorRatio = float64(serverErrors) / float64(st.TotalRequests)
		}
		baseline := map[string]any{
			"dimension":        trimIPBehaviorPrefix(st.SignalType),
			"dimension_key":    st.Dimension,
			"sample_count":     st.SampleCount,
			"observed_hours":   st.ObservedHours,
			"total_requests":   st.TotalRequests,
			"request_count":    statsMap(float64(st.RequestMin), st.RequestAvg, st.RequestP50, st.RequestP95, st.RequestP99, float64(st.RequestPeak)),
			"bytes_out":        statsMap(float64(st.BytesMin), st.BytesAvg, st.BytesP50, st.BytesP95, st.BytesP99, float64(st.BytesPeak)),
			"status_counts":    status,
			"auth_fail_ratio":  authRatio,
			"server_err_ratio": serverErrorRatio,
			"seasonality": map[string]any{
				"active_hours":          st.ActiveHours,
				"active_weekdays":       st.ActiveWeekdays,
				"active_weeks_of_month": st.ActiveWeeks,
				"active_months":         st.ActiveMonths,
				"business_calendar_hooks": []string{
					"weekend",
					"public_holiday",
					"maintenance_window",
					"batch_window",
					"month_end",
				},
			},
			"active_hours":              st.ActiveHours,
			"active_weekdays":           st.ActiveWeekdays,
			"active_weeks_of_month":     st.ActiveWeeks,
			"active_months":             st.ActiveMonths,
			"minimum_sample_count":      24,
			"autonomous_action_ready":   st.SampleCount >= 24 && st.TotalRequests >= 100,
			"rolling_window_days":       r.windowDays,
			"supported_rolling_windows": []int{7, 30, 90, 365},
			"first_seen_at":             st.FirstSeenAt.UTC().Format(time.RFC3339),
			"last_seen_at":              st.LastSeenAt.UTC().Format(time.RFC3339),
			"computed_at_utc":           now,
		}
		var nodeID *uuid.UUID
		if st.NodeID.Valid {
			id := st.NodeID.UUID
			nodeID = &id
		}
		if err := r.store.UpsertBehavioralBaseline(ctx, tenantID, nodeID, st.SignalType, st.Dimension, baseline, r.windowDays); err != nil {
			return err
		}
	}
	return nil
}

func statsMap(min, avg, p50, p95, p99, peak float64) map[string]any {
	return map[string]any{
		"min":  min,
		"avg":  avg,
		"p50":  p50,
		"p95":  p95,
		"p99":  p99,
		"peak": peak,
	}
}

func trimIPBehaviorPrefix(signalType string) string {
	const prefix = "ip_behavior."
	if len(signalType) >= len(prefix) && signalType[:len(prefix)] == prefix {
		return signalType[len(prefix):]
	}
	return signalType
}

func dominantState(states map[string]int) (string, float64) {
	total := 0
	for _, n := range states {
		total += n
	}
	if total == 0 {
		return "", 0
	}
	var dom string
	var domCount int
	for s, n := range states {
		if n > domCount {
			dom = s
			domCount = n
		}
	}
	return dom, float64(domCount) / float64(total)
}

func itoa10(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
