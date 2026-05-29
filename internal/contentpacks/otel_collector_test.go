package contentpacks

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildOTelCollectorConfigRendersDeterministicYAML(t *testing.T) {
	fileSource := testResolvedOTelSource("app.access", CollectorOTelFileLog, "filelog", map[string]any{
		"include": []any{"/var/log/app/access.log", "/var/log/app/access.*.log"},
	})
	syslogSource := testResolvedOTelSource("network.firewall", CollectorSyslog, "syslog", map[string]any{
		"transport":      "udp",
		"listen_address": "0.0.0.0:5514",
		"protocol":       "rfc3164",
	})
	opts := OTelCollectorConfigOptions{
		Endpoint:    "controlone.local:4317",
		TenantID:    "tenant-a",
		CollectorID: "collector-edge-1",
		Headers: map[string]string{
			"x-controlone-token": "redacted",
		},
		InsecureTLS: true,
	}

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{
		{Source: syslogSource},
		{Source: fileSource},
	}, opts)
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig() error = %v", err)
	}
	rendered, err := RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		t.Fatalf("RenderOTelCollectorConfigYAML() error = %v", err)
	}

	reversed, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{
		{Source: fileSource},
		{Source: syslogSource},
	}, opts)
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig(reversed) error = %v", err)
	}
	renderedReversed, err := RenderOTelCollectorConfigYAML(reversed)
	if err != nil {
		t.Fatalf("RenderOTelCollectorConfigYAML(reversed) error = %v", err)
	}
	if !bytes.Equal(rendered, renderedReversed) {
		t.Fatalf("rendered YAML is not deterministic\nfirst:\n%s\nsecond:\n%s", rendered, renderedReversed)
	}

	text := string(rendered)
	for _, want := range []string{
		"filelog/controlone.app.access:",
		"syslog/controlone.network.firewall:",
		"logs/controlone.app.access:",
		"logs/controlone.network.firewall:",
		"resource/controlone.source.app.access:",
		"file_storage/controlone:",
		"storage: file_storage/controlone",
		"sending_queue:",
		"retry_on_failure:",
		"x-controlone-token: redacted",
		"listen_address: 0.0.0.0:5514",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, text)
		}
	}
	if len(plan.Sources) != 2 || plan.Sources[0].SourceID != "app.access" {
		t.Fatalf("sources not sorted by source_id: %#v", plan.Sources)
	}
	version := OTelCollectorConfigVersion(rendered)
	if !strings.HasPrefix(version, "sha256:") || len(version) != len("sha256:")+64 {
		t.Fatalf("config version = %q, want sha256 digest", version)
	}
	if got := OTelCollectorConfigVersion(renderedReversed); got != version {
		t.Fatalf("reversed config version = %q, want %q", got, version)
	}
}

func TestBuildOTelCollectorConfigRequiresApprovalForSensitiveSources(t *testing.T) {
	source := testResolvedOTelSource("db.audit", CollectorOTelFileLog, "filelog", map[string]any{
		"include": []string{"/var/log/postgresql/audit.log"},
	})
	source.Source.RiskClass = RiskHigh
	source.Source.DataSensitivity = SensitivityHigh
	source.Source.ApprovalRequired = true

	_, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint: "controlone.local:4317",
	})
	if err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("BuildOTelCollectorConfig() error = %v, want approval error", err)
	}

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source, ApprovalRef: "CAB-1234"}}, OTelCollectorConfigOptions{
		Endpoint: "controlone.local:4317",
	})
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig(approved) error = %v", err)
	}
	if got := plan.Sources[0].ApprovalRef; got != "CAB-1234" {
		t.Fatalf("ApprovalRef = %q", got)
	}
}

