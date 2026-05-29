package logforward

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type Store interface {
	ListTenants(context.Context, string, int, int) ([]storage.Tenant, int, error)
	ListSIEMForwardingDestinations(context.Context, uuid.UUID, string, int, int) ([]storage.SIEMForwardingDestination, int, error)
	GetSIEMForwardingCheckpoint(context.Context, uuid.UUID, uuid.UUID) (*storage.SIEMForwardingCheckpoint, error)
	RecordSIEMForwardingCheckpoint(context.Context, storage.RecordSIEMForwardingCheckpointParams) (*storage.SIEMForwardingCheckpoint, error)
	RecordSIEMForwardingDeliveryAttempt(context.Context, storage.RecordSIEMForwardingDeliveryAttemptParams) (*storage.SIEMForwardingDeliveryAttempt, error)
	ListTelemetryLogsForForwarding(context.Context, uuid.UUID, time.Time, uuid.UUID, int) ([]storage.TelemetryLog, error)
}

type SinkFactory func(SinkConfig) (Sink, error)

type RunnerOptions struct {
	Interval                 time.Duration
	RunTimeout               time.Duration
	InitialLookback          time.Duration
	MaxBatchSize             int
	MaxTenantsPerPass        int
	MaxDestinationsPerTenant int
	SinkFactory              SinkFactory
}

type RunSummary struct {
	Tenants      int
	Destinations int
	Batches      int
	Records      int
	Failures     int
}

type Runner struct {
	store    Store
	resolver CredentialResolver
	log      *zap.Logger
	opts     RunnerOptions
}

func NewRunner(store Store, resolver CredentialResolver, log *zap.Logger, opts RunnerOptions) (*Runner, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.RunTimeout <= 0 {
		opts.RunTimeout = 2 * time.Minute
	}
	if opts.InitialLookback <= 0 {
		opts.InitialLookback = 15 * time.Minute
	}
	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = 500
	}
	if opts.MaxTenantsPerPass <= 0 {
		opts.MaxTenantsPerPass = 500
	}
	if opts.MaxDestinationsPerTenant <= 0 {
		opts.MaxDestinationsPerTenant = 100
	}
	if opts.SinkFactory == nil {
		opts.SinkFactory = NewSinkFromConfig
	}
	return &Runner{store: store, resolver: resolver, log: log, opts: opts}, nil
}

func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, r.opts.RunTimeout)
			summary, err := r.RunOnce(runCtx)
			cancel()
			if err != nil {
				r.log.Warn("SIEM forwarding pass failed", zap.Error(err))
				continue
			}
			if summary.Destinations > 0 || summary.Failures > 0 || summary.Records > 0 {
				r.log.Info("SIEM forwarding pass complete",
					zap.Int("tenants", summary.Tenants),
					zap.Int("destinations", summary.Destinations),
					zap.Int("batches", summary.Batches),
					zap.Int("records", summary.Records),
					zap.Int("failures", summary.Failures),
				)
			}
		}
	}
}

func (r *Runner) RunOnce(ctx context.Context) (RunSummary, error) {
	var summary RunSummary
	tenants, _, err := r.store.ListTenants(ctx, "", r.opts.MaxTenantsPerPass, 0)
	if err != nil {
		return summary, fmt.Errorf("list tenants for SIEM forwarding: %w", err)
	}
	summary.Tenants = len(tenants)
	var errs []error
	for _, tenant := range tenants {
		destinations, _, err := r.store.ListSIEMForwardingDestinations(ctx, tenant.ID, storage.SIEMForwardingDestinationStatusEnabled, r.opts.MaxDestinationsPerTenant, 0)
		if err != nil {
			errs = append(errs, fmt.Errorf("list forwarding destinations for tenant %s: %w", tenant.ID, err))
			summary.Failures++
			continue
		}
		for _, destination := range destinations {
			summary.Destinations++
			batch, err := r.forwardDestination(ctx, destination)
			if err != nil {
				errs = append(errs, fmt.Errorf("forward destination %s: %w", destination.ID, err))
				summary.Failures++
				continue
			}
			if batch > 0 {
				summary.Batches++
				summary.Records += batch
			}
		}
	}
	return summary, errors.Join(errs...)
}

