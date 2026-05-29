package logforward

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type runnerStore struct {
	tenants      []storage.Tenant
	destinations []storage.SIEMForwardingDestination
	checkpoints  map[uuid.UUID]storage.SIEMForwardingCheckpoint
	logs         []storage.TelemetryLog
	attempts     []storage.RecordSIEMForwardingDeliveryAttemptParams
	updates      []storage.RecordSIEMForwardingCheckpointParams
}

func (s *runnerStore) ListTenants(_ context.Context, _ string, limit, offset int) ([]storage.Tenant, int, error) {
	total := len(s.tenants)
	if offset > total {
		return []storage.Tenant{}, total, nil
	}
	out := append([]storage.Tenant(nil), s.tenants[offset:]...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (s *runnerStore) ListSIEMForwardingDestinations(_ context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]storage.SIEMForwardingDestination, int, error) {
	var filtered []storage.SIEMForwardingDestination
	for _, destination := range s.destinations {
		if destination.TenantID != tenantID {
			continue
		}
		if strings.TrimSpace(status) != "" && destination.Status != status {
			continue
		}
		filtered = append(filtered, destination)
	}
	total := len(filtered)
	if offset > total {
		return []storage.SIEMForwardingDestination{}, total, nil
	}
	filtered = filtered[offset:]
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (s *runnerStore) GetSIEMForwardingCheckpoint(_ context.Context, _ uuid.UUID, destinationID uuid.UUID) (*storage.SIEMForwardingCheckpoint, error) {
	if s.checkpoints == nil {
		return nil, nil
	}
	checkpoint, ok := s.checkpoints[destinationID]
	if !ok {
		return nil, nil
	}
	return &checkpoint, nil
}

func (s *runnerStore) RecordSIEMForwardingCheckpoint(_ context.Context, p storage.RecordSIEMForwardingCheckpointParams) (*storage.SIEMForwardingCheckpoint, error) {
	s.updates = append(s.updates, p)
	if s.checkpoints == nil {
		s.checkpoints = map[uuid.UUID]storage.SIEMForwardingCheckpoint{}
	}
	row := storage.SIEMForwardingCheckpoint{
		ID:               uuid.New(),
		TenantID:         p.TenantID,
		DestinationID:    p.DestinationID,
		CursorAt:         p.CursorAt,
		CursorLogID:      p.CursorLogID,
		LastRecordAt:     p.LastRecordAt,
		LastSuccessAt:    p.LastSuccessAt,
		LastError:        p.LastError,
		RecordsForwarded: p.RecordsForwarded,
		BatchesForwarded: p.BatchesForwarded,
	}
	s.checkpoints[p.DestinationID] = row
	return &row, nil
}

func (s *runnerStore) RecordSIEMForwardingDeliveryAttempt(_ context.Context, p storage.RecordSIEMForwardingDeliveryAttemptParams) (*storage.SIEMForwardingDeliveryAttempt, error) {
	s.attempts = append(s.attempts, p)
	return &storage.SIEMForwardingDeliveryAttempt{
		ID:            uuid.New(),
		TenantID:      p.TenantID,
		DestinationID: p.DestinationID,
		Status:        p.Status,
		RecordCount:   p.RecordCount,
		BatchStartAt:  p.BatchStartAt,
		BatchEndAt:    p.BatchEndAt,
		Error:         p.Error,
		Details:       p.Details,
		CompletedAt:   p.CompletedAt,
	}, nil
}

func (s *runnerStore) ListTelemetryLogsForForwarding(_ context.Context, tenantID uuid.UUID, since time.Time, cursorLogID uuid.UUID, limit int) ([]storage.TelemetryLog, error) {
	var filtered []storage.TelemetryLog
	for _, row := range s.logs {
		if row.TenantID != tenantID {
			continue
		}
		if row.Timestamp.Before(since) {
			continue
		}
		if row.Timestamp.Equal(since) {
			if cursorLogID == uuid.Nil || strings.Compare(row.ID.String(), cursorLogID.String()) <= 0 {
				continue
			}
		}
		filtered = append(filtered, row)
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

type runnerCaptureSink struct {
	name    string
	records []LogRecord
}

func (s *runnerCaptureSink) Name() string { return s.name }
func (s *runnerCaptureSink) Push(_ context.Context, records []LogRecord) error {
	s.records = append(s.records, records...)
	return nil
}

func TestRunnerForwardsEnabledDestinationsAndRecordsCheckpoint(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	destinationID := uuid.New()
	firstLogID := uuid.New()
	secondLogID := uuid.New()
	start := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	sink := &runnerCaptureSink{name: "capture"}
	store := &runnerStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "bank-a"}},
		destinations: []storage.SIEMForwardingDestination{{
			ID:       destinationID,
			TenantID: tenantID,
			Name:     "Splunk",
			Kind:     storage.SIEMForwardingKindSplunkHEC,
			Status:   storage.SIEMForwardingDestinationStatusEnabled,
			URL:      "https://splunk.example.test/services/collector",
			Config: map[string]any{
				"token_ref":   "env:SPLUNK_HEC_TOKEN",
				"index":       "bank_sec",
				"source_type": "controlone:bank",
			},
		}},
		checkpoints: map[uuid.UUID]storage.SIEMForwardingCheckpoint{
			destinationID: {TenantID: tenantID, DestinationID: destinationID, CursorAt: start},
		},
		logs: []storage.TelemetryLog{
			{ID: uuid.New(), TenantID: tenantID, NodeID: nodeID, LogLevel: "info", LogMessage: "ignored old", Timestamp: start},
			{ID: firstLogID, TenantID: tenantID, NodeID: nodeID, LogLevel: "warn", LogMessage: "first", LogSource: sql.NullString{String: "wec", Valid: true}, Timestamp: start.Add(time.Second), Labels: map[string]string{"channel": "Security"}},
			{ID: secondLogID, TenantID: tenantID, NodeID: nodeID, LogLevel: "error", LogMessage: "second", LogProgram: sql.NullString{String: "collector", Valid: true}, Timestamp: start.Add(2 * time.Second)},
		},
	}
	var gotConfig SinkConfig
	runner, err := NewRunner(store, CredentialResolverFunc(func(_ context.Context, gotTenant uuid.UUID, ref string) (string, error) {
		if gotTenant != tenantID || ref != "env:SPLUNK_HEC_TOKEN" {
			t.Fatalf("resolver called with tenant=%s ref=%q", gotTenant, ref)
		}
		return "resolved-token", nil
	}), nil, RunnerOptions{
		MaxBatchSize: 10,
		SinkFactory: func(config SinkConfig) (Sink, error) {
			gotConfig = config
			return sink, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error: %v", err)
	}
	if summary.Records != 2 || summary.Batches != 1 || summary.Failures != 0 {
		t.Fatalf("summary = %#v", summary)
	}
	if gotConfig.Token != "resolved-token" || gotConfig.Index != "bank_sec" || gotConfig.SourceType != "controlone:bank" {
		t.Fatalf("sink config = %#v", gotConfig)
	}
	if len(sink.records) != 2 || sink.records[0].Message != "first" || sink.records[1].Message != "second" {
		t.Fatalf("records = %#v", sink.records)
	}
	if len(store.attempts) != 1 || store.attempts[0].Status != storage.SIEMForwardingDeliveryStatusSucceeded || store.attempts[0].RecordCount != 2 {
		t.Fatalf("attempts = %#v", store.attempts)
	}
	if len(store.updates) != 1 || !store.updates[0].CursorAt.Equal(start.Add(2*time.Second)) || store.updates[0].CursorLogID != secondLogID || store.updates[0].RecordsForwarded != 2 {
		t.Fatalf("checkpoint updates = %#v", store.updates)
	}
}

func TestRunnerRecordsFailedAttemptOnCredentialError(t *testing.T) {
	tenantID := uuid.New()
	destinationID := uuid.New()
	now := time.Now().UTC()
	store := &runnerStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "bank-a"}},
		destinations: []storage.SIEMForwardingDestination{{
			ID:       destinationID,
			TenantID: tenantID,
			Name:     "Splunk",
			Kind:     storage.SIEMForwardingKindSplunkHEC,
			Status:   storage.SIEMForwardingDestinationStatusEnabled,
			URL:      "https://splunk.example.test/services/collector",
			Config:   map[string]any{"token_ref": "vault://tenant/splunk"},
		}},
		checkpoints: map[uuid.UUID]storage.SIEMForwardingCheckpoint{
			destinationID: {TenantID: tenantID, DestinationID: destinationID, CursorAt: now.Add(-time.Minute)},
		},
		logs: []storage.TelemetryLog{{TenantID: tenantID, NodeID: uuid.New(), LogMessage: "send me", Timestamp: now}},
	}
	runner, err := NewRunner(store, CredentialResolverFunc(func(context.Context, uuid.UUID, string) (string, error) {
		return "", errors.New("vault unavailable")
	}), nil, RunnerOptions{MaxBatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := runner.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected RunOnce error")
	}
	if summary.Failures != 1 || summary.Records != 0 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(store.attempts) != 1 || store.attempts[0].Status != storage.SIEMForwardingDeliveryStatusFailed || store.attempts[0].RecordCount != 1 {
		t.Fatalf("attempts = %#v", store.attempts)
	}
	if !strings.Contains(store.attempts[0].Error, "vault unavailable") {
		t.Fatalf("attempt error = %q", store.attempts[0].Error)
	}
	if len(store.updates) != 0 {
		t.Fatalf("checkpoint should not advance on failed push: %#v", store.updates)
	}
}
