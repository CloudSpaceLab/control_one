package connectordiscovery

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/appcatalog"
)

func TestDiscoverLocalAutoConnectsRunningSafeCatalogService(t *testing.T) {
	got := DiscoverLocal(Options{
		GOOS: "linux",
		Services: []Service{{
			Process:     "nginx",
			ServiceKind: "nginx",
			Port:        80,
		}},
	})
	if len(got) != 1 {
		t.Fatalf("proposals = %#v, want one", got)
	}
	p := got[0]
	if p.Program != "nginx" || !p.AutoConnectEligible || p.RequiresApproval {
		t.Fatalf("nginx proposal not auto eligible: %#v", p)
	}
	if p.Formatter != "nginx" || len(p.Paths) == 0 {
		t.Fatalf("nginx source metadata missing: %#v", p)
	}
	if p.Labels["catalog_version"] != appcatalog.CatalogVersion() {
		t.Fatalf("catalog version = %q, want %q", p.Labels["catalog_version"], appcatalog.CatalogVersion())
	}
}

func TestDiscoverLocalRequiresApprovalForSensitiveBankingPackage(t *testing.T) {
	got := DiscoverLocal(Options{
		GOOS: "linux",
		Packages: []Package{{
			Name:    "temenos-tafj",
			Version: "R24",
			Source:  "rpm",
		}},
	})
	if len(got) != 1 {
		t.Fatalf("proposals = %#v, want one", got)
	}
	p := got[0]
	if p.Program != "temenos-t24" || p.AutoConnectEligible || !p.RequiresApproval || p.Risk != "high" {
		t.Fatalf("temenos proposal should be advisory/high risk: %#v", p)
	}
}

func TestDiscoverLocalRequiresApprovalForMediumRiskDatabaseByDefault(t *testing.T) {
	got := DiscoverLocal(Options{
		GOOS: "linux",
		Services: []Service{{
			Process:     "postgres",
			ServiceKind: "postgres",
			Port:        5432,
		}},
	})
	if len(got) != 1 {
		t.Fatalf("proposals = %#v, want one", got)
	}
	p := got[0]
	if p.Program != "postgresql" || p.AutoConnectEligible || !p.RequiresApproval || p.Risk != "medium" {
		t.Fatalf("postgres proposal should require approval by default: %#v", p)
	}
	if p.Labels["policy_decision"] != "approval_required" {
		t.Fatalf("policy decision label = %q", p.Labels["policy_decision"])
	}
}

func TestDiscoverLocalAllowsMediumRiskWhenPolicyAllows(t *testing.T) {
	got := DiscoverLocal(Options{
		GOOS: "linux",
		Services: []Service{{
			Process:     "postgres",
			ServiceKind: "postgres",
			Port:        5432,
		}},
		AutoConnect: AutoConnectPolicy{AllowMediumRisk: true},
	})
	if len(got) != 1 {
		t.Fatalf("proposals = %#v, want one", got)
	}
	p := got[0]
	if p.Program != "postgresql" || !p.AutoConnectEligible || p.RequiresApproval {
		t.Fatalf("postgres proposal should be auto eligible when policy allows medium risk: %#v", p)
	}
	if p.Labels["policy_decision"] != "auto_eligible" {
		t.Fatalf("policy decision label = %q", p.Labels["policy_decision"])
	}
}

func TestDiscoverLocalPolicyCanRequireApprovalForLowRiskProgram(t *testing.T) {
	got := DiscoverLocal(Options{
		GOOS: "linux",
		Services: []Service{{
			Process:     "nginx",
			ServiceKind: "nginx",
			Port:        80,
		}},
		AutoConnect: AutoConnectPolicy{ApprovalRequiredPrograms: []string{"nginx"}},
	})
	if len(got) != 1 {
		t.Fatalf("proposals = %#v, want one", got)
	}
	p := got[0]
	if p.Program != "nginx" || p.AutoConnectEligible || !p.RequiresApproval {
		t.Fatalf("nginx proposal should require approval by policy: %#v", p)
	}
}

func TestDiscoverLocalSkipsExistingProgram(t *testing.T) {
	got := DiscoverLocal(Options{
		GOOS:             "linux",
		ExistingPrograms: []string{"nginx"},
		Services: []Service{{
			Process:     "nginx",
			ServiceKind: "nginx",
			Port:        80,
		}},
	})
	if len(got) != 0 {
		t.Fatalf("existing source should be skipped: %#v", got)
	}
}

func TestAutoLogSourcesUsesOnlyAutoEligibleProposals(t *testing.T) {
	proposals := DiscoverLocal(Options{
		GOOS: "linux",
		Services: []Service{
			{Process: "nginx", ServiceKind: "nginx", Port: 80},
			{Process: "weblogic", ServiceKind: "weblogic", Port: 7001},
		},
	})
	sources := AutoLogSources(proposals)
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want only nginx", sources)
	}
	if sources[0].Program != "nginx" || sources[0].Type != CollectorTypeFile {
		t.Fatalf("unexpected source: %#v", sources[0])
	}
}

func TestAutoLogSourcesRefusesMalformedApprovalRequiredProposal(t *testing.T) {
	sources := AutoLogSources([]Proposal{{
		ID:                  "local-log:bank-core",
		Kind:                KindLocalLog,
		Program:             "bank-core",
		CollectorType:       CollectorTypeFile,
		AutoConnectEligible: true,
		RequiresApproval:    true,
		Paths:               []string{"/opt/bank-core/logs/app.log"},
		Labels:              map[string]string{"connector_contract": "control_one.local_log.v1"},
	}})
	if len(sources) != 0 {
		t.Fatalf("approval-required proposal must not become an auto log source: %#v", sources)
	}
}