func TestBuildOTelCollectorConfigRendersParsedOnlyRawBodyRedaction(t *testing.T) {
	source := testResolvedOTelSource("app.audit", CollectorOTelFileLog, "filelog", map[string]any{
		"include": []string{"/var/log/app/audit.log"},
		"operators": []map[string]any{{
			"type": "json_parser",
		}},
	})

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{
		Source:      source,
		CollectMode: OTelCollectModeCollectParsed,
	}}, OTelCollectorConfigOptions{
		Endpoint: "controlone.local:4317",
	})
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig() error = %v", err)
	}
	if got := plan.Sources[0].CollectMode; got != OTelCollectModeCollectParsed {
		t.Fatalf("CollectMode = %q", got)
	}
	pipeline := plan.Config.Service.Pipelines["logs/controlone.app.audit"]
	if !containsString(pipeline.Processors, "transform/controlone.source.app.audit.redact_raw") {
		t.Fatalf("pipeline processors = %#v", pipeline.Processors)
	}
	rendered, err := RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		t.Fatalf("RenderOTelCollectorConfigYAML() error = %v", err)
	}
	text := string(rendered)
	for _, want := range []string{
		"transform/controlone.source.app.audit.redact_raw:",
		`set(attributes["control_one.raw_message_retained"], "false")`,
		`set(body, "raw log omitted by collect_parsed")`,
		"c1.collect_mode",
		"c1.raw_message_retained",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, text)
		}
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "collect_parsed") {
		t.Fatalf("warnings = %#v", plan.Warnings)
	}
}

func TestBuildOTelCollectorConfigRendersWindowsEventChannels(t *testing.T) {
	// Old packs may still declare the deprecated receiver alias. Rendering
	// should normalize it to the current OTel contrib receiver name.
	source := testResolvedOTelSource("windows.security", CollectorWindowsEvent, "windowseventlog", map[string]any{
		"channels": []string{"Security", "Microsoft-Windows-Sysmon/Operational"},
	})

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint:    "controlone.local:4317",
		CollectorID: "collector-windows-1",
	})
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig() error = %v", err)
	}
	if len(plan.Sources) != 1 {
		t.Fatalf("sources = %d", len(plan.Sources))
	}
	gotReceivers := plan.Sources[0].ReceiverIDs
	if len(gotReceivers) != 2 {
		t.Fatalf("receiver ids = %#v, want 2", gotReceivers)
	}
	for _, want := range []string{
		"windows_event_log/controlone.windows.security.microsoft-windows-sysmon_operational",
		"windows_event_log/controlone.windows.security.security",
	} {
		if !containsString(gotReceivers, want) {
			t.Fatalf("receiver ids = %#v, missing %s", gotReceivers, want)
		}
	}
	pipeline := plan.Config.Service.Pipelines["logs/controlone.windows.security"]
	if len(pipeline.Receivers) != 2 {
		t.Fatalf("pipeline receivers = %#v, want 2", pipeline.Receivers)
	}
}

func TestBuildOTelCollectorConfigRendersWEFForwardedEventsDefault(t *testing.T) {
	source := testResolvedOTelSource("windows.forwarded", CollectorWEF, "", nil)

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint:    "controlone.local:4317",
		CollectorID: "collector-wec-1",
	})
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig() error = %v", err)
	}
	if len(plan.Sources) != 1 {
		t.Fatalf("sources = %d", len(plan.Sources))
	}
	if got := plan.Sources[0].Receiver; got != "windows_event_log" {
		t.Fatalf("receiver = %q, want windows_event_log", got)
	}
	gotReceivers := plan.Sources[0].ReceiverIDs
	want := "windows_event_log/controlone.windows.forwarded"
	if len(gotReceivers) != 1 || gotReceivers[0] != want {
		t.Fatalf("receiver ids = %#v, want [%s]", gotReceivers, want)
	}
	receiver := plan.Config.Receivers[want]
	if receiver["channel"] != "ForwardedEvents" {
		t.Fatalf("receiver channel = %#v, want ForwardedEvents", receiver["channel"])
	}
}

