package logs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

func TestFileCollectorPersistsCursorAcrossRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	cursorPath := filepath.Join(dir, "cursors", "app.json")
	if err := os.WriteFile(logPath, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	source := config.LogSourceConfig{
		Program:         "app",
		Type:            "file",
		Paths:           []string{logPath},
		PollInterval:    10 * time.Millisecond,
		CursorStateFile: cursorPath,
	}
	config.NormalizeLogSourceConfig(&source)

	first := runFileCollectorUntilLog(t, source)
	if first.Message != "first" {
		t.Fatalf("first message = %q", first.Message)
	}
	waitForFile(t, cursorPath)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString("second\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append log: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close append: %v", err)
	}

	second := runFileCollectorUntilLog(t, source)
	if second.Message != "second" {
		t.Fatalf("restart message = %q, want second without rereading first", second.Message)
	}
}

func runFileCollectorUntilLog(t *testing.T, source config.LogSourceConfig) RawLog {
	t.Helper()

	collector, err := NewFileCollector(source, zap.NewNop())
	if err != nil {
		t.Fatalf("NewFileCollector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan RawLog, 8)
	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx, out)
	}()
	select {
	case raw := <-out:
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("collector Run: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("collector did not stop")
		}
		return raw
	case err := <-done:
		t.Fatalf("collector exited before log: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for log")
	}
	return RawLog{}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
