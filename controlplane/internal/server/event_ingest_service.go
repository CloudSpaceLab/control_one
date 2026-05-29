package server

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type eventIngestServiceStore interface {
	RecordEventIngest(context.Context, storage.CreateEventIngestBatchParams) (uuid.UUID, error)
	MarkEventIngestLocalComplete(context.Context, uuid.UUID, []byte, int) error
	MarkEventIngestStatus(context.Context, uuid.UUID, string, string, string) error
}

type eventIngestPrepareFunc func(context.Context, uuid.UUID, uuid.UUID, []IngestedEvent) ([]IngestedEvent, []IngestedEvent)
type eventIngestApplyFunc func(context.Context, uuid.UUID, uuid.UUID, []IngestedEvent, []IngestedEvent)
type eventIngestFlushFunc func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []IngestedEvent) (string, error)

type eventIngestService struct {
	store   eventIngestServiceStore
	logger  *zap.Logger
	prepare eventIngestPrepareFunc
	apply   eventIngestApplyFunc
	flush   eventIngestFlushFunc
}

type journaledLogEventBatch struct {
	ID          uuid.UUID
	Status      string
	DorisStatus string
	Duplicate   bool
}

func (s *Server) eventIngestService() eventIngestService {
	if s == nil {
		return eventIngestService{logger: zap.NewNop()}
	}
	logger := s.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return eventIngestService{
		store:   s.store,
		logger:  logger,
		prepare: s.prepareEventFanout,
		apply:   s.applyLocalEventFanout,
		flush:   s.flushDorisEventFanout,
	}
}

func (svc eventIngestService) recordLogDerivedBatch(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent, replayKey string) (journaledLogEventBatch, error) {
	if len(events) == 0 {
		return journaledLogEventBatch{Status: "accepted", DorisStatus: "disabled"}, nil
	}
	if svc.store == nil {
		return journaledLogEventBatch{}, errors.New("event ingest storage unavailable")
	}
	payload, err := encodeIngestedEventPayload(events)
	if err != nil {
		return journaledLogEventBatch{}, err
	}
	tArg := tenantID
	nArg := nodeID
	batchID, err := svc.store.RecordEventIngest(ctx, storage.CreateEventIngestBatchParams{
		TenantID:  &tArg,
		NodeID:    &nArg,
		SizeBytes: int64(len(payload)),
		Rows:      len(events),
		Status:    "received",
		ReplayKey: logDerivedEventReplayKey(replayKey),
		Payload:   payload,
	})
	if err != nil {
		var duplicate *storage.DuplicateEventIngestReplayError
		if errors.As(err, &duplicate) {
			status := strings.TrimSpace(duplicate.Batch.Status)
			if status == "" {
				status = "received"
			}
			dorisStatus := ""
			if duplicate.Batch.DorisStatus.Valid {
				dorisStatus = duplicate.Batch.DorisStatus.String
			}
			return journaledLogEventBatch{
				ID:          duplicate.Batch.ID,
				Status:      status,
				DorisStatus: dorisStatus,
				Duplicate:   true,
			}, nil
		}
		return journaledLogEventBatch{}, err
	}
	return journaledLogEventBatch{ID: batchID, Status: "received"}, nil
}

func (svc eventIngestService) complete(ctx context.Context, batchID, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, string, error) {
	if svc.store == nil {
		return "", "failed", errors.New("event ingest storage unavailable")
	}
	dorisStatus, fanoutErr := svc.fanoutMarkLocalAndFlush(ctx, batchID, tenantID, nodeID, events)
	finalStatus := "accepted"
	errMsg := ""
	if fanoutErr != nil {
		finalStatus = "pending_doris"
		errMsg = fanoutErr.Error()
	}
	if uerr := svc.store.MarkEventIngestStatus(ctx, batchID, finalStatus, dorisStatus, errMsg); uerr != nil {
		svc.log().Warn("mark event ingest status", zap.Error(uerr), zap.String("batch_id", batchID.String()))
	}
	return dorisStatus, finalStatus, fanoutErr
}

func (svc eventIngestService) fanoutMarkLocalAndFlush(ctx context.Context, batchID, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
	if svc.store == nil {
		return "", errors.New("event ingest storage unavailable")
	}
	fanoutEvents := events
	var anomalies []IngestedEvent
	if svc.prepare != nil {
		fanoutEvents, anomalies = svc.prepare(ctx, tenantID, nodeID, events)
	}
	if svc.apply != nil {
		svc.apply(ctx, tenantID, nodeID, fanoutEvents, anomalies)
	}
	if payload, encErr := encodeIngestedEventPayload(fanoutEvents); encErr != nil {
		svc.log().Warn("encode normalized event ingest payload", zap.Error(encErr), zap.String("batch_id", batchID.String()))
	} else if uerr := svc.store.MarkEventIngestLocalComplete(ctx, batchID, payload, len(fanoutEvents)); uerr != nil {
		svc.log().Warn("mark event ingest local complete", zap.Error(uerr), zap.String("batch_id", batchID.String()))
	}
	return svc.flushDoris(ctx, batchID, tenantID, nodeID, fanoutEvents)
}

func (svc eventIngestService) flushDoris(ctx context.Context, batchID, tenantID, nodeID uuid.UUID, events []IngestedEvent) (string, error) {
	if svc.flush == nil {
		return "disabled", nil
	}
	return svc.flush(ctx, batchID, tenantID, nodeID, events)
}

func (svc eventIngestService) log() *zap.Logger {
	if svc.logger != nil {
		return svc.logger
	}
	return zap.NewNop()
}
