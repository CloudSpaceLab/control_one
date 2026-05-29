package contentpacks

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	bankStarterCEFParserID         = "controlone.carrier.cef.v1"
	bankStarterLEEFParserID        = "controlone.carrier.leef.v1"
	bankStarterJSONParserID        = "controlone.carrier.json.v1"
	bankFortinetFortigateParserID  = "controlone.fortinet.fortigate.cef.v1"
	bankPaloAltoPANOSParserID      = "controlone.palo_alto.panos.cef.v1"
	bankCiscoASAParserID           = "controlone.cisco.asa.cef.v1"
	bankCheckPointFirewallParserID = "controlone.checkpoint.firewall.leef.v1"
	bankF5BigIPASMParserID         = "controlone.f5.bigip.asm.cef.v1"
	bankImpervaWAFParserID         = "controlone.imperva.waf.cef.v1"
	bankCloudflareWAFParserID      = "controlone.cloudflare.waf.cef.v1"
	bankOktaSystemLogParserID      = "controlone.okta.system_log.json.v1"
	bankMicrosoftEntraIDParserID   = "controlone.microsoft.entra_id.json.v1"
	bankWindowsSecurityParserID    = "controlone.microsoft.windows.security.eventdata.v1"
	bankWindowsSysmonParserID      = "controlone.microsoft.windows.sysmon.eventdata.v1"
	bankWindowsPowerShellParserID  = "controlone.microsoft.windows.powershell.eventdata.v1"
	bankFirewallSemanticMetadata   = "first_pass_firewall_semantic"
	bankWAFSemanticMetadata        = "first_pass_waf_semantic"
	bankIAMSemanticMetadata        = "first_pass_iam_semantic"
	bankWindowsSemanticMetadata    = "first_pass_windows_semantic"
	bankStarterCarrierOnlyMetadata = "starter_carrier_only"
	bankStarterGoodSampleSuffix    = "starter.good"
	bankStarterDeniedSampleSuffix  = "denied.bad"
	bankStarterBlockedSampleSuffix = "blocked.bad"
	bankStarterEncodedSampleSuffix = "encoded.bad"
)

type bankStarterSourceSpec struct {
	SourceID    string
	DisplayName string
	Vendor      string
	Product     string
	SourceClass string
	Format      string
}

type bankStarterDetectionSpec struct {
	Detection     Detection
	SourceClasses []string
	Rule          string
}

type bankStarterSampleVariant struct {
	Suffix      string
	Description string
	Action      string
}

// BankSecurityStarterPack returns a replayable starter pack for common bank
// security sources over supported carrier formats. It is not a replacement for
// deep vendor-semantic packs, but it gives pilots named source profiles with
// parser/golden coverage on day one.
func BankSecurityStarterPack() (*PackContent, error) {
	specs := bankStarterSourceSpecs()
	detections := bankStarterDetectionSpecs()
	detectionsByClass := bankStarterDetectionIDsByClass(detections)
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		PackID:        "controlone.bank_security_starter",
		PackVersion:   "0.1.0",
		DisplayName:   "Control One Bank Security Starter Pack",
		Description:   "Starter carrier-format profiles for common bank firewall, WAF, IAM, EDR, DNS, proxy, mail, and private-access audit sources.",
		Labels: map[string]string{
			"control_one.tier":  "starter",
			"control_one.scope": "bank_security",
		},
		License: LicenseMetadata{
			SPDX: "Apache-2.0",
		},
		Provenance: Provenance{
			Author: "Control One",
			Sources: []string{
				"Control One issue #212 bank SIEM readiness plan",
				"Carrier-format CEF/LEEF/JSON parser contract",
			},
		},
		Parsers:    bankStarterParserProfiles(),
		Sources:    make([]SourceProfile, 0, len(specs)),
		Detections: make([]Detection, 0, len(detections)),
		Samples:    make([]SampleCase, 0, len(specs)),
	}
	files := map[string][]byte{}
	for _, detection := range detections {
		manifest.Detections = append(manifest.Detections, detection.Detection)
		files[detection.Detection.Path] = []byte(strings.TrimSpace(detection.Rule) + "\n")
	}
	for _, spec := range specs {
		source := bankStarterSourceProfile(spec, detectionsByClass)
		manifest.Sources = append(manifest.Sources, source)
		for _, variant := range bankStarterSampleVariants(spec) {
			sample := bankStarterSampleCase(spec, variant)
			manifest.Samples = append(manifest.Samples, sample)
			input, golden, err := bankStarterSampleFiles(spec, variant)
			if err != nil {
				return nil, err
			}
			files[sample.InputPath] = input
			files[sample.GoldenPath] = golden
		}
	}
	if err := Validate(manifest); err != nil {
		return nil, err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal bank security starter manifest: %w", err)
	}
	files["manifest.json"] = append(manifestBytes, '\n')
	return &PackContent{
		Manifest:     manifest,
		ManifestPath: "manifest.json",
		Root:         newMemoryFS(files),
	}, nil
}