func TestBuildOTelCollectorConfigRendersSyslogTLSIdentityControls(t *testing.T) {
	source := testResolvedOTelSource("network.firewall", CollectorSyslog, "syslog", map[string]any{
		"protocol": "rfc5424",
		"tls": map[string]any{
			"cert_file":      "/etc/control-one/syslog/tls.crt",
			"key_file":       "/etc/control-one/syslog/tls.key",
			"client_ca_file": "/etc/control-one/syslog/client-ca.crt",
		},
		"source_identity":        "fortigate-dc1",
		"source_allowlist_cidrs": []string{"10.10.10.6/32", "10.10.10.5/32"},
		"rate_limit_per_second":  750,
		"rate_limit_burst":       1500,
	})

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint: "controlone.local:4317",
	})
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig() error = %v", err)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for mTLS identity-controlled syslog", plan.Warnings)
	}
	receiverID := "syslog/controlone.network.firewall"
	receiver := plan.Config.Receivers[receiverID]
	if receiver == nil {
		t.Fatalf("receiver %s missing from %#v", receiverID, plan.Config.Receivers)
	}
	tcp, ok := receiver["tcp"].(map[string]any)
	if !ok {
		t.Fatalf("tcp receiver config = %#v", receiver["tcp"])
	}
	if got := tcp["listen_address"]; got != "0.0.0.0:6514" {
		t.Fatalf("listen_address = %#v, want TLS syslog default 0.0.0.0:6514", got)
	}
	if got := tcp["add_attributes"]; got != true {
		t.Fatalf("add_attributes = %#v, want true", got)
	}
	tls, ok := tcp["tls"].(map[string]any)
	if !ok {
		t.Fatalf("tls config = %#v", tcp["tls"])
	}
	if got := tls["client_ca_file"]; got != "/etc/control-one/syslog/client-ca.crt" {
		t.Fatalf("client_ca_file = %#v", got)
	}
	resource, ok := receiver["resource"].(map[string]any)
	if !ok {
		t.Fatalf("resource config = %#v", receiver["resource"])
	}
	if got := resource["control_one.syslog.source_identity"]; got != "fortigate-dc1" {
		t.Fatalf("source identity = %#v", got)
	}
	if got := resource["control_one.syslog.allowlist_cidrs"]; got != "10.10.10.5/32,10.10.10.6/32" {
		t.Fatalf("allowlist_cidrs = %#v", got)
	}
	if got := resource["control_one.syslog.rate_limit_per_second"]; got != "750" {
		t.Fatalf("rate_limit_per_second = %#v", got)
	}
	if len(plan.Sources) != 1 || len(plan.Sources[0].EdgeNetworkPolicies) != 1 {
		t.Fatalf("edge policies = %#v", plan.Sources)
	}
	policy := plan.Sources[0].EdgeNetworkPolicies[0]
	if policy.Kind != "nftables" || policy.Transport != "tcp" || policy.ListenAddress != "0.0.0.0:6514" {
		t.Fatalf("edge policy identity = %#v", policy)
	}
	if policy.RateLimitPerSecond != 750 || policy.RateLimitBurst != 1500 {
		t.Fatalf("edge policy rate limit = %#v", policy)
	}
	if len(policy.AllowlistCIDRs) != 2 || policy.AllowlistCIDRs[0] != "10.10.10.5/32" || policy.AllowlistCIDRs[1] != "10.10.10.6/32" {
		t.Fatalf("edge policy allowlist = %#v", policy.AllowlistCIDRs)
	}
	renderedRules := strings.Join(policy.NftablesRules, "\n")
	for _, want := range []string{
		"tcp dport 6514 ip saddr { 10.10.10.5/32, 10.10.10.6/32 } limit rate over 750/second burst 1500 packets drop",
		"tcp dport 6514 ip saddr { 10.10.10.5/32, 10.10.10.6/32 } accept",
		"tcp dport 6514 drop",
	} {
		if !strings.Contains(renderedRules, want) {
			t.Fatalf("nftables rules = %#v, missing %q", policy.NftablesRules, want)
		}
	}
	for _, forbidden := range []string{"transport", "tls", "source_identity", "source_allowlist_cidrs", "rate_limit_per_second", "rate_limit_burst"} {
		if _, ok := receiver[forbidden]; ok {
			t.Fatalf("receiver leaked Control One-only key %q: %#v", forbidden, receiver)
		}
	}
}