func (r *Runner) forwardDestination(ctx context.Context, destination storage.SIEMForwardingDestination) (int, error) {
	cursor, cursorLogID, err := r.destinationCursor(ctx, destination)
	if err != nil {
		_ = r.recordFailedAttempt(ctx, destination, 0, nil, nil, err)
		return 0, err
	}
	logs, err := r.store.ListTelemetryLogsForForwarding(ctx, destination.TenantID, cursor, cursorLogID, r.opts.MaxBatchSize)
	if err != nil {
		_ = r.recordFailedAttempt(ctx, destination, 0, nil, nil, err)
		return 0, err
	}
	if len(logs) == 0 {
		return 0, nil
	}
	records := make([]LogRecord, 0, len(logs))
	for _, row := range logs {
		records = append(records, logRecordFromTelemetryLog(row))
	}
	batchStart, batchEnd := batchWindow(records)
	sink, err := r.sinkForDestination(ctx, destination)
	if err != nil {
		_ = r.recordFailedAttempt(ctx, destination, len(records), batchStart, batchEnd, err)
		return 0, err
	}
	if err := sink.Push(ctx, records); err != nil {
		_ = r.recordFailedAttempt(ctx, destination, len(records), batchStart, batchEnd, err)
		return 0, err
	}
	completedAt := time.Now().UTC()
	lastLog := logs[len(logs)-1]
	if _, err := r.store.RecordSIEMForwardingDeliveryAttempt(ctx, storage.RecordSIEMForwardingDeliveryAttemptParams{
		TenantID:      destination.TenantID,
		DestinationID: destination.ID,
		Status:        storage.SIEMForwardingDeliveryStatusSucceeded,
		RecordCount:   len(records),
		BatchStartAt:  batchStart,
		BatchEndAt:    batchEnd,
		CompletedAt:   &completedAt,
		Details: map[string]any{
			"sink": sink.Name(),
		},
	}); err != nil {
		return 0, fmt.Errorf("record successful SIEM forwarding attempt: %w", err)
	}
	cursorAt := lastLog.Timestamp.UTC()
	if cursorAt.IsZero() {
		cursorAt = *batchEnd
	}
	if _, err := r.store.RecordSIEMForwardingCheckpoint(ctx, storage.RecordSIEMForwardingCheckpointParams{
		TenantID:         destination.TenantID,
		DestinationID:    destination.ID,
		CursorAt:         cursorAt,
		CursorLogID:      lastLog.ID,
		LastRecordAt:     batchEnd,
		LastSuccessAt:    &completedAt,
		RecordsForwarded: int64(len(records)),
		BatchesForwarded: 1,
	}); err != nil {
		return 0, fmt.Errorf("record SIEM forwarding checkpoint: %w", err)
	}
	return len(records), nil
}

func (r *Runner) destinationCursor(ctx context.Context, destination storage.SIEMForwardingDestination) (time.Time, uuid.UUID, error) {
	checkpoint, err := r.store.GetSIEMForwardingCheckpoint(ctx, destination.TenantID, destination.ID)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	if checkpoint != nil && !checkpoint.CursorAt.IsZero() {
		return checkpoint.CursorAt.UTC(), checkpoint.CursorLogID, nil
	}
	return time.Now().UTC().Add(-r.opts.InitialLookback), uuid.Nil, nil
}

