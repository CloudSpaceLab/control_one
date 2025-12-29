package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

const defaultRetryBackoff = 5 * time.Second

// Task represents a unit of background work.
type Task struct {
	Name         string
	Job          func(context.Context) error
	MaxAttempts  int
	RetryBackoff time.Duration
}

func (m *Manager) startMemory(ctx context.Context) {
	m.queue = make(chan Task, m.cfg.QueueSize)
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.useAsynq = false

	for i := 0; i < m.cfg.Concurrency; i++ {
		m.wg.Add(1)
		go m.runWorker(i)
	}

	metricsRecordBackendState(metricsBackendAsynq, false)
	metricsRecordBackendState(metricsBackendMemory, true)
	metricsRecordQueueDepth(metricsBackendMemory, 0)
	m.started = true
}

func (m *Manager) startAsynq(ctx context.Context) error {
	redisAddr := strings.TrimSpace(m.cfg.Asynq.RedisAddress)
	if redisAddr == "" {
		return fmt.Errorf("asynq redis_address is required")
	}

	opt := asynq.RedisClientOpt{
		Addr:     redisAddr,
		DB:       m.cfg.Asynq.RedisDB,
		Password: m.cfg.Asynq.RedisPassword,
	}

	client := asynq.NewClient(opt)
	backoff := m.cfg.RetryBackoff
	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}
	delayFunc := asynq.RetryDelayFunc(func(n int, _ error, _ *asynq.Task) time.Duration {
		if n <= 0 {
			n = 1
		}
		return time.Duration(n) * backoff
	})

	server := asynq.NewServer(opt, asynq.Config{
		Concurrency:    m.cfg.Concurrency,
		Queues:         map[string]int{"default": 1},
		RetryDelayFunc: delayFunc,
	})
	m.asynqClient = client
	m.asynqServer = server
	m.useAsynq = true
	m.queue = nil
	m.ctx, m.cancel = context.WithCancel(ctx)

	mux := asynq.NewServeMux()
	mux.HandleFunc(asynqTaskType, m.handleAsynqTask)
	if err := server.Start(mux); err != nil {
		m.asynqClient.Close()
		m.asynqClient = nil
		m.asynqServer = nil
		metricsRecordBackendState(metricsBackendAsynq, false)
		return fmt.Errorf("start asynq server: %w", err)
	}

	metricsRecordBackendState(metricsBackendMemory, false)
	metricsRecordBackendState(metricsBackendAsynq, true)
	m.started = true
	return nil
}

func (m *Manager) stopAsynq(ctx context.Context) error {
	shutdownDone := make(chan struct{})
	if m.asynqServer != nil {
		go func(server *asynq.Server) {
			m.log.Info("stopAsynq: initiating shutdown")
			server.Shutdown()
			m.log.Info("stopAsynq: shutdown returned")
			close(shutdownDone)
		}(m.asynqServer)
	} else {
		close(shutdownDone)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-shutdownDone:
	}

	if m.asynqClient != nil {
		if err := m.asynqClient.Close(); err != nil {
			m.log.Warn("asynq client close", zap.Error(err))
		}
	}

	m.tasks.Range(func(key, _ any) bool {
		m.tasks.Delete(key)
		return true
	})
	m.useAsynq = false
	m.asynqClient = nil
	m.asynqServer = nil
	metricsRecordBackendState(metricsBackendAsynq, false)
	m.log.Info("stopAsynq: cleanup complete")
	return nil
}

func (m *Manager) enqueueAsynq(task Task) error {
	if m.asynqClient == nil {
		return errors.New("asynq client not initialized")
	}
	if strings.TrimSpace(task.Name) == "" {
		return errors.New("task name required for asynq backend")
	}
	m.tasks.Store(task.Name, task.Job)
	payloadBytes, err := json.Marshal(asynqTaskPayload{Name: task.Name})
	if err != nil {
		return fmt.Errorf("marshal asynq payload: %w", err)
	}
	asynqTask := asynq.NewTask(asynqTaskType, payloadBytes)
	maxAttempts := m.effectiveMaxAttempts(task)
	var opts []asynq.Option
	if maxAttempts > 0 {
		opts = append(opts, asynq.MaxRetry(maxAttempts-1))
	}
	if _, err := m.asynqClient.Enqueue(asynqTask, opts...); err != nil {
		m.tasks.Delete(task.Name)
		return fmt.Errorf("enqueue asynq task: %w", err)
	}
	return nil
}

