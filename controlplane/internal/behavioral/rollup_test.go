package behavioral

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type fakeStore struct {
	tenants []storage.Tenant
	stats   []storage.PortObservationStats
	upserts []upsertCall
	listErr error
}

type upsertCall struct {
	tenantID   uuid.UUID
	signalType string
	dimension  string
	baseline   map[string]any
}

func (f *fakeStore) ListTenants(_ context.Context, _ string, _, _ int) ([]storage.Tenant, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.tenants, len(f.tenants), nil
}

func (f *fakeStore) AggregatePortObservations(_ context.Context, _ uuid.UUID, _ time.Time) ([]storage.PortObservationStats, error) {
	return f.stats, nil
}

func (f *fakeStore) UpsertBehavioralBaseline(_ context.Context, t uuid.UUID, _ *uuid.UUID, st, dim string, b map[string]any, _ int) error {
	f.upserts = append(f.upserts, upsertCall{tenantID: t, signalType: st, dimension: dim, baseline: b})
	return nil
}

func TestRollupUpsertsPerPort(t *testing.T) {
	tenant := uuid.New()
	fs := &fakeStore{
		tenants: []storage.Tenant{{ID: tenant}},
		stats: []storage.PortObservationStats{
			{Port: 22, Protocol: "tcp", State: "open", Count: 95},
			{Port: 22, Protocol: "tcp", State: "closed", Count: 5},
			{Port: 80, Protocol: "tcp", State: "open", Count: 50},
		},
	}
	r := NewRollup(fs, nil, time.Minute, 30)
	r.RunOnce(context.Background())

	if len(fs.upserts) != 2 {
		t.Fatalf("want 2 upserts, got %d", len(fs.upserts))
	}
	for _, c := range fs.upserts {
		if c.signalType != "port_state" {
			t.Fatalf("unexpected signal %s", c.signalType)
		}
		if c.baseline["dominant_state"] != "open" {
			t.Fatalf("unexpected dominant %v", c.baseline["dominant_state"])
		}
	}
}

func TestRollupSkipsEmpty(t *testing.T) {
	fs := &fakeStore{tenants: []storage.Tenant{{ID: uuid.New()}}}
	r := NewRollup(fs, nil, time.Minute, 30)
	r.RunOnce(context.Background())
	if len(fs.upserts) != 0 {
		t.Fatal("should not upsert with no stats")
	}
}

func TestDominantState(t *testing.T) {
	state, ratio := dominantState(map[string]int{"open": 9, "closed": 1})
	if state != "open" {
		t.Fatalf("want open, got %s", state)
	}
	if ratio < 0.899 || ratio > 0.901 {
		t.Fatalf("unexpected ratio %f", ratio)
	}
}
