package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestSIEMImportsAPIPersistsExistingSIEMArchive(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "siem-operator"),
	}, store, &stubQueue{})

	body := strings.NewReader(`{"time":"2026-02-02T03:04:05Z","host":"dc01","source":"/var/log/auth.log","sourcetype":"linux_secure","event":"failed password","fields":{"env":"prod"}}
{"time":"2026-02-02T03:05:05Z","event":{"message":"accepted publickey","user":"alice"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/siem/imports?tenant_id="+tenantID.String()+"&format=splunk_hec&source=splunk-main", body)
	req.Header.Set("Authorization", "Bearer siem-operator")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.telemetryLogs) != 2 {
		t.Fatalf("telemetry logs = %d, want 2", len(store.telemetryLogs))
	}
	first := store.telemetryLogs[0]
	if first.TenantID != tenantID || first.NodeID != uuid.Nil || first.LogMessage != "failed password" {
		t.Fatalf("first log = %+v", first)
	}
	if first.Labels["control_one.import_format"] != "splunk_hec" || first.Labels["control_one.import_source"] != "splunk-main" {
		t.Fatalf("labels = %#v", first.Labels)
	}
	var resp siemImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StoredRows != 2 || resp.Summary.RowsAccepted != 2 || resp.RawSHA256 == "" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestSIEMImportsAPISupportsGzipDryRun(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "siem-operator"),
	}, store, &stubQueue{})

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(`[{"TimeGenerated":"2026-02-02T03:04:05Z","Computer":"srv01","Type":"SecurityEvent","RenderedDescription":"account logon"}]`)); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/siem/imports?tenant_id="+tenantID.String()+"&format=log_analytics&dry_run=true", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Authorization", "Bearer siem-operator")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.telemetryLogs) != 0 {
		t.Fatalf("dry-run stored logs = %d, want 0", len(store.telemetryLogs))
	}
	var resp siemImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.DryRun || resp.Summary.RowsAccepted != 1 || resp.StoredRows != 0 {
		t.Fatalf("response = %+v", resp)
	}
}
