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
			r.log.Warn("behavioral rollup tenant", zap.String("tenant_id", t.ID.String()), zap.Error(err))
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
