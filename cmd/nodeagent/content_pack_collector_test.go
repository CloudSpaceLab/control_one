package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

func TestContentPackEdgeCollectorWrapperAppliesDesiredConfigAndReports(t *testing.T) {
	desiredYAML := "receivers:\n  filelog/test: {}\n"
	configVersion := contentPackRenderedConfigVersion(desiredYAML)
	token := "c1ec_test_token"

	var mu sync.Mutex
	var applyStatus string
	var applyVersion string
	var heartbeatStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-ControlOne-Collector-Token"); got != token {
			t.Fatalf("collector token = %q, want %q", got, token)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-1" {
			t.Fatalf("tenant_id = %q", got)
		}
		switch r.URL.Path {
		case "/api/v1/content-packs/collectors/edge-1/desired-config":
			if r.Method != http.MethodGet {
				t.Fatalf("desired method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(contentPackDesiredConfigResponse{
				TenantID:      "tenant-1",
				CollectorID:   "edge-1",
				CandidateID:   "candidate-1",
				ConfigVersion: configVersion,
				YAML:          desiredYAML,
			})
		case "/api/v1/content-packs/collectors/edge-1/apply-result":
			if r.Method != http.MethodPost {
				t.Fatalf("apply method = %s", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode apply body: %v", err)
			}
			mu.Lock()
			applyStatus = body["status"]
			applyVersion = body["config_version"]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		case "/api/v1/content-packs/collectors/edge-1/heartbeat":
			if r.Method != http.MethodPost {
				t.Fatalf("heartbeat method = %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode heartbeat body: %v", err)
			}
			mu.Lock()
			heartbeatStatus, _ = body["status"].(string)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	dir := t.TempDir()
	wrapper := newContentPackEdgeCollectorWrapper(client, zap.NewNop(), contentPackEdgeCollectorOptions{
		TenantID:     "tenant-1",
		CollectorID:  "edge-1",
		Kind:         "otel",
		Version:      "test",
		Token:        token,
		ConfigPath:   filepath.Join(dir, "otel.yaml"),
		StateFile:    filepath.Join(dir, "state.json"),
		ApplyTimeout: time.Second,
	})
	if err := wrapper.syncOnce(context.Background()); err != nil {
		t.Fatalf("sync once: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "otel.yaml"))
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if string(data) != desiredYAML {
		t.Fatalf("written config = %q", string(data))
	}
	if wrapper.state.RunningConfigVersion != configVersion || wrapper.state.LastError != "" {
		t.Fatalf("state after apply = %#v", wrapper.state)
	}
	mu.Lock()
	defer mu.Unlock()
	if applyStatus != contentPackApplyStatusDeployed || applyVersion != configVersion {
		t.Fatalf("apply result status=%q version=%q", applyStatus, applyVersion)
	}
	if heartbeatStatus != contentPackCollectorStatusOK {
		t.Fatalf("heartbeat status = %q", heartbeatStatus)
	}
}

func TestContentPackEdgeCollectorWrapperValidationFailureReportsFailed(t *testing.T) {
	t.Setenv("C1_CONTENT_PACK_HELPER", "1")
	t.Setenv("C1_CONTENT_PACK_HELPER_EXIT", "7")
	desiredYAML := "receivers:\n  filelog/new: {}\n"
	configVersion := contentPackRenderedConfigVersion(desiredYAML)
	token := "c1ec_test_token"

	var mu sync.Mutex
	var applyStatus string
	var applyError string
	var heartbeatStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ControlOne-Collector-Token") != token {
			t.Fatalf("missing collector token")
		}
		switch r.URL.Path {
		case "/api/v1/content-packs/collectors/edge-1/desired-config":
			_ = json.NewEncoder(w).Encode(contentPackDesiredConfigResponse{
				TenantID:      "tenant-1",
				CollectorID:   "edge-1",
				CandidateID:   "candidate-1",
				ConfigVersion: configVersion,
				YAML:          desiredYAML,
			})
		case "/api/v1/content-packs/collectors/edge-1/apply-result":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode apply body: %v", err)
			}
			mu.Lock()
			applyStatus = body["status"]
			applyError = body["error"]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		case "/api/v1/content-packs/collectors/edge-1/heartbeat":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode heartbeat body: %v", err)
			}
			mu.Lock()
			heartbeatStatus, _ = body["status"].(string)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "otel.yaml")
	if err := os.WriteFile(configPath, []byte("old-config\n"), 0o600); err != nil {
		t.Fatalf("write old config: %v", err)
	}
	wrapper := newContentPackEdgeCollectorWrapper(client, zap.NewNop(), contentPackEdgeCollectorOptions{
		TenantID:        "tenant-1",
		CollectorID:     "edge-1",
		Kind:            "otel",
		Token:           token,
		ConfigPath:      configPath,
		StateFile:       filepath.Join(dir, "state.json"),
		ApplyTimeout:    time.Second,
		ValidateCommand: contentPackCollectorHelperCommand(),
	})
	if err := wrapper.syncOnce(context.Background()); err == nil {
		t.Fatal("sync once succeeded, want validation failure")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after failure: %v", err)
	}
	if string(data) != "old-config\n" {
		t.Fatalf("config was overwritten after validation failure: %q", string(data))
	}
	mu.Lock()
	defer mu.Unlock()
	if applyStatus != contentPackApplyStatusFailed {
		t.Fatalf("apply status = %q", applyStatus)
	}
	if applyError == "" {
		t.Fatal("apply error was empty")
	}
	if heartbeatStatus != contentPackCollectorStatusWarn {
		t.Fatalf("heartbeat status = %q", heartbeatStatus)
	}
}

