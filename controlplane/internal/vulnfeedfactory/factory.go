package vulnfeedfactory

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
)

type Input struct {
	Format string
	Name   string
	Data   []byte
}

type Options struct {
	Source      string
	GeneratedAt time.Time
}

func Build(inputs []Input, opts Options) (*offlinebundle.VulnerabilityFeed, error) {
	builder := newBuilder(opts)
	for _, input := range inputs {
		format := normalizeInputFormat(input.Format)
		switch format {
		case "osv":
			if err := ingestOSV(builder, input); err != nil {
				return nil, err
			}
		case "github", "ghsa":
			if err := ingestGitHub(builder, input); err != nil {
				return nil, err
			}
		case "nvd":
			if err := ingestNVD(builder, input); err != nil {
				return nil, err
			}
		case "cisa-kev", "kev":
			if err := ingestCISAKEV(builder, input); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported vulnerability feed input format %q", input.Format)
		}
	}
	return builder.feed(), nil
}

func normalizeInputFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	format = strings.TrimPrefix(format, ".")
	switch format {
	case "json":
		return ""
	case "cisa", "cisa_kev", "cisa-kev", "known_exploited_vulnerabilities":
		return "cisa-kev"
	case "github", "ghsa", "github_advisory", "github-advisory":
		return "github"
	case "nvd", "nvd20", "nvd-2.0":
		return "nvd"
	default:
		return format
	}
}

type builder struct {
	opts       Options
	advisories map[string]*offlinebundle.VulnerabilityAdvisory
}

func newBuilder(opts Options) *builder {
	return &builder{opts: opts, advisories: map[string]*offlinebundle.VulnerabilityAdvisory{}}
}

func (b *builder) upsert(in offlinebundle.VulnerabilityAdvisory) *offlinebundle.VulnerabilityAdvisory {
	id := canonicalAdvisoryID(in.CVEID, "")
	if id == "" {
		return nil
	}
	existing, ok := b.advisories[id]
	if !ok {
		in.CVEID = id
		in.Severity = normalizeSeverity(in.Severity)
		in.References = uniqueStrings(in.References)
		if in.Metadata == nil {
			in.Metadata = map[string]any{}
		}
		b.advisories[id] = &in
		return &in
	}
	if existing.Severity == "" {
		existing.Severity = normalizeSeverity(in.Severity)
	}
	if existing.CVSSScore == nil && in.CVSSScore != nil {
		existing.CVSSScore = in.CVSSScore
	}
	if existing.EPSSScore == nil && in.EPSSScore != nil {
		existing.EPSSScore = in.EPSSScore
	}
	existing.KEV = existing.KEV || in.KEV
	if existing.AdvisoryURL == "" {
		existing.AdvisoryURL = strings.TrimSpace(in.AdvisoryURL)
	}
	existing.References = uniqueStrings(append(existing.References, in.References...))
	existing.AffectedPackages = append(existing.AffectedPackages, in.AffectedPackages...)
	if existing.Metadata == nil {
		existing.Metadata = map[string]any{}
	}
	for k, v := range in.Metadata {
		if strings.TrimSpace(k) != "" {
			existing.Metadata[k] = v
		}
	}
	return existing
}

func (b *builder) feed() *offlinebundle.VulnerabilityFeed {
	generatedAt := b.opts.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	out := &offlinebundle.VulnerabilityFeed{
		SchemaVersion: 1,
		GeneratedAt:   generatedAt.Format(time.RFC3339),
		Source:        strings.TrimSpace(b.opts.Source),
	}
	if out.Source == "" {
		out.Source = "control-one-vulnerability-feed-factory"
	}
	keys := make([]string, 0, len(b.advisories))
	for key := range b.advisories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		adv := *b.advisories[key]
		adv.AffectedPackages = normalizeAffectedPackages(adv.AffectedPackages)
		adv.References = uniqueStrings(adv.References)
		out.Advisories = append(out.Advisories, adv)
	}
	return out
}

