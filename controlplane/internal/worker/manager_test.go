package worker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/alicebob/miniredis/v2"
	"go.uber.org/zap/zaptest"
)

func TestManagerProcessesTaskMemory(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cfg := config.WorkerConfig{Concurrency: 1, QueueSize: 2, Backend: "memory"}

	mgr := New(logger, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := mgr.Stop(stopCtx); err != nil {
			t.Fatalf("stop manager: %v", err)
		}
	}()

	done := make(chan struct{})
	task := Task{
		Name: "process-once",
		Job: func(ctx context.Context) error {
			close(done)
			return nil
		},
	}

	if err := mgr.Enqueue(task); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task was not processed")
	}
}

func TestManagerProcessesTaskAsynq(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	logger := zaptest.NewLogger(t)
	cfg := config.WorkerConfig{
		Concurrency: 1,
		Backend:     "asynq",
		Asynq: config.AsynqConfig{
			Enabled:      true,
			RedisAddress: mr.Addr(),
		},
	}
	name := fmt.Sprintf("asynq-%d", time.Now().UnixNano())
	mgr := New(logger, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	defer func() {
		stopDone := make(chan error, 1)
		go func() {
			stopDone <- mgr.Stop(context.Background())
		}()
		sel := time.NewTimer(10 * time.Second)
		defer sel.Stop()
		select {
		case err := <-stopDone:
			if err != nil {
				t.Fatalf("stop manager: %v", err)
			}
		case <-sel.C:
			t.Fatal("stop manager timed out")
		}
	}()

	done := make(chan struct{})
	task := Task{
		Name: name,
		Job: func(ctx context.Context) error {
			close(done)
			return nil
		},
	}

	if err := mgr.Enqueue(task); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("asynq task was not processed")
	}
}

func TestManagerStartAsynqMissingRedis(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := New(logger, config.WorkerConfig{Backend: "asynq", Asynq: config.AsynqConfig{Enabled: true}})

	if err := mgr.Start(context.Background()); err == nil {
		t.Fatal("expected error when redis address missing")
	}
}

func TestManagerEnqueueBeforeStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := New(logger, config.WorkerConfig{Concurrency: 1, QueueSize: 1, Backend: "memory"})

	err := mgr.Enqueue(Task{Name: "pre-start", Job: func(context.Context) error { return nil }})
	if !errors.Is(err, errNotStarted) {
		t.Fatalf("expected errNotStarted, got %v", err)
	}
}

func TestManagerQueueFull(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cfg := config.WorkerConfig{Concurrency: 1, QueueSize: 1, Backend: "memory"}
	mgr := New(logger, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := mgr.Stop(stopCtx); err != nil {
			t.Fatalf("stop manager: %v", err)
		}
	}()

	block := make(chan struct{})
	jobStarted := make(chan struct{})
	secondDone := make(chan struct{})

	// First task blocks the worker so the queue can fill up.
	first := Task{
		Name: "blocker",
		Job: func(ctx context.Context) error {
			close(jobStarted)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-block:
				return nil
			}
		},
	}
	if err := mgr.Enqueue(first); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}

	select {
	case <-jobStarted:
	case <-time.After(time.Second):
		t.Fatal("worker never started first job")
	}

	if err := mgr.Enqueue(Task{Name: "queued", Job: func(ctx context.Context) error { close(secondDone); return nil }}); err != nil {
		t.Fatalf("enqueue second: %v", err)
	}

	err := mgr.Enqueue(Task{Name: "overflow", Job: func(ctx context.Context) error { return nil }})
	if !errors.Is(err, errQueueFull) {
		t.Fatalf("expected errQueueFull, got %v", err)
	}

	close(block)

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second job never completed")
	}
}
