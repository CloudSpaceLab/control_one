package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeSecurityEventsRepresentativeSources(t *testing.T) {
	tenantID, nodeID := uuid.New(), uuid.New()
	tests := []struct {
		name      string
		event     IngestedEvent
		wantTypes []string
		want      map[string]any
	}{
		{name: "linux sshd failure", event: logEvent("sshd", "/var/log/auth.log", "Failed password for invalid user root from 203.0.113.25 port 54321 ssh2", nil), wantTypes: []string{"ssh.authentication_failure", "authentication.failure"}, want: map[string]any{"src_ip": "203.0.113.25", "src_port": 54321, "dst_port": 22, "user_name": "root", "auth_result": "failure", "source_os": "linux", "source_channel": "/var/log/auth.log"}},
		{name: "windows security failure", event: logEvent("Microsoft-Windows-Security-Auditing", "Security", "An account failed to log on", map[string]any{"EventID": "4625", "IpAddress": "198.51.100.9", "IpPort": "51514", "TargetUserName": "BANK\\alice"}), wantTypes: []string{"windows.authentication_failure", "authentication.failure"}, want: map[string]any{"src_ip": "198.51.100.9", "src_port": 51514, "user_name": "BANK\\alice", "auth_result": "failure", "source_os": "windows", "source_channel": "Security", "source_event_id": "4625"}},
		{name: "windows hello failure", event: logEvent("Microsoft-Windows-HelloForBusiness", "Microsoft-Windows-HelloForBusiness/Operational", "A user failed to sign into the device with the following information:\n\nUsername: SYSTEM\nCredential Type: Software Key\nAuthentication Error Status: 0xC000006D\nAuthentication Error Substatus: 0xC0000380", map[string]any{"EventID": 7001}), wantTypes: []string{"windows.authentication_failure", "authentication.failure"}, want: map[string]any{"user_name": "SYSTEM", "auth_status": "0xC000006D", "auth_substatus": "0xC0000380", "credential_type": "Software Key", "auth_result": "failure", "source_os": "windows", "source_channel": "Microsoft-Windows-HelloForBusiness/Operational", "source_event_id": "7001"}},
		{name: "windows biometric mismatch", event: logEvent("Microsoft-Windows-Biometrics", "Microsoft-Windows-Biometrics/Operational", "The Windows Biometric Service could not determine the identity of the sample from sensor: ELAN WBF Fingerprint Sensor", map[string]any{"EventID": 1005, "RecordId": 41}), wantTypes: []string{"windows.authentication_failure", "authentication.failure"}, want: map[string]any{"user_name": "unknown", "auth_result": "failure", "source_os": "windows", "source_channel": "Microsoft-Windows-Biometrics/Operational", "source_event_id": "1005", "source_record_id": "41", "sensor_name": "ELAN WBF Fingerprint Sensor"}},
		{name: "nginx access", event: logEvent("nginx", "/var/log/nginx/access.log", `192.0.2.4 - - [15/Sep/2026:10:00:00 +0100] "GET /.env HTTP/1.1" 404 123`, nil), wantTypes: []string{"web.request"}, want: map[string]any{"src_ip": "192.0.2.4", "path": "/.env", "status_code": 404, "dst_port": 80}},
		{name: "apache access", event: logEvent("apache2", "/var/log/apache2/access.log", `192.0.2.5 - - [15/Sep/2026:10:00:00 +0100] "POST /login HTTP/1.1" 503 42`, nil), wantTypes: []string{"web.request"}, want: map[string]any{"http_method": "POST", "status_code": 503}},
		{name: "iis access", event: logEvent("iis", "W3SVC1", `2026-09-15 10:00:00 10.0.0.5 GET /health - 443 - 192.0.2.6 Mozilla/5.0 - 200 0 0 12`, nil), wantTypes: []string{"web.request"}, want: map[string]any{"src_ip": "192.0.2.6", "path": "/health", "dst_port": 443}},
		{name: "reverse proxy and waf", event: logEvent("haproxy-waf", "https_access.log", `192.0.2.7 - - [15/Sep/2026:10:00:00 +0100] "GET /wp-admin HTTP/1.1" 403 9`, nil), wantTypes: []string{"web.request"}, want: map[string]any{"dst_port": 443, "path": "/wp-admin"}},
		{name: "structured reverse proxy event", event: IngestedEvent{Type: "web.request", TS: time.Now(), SrcIP: "192.0.2.8", Details: map[string]any{"method": "GET", "path": "/login", "status_code": 429, "port": 443}}, wantTypes: []string{"web.request"}, want: map[string]any{"src_ip": "192.0.2.8", "dst_port": 443, "status_code": 429, "protocol": "tcp"}},
		{name: "network collector", event: IngestedEvent{Type: "conn.open", TS: time.Now(), SrcIP: "203.0.113.8", DstIP: "10.0.0.5", DstPort: 22, Protocol: "TCP", BytesOut: 2048, Details: map[string]any{"direction": "outbound"}}, wantTypes: []string{"network.connection"}, want: map[string]any{"dst_port": 22, "protocol": "tcp", "direction": "outbound"}},
		{name: "postgres audit", event: logEvent("postgresql", "postgresql.log", `FATAL: password authentication failed for user "app_user"`, nil), wantTypes: []string{"database.authentication_failure", "authentication.failure"}, want: map[string]any{"user_name": "app_user", "auth_result": "failure"}},
		{name: "finacle authentication", event: logEvent("finacle", "finacle-audit", "Authentication failed for user teller01", map[string]any{"finacle_uid": "teller01"}), wantTypes: []string{"database.authentication_failure", "authentication.failure"}, want: map[string]any{"user_name": "teller01"}},
		{name: "finacle availability", event: logEvent("finacle", "finacle-monitor", "CBS gateway unavailable: upstream timeout", nil), wantTypes: []string{"finacle.operation_failure"}, want: map[string]any{"outcome": "failure"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSecurityEvents(tenantID, nodeID, []IngestedEvent{tc.event})
			if len(got) != len(tc.wantTypes) {
				t.Fatalf("got %d events, want %d: %#v", len(got), len(tc.wantTypes), got)
			}
			for i, wantType := range tc.wantTypes {
				if got[i].Type != "security.event" || got[i].Details["event_type"] != wantType {
					t.Fatalf("event %d = %#v, want %s", i, got[i], wantType)
				}
			}
			for key, want := range tc.want {
				if value := got[0].Details[key]; value != want {
					t.Errorf("details[%s] = %#v, want %#v", key, value, want)
				}
			}
			if got[0].Details["normalized"] == nil {
				t.Error("normalized canonical fields missing")
			}
		})
	}
}

