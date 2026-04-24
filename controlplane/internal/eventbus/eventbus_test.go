package eventbus

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBusDeliversMatchingEvents(t *testing.T) {
	b := New(4)
	tenant := uuid.New()
	sub := b.Subscribe(tenant, []string{TopicPolicyUpdated}, nil)
	defer sub.Close()

	b.Publish(Event{Topic: TopicPolicyUpdated, TenantID: tenant})

	select {
	case ev := <-sub.Ch:
		if ev.Topic != TopicPolicyUpdated {
			t.Fatalf("unexpected topic %q", ev.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered")
	}
}

func TestBusFiltersByTenant(t *testing.T) {
	b := New(4)
	tenantA := uuid.New()
	tenantB := uuid.New()
	sub := b.Subscribe(tenantA, nil, nil)
	defer sub.Close()

	b.Publish(Event{Topic: TopicPolicyUpdated, TenantID: tenantB})
	select {
	case ev := <-sub.Ch:
		t.Fatalf("got unexpected event for wrong tenant %s", ev.TenantID)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBusFiltersByTopic(t *testing.T) {
	b := New(4)
	tenant := uuid.New()
	sub := b.Subscribe(tenant, []string{TopicAlertOpened}, nil)
	defer sub.Close()

	b.Publish(Event{Topic: TopicPolicyUpdated, TenantID: tenant})
	select {
	case ev := <-sub.Ch:
		t.Fatalf("got unexpected topic %s", ev.Topic)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBusDropsOnFullBuffer(t *testing.T) {
	b := New(1)
	tenant := uuid.New()
	sub := b.Subscribe(tenant, nil, nil)
	defer sub.Close()

	dropped := 0
	b.SetDropHook(func(_ uuid.UUID, _ Event) { dropped++ })

	b.Publish(Event{Topic: "x", TenantID: tenant})
	b.Publish(Event{Topic: "y", TenantID: tenant})
	b.Publish(Event{Topic: "z", TenantID: tenant})
	if dropped == 0 {
		t.Fatal("expected drops on full buffer")
	}
}

func TestSubscriptionClose(t *testing.T) {
	b := New(4)
	tenant := uuid.New()
	sub := b.Subscribe(tenant, nil, nil)
	if b.SubscriberCount() != 1 {
		t.Fatalf("want 1 sub, got %d", b.SubscriberCount())
	}
	sub.Close()
	if b.SubscriberCount() != 0 {
		t.Fatalf("want 0 subs after close, got %d", b.SubscriberCount())
	}
}

func TestBusNodeFilter(t *testing.T) {
	b := New(4)
	tenant := uuid.New()
	nodeA := uuid.New()
	nodeB := uuid.New()
	sub := b.Subscribe(tenant, nil, &nodeA)
	defer sub.Close()

	b.Publish(Event{Topic: "x", TenantID: tenant, NodeID: &nodeB})
	select {
	case <-sub.Ch:
		t.Fatal("should not receive event for other node")
	case <-time.After(50 * time.Millisecond):
	}

	b.Publish(Event{Topic: "x", TenantID: tenant, NodeID: &nodeA})
	select {
	case <-sub.Ch:
	case <-time.After(time.Second):
		t.Fatal("expected event for matching node")
	}
}
