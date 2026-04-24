package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReadLoopDecodesMultipleEvents(t *testing.T) {
	sub := &Subscriber{}
	stream := ": connected\n\n" +
		"id: a\nevent: policy.updated\ndata: {\"id\":\"a\",\"topic\":\"policy.updated\",\"tenant_id\":\"t1\"}\n\n" +
		"id: b\nevent: alert.opened\ndata: {\"id\":\"b\",\"topic\":\"alert.opened\",\"tenant_id\":\"t1\"}\n\n"

	got := make([]Event, 0, 2)
	sub.handler = func(_ context.Context, ev Event) { got = append(got, ev) }
	if err := sub.readLoop(context.Background(), io.NopCloser(bytes.NewBufferString(stream))); err != nil {
		t.Fatalf("readLoop: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if got[0].Topic != "policy.updated" || got[1].Topic != "alert.opened" {
		t.Fatalf("unexpected topics: %+v", got)
	}
}

func TestReadLoopSkipsComments(t *testing.T) {
	sub := &Subscriber{}
	var buf bytes.Buffer
	buf.WriteString(": ping\n\n")
	ev := Event{ID: "x", Topic: "x", TenantID: "t", Timestamp: time.Now()}
	raw, _ := json.Marshal(ev)
	fmt.Fprintf(&buf, "data: %s\n\n", raw)
	var got []Event
	sub.handler = func(_ context.Context, e Event) { got = append(got, e) }
	if err := sub.readLoop(context.Background(), io.NopCloser(strings.NewReader(buf.String()))); err != nil {
		t.Fatalf("readLoop: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
}