func bankStarterSourceSpecs() []bankStarterSourceSpec {
	return []bankStarterSourceSpec{
		{SourceID: "fortinet.fortigate", DisplayName: "Fortinet FortiGate Firewall", Vendor: "Fortinet", Product: "FortiGate", SourceClass: "firewall", Format: "cef"},
		{SourceID: "palo_alto.panos", DisplayName: "Palo Alto Networks PAN-OS Firewall", Vendor: "Palo Alto Networks", Product: "PAN-OS", SourceClass: "firewall", Format: "cef"},
		{SourceID: "cisco.asa", DisplayName: "Cisco ASA Firewall", Vendor: "Cisco", Product: "ASA", SourceClass: "firewall", Format: "cef"},
		{SourceID: "cisco.ftd", DisplayName: "Cisco Firepower Threat Defense", Vendor: "Cisco", Product: "FTD", SourceClass: "firewall", Format: "cef"},
		{SourceID: "cisco.meraki", DisplayName: "Cisco Meraki Security Appliance", Vendor: "Cisco", Product: "Meraki", SourceClass: "firewall", Format: "cef"},
		{SourceID: "checkpoint.firewall", DisplayName: "Check Point Firewall", Vendor: "Check Point", Product: "Quantum Security Gateway", SourceClass: "firewall", Format: "leef"},
		{SourceID: "f5.bigip.asm", DisplayName: "F5 BIG-IP ASM/WAF", Vendor: "F5", Product: "BIG-IP ASM", SourceClass: "waf", Format: "cef"},
		{SourceID: "imperva.waf", DisplayName: "Imperva WAF", Vendor: "Imperva", Product: "SecureSphere WAF", SourceClass: "waf", Format: "cef"},
		{SourceID: "cloudflare.waf", DisplayName: "Cloudflare WAF", Vendor: "Cloudflare", Product: "WAF", SourceClass: "waf", Format: "cef"},
		{SourceID: "zscaler.zia", DisplayName: "Zscaler Internet Access", Vendor: "Zscaler", Product: "ZIA", SourceClass: "proxy", Format: "leef"},
		{SourceID: "okta.system_log", DisplayName: "Okta System Log", Vendor: "Okta", Product: "System Log", SourceClass: "iam", Format: "json"},
		{SourceID: "microsoft.entra_id", DisplayName: "Microsoft Entra ID", Vendor: "Microsoft", Product: "Entra ID", SourceClass: "iam", Format: "json"},
		{SourceID: "microsoft.active_directory_security", DisplayName: "Microsoft Active Directory Security", Vendor: "Microsoft", Product: "Active Directory", SourceClass: "iam", Format: "json"},
		{SourceID: "crowdstrike.falcon", DisplayName: "CrowdStrike Falcon", Vendor: "CrowdStrike", Product: "Falcon", SourceClass: "edr", Format: "cef"},
		{SourceID: "sentinelone.singularity", DisplayName: "SentinelOne Singularity", Vendor: "SentinelOne", Product: "Singularity", SourceClass: "edr", Format: "cef"},
		{SourceID: "microsoft.defender_endpoint", DisplayName: "Microsoft Defender for Endpoint", Vendor: "Microsoft", Product: "Defender for Endpoint", SourceClass: "edr", Format: "json"},
		{SourceID: "microsoft.windows_sysmon", DisplayName: "Microsoft Windows Sysmon", Vendor: "Microsoft", Product: "Sysmon", SourceClass: "edr", Format: "json"},
		{SourceID: "microsoft.windows_powershell", DisplayName: "Microsoft Windows PowerShell", Vendor: "Microsoft", Product: "PowerShell", SourceClass: "edr", Format: "json"},
		{SourceID: "coredns.query", DisplayName: "CoreDNS Query Logs", Vendor: "CoreDNS", Product: "CoreDNS", SourceClass: "dns", Format: "json"},
		{SourceID: "isc.bind_dns", DisplayName: "ISC BIND DNS Logs", Vendor: "ISC", Product: "BIND", SourceClass: "dns", Format: "json"},
		{SourceID: "infoblox.dns_dhcp", DisplayName: "Infoblox DNS/DHCP", Vendor: "Infoblox", Product: "NIOS", SourceClass: "dns", Format: "leef"},
		{SourceID: "squid.proxy", DisplayName: "Squid Proxy", Vendor: "Squid", Product: "Squid Proxy", SourceClass: "proxy", Format: "json"},
		{SourceID: "proofpoint.tap", DisplayName: "Proofpoint TAP", Vendor: "Proofpoint", Product: "TAP", SourceClass: "mail", Format: "cef"},
		{SourceID: "netbird.audit", DisplayName: "NetBird Audit", Vendor: "NetBird", Product: "Management", SourceClass: "private_access", Format: "json"},
		{SourceID: "openziti.audit", DisplayName: "OpenZiti Audit", Vendor: "NetFoundry", Product: "OpenZiti", SourceClass: "private_access", Format: "json"},
		{SourceID: "headscale.audit", DisplayName: "Headscale Audit", Vendor: "Headscale", Product: "Headscale", SourceClass: "private_access", Format: "json"},
	}
}