func TestBuildOTelCollectorConfigRejectsSyslogTLSOnUDP(t *testing.T) {
	source := testResolvedOTelSource("network.firewall", CollectorSyslog, "syslog", map[string]any{
		"transport": "udp",
		"tls": map[string]any{
			"cert_file": "/etc/control-one/syslog/tls.crt",
			"key_file":  "/etc/control-one/syslog/tls.key",
		},
	})

	_, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint: "controlone.local:4317",
	})
	if err == nil || !strings.Contains(err.Error(), "TLS requires tcp transport") {
		t.Fatalf("BuildOTelCollectorConfig() error = %v, want TLS-over-UDP rejection", err)
	}
}

func TestBuildOTelCollectorConfigCanDisablePersistentStorage(t *testing.T) {
	source := testResolvedOTelSource("app.access", CollectorOTelFileLog, "filelog", map[string]any{
		"include": []string{"/var/log/app/access.log"},
	})

	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint:                 "controlone.local:4317",
		DisablePersistentStorage: true,
	})
	if err != nil {
		t.Fatalf("BuildOTelCollectorConfig() error = %v", err)
	}
	if _, ok := plan.Config.Extensions[defaultOTelFileStorageExtensionID]; ok {
		t.Fatalf("file storage extension rendered despite DisablePersistentStorage: %#v", plan.Config.Extensions)
	}
	rendered, err := RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		t.Fatalf("RenderOTelCollectorConfigYAML() error = %v", err)
	}
	if strings.Contains(string(rendered), "storage: file_storage/controlone") {
		t.Fatalf("rendered YAML contains persistent storage despite opt-out:\n%s", rendered)
	}
}

func TestBuildOTelCollectorConfigRejectsUnsupportedNodeOnlySource(t *testing.T) {
	source := testResolvedOTelSource("node.only", CollectorNodeFileLog, "", nil)
	source.Source.CollectorRecipes = nil

	_, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{
		Endpoint: "controlone.local:4317",
	})
	if err == nil || !strings.Contains(err.Error(), "no OTel-renderable collector recipe") {
		t.Fatalf("BuildOTelCollectorConfig() error = %v, want unsupported source error", err)
	}
}

func testResolvedOTelSource(sourceID, mode, receiver string, config map[string]any) ResolvedSource {
	recipe := CollectorRecipe{Mode: mode, Receiver: receiver, Config: config}
	source := SourceProfile{
		SourceID:        sourceID,
		DisplayName:     sourceID,
		Vendor:          "controlone",
		Product:         "test",
		SourceClass:     "test",
		RiskClass:       RiskLow,
		DataSensitivity: SensitivityModerate,
		CollectorModes:  []string{mode},
		Schemas: SchemaBinding{
			Primary: SchemaOCSF,
			OCSF: OCSFBinding{
				Category: "application_activity",
				Class:    "application_log",
			},
		},
		Parsers: []string{sourceID + ".parser"},
		Samples: []string{sourceID + ".sample"},
	}
	if mode != "" {
		source.CollectorRecipes = []CollectorRecipe{recipe}
	}
	parser := ParserProfile{ParserID: sourceID + ".parser", DisplayName: sourceID + " parser"}
	return ResolvedSource{
		PackID:      "controlone.test",
		PackVersion: "1.0.0",
		Source:      source,
		Parsers:     []ParserProfile{parser},
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
