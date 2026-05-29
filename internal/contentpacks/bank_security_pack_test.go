package contentpacks

import (
	"context"
	"testing"
)

func TestBankSecurityStarterPackValidatesAndReplays(t *testing.T) {
	pack, err := BankSecurityStarterPack()
	if err != nil {
		t.Fatalf("BankSecurityStarterPack() error = %v", err)
	}
	if pack.Manifest.PackID != "controlone.bank_security_starter" {
		t.Fatalf("pack id = %q", pack.Manifest.PackID)
	}
	if len(pack.Manifest.Sources) < 20 {
		t.Fatalf("sources = %d, want at least 20", len(pack.Manifest.Sources))
	}
	if len(pack.Manifest.Samples) < len(pack.Manifest.Sources) {
		t.Fatalf("samples = %d, sources = %d", len(pack.Manifest.Samples), len(pack.Manifest.Sources))
	}
	if len(pack.Manifest.Detections) < 6 {
		t.Fatalf("detections = %d, want starter detection coverage", len(pack.Manifest.Detections))
	}
	for _, detection := range pack.Manifest.Detections {
		if detection.RiskScore <= 0 || detection.RiskScore > 100 {
			t.Fatalf("detection %s risk score = %d, want 1-100", detection.DetectionID, detection.RiskScore)
		}
	}
	if err := Validate(pack.Manifest); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	report, err := ReplayManifestSamples(context.Background(), pack.Manifest, pack.Root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report = %#v, want starter pack replay pass", report)
	}
	if report.TotalCases != len(pack.Manifest.Samples) || report.TotalEvents != len(pack.Manifest.Samples) {
		t.Fatalf("report counts = %#v", report)
	}
	detectionReport, err := ReplayManifestDetections(context.Background(), pack.Manifest, pack.Root, DetectionReplayOptions{
		DetectionLoadOptions: DetectionLoadOptions{SigmaFieldMap: DefaultSigmaFieldMap()},
	})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if !detectionReport.Passed() {
		t.Fatalf("detection report = %#v, want starter detection replay pass", detectionReport)
	}
	if detectionReport.TotalRules != len(pack.Manifest.Detections) || detectionReport.TotalMatches != 8 {
		t.Fatalf("detection counts = %#v, want all starter rules loaded and 8 semantic bad-case matches", detectionReport)
	}
	wantMatches := map[string]string{
		"fortinet.fortigate.denied.bad":            "controlone.bank.network_denied_traffic",
		"palo_alto.panos.denied.bad":               "controlone.bank.network_denied_traffic",
		"cisco.asa.denied.bad":                     "controlone.bank.network_denied_traffic",
		"checkpoint.firewall.denied.bad":           "controlone.bank.network_denied_traffic",
		"f5.bigip.asm.blocked.bad":                 "controlone.bank.waf_exploit_blocked",
		"imperva.waf.blocked.bad":                  "controlone.bank.waf_exploit_blocked",
		"cloudflare.waf.blocked.bad":               "controlone.bank.waf_exploit_blocked",
		"microsoft.windows_powershell.encoded.bad": "controlone.bank.powershell_suspicious_script_block",
	}
	seenMatches := map[string]bool{}
	for _, result := range detectionReport.Results {
		wantDetectionID, ok := wantMatches[result.CaseID]
		if !ok {
			if len(result.Matches) != 0 {
				t.Fatalf("case %s matches = %#v, want clean starter case quiet", result.CaseID, result.Matches)
			}
			continue
		}
		if len(result.Matches) != 1 || result.Matches[0].DetectionID != wantDetectionID {
			t.Fatalf("case %s matches = %#v, want %s match", result.CaseID, result.Matches, wantDetectionID)
		}
		seenMatches[result.CaseID] = true
	}
	for caseID := range wantMatches {
		if !seenMatches[caseID] {
			t.Fatalf("missing detection replay match for %s", caseID)
		}
	}
}

