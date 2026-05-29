package connectordiscovery

import (
	"sort"
	"strconv"
	"strings"

	"github.com/CloudSpaceLab/control_one/internal/appcatalog"
	"github.com/CloudSpaceLab/control_one/internal/config"
)

const (
	KindLocalLog      = "local_log"
	CollectorTypeFile = "file"
)

// Package is the minimal installed-package evidence the connector discovery
// engine needs. It intentionally mirrors the node agent inventory shape
// without importing cmd/nodeagent.
type Package struct {
	Name    string
	Version string
	Source  string
	Arch    string
}

// Service is one listening local service observed on a self-owned host.
type Service struct {
	Process     string
	BinaryPath  string
	ServiceKind string
	Port        int
}

type Options struct {
	GOOS             string
	Packages         []Package
	Services         []Service
	ExistingPrograms []string
	MaxProposals     int
	AutoConnect      AutoConnectPolicy
}

// AutoConnectPolicy keeps local discovery bank-safe by default while leaving
// room for explicit tenant policy to widen automatic collection later.
type AutoConnectPolicy struct {
	AllowMediumRisk          bool     `json:"allow_medium_risk"`
	AllowHighRisk            bool     `json:"allow_high_risk"`
	AutoConnectPrograms      []string `json:"auto_connect_programs,omitempty"`
	ApprovalRequiredPrograms []string `json:"approval_required_programs,omitempty"`
	BlockedPrograms          []string `json:"blocked_programs,omitempty"`
}