func bankStarterSourceProfile(spec bankStarterSourceSpec, detectionsByClass map[string][]string) SourceProfile {
	parserID := bankStarterParserID(spec)
	metadata := bankStarterCarrierOnlyMetadata
	if bankStarterHasFirewallSemantics(spec.SourceID) {
		metadata = bankFirewallSemanticMetadata
	} else if bankStarterHasWAFSemantics(spec.SourceID) {
		metadata = bankWAFSemanticMetadata
	} else if bankStarterHasIAMSemantics(spec.SourceID) {
		metadata = bankIAMSemanticMetadata
	} else if bankStarterHasWindowsSemantics(spec.SourceID) {
		metadata = bankWindowsSemanticMetadata
	}
	return SourceProfile{
		SourceID:         spec.SourceID,
		DisplayName:      spec.DisplayName,
		Vendor:           spec.Vendor,
		Product:          spec.Product,
		Versions:         []string{"starter"},
		SourceClass:      spec.SourceClass,
		RiskClass:        RiskMedium,
		DataSensitivity:  SensitivityHigh,
		CollectorModes:   bankStarterCollectorModes(spec),
		CollectorRecipes: bankStarterCollectorRecipes(spec),
		ApprovalRequired: true,
		RequiredPrivileges: []string{
			"configure_log_forwarding",
			"read_security_events",
		},
		ExpectedVolume: VolumeHint{
			EventsPerSecond: 100,
			BytesPerSecond:  75000,
			Burst:           "5x for 10m",
		},
		RawRetentionDefault: "7d",
		Schemas: SchemaBinding{
			Primary:       SchemaOCSF,
			ExportAliases: []string{SchemaECS},
			OCSF: OCSFBinding{
				Category: bankStarterOCSFCategory(spec.SourceClass),
				Class:    bankStarterOCSFClass(spec.SourceClass),
				Activity: "activity",
			},
		},
		Parsers:    []string{parserID},
		Detections: append([]string(nil), detectionsByClass[strings.TrimSpace(spec.SourceClass)]...),
		Samples:    bankStarterSampleIDs(spec),
		Labels: map[string]string{
			"control_one.pack_tier":        "starter",
			"control_one.carrier_format":   spec.Format,
			"control_one.vendor_semantics": metadata,
		},
		Metadata: map[string]string{
			"vendor_semantics": metadata,
		},
	}
}

func bankStarterDetectionSpecs() []bankStarterDetectionSpec {
	return []bankStarterDetectionSpec{
		{
			Detection: Detection{
				DetectionID: "controlone.bank.network_denied_traffic",
				Title:       "Denied Or Blocked Network Traffic",
				Kind:        DetectionKindSigma,
				Path:        "detections/network-denied-traffic.yml",
				Severity:    "medium",
				RiskScore:   55,
				Tags:        []string{"attack.command_and_control", "attack.exfiltration", "attack.t1090", "controlone.bank"},
			},
			SourceClasses: []string{"firewall", "proxy"},
			Rule: `
title: Denied Or Blocked Network Traffic
status: stable
logsource:
  category: network_connection
detection:
  selection:
    event.action|contains:
      - deny
      - denied
      - block
      - blocked
      - reset
      - drop
      - dropped
  condition: selection
level: medium
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.waf_exploit_blocked",
				Title:       "WAF Blocked Exploit Attempt",
				Kind:        DetectionKindSigma,
				Path:        "detections/waf-exploit-blocked.yml",
				Severity:    "high",
				RiskScore:   82,
				Tags:        []string{"attack.initial_access", "attack.t1190", "controlone.bank"},
			},
			SourceClasses: []string{"waf"},
			Rule: `
title: WAF Blocked Exploit Attempt
status: stable
logsource:
  category: webserver
detection:
  selection_category:
    event.category: waf
  selection_action:
    event.action|contains:
      - block
      - blocked
      - deny
      - denied
  condition: selection_category and selection_action
level: high
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.iam_auth_failure",
				Title:       "IAM Authentication Failure",
				Kind:        DetectionKindSigma,
				Path:        "detections/iam-auth-failure.yml",
				Severity:    "medium",
				RiskScore:   65,
				Tags:        []string{"attack.credential_access", "attack.t1110", "controlone.bank"},
				Temporal: &DetectionTemporal{
					Kind:               "threshold",
					WindowSeconds:      300,
					Threshold:          5,
					GroupBy:            []string{"user.name", "source.ip"},
					SuppressForSeconds: 900,
				},
			},
			SourceClasses: []string{"iam"},
			Rule: `
title: IAM Authentication Failure
status: stable
logsource:
  category: authentication
detection:
  selection:
    event.action|contains:
      - fail
      - failed
      - denied
      - rejected
      - lockout
      - locked
  condition: selection
level: medium
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.edr_malware_alert",
				Title:       "EDR Malware Alert",
				Kind:        DetectionKindSigma,
				Path:        "detections/edr-malware-alert.yml",
				Severity:    "high",
				RiskScore:   88,
				Tags:        []string{"attack.execution", "attack.defense_evasion", "attack.t1204", "controlone.bank"},
			},
			SourceClasses: []string{"edr"},
			Rule: `
title: EDR Malware Alert
status: stable
logsource:
  category: process_creation
detection:
  selection_action:
    event.action|contains:
      - malware
      - ransomware
      - quarantine
      - blocked
      - prevented
  selection_rule:
    rule.name|contains:
      - malware
      - ransomware
      - quarantine
      - blocked
      - prevented
  condition: selection_action or selection_rule
level: high
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.powershell_suspicious_script_block",
				Title:       "Suspicious PowerShell Script Block",
				Kind:        DetectionKindSigma,
				Path:        "detections/powershell-suspicious-script-block.yml",
				Severity:    "high",
				RiskScore:   86,
				Tags:        []string{"attack.execution", "attack.t1059.001", "attack.defense_evasion", "controlone.bank"},
			},
			SourceClasses: []string{"edr"},
			Rule: `
title: Suspicious PowerShell Script Block
status: stable
logsource:
  product: windows
  service: powershell
detection:
  selection_provider:
    event.provider|contains: PowerShell
  selection_action:
    event.action: powershell_script_block
  selection_script:
    process.command_line|contains:
      - EncodedCommand
      - -enc
      - Invoke-Expression
      - DownloadString
      - FromBase64String
  condition: selection_provider and selection_action and selection_script
level: high
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.dns_query_burst",
				Title:       "DNS Query Burst",
				Kind:        DetectionKindSigma,
				Path:        "detections/dns-query-burst.yml",
				Severity:    "medium",
				RiskScore:   60,
				Tags:        []string{"attack.command_and_control", "attack.t1071.004", "controlone.bank"},
				Temporal: &DetectionTemporal{
					Kind:               "threshold",
					WindowSeconds:      60,
					Threshold:          50,
					GroupBy:            []string{"source.ip"},
					SuppressForSeconds: 300,
				},
			},
			SourceClasses: []string{"dns"},
			Rule: `
title: DNS Query Burst
status: stable
logsource:
  category: dns
detection:
  selection:
    event.category: dns
  condition: selection
level: medium
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.mail_threat_detected",
				Title:       "Mail Threat Detected",
				Kind:        DetectionKindSigma,
				Path:        "detections/mail-threat-detected.yml",
				Severity:    "high",
				RiskScore:   84,
				Tags:        []string{"attack.initial_access", "attack.t1566", "controlone.bank"},
			},
			SourceClasses: []string{"mail"},
			Rule: `
title: Mail Threat Detected
status: stable
logsource:
  category: email
detection:
  selection_action:
    event.action|contains:
      - phish
      - malware
      - spam
      - malicious
      - blocked
  selection_rule:
    rule.name|contains:
      - phish
      - malware
      - spam
      - malicious
      - blocked
  condition: selection_action or selection_rule
level: high
`,
		},
		{
			Detection: Detection{
				DetectionID: "controlone.bank.private_access_policy_change",
				Title:       "Private Access Policy Change",
				Kind:        DetectionKindSigma,
				Path:        "detections/private-access-policy-change.yml",
				Severity:    "medium",
				RiskScore:   58,
				Tags:        []string{"attack.persistence", "attack.t1098", "controlone.bank"},
			},
			SourceClasses: []string{"private_access"},
			Rule: `
title: Private Access Policy Change
status: stable
logsource:
  category: iam
detection:
  selection:
    event.action|contains:
      - policy
      - route
      - admin
      - invite
      - grant
      - approve
  condition: selection
level: medium
`,
		},
	}
}

