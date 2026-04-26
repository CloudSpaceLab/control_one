package eventstream

import (
	"testing"
	"time"
)

func TestCorrelatorJoinsByPIDWithinWindow(t *testing.T) {
	c := NewCorrelator(2 * time.Second)
	now := time.Now()
	exec := Event{Type: "proc.exec", PID: 1234, TS: now}
	conn := Event{Type: "conn.open", PID: 1234, ConnID: "c1", TS: now.Add(500 * time.Millisecond)}
	c.Stamp(&exec)
	c.Stamp(&conn)
	if exec.CorrelationID == "" || exec.CorrelationID != conn.CorrelationID {
		t.Fatalf("expected shared correlation_id; got exec=%q conn=%q", exec.CorrelationID, conn.CorrelationID)
	}
}

func TestCorrelatorIgnoresStaleJoin(t *testing.T) {
	c := NewCorrelator(500 * time.Millisecond)
	now := time.Now()
	exec := Event{Type: "proc.exec", PID: 1234, TS: now}
	late := Event{Type: "conn.open", PID: 1234, ConnID: "c1", TS: now.Add(2 * time.Second)}
	c.Stamp(&exec)
	c.Stamp(&late)
	if exec.CorrelationID == late.CorrelationID {
		t.Fatal("late event should NOT join expired window")
	}
}

func TestCorrelatorJoinsByConnID(t *testing.T) {
	c := NewCorrelator(time.Second)
	now := time.Now()
	conn := Event{Type: "conn.open", PID: 99, ConnID: "abc", TS: now}
	file := Event{Type: "file.read.summary", ConnID: "abc", TS: now.Add(200 * time.Millisecond)}
	c.Stamp(&conn)
	c.Stamp(&file)
	if conn.CorrelationID != file.CorrelationID || conn.CorrelationID == "" {
		t.Fatalf("conn-id join failed: %q vs %q", conn.CorrelationID, file.CorrelationID)
	}
}

func TestCorrelatorPreservesProvidedID(t *testing.T) {
	c := NewCorrelator(time.Second)
	ev := Event{Type: "proc.exec", PID: 1, CorrelationID: "preset", TS: time.Now()}
	c.Stamp(&ev)
	if ev.CorrelationID != "preset" {
		t.Fatalf("preset id should win, got %q", ev.CorrelationID)
	}
}

func TestStreamPublishDropsOnFull(t *testing.T) {
	s := NewStream(2)
	s.Publish(Event{Type: "x"})
	s.Publish(Event{Type: "x"})
	s.Publish(Event{Type: "x"}) // dropped
	if got := s.Dropped(); got != 1 {
		t.Fatalf("want 1 dropped, got %d", got)
	}
}