func TestContentPackEdgeCollectorWrapperSupervisesCollectorProcess(t *testing.T) {
	t.Setenv("C1_CONTENT_PACK_HELPER", "1")
	t.Setenv("C1_CONTENT_PACK_HELPER_WAIT", "1")
	desiredYAML := "receivers:\n  filelog/supervised: {}\n"
	configVersion := contentPackRenderedConfigVersion(desiredYAML)
	token := "c1ec_test_token"

	var mu sync.Mutex
	var heartbeat map[string]any
	var applyStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ControlOne-Collector-Token") != token {
			t.Fatalf("missing collector token")
		}
		switch r.URL.Path {
		case "/api/v1/content-packs/collectors/edge-1/desired-config":
			_ = json.NewEncoder(w).Encode(contentPackDesiredConfigResponse{
				TenantID:      "tenant-1",
				CollectorID:   "edge-1",
				CandidateID:   "candidate-1",
				ConfigVersion: configVersion,
				YAML:          desiredYAML,
			})
		case "/api/v1/content-packs/collectors/edge-1/apply-result":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode apply body: %v", err)
			}
			mu.Lock()
			applyStatus = body["status"]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		case "/api/v1/content-packs/collectors/edge-1/heartbeat":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode heartbeat body: %v", err)
			}
			mu.Lock()
			heartbeat = body
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	dir := t.TempDir()
	wrapper := newContentPackEdgeCollectorWrapper(client, zap.NewNop(), contentPackEdgeCollectorOptions{
		TenantID:         "tenant-1",
		CollectorID:      "edge-1",
		Kind:             "otel",
		Token:            token,
		ConfigPath:       filepath.Join(dir, "otel.yaml"),
		StateFile:        filepath.Join(dir, "state.json"),
		ApplyTimeout:     time.Second,
		SuperviseCommand: contentPackCollectorHelperCommand(),
	})
	t.Cleanup(wrapper.stopSupervisedCollector)
	if err := wrapper.syncOnce(context.Background()); err != nil {
		t.Fatalf("sync once: %v", err)
	}
	if wrapper.proc == nil || !boolFromAny(wrapper.proc.Snapshot()["running"]) {
		t.Fatalf("supervised process snapshot = %#v", wrapper.proc.Snapshot())
	}
	mu.Lock()
	defer mu.Unlock()
	if applyStatus != contentPackApplyStatusDeployed {
		t.Fatalf("apply status = %q", applyStatus)
	}
	if heartbeat["status"] != contentPackCollectorStatusOK {
		t.Fatalf("heartbeat = %#v", heartbeat)
	}
	health, _ := heartbeat["health"].(map[string]any)
	wrapperHealth, _ := health["wrapper"].(map[string]any)
	process, _ := wrapperHealth["process"].(map[string]any)
	if process["managed"] != true || process["running"] != true || process["state"] != "running" {
		t.Fatalf("process heartbeat = %#v", process)
	}
}