func TestBankSecurityStarterPackCoversExpectedEnterpriseSources(t *testing.T) {
	pack, err := BankSecurityStarterPack()
	if err != nil {
		t.Fatalf("BankSecurityStarterPack() error = %v", err)
	}
	want := map[string]struct{}{
		"fortinet.fortigate":                  {},
		"palo_alto.panos":                     {},
		"cisco.asa":                           {},
		"checkpoint.firewall":                 {},
		"f5.bigip.asm":                        {},
		"imperva.waf":                         {},
		"cloudflare.waf":                      {},
		"zscaler.zia":                         {},
		"okta.system_log":                     {},
		"microsoft.entra_id":                  {},
		"microsoft.active_directory_security": {},
		"crowdstrike.falcon":                  {},
		"sentinelone.singularity":             {},
		"microsoft.defender_endpoint":         {},
		"microsoft.windows_sysmon":            {},
		"microsoft.windows_powershell":        {},
		"coredns.query":                       {},
		"isc.bind_dns":                        {},
		"infoblox.dns_dhcp":                   {},
		"squid.proxy":                         {},
		"netbird.audit":                       {},
		"openziti.audit":                      {},
	}
	seen := map[string]SourceProfile{}
	for _, source := range pack.Manifest.Sources {
		seen[source.SourceID] = source
	}
	for sourceID := range want {
		source, ok := seen[sourceID]
		if !ok {
			t.Fatalf("starter pack missing source %s", sourceID)
		}
		if len(source.Parsers) != 1 || len(source.Samples) == 0 {
			t.Fatalf("source %s parsers/samples = %#v/%#v", sourceID, source.Parsers, source.Samples)
		}
		if len(source.Detections) == 0 {
			t.Fatalf("source %s has no starter detections", sourceID)
		}
		wantSemantics := bankStarterCarrierOnlyMetadata
		if bankStarterHasFirewallSemantics(sourceID) {
			wantSemantics = bankFirewallSemanticMetadata
			if len(source.Samples) != 2 {
				t.Fatalf("source %s samples = %#v, want clean and denied semantic samples", sourceID, source.Samples)
			}
		} else if bankStarterHasWAFSemantics(sourceID) {
			wantSemantics = bankWAFSemanticMetadata
			if len(source.Samples) != 2 {
				t.Fatalf("source %s samples = %#v, want clean and blocked semantic samples", sourceID, source.Samples)
			}
		} else if bankStarterHasIAMSemantics(sourceID) {
			wantSemantics = bankIAMSemanticMetadata
		} else if bankStarterHasWindowsSemantics(sourceID) {
			wantSemantics = bankWindowsSemanticMetadata
			if !stringSliceContains(source.CollectorModes, CollectorWEF) || len(source.CollectorRecipes) == 0 {
				t.Fatalf("source %s windows collector support = %#v/%#v", sourceID, source.CollectorModes, source.CollectorRecipes)
			}
		}
		if !source.ApprovalRequired || source.Metadata["vendor_semantics"] != wantSemantics {
			t.Fatalf("source %s bank-safe metadata = %#v", sourceID, source)
		}
	}
}