func ingestOSV(b *builder, input Input) error {
	var docs []osvAdvisory
	if err := decodeOneOrMany(input.Data, &docs); err != nil {
		return fmt.Errorf("parse OSV %s: %w", input.Name, err)
	}
	for _, doc := range docs {
		id := canonicalAdvisoryID(doc.ID, strings.Join(doc.Aliases, " "))
		if id == "" {
			continue
		}
		adv := offlinebundle.VulnerabilityAdvisory{
			CVEID:            id,
			Severity:         osvSeverityLevel(doc),
			CVSSScore:        osvCVSSScore(doc),
			AdvisoryURL:      firstReferenceURL(doc.References),
			References:       osvReferences(doc),
			AffectedPackages: osvAffectedPackages(doc),
			Metadata: map[string]any{
				"upstream_format": "osv",
				"upstream_id":     strings.TrimSpace(doc.ID),
				"summary":         strings.TrimSpace(doc.Summary),
			},
		}
		b.upsert(adv)
	}
	return nil
}

type osvAdvisory struct {
	ID         string             `json:"id"`
	Aliases    []string           `json:"aliases"`
	Summary    string             `json:"summary"`
	Details    string             `json:"details"`
	Affected   []osvAffected      `json:"affected"`
	Severity   []osvSeverityEntry `json:"severity"`
	References []osvReference     `json:"references"`
}

type osvAffected struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
		PURL      string `json:"purl"`
	} `json:"package"`
	Ranges   []osvRange `json:"ranges"`
	Versions []string   `json:"versions"`
}

type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Introduced   string `json:"introduced"`
	Fixed        string `json:"fixed"`
	LastAffected string `json:"last_affected"`
	Limit        string `json:"limit"`
}

type osvSeverityEntry struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func osvAffectedPackages(doc osvAdvisory) []offlinebundle.VulnerabilityAffectedPkg {
	var out []offlinebundle.VulnerabilityAffectedPkg
	for _, affected := range doc.Affected {
		source := ecosystemToSource(affected.Package.Ecosystem)
		name := strings.TrimSpace(affected.Package.Name)
		if name == "" && strings.TrimSpace(affected.Package.PURL) != "" {
			source = "purl"
			name = strings.TrimSpace(affected.Package.PURL)
		}
		if name == "" {
			continue
		}
		pkg := offlinebundle.VulnerabilityAffectedPkg{
			Name:              name,
			Source:            source,
			VersionScheme:     versionSchemeForSource(source),
			InstalledVersions: uniqueStrings(affected.Versions),
			VersionRanges:     osvVersionRanges(affected.Ranges),
		}
		pkg.FixedVersion = firstFixedVersion(pkg.VersionRanges)
		out = append(out, pkg)
	}
	return out
}

func osvVersionRanges(ranges []osvRange) []offlinebundle.VulnerabilityVersionRange {
	var out []offlinebundle.VulnerabilityVersionRange
	for _, r := range ranges {
		current := offlinebundle.VulnerabilityVersionRange{Type: strings.TrimSpace(r.Type)}
		for _, event := range r.Events {
			if event.Introduced != "" {
				if hasVersionRangeConstraint(current) {
					out = append(out, current)
				}
				current = offlinebundle.VulnerabilityVersionRange{Type: strings.TrimSpace(r.Type), Introduced: strings.TrimSpace(event.Introduced)}
			}
			if event.Fixed != "" {
				current.Fixed = strings.TrimSpace(event.Fixed)
			}
			if event.LastAffected != "" {
				current.LastAffected = strings.TrimSpace(event.LastAffected)
			}
			if event.Limit != "" {
				current.Limit = strings.TrimSpace(event.Limit)
			}
		}
		if hasVersionRangeConstraint(current) {
			out = append(out, current)
		}
	}
	return out
}

