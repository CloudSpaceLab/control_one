package contentpacks

import (
	"fmt"
	"testing"
	"time"
)

func TestDefaultParserRuntimeCompilesAndRunsRegexProfile(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	compiled, err := DefaultParserRuntimeRegistry().Compile(manifest.Parsers[0])
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `203.0.113.10 - - [27/May/2026:12:00:00 +0000] "GET / HTTP/1.1" 200 12`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if out.Status != ParserStatusParsed {
		t.Fatalf("status = %q", out.Status)
	}
	if got := out.Event.Fields["remote_addr"]; got != "203.0.113.10" {
		t.Fatalf("remote_addr = %#v", got)
	}
}

func TestParserRuntimeRejectsUnsupportedStageAtCompile(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.unsupported",
		DisplayName: "unsupported parser",
		Stages: []ParserStage{{
			Type: "custom_missing",
		}},
	}
	_, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err == nil {
		t.Fatal("Compile() error = nil, want unsupported stage error")
	}
}

func TestParserRuntimeParsesRFC3164Syslog(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.syslog3164",
		DisplayName: "RFC3164 parser",
		Stages:      []ParserStage{{Type: StageSyslogRFC3164}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{
		Raw:       `<34>May 27 12:34:56 web-1 sshd[123]: Failed password for root`,
		Timestamp: time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "host.hostname"); got != "web-1" {
		t.Fatalf("host.hostname = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "process.name"); got != "sshd" {
		t.Fatalf("process.name = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "syslog.facility"); got != 4 {
		t.Fatalf("syslog.facility = %#v", got)
	}
	if out.Event.Timestamp.Year() != 2026 {
		t.Fatalf("timestamp = %s", out.Event.Timestamp)
	}
}

func TestParserRuntimeParsesRFC5424Syslog(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.syslog5424",
		DisplayName: "RFC5424 parser",
		Stages:      []ParserStage{{Type: StageSyslogRFC5424}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `<165>1 2026-05-27T12:34:56Z host app 8710 ID47 [exampleSDID iut="3"] message body`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "process.name"); got != "app" {
		t.Fatalf("process.name = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "syslog.structured_data"); got != `[exampleSDID iut="3"]` {
		t.Fatalf("structured_data = %#v", got)
	}
}

func TestParserRuntimeParsesCEF(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.cef",
		DisplayName: "CEF parser",
		Stages:      []ParserStage{{Type: StageCEF}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `CEF:0|Fortinet|FortiGate|7.2|100|Allowed traffic|5|src=10.0.0.1 dst=10.0.0.2 spt=443`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "observer.vendor"); got != "Fortinet" {
		t.Fatalf("observer.vendor = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "cef.extensions.src"); got != "10.0.0.1" {
		t.Fatalf("src = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "source.ip"); got != "10.0.0.1" {
		t.Fatalf("source.ip = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "destination.ip"); got != "10.0.0.2" {
		t.Fatalf("destination.ip = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "source.port"); got != 443 {
		t.Fatalf("source.port = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.provider"); got != "Fortinet/FortiGate" {
		t.Fatalf("event.provider = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.outcome"); got != "success" {
		t.Fatalf("event.outcome = %#v", got)
	}
}

func TestParserRuntimeParsesLEEF(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.leef",
		DisplayName: "LEEF parser",
		Stages:      []ParserStage{{Type: StageLEEF}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: "LEEF:2.0|IBM|QRadar|1.0|login|^|usrName=alice^src=10.0.0.5^action=allowed"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "leef.extensions.usrName"); got != "alice" {
		t.Fatalf("usrName = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "user.name"); got != "alice" {
		t.Fatalf("user.name = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "source.ip"); got != "10.0.0.5" {
		t.Fatalf("source.ip = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.provider"); got != "IBM/QRadar" {
		t.Fatalf("event.provider = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.outcome"); got != "success" {
		t.Fatalf("event.outcome = %#v", got)
	}
}

func TestParserRuntimeInfersFailureOutcomeFromCarrierAction(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.cef",
		DisplayName: "CEF parser",
		Stages:      []ParserStage{{Type: StageCEF}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `CEF:0|Fortinet|FortiGate|7.2|100|Denied traffic|5|src=10.0.0.1 dst=10.0.0.2 act=denied`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "event.outcome"); got != "failure" {
		t.Fatalf("event.outcome = %#v", got)
	}
}

func TestParserRuntimeParsesGrok(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.grok",
		DisplayName: "grok parser",
		Stages: []ParserStage{{
			Type:   StageGrok,
			Config: map[string]any{"pattern": `%{IP:source.ip} %{WORD:http.request.method} %{NOTSPACE:url.path}`},
		}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `203.0.113.10 GET /login`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "source.ip"); got != "203.0.113.10" {
		t.Fatalf("source.ip = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "url.path"); got != "/login" {
		t.Fatalf("url.path = %#v", got)
	}
}

func TestParserRuntimeParsesKVAndLogfmt(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.kv",
		DisplayName: "kv parser",
		Stages: []ParserStage{
			{Type: StageKV, Config: map[string]any{"target_prefix": "kv."}},
			{Type: StageLogfmt, Config: map[string]any{"source_field": "kv.message", "target_prefix": "logfmt."}},
		},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `user=alice message="status=ok action=login"`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "kv.user"); got != "alice" {
		t.Fatalf("kv.user = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "logfmt.action"); got != "login" {
		t.Fatalf("logfmt.action = %#v", got)
	}
}

func TestParserRuntimeParsesXMLAndWindowsEventData(t *testing.T) {
	xmlProfile := ParserProfile{
		ParserID:    "test.xml",
		DisplayName: "xml parser",
		Stages:      []ParserStage{{Type: StageXML}},
	}
	compiledXML, err := DefaultParserRuntimeRegistry().Compile(xmlProfile)
	if err != nil {
		t.Fatalf("Compile(xml) error = %v", err)
	}
	outXML, err := compiledXML.Parse(ParserInput{Raw: `<Event><System><EventID>4624</EventID></System></Event>`})
	if err != nil {
		t.Fatalf("Parse(xml) error = %v", err)
	}
	if got, _ := getField(outXML.Event.Fields, "xml.Event.System.EventID.#text"); got != "4624" {
		t.Fatalf("EventID text = %#v", got)
	}

	winProfile := ParserProfile{
		ParserID:    "test.windows",
		DisplayName: "windows parser",
		Stages:      []ParserStage{{Type: StageWindowsEventData}},
	}
	compiledWin, err := DefaultParserRuntimeRegistry().Compile(winProfile)
	if err != nil {
		t.Fatalf("Compile(windows) error = %v", err)
	}
	outWin, err := compiledWin.Parse(ParserInput{Raw: `{"Event":{"System":{"EventID":4624,"Computer":"dc1"},"EventData":{"Data":[{"Name":"SubjectUserName","Value":"alice"}]}}}`})
	if err != nil {
		t.Fatalf("Parse(windows) error = %v", err)
	}
	if got, _ := getField(outWin.Event.Fields, "windows.event_data.SubjectUserName"); got != "alice" {
		t.Fatalf("SubjectUserName = %#v", got)
	}
}

func TestParserRuntimeNormalizesWindowsSecurityLogon(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.windows.security",
		DisplayName: "windows security parser",
		Stages:      []ParserStage{{Type: StageWindowsEventData}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	raw := `{"Event":{"System":{"Provider":{"Name":"Microsoft-Windows-Security-Auditing"},"EventID":4624,"Computer":"dc1.bank.local","Channel":"Security"},"EventData":{"Data":[{"Name":"SubjectUserName","Value":"svc-collector"},{"Name":"SubjectDomainName","Value":"BANK"},{"Name":"TargetUserName","Value":"alice"},{"Name":"TargetDomainName","Value":"BANK"},{"Name":"IpAddress","Value":"10.10.1.25"},{"Name":"IpPort","Value":"51514"},{"Name":"LogonType","Value":"3"}]}}}`

	out, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertField(t, out.Event.Fields, "event.code", "4624")
	assertField(t, out.Event.Fields, "event.provider", "Microsoft-Windows-Security-Auditing")
	assertField(t, out.Event.Fields, "event.category", "authentication")
	assertField(t, out.Event.Fields, "event.action", "logon_success")
	assertField(t, out.Event.Fields, "event.outcome", "success")
	assertField(t, out.Event.Fields, "event.dataset", "Security")
	assertField(t, out.Event.Fields, "host.hostname", "dc1.bank.local")
	assertField(t, out.Event.Fields, "user.name", "alice")
	assertField(t, out.Event.Fields, "user.domain", "BANK")
	assertField(t, out.Event.Fields, "source.user.name", "svc-collector")
	assertField(t, out.Event.Fields, "source.ip", "10.10.1.25")
	assertField(t, out.Event.Fields, "source.port", 51514)
}

func TestParserRuntimeNormalizesWindowsSecurityCommonEventIDs(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.windows.security.common",
		DisplayName: "windows security common parser",
		Stages:      []ParserStage{{Type: StageWindowsEventData}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	cases := []struct {
		name        string
		eventID     int
		eventData   string
		wantAction  string
		wantOutcome string
		wantField   string
		wantValue   any
	}{
		{
			name:        "logon failure",
			eventID:     4625,
			eventData:   `{"Name":"TargetUserName","Value":"alice"},{"Name":"TargetDomainName","Value":"BANK"},{"Name":"IpAddress","Value":"10.10.1.25"},{"Name":"IpPort","Value":"51514"}`,
			wantAction:  "logon_failure",
			wantOutcome: "failure",
			wantField:   "user.name",
			wantValue:   "alice",
		},
		{
			name:        "logoff",
			eventID:     4634,
			eventData:   `{"Name":"TargetUserName","Value":"alice"},{"Name":"TargetDomainName","Value":"BANK"}`,
			wantAction:  "logoff",
			wantOutcome: "success",
			wantField:   "user.domain",
			wantValue:   "BANK",
		},
		{
			name:        "user initiated logoff",
			eventID:     4647,
			eventData:   `{"Name":"TargetUserName","Value":"alice"},{"Name":"TargetDomainName","Value":"BANK"}`,
			wantAction:  "logoff",
			wantOutcome: "success",
			wantField:   "user.name",
			wantValue:   "alice",
		},
		{
			name:        "process start",
			eventID:     4688,
			eventData:   `{"Name":"NewProcessName","Value":"C:\\Windows\\System32\\cmd.exe"},{"Name":"CommandLine","Value":"cmd.exe /c whoami"},{"Name":"NewProcessId","Value":"0x12a4"}`,
			wantAction:  "process_start",
			wantOutcome: "success",
			wantField:   "process.executable",
			wantValue:   `C:\Windows\System32\cmd.exe`,
		},
		{
			name:        "process end",
			eventID:     4689,
			eventData:   `{"Name":"ProcessName","Value":"C:\\Windows\\System32\\cmd.exe"},{"Name":"ProcessId","Value":"0x12a4"}`,
			wantAction:  "process_end",
			wantOutcome: "success",
			wantField:   "process.pid",
			wantValue:   "0x12a4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"Event":{"System":{"Provider":{"Name":"Microsoft-Windows-Security-Auditing"},"EventID":%d,"Computer":"dc1.bank.local","Channel":"Security"},"EventData":{"Data":[%s]}}}`, tc.eventID, tc.eventData)
			out, err := compiled.Parse(ParserInput{Raw: raw})
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			assertField(t, out.Event.Fields, "event.code", fmt.Sprint(tc.eventID))
			assertField(t, out.Event.Fields, "event.action", tc.wantAction)
			assertField(t, out.Event.Fields, "event.outcome", tc.wantOutcome)
			assertField(t, out.Event.Fields, tc.wantField, tc.wantValue)
		})
	}
}

func TestParserRuntimeNormalizesSysmonProcessCreate(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.windows.sysmon",
		DisplayName: "windows sysmon parser",
		Stages:      []ParserStage{{Type: StageWindowsEventData}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	raw := `{"Event":{"System":{"Provider":{"Name":"Microsoft-Windows-Sysmon"},"EventID":1,"Computer":"workstation-7","Channel":"Microsoft-Windows-Sysmon/Operational"},"EventData":{"Data":[{"Name":"User","Value":"BANK\\alice"},{"Name":"Image","Value":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe"},{"Name":"CommandLine","Value":"powershell.exe -NoProfile"},{"Name":"ParentImage","Value":"C:\\Windows\\explorer.exe"},{"Name":"ProcessId","Value":"4242"}]}}}`

	out, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertField(t, out.Event.Fields, "event.code", "1")
	assertField(t, out.Event.Fields, "event.provider", "Microsoft-Windows-Sysmon")
	assertField(t, out.Event.Fields, "event.category", "process")
	assertField(t, out.Event.Fields, "event.action", "process_start")
	assertField(t, out.Event.Fields, "user.name", `BANK\alice`)
	assertField(t, out.Event.Fields, "process.executable", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`)
	assertField(t, out.Event.Fields, "process.command_line", "powershell.exe -NoProfile")
	assertField(t, out.Event.Fields, "process.parent.executable", `C:\Windows\explorer.exe`)
	assertField(t, out.Event.Fields, "process.pid", "4242")
}

func TestParserRuntimeNormalizesPowerShellScriptBlock(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.windows.powershell",
		DisplayName: "windows powershell parser",
		Stages:      []ParserStage{{Type: StageWindowsEventData}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	raw := `{"Event":{"System":{"Provider":{"Name":"Microsoft-Windows-PowerShell"},"EventID":4104,"Computer":"dc1.bank.local","Channel":"Microsoft-Windows-PowerShell/Operational"},"EventData":{"Data":[{"Name":"User","Value":"BANK\\alice"},{"Name":"ScriptBlockText","Value":"Invoke-Command -ComputerName dc1 { Get-ADUser alice }"},{"Name":"ScriptBlockId","Value":"script-1"}]}}}`

	out, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertField(t, out.Event.Fields, "event.code", "4104")
	assertField(t, out.Event.Fields, "event.provider", "Microsoft-Windows-PowerShell")
	assertField(t, out.Event.Fields, "event.category", "process")
	assertField(t, out.Event.Fields, "event.action", "powershell_script_block")
	assertField(t, out.Event.Fields, "event.outcome", "success")
	assertField(t, out.Event.Fields, "event.dataset", "Microsoft-Windows-PowerShell/Operational")
	assertField(t, out.Event.Fields, "user.name", `BANK\alice`)
	assertField(t, out.Event.Fields, "process.command_line", "Invoke-Command -ComputerName dc1 { Get-ADUser alice }")
}

func TestParserRuntimeParsesTimestamp(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.timestamp",
		DisplayName: "timestamp parser",
		Stages: []ParserStage{{
			Type:   StageTimestamp,
			Config: map[string]any{"source_field": "ts", "target_field": "event.time"},
		}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Fields: map[string]any{"ts": "2026-05-27T12:34:56Z"}})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if out.Event.Timestamp.Format(time.RFC3339) != "2026-05-27T12:34:56Z" {
		t.Fatalf("timestamp = %s", out.Event.Timestamp)
	}
	if got, _ := getField(out.Event.Fields, "event.time"); got != "2026-05-27T12:34:56Z" {
		t.Fatalf("event.time = %#v", got)
	}
}

func TestParserRuntimeJSONFieldMapRedactAndDrop(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.json",
		DisplayName: "JSON parser",
		Stages: []ParserStage{
			{Type: StageJSON},
			{Type: StageFieldMap, Config: map[string]any{
				"mappings": map[string]any{
					"http.response.status_code": "status",
				},
				"set": map[string]any{
					"event.category": "network_activity",
				},
			}},
			{Type: StageRedact, Config: map[string]any{
				"fields": []any{"password"},
			}},
			{Type: StageDrop, Config: map[string]any{
				"when_field": "drop",
				"equals":     "true",
			}},
		},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: `{"status":200,"password":"secret","drop":"true"}`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if out.Status != ParserStatusDropped || !out.Event.Dropped {
		t.Fatalf("output = %#v, want dropped", out)
	}
	if got := out.Event.Fields["password"]; got != "[redacted]" {
		t.Fatalf("password = %#v, want redacted", got)
	}
	if got, _ := getField(out.Event.Fields, "http.response.status_code"); fmt.Sprint(got) != "200" {
		t.Fatalf("mapped status = %#v", got)
	}
}

func TestParserRuntimeOnErrorKeepRawContinues(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.partial",
		DisplayName: "partial parser",
		Stages: []ParserStage{
			{StageID: "extract", Type: StageRegex, OnError: OnErrorKeepRaw, Config: map[string]any{"pattern": `^user=(?P<user>\S+)`}},
			{StageID: "mark", Type: StageFieldMap, Config: map[string]any{"set": map[string]any{"event.kind": "event"}}},
		},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: "no-match"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if out.Status != ParserStatusPartial || len(out.StageErrors) != 1 {
		t.Fatalf("output = %#v, want partial with one stage error", out)
	}
	if got, _ := getField(out.Event.Fields, "event.kind"); got != "event" {
		t.Fatalf("event.kind = %#v", got)
	}
}

func TestParserRuntimeOnErrorFailStops(t *testing.T) {
	profile := ParserProfile{
		ParserID:    "test.fail",
		DisplayName: "fail parser",
		Stages: []ParserStage{{
			Type:   StageRegex,
			Config: map[string]any{"pattern": `^user=(?P<user>\S+)`},
		}},
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: "no-match"})
	if err == nil {
		t.Fatal("Parse() error = nil, want match failure")
	}
	if out.Status != ParserStatusFailed || len(out.StageErrors) != 1 {
		t.Fatalf("output = %#v, want failed with stage error", out)
	}
}

func assertField(t *testing.T, fields map[string]any, path string, want any) {
	t.Helper()
	got, ok := getField(fields, path)
	if !ok {
		t.Fatalf("%s missing from %#v", path, fields)
	}
	if got != want {
		t.Fatalf("%s = %#v, want %#v", path, got, want)
	}
}

func TestCompileResolvedSourceCompilesReferencedParsers(t *testing.T) {
	now := time.Date(2026, 5, 27, 15, 0, 0, 0, time.UTC)
	manifest := *mustManifest(t, validPackYAML)
	registry := NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	resolved, ok := registry.ResolveSource("nginx.access")
	if !ok {
		t.Fatal("ResolveSource() ok = false")
	}
	compiled, err := CompileResolvedSource(resolved, DefaultParserRuntimeRegistry())
	if err != nil {
		t.Fatalf("CompileResolvedSource() error = %v", err)
	}
	if len(compiled) != 1 || compiled[0].Profile.ParserID != "nginx.access.combined" {
		t.Fatalf("compiled = %#v", compiled)
	}
}