func TestWindowsSecurityMessageFieldsAreExtractedWithoutStructuredFields(t *testing.T) {
	event := logEvent("Microsoft-Windows-Security-Auditing", "Security", `An account failed to log on.

Account For Which Logon Failed:
    Account Name: alice

Failure Information:
    Status: 0xC000006D

Network Information:
    Source Network Address: 192.0.2.55
    Source Port: 51514

Logon Type: 3`, map[string]any{"EventID": 4625})
	got := normalizeSecurityEvents(uuid.New(), uuid.New(), []IngestedEvent{event})
	if len(got) != 2 || got[0].Details["event_type"] != "windows.authentication_failure" {
		t.Fatalf("normalized events = %#v", got)
	}
	for field, want := range map[string]any{"user_name": "alice", "src_ip": "192.0.2.55", "src_port": 51514, "logon_type": "3", "auth_result": "failure"} {
		if got[0].Details[field] != want {
			t.Errorf("details[%s] = %#v, want %#v", field, got[0].Details[field], want)
		}
	}
}

func TestSecurityNormalizationParserFixtures(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "security_normalization")
	readText := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(raw)
	}
	readFields := func(name string) map[string]any {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal([]byte(readText(name)), &fields); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		return fields
	}
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	fixtures := []struct {
		name                 string
		event                IngestedEvent
		wantType, wantSource string
	}{
		{"linux sshd", logEvent("sshd", "/var/log/auth.log", readText("linux_sshd.log"), nil), "ssh.authentication_failure", "linux.sshd"},
		{"nginx", logEvent("nginx", "/var/log/nginx/access.log", readText("nginx_access.log"), nil), "web.request", "web.nginx"},
		{"apache", logEvent("apache2", "/var/log/apache2/access.log", readText("apache_access.log"), nil), "web.request", "web.apache"},
		{"windows", logEvent("Microsoft-Windows-Security-Auditing", "Security", "An account failed to log on", readFields("windows_4625.json")), "windows.authentication_failure", "windows.security"},
		{"iis", logEvent("iis", "W3SVC1", readText("iis_w3c.log"), nil), "web.request", "web.iis"},
		{"haproxy", logEvent("haproxy", "https_access.log", readText("haproxy_access.log"), nil), "web.request", "web.reverse_proxy"},
		{"waf", IngestedEvent{Type: "web.request", TS: now, Collector: "waf", Details: readFields("waf_event.json"), DedupKey: uuid.NewString()}, "web.request", "web.waf"},
		{"network", IngestedEvent{Type: "conn.open", TS: now, Collector: "network", Details: readFields("network_connection.json"), DedupKey: uuid.NewString()}, "network.connection", "network.connection_collector"},
		{"postgres", logEvent("postgresql", "postgresql.log", readText("postgres.log"), nil), "database.authentication_failure", "database.postgresql"},
		{"mysql", logEvent("mysql", "mysql-error.log", readText("mysql.log"), nil), "database.authentication_failure", "database.mysql"},
		{"sql server", logEvent("sqlserver", "errorlog", readText("sqlserver.log"), nil), "database.authentication_failure", "database.sqlserver"},
		{"oracle", logEvent("oracle", "audit.log", readText("oracle.log"), map[string]any{"username": "SYSTEM"}), "database.authentication_failure", "database.oracle"},
		{"finacle", logEvent("finacle", "finacle-audit", readText("finacle.log"), map[string]any{"finacle_uid": "teller01"}), "database.authentication_failure", "finacle.monitoring"},
	}
	tenantID, nodeID := uuid.New(), uuid.New()
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if fixture.event.TS.IsZero() {
				fixture.event.TS = now
			}
			got := normalizeSecurityEvents(tenantID, nodeID, []IngestedEvent{fixture.event})
			if len(got) == 0 {
				t.Fatal("no normalized events")
			}
			if got[0].Details["event_type"] != fixture.wantType {
				t.Fatalf("event_type=%v want %s", got[0].Details["event_type"], fixture.wantType)
			}
			if got[0].Details["source"] != fixture.wantSource {
				t.Fatalf("source=%v want %s", got[0].Details["source"], fixture.wantSource)
			}
			for _, field := range []string{"node_id", "timestamp", "outcome"} {
				if got[0].Details[field] == "" {
					t.Errorf("missing %s", field)
				}
			}
		})
	}
}