func ingestGitHub(b *builder, input Input) error {
	var docs []githubAdvisory
	if err := decodeOneOrMany(input.Data, &docs); err != nil {
		return fmt.Errorf("parse GitHub advisory %s: %w", input.Name, err)
	}
	for _, doc := range docs {
		id := canonicalAdvisoryID(doc.CVEID, strings.Join([]string{doc.GHSAID, doc.ID}, " "))
		if id == "" {
			continue
		}
		adv := offlinebundle.VulnerabilityAdvisory{
			CVEID:            id,
			Severity:         normalizeSeverity(doc.Severity),
			CVSSScore:        doc.CVSS.ScorePtr(),
			AdvisoryURL:      firstNonEmpty(doc.HTMLURL, doc.URL),
			References:       githubReferences(doc),
			AffectedPackages: githubAffectedPackages(doc.Vulnerabilities),
			Metadata: map[string]any{
				"upstream_format": "github_advisory",
				"ghsa_id":         strings.TrimSpace(doc.GHSAID),
				"summary":         strings.TrimSpace(doc.Summary),
			},
		}
		b.upsert(adv)
	}
	return nil
}

type githubAdvisory struct {
	ID              string                `json:"id"`
	GHSAID          string                `json:"ghsa_id"`
	CVEID           string                `json:"cve_id"`
	URL             string                `json:"url"`
	HTMLURL         string                `json:"html_url"`
	Summary         string                `json:"summary"`
	Severity        string                `json:"severity"`
	CVSS            githubCVSS            `json:"cvss"`
	References      []string              `json:"references"`
	Vulnerabilities []githubVulnerability `json:"vulnerabilities"`
}

type githubCVSS struct {
	Score *float64 `json:"score"`
}

func (c githubCVSS) ScorePtr() *float64 {
	if c.Score == nil {
		return nil
	}
	value := *c.Score
	return &value
}

type githubVulnerability struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
	} `json:"package"`
	VulnerableVersionRange string `json:"vulnerable_version_range"`
	FirstPatchedVersion    struct {
		Identifier string `json:"identifier"`
	} `json:"first_patched_version"`
}

func githubAffectedPackages(vulns []githubVulnerability) []offlinebundle.VulnerabilityAffectedPkg {
	var out []offlinebundle.VulnerabilityAffectedPkg
	for _, vuln := range vulns {
		name := strings.TrimSpace(vuln.Package.Name)
		if name == "" {
			continue
		}
		source := ecosystemToSource(vuln.Package.Ecosystem)
		out = append(out, offlinebundle.VulnerabilityAffectedPkg{
			Name:          name,
			Source:        source,
			VersionScheme: versionSchemeForSource(source),
			VersionRange:  normalizeRangeExpression(vuln.VulnerableVersionRange),
			FixedVersion:  strings.TrimSpace(vuln.FirstPatchedVersion.Identifier),
		})
	}
	return out
}

func ingestNVD(b *builder, input Input) error {
	var doc nvdDocument
	if err := json.Unmarshal(input.Data, &doc); err != nil {
		return fmt.Errorf("parse NVD %s: %w", input.Name, err)
	}
	for _, item := range doc.Vulnerabilities {
		id := canonicalAdvisoryID(item.CVE.ID, "")
		if id == "" {
			continue
		}
		score, severity := nvdScoreSeverity(item.CVE.Metrics)
		adv := offlinebundle.VulnerabilityAdvisory{
			CVEID:            id,
			Severity:         severity,
			CVSSScore:        score,
			AdvisoryURL:      "https://nvd.nist.gov/vuln/detail/" + url.PathEscape(id),
			References:       nvdReferences(item.CVE.References.ReferenceData),
			AffectedPackages: nvdAffectedPackages(item.CVE.Configurations),
			Metadata: map[string]any{
				"upstream_format": "nvd",
				"published":       strings.TrimSpace(item.CVE.Published),
				"last_modified":   strings.TrimSpace(item.CVE.LastModified),
			},
		}
		b.upsert(adv)
	}
	return nil
}

type nvdDocument struct {
	Vulnerabilities []struct {
		CVE nvdCVE `json:"cve"`
	} `json:"vulnerabilities"`
}

type nvdCVE struct {
	ID             string            `json:"id"`
	Published      string            `json:"published"`
	LastModified   string            `json:"lastModified"`
	Metrics        nvdMetrics        `json:"metrics"`
	References     nvdReferencesNode `json:"references"`
	Configurations []nvdConfig       `json:"configurations"`
}

type nvdMetrics struct {
	CVSSMetricV31 []nvdCVSSMetric `json:"cvssMetricV31"`
	CVSSMetricV30 []nvdCVSSMetric `json:"cvssMetricV30"`
	CVSSMetricV2  []nvdCVSSMetric `json:"cvssMetricV2"`
}

