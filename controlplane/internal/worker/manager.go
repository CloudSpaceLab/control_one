package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	DurableJob   DurableJobRef
	MaxAttempts  int
	RetryBackoff time.Duration
}

// DurableJobRef identifies a persisted job that can be rehydrated after a
// process restart instead of relying on an in-memory closure.
type DurableJobRef struct {
	ID   string `json:"job_id,omitempty"`
	Type string `json:"job_type,omitempty"`
}

// Valid reports whether the task carries enough metadata for a durable handler.
func (r DurableJobRef) Valid() bool {
	return strings.TrimSpace(r.ID) != "" && strings.TrimSpace(r.Type) != ""
}

// DurableJobHandler executes a persisted job reference.
type DurableJobHandler func(context.Context, DurableJobRef) error

// Status describes the worker backend state for health checks and diagnostics.
type Status struct {
	Backend    string `json:"backend"`
	Started    bool   `json:"started"`
	QueueDepth int    `json:"queue_depth"`
	Active     int    `json:"active"`
	LastError  string `json:"last_error,omitempty"`
}

func (m *Manager) startMemory(ctx context.Context) {
	m.queue = make(chan Task, m.cfg.QueueSize)
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.useAsynq = false
	m.recordQueueDepth(metricsBackendMemory, 0)
	m.setLastError(nil)

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
	inspector := asynq.NewInspector(opt)
	m.asynqClient = client
	m.asynqServer = server
	m.asynqInspector = inspector
	m.useAsynq = true
	m.queue = nil
	m.ctx, m.cancel = context.WithCancel(ctx)

	mux := asynq.NewServeMux()
	mux.HandleFunc(asynqTaskType, m.handleAsynqTask)
	if err := server.Start(mux); err != nil {
		_ = m.asynqClient.Close()
		m.asynqClient = nil
		m.asynqServer = nil
		_ = m.asynqInspector.Close()
		m.asynqInspector = nil
		metricsRecordBackendState(metricsBackendAsynq, false)
		return fmt.Errorf("start asynq server: %w", err)
	}

	metricsRecordBackendState(metricsBackendMemory, false)
	metricsRecordBackendState(metricsBackendAsynq, true)
	m.recordQueueDepth(metricsBackendAsynq, 0)
	m.setLastError(nil)
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
	if m.asynqInspector != nil {
		if err := m.asynqInspector.Close(); err != nil {
			m.log.Warn("asynq inspector close", zap.Error(err))
		}
	}

	m.tasks.Range(func(key, _ any) bool {
		m.tasks.Delete(key)
		return true
	})
	m.useAsynq = false
	m.asynqClient = nil
	m.asynqServer = nil
	m.asynqInspector = nil
	metricsRecordBackendState(metricsBackendAsynq, false)
	m.recordQueueDepth(metricsBackendAsynq, 0)
	m.setLastError(nil)
	m.log.Info("stopAsynq: cleanup complete")
	return nil
}