func TestContentPackEdgeCollectorWrapperSuperviseStartFailureReportsFailed(t *testing.T) {
	desiredYAML := "receivers:\n  filelog/supervised: {}\n"
	configVersion := contentPackRenderedConfigVersion(desiredYAML)
	token := "c1ec_test_token"

	var mu sync.Mutex
	var applyStatus string
	var applyError string
	var heartbeatStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ControlOne-Collector-Token") != token {
			t.Fatalf("missing collector token")
		}
		switch r.URL.Path {
		case "/api/v1/content-packs/collectors/edge-1/desired-config":
			_ = json.NewEncoder(w).Encode(contentPackDesiredConfigResponse{
				TenantID:      "tenant-1",
				CollectorID:   "edge-1",
				CandidateID:   "candidate-1",
				ConfigVersion: configVersion,
				YAML:          desiredYAML,
			})
		case "/api/v1/content-packs/collectors/edge-1/apply-result":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode apply body: %v", err)
			}
			mu.Lock()
			applyStatus = body["status"]
			applyError = body["error"]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		case "/api/v1/content-packs/collectors/edge-1/heartbeat":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode heartbeat body: %v", err)
			}
			mu.Lock()
			heartbeatStatus, _ = body["status"].(string)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	dir := t.TempDir()
	wrapper := newContentPackEdgeCollectorWrapper(client, zap.NewNop(), contentPackEdgeCollectorOptions{
		TenantID:         "tenant-1",
		CollectorID:      "edge-1",
		Kind:             "otel",
		Token:            token,
		ConfigPath:       filepath.Join(dir, "otel.yaml"),
		StateFile:        filepath.Join(dir, "state.json"),
		ApplyTimeout:     time.Second,
		SuperviseCommand: []string{"control-one-missing-collector-binary-for-test"},
	})
	if err := wrapper.syncOnce(context.Background()); err == nil {
		t.Fatal("sync once succeeded, want supervise start failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if applyStatus != contentPackApplyStatusFailed {
		t.Fatalf("apply status = %q", applyStatus)
	}
	if applyError == "" {
		t.Fatal("apply error was empty")
	}
	if heartbeatStatus != contentPackCollectorStatusWarn {
		t.Fatalf("heartbeat status = %q", heartbeatStatus)
	}
}

func TestContentPackEdgeCollectorWrapperScrapesReceiverMetrics(t *testing.T) {
	token := "c1ec_test_token"
	var mu sync.Mutex
	var heartbeat map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte(`
# HELP otelcol_receiver_accepted_log_records Number of log records successfully pushed into the pipeline.
otelcol_receiver_accepted_log_records{receiver="filelog/controlone.server_pack"} 42
otelcol_receiver_refused_log_records{receiver="filelog/controlone.server_pack"} 1
otelcol_exporter_queue_size{exporter="otlp/controlone"} 3
`))
		case "/api/v1/content-packs/collectors/edge-1/desired-config":
			http.NotFound(w, r)
		case "/api/v1/content-packs/collectors/edge-1/heartbeat":
			if r.Header.Get("X-ControlOne-Collector-Token") != token {
				t.Fatalf("missing collector token")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode heartbeat body: %v", err)
			}
			mu.Lock()
			heartbeat = body
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	dir := t.TempDir()
	wrapper := newContentPackEdgeCollectorWrapper(client, zap.NewNop(), contentPackEdgeCollectorOptions{
		TenantID:        "tenant-1",
		CollectorID:     "edge-1",
		Kind:            "otel",
		Token:           token,
		ConfigPath:      filepath.Join(dir, "otel.yaml"),
		StateFile:       filepath.Join(dir, "state.json"),
		MetricsEndpoint: server.URL + "/metrics",
		MetricsTimeout:  time.Second,
	})
	if err := wrapper.syncOnce(context.Background()); err != nil {
		t.Fatalf("sync once: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if heartbeat["status"] != contentPackCollectorStatusWarn {
		t.Fatalf("heartbeat status = %v, want degraded due to drops/queue; heartbeat=%#v", heartbeat["status"], heartbeat)
	}
	health, _ := heartbeat["health"].(map[string]any)
	receivers, _ := health["receivers"].(map[string]any)
	rawReceiver, ok := receivers["filelog/controlone.server_pack"]
	if !ok {
		t.Fatalf("receiver health missing: %#v", receivers)
	}
	receiver, _ := rawReceiver.(map[string]any)
	if receiver["source_id"] != "controlone.server_pack" || receiver["state"] != "backpressured" {
		t.Fatalf("receiver = %#v", receiver)
	}
	if receiver["events_received"] != float64(42) || receiver["events_dropped"] != float64(1) || receiver["queue_depth"] != float64(3) {
		t.Fatalf("receiver metrics = %#v", receiver)
	}
}

