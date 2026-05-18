package logs

import (
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

func TestJSONAccessFormatterPreservesLifecycleHeaders(t *testing.T) {
	raw := RawLog{
		Timestamp: time.Date(2026, 5, 18, 4, 0, 0, 0, time.UTC),
		Message:   `{"ts":"2026-05-18T04:00:00Z","remote_ip":"203.0.113.10","method":"GET","path":"/login","status":401,"bytes":120,"request_id":"req-1","response_request_id":"resp-1","captured_response_headers":"X-Request-ID: resp-1"}`,
	}
	got, err := haproxyFormatter{}.Format(raw, config.LogSourceConfig{Program: "haproxy"})
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if got.Program != "haproxy" || got.Severity != "warn" {
		t.Fatalf("unexpected structured log program/severity: %#v", got)
	}
	if got.Fields["response_request_id"] != "resp-1" {
		t.Fatalf("response header field not preserved: %#v", got.Fields)
	}
	if got.Fields["captured_response_headers"] == "" {
		t.Fatalf("captured response headers missing: %#v", got.Fields)
	}
}
