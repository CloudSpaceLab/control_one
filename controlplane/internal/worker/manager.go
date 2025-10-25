package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// Task represents a unit of background work.
type Task struct {
	Name string
	Job  func(context.Context) error
}

var (
	errNotStarted = errors.New("worker manager not started")
	errStopped    = errors.New("worker manager stopped")
	errQueueFull  = errors.New("worker queue full")
)

// Manager coordinates background worker goroutines and task queueing.
type Manager struct {
	log *zap.Logger
	cfg config.WorkerConfig

	mu       sync.RWMutex
	started  bool
	queue    chan Task
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closeOnce sync.Once
}

// New constructs a Manager with the provided configuration.
func New(log *zap.Logger, cfg config.WorkerConfig) *Manager {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}

	return &Manager{
		log: log.Named("worker"),
		cfg: cfg,
	}
}

// Start launches worker goroutines.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.queue = make(chan Task, m.cfg.QueueSize)
	m.ctx, m.cancel = context.WithCancel(ctx)

	for i := 0; i < m.cfg.Concurrency; i++ {
		m.wg.Add(1)
		go m.runWorker(i)
	}

	m.started = true
	m.log.Info("worker manager started",
		zap.Int("concurrency", m.cfg.Concurrency),
		zap.Int("queue_size", cap(m.queue)),
	)
	return nil
}

// Stop signals workers to exit and waits for completion.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	queue := m.queue
	m.started = false
	m.mu.Unlock()

	cancel()
	m.closeOnce.Do(func() {
		close(queue)
	})
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		m.log.Info("worker manager stopped")
		return nil
	}
}

// Enqueue schedules a task for asynchronous execution.
func (m *Manager) Enqueue(task Task) error {
	if task.Job == nil {
		return errors.New("task job cannot be nil")
	}

	m.mu.RLock()
	if !m.started {
		m.mu.RUnlock()
		return errNotStarted
	}
	ctx := m.ctx
	queue := m.queue
	m.mu.RUnlock()

	select {
	case <-ctx.Done():
		return errStopped
	case queue <- task:
		return nil
	default:
		return errQueueFull
	}
}

func (m *Manager) runWorker(id int) {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case task, ok := <-m.queue:
			if !ok {
				return
			}
			m.executeTask(task, id)
		}
	}
}

func (m *Manager) executeTask(task Task, workerID int) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	defer cancel()

	err := task.Job(ctx)
	if err != nil {
		m.log.Warn("task failed",
			zap.String("task", task.Name),
			zap.Int("worker_id", workerID),
			zap.Error(err),
		)
		return
	}

	m.log.Debug("task completed",
		zap.String("task", task.Name),
		zap.Int("worker_id", workerID),
	)
}