func bankStarterDetectionIDsByClass(detections []bankStarterDetectionSpec) map[string][]string {
	out := map[string][]string{}
	for _, detection := range detections {
		id := strings.TrimSpace(detection.Detection.DetectionID)
		if id == "" {
			continue
		}
		for _, class := range detection.SourceClasses {
			class = strings.TrimSpace(class)
			if class == "" {
				continue
			}
			out[class] = append(out[class], id)
		}
	}
	return out
}

func bankStarterCollectorModes(spec bankStarterSourceSpec) []string {
	if bankStarterHasWindowsSemantics(spec.SourceID) {
		return []string{CollectorWindowsEvent, CollectorWEF, CollectorOTelFileLog, CollectorVendorAPI}
	}
	switch strings.ToLower(strings.TrimSpace(spec.Format)) {
	case "json":
		return []string{CollectorOTelFileLog, CollectorSplunkHEC, CollectorVendorAPI}
	default:
		return []string{CollectorSyslog, CollectorOTelFileLog}
	}
}

func bankStarterCollectorRecipes(spec bankStarterSourceSpec) []CollectorRecipe {
	switch strings.TrimSpace(spec.SourceID) {
	case "microsoft.active_directory_security":
		return []CollectorRecipe{
			{Mode: CollectorWindowsEvent, Receiver: "windows_event_log", Config: map[string]any{"channels": []string{"Security"}}},
			{Mode: CollectorWEF, Receiver: "windows_event_log", Config: map[string]any{"channel": "ForwardedEvents"}},
		}
	case "microsoft.windows_sysmon":
		return []CollectorRecipe{
			{Mode: CollectorWindowsEvent, Receiver: "windows_event_log", Config: map[string]any{"channels": []string{"Microsoft-Windows-Sysmon/Operational"}}},
			{Mode: CollectorWEF, Receiver: "windows_event_log", Config: map[string]any{"channel": "ForwardedEvents"}},
		}
	case "microsoft.windows_powershell":
		return []CollectorRecipe{
			{Mode: CollectorWindowsEvent, Receiver: "windows_event_log", Config: map[string]any{"channels": []string{"Microsoft-Windows-PowerShell/Operational"}}},
			{Mode: CollectorWEF, Receiver: "windows_event_log", Config: map[string]any{"channel": "ForwardedEvents"}},
		}
	default:
		return nil
	}
}

