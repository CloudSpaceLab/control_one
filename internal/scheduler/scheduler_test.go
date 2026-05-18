package scheduler

import (
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestWrapJobSkipsOverlappingRun(t *testing.T) {
	s := New(zap.NewNop())
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var runs atomic.Int32

	wrapped := s.wrapJob("slow", func() {
		runs.Add(1)
		started <- struct{}{}
		<-release
	})

	go wrapped()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first run did not start")
	}

	wrapped()
	if got := runs.Load(); got != 1 {
		t.Fatalf("overlapping run executed; runs = %d", got)
	}
	close(release)
}
