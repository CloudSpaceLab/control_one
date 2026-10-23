package main

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestCollectorRuntimeStartStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	rt := newCollectorRuntime(ctx, zap.NewNop())
	rt.Register("fileaccess", func(ctx context.Context) {
		started <- struct{}{}
		<-ctx.Done()
	}, func() string { return "test" })

	rt.Start("fileaccess")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("collector did not start")
	}

	snap := rt.Snapshot()
	if len(snap) != 1 || snap[0].State != "running" || snap[0].Backend != "test" {
		t.Fatalf("unexpected running snapshot: %#v", snap)
	}

	rt.Stop("fileaccess", "policy disabled")
	snap = rt.Snapshot()
	if len(snap) != 1 || snap[0].State != "stopped" || snap[0].BackoffReason != "policy disabled" {
		t.Fatalf("unexpected stopped snapshot: %#v", snap)
	}
}

func TestCollectorRuntimeUnavailablePrerequisite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1)
	reason := "no supported backend"
	rt := newCollectorRuntime(ctx, zap.NewNop())
	rt.RegisterWithPrerequisite("fileaccess", func(ctx context.Context) { started <- struct{}{}; <-ctx.Done() }, nil, func() string { return reason })
	rt.Start("fileaccess")
	rt.Start("fileaccess")
	state := rt.Snapshot()[0]
	if state.State != "unsupported" || state.BackoffReason != reason {
		t.Fatalf("state=%+v", state)
	}
	select {
	case <-started:
		t.Fatal("unsupported collector started")
	default:
	}
	reason = ""
	rt.Start("fileaccess")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("collector did not recover when prerequisite became available")
	}
	rt.Stop("fileaccess", "test complete")
}