func (r *Runner) sinkForDestination(ctx context.Context, destination storage.SIEMForwardingDestination) (Sink, error) {
	cfg := SinkConfig{
		Kind:       destination.Kind,
		URL:        destination.URL,
		Tenant:     configString(destination.Config, "tenant", "tenant_id", "tenant_header", "x_scope_orgid", "loki_tenant"),
		Index:      configString(destination.Config, "index"),
		Source:     configString(destination.Config, "source"),
		SourceType: configString(destination.Config, "source_type", "sourcetype"),
	}
	switch destination.Kind {
	case storage.SIEMForwardingKindSplunkHEC:
		token, err := r.resolveCredential(ctx, destination, "token_ref", "credential_ref", "secret_ref")
		if err != nil {
			return nil, err
		}
		cfg.Token = token
	case storage.SIEMForwardingKindSentinel:
		token, err := r.resolveCredential(ctx, destination, "token_ref", "credential_ref", "secret_ref")
		if err != nil {
			return nil, err
		}
		cfg.Token = token
	case storage.SIEMForwardingKindElasticsearch:
		apiKey, err := r.resolveCredential(ctx, destination, "api_key_ref", "credential_ref", "secret_ref")
		if err != nil {
			return nil, err
		}
		cfg.APIKey = apiKey
	}
	return r.opts.SinkFactory(cfg)
}

func (r *Runner) resolveCredential(ctx context.Context, destination storage.SIEMForwardingDestination, keys ...string) (string, error) {
	ref := configString(destination.Config, keys...)
	if ref == "" {
		return "", fmt.Errorf("%s destination %s missing credential ref", destination.Kind, destination.ID)
	}
	if r.resolver == nil {
		return "", fmt.Errorf("%s destination %s has no credential resolver", destination.Kind, destination.ID)
	}
	value, err := r.resolver.ResolveCredential(ctx, destination.TenantID, ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("credential ref %q resolved to empty value", ref)
	}
	return strings.TrimSpace(value), nil
}

func (r *Runner) recordFailedAttempt(ctx context.Context, destination storage.SIEMForwardingDestination, recordCount int, batchStart, batchEnd *time.Time, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	_, err := r.store.RecordSIEMForwardingDeliveryAttempt(ctx, storage.RecordSIEMForwardingDeliveryAttemptParams{
		TenantID:      destination.TenantID,
		DestinationID: destination.ID,
		Status:        storage.SIEMForwardingDeliveryStatusFailed,
		RecordCount:   recordCount,
		BatchStartAt:  batchStart,
		BatchEndAt:    batchEnd,
		Error:         msg,
		Details: map[string]any{
			"kind": destination.Kind,
		},
	})
	return err
}

func logRecordFromTelemetryLog(row storage.TelemetryLog) LogRecord {
	source := ""
	if row.LogSource.Valid {
		source = row.LogSource.String
	}
	program := ""
	if row.LogProgram.Valid {
		program = row.LogProgram.String
	}
	labels := map[string]string{}
	for key, value := range row.Labels {
		labels[key] = value
	}
	timestamp := row.Timestamp.UTC()
	if timestamp.IsZero() {
		timestamp = row.CreatedAt.UTC()
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	return LogRecord{
		Timestamp: timestamp,
		Level:     row.LogLevel,
		Message:   row.LogMessage,
		Source:    source,
		Program:   program,
		NodeID:    row.NodeID.String(),
		TenantID:  row.TenantID.String(),
		Labels:    labels,
	}
}

func batchWindow(records []LogRecord) (*time.Time, *time.Time) {
	if len(records) == 0 {
		return nil, nil
	}
	start := records[0].Timestamp.UTC()
	end := start
	for _, record := range records[1:] {
		ts := record.Timestamp.UTC()
		if ts.Before(start) {
			start = ts
		}
		if ts.After(end) {
			end = ts
		}
	}
	return &start, &end
}

func configString(config map[string]any, keys ...string) string {
	for _, key := range keys {
		normalizedKey := normalizeConfigKey(key)
		value, ok := config[key]
		if ok {
			if text := configValueString(value); text != "" {
				return text
			}
			continue
		}
		for actualKey, actualValue := range config {
			if normalizeConfigKey(actualKey) != normalizedKey {
				continue
			}
			if text := configValueString(actualValue); text != "" {
				return text
			}
		}
	}
	return ""
}

func configValueString(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func normalizeConfigKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
}