type Proposal struct {
	ID                  string            `json:"id"`
	Kind                string            `json:"kind"`
	Program             string            `json:"program"`
	CollectorType       string            `json:"collector_type"`
	Formatter           string            `json:"formatter,omitempty"`
	Paths               []string          `json:"paths,omitempty"`
	Confidence          int               `json:"confidence"`
	Risk                string            `json:"risk"`
	AutoConnectEligible bool              `json:"auto_connect_eligible"`
	RequiresApproval    bool              `json:"requires_approval"`
	Reason              string            `json:"reason,omitempty"`
	Evidence            []string          `json:"evidence,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
}

type candidate struct {
	program  string
	evidence []string
	fromSvc  bool
	fromPkg  bool
}

var serviceKindPrograms = map[string]string{
	"apache":     "apache",
	"caddy":      "caddy",
	"envoy":      "envoy",
	"haproxy":    "haproxy",
	"iis":        "iis",
	"kafka":      "kafka",
	"mongodb":    "mongodb",
	"mysql":      "mysql",
	"nginx":      "nginx",
	"opensearch": "opensearch",
	"postgres":   "postgresql",
	"rabbitmq":   "rabbitmq",
	"redis":      "redis",
	"tomcat":     "tomcat",
	"traefik":    "traefik",
	"weblogic":   "weblogic",
	"websphere":  "websphere",
}

var packageProgramHints = []struct {
	alias   string
	program string
}{
	{"temenos", "temenos-t24"},
	{"t24", "temenos-t24"},
	{"transact", "temenos-t24"},
	{"tafj", "temenos-t24"},
	{"flexcube", "oracle-flexcube"},
	{"fcubs", "oracle-flexcube"},
	{"finacle", "finacle"},
	{"finastra", "finastra-fusion"},
	{"weblogic", "weblogic"},
	{"websphere", "websphere"},
	{"ibm-mq", "ibm-mq"},
	{"ibmmq", "ibm-mq"},
	{"mqseries", "ibm-mq"},
	{"oracle-database", "oracle"},
	{"oracle-xe", "oracle"},
	{"oracledb", "oracle"},
	{"db2", "ibm-db2"},
	{"db2server", "ibm-db2"},
	{"mssql-server", "mssql"},
	{"sqlserver", "mssql"},
	{"nginx", "nginx"},
	{"openresty", "nginx"},
	{"apache2", "apache"},
	{"httpd", "apache"},
	{"haproxy", "haproxy"},
	{"tomcat", "tomcat"},
	{"postgresql", "postgresql"},
	{"postgres", "postgresql"},
	{"mysql-server", "mysql"},
	{"mysqld", "mysql"},
	{"mariadb", "mariadb"},
	{"redis-server", "redis"},
	{"redis", "redis"},
	{"kafka", "kafka"},
	{"rabbitmq-server", "rabbitmq"},
	{"rabbitmq", "rabbitmq"},
	{"mongodb-org", "mongodb"},
	{"mongod", "mongodb"},
}

var highRiskPrograms = map[string]bool{
	"finacle":         true,
	"finastra-fusion": true,
	"ibm-db2":         true,
	"ibm-mq":          true,
	"iis":             true,
	"mssql":           true,
	"oracle":          true,
	"oracle-flexcube": true,
	"temenos-t24":     true,
	"weblogic":        true,
	"websphere":       true,
}

// DiscoverLocal returns local connector proposals using only observed host
// evidence. Running service evidence is required for auto-connect eligibility;
// package-only detections are advisory proposals because installed software is
// not necessarily active or safe to collect from.
func DiscoverLocal(opts Options) []Proposal {
	goos := strings.ToLower(strings.TrimSpace(opts.GOOS))
	if goos == "" {
		goos = "linux"
	}
	existing := programSet(opts.ExistingPrograms)
	candidates := map[string]*candidate{}

	for _, svc := range opts.Services {
		program := programForService(svc)
		if program == "" || existing[program] {
			continue
		}
		c := candidateFor(candidates, program)
		c.fromSvc = true
		c.evidence = appendLimited(c.evidence, serviceEvidence(svc), 8)
	}
	for _, pkg := range opts.Packages {
		program := programForPackage(pkg.Name)
		if program == "" || existing[program] {
			continue
		}
		c := candidateFor(candidates, program)
		c.fromPkg = true
		c.evidence = appendLimited(c.evidence, packageEvidence(pkg), 8)
	}

	out := make([]Proposal, 0, len(candidates))
	for _, c := range candidates {
		profile, ok := appcatalog.LogProfileForProgram(c.program)
		if !ok {
			continue
		}
		paths := appcatalog.LogPathCandidates(c.program, goos)
		if len(paths) == 0 {
			continue
		}
		risk := riskForProgram(c.program)
		auto, requiresApproval, decisionReason := autoConnectDecision(c, profile.AutoCollect, risk, opts.AutoConnect)
		confidence := 75
		if c.fromSvc {
			confidence = 90
		}
		if c.fromPkg && c.fromSvc {
			confidence = 95
		}
		proposal := Proposal{
			ID:                  "local-log:" + c.program,
			Kind:                KindLocalLog,
			Program:             c.program,
			CollectorType:       CollectorTypeFile,
			Formatter:           appcatalog.LogFormatter(c.program),
			Paths:               append([]string(nil), paths...),
			Confidence:          confidence,
			Risk:                risk,
			AutoConnectEligible: auto,
			RequiresApproval:    requiresApproval,
			Reason:              decisionReason,
			Evidence:            dedupeStrings(c.evidence),
			Labels: map[string]string{
				"catalog_version":    profile.CatalogVersion,
				"connector_contract": "control_one.local_log.v1",
				"discovery_source":   "local",
				"parser_profile":     c.program,
				"policy_decision":    policyDecisionLabel(auto, requiresApproval),
				"risk_class":         risk,
			},
		}
		out = append(out, proposal)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].AutoConnectEligible != out[j].AutoConnectEligible {
			return out[i].AutoConnectEligible
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Program < out[j].Program
	})
	if opts.MaxProposals > 0 && len(out) > opts.MaxProposals {
		return out[:opts.MaxProposals]
	}
	return out
}

func AutoLogSources(proposals []Proposal) []config.LogSourceConfig {
	out := make([]config.LogSourceConfig, 0, len(proposals))
	for _, proposal := range proposals {
		if !proposal.AutoConnectEligible || proposal.RequiresApproval || proposal.Kind != KindLocalLog {
			continue
		}
		src := config.LogSourceConfig{
			Program:   proposal.Program,
			Type:      proposal.CollectorType,
			Paths:     append([]string(nil), proposal.Paths...),
			Formatter: proposal.Formatter,
			Labels: map[string]string{
				"catalog_version":    proposal.Labels["catalog_version"],
				"connector_contract": proposal.Labels["connector_contract"],
				"discovery_source":   proposal.Labels["discovery_source"],
				"parser_profile":     proposal.Labels["parser_profile"],
			},
		}
		config.NormalizeLogSourceConfig(&src)
		out = append(out, src)
	}
	return out
}

func candidateFor(candidates map[string]*candidate, program string) *candidate {
	program = strings.ToLower(strings.TrimSpace(program))
	c := candidates[program]
	if c == nil {
		c = &candidate{program: program}
		candidates[program] = c
	}
	return c
}

func programForService(svc Service) string {
	kind := strings.ToLower(strings.TrimSpace(svc.ServiceKind))
	if program := serviceKindPrograms[kind]; program != "" {
		return program
	}
	name := strings.ToLower(strings.TrimSpace(svc.Process))
	if svc.BinaryPath != "" {
		name += " " + strings.ToLower(strings.TrimSpace(svc.BinaryPath))
	}
	switch {
	case strings.Contains(name, "nginx"):
		return "nginx"
	case strings.Contains(name, "apache"), strings.Contains(name, "httpd"):
		return "apache"
	case strings.Contains(name, "haproxy"):
		return "haproxy"
	case strings.Contains(name, "tomcat"):
		return "tomcat"
	case strings.Contains(name, "postgres"):
		return "postgresql"
	case strings.Contains(name, "mysqld"), strings.Contains(name, "mariadb"):
		return "mysql"
	case strings.Contains(name, "redis"):
		return "redis"
	case strings.Contains(name, "kafka"):
		return "kafka"
	case strings.Contains(name, "rabbitmq"):
		return "rabbitmq"
	case strings.Contains(name, "weblogic"):
		return "weblogic"
	case strings.Contains(name, "websphere"):
		return "websphere"
	}
	return ""
}

func programForPackage(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	for _, hint := range packageProgramHints {
		if appcatalog.PackageMatches(name, hint.alias) {
			return hint.program
		}
	}
	return ""
}

func riskForProgram(program string) string {
	if highRiskPrograms[program] {
		return "high"
	}
	switch program {
	case "mongodb", "mysql", "postgresql", "rabbitmq":
		return "medium"
	default:
		return "low"
	}
}

func autoConnectDecision(c *candidate, autoProfile bool, risk string, policy AutoConnectPolicy) (bool, bool, string) {
	program := strings.ToLower(strings.TrimSpace(c.program))
	blockedPrograms := programSet(policy.BlockedPrograms)
	approvalRequiredPrograms := programSet(policy.ApprovalRequiredPrograms)
	autoConnectPrograms := programSet(policy.AutoConnectPrograms)
	if blockedPrograms[program] {
		return false, true, "tenant connector policy blocks automatic collection for this program"
	}
	if approvalRequiredPrograms[program] {
		return false, true, "tenant connector policy requires approval before collection"
	}
	if !c.fromSvc {
		return false, true, "installed package matches a catalog profile; runtime service evidence is required before auto-connect"
	}
	if !autoProfile {
		return false, true, "running local service matches a catalog profile that is not marked for automatic collection"
	}
	switch risk {
	case "low":
		return true, false, "running local service matches a low-risk automatic catalog log profile"
	case "medium":
		if policy.AllowMediumRisk || autoConnectPrograms[program] {
			return true, false, "tenant connector policy allows automatic collection for this medium-risk local service"
		}
		return false, true, "running local service matches a medium-risk catalog profile and needs operator approval"
	case "high", "critical":
		if policy.AllowHighRisk && autoConnectPrograms[program] {
			return true, false, "tenant connector policy explicitly allows automatic collection for this high-risk local service"
		}
		return false, true, "running local service matches a sensitive catalog profile and needs operator approval"
	default:
		return false, true, "running local service has unknown connector risk and needs operator approval"
	}
}

func policyDecisionLabel(autoEligible, requiresApproval bool) string {
	switch {
	case autoEligible:
		return "auto_eligible"
	case requiresApproval:
		return "approval_required"
	default:
		return "proposed"
	}
}

func serviceEvidence(svc Service) string {
	parts := []string{"service"}
	if svc.ServiceKind != "" {
		parts = append(parts, "kind="+strings.ToLower(strings.TrimSpace(svc.ServiceKind)))
	}
	if svc.Process != "" {
		parts = append(parts, "process="+strings.TrimSpace(svc.Process))
	}
	if svc.Port > 0 {
		parts = append(parts, "port="+strconv.Itoa(svc.Port))
	}
	return strings.Join(parts, ":")
}

func packageEvidence(pkg Package) string {
	parts := []string{"package", strings.TrimSpace(pkg.Name)}
	if pkg.Version != "" {
		parts = append(parts, "version="+strings.TrimSpace(pkg.Version))
	}
	if pkg.Source != "" {
		parts = append(parts, "source="+strings.TrimSpace(pkg.Source))
	}
	return strings.Join(parts, ":")
}

func programSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func appendLimited(existing []string, next string, limit int) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return existing
	}
	for _, value := range existing {
		if strings.EqualFold(value, next) {
			return existing
		}
	}
	if len(existing) >= limit {
		return existing
	}
	return append(existing, next)
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