func TestParseContentPackCollectorPrometheusMetrics(t *testing.T) {
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	t.Run("otel receiver and scraper counters", func(t *testing.T) {
		raw := strings.NewReader(`
otelcol_receiver_accepted_log_records{receiver="filelog/controlone.server_pack"} 12
otelcol_receiver_refused_log_records{receiver="filelog/controlone.server_pack"} 0
otelcol_receiver_accepted_metric_points{receiver="prometheus/controlone.prometheus"} 9
otelcol_scraper_scraped_metric_points{scraper="prometheus/controlone.hostmetrics"} 7
`)
		receivers, ok, err := parseContentPackCollectorPrometheusMetrics(raw, now)
		if err != nil {
			t.Fatalf("parse metrics: %v", err)
		}
		if !ok {
			t.Fatal("metrics should be healthy")
		}
		receiver := receivers["filelog/controlone.server_pack"].(map[string]any)
		if receiver["source_id"] != "controlone.server_pack" || receiver["state"] != "collecting" || receiver["events_received"] != int64(12) {
			t.Fatalf("receiver = %#v", receiver)
		}
		prometheus := receivers["prometheus/controlone.prometheus"].(map[string]any)
		if prometheus["source_id"] != "controlone.prometheus" || prometheus["events_received"] != int64(9) {
			t.Fatalf("prometheus receiver = %#v", prometheus)
		}
		hostmetrics := receivers["prometheus/controlone.hostmetrics"].(map[string]any)
		if hostmetrics["source_id"] != "controlone.hostmetrics" || hostmetrics["events_received"] != int64(7) {
			t.Fatalf("hostmetrics scraper = %#v", hostmetrics)
		}
	})

	t.Run("otel total suffix counters", func(t *testing.T) {
		raw := strings.NewReader(`
otelcol_receiver_accepted_log_records_total{receiver="splunk_hec/controlone.okta_system_log"} 5
otelcol_receiver_refused_log_records_total{receiver="splunk_hec/controlone.okta_system_log"} 1
otelcol_exporter_send_failed_log_records_total{exporter="otlp/controlone"} 2
`)
		receivers, ok, err := parseContentPackCollectorPrometheusMetrics(raw, now)
		if err != nil {
			t.Fatalf("parse metrics: %v", err)
		}
		if ok {
			t.Fatal("metrics should be degraded due to refused records and exporter failures")
		}
		receiver := receivers["splunk_hec/controlone.okta_system_log"].(map[string]any)
		if receiver["source_id"] != "controlone.okta_system_log" || receiver["state"] != "backpressured" {
			t.Fatalf("receiver = %#v", receiver)
		}
		if receiver["events_received"] != int64(5) || receiver["events_dropped"] != int64(1) || receiver["retry_count"] != int64(2) {
			t.Fatalf("receiver metrics = %#v", receiver)
		}
	})

	t.Run("vector component counters", func(t *testing.T) {
		raw := strings.NewReader(`
component_received_events_total{component_id="controlone.server_pack",component_kind="source"} 10
component_sent_events_total{component_id="controlone.server_pack",component_kind="source"} 8
component_discarded_events_total{component_id="controlone.server_pack",component_kind="source"} 1
component_errors_total{component_id="controlone.server_pack",component_kind="source"} 2
`)
		receivers, ok, err := parseContentPackCollectorPrometheusMetrics(raw, now)
		if err != nil {
			t.Fatalf("parse metrics: %v", err)
		}
		if ok {
			t.Fatal("metrics should be degraded due to Vector component errors")
		}
		receiver := receivers["vector/controlone.server_pack"].(map[string]any)
		if receiver["source_id"] != "controlone.server_pack" || receiver["state"] != "backpressured" {
			t.Fatalf("receiver = %#v", receiver)
		}
		if receiver["events_received"] != int64(10) || receiver["events_parsed"] != int64(8) || receiver["events_dropped"] != int64(1) || receiver["retry_count"] != int64(2) {
			t.Fatalf("receiver metrics = %#v", receiver)
		}
		if receiver["last_error"] != "vector component errors reported" {
			t.Fatalf("last_error = %#v", receiver["last_error"])
		}
	})

	t.Run("vector fractional buffer utilization degrades source", func(t *testing.T) {
		raw := strings.NewReader(`
component_received_events_total{component_id="controlone.server_pack",component_kind="source"} 10
source_buffer_utilization_level{component_id="controlone.server_pack",component_kind="source"} 0.73
`)
		receivers, ok, err := parseContentPackCollectorPrometheusMetrics(raw, now)
		if err != nil {
			t.Fatalf("parse metrics: %v", err)
		}
		if ok {
			t.Fatal("metrics should be degraded due to non-zero Vector buffer utilization")
		}
		receiver := receivers["vector/controlone.server_pack"].(map[string]any)
		if receiver["state"] != "backpressured" || receiver["queue_depth"] != int64(1) {
			t.Fatalf("receiver = %#v", receiver)
		}
	})

	t.Run("fluent bit input and output counters", func(t *testing.T) {
		raw := strings.NewReader(`
fluentbit_input_records_total{name="controlone.server_pack"} 25
fluentbit_input_ring_buffer_retries_total{name="controlone.server_pack"} 1
fluentbit_output_dropped_records_total{name="controlone"} 2
fluentbit_output_retries_failed_total{name="controlone"} 3
`)
		receivers, ok, err := parseContentPackCollectorPrometheusMetrics(raw, now)
		if err != nil {
			t.Fatalf("parse metrics: %v", err)
		}
		if ok {
			t.Fatal("metrics should be degraded due to Fluent Bit drops/retries")
		}
		receiver := receivers["fluentbit/controlone.server_pack"].(map[string]any)
		if receiver["source_id"] != "controlone.server_pack" || receiver["state"] != "backpressured" {
			t.Fatalf("receiver = %#v", receiver)
		}
		if receiver["events_received"] != int64(25) || receiver["events_dropped"] != int64(2) || receiver["retry_count"] != int64(4) {
			t.Fatalf("receiver metrics = %#v", receiver)
		}
		if receiver["last_error"] != "collector output errors or retries reported" {
			t.Fatalf("last_error = %#v", receiver["last_error"])
		}
	})
}

func contentPackCollectorHelperCommand() []string {
	return []string{os.Args[0], "-test.run=TestContentPackCollectorHelperProcess", "--"}
}

func TestContentPackCollectorHelperProcess(t *testing.T) {
	if os.Getenv("C1_CONTENT_PACK_HELPER") != "1" {
		return
	}
	if os.Getenv("C1_CONTENT_PACK_HELPER_WAIT") == "1" {
		for {
			time.Sleep(time.Second)
		}
	}
	code, _ := strconv.Atoi(os.Getenv("C1_CONTENT_PACK_HELPER_EXIT"))
	if code == 0 {
		code = 1
	}
	_, _ = os.Stderr.WriteString("content pack helper forced failure")
	os.Exit(code)
}