func bankStarterSampleCase(spec bankStarterSourceSpec, variant bankStarterSampleVariant) SampleCase {
	sampleID := bankStarterSampleID(spec, variant)
	return SampleCase{
		CaseID:      sampleID,
		SourceID:    spec.SourceID,
		ParserID:    bankStarterParserID(spec),
		InputPath:   "samples/" + sampleID + ".input.jsonl",
		GoldenPath:  "samples/" + sampleID + ".golden.jsonl",
		Description: variant.Description + " for " + spec.DisplayName,
	}
}

func bankStarterSampleFiles(spec bankStarterSourceSpec, variant bankStarterSampleVariant) ([]byte, []byte, error) {
	raw, err := bankStarterRawEvent(spec, variant)
	if err != nil {
		return nil, nil, err
	}
	inputLine, err := json.Marshal(ParserInput{Raw: raw})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal starter sample input %s: %w", spec.SourceID, err)
	}
	profile := bankStarterParserProfile(bankStarterParserID(spec))
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		return nil, nil, fmt.Errorf("compile starter parser %s: %w", profile.ParserID, err)
	}
	output, err := compiled.Parse(ParserInput{Raw: raw})
	if err != nil {
		return nil, nil, fmt.Errorf("parse starter sample %s: %w", spec.SourceID, err)
	}
	golden := sampleGoldenRecord{
		ParserID: output.ParserID,
		Status:   output.Status,
		Fields:   output.Event.Fields,
		Labels:   output.Event.Labels,
	}
	if output.Event.Dropped {
		dropped := true
		golden.Dropped = &dropped
	}
	goldenLine, err := json.Marshal(golden)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal starter sample golden %s: %w", spec.SourceID, err)
	}
	return append(inputLine, '\n'), append(goldenLine, '\n'), nil
}

func bankStarterParserProfile(parserID string) ParserProfile {
	switch parserID {
	case bankStarterCEFParserID:
		return ParserProfile{ParserID: parserID, DisplayName: "Control One CEF Carrier Parser", Version: "1.0.0", Stages: []ParserStage{{Type: StageCEF}}}
	case bankStarterLEEFParserID:
		return ParserProfile{ParserID: parserID, DisplayName: "Control One LEEF Carrier Parser", Version: "1.0.0", Stages: []ParserStage{{Type: StageLEEF}}}
	case bankFortinetFortigateParserID:
		return bankStarterFirewallParserProfile(parserID, "Fortinet FortiGate Firewall", "fortinet.fortigate")
	case bankPaloAltoPANOSParserID:
		return bankStarterFirewallParserProfile(parserID, "Palo Alto Networks PAN-OS Firewall", "palo_alto.panos")
	case bankCiscoASAParserID:
		return bankStarterFirewallParserProfile(parserID, "Cisco ASA Firewall", "cisco.asa")
	case bankCheckPointFirewallParserID:
		return bankStarterLEEFFirewallParserProfile(parserID, "Check Point Firewall", "checkpoint.firewall")
	case bankF5BigIPASMParserID:
		return bankStarterWAFParserProfile(parserID, "F5 BIG-IP ASM/WAF", "f5.bigip.asm")
	case bankImpervaWAFParserID:
		return bankStarterWAFParserProfile(parserID, "Imperva WAF", "imperva.waf")
	case bankCloudflareWAFParserID:
		return bankStarterWAFParserProfile(parserID, "Cloudflare WAF", "cloudflare.waf")
	case bankOktaSystemLogParserID:
		return bankStarterOktaParserProfile(parserID)
	case bankMicrosoftEntraIDParserID:
		return bankStarterEntraIDParserProfile(parserID)
	case bankWindowsSecurityParserID:
		return bankStarterWindowsEventParserProfile(parserID, "Microsoft Windows Security", "Security")
	case bankWindowsSysmonParserID:
		return bankStarterWindowsEventParserProfile(parserID, "Microsoft Windows Sysmon", "Microsoft-Windows-Sysmon/Operational")
	case bankWindowsPowerShellParserID:
		return bankStarterWindowsEventParserProfile(parserID, "Microsoft Windows PowerShell", "Microsoft-Windows-PowerShell/Operational")
	default:
		return ParserProfile{ParserID: bankStarterJSONParserID, DisplayName: "Control One JSON Carrier Parser", Version: "1.0.0", Stages: []ParserStage{{Type: StageJSON}}}
	}
}

func bankStarterFirewallParserProfile(parserID, displayName, dataset string) ParserProfile {
	return ParserProfile{
		ParserID:    parserID,
		DisplayName: displayName + " CEF Parser",
		Version:     "1.0.0",
		Stages: []ParserStage{
			{Type: StageCEF},
			{Type: StageFieldMap, Config: map[string]any{"set": map[string]any{
				"event.dataset":                dataset,
				"observer.type":                "firewall",
				"control_one.vendor_semantics": bankFirewallSemanticMetadata,
			}}},
		},
	}
}