type nvdCVSSMetric struct {
	CVSSData struct {
		BaseScore    *float64 `json:"baseScore"`
		BaseSeverity string   `json:"baseSeverity"`
	} `json:"cvssData"`
	BaseSeverity string `json:"baseSeverity"`
}

type nvdReferencesNode struct {
	ReferenceData []struct {
		URL string `json:"url"`
	} `json:"referenceData"`
}

type nvdConfig struct {
	Nodes []nvdNode `json:"nodes"`
}

type nvdNode struct {
	CPEMatch []nvdCPEMatch `json:"cpeMatch"`
	Children []nvdNode     `json:"children"`
}

type nvdCPEMatch struct {
	Vulnerable            bool   `json:"vulnerable"`
	Criteria              string `json:"criteria"`
	VersionStartIncluding string `json:"versionStartIncluding"`
	VersionStartExcluding string `json:"versionStartExcluding"`
	VersionEndIncluding   string `json:"versionEndIncluding"`
	VersionEndExcluding   string `json:"versionEndExcluding"`
}

func nvdAffectedPackages(configs []nvdConfig) []offlinebundle.VulnerabilityAffectedPkg {
	var out []offlinebundle.VulnerabilityAffectedPkg
	for _, cfg := range configs {
		for _, node := range cfg.Nodes {
			out = append(out, nvdAffectedFromNode(node)...)
		}
	}
	return out
}

func nvdAffectedFromNode(node nvdNode) []offlinebundle.VulnerabilityAffectedPkg {
	var out []offlinebundle.VulnerabilityAffectedPkg
	for _, match := range node.CPEMatch {
		criteria := strings.TrimSpace(match.Criteria)
		if !match.Vulnerable || criteria == "" {
			continue
		}
		rangeExpr := nvdVersionRangeExpression(match)
		pkg := offlinebundle.VulnerabilityAffectedPkg{
			Name:          criteria,
			Source:        "cpe",
			VersionScheme: "generic",
			VersionRanges: []offlinebundle.VulnerabilityVersionRange{},
		}
		if rangeExpr != "" {
			pkg.VersionRanges = append(pkg.VersionRanges, offlinebundle.VulnerabilityVersionRange{
				Type:  "generic",
				Range: rangeExpr,
			})
		}
		pkg.FixedVersion = strings.TrimSpace(match.VersionEndExcluding)
		out = append(out, pkg)
	}
	for _, child := range node.Children {
		out = append(out, nvdAffectedFromNode(child)...)
	}
	return out
}

func nvdVersionRangeExpression(match nvdCPEMatch) string {
	var constraints []string
	if value := strings.TrimSpace(match.VersionStartIncluding); value != "" {
		constraints = append(constraints, ">= "+value)
	}
	if value := strings.TrimSpace(match.VersionStartExcluding); value != "" {
		constraints = append(constraints, "> "+value)
	}
	if value := strings.TrimSpace(match.VersionEndIncluding); value != "" {
		constraints = append(constraints, "<= "+value)
	}
	if value := strings.TrimSpace(match.VersionEndExcluding); value != "" {
		constraints = append(constraints, "< "+value)
	}
	return strings.Join(constraints, ", ")
}

func ingestCISAKEV(b *builder, input Input) error {
	var doc cisaKEVDocument
	if err := json.Unmarshal(input.Data, &doc); err != nil {
		return fmt.Errorf("parse CISA KEV %s: %w", input.Name, err)
	}
	for _, vuln := range doc.Vulnerabilities {
		id := canonicalAdvisoryID(vuln.CVEID, "")
		if id == "" {
			continue
		}
		adv := offlinebundle.VulnerabilityAdvisory{
			CVEID: id,
			KEV:   true,
			Metadata: map[string]any{
				"cisa_kev":                      true,
				"cisa_vendor_project":           strings.TrimSpace(vuln.VendorProject),
				"cisa_product":                  strings.TrimSpace(vuln.Product),
				"cisa_vulnerability_name":       strings.TrimSpace(vuln.VulnerabilityName),
				"cisa_date_added":               strings.TrimSpace(vuln.DateAdded),
				"cisa_due_date":                 strings.TrimSpace(vuln.DueDate),
				"cisa_required_action":          strings.TrimSpace(vuln.RequiredAction),
				"known_ransomware_campaign_use": strings.TrimSpace(vuln.KnownRansomwareCampaignUse),
			},
		}
		if existing := b.upsert(adv); existing != nil {
			existing.KEV = true
		}
	}
	return nil
}

