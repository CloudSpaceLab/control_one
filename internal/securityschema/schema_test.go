package securityschema

import "testing"

func TestValidateAcceptsNormalizedWindowsEventFields(t *testing.T) {
	fields := map[string]any{
		"event": map[string]any{
			"kind":     "event",
			"category": "authentication",
			"action":   "logon_success",
			"outcome":  "success",
			"code":     "4624",
			"provider": "Microsoft-Windows-Security-Auditing",
			"dataset":  "Security",
		},
		"host": map[string]any{"hostname": "dc1.bank.local"},
		"user": map[string]any{"name": "alice", "domain": "BANK"},
		"source": map[string]any{
			"ip":   "10.10.1.25",
			"port": 51514,
			"user": map[string]any{"name": "svc-collector"},
		},
	}
	if got := Validate(fields); len(got) != 0 {
		t.Fatalf("Validate() = %#v, want no violations", got)
	}
	aliases := ECSAliases(fields)
	if aliases["event.code"] != "4624" || aliases["source.ip"] != "10.10.1.25" || aliases["source.user.name"] != "svc-collector" {
		t.Fatalf("aliases = %#v", aliases)
	}
	ocsf := OCSFAliases(fields)
	wantOCSF := map[string]any{
		"category_name":         "identity_access",
		"class_name":            "authentication",
		"activity_name":         "logon_success",
		"status":                "success",
		"type_uid":              "4624",
		"metadata.product.name": "Microsoft-Windows-Security-Auditing",
		"metadata.log_name":     "Security",
		"src_endpoint.ip":       "10.10.1.25",
		"src_endpoint.port":     51514,
		"actor.user.name":       "svc-collector",
		"user.name":             "alice",
		"user.domain":           "BANK",
	}
	for key, want := range wantOCSF {
		if got := ocsf[key]; got != want {
			t.Fatalf("OCSFAliases()[%s] = %#v, want %#v (all aliases %#v)", key, got, want, ocsf)
		}
	}
	udm := UDMAliases(fields)
	wantUDM := map[string]any{
		"metadata.event_type":         "USER_LOGIN",
		"metadata.product_event_type": "4624",
		"metadata.product_name":       "Microsoft-Windows-Security-Auditing",
		"metadata.log_type":           "Security",
		"principal.ip":                "10.10.1.25",
		"principal.port":              51514,
		"principal.user.userid":       "svc-collector",
		"target.user.userid":          "alice",
	}
	for key, want := range wantUDM {
		if got := udm[key]; got != want {
			t.Fatalf("UDMAliases()[%s] = %#v, want %#v (all aliases %#v)", key, got, want, udm)
		}
	}
}

func TestValidateRejectsBadNormalizedTypes(t *testing.T) {
	fields := map[string]any{
		"source.ip":   "not-an-ip",
		"source.port": "51514",
		"event.code":  4624,
	}
	got := Validate(fields)
	if len(got) != 3 {
		t.Fatalf("Validate() = %#v, want 3 violations", got)
	}
	want := map[string]bool{
		"event.code":  false,
		"source.ip":   false,
		"source.port": false,
	}
	for _, violation := range got {
		if _, ok := want[violation.Field]; !ok {
			t.Fatalf("unexpected violation %#v", violation)
		}
		want[violation.Field] = true
	}
	for field, seen := range want {
		if !seen {
			t.Fatalf("missing violation for %s in %#v", field, got)
		}
	}
}

func TestDictionaryIsSortedAndContainsSchemaAnchors(t *testing.T) {
	fields := Dictionary()
	if len(fields) == 0 {
		t.Fatal("Dictionary() returned no fields")
	}
	for i := 1; i < len(fields); i++ {
		if fields[i-1].Name > fields[i].Name {
			t.Fatalf("dictionary not sorted at %d: %s > %s", i, fields[i-1].Name, fields[i].Name)
		}
	}
	defs := FieldMap()
	for _, field := range []string{"event.kind", "event.category", "source.ip", "destination.ip", "process.command_line"} {
		if _, ok := defs[field]; !ok {
			t.Fatalf("missing dictionary field %s", field)
		}
	}
	if defs["source.ip"].UDMAlias != "principal.ip" || defs["destination.ip"].UDMAlias != "target.ip" {
		t.Fatalf("UDM aliases not anchored: source=%q destination=%q", defs["source.ip"].UDMAlias, defs["destination.ip"].UDMAlias)
	}
	if SchemaName == "" || SchemaVersion != 1 {
		t.Fatalf("schema identity = %q v%d", SchemaName, SchemaVersion)
	}
}

func TestDeriveOCSFEventMapping(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]any
		want   OCSFEventMapping
	}{
		{
			name:   "dns",
			fields: map[string]any{"event.category": "dns"},
			want:   OCSFEventMapping{Category: "network_activity", Class: "dns_activity"},
		},
		{
			name:   "firewall",
			fields: map[string]any{"event.category": "firewall", "event.action": "deny"},
			want:   OCSFEventMapping{Category: "network_activity", Class: "network_activity"},
		},
		{
			name:   "powershell",
			fields: map[string]any{"event.category": "edr", "event.action": "powershell_script_block"},
			want:   OCSFEventMapping{Category: "system_activity", Class: "process_activity"},
		},
		{
			name:   "detection",
			fields: map[string]any{"event.kind": "alert", "rule.id": "controlone.demo"},
			want:   OCSFEventMapping{Category: "findings", Class: "detection_finding"},
		},
		{
			name:   "fallback",
			fields: map[string]any{"event.category": "app"},
			want:   OCSFEventMapping{Category: "application_activity", Class: "application_event"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveOCSFEventMapping(tc.fields); got != tc.want {
				t.Fatalf("DeriveOCSFEventMapping() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestUDMEventTypeDerivation(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{
			name:   "network",
			fields: map[string]any{"event.category": "network", "event.action": "network_connection"},
			want:   "NETWORK_CONNECTION",
		},
		{
			name:   "dns",
			fields: map[string]any{"event.category": "dns"},
			want:   "NETWORK_DNS",
		},
		{
			name:   "process start",
			fields: map[string]any{"event.category": "process", "event.action": "process_start"},
			want:   "PROCESS_LAUNCH",
		},
		{
			name:   "process end",
			fields: map[string]any{"event.category": "process", "event.action": "process_end"},
			want:   "PROCESS_TERMINATION",
		},
		{
			name:   "fallback",
			fields: map[string]any{"event.category": "mail"},
			want:   "GENERIC_EVENT",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UDMEventType(tc.fields); got != tc.want {
				t.Fatalf("UDMEventType() = %q, want %q", got, tc.want)
			}
		})
	}
}