func bankStarterLEEFFirewallParserProfile(parserID, displayName, dataset string) ParserProfile {
	return ParserProfile{
		ParserID:    parserID,
		DisplayName: displayName + " LEEF Parser",
		Version:     "1.0.0",
		Stages: []ParserStage{
			{Type: StageLEEF},
			{Type: StageFieldMap, Config: map[string]any{"set": map[string]any{
				"event.dataset":                dataset,
				"observer.type":                "firewall",
				"control_one.vendor_semantics": bankFirewallSemanticMetadata,
			}}},
		},
	}
}

func bankStarterWAFParserProfile(parserID, displayName, dataset string) ParserProfile {
	return ParserProfile{
		ParserID:    parserID,
		DisplayName: displayName + " CEF Parser",
		Version:     "1.0.0",
		Stages: []ParserStage{
			{Type: StageCEF},
			{Type: StageFieldMap, Config: map[string]any{"set": map[string]any{
				"event.dataset":                dataset,
				"observer.type":                "waf",
				"control_one.vendor_semantics": bankWAFSemanticMetadata,
			}}},
		},
	}
}

func bankStarterOktaParserProfile(parserID string) ParserProfile {
	return ParserProfile{
		ParserID:    parserID,
		DisplayName: "Okta System Log JSON Parser",
		Version:     "1.0.0",
		Stages: []ParserStage{
			{Type: StageJSON},
			{Type: StageFieldMap, Config: map[string]any{
				"mappings": map[string]any{
					"event.code":    "eventType",
					"event.action":  "eventType",
					"event.outcome": "outcome.result",
					"user.name":     "actor.alternateId",
					"source.ip":     "client.ipAddress",
				},
				"set": map[string]any{
					"event.kind":                   "event",
					"event.category":               "authentication",
					"event.dataset":                "okta.system_log",
					"event.provider":               "Okta/System Log",
					"control_one.vendor_semantics": bankIAMSemanticMetadata,
				},
			}},
		},
	}
}

func bankStarterEntraIDParserProfile(parserID string) ParserProfile {
	return ParserProfile{
		ParserID:    parserID,
		DisplayName: "Microsoft Entra ID JSON Parser",
		Version:     "1.0.0",
		Stages: []ParserStage{
			{Type: StageJSON},
			{Type: StageFieldMap, Config: map[string]any{
				"mappings": map[string]any{
					"event.code":    "resultType",
					"event.action":  "operationName",
					"event.outcome": "result",
					"user.name":     "userPrincipalName",
					"source.ip":     "ipAddress",
				},
				"set": map[string]any{
					"event.kind":                   "event",
					"event.category":               "authentication",
					"event.dataset":                "microsoft.entra_id",
					"event.provider":               "Microsoft/Entra ID",
					"control_one.vendor_semantics": bankIAMSemanticMetadata,
				},
			}},
		},
	}
}

func bankStarterWindowsEventParserProfile(parserID, displayName, dataset string) ParserProfile {
	return ParserProfile{
		ParserID:    parserID,
		DisplayName: displayName + " EventData Parser",
		Version:     "1.0.0",
		Stages: []ParserStage{
			{Type: StageWindowsEventData},
			{Type: StageFieldMap, Config: map[string]any{"set": map[string]any{
				"event.dataset":                dataset,
				"control_one.vendor_semantics": bankWindowsSemanticMetadata,
			}}},
		},
	}
}

