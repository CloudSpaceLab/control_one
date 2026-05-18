// Package eventbus implements an in-process pub/sub topic bus used by the
// control plane to fan real-time events (policy.updated, alert.opened, etc.)
// out to subscribers like the SSE stream handler and internal correlators.
package eventbus

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event is the unit delivered to subscribers. TenantID narrows fan-out so
// one tenant never sees another tenant's events. NodeID is optional.
type Event struct {
	ID        uuid.UUID       `json:"id"`
	Topic     string          `json:"topic"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	NodeID    *uuid.UUID      `json:"node_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Subscription is a buffered receiver. Cancel to unsubscribe and release.
type Subscription struct {
	ID       uuid.UUID
	Topics   []string
	TenantID uuid.UUID
	NodeID   *uuid.UUID
	Ch       chan Event
	cancel   func()
}

// Close releases the subscription.
func (s *Subscription) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// Bus fans events to matching subscribers. Publish is non-blocking per
// subscriber: slow receivers drop events rather than stall the producer.
type Bus struct {
	mu       sync.RWMutex
	subs     map[uuid.UUID]*Subscription
	bufSize  int
	dropHook func(subID uuid.UUID, ev Event)
}

// New returns a ready-to-use bus. bufSize is the per-subscriber channel
// buffer (16 is a sensible default).
func New(bufSize int) *Bus {
	if bufSize <= 0 {
		bufSize = 16
	}
	return &Bus{subs: make(map[uuid.UUID]*Subscription), bufSize: bufSize}
}

// Subscribe registers a receiver. Pass empty topics slice to receive all topics.
// NodeID may be nil to receive everything for the tenant.
func (b *Bus) Subscribe(tenantID uuid.UUID, topics []string, nodeID *uuid.UUID) *Subscription {
	id := uuid.New()
	sub := &Subscription{
		ID:       id,
		Topics:   append([]string(nil), topics...),
		TenantID: tenantID,
		NodeID:   nodeID,
		Ch:       make(chan Event, b.bufSize),
	}
	sub.cancel = func() {
		b.mu.Lock()
		if _, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(sub.Ch)
		}
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.subs[id] = sub
	b.mu.Unlock()
	return sub
}

// Publish fans an event out. Callers supply topic/tenant/payload; bus fills ID/time.
func (b *Bus) Publish(ev Event) {
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	b.mu.RLock()
	targets := make([]*Subscription, 0, len(b.subs))
	for _, s := range b.subs {
		if !matches(s, ev) {
			continue
		}
		targets = append(targets, s)
	}
	b.mu.RUnlock()
	for _, s := range targets {
		select {
		case s.Ch <- ev:
		default:
			if b.dropHook != nil {
				b.dropHook(s.ID, ev)
			}
		}
	}
}

// SetDropHook registers a callback invoked when an event is dropped because
// the subscriber buffer was full. Used for metrics.
func (b *Bus) SetDropHook(fn func(subID uuid.UUID, ev Event)) {
	b.mu.Lock()
	b.dropHook = fn
	b.mu.Unlock()
}

// SubscriberCount returns the current number of active subscribers (for metrics/tests).
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

func matches(s *Subscription, ev Event) bool {
	if s.TenantID != uuid.Nil && ev.TenantID != uuid.Nil && s.TenantID != ev.TenantID {
		return false
	}
	if s.NodeID != nil && ev.NodeID != nil && *s.NodeID != *ev.NodeID {
		return false
	}
	if len(s.Topics) == 0 {
		return true
	}
	for _, t := range s.Topics {
		if t == ev.Topic {
			return true
		}
	}
	return false
}

// Topics is a central registry of well-known topic strings. Using constants
// avoids typos between publisher and subscriber.
const (
	TopicPolicyUpdated      = "policy.updated"
	TopicComplianceFired    = "compliance.fired"
	TopicRemediationApplied = "remediation.applied"
	TopicAlertOpened        = "alert.opened"
	TopicRuleTriggered      = "rule.triggered"
	TopicSecurityEvent      = "security.event"
	TopicHealthIncident     = "health.incident"
	TopicDashboardTick      = "dashboard.tick"
	TopicEventsAnomaly      = "events.anomaly"
)