func TestInvalidRecognizedRecordProducesObservableError(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "security_normalization", "invalid_nginx.log"))
	if err != nil {
		t.Fatal(err)
	}
	event := logEvent("nginx", "/var/log/nginx/access.log", string(raw), nil)
	got := normalizeSecurityEvents(uuid.New(), uuid.New(), []IngestedEvent{event})
	if len(got) != 1 {
		t.Fatalf("got %d events, want one error", len(got))
	}
	if got[0].ParserStatus != "error" || got[0].Details["event_type"] != "security.normalization_error" {
		t.Fatalf("error event = %#v", got[0])
	}
	if got[0].Details["error"] == "" {
		t.Fatal("observable error reason missing")
	}
}

func TestRequiredNormalizedFieldsAreValidated(t *testing.T) {
	now := time.Now().UTC()
	validBase := func(eventType string) IngestedEvent {
		return IngestedEvent{Type: "security.event", TS: now, Details: map[string]any{"event_type": eventType, "source": "test", "node_id": uuid.NewString(), "timestamp": now.Format(time.RFC3339Nano), "outcome": "failure"}}
	}
	tests := []struct {
		name, eventType string
		fields          map[string]any
	}{
		{"ssh missing source ip", "ssh.authentication_failure", map[string]any{"dst_port": 22, "protocol": "tcp", "user_name": "root", "auth_result": "failure"}},
		{"windows missing user", "windows.authentication_failure", map[string]any{"auth_result": "failure"}},
		{"database missing user", "database.authentication_failure", map[string]any{"auth_result": "failure"}},
		{"web missing status", "web.request", map[string]any{"src_ip": "192.0.2.1", "dst_port": 443, "protocol": "tcp", "http_method": "GET", "path": "/"}},
		{"network missing destination", "network.connection", map[string]any{"src_ip": "192.0.2.1", "dst_port": 22, "protocol": "tcp"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := validBase(tc.eventType)
			for key, value := range tc.fields {
				event.Details[key] = value
			}
			if err := validateNormalizedSecurityEvent(&event); err == nil {
				t.Fatal("expected required-field error")
			}
		})
	}
}

