package netflow

import (
	"github.com/CloudSpaceLab/control_one/internal/eventstream"
	"net"
	"testing"
	"time"
)

func TestLongLivedConnectionUsesObservationTimestamp(t *testing.T) {
	stream := eventstream.NewStream(1)
	manager := &Manager{stream: stream}
	started := time.Now().UTC().Add(-14 * 24 * time.Hour)
	before := time.Now().UTC()
	manager.handle(ConnectionEvent{Kind: "summary", StartedAt: started, LastDataAt: started, SrcIP: net.ParseIP("10.0.0.2"), DstIP: net.ParseIP("203.0.113.10")})
	event := <-stream.Out()
	if event.TS.Before(before) || event.TS.After(time.Now().UTC()) {
		t.Fatalf("observation timestamp=%s", event.TS)
	}
	if event.Details["started_at"] != started.Format("2006-01-02 15:04:05.000") {
		t.Fatal("original connection start time was lost")
	}
}
