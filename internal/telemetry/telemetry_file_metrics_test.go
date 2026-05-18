package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

func TestFileSizeMetricSamplesFromLogSourcePaths(t *testing.T) {
	dir := t.TempDir()
	appLog := filepath.Join(dir, "app.log")
	otherLog := filepath.Join(dir, "other.log")
	if err := os.WriteFile(appLog, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write app log: %v", err)
	}
	if err := os.WriteFile(otherLog, []byte("larger-log\n"), 0o600); err != nil {
		t.Fatalf("write other log: %v", err)
	}

	samples := fileSizeMetricSamples(config.LogSourceConfig{
		Program: "app",
		Paths:   []string{appLog, filepath.Join(dir, "missing.log"), otherLog},
		Labels:  map[string]string{"env": "test"},
	})

	if len(samples) != 2 {
		t.Fatalf("expected two file-size samples, got %+v", samples)
	}
	if samples[0].Name != metricFileSizeBytes || samples[0].Unit != "bytes" || samples[0].Value != float64(6) {
		t.Fatalf("unexpected first sample: %+v", samples[0])
	}
	if samples[0].Labels["path"] != appLog || samples[0].Labels["program"] != "app" || samples[0].Labels["source"] != "file" || samples[0].Labels["env"] != "test" {
		t.Fatalf("labels were not preserved/enriched: %+v", samples[0].Labels)
	}
	if samples[1].Labels["path"] != otherLog || samples[1].Value != float64(11) {
		t.Fatalf("unexpected second sample: %+v", samples[1])
	}
}