type cisaKEVDocument struct {
	Vulnerabilities []struct {
		CVEID                      string `json:"cveID"`
		VendorProject              string `json:"vendorProject"`
		Product                    string `json:"product"`
		VulnerabilityName          string `json:"vulnerabilityName"`
		DateAdded                  string `json:"dateAdded"`
		ShortDescription           string `json:"shortDescription"`
		RequiredAction             string `json:"requiredAction"`
		DueDate                    string `json:"dueDate"`
		KnownRansomwareCampaignUse string `json:"knownRansomwareCampaignUse"`
		Notes                      string `json:"notes"`
	} `json:"vulnerabilities"`
}

func decodeOneOrMany[T any](raw []byte, out *[]T) error {
	var many []T
	if err := json.Unmarshal(raw, &many); err == nil {
		*out = many
		return nil
	}
	var wrapper struct {
		Vulns      []T `json:"vulns"`
		Advisories []T `json:"advisories"`
		Items      []T `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		switch {
		case len(wrapper.Vulns) > 0:
			*out = wrapper.Vulns
			return nil
		case len(wrapper.Advisories) > 0:
			*out = wrapper.Advisories
			return nil
		case len(wrapper.Items) > 0:
			*out = wrapper.Items
			return nil
		}
	}
	var one T
	if err := json.Unmarshal(raw, &one); err != nil {
		return err
	}
	*out = []T{one}
	return nil
}

func canonicalAdvisoryID(primary, aliases string) string {
	candidates := append([]string{primary}, strings.FieldsFunc(aliases, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})...)
	for _, candidate := range candidates {
		candidate = strings.ToUpper(strings.TrimSpace(candidate))
		if strings.HasPrefix(candidate, "CVE-") {
			return candidate
		}
	}
	for _, candidate := range candidates {
		candidate = strings.ToUpper(strings.TrimSpace(candidate))
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func ecosystemToSource(ecosystem string) string {
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	if idx := strings.Index(ecosystem, ":"); idx >= 0 {
		ecosystem = ecosystem[:idx]
	}
	switch ecosystem {
	case "debian", "ubuntu":
		return "apt"
	case "go":
		return "go"
	case "maven":
		return "maven"
	case "npm":
		return "npm"
	case "nuget":
		return "nuget"
	case "pip", "pypi":
		return "pypi"
	case "rubygems":
		return "gem"
	default:
		return ecosystem
	}
}

func versionSchemeForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "npm", "pypi", "go", "gomod", "maven", "nuget", "gem":
		return "semver"
	case "apt", "deb", "debian", "ubuntu":
		return "debian"
	case "rpm", "dnf", "yum", "rhel", "redhat":
		return "rpm"
	default:
		return "generic"
	}
}

func normalizeAffectedPackages(in []offlinebundle.VulnerabilityAffectedPkg) []offlinebundle.VulnerabilityAffectedPkg {
	seen := map[string]struct{}{}
	out := make([]offlinebundle.VulnerabilityAffectedPkg, 0, len(in))
	for _, pkg := range in {
		pkg.Name = strings.TrimSpace(pkg.Name)
		pkg.Source = strings.TrimSpace(pkg.Source)
		pkg.Arch = strings.TrimSpace(pkg.Arch)
		pkg.FixedVersion = strings.TrimSpace(pkg.FixedVersion)
		pkg.VersionScheme = strings.TrimSpace(pkg.VersionScheme)
		pkg.VersionRange = normalizeRangeExpression(pkg.VersionRange)
		pkg.InstalledVersion = strings.TrimSpace(pkg.InstalledVersion)
		pkg.InstalledVersions = uniqueStrings(pkg.InstalledVersions)
		pkg.VersionRanges = normalizeVersionRanges(pkg.VersionRanges)
		if pkg.Name == "" {
			continue
		}
		keyBytes, _ := json.Marshal(pkg)
		key := string(keyBytes)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join([]string{out[i].Source, out[i].Name, out[i].FixedVersion}, "|") <
			strings.Join([]string{out[j].Source, out[j].Name, out[j].FixedVersion}, "|")
	})
	return out
}

func normalizeVersionRanges(in []offlinebundle.VulnerabilityVersionRange) []offlinebundle.VulnerabilityVersionRange {
	var out []offlinebundle.VulnerabilityVersionRange
	for _, r := range in {
		r.Type = strings.TrimSpace(r.Type)
		r.Range = normalizeRangeExpression(r.Range)
		r.Introduced = strings.TrimSpace(r.Introduced)
		r.Fixed = strings.TrimSpace(r.Fixed)
		r.LastAffected = strings.TrimSpace(r.LastAffected)
		r.Limit = strings.TrimSpace(r.Limit)
		if hasVersionRangeConstraint(r) {
			out = append(out, r)
		}
	}
	return out
}

func normalizeRangeExpression(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.Contains(value, ",") {
		return value
	}
	parts := strings.Split(value, ",")
	out := parts[:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, ", ")
}

func hasVersionRangeConstraint(r offlinebundle.VulnerabilityVersionRange) bool {
	return strings.TrimSpace(r.Range) != "" || strings.TrimSpace(r.Introduced) != "" ||
		strings.TrimSpace(r.Fixed) != "" || strings.TrimSpace(r.LastAffected) != "" ||
		strings.TrimSpace(r.Limit) != ""
}

func firstFixedVersion(ranges []offlinebundle.VulnerabilityVersionRange) string {
	for _, r := range ranges {
		if strings.TrimSpace(r.Fixed) != "" {
			return strings.TrimSpace(r.Fixed)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
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
	sort.Strings(out)
	return out
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "high", "medium", "low", "none":
		return strings.ToLower(strings.TrimSpace(value))
	case "moderate":
		return "medium"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func osvSeverityLevel(doc osvAdvisory) string {
	for _, item := range doc.Severity {
		if strings.Contains(strings.ToLower(item.Type), "cvss") {
			return ""
		}
	}
	return ""
}

func osvCVSSScore(doc osvAdvisory) *float64 {
	return nil
}

func osvReferences(doc osvAdvisory) []string {
	var refs []string
	for _, ref := range doc.References {
		refs = append(refs, ref.URL)
	}
	return uniqueStrings(refs)
}

func githubReferences(doc githubAdvisory) []string {
	return uniqueStrings(append(doc.References, firstNonEmpty(doc.HTMLURL, doc.URL)))
}

func nvdReferences(refs []struct {
	URL string `json:"url"`
}) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.URL)
	}
	return uniqueStrings(out)
}

func firstReferenceURL(refs []osvReference) string {
	for _, ref := range refs {
		if strings.TrimSpace(ref.URL) != "" {
			return strings.TrimSpace(ref.URL)
		}
	}
	return ""
}

func nvdScoreSeverity(metrics nvdMetrics) (*float64, string) {
	for _, list := range [][]nvdCVSSMetric{metrics.CVSSMetricV31, metrics.CVSSMetricV30, metrics.CVSSMetricV2} {
		for _, item := range list {
			score := item.CVSSData.BaseScore
			severity := firstNonEmpty(item.CVSSData.BaseSeverity, item.BaseSeverity)
			if score != nil || severity != "" {
				return score, normalizeSeverity(severity)
			}
		}
	}
	return nil, ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ExpandInputFiles(format string, paths []string, readFile func(string) ([]byte, error)) ([]Input, error) {
	var out []Input
	for _, path := range paths {
		matches, err := filepath.Glob(path)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			matches = []string{path}
		}
		sort.Strings(matches)
		for _, match := range matches {
			data, err := readFile(match)
			if err != nil {
				return nil, err
			}
			out = append(out, Input{Format: format, Name: match, Data: data})
		}
	}
	return out, nil
}
