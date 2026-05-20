package server

const coverageCatalogVersion = "coverage.truth.v1"

type coverageSupportState string

const (
	coverageStateSupported      coverageSupportState = "supported"
	coverageStatePartial        coverageSupportState = "partial"
	coverageStateRawOnly        coverageSupportState = "raw_only"
	coverageStateUnsupported    coverageSupportState = "unsupported"
	coverageStateManualEvidence coverageSupportState = "manual_evidence"
	coverageStateStale          coverageSupportState = "stale"
	coverageStateException      coverageSupportState = "exception"
	coverageStateNotApplicable  coverageSupportState = "not_applicable"
)

type coverageQualityState string

const (
	coverageQualityFixtureTested    coverageQualityState = "fixture_tested"
	coverageQualityProductionTested coverageQualityState = "production_tested"
)

type coverageDomainDefinition struct {
	Domain      string `json:"domain"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type coverageStateDefinition struct {
	State       string `json:"state"`
	Description string `json:"description"`
}

type coverageQualityDefinition struct {
	State       string `json:"state"`
	Description string `json:"description"`
}

type coverageMatrixRow struct {
	Domain   string                 `json:"domain"`
	Title    string                 `json:"title"`
	State    coverageSupportState   `json:"state"`
	Quality  []coverageQualityState `json:"quality,omitempty"`
	Signals  []string               `json:"signals"`
	Evidence []string               `json:"evidence"`
	Gaps     []string               `json:"gaps,omitempty"`
}

type coverageExplanation struct {
	Domain      string                 `json:"domain"`
	State       coverageSupportState   `json:"state"`
	Quality     []coverageQualityState `json:"quality,omitempty"`
	Rationale   string                 `json:"rationale"`
	Evidence    []string               `json:"evidence"`
	Limitations []string               `json:"limitations,omitempty"`
	Next        []string               `json:"next,omitempty"`
}

type coverageLegend struct {
	States        []coverageStateDefinition   `json:"states"`
	QualityStates []coverageQualityDefinition `json:"quality_states"`
}

var coverageDomainDefinitions = []coverageDomainDefinition{
	{
		Domain:      "telemetry",
		Title:       "Telemetry",
		Description: "Agent and API collection surfaces that record host, log, heartbeat, event, and fleet health signals.",
	},
	{
		Domain:      "parser",
		Title:       "Parser",
		Description: "Normalization of collected raw signals into typed records that downstream views and detections can rely on.",
	},
	{
		Domain:      "detection",
		Title:       "Detection",
		Description: "Rules, correlations, baselines, and findings that turn telemetry into security or health decisions.",
	},
	{
		Domain:      "compliance",
		Title:       "Compliance",
		Description: "Policy, control, evidence, report, and review surfaces used to support audit workflows.",
	},
	{
		Domain:      "remediation",
		Title:       "Remediation",
		Description: "Operator-approved or automated changes that close findings, apply patches, or alter host/network state.",
	},
	{
		Domain:      "vulnerability",
		Title:       "Vulnerability",
		Description: "Package, patch, CVE, and exposure intelligence used to reason about exploitable software risk.",
	},
	{
		Domain:      "posture",
		Title:       "Posture",
		Description: "Rollups and dashboards that summarize fleet, control, firewall, isolation, and service state.",
	},
	{
		Domain:      "ai",
		Title:       "AI",
		Description: "LLM-backed investigation and operator proposal surfaces, including their persisted audit trail.",
	},
	{
		Domain:      "cases",
		Title:       "Cases",
		Description: "Human investigation records, evidence links, and case workflows that preserve analyst decisions.",
	},
}

var coverageStateDefinitions = []coverageStateDefinition{
	{
		State:       string(coverageStateSupported),
		Description: "A first-party path exists and is considered usable for this catalog slice.",
	},
	{
		State:       string(coverageStatePartial),
		Description: "A first-party path exists, but important inputs, workflows, or verification are incomplete.",
	},
	{
		State:       string(coverageStateRawOnly),
		Description: "Raw collection exists, but normalized semantics or downstream decisions are not guaranteed.",
	},
	{
		State:       string(coverageStateUnsupported),
		Description: "No first-party implementation should be claimed for this catalog slice.",
	},
	{
		State:       string(coverageStateManualEvidence),
		Description: "The system can store or present evidence, but a human or external process must supply the proof.",
	},
	{
		State:       string(coverageStateStale),
		Description: "A path exists, but freshness or recency cannot yet be asserted by this static catalog.",
	},
	{
		State:       string(coverageStateException),
		Description: "A path exists only for scoped exceptions or explicitly accepted risk.",
	},
	{
		State:       string(coverageStateNotApplicable),
		Description: "The state is intentionally out of scope for the domain or signal in this catalog slice.",
	},
}

var coverageQualityDefinitions = []coverageQualityDefinition{
	{
		State:       string(coverageQualityFixtureTested),
		Description: "Behavior is covered by focused deterministic fixtures or handler tests.",
	},
	{
		State:       string(coverageQualityProductionTested),
		Description: "Behavior has a production-oriented implementation path or existing operational surface.",
	},
}

var coverageMatrixCatalog = []coverageMatrixRow{
	{
		Domain:  "telemetry",
		Title:   "Agent and event telemetry",
		State:   coverageStateSupported,
		Quality: []coverageQualityState{coverageQualityFixtureTested, coverageQualityProductionTested},
		Signals: []string{
			"agent heartbeat",
			"telemetry metrics",
			"telemetry logs",
			"security events",
			"event ingest journal",
		},
		Evidence: []string{
			"controlplane/internal/server/telemetry.go",
			"controlplane/internal/server/heartbeat.go",
			"controlplane/internal/server/events_ingest.go",
		},
		Gaps: []string{
			"tenant-specific freshness scoring is not read from storage in this catalog slice",
		},
	},
	{
		Domain:  "parser",
		Title:   "Typed parser coverage",
		State:   coverageStateRawOnly,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"generic logs",
			"node services",
			"DB audit discovery states",
			"webserver inventory",
			"connection rows",
		},
		Evidence: []string{
			"controlplane/internal/server/sessions_parse.go",
			"controlplane/internal/server/db_audit_discovery.go",
			"controlplane/internal/server/webservers.go",
			"internal/appcatalog/catalog.go",
		},
		Gaps: []string{
			"raw log ingest exists before every source has a durable typed parser contract",
			"parser freshness is not yet tenant-measured by this endpoint",
		},
	},
	{
		Domain:  "detection",
		Title:   "Rules, correlation, and behavioral findings",
		State:   coverageStatePartial,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"port rules",
			"log rules",
			"correlation rules",
			"behavioral baselines",
			"IP behavior findings",
		},
		Evidence: []string{
			"controlplane/internal/server/rules.go",
			"controlplane/internal/server/correlation.go",
			"controlplane/internal/server/behavioral_api.go",
			"controlplane/internal/server/ip_behavior.go",
		},
		Gaps: []string{
			"detection precision and recall are not asserted by this static catalog",
		},
	},
	{
		Domain:  "compliance",
		Title:   "Compliance evidence and reporting",
		State:   coverageStateManualEvidence,
		Quality: []coverageQualityState{coverageQualityFixtureTested, coverageQualityProductionTested},
		Signals: []string{
			"policy evaluation",
			"control mappings",
			"evidence attachments",
			"audit reports",
			"review workflows",
		},
		Evidence: []string{
			"controlplane/internal/server/compliance.go",
			"controlplane/internal/server/compliance_evidence.go",
			"controlplane/internal/server/compliance_reporting.go",
			"controlplane/internal/server/compliance_posture.go",
		},
		Gaps: []string{
			"manual evidence remains required for controls without automated collection",
		},
	},
	{
		Domain:  "remediation",
		Title:   "Remediation and approval actions",
		State:   coverageStatePartial,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"remediation scripts",
			"approval gate",
			"patch deployments",
			"network blocks",
			"node repair",
		},
		Evidence: []string{
			"controlplane/internal/server/remediation.go",
			"controlplane/internal/server/remediation_approvals.go",
			"controlplane/internal/server/patch.go",
			"controlplane/internal/server/network_security.go",
		},
		Gaps: []string{
			"closed-loop verification is implementation-specific and not summarized here yet",
		},
	},
	{
		Domain:  "vulnerability",
		Title:   "Vulnerability and patch intelligence",
		State:   coverageStatePartial,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"node packages",
			"CVE findings",
			"fixed-version evidence",
			"CVSS/EPSS/KEV fields",
			"patch state",
			"source-row citations",
		},
		Evidence: []string{
			"controlplane/internal/server/node_packages.go",
			"controlplane/internal/server/node_vulnerabilities.go",
			"controlplane/internal/storage/vulnerability_findings.go",
			"controlplane/internal/migrate/sql/0106_node_vulnerability_findings.up.sql",
			"controlplane/internal/server/patch.go",
		},
		Gaps: []string{
			"signed feed import, scanner-grade coverage, and patch-plan verification remain separate milestone work",
		},
	},
	{
		Domain:  "posture",
		Title:   "Fleet and control posture",
		State:   coverageStateStale,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"dashboard overview",
			"control room overview",
			"control posture",
			"signed network-policy desired state",
			"agent network-policy apply and drift receipts",
			"risk score",
			"fleet health",
		},
		Evidence: []string{
			"controlplane/internal/server/dashboard.go",
			"controlplane/internal/server/control_room.go",
			"controlplane/internal/server/compliance_posture.go",
			"controlplane/internal/server/network_policy_desired_state.go",
			"cmd/nodeagent/network_policy.go",
			"controlplane/internal/server/fleet.go",
		},
		Gaps: []string{
			"this endpoint does not yet read last-observed timestamps for tenant-specific freshness",
		},
	},
	{
		Domain:  "ai",
		Title:   "AI investigation and proposals",
		State:   coverageStateException,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"AI config",
			"AI ask",
			"investigation persistence",
			"operator proposals",
			"admin-gated ingest health citations",
		},
		Evidence: []string{
			"controlplane/internal/server/ai_ask.go",
			"controlplane/internal/server/ai_ingest_health_tool.go",
			"controlplane/internal/server/ai_operator_handlers.go",
			"controlplane/internal/server/ai_operator_persistence.go",
		},
		Gaps: []string{
			"AI output must remain advisory and tenant policy exceptions must be explicit",
		},
	},
	{
		Domain:  "cases",
		Title:   "Investigation cases",
		State:   coverageStatePartial,
		Quality: []coverageQualityState{coverageQualityFixtureTested},
		Signals: []string{
			"SOC cases",
			"audit-ready case export",
			"AI investigation incidents",
			"misconduct cases",
			"case evidence links",
			"risk signals",
		},
		Evidence: []string{
			"controlplane/internal/server/soc_cases.go",
			"controlplane/internal/server/ai_case_workflow_tools.go",
			"controlplane/internal/storage/ai_operator.go",
			"controlplane/internal/server/misconduct.go",
		},
		Gaps: []string{
			"linked actions, ownership workflow, and binary evidence attachments are still narrower than a full SOC case workspace",
		},
	},
}

var coverageExplanationsCatalog = []coverageExplanation{
	{
		Domain:    "telemetry",
		State:     coverageStateSupported,
		Quality:   []coverageQualityState{coverageQualityFixtureTested, coverageQualityProductionTested},
		Rationale: "The server already exposes first-party ingestion and read paths for agent heartbeat, telemetry metrics, logs, and events. This catalog does not imply every tenant currently has fresh data.",
		Evidence: []string{
			"controlplane/internal/server/telemetry.go",
			"controlplane/internal/server/heartbeat.go",
			"controlplane/internal/server/events_ingest.go",
		},
		Limitations: []string{
			"tenant freshness is not computed without a future storage-backed layer",
		},
		Next: []string{
			"add live tenant rollups for last_seen_at and accepted/rejected ingest counts",
		},
	},
	{
		Domain:    "parser",
		State:     coverageStateRawOnly,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "Collection can retain raw logs and service facts, while typed parser guarantees are narrower and source-specific. The conservative claim is raw-only until parser contracts are enumerated per source.",
		Evidence: []string{
			"controlplane/internal/server/sessions_parse.go",
			"controlplane/internal/server/webservers.go",
			"internal/appcatalog/catalog.go",
		},
		Limitations: []string{
			"not every raw event has normalized fields suitable for detections",
		},
		Next: []string{
			"split parser coverage by source and parser version",
		},
	},
	{
		Domain:    "detection",
		State:     coverageStatePartial,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "Rules, correlation, and behavioral APIs exist, but this slice does not assert tuned detections for every telemetry type or environment.",
		Evidence: []string{
			"controlplane/internal/server/rules.go",
			"controlplane/internal/server/correlation.go",
			"controlplane/internal/server/behavioral_api.go",
			"controlplane/internal/server/ip_behavior.go",
		},
		Limitations: []string{
			"precision, recall, and alert routing are outside this static endpoint",
		},
		Next: []string{
			"attach each detection to required telemetry and parser inputs",
		},
	},
	{
		Domain:    "compliance",
		State:     coverageStateManualEvidence,
		Quality:   []coverageQualityState{coverageQualityFixtureTested, coverageQualityProductionTested},
		Rationale: "Compliance workflows can evaluate policies and collect evidence, but controls without automated collection still depend on manual evidence and review.",
		Evidence: []string{
			"controlplane/internal/server/compliance.go",
			"controlplane/internal/server/compliance_evidence.go",
			"controlplane/internal/server/compliance_reporting.go",
			"controlplane/internal/server/compliance_posture.go",
		},
		Limitations: []string{
			"manual evidence is explicitly not equivalent to automated control proof",
		},
		Next: []string{
			"join automated control mappings to tenant evidence and report windows",
		},
	},
	{
		Domain:    "remediation",
		State:     coverageStatePartial,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "Remediation scripts, approvals, patches, network blocks, and repair actions exist, but verification and rollback guarantees vary by action type.",
		Evidence: []string{
			"controlplane/internal/server/remediation.go",
			"controlplane/internal/server/remediation_approvals.go",
			"controlplane/internal/server/patch.go",
			"controlplane/internal/server/network_security.go",
		},
		Limitations: []string{
			"not all remediation paths have uniform post-action verification",
		},
		Next: []string{
			"report action-specific apply, verify, rollback, and approval states",
		},
	},
	{
		Domain:    "vulnerability",
		State:     coverageStatePartial,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "The codebase can now persist and query CVE/package/fixed-version findings with CVSS, EPSS, KEV, source references, and citation IDs. This is partial vulnerability intelligence, not a claim of complete scanner or feed coverage.",
		Evidence: []string{
			"controlplane/internal/server/node_packages.go",
			"controlplane/internal/server/node_vulnerabilities.go",
			"controlplane/internal/storage/vulnerability_findings.go",
			"controlplane/internal/migrate/sql/0106_node_vulnerability_findings.up.sql",
			"controlplane/internal/server/patch.go",
		},
		Limitations: []string{
			"findings must come from trusted import/matching paths; package presence alone is not vulnerability intelligence",
			"signed offline bundle import and scanner breadth are not complete in this slice",
		},
		Next: []string{
			"add signed feed bundle import, package-to-CVE matching provenance, patch plans, and verification receipts before upgrading this state",
		},
	},
	{
		Domain:    "posture",
		State:     coverageStateStale,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "Posture views exist, but this static catalog cannot assert tenant freshness or whether every posture signal was recently observed.",
		Evidence: []string{
			"controlplane/internal/server/dashboard.go",
			"controlplane/internal/server/control_room.go",
			"controlplane/internal/server/compliance_posture.go",
			"controlplane/internal/server/fleet.go",
		},
		Limitations: []string{
			"freshness requires live reads from node, service, firewall, compliance, and event stores",
		},
		Next: []string{
			"compute per-tenant stale windows from last observed timestamps",
		},
	},
	{
		Domain:    "ai",
		State:     coverageStateException,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "AI workflows are wired for investigation and proposals, but their outputs are advisory and should be treated as policy-gated exceptions rather than autonomous truth.",
		Evidence: []string{
			"controlplane/internal/server/ai_ask.go",
			"controlplane/internal/server/ai_ingest_health_tool.go",
			"controlplane/internal/server/ai_operator_handlers.go",
			"controlplane/internal/server/ai_operator_persistence.go",
		},
		Limitations: []string{
			"model quality and provider availability are not measured by this endpoint",
		},
		Next: []string{
			"surface provider health, persisted proposal outcomes, and tenant AI policy",
		},
	},
	{
		Domain:    "cases",
		State:     coverageStatePartial,
		Quality:   []coverageQualityState{coverageQualityFixtureTested},
		Rationale: "SOC cases can list, retrieve, annotate, and export AI investigation incidents with source-row citations, while misconduct cases retain their existing evidence and risk-signal workflow. This is useful case handling, but not yet a full collaborative SOC workspace.",
		Evidence: []string{
			"controlplane/internal/server/soc_cases.go",
			"controlplane/internal/server/ai_case_workflow_tools.go",
			"controlplane/internal/storage/ai_operator.go",
			"controlplane/internal/server/misconduct.go",
		},
		Limitations: []string{
			"linked actions, ownership workflow, and binary evidence attachments are not first-class across all case types yet",
		},
		Next: []string{
			"promote notes, linked actions, timelines, and exports into a unified SOC case model",
		},
	},
}

func buildCoverageLegend() coverageLegend {
	return coverageLegend{
		States:        append([]coverageStateDefinition(nil), coverageStateDefinitions...),
		QualityStates: append([]coverageQualityDefinition(nil), coverageQualityDefinitions...),
	}
}

func cloneCoverageDomains() []coverageDomainDefinition {
	return append([]coverageDomainDefinition(nil), coverageDomainDefinitions...)
}

func cloneCoverageMatrix() []coverageMatrixRow {
	out := make([]coverageMatrixRow, len(coverageMatrixCatalog))
	for i, row := range coverageMatrixCatalog {
		out[i] = row
		out[i].Quality = append([]coverageQualityState(nil), row.Quality...)
		out[i].Signals = append([]string(nil), row.Signals...)
		out[i].Evidence = append([]string(nil), row.Evidence...)
		out[i].Gaps = append([]string(nil), row.Gaps...)
	}
	return out
}

func cloneCoverageExplanations() []coverageExplanation {
	out := make([]coverageExplanation, len(coverageExplanationsCatalog))
	for i, row := range coverageExplanationsCatalog {
		out[i] = row
		out[i].Quality = append([]coverageQualityState(nil), row.Quality...)
		out[i].Evidence = append([]string(nil), row.Evidence...)
		out[i].Limitations = append([]string(nil), row.Limitations...)
		out[i].Next = append([]string(nil), row.Next...)
	}
	return out
}
