package main

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestFilterApplierControlsCollectorLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	collectors := newCollectorRuntime(ctx, zap.NewNop())
	collectors.Register("fileaccess", func(ctx context.Context) {
		started <- struct{}{}
		<-ctx.Done()
	}, nil)
	collectors.Register("dbquery", func(ctx context.Context) {
		<-ctx.Done()
	}, nil)

	apply := makeFilterApplier(zap.NewNop(), collectors, nil, nil, nil)
	apply([]byte(`{"capture_files":true,"capture_db_queries":true}`))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fileaccess collector did not start after capture_files=true")
	}
	if got := collectorStateByName(collectors.Snapshot(), "dbquery"); got != "running" {
		t.Fatalf("dbquery state = %q, want running", got)
	}

	apply([]byte(`{"capture_files":false,"capture_db_queries":false}`))
	if got := collectorStateByName(collectors.Snapshot(), "fileaccess"); got != "stopped" {
		t.Fatalf("fileaccess state = %q, want stopped", got)
	}
	if got := collectorStateByName(collectors.Snapshot(), "dbquery"); got != "stopped" {
		t.Fatalf("dbquery state = %q, want stopped", got)
	}
}

func collectorStateByName(states []collectorStateReport, name string) string {
	for _, state := range states {
		if state.Name == name {
			return state.State
		}
	}
	return ""
}