func TestNormalizedFixtureContractsMatchPhase3Templates(t *testing.T) {
	tenantID, nodeID := uuid.New(), uuid.New()
	assert := func(name string, event IngestedEvent, eventType string, fields map[string]any) {
		t.Helper()
		got := normalizeSecurityEvents(tenantID, nodeID, []IngestedEvent{event})
		for _, normalized := range got {
			if normalized.Details["event_type"] != eventType {
				continue
			}
			for field, want := range fields {
				if normalized.Details[field] != want {
					t.Fatalf("%s: %s=%#v want %#v", name, field, normalized.Details[field], want)
				}
			}
			return
		}
		t.Fatalf("%s: normalized type %s not produced", name, eventType)
	}
	assert("ssh brute force", logEvent("sshd", "/var/log/auth.log", "Failed password for root from 203.0.113.25 port 50124 ssh2", nil), "ssh.authentication_failure", map[string]any{"dst_port": 22, "protocol": "tcp", "auth_result": "failure"})
	assert("credential stuffing", logEvent("sshd", "/var/log/auth.log", "Failed password for root from 203.0.113.25 port 50124 ssh2", nil), "authentication.failure", map[string]any{"src_ip": "203.0.113.25", "user_name": "root", "auth_result": "failure"})
	assert("windows failures", logEvent("Microsoft-Windows-Security-Auditing", "Security", "failure", map[string]any{"EventID": 4625, "TargetUserName": "alice"}), "windows.authentication_failure", map[string]any{"user_name": "alice", "auth_result": "failure"})
	assert("web scanner and flood", logEvent("nginx", "https_access.log", `192.0.2.2 - - [15/Sep/2026:10:00:00 +0100] "GET /.env HTTP/1.1" 500 1`, nil), "web.request", map[string]any{"src_ip": "192.0.2.2", "dst_port": 443, "protocol": "tcp", "path": "/.env", "status_code": 500})
	assert("database failures", logEvent("postgresql", "postgres.log", `password authentication failed for user "app"`, nil), "database.authentication_failure", map[string]any{"user_name": "app", "auth_result": "failure"})
	assert("port scan", IngestedEvent{Type: "conn.open", TS: time.Now(), SrcIP: "192.0.2.3", DstIP: "10.0.0.2", DstPort: 22, Protocol: "tcp"}, "network.connection", map[string]any{"src_ip": "192.0.2.3", "dst_port": 22, "protocol": "tcp"})
}

func TestNormalizeSecurityEventsIgnoresUnrelatedLog(t *testing.T) {
	event := logEvent("systemd", "journal", "Started Daily apt download activities", nil)
	if got := normalizeSecurityEvents(uuid.New(), uuid.New(), []IngestedEvent{event}); len(got) != 0 {
		t.Fatalf("got %#v, want no security signals", got)
	}
}

func logEvent(program, source, message string, fields map[string]any) IngestedEvent {
	return IngestedEvent{Type: "log.line", TS: time.Now(), Message: message, Details: map[string]any{"program": program, "source": source, "fields": fields}, DedupKey: uuid.NewString()}
}