func bankStarterRawEvent(spec bankStarterSourceSpec, variant bankStarterSampleVariant) (string, error) {
	action := strings.TrimSpace(variant.Action)
	if action == "" {
		action = "allowed"
	}
	if spec.SourceID == "microsoft.active_directory_security" {
		return bankStarterWindowsSecurityRawEvent(action)
	}
	if spec.SourceID == "microsoft.windows_sysmon" {
		return bankStarterWindowsSysmonRawEvent()
	}
	if spec.SourceID == "microsoft.windows_powershell" {
		return bankStarterWindowsPowerShellRawEvent(action)
	}
	if spec.SourceID == "okta.system_log" {
		return bankStarterOktaRawEvent(action)
	}
	if spec.SourceID == "microsoft.entra_id" {
		return bankStarterEntraIDRawEvent(action)
	}
	switch strings.ToLower(strings.TrimSpace(spec.Format)) {
	case "cef":
		return fmt.Sprintf("CEF:0|%s|%s|starter|100|Starter event|5|src=10.10.1.10 dst=10.10.2.20 spt=51515 dpt=443 proto=TCP act=%s cat=%s suser=alice", spec.Vendor, spec.Product, action, spec.SourceClass), nil
	case "leef":
		return fmt.Sprintf("LEEF:2.0|%s|%s|starter|starter_event|^|usrName=alice^src=10.10.1.10^dst=10.10.2.20^srcPort=51515^dstPort=443^proto=TCP^action=%s^cat=%s", spec.Vendor, spec.Product, action, spec.SourceClass), nil
	case "json":
		payload := map[string]any{
			"event.kind":       "event",
			"event.category":   spec.SourceClass,
			"event.action":     action,
			"event.outcome":    "success",
			"event.provider":   spec.Vendor + "/" + spec.Product,
			"host.hostname":    "starter-host-01",
			"user.name":        "alice",
			"source.ip":        "10.10.1.10",
			"destination.ip":   "10.10.2.20",
			"network.protocol": "tcp",
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("unsupported starter format %q for %s", spec.Format, spec.SourceID)
	}
}

func bankStarterOktaRawEvent(action string) (string, error) {
	eventType := "user.session.start"
	outcome := "success"
	if actionHasFailureSemantics(action) {
		eventType = "user.authentication.failed"
		outcome = "failure"
	}
	payload := map[string]any{
		"eventType":      eventType,
		"displayMessage": "Starter Okta authentication event",
		"published":      "2026-05-29T10:00:00Z",
		"outcome": map[string]any{
			"result": outcome,
		},
		"actor": map[string]any{
			"id":          "00u-bank-alice",
			"type":        "User",
			"alternateId": "alice@bank.local",
			"displayName": "Alice Banker",
		},
		"client": map[string]any{
			"ipAddress": "10.10.1.25",
			"userAgent": map[string]any{"rawUserAgent": "Mozilla/5.0"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func bankStarterEntraIDRawEvent(action string) (string, error) {
	operation := "user_login_success"
	result := "success"
	resultType := "0"
	if actionHasFailureSemantics(action) {
		operation = "user_login_failure"
		result = "failure"
		resultType = "50074"
	}
	payload := map[string]any{
		"category":          "SignInLogs",
		"operationName":     operation,
		"result":            result,
		"resultType":        resultType,
		"resultDescription": "Starter Entra ID sign-in event",
		"userPrincipalName": "alice@bank.local",
		"appDisplayName":    "Microsoft 365",
		"ipAddress":         "10.10.1.25",
		"createdDateTime":   "2026-05-29T10:00:00Z",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func bankStarterWindowsSecurityRawEvent(action string) (string, error) {
	eventID := 4624
	if actionHasFailureSemantics(action) {
		eventID = 4625
	}
	payload := map[string]any{
		"Event": map[string]any{
			"System": map[string]any{
				"Provider": map[string]any{"Name": "Microsoft-Windows-Security-Auditing"},
				"EventID":  eventID,
				"Computer": "dc1.bank.local",
				"Channel":  "Security",
			},
			"EventData": map[string]any{"Data": []map[string]any{
				{"Name": "SubjectUserName", "Value": "svc-wec"},
				{"Name": "SubjectDomainName", "Value": "BANK"},
				{"Name": "TargetUserName", "Value": "alice"},
				{"Name": "TargetDomainName", "Value": "BANK"},
				{"Name": "IpAddress", "Value": "10.10.1.25"},
				{"Name": "IpPort", "Value": "51514"},
				{"Name": "LogonType", "Value": "3"},
			}},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func bankStarterWindowsSysmonRawEvent() (string, error) {
	payload := map[string]any{
		"Event": map[string]any{
			"System": map[string]any{
				"Provider": map[string]any{"Name": "Microsoft-Windows-Sysmon"},
				"EventID":  1,
				"Computer": "workstation-7.bank.local",
				"Channel":  "Microsoft-Windows-Sysmon/Operational",
			},
			"EventData": map[string]any{"Data": []map[string]any{
				{"Name": "User", "Value": `BANK\alice`},
				{"Name": "Image", "Value": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
				{"Name": "CommandLine", "Value": "powershell.exe -NoProfile"},
				{"Name": "ParentImage", "Value": `C:\Windows\explorer.exe`},
				{"Name": "ProcessId", "Value": "4242"},
			}},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func bankStarterWindowsPowerShellRawEvent(action string) (string, error) {
	script := "Invoke-Command -ComputerName dc1 { Get-ADUser alice }"
	if strings.Contains(strings.ToLower(strings.TrimSpace(action)), "encoded") {
		script = "powershell.exe -NoProfile -EncodedCommand SQBFAFgA"
	}
	payload := map[string]any{
		"Event": map[string]any{
			"System": map[string]any{
				"Provider": map[string]any{"Name": "Microsoft-Windows-PowerShell"},
				"EventID":  4104,
				"Computer": "dc1.bank.local",
				"Channel":  "Microsoft-Windows-PowerShell/Operational",
			},
			"EventData": map[string]any{"Data": []map[string]any{
				{"Name": "User", "Value": `BANK\alice`},
				{"Name": "ScriptBlockText", "Value": script},
				{"Name": "ScriptBlockId", "Value": "script-1"},
			}},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func actionHasFailureSemantics(action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	return strings.Contains(action, "fail") ||
		strings.Contains(action, "deny") ||
		strings.Contains(action, "denied") ||
		strings.Contains(action, "block")
}

func bankStarterParserID(spec bankStarterSourceSpec) string {
	switch strings.TrimSpace(spec.SourceID) {
	case "fortinet.fortigate":
		return bankFortinetFortigateParserID
	case "palo_alto.panos":
		return bankPaloAltoPANOSParserID
	case "cisco.asa":
		return bankCiscoASAParserID
	case "checkpoint.firewall":
		return bankCheckPointFirewallParserID
	case "okta.system_log":
		return bankOktaSystemLogParserID
	case "microsoft.entra_id":
		return bankMicrosoftEntraIDParserID
	case "f5.bigip.asm":
		return bankF5BigIPASMParserID
	case "imperva.waf":
		return bankImpervaWAFParserID
	case "cloudflare.waf":
		return bankCloudflareWAFParserID
	case "microsoft.active_directory_security":
		return bankWindowsSecurityParserID
	case "microsoft.windows_sysmon":
		return bankWindowsSysmonParserID
	case "microsoft.windows_powershell":
		return bankWindowsPowerShellParserID
	}
	return bankStarterCarrierParserID(spec.Format)
}

func bankStarterCarrierParserID(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "cef":
		return bankStarterCEFParserID
	case "leef":
		return bankStarterLEEFParserID
	default:
		return bankStarterJSONParserID
	}
}

func bankStarterParserProfiles() []ParserProfile {
	return []ParserProfile{
		bankStarterParserProfile(bankStarterCEFParserID),
		bankStarterParserProfile(bankStarterLEEFParserID),
		bankStarterParserProfile(bankStarterJSONParserID),
		bankStarterParserProfile(bankFortinetFortigateParserID),
		bankStarterParserProfile(bankPaloAltoPANOSParserID),
		bankStarterParserProfile(bankCiscoASAParserID),
		bankStarterParserProfile(bankCheckPointFirewallParserID),
		bankStarterParserProfile(bankF5BigIPASMParserID),
		bankStarterParserProfile(bankImpervaWAFParserID),
		bankStarterParserProfile(bankCloudflareWAFParserID),
		bankStarterParserProfile(bankOktaSystemLogParserID),
		bankStarterParserProfile(bankMicrosoftEntraIDParserID),
		bankStarterParserProfile(bankWindowsSecurityParserID),
		bankStarterParserProfile(bankWindowsSysmonParserID),
		bankStarterParserProfile(bankWindowsPowerShellParserID),
	}
}

func bankStarterSampleVariants(spec bankStarterSourceSpec) []bankStarterSampleVariant {
	variants := []bankStarterSampleVariant{{
		Suffix:      bankStarterGoodSampleSuffix,
		Description: "Starter " + strings.ToUpper(spec.Format) + " carrier-format event",
		Action:      "allowed",
	}}
	if bankStarterHasFirewallSemantics(spec.SourceID) {
		variants = append(variants, bankStarterSampleVariant{
			Suffix:      bankStarterDeniedSampleSuffix,
			Description: "Denied " + strings.ToUpper(spec.Format) + " firewall event",
			Action:      "denied",
		})
	}
	if bankStarterHasWAFSemantics(spec.SourceID) {
		variants = append(variants, bankStarterSampleVariant{
			Suffix:      bankStarterBlockedSampleSuffix,
			Description: "Blocked " + strings.ToUpper(spec.Format) + " WAF event",
			Action:      "blocked",
		})
	}
	if strings.TrimSpace(spec.SourceID) == "microsoft.windows_powershell" {
		variants = append(variants, bankStarterSampleVariant{
			Suffix:      bankStarterEncodedSampleSuffix,
			Description: "Encoded PowerShell script-block event",
			Action:      "encoded",
		})
	}
	return variants
}

func bankStarterSampleIDs(spec bankStarterSourceSpec) []string {
	variants := bankStarterSampleVariants(spec)
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		out = append(out, bankStarterSampleID(spec, variant))
	}
	return out
}

func bankStarterSampleID(spec bankStarterSourceSpec, variant bankStarterSampleVariant) string {
	return spec.SourceID + "." + strings.TrimSpace(variant.Suffix)
}

func bankStarterHasFirewallSemantics(sourceID string) bool {
	switch strings.TrimSpace(sourceID) {
	case "fortinet.fortigate", "palo_alto.panos", "cisco.asa", "checkpoint.firewall":
		return true
	default:
		return false
	}
}

func bankStarterHasWAFSemantics(sourceID string) bool {
	switch strings.TrimSpace(sourceID) {
	case "f5.bigip.asm", "imperva.waf", "cloudflare.waf":
		return true
	default:
		return false
	}
}

func bankStarterHasIAMSemantics(sourceID string) bool {
	switch strings.TrimSpace(sourceID) {
	case "okta.system_log", "microsoft.entra_id":
		return true
	default:
		return false
	}
}

func bankStarterHasWindowsSemantics(sourceID string) bool {
	switch strings.TrimSpace(sourceID) {
	case "microsoft.active_directory_security", "microsoft.windows_sysmon", "microsoft.windows_powershell":
		return true
	default:
		return false
	}
}

func bankStarterOCSFCategory(sourceClass string) string {
	switch sourceClass {
	case "iam":
		return "identity_access"
	case "dns", "firewall", "proxy", "private_access":
		return "network_activity"
	case "edr":
		return "system_activity"
	case "mail":
		return "email_activity"
	default:
		return "application_activity"
	}
}

func bankStarterOCSFClass(sourceClass string) string {
	switch sourceClass {
	case "iam":
		return "authentication"
	case "dns":
		return "dns_activity"
	case "firewall", "proxy", "private_access":
		return "network_activity"
	case "edr":
		return "process_activity"
	case "mail":
		return "email_activity"
	default:
		return "application_event"
	}
}
