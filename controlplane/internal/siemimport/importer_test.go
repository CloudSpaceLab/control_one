package siemimport

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseSplunkHECImport(t *testing.T) {
	tenantID := uuid.New()
	body := []byte(`{"time":1770000000.25,"host":"dc01","source":"/var/log/auth.log","sourcetype":"linux_secure","index":"main","event":{"message":"failed password","user":"root"},"fields":{"env":"prod"}}
{"time":"2026-02-02T03:04:05Z","event":"accepted publickey","severity":"warning"}`)

	rows, summary, err := ParseLogs(body, Options{
		TenantID:   tenantID,
		Format:     FormatSplunkHEC,
		Source:     "splunk-main",
		ImportID:   "imp-1",
		ImportedAt: time.Date(2026, 2, 2, 3, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ParseLogs: %v", err)
	}
	if summary.RowsAccepted != 2 || len(rows) != 2 {
		t.Fatalf("accepted = %d rows=%d", summary.RowsAccepted, len(rows))
	}
	if rows[0].TenantID != tenantID || rows[0].LogLevel != "info" || rows[0].Labels["sourcetype"] != "linux_secure" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].LogLevel != "warn" || rows[1].LogMessage != "accepted publickey" {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
}

func TestParseElasticBulkImport(t *testing.T) {
	body := []byte(`{"index":{"_index":"winlogbeat-2026.02.02","_id":"1"}}
{"@timestamp":"2026-02-02T03:04:05Z","message":"login failed","log":{"level":"error"},"event":{"dataset":"windows.security"},"host":{"name":"dc01"}}
{"create":{"_index":"filebeat-2026.02.02","_id":"2"}}
{"@timestamp":"2026-02-02T03:05:05Z","message":"nginx 500","log.level":"warning","agent.type":"filebeat"}`)

	rows, summary, err := ParseLogs(body, Options{TenantID: uuid.New(), Format: FormatElasticBulk, ImportID: "imp-2"})
	if err != nil {
		t.Fatalf("ParseLogs: %v", err)
	}
	if summary.RowsParsed != 2 || summary.RowsAccepted != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if rows[0].LogLevel != "error" || rows[0].Labels["elasticindex"] != "winlogbeat-2026.02.02" || rows[0].Labels["event.dataset"] != "windows.security" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].LogLevel != "warn" || rows[1].Labels["elasticindex"] != "filebeat-2026.02.02" {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
}

func TestParseSentinelArrayImport(t *testing.T) {
	body := []byte(`[
  {"TimeGenerated":"2026-02-02T03:04:05Z","Computer":"srv01","Type":"SecurityEvent","Level":"Informational","RenderedDescription":"account logon","EventID":4624},
  {"TimeGenerated":"2026-02-02T03:05:05Z","Computer":"srv01","Type":"SecurityEvent","Severity":"Error","RawData":"bad login"}
]`)

	rows, summary, err := ParseLogs(body, Options{TenantID: uuid.New(), Format: FormatLogAnalytics, Source: "sentinel-export", ImportID: "imp-3"})
	if err != nil {
		t.Fatalf("ParseLogs: %v", err)
	}
	if summary.RowsAccepted != 2 || rows[0].Labels["computer"] != "srv01" || rows[0].LogMessage != "account logon" {
		t.Fatalf("unexpected import: summary=%+v rows=%+v", summary, rows)
	}
	if rows[1].LogLevel != "error" {
		t.Fatalf("second level = %q", rows[1].LogLevel)
	}
}