func (m *Manager) handleAsynqTask(ctx context.Context, t *asynq.Task) error {
	finish := metricsTrackWorkerTask(metricsBackendAsynq)
	outcome := metricsStatusSuccess
	defer func() { finish(outcome) }()

	var payload asynqTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		outcome = metricsStatusError
		return fmt.Errorf("decode asynq payload: %w", err)
	}
	value, ok := m.tasks.Load(payload.Name)
	if !ok {
		outcome = metricsStatusError
		return fmt.Errorf("task %s not found", payload.Name)
	}
	job, ok := value.(func(context.Context) error)
	if !ok {
		outcome = metricsStatusError
		return fmt.Errorf("invalid job type for %s", payload.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := job(ctx); err != nil {
		outcome = metricsStatusFailure
		return err
	}
	m.tasks.Delete(payload.Name)
	return nil
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

	mu        sync.RWMutex
	started   bool
	queue     chan Task
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	useAsynq    bool
	asynqClient *asynq.Client
	asynqServer *asynq.Server
	tasks       sync.Map
}

const asynqTaskType = "control_one:execute"

type asynqTaskPayload struct {
	Name string `json:"name"`
}

// New constructs a Manager with the provided configuration.
func New(log *zap.Logger, cfg config.WorkerConfig) *Manager {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = defaultRetryBackoff
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

	backend := strings.ToLower(strings.TrimSpace(m.cfg.Backend))
	if backend == "" {
		backend = "memory"
	}

	if backend == "asynq" || m.cfg.Asynq.Enabled {
		if err := m.startAsynq(ctx); err != nil {
			return err
		}
		m.log.Info("worker manager started", zap.String("backend", "asynq"), zap.Int("concurrency", m.cfg.Concurrency))
		return nil
	}

	m.startMemory(ctx)
	m.log.Info("worker manager started", zap.String("backend", "memory"), zap.Int("concurrency", m.cfg.Concurrency), zap.Int("queue_size", cap(m.queue)))
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
	useAsynq := m.useAsynq
	m.started = false
	m.mu.Unlock()

	cancel()

	if useAsynq {
		if err := m.stopAsynq(ctx); err != nil {
			return err
		}
		return nil
	}

	m.closeOnce.Do(func() {
		if m.queue != nil {
			close(m.queue)
		}
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
		metricsRecordBackendState(metricsBackendMemory, false)
		return nil
	}
}

// Enqueue schedules a task for asynchronous execution.
func (m *Manager) Enqueue(task Task) error {
	if task.Job == nil {
		return errors.New("task job cannot be nil")
	}

	m.mu.RLock()
	started := m.started
	useAsynq := m.useAsynq
	queue := m.queue
	m.mu.RUnlock()

	if !started {
		return errNotStarted
	}

	backendLabel := metricsBackendMemory
	if useAsynq {
		backendLabel = metricsBackendAsynq
	}

	if useAsynq {
		if err := m.enqueueAsynq(task); err != nil {
			metricsRecordEnqueueResult(backendLabel, metricsStatusFailure)
			return err
		}
		metricsRecordEnqueueResult(backendLabel, metricsStatusSuccess)
		return nil
	}

	select {
	case <-m.ctx.Done():
		metricsRecordEnqueueResult(backendLabel, metricsStatusError)
		return errStopped
	case queue <- task:
		metricsRecordEnqueueResult(backendLabel, metricsStatusSuccess)
		metricsRecordQueueDepth(backendLabel, len(queue))
		return nil
	default:
		metricsRecordEnqueueResult(backendLabel, metricsStatusFailure)
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
	finish := metricsTrackWorkerTask(metricsBackendMemory)
	outcome := metricsStatusSuccess
	defer func() { finish(outcome) }()

	maxAttempts := m.effectiveMaxAttempts(task)
	backoff := m.effectiveRetryBackoff(task)

	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
		err := task.Job(ctx)
		cancel()

		if err == nil {
			m.log.Debug("task completed",
				zap.String("task", task.Name),
				zap.Int("worker_id", workerID),
				zap.Int("attempt", attempt),
			)
			return
		}

		outcome = metricsStatusFailure
		m.log.Warn("task failed",
			zap.String("task", task.Name),
			zap.Int("worker_id", workerID),
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxAttempts),
			zap.Error(err),
		)

		if attempt == maxAttempts {
			return
		}

		delay := backoff
		if attempt > 1 {
			delay = time.Duration(attempt) * backoff
		}

		select {
		case <-m.ctx.Done():
			outcome = metricsStatusError
			m.log.Warn("task retry cancelled",
				zap.String("task", task.Name),
				zap.Int("worker_id", workerID),
				zap.Int("attempt", attempt),
			)
			return
		case <-time.After(delay):
		}
	}
}

func (m *Manager) effectiveMaxAttempts(task Task) int {
	if task.MaxAttempts > 0 {
		return task.MaxAttempts
	}
	if m.cfg.MaxAttempts > 0 {
		return m.cfg.MaxAttempts
	}
	return 1
}

func (m *Manager) effectiveRetryBackoff(task Task) time.Duration {
	if task.RetryBackoff > 0 {
		return task.RetryBackoff
	}
	if m.cfg.RetryBackoff > 0 {
		return m.cfg.RetryBackoff
	}
	return defaultRetryBackoff
}