func TestBankSecurityStarterPackIAMSemanticParsers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parserID   string
		spec       bankStarterSourceSpec
		wantCode   string
		wantAction string
	}{
		{
			name:       "okta",
			parserID:   bankOktaSystemLogParserID,
			spec:       bankStarterSourceSpec{SourceID: "okta.system_log", DisplayName: "Okta System Log", Vendor: "Okta", Product: "System Log", SourceClass: "iam", Format: "json"},
			wantCode:   "user.session.start",
			wantAction: "user.session.start",
		},
		{
			name:       "entra",
			parserID:   bankMicrosoftEntraIDParserID,
			spec:       bankStarterSourceSpec{SourceID: "microsoft.entra_id", DisplayName: "Microsoft Entra ID", Vendor: "Microsoft", Product: "Entra ID", SourceClass: "iam", Format: "json"},
			wantCode:   "0",
			wantAction: "user_login_success",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := bankStarterParserProfile(tc.parserID)
			compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			raw, err := bankStarterRawEvent(tc.spec, bankStarterSampleVariant{Action: "allowed"})
			if err != nil {
				t.Fatalf("bankStarterRawEvent() error = %v", err)
			}
			out, err := compiled.Parse(ParserInput{Raw: raw})
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got, _ := getField(out.Event.Fields, "event.code"); got != tc.wantCode {
				t.Fatalf("event.code = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "event.action"); got != tc.wantAction {
				t.Fatalf("event.action = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "event.category"); got != "authentication" {
				t.Fatalf("event.category = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "event.outcome"); got != "success" {
				t.Fatalf("event.outcome = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "user.name"); got != "alice@bank.local" {
				t.Fatalf("user.name = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "source.ip"); got != "10.10.1.25" {
				t.Fatalf("source.ip = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "control_one.vendor_semantics"); got != bankIAMSemanticMetadata {
				t.Fatalf("control_one.vendor_semantics = %#v", got)
			}
		})
	}
}

func TestBankSecurityStarterPackFirewallSemanticParser(t *testing.T) {
	profile := bankStarterParserProfile(bankFortinetFortigateParserID)
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	raw, err := bankStarterRawEvent(bankStarterSourceSpec{
		SourceID:    "fortinet.fortigate",
		DisplayName: "Fortinet FortiGate Firewall",
		Vendor:      "Fortinet",
		Product:     "FortiGate",
		SourceClass: "firewall",
		Format:      "cef",
	}, bankStarterSampleVariant{Action: "denied"})
	if err != nil {
		t.Fatalf("bankStarterRawEvent() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "event.dataset"); got != "fortinet.fortigate" {
		t.Fatalf("event.dataset = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "observer.type"); got != "firewall" {
		t.Fatalf("observer.type = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.outcome"); got != "failure" {
		t.Fatalf("event.outcome = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "control_one.vendor_semantics"); got != bankFirewallSemanticMetadata {
		t.Fatalf("control_one.vendor_semantics = %#v", got)
	}
}

func TestBankSecurityStarterPackCheckPointLEEFFirewallSemanticParser(t *testing.T) {
	profile := bankStarterParserProfile(bankCheckPointFirewallParserID)
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	raw, err := bankStarterRawEvent(bankStarterSourceSpec{
		SourceID:    "checkpoint.firewall",
		DisplayName: "Check Point Firewall",
		Vendor:      "Check Point",
		Product:     "Quantum Security Gateway",
		SourceClass: "firewall",
		Format:      "leef",
	}, bankStarterSampleVariant{Action: "denied"})
	if err != nil {
		t.Fatalf("bankStarterRawEvent() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "event.dataset"); got != "checkpoint.firewall" {
		t.Fatalf("event.dataset = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "observer.type"); got != "firewall" {
		t.Fatalf("observer.type = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.outcome"); got != "failure" {
		t.Fatalf("event.outcome = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "source.ip"); got != "10.10.1.10" {
		t.Fatalf("source.ip = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "control_one.vendor_semantics"); got != bankFirewallSemanticMetadata {
		t.Fatalf("control_one.vendor_semantics = %#v", got)
	}
}

func TestBankSecurityStarterPackWAFSemanticParser(t *testing.T) {
	profile := bankStarterParserProfile(bankF5BigIPASMParserID)
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	raw, err := bankStarterRawEvent(bankStarterSourceSpec{
		SourceID:    "f5.bigip.asm",
		DisplayName: "F5 BIG-IP ASM/WAF",
		Vendor:      "F5",
		Product:     "BIG-IP ASM",
		SourceClass: "waf",
		Format:      "cef",
	}, bankStarterSampleVariant{Action: "blocked"})
	if err != nil {
		t.Fatalf("bankStarterRawEvent() error = %v", err)
	}
	out, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := getField(out.Event.Fields, "event.dataset"); got != "f5.bigip.asm" {
		t.Fatalf("event.dataset = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "observer.type"); got != "waf" {
		t.Fatalf("observer.type = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "event.outcome"); got != "failure" {
		t.Fatalf("event.outcome = %#v", got)
	}
	if got, _ := getField(out.Event.Fields, "control_one.vendor_semantics"); got != bankWAFSemanticMetadata {
		t.Fatalf("control_one.vendor_semantics = %#v", got)
	}
}

func TestBankSecurityStarterPackWindowsSemanticParsers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parserID   string
		spec       bankStarterSourceSpec
		wantCode   string
		wantAction string
		wantUser   string
	}{
		{
			name:       "security",
			parserID:   bankWindowsSecurityParserID,
			spec:       bankStarterSourceSpec{SourceID: "microsoft.active_directory_security", DisplayName: "Microsoft Active Directory Security", Vendor: "Microsoft", Product: "Active Directory", SourceClass: "iam", Format: "json"},
			wantCode:   "4624",
			wantAction: "logon_success",
			wantUser:   "alice",
		},
		{
			name:       "sysmon",
			parserID:   bankWindowsSysmonParserID,
			spec:       bankStarterSourceSpec{SourceID: "microsoft.windows_sysmon", DisplayName: "Microsoft Windows Sysmon", Vendor: "Microsoft", Product: "Sysmon", SourceClass: "edr", Format: "json"},
			wantCode:   "1",
			wantAction: "process_start",
			wantUser:   `BANK\alice`,
		},
		{
			name:       "powershell",
			parserID:   bankWindowsPowerShellParserID,
			spec:       bankStarterSourceSpec{SourceID: "microsoft.windows_powershell", DisplayName: "Microsoft Windows PowerShell", Vendor: "Microsoft", Product: "PowerShell", SourceClass: "edr", Format: "json"},
			wantCode:   "4104",
			wantAction: "powershell_script_block",
			wantUser:   `BANK\alice`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := bankStarterParserProfile(tc.parserID)
			compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			raw, err := bankStarterRawEvent(tc.spec, bankStarterSampleVariant{Action: "allowed"})
			if err != nil {
				t.Fatalf("bankStarterRawEvent() error = %v", err)
			}
			out, err := compiled.Parse(ParserInput{Raw: raw})
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got, _ := getField(out.Event.Fields, "event.code"); got != tc.wantCode {
				t.Fatalf("event.code = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "event.action"); got != tc.wantAction {
				t.Fatalf("event.action = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "user.name"); got != tc.wantUser {
				t.Fatalf("user.name = %#v", got)
			}
			if got, _ := getField(out.Event.Fields, "control_one.vendor_semantics"); got != bankWindowsSemanticMetadata {
				t.Fatalf("control_one.vendor_semantics = %#v", got)
			}
		})
	}
}
