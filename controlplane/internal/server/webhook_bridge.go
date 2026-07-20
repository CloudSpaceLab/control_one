package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// DeliverFn is the function signature for webhook delivery used by the bridge.
type DeliverFn func(webhook *storage.Webhook, eventType string, payload map[string]any) (bool, int, string, error)

// WebhookBridge subscribes to the event bus and dispatches webhooks
// for events that have matching webhook subscribers.
type WebhookBridge struct {
	store     Store
	bus       *eventbus.Bus
	logger    *zap.Logger
	deliverFn DeliverFn
	client    *http.Client
	mu        sync.Mutex
	inFlight  map[string]time.Time
	cancel    context.CancelFunc
}

// NewWebhookBridge creates a bridge. Call Start to begin processing.
func NewWebhookBridge(store Store, bus *eventbus.Bus, logger *zap.Logger, deliverFn DeliverFn) *WebhookBridge {
	return &WebhookBridge{
		store:     store,
		bus:       bus,
		logger:    logger,
		deliverFn: deliverFn,
		client:    &http.Client{Timeout: 10 * time.Second},
		inFlight:  make(map[string]time.Time),
	}
}

// Start begins the bridge goroutine. Safe to call once; no-op on repeat.
func (b *WebhookBridge) Start(ctx context.Context) {
	if b == nil || b.bus == nil || b.store == nil {
		return
	}
	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.mu.Unlock()

	sub := b.bus.Subscribe(uuid.Nil, nil, nil)
	go b.run(ctx, sub)
}

// Stop halts the bridge goroutine. Safe to call when never started.
func (b *WebhookBridge) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

func (b *WebhookBridge) run(ctx context.Context, sub *eventbus.Subscription) {
	defer sub.Close()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Ch:
			if !ok {
				return
			}
			b.handleEvent(ctx, ev)
		case <-ticker.C:
			b.pruneDedup()
		}
	}
}

func (b *WebhookBridge) handleEvent(ctx context.Context, ev eventbus.Event) {
	if ev.TenantID == uuid.Nil || ev.Topic == "" {
		return
	}
	webhooks, err := b.store.ListWebhooksByEvent(ctx, ev.TenantID, ev.Topic)
	if err != nil {
		b.logger.Warn("webhook bridge: list webhooks",
			zap.String("topic", ev.Topic),
			zap.Error(err),
		)
		return
	}
	for i := range webhooks {
		wh := webhooks[i]
		dedupKey := wh.ID.String() + ":" + ev.ID.String()
		b.mu.Lock()
		if last, ok := b.inFlight[dedupKey]; ok && time.Since(last) < 5*time.Second {
			b.mu.Unlock()
			continue
		}
		b.inFlight[dedupKey] = time.Now()
		b.mu.Unlock()
		go b.deliver(ctx, &wh, ev)
	}
}

func (b *WebhookBridge) deliver(ctx context.Context, webhook *storage.Webhook, ev eventbus.Event) {
	var payload map[string]any
	if len(ev.Payload) > 0 {
		_ = json.Unmarshal(ev.Payload, &payload)
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["event_id"] = ev.ID.String()
	payload["event_topic"] = ev.Topic
	payload["tenant_id"] = ev.TenantID.String()
	payload["timestamp"] = ev.Timestamp.UTC().Format(time.RFC3339)
	if ev.NodeID != nil {
		payload["node_id"] = ev.NodeID.String()
	}

	var success bool
	var statusCode int
	var responseBody string
	var err error

	if b.deliverFn != nil {
		success, statusCode, responseBody, err = b.deliverFn(webhook, ev.Topic, payload)
	} else {
		success, statusCode, responseBody, err = b.deliverHTTP(ctx, webhook, ev.Topic, payload)
	}

	deliveryStatus := "success"
	if !success {
		deliveryStatus = "failed"
	}

	delivery := storage.WebhookDelivery{
		ID:            uuid.New(),
		WebhookID:     webhook.ID,
		EventType:     ev.Topic,
		Status:        deliveryStatus,
		AttemptNumber: 1,
		RequestBody:   payload,
		CreatedAt:     time.Now().UTC(),
	}
	if statusCode > 0 {
		delivery.HTTPStatusCode = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	if responseBody != "" {
		delivery.ResponseBody = sql.NullString{String: responseBody, Valid: true}
	}
	if err != nil {
		delivery.ErrorMessage = sql.NullString{String: err.Error(), Valid: true}
		b.logger.Warn("webhook bridge: delivery failed",
			zap.String("webhook_id", webhook.ID.String()),
			zap.String("topic", ev.Topic),
			zap.Error(err),
		)
	}
	now := time.Now().UTC()
	delivery.DeliveredAt = sql.NullTime{Time: now, Valid: true}

	if rerr := b.store.RecordWebhookDelivery(ctx, delivery); rerr != nil {
		b.logger.Error("webhook bridge: record delivery",
			zap.String("webhook_id", webhook.ID.String()),
			zap.Error(rerr),
		)
	}
}

func (b *WebhookBridge) deliverHTTP(ctx context.Context, webhook *storage.Webhook, eventType string, payload map[string]any) (bool, int, string, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		return false, 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", eventType)
	req.Header.Set("X-Webhook-ID", webhook.ID.String())
	req.Header.Set("User-Agent", "Control-One-Webhook/1.0")
	resp, err := b.client.Do(req)
	if err != nil {
		return false, 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	return success, resp.StatusCode, string(body), nil
}

func (b *WebhookBridge) pruneDedup() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, t := range b.inFlight {
		if now.Sub(t) > 10*time.Second {
			delete(b.inFlight, k)
		}
	}
}