func (m *Manager) enqueueAsynq(task Task, processAt time.Time) error {
	if m.asynqClient == nil {
		return errors.New("asynq client not initialized")
	}
	if strings.TrimSpace(task.Name) == "" {
		return errors.New("task name required for asynq backend")
	}
	if !task.DurableJob.Valid() {
		if task.Job == nil {
			return errors.New("task job cannot be nil")
		}
		m.tasks.Store(task.Name, task.Job)
	}
	payload := asynqTaskPayload{Name: task.Name}
	if task.DurableJob.Valid() {
		payload.JobID = strings.TrimSpace(task.DurableJob.ID)
		payload.JobType = strings.TrimSpace(task.DurableJob.Type)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal asynq payload: %w", err)
	}
	asynqTask := asynq.NewTask(asynqTaskType, payloadBytes)
	maxAttempts := m.effectiveMaxAttempts(task)
	var opts []asynq.Option
	if maxAttempts > 0 {
		opts = append(opts, asynq.MaxRetry(maxAttempts-1))
	}
	if !processAt.IsZero() {
		opts = append(opts, asynq.ProcessAt(processAt))
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
	m.adjustActive(1)
	defer m.adjustActive(-1)

	var payload asynqTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		outcome = metricsStatusError
		return fmt.Errorf("decode asynq payload: %w", err)
	}
	if strings.TrimSpace(payload.JobID) != "" || strings.TrimSpace(payload.JobType) != "" {
		ref := DurableJobRef{ID: payload.JobID, Type: payload.JobType}
		if !ref.Valid() {
			outcome = metricsStatusError
			return fmt.Errorf("durable job payload requires job_id and job_type")
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		if err := m.executeDurableJob(ctx, ref); err != nil {
			outcome = metricsStatusFailure
			return err
		}
		return nil
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

func (m *Manager) executeDurableJob(ctx context.Context, ref DurableJobRef) error {
	ref.ID = strings.TrimSpace(ref.ID)
	ref.Type = strings.TrimSpace(ref.Type)
	if !ref.Valid() {
		return errors.New("durable job reference requires id and type")
	}
	value, ok := m.durableJobs.Load(ref.Type)
	if !ok {
		value, ok = m.durableJobs.Load("*")
	}
	if !ok {
		return fmt.Errorf("durable job handler not registered for %s", ref.Type)
	}
	handler, ok := value.(DurableJobHandler)
	if !ok {
		return fmt.Errorf("invalid durable job handler for %s", ref.Type)
	}
	return handler(ctx, ref)
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

	useAsynq       bool
	asynqClient    *asynq.Client
	asynqServer    *asynq.Server
	asynqInspector *asynq.Inspector
	tasks          sync.Map
	durableJobs    sync.Map

	statusMu         sync.RWMutex
	statusQueueDepth int
	statusActive     int
	statusError      string
}

const asynqTaskType = "control_one:execute"
const asynqQueueDefault = "default"

type asynqTaskPayload struct {
	Name    string `json:"name"`
	JobID   string `json:"job_id,omitempty"`
	JobType string `json:"job_type,omitempty"`
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

// RegisterDurableJobHandler registers an executor for persisted jobs. Use "*"
// as jobType to provide a default handler for all durable job references.
func (m *Manager) RegisterDurableJobHandler(jobType string, handler DurableJobHandler) {
	jobType = strings.TrimSpace(jobType)
	if jobType == "" || handler == nil {
		return
	}
	m.durableJobs.Store(jobType, handler)
}

// Status returns a snapshot of the worker manager state.
func (m *Manager) Status() Status {
	m.mu.RLock()
	started := m.started
	useAsynq := m.useAsynq
	queue := m.queue
	m.mu.RUnlock()

	status := Status{
		Backend: "memory",
		Started: started,
	}
	if useAsynq {
		status.Backend = "asynq"
		m.refreshAsynqQueueDepth()
	} else if started && queue != nil {
		m.recordQueueDepth(metricsBackendMemory, len(queue))
	}

	m.statusMu.RLock()
	status.QueueDepth = m.statusQueueDepth
	status.Active = m.statusActive
	status.LastError = m.statusError
	m.statusMu.RUnlock()
	return status
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

// Enqueue schedules a task for asynchronous execution as soon as a worker is
// available. Kept for back-compat; callers that want a deferred start time
// should use EnqueueAt.
func (m *Manager) Enqueue(task Task) error {
	return m.enqueue(task, time.Time{})
}

// EnqueueAt schedules a task to run no earlier than `processAt`. For the
// in-memory backend the task is pushed onto the queue immediately but its
// handler sleeps until processAt (capped at the manager's stop signal). For
// the asynq backend the call wraps asynq.Client.Enqueue with asynq.ProcessAt(t)
// so the task sits in the scheduled set server-side.
//
// A zero `processAt` is treated as "run immediately" — equivalent to Enqueue.
func (m *Manager) EnqueueAt(task Task, processAt time.Time) error {
	return m.enqueue(task, processAt)
}

func (m *Manager) enqueue(task Task, processAt time.Time) error {
	if task.Job == nil && !task.DurableJob.Valid() {
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
		if err := m.enqueueAsynq(task, processAt); err != nil {
			metricsRecordEnqueueResult(backendLabel, metricsStatusFailure)
			return err
		}
		metricsRecordEnqueueResult(backendLabel, metricsStatusSuccess)
		return nil
	}
	if task.Job == nil {
		metricsRecordEnqueueResult(backendLabel, metricsStatusFailure)
		return errors.New("task job cannot be nil for memory backend")
	}

	// Memory backend: wrap the job so the worker waits until processAt before
	// invoking it. A zero processAt leaves the inner job untouched.
	queueTask := task
	if !processAt.IsZero() {
		inner := task.Job
		mgrCtx := m.ctx
		queueTask.Job = func(ctx context.Context) error {
			delay := time.Until(processAt)
			if delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-mgrCtx.Done():
					return errStopped
				case <-timer.C:
				}
			}
			return inner(ctx)
		}
	}

	select {
	case <-m.ctx.Done():
		metricsRecordEnqueueResult(backendLabel, metricsStatusError)
		return errStopped
	case queue <- queueTask:
		metricsRecordEnqueueResult(backendLabel, metricsStatusSuccess)
		m.recordQueueDepth(backendLabel, len(queue))
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
			m.recordQueueDepth(metricsBackendMemory, len(m.queue))
			m.executeTask(task, id)
		}
	}
}

func (m *Manager) executeTask(task Task, workerID int) {
	finish := metricsTrackWorkerTask(metricsBackendMemory)
	outcome := metricsStatusSuccess
	m.adjustActive(1)
	defer func() {
		m.adjustActive(-1)
		finish(outcome)
	}()

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

func (m *Manager) recordQueueDepth(backend string, depth int) {
	metricsRecordQueueDepth(backend, depth)
	m.statusMu.Lock()
	m.statusQueueDepth = depth
	m.statusMu.Unlock()
}

func (m *Manager) setLastError(err error) {
	m.statusMu.Lock()
	if err != nil {
		m.statusError = err.Error()
	} else {
		m.statusError = ""
	}
	m.statusMu.Unlock()
}

func (m *Manager) adjustActive(delta int) {
	m.statusMu.Lock()
	m.statusActive += delta
	if m.statusActive < 0 {
		m.statusActive = 0
	}
	m.statusMu.Unlock()
}

func (m *Manager) refreshAsynqQueueDepth() {
	inspector := m.asynqInspector
	if inspector == nil {
		m.recordQueueDepth(metricsBackendAsynq, 0)
		return
	}
	stats, err := inspector.GetQueueInfo(asynqQueueDefault)
	if err != nil {
		if isAsynqQueueMissing(err) {
			m.setLastError(nil)
			m.recordQueueDepth(metricsBackendAsynq, 0)
			return
		}
		m.setLastError(err)
		return
	}
	m.setLastError(nil)
	depth := stats.Pending + stats.Active
	m.recordQueueDepth(metricsBackendAsynq, depth)
}

func isAsynqQueueMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, asynq.ErrQueueNotFound) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "queue "+strconv.Quote(asynqQueueDefault)+" does not exist") ||
		strings.Contains(msg, "queue not found")
}
