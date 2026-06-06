package server

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestHealthSchedulerEnqueuesDurableJobTask(t *testing.T) {
	store := &fakeStore{
		jobs:   make(map[uuid.UUID]*storage.Job),
		events: make(map[uuid.UUID][]storage.JobEvent),
	}
	queue := &spyQueue{}
	srv := &Server{
		logger: zap.NewNop(),
		cfg:    &config.Config{},
		store:  store,
		worker: queue,
	}
	hs := NewHealthScheduler(srv)

	hs.enqueueJob(context.Background(), JobTypeHealthBaselines)

	if len(queue.tasks) != 1 {
		t.Fatalf("expected one health task, got %d", len(queue.tasks))
	}
	task := queue.tasks[0]
	if !task.DurableJob.Valid() || task.DurableJob.Type != JobTypeHealthBaselines {
		t.Fatalf("health task missing durable job ref: %#v", task.DurableJob)
	}
	if task.Job == nil {
		t.Fatal("health task should retain an in-process job for memory workers")
	}
}
