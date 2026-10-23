package telemetry

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/telemetry/logs"
)

func TestMissingJournaldRemainsExplicitlyUnsupported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := &Service{log: zap.NewNop()}
	out := make(chan logs.RawLog)
	s.runCollectorLoop(context.Background(), "test-node", config.LogSourceConfig{Program: "linux-auth", Type: "journald"}, out)
	states := s.LogSourceStates()
	if len(states) != 1 || states[0].State != "unsupported" || states[0].BackoffReason == "" {
		t.Fatalf("missing backend must remain visible as unsupported coverage: %#v", states)
	}
	if _, open := <-out; open {
		t.Fatal("unsupported collector left output open")
	}
}
