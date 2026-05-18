package appcatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Profile struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	Category           string           `json:"category"`
	CatalogVersion     string           `json:"catalog_version,omitempty"`
	StackTags          []string         `json:"stack_tags,omitempty"`
	Purposes           []string         `json:"purposes,omitempty"`
	PackageAliases     []string         `json:"package_aliases,omitempty"`
	PathHints          []string         `json:"path_hints,omitempty"`
	RootMarkers        []RootMarker     `json:"root_markers,omitempty"`
	DependencyHints    []DependencyHint `json:"dependency_hints,omitempty"`
	ParserProfileID    string           `json:"parser_profile_id,omitempty"`
	RemediationSkillID string           `json:"remediation_skill_id,omitempty"`
	LogProfiles        []LogProfile     `json:"log_profiles,omitempty"`
}

type RootMarker struct {
	Any []string `json:"any,omitempty"`
	All []string `json:"all,omitempty"`
}

type DependencyHint struct {
	File     string   `json:"file"`
	Any      []string `json:"any,omitempty"`
	Evidence string   `json:"evidence,omitempty"`
}

type LogProfile struct {
	Program        string   `json:"program"`
	Formatter      string   `json:"formatter,omitempty"`
	CatalogVersion string   `json:"catalog_version,omitempty"`
	Paths          []string `json:"paths,omitempty"`
	WindowsPaths   []string `json:"windows_paths,omitempty"`
	AutoCollect    bool     `json:"auto_collect,omitempty"`
}

type Catalog struct {
	Version  string    `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type PurposeDetection struct {
	Purpose    string
	Confidence int
	Evidence   []string
}

type RootDetection struct {
	ProfileID          string
	Name               string
	Category           string
	CatalogVersion     string
	StackTags          []string
	Purposes           []string
	Evidence           []string
	Confidence         int
	ParserProfileID    string
	RemediationSkillID string
	CoverageState      string
}

type fileExistsFunc func(path string) bool
type fileReadFunc func(path string) ([]byte, bool)

const (
	builtinCatalogVersion = "builtin-2026-05-18"

	appCatalogRootEnv        = "CONTROL_ONE_APP_CATALOG_ROOT"
	offlineContentRootEnv    = "CONTROL_ONE_OFFLINE_CONTENT_ROOT"
	appCatalogContentTypeDir = "app_catalog"
)

var purposeOrder = []string{
	"db_node", "load_balancer", "web_server", "app_node", "cache_server", "message_queue",
	"monitoring_server", "security_service", "storage_service", "container_platform",
	"cloud_runtime", "infrastructure_as_code", "integration_platform",
}

func CatalogVersion() string {
	return builtinCatalogVersion
}

func Profiles() []Profile {
	return profilesFromEnv()
}

func ProfilesFromRoot(root string) []Profile {
	return profilesWithActiveOverlays(root)
}

func LoadCatalogFile(path string) ([]Profile, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	return ParseCatalog(data)
}

func ParseCatalog(data []byte) ([]Profile, error) {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err == nil && len(catalog.Profiles) > 0 {
		version := firstNonEmpty(catalog.Version, CatalogVersion())
		return normalizeProfiles(catalog.Profiles, version), nil
	}
	var profiles []Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("parse app catalog: %w", err)
	}
	return normalizeProfiles(profiles, CatalogVersion()), nil
}

func LoadActiveCatalog(root string) ([]Profile, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	pattern := filepath.Join(root, "active", appCatalogContentTypeDir, "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var out []Profile
	for _, path := range matches {
		if strings.HasSuffix(path, ".receipt.json") {
			continue
		}
		profiles, err := LoadCatalogFile(path)
		if err != nil {
			return nil, err
		}
		applyCatalogReceipt(path+".receipt.json", profiles)
		out = append(out, profiles...)
	}
	return out, nil
}

func DetectRoot(root string, exists fileExistsFunc) RootDetection {
	return DetectRootWithFS(root, exists, nil)
}

func DetectRootWithFS(root string, exists fileExistsFunc, read fileReadFunc) RootDetection {
	root = strings.TrimSpace(root)
	if root == "" || strings.Contains(root, "$") {
		return unknownRoot()
	}
	best := RootDetection{}
	bestScore := -1
	for _, profile := range profilesFromEnv() {
		if len(profile.RootMarkers) == 0 && len(profile.PathHints) == 0 && len(profile.DependencyHints) == 0 {
			continue
		}
		pathEvidence := pathHintEvidence(root, profile.PathHints)
		markerEvidence := rootMarkerEvidence(root, profile.RootMarkers, exists)
		dependencyEvidence := dependencyHintEvidence(root, profile.DependencyHints, read)
		evidence := append([]string{}, pathEvidence...)
		evidence = append(evidence, markerEvidence...)
		evidence = append(evidence, dependencyEvidence...)
		evidence = dedupe(evidence)
		if len(evidence) == 0 {
			continue
		}
		score := len(evidence)*10 + len(markerEvidence) + len(dependencyEvidence)*3
		if len(pathEvidence) > 0 {
			score += 10
		}
		if score <= bestScore {
			continue
		}
		bestScore = score
		best = RootDetection{
			ProfileID:          profile.ID,
			Name:               profile.Name,
			Category:           profile.Category,
			CatalogVersion:     profile.CatalogVersion,
			StackTags:          append([]string(nil), profile.StackTags...),
			Purposes:           append([]string(nil), profile.Purposes...),
			Evidence:           evidence,
			Confidence:         80 + minInt(len(evidence)*3, 15),
			ParserProfileID:    profile.ParserProfileID,
			RemediationSkillID: profile.RemediationSkillID,
			CoverageState:      coverageState(profile),
		}
	}
	if best.ProfileID != "" {
		return best
	}
	if exists != nil && (exists(filepath.Join(root, "index.html")) || exists(filepath.Join(root, "index.htm"))) {
		return RootDetection{
			ProfileID:          "static_web",
			Name:               "Static web content",
			Category:           "framework",
			CatalogVersion:     CatalogVersion(),
			StackTags:          []string{"web"},
			Purposes:           []string{"web_server"},
			Evidence:           []string{"index.html"},
			Confidence:         70,
			ParserProfileID:    "web-static",
			RemediationSkillID: "web-static-remediation",
			CoverageState:      "generic_access_log",
		}
	}
	return unknownRoot()
}

func ResolvePackagePurposes(packageNames []string) []PurposeDetection {
	evidence := map[string][]string{}
	confidence := map[string]int{}
	for _, raw := range packageNames {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		for _, profile := range profilesFromEnv() {
			if len(profile.Purposes) == 0 {
				continue
			}
			if !matchesAnyPackageAlias(name, profile.PackageAliases) {
				continue
			}
			for _, purpose := range profile.Purposes {
				evidence[purpose] = appendLimitedEvidence(evidence[purpose], raw)
				next := confidenceForPurpose(purpose)
				if confidence[purpose] < next {
					confidence[purpose] = next
				}
			}
		}
	}
	out := make([]PurposeDetection, 0, len(evidence))
	for _, purpose := range purposeOrder {
		ev := evidence[purpose]
		if len(ev) == 0 {
			continue
		}
		score := confidence[purpose]
		if len(ev) == 1 {
			score -= 5
		}
		out = append(out, PurposeDetection{Purpose: purpose, Confidence: score, Evidence: ev})
	}
	return out
}

func DefaultLogProgramOrder() []string {
	var out []string
	for _, lp := range DefaultLogProfiles() {
		if lp.AutoCollect {
			out = append(out, lp.Program)
		}
	}
	return dedupe(out)
}

func DefaultLogProfiles() []LogProfile {
	var out []LogProfile
	for _, profile := range profilesFromEnv() {
		out = append(out, profile.LogProfiles...)
	}
	return out
}

func LogProfileForProgram(program string) (LogProfile, bool) {
	program = strings.ToLower(strings.TrimSpace(program))
	if program == "" {
		return LogProfile{}, false
	}
	for _, lp := range DefaultLogProfiles() {
		if strings.EqualFold(lp.Program, program) {
			return lp, true
		}
	}
	return LogProfile{}, false
}

func LogPathCandidates(program, goos string) []string {
	program = strings.ToLower(strings.TrimSpace(program))
	if program == "" {
		return nil
	}
	for _, lp := range DefaultLogProfiles() {
		if strings.EqualFold(lp.Program, program) {
			if strings.EqualFold(goos, "windows") && len(lp.WindowsPaths) > 0 {
				return append([]string(nil), lp.WindowsPaths...)
			}
			return append([]string(nil), lp.Paths...)
		}
	}
	return nil
}

func LogFormatter(program string) string {
	program = strings.ToLower(strings.TrimSpace(program))
	for _, lp := range DefaultLogProfiles() {
		if strings.EqualFold(lp.Program, program) {
			if strings.TrimSpace(lp.Formatter) != "" {
				return lp.Formatter
			}
			return "generic"
		}
	}
	return "generic"
}

func KnownProfileIDs() []string {
	profiles := profilesFromEnv()
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profile.ID)
	}
	sort.Strings(out)
	return out
}

type catalogContentReceipt struct {
	BundleID      string `json:"bundle_id"`
	BundleVersion string `json:"bundle_version"`
	Version       string `json:"version"`
	Stale         bool   `json:"stale"`
}

func profilesFromEnv() []Profile {
	root := firstNonEmpty(os.Getenv(appCatalogRootEnv), os.Getenv(offlineContentRootEnv))
	return profilesWithActiveOverlays(root)
}

func profilesWithActiveOverlays(root string) []Profile {
	base := normalizeProfiles(defaultProfiles, CatalogVersion())
	overlays, err := LoadActiveCatalog(root)
	if err != nil || len(overlays) == 0 {
		return base
	}
	return mergeProfiles(base, overlays)
}

func mergeProfiles(base, overlays []Profile) []Profile {
	out := make([]Profile, 0, len(base)+len(overlays))
	index := map[string]int{}
	for _, profile := range base {
		if strings.TrimSpace(profile.ID) == "" {
			continue
		}
		index[strings.ToLower(profile.ID)] = len(out)
		out = append(out, profile)
	}
	for _, profile := range overlays {
		if strings.TrimSpace(profile.ID) == "" {
			continue
		}
		key := strings.ToLower(profile.ID)
		if pos, ok := index[key]; ok {
			out[pos] = profile
			continue
		}
		index[key] = len(out)
		out = append(out, profile)
	}
	return out
}

func normalizeProfiles(profiles []Profile, catalogVersion string) []Profile {
	out := make([]Profile, 0, len(profiles))
	for _, profile := range profiles {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			continue
		}
		if strings.TrimSpace(profile.CatalogVersion) == "" {
			profile.CatalogVersion = firstNonEmpty(catalogVersion, CatalogVersion())
		}
		if strings.TrimSpace(profile.Name) == "" {
			profile.Name = profile.ID
		}
		if strings.TrimSpace(profile.Category) == "" {
			profile.Category = "custom"
		}
		if strings.TrimSpace(profile.RemediationSkillID) == "" && strings.TrimSpace(profile.ParserProfileID) != "" {
			profile.RemediationSkillID = profile.ParserProfileID + "-remediation"
		}
		for i := range profile.LogProfiles {
			if strings.TrimSpace(profile.LogProfiles[i].CatalogVersion) == "" {
				profile.LogProfiles[i].CatalogVersion = profile.CatalogVersion
			}
			if strings.TrimSpace(profile.LogProfiles[i].Formatter) == "" {
				profile.LogProfiles[i].Formatter = "generic"
			}
		}
		out = append(out, profile)
	}
	return out
}

func applyCatalogReceipt(path string, profiles []Profile) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var receipt catalogContentReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return
	}
	version := firstNonEmpty(receipt.BundleVersion, receipt.Version)
	if version == "" {
		return
	}
	catalogVersion := version
	if receipt.BundleID != "" {
		catalogVersion = receipt.BundleID + "@" + version
	}
	for i := range profiles {
		profiles[i].CatalogVersion = catalogVersion
		for j := range profiles[i].LogProfiles {
			profiles[i].LogProfiles[j].CatalogVersion = catalogVersion
		}
	}
}

func rootMarkerEvidence(root string, markers []RootMarker, exists fileExistsFunc) []string {
	if exists == nil {
		return nil
	}
	var evidence []string
	for _, marker := range markers {
		allOK := len(marker.All) > 0
		for _, file := range marker.All {
			if !markerExists(root, file, exists) {
				allOK = false
				break
			}
		}
		if allOK {
			evidence = append(evidence, marker.All...)
		}
		for _, file := range marker.Any {
			if markerExists(root, file, exists) {
				evidence = append(evidence, file)
				break
			}
		}
	}
	return dedupe(evidence)
}

func pathHintEvidence(root string, hints []string) []string {
	lower := strings.ToLower(filepath.ToSlash(root))
	var evidence []string
	for _, hint := range hints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if hint == "" {
			continue
		}
		if strings.Contains(lower, hint) {
			evidence = append(evidence, "path:"+hint)
		}
	}
	return evidence
}

func dependencyHintEvidence(root string, hints []DependencyHint, read fileReadFunc) []string {
	if read == nil {
		return nil
	}
	var evidence []string
	for _, hint := range hints {
		file := strings.TrimSpace(hint.File)
		if file == "" {
			continue
		}
		for _, data := range readDependencyFiles(root, file, read) {
			lower := strings.ToLower(string(data))
			for _, token := range hint.Any {
				token = strings.ToLower(strings.TrimSpace(token))
				if token == "" {
					continue
				}
				if strings.Contains(lower, token) {
					ev := hint.Evidence
					if strings.TrimSpace(ev) == "" {
						ev = file + ":" + token
					}
					evidence = append(evidence, ev)
					break
				}
			}
		}
	}
	return dedupe(evidence)
}

func readDependencyFiles(root, file string, read fileReadFunc) [][]byte {
	if strings.ContainsAny(file, "*?[") {
		matches, err := filepath.Glob(filepath.Join(root, file))
		if err != nil {
			return nil
		}
		out := make([][]byte, 0, len(matches))
		for _, match := range matches {
			if data, ok := read(match); ok {
				out = append(out, data)
			}
		}
		return out
	}
	if data, ok := read(filepath.Join(root, file)); ok {
		return [][]byte{data}
	}
	return nil
}

func markerExists(root, file string, exists fileExistsFunc) bool {
	if strings.ContainsAny(file, "*?[") {
		matches, err := filepath.Glob(filepath.Join(root, file))
		return err == nil && len(matches) > 0
	}
	return exists(filepath.Join(root, file))
}

func unknownRoot() RootDetection {
	return RootDetection{
		ProfileID:          "unknown",
		Name:               "Unknown application",
		Category:           "unknown",
		CatalogVersion:     CatalogVersion(),
		StackTags:          []string{"custom"},
		Confidence:         30,
		ParserProfileID:    "custom-parser-profile",
		RemediationSkillID: "custom-remediation-skill",
		CoverageState:      "skill_required",
	}
}

func coverageState(profile Profile) string {
	if strings.TrimSpace(profile.ParserProfileID) == "" {
		return "skill_required"
	}
	return "profile_available"
}

func matchesAnyPackageAlias(name string, aliases []string) bool {
	for _, alias := range aliases {
		if PackageMatches(name, alias) {
			return true
		}
	}
	return false
}

func PackageMatches(name, pattern string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if name == "" || pattern == "" {
		return false
	}
	if name == pattern {
		return true
	}
	if strings.HasPrefix(name, pattern) && len(name) > len(pattern) {
		next := name[len(pattern)]
		if next < 'a' || next > 'z' {
			return true
		}
	}
	return strings.Contains(name, pattern+"-") ||
		strings.Contains(name, "-"+pattern) ||
		strings.Contains(name, pattern+".") ||
		strings.Contains(name, "."+pattern)
}

func confidenceForPurpose(purpose string) int {
	switch purpose {
	case "db_node":
		return 92
	case "load_balancer":
		return 90
	case "cache_server":
		return 88
	case "message_queue", "app_node":
		return 86
	case "monitoring_server":
		return 84
	case "container_platform", "cloud_runtime", "infrastructure_as_code", "integration_platform":
		return 82
	default:
		return 82
	}
}

func appendLimitedEvidence(existing []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return existing
	}
	for _, value := range existing {
		if strings.EqualFold(value, next) {
			return existing
		}
	}
	if len(existing) >= 6 {
		return existing
	}
	return append(existing, next)
}

func dedupe(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func prof(id, name, category string, purposes, aliases []string, markers []RootMarker, parser string, logs []LogProfile) Profile {
	return Profile{
		ID:                 id,
		Name:               name,
		Category:           category,
		CatalogVersion:     CatalogVersion(),
		Purposes:           purposes,
		PackageAliases:     aliases,
		RootMarkers:        markers,
		ParserProfileID:    parser,
		RemediationSkillID: id + "-remediation",
		LogProfiles:        logs,
	}
}

func withTags(profile Profile, tags ...string) Profile {
	profile.StackTags = append(profile.StackTags, tags...)
	return profile
}

func withPathHints(profile Profile, hints ...string) Profile {
	profile.PathHints = append(profile.PathHints, hints...)
	return profile
}

func withDeps(profile Profile, hints ...DependencyHint) Profile {
	profile.DependencyHints = append(profile.DependencyHints, hints...)
	return profile
}

func marker(files ...string) []RootMarker {
	return []RootMarker{{Any: files}}
}

func allMarker(files ...string) []RootMarker {
	return []RootMarker{{All: files}}
}

func dep(file string, tokens ...string) DependencyHint {
	return DependencyHint{File: file, Any: tokens}
}

func depEv(file, evidence string, tokens ...string) DependencyHint {
	return DependencyHint{File: file, Any: tokens, Evidence: evidence}
}

func lp(program, formatter string, auto bool, paths ...string) LogProfile {
	return LogProfile{Program: program, Formatter: formatter, CatalogVersion: CatalogVersion(), AutoCollect: auto, Paths: paths}
}

func lpw(program, formatter string, auto bool, paths, windows []string) LogProfile {
	return LogProfile{Program: program, Formatter: formatter, CatalogVersion: CatalogVersion(), AutoCollect: auto, Paths: paths, WindowsPaths: windows}
}

var defaultProfiles = []Profile{
	withTags(withPathHints(prof("temenos_transact", "Temenos Transact/T24", "core_banking", []string{"app_node", "integration_platform"}, []string{"temenos", "t24", "transact", "tafj"}, marker("tafj.properties", "TAFJ.properties", "t24.properties"), "temenos-transact", []LogProfile{
		lp("temenos-t24", "generic", false, "/opt/temenos/*/logs/*.log", "/opt/TAFJ/logs/*.log", "/var/log/temenos/*.log"),
	}), "temenos", "t24", "transact", "tafj"), "banking", "core-banking", "java", "j2ee", "spring", "mq"),
	withTags(withPathHints(prof("oracle_flexcube", "Oracle Flexcube", "core_banking", []string{"app_node", "db_node", "integration_platform"}, []string{"flexcube", "fcubs", "oracle-flexcube"}, marker("fcubs.properties", "flexcube.properties"), "oracle-flexcube", []LogProfile{
		lp("oracle-flexcube", "generic", false, "/u01/flexcube/logs/*.log", "/opt/flexcube/logs/*.log", "/u01/oracle/user_projects/domains/*/servers/*/logs/*.log"),
	}), "flexcube", "fcubs", "oracle-flexcube"), "banking", "core-banking", "oracle", "weblogic", "soa"),
	withTags(withPathHints(prof("finacle", "Infosys Finacle", "core_banking", []string{"app_node", "integration_platform"}, []string{"finacle", "infosys-finacle"}, marker("finacle.properties"), "finacle", []LogProfile{
		lp("finacle", "generic", false, "/opt/finacle/logs/*.log", "/var/log/finacle/*.log", "/finacle/logs/*.log"),
	}), "finacle", "infosys-finacle"), "banking", "core-banking", "java", "spring", "kubernetes", "microservices"),
	withTags(withPathHints(prof("finastra_fusion", "Finastra Fusion", "core_banking", []string{"app_node", "cloud_runtime"}, []string{"finastra", "fusion-phoenix", "fusion-essence", "fusion-fabric"}, nil, "finastra-fusion", []LogProfile{
		lpw("finastra-fusion", "generic", false, []string{"/var/log/finastra/*.log", "/opt/finastra/logs/*.log"}, []string{"C:/Finastra/Logs/*.log", "C:/inetpub/logs/LogFiles/W3SVC1/u_ex*.log"}),
	}), "finastra", "fusion-phoenix", "fusion-essence", "fusion-fabric"), "banking", "core-banking", "dotnet", "azure"),
	withTags(withPathHints(prof("mambu", "Mambu integration", "saas", []string{"cloud_runtime", "integration_platform"}, []string{"mambu"}, nil, "mambu-api", nil), "mambu"), "banking", "saas", "api"),
	withTags(withDeps(prof("nestjs", "NestJS", "framework", []string{"app_node"}, []string{"nestjs", "@nestjs/core"}, marker("nest-cli.json"), "nestjs", nil), dep("package.json", "@nestjs/core")), "nodejs", "api"),
	withTags(withDeps(prof("express", "Express.js", "framework", []string{"app_node"}, []string{"express"}, nil, "express", nil), dep("package.json", "\"express\"")), "nodejs", "api"),
	withTags(withDeps(prof("nextjs", "Next.js", "framework", []string{"app_node"}, []string{"nextjs", "next.js"}, marker("next.config.js", "next.config.mjs"), "nextjs", nil), dep("package.json", "\"next\"")), "nodejs", "react", "frontend"),
	withTags(withDeps(prof("react", "React", "frontend", []string{"app_node"}, []string{"react"}, nil, "react", nil), dep("package.json", "\"react\"")), "javascript", "frontend"),
	withTags(withDeps(prof("angular", "Angular", "frontend", []string{"app_node"}, []string{"angular", "@angular/core"}, marker("angular.json"), "angular", nil), dep("package.json", "@angular/core")), "javascript", "frontend"),
	withTags(withDeps(prof("vue", "Vue.js", "frontend", []string{"app_node"}, []string{"vue", "vuejs"}, marker("vue.config.js", "vite.config.js"), "vue", nil), dep("package.json", "\"vue\"")), "javascript", "frontend"),
	withTags(withDeps(prof("jquery", "jQuery", "frontend", []string{"app_node"}, []string{"jquery"}, nil, "jquery", nil), dep("package.json", "\"jquery\"")), "javascript", "frontend"),
	withTags(withDeps(prof("lodash", "Lodash", "library", []string{"app_node"}, []string{"lodash"}, nil, "lodash", nil), dep("package.json", "\"lodash\"")), "javascript"),
	withTags(withDeps(prof("flask", "Flask", "framework", []string{"app_node"}, []string{"flask"}, nil, "flask", nil), dep("requirements.txt", "flask"), dep("pyproject.toml", "flask")), "python", "api"),
	withTags(withDeps(prof("fastapi", "FastAPI", "framework", []string{"app_node"}, []string{"fastapi"}, nil, "fastapi", nil), dep("requirements.txt", "fastapi"), dep("pyproject.toml", "fastapi")), "python", "api"),
	prof("django", "Django", "framework", []string{"app_node"}, []string{"django"}, marker("manage.py"), "django", nil),
	prof("laravel", "Laravel", "framework", []string{"app_node"}, []string{"laravel"}, marker("artisan"), "laravel", nil),
	withTags(withDeps(prof("symfony", "Symfony", "framework", []string{"app_node"}, []string{"symfony"}, marker("symfony.lock"), "symfony", nil), dep("composer.json", "symfony/")), "php", "api"),
	withTags(prof("codeigniter", "CodeIgniter", "framework", []string{"app_node"}, []string{"codeigniter"}, marker("app/Config/Boot", "application/config/config.php", "system/CodeIgniter.php"), "codeigniter", nil), "php", "api"),
	prof("wordpress", "WordPress", "framework", []string{"app_node"}, []string{"wordpress"}, marker("wp-config.php"), "wordpress", nil),
	prof("rails", "Ruby on Rails", "framework", []string{"app_node"}, []string{"rails"}, allMarker("Gemfile", "config.ru"), "rails", nil),
	withTags(withDeps(prof("spring_boot", "Spring Boot", "framework", []string{"app_node"}, []string{"spring-boot", "springboot"}, marker("application.properties", "application.yml", "application.yaml", "pom.xml", "build.gradle"), "spring-boot", nil), dep("pom.xml", "spring-boot"), dep("build.gradle", "spring-boot")), "java", "api"),
	withTags(prof("jakarta_ee", "Jakarta EE/J2EE", "framework", []string{"app_node"}, []string{"jakarta-ee", "j2ee", "javaee"}, marker("WEB-INF/web.xml", "META-INF/application.xml"), "jakarta-ee", nil), "java", "j2ee"),
	prof("java_webapp", "Java web application", "framework", []string{"app_node"}, []string{"war", "java-webapp"}, marker("WEB-INF/web.xml"), "java-webapp", nil),
	withTags(withDeps(prof("micronaut", "Micronaut", "framework", []string{"app_node"}, []string{"micronaut"}, marker("micronaut-cli.yml", "application.yml", "pom.xml", "build.gradle"), "micronaut", nil), dep("pom.xml", "micronaut"), dep("build.gradle", "micronaut")), "java", "api"),
	withTags(withDeps(prof("quarkus", "Quarkus", "framework", []string{"app_node"}, []string{"quarkus"}, marker("application.properties", "pom.xml", "build.gradle"), "quarkus", nil), dep("pom.xml", "quarkus"), dep("build.gradle", "quarkus")), "java", "api"),
	withTags(withDeps(prof("play_framework", "Play Framework", "framework", []string{"app_node"}, []string{"playframework", "play-framework"}, marker("build.sbt", "conf/application.conf"), "play-framework", nil), dep("build.sbt", "playframework")), "java", "scala"),
	withTags(withDeps(prof("vertx", "Vert.x", "framework", []string{"app_node"}, []string{"vertx", "vert.x"}, marker("pom.xml", "build.gradle"), "vertx", nil), dep("pom.xml", "vertx"), dep("build.gradle", "vertx")), "java", "api"),
	withTags(withDeps(prof("apache_camel", "Apache Camel", "integration", []string{"app_node", "integration_platform"}, []string{"camel", "apache-camel"}, marker("pom.xml", "build.gradle"), "apache-camel", nil), dep("pom.xml", "camel-core"), dep("build.gradle", "camel-core")), "java", "integration"),
	withTags(withDeps(prof("aspnet", "ASP.NET Core", "framework", []string{"app_node"}, []string{"aspnet", "asp.net", "aspnetcore-runtime"}, marker("web.config", "appsettings.json", "*.csproj"), "aspnet", nil), dep("*.csproj", "Microsoft.AspNetCore")), "dotnet", "api"),
	withTags(withDeps(prof("ef_core", "Entity Framework Core", "orm", []string{"app_node"}, []string{"entity-framework-core", "efcore"}, marker("*.csproj"), "ef-core", nil), dep("*.csproj", "Microsoft.EntityFrameworkCore")), "dotnet", "database"),
	prof("go", "Go application", "framework", []string{"app_node"}, []string{"golang", "go"}, marker("go.mod"), "go", nil),
	withTags(withDeps(prof("gin", "Gin", "framework", []string{"app_node"}, []string{"gin-gonic", "gin"}, nil, "gin", nil), dep("go.mod", "gin-gonic/gin")), "go", "api"),
	withTags(withDeps(prof("fiber", "Fiber", "framework", []string{"app_node"}, []string{"gofiber", "fiber"}, nil, "fiber", nil), dep("go.mod", "gofiber/fiber")), "go", "api"),
	withTags(withDeps(prof("echo", "Echo", "framework", []string{"app_node"}, []string{"labstack-echo", "echo"}, nil, "echo", nil), dep("go.mod", "labstack/echo")), "go", "api"),
	prof("nodejs", "Node.js", "runtime", []string{"app_node"}, []string{"nodejs", "node.js", "node", "npm", "yarn"}, marker("package.json"), "nodejs", nil),
	prof("java", "Java", "runtime", []string{"app_node"}, []string{"openjdk", "jdk", "jre", "java"}, marker("pom.xml", "build.gradle", "gradlew"), "java", nil),
	prof("python", "Python", "runtime", []string{"app_node"}, []string{"python", "python3", "gunicorn", "uwsgi"}, marker("requirements.txt", "pyproject.toml", "setup.py"), "python", nil),
	prof("ruby", "Ruby", "runtime", []string{"app_node"}, []string{"ruby", "bundler"}, marker("Gemfile"), "ruby", nil),
	prof("php", "PHP", "runtime", []string{"app_node"}, []string{"php", "php-fpm", "composer"}, marker("composer.json"), "php", nil),
	prof("dotnet", ".NET", "runtime", []string{"app_node"}, []string{"dotnet", "aspnetcore-runtime"}, marker("*.csproj", "*.fsproj"), "dotnet", nil),
	prof("nginx", "Nginx/OpenResty", "webserver", []string{"load_balancer", "web_server"}, []string{"nginx", "openresty", "ingress-nginx"}, nil, "nginx", []LogProfile{lpw("nginx", "nginx", true, []string{"/var/log/nginx/access.log", "/var/log/nginx/error.log"}, []string{"C:/nginx/logs/access.log", "C:/nginx/logs/error.log"})}),
	prof("apache", "Apache HTTPD", "webserver", []string{"load_balancer", "web_server"}, []string{"apache2", "httpd", "apache"}, nil, "apache", []LogProfile{lpw("apache", "apache", true, []string{"/var/log/apache2/access.log", "/var/log/apache2/error.log", "/var/log/httpd/access_log", "/var/log/httpd/error_log"}, []string{"C:/Program Files/Apache Group/Apache2/logs/access.log", "C:/Program Files/Apache Group/Apache2/logs/error.log"})}),
	prof("lighttpd", "lighttpd", "webserver", []string{"web_server"}, []string{"lighttpd"}, nil, "apache", []LogProfile{lpw("lighttpd", "apache", true, []string{"/var/log/lighttpd/access.log", "/var/log/lighttpd/error.log"}, []string{"C:/lighttpd/logs/access.log", "C:/lighttpd/logs/error.log"})}),
	prof("tomcat", "Apache Tomcat", "app_server", []string{"app_node"}, []string{"tomcat", "tomcat9", "tomcat10"}, nil, "apache", []LogProfile{lpw("tomcat", "apache", true, []string{"/var/log/tomcat/localhost_access_log.txt", "/var/log/tomcat9/localhost_access_log.txt", "/opt/tomcat/logs/localhost_access_log.txt", "/opt/tomcat/logs/catalina.out"}, []string{"C:/Program Files/Apache Software Foundation/Tomcat/logs/localhost_access_log.txt", "C:/Program Files/Apache Software Foundation/Tomcat/logs/catalina.out"})}),
	prof("haproxy", "HAProxy", "edge_proxy", []string{"load_balancer"}, []string{"haproxy"}, nil, "haproxy", []LogProfile{lpw("haproxy", "haproxy", true, []string{"/var/log/haproxy.log", "/var/log/haproxy/haproxy.log"}, []string{"C:/haproxy/logs/haproxy.log"})}),
	prof("iis", "Microsoft IIS", "webserver", []string{"web_server", "app_node"}, []string{"iis", "w3svc"}, nil, "iis", []LogProfile{lpw("iis", "generic", false, []string{}, []string{"C:/inetpub/logs/LogFiles/W3SVC1/u_ex*.log"})}),
	prof("caddy", "Caddy", "edge_proxy", []string{"load_balancer", "web_server"}, []string{"caddy"}, nil, "caddy", []LogProfile{lp("caddy", "generic", false, "/var/log/caddy/access.log", "/var/log/caddy/error.log")}),
	prof("envoy", "Envoy", "edge_proxy", []string{"load_balancer"}, []string{"envoy"}, nil, "envoy", []LogProfile{lp("envoy", "generic", false, "/var/log/envoy/access.log")}),
	prof("traefik", "Traefik", "edge_proxy", []string{"load_balancer"}, []string{"traefik"}, nil, "traefik", []LogProfile{lp("traefik", "generic", false, "/var/log/traefik/access.log", "/var/log/traefik/traefik.log")}),
	prof("jetty", "Eclipse Jetty", "app_server", []string{"app_node"}, []string{"jetty"}, nil, "jetty", []LogProfile{lp("jetty", "generic", false, "/var/log/jetty/access.log", "/var/log/jetty/jetty.log")}),
	prof("wildfly", "WildFly/JBoss", "app_server", []string{"app_node"}, []string{"wildfly", "jboss", "jbossas"}, nil, "wildfly", []LogProfile{lp("wildfly", "generic", false, "/opt/wildfly/standalone/log/server.log", "/var/log/wildfly/server.log")}),
	prof("weblogic", "Oracle WebLogic", "app_server", []string{"app_node"}, []string{"weblogic", "wls"}, nil, "weblogic", []LogProfile{lp("weblogic", "generic", false, "/u01/oracle/user_projects/domains/base_domain/servers/AdminServer/logs/AdminServer.log")}),
	prof("websphere", "IBM WebSphere", "app_server", []string{"app_node"}, []string{"websphere", "was"}, nil, "websphere", []LogProfile{lp("websphere", "generic", false, "/opt/IBM/WebSphere/AppServer/profiles/AppSrv01/logs/server1/SystemOut.log")}),
	prof("postgresql", "PostgreSQL", "database", []string{"db_node"}, []string{"postgresql", "postgres"}, nil, "postgresql", []LogProfile{lpw("postgresql", "generic", true, []string{"/var/log/postgresql/postgresql-14-main.log", "/var/lib/pgsql/data/log/postgresql.log"}, []string{"C:/Program Files/PostgreSQL/14/data/log/postgresql.log"})}),
	prof("mysql", "MySQL", "database", []string{"db_node"}, []string{"mysql-server", "mysqld", "mysql-client"}, nil, "mysql", []LogProfile{lpw("mysql", "mysql", true, []string{"/var/log/mysql/error.log", "/var/log/mysqld.log"}, []string{"C:/ProgramData/MySQL/MySQL Server 8.0/Data/hostname.err"})}),
	prof("mariadb", "MariaDB", "database", []string{"db_node"}, []string{"mariadb", "mariadb-server"}, nil, "mysql", []LogProfile{lp("mariadb", "mysql", false, "/var/log/mysql/error.log", "/var/log/mariadb/mariadb.log")}),
	prof("mssql", "Microsoft SQL Server", "database", []string{"db_node"}, []string{"mssql-server", "sqlserver"}, nil, "mssql", []LogProfile{lpw("mssql", "generic", false, []string{"/var/opt/mssql/log/errorlog"}, []string{"C:/Program Files/Microsoft SQL Server/MSSQL/Log/ERRORLOG"})}),
	prof("oracle", "Oracle Database", "database", []string{"db_node"}, []string{"oracle-database", "oracle-xe", "oracledb"}, nil, "oracle", []LogProfile{lp("oracle", "generic", false, "/opt/oracle/diag/rdbms/*/*/trace/alert_*.log")}),
	prof("ibm_db2", "IBM Db2", "database", []string{"db_node"}, []string{"db2", "db2server"}, nil, "ibm-db2", []LogProfile{lp("ibm-db2", "generic", false, "/home/db2inst1/sqllib/db2dump/db2diag.log", "/var/log/db2/db2diag.log")}),
	prof("sqlite", "SQLite", "embedded_database", []string{"app_node"}, []string{"sqlite", "sqlite3"}, nil, "sqlite", nil),
	prof("snowflake", "Snowflake", "managed_database", []string{"cloud_runtime"}, []string{"snowflake", "snowflake-connector"}, nil, "snowflake", nil),
	prof("amazon_aurora", "Amazon RDS/Aurora", "managed_database", []string{"cloud_runtime"}, []string{"aurora", "aws-rds", "rds"}, nil, "amazon-rds-aurora", nil),
	prof("cloud_spanner", "Google Cloud Spanner", "managed_database", []string{"cloud_runtime"}, []string{"spanner", "cloud-spanner"}, nil, "cloud-spanner", nil),
	prof("oracle_exadata", "Oracle Exadata", "database_platform", []string{"db_node"}, []string{"exadata", "exadata-db"}, nil, "oracle-exadata", []LogProfile{lp("oracle-exadata", "generic", false, "/opt/oracle.ExaWatcher/archive/*/*.log")}),
	prof("mongodb", "MongoDB", "database", []string{"db_node"}, []string{"mongodb", "mongodb-org", "mongod"}, nil, "mongodb", []LogProfile{lp("mongodb", "generic", false, "/var/log/mongodb/mongod.log")}),
	prof("cassandra", "Apache Cassandra", "database", []string{"db_node"}, []string{"cassandra"}, nil, "cassandra", []LogProfile{lp("cassandra", "generic", false, "/var/log/cassandra/system.log")}),
	prof("cockroachdb", "CockroachDB", "database", []string{"db_node"}, []string{"cockroach", "cockroachdb"}, nil, "cockroachdb", []LogProfile{lp("cockroachdb", "generic", false, "/var/log/cockroachdb/cockroach.log")}),
	prof("redis", "Redis", "cache", []string{"cache_server"}, []string{"redis", "redis-server"}, nil, "redis", []LogProfile{lpw("redis", "generic", true, []string{"/var/log/redis/redis-server.log"}, []string{"C:/Program Files/Redis/logs/redis.log"})}),
	prof("memcached", "Memcached", "cache", []string{"cache_server"}, []string{"memcached"}, nil, "memcached", []LogProfile{lp("memcached", "generic", false, "/var/log/memcached.log")}),
	prof("varnish", "Varnish", "cache", []string{"cache_server", "web_server"}, []string{"varnish", "varnishd"}, nil, "varnish", []LogProfile{lp("varnish", "generic", false, "/var/log/varnish/varnishncsa.log")}),
	prof("kafka", "Apache Kafka", "message_queue", []string{"message_queue"}, []string{"kafka"}, nil, "kafka", []LogProfile{lpw("kafka", "generic", true, []string{"/var/log/kafka/server.log", "/opt/kafka/logs/server.log"}, []string{"C:/kafka/logs/server.log"})}),
	prof("rabbitmq", "RabbitMQ", "message_queue", []string{"message_queue"}, []string{"rabbitmq", "rabbitmq-server"}, nil, "rabbitmq", []LogProfile{lp("rabbitmq", "generic", false, "/var/log/rabbitmq/rabbit@*.log")}),
	prof("ibm_mq", "IBM MQ", "message_queue", []string{"message_queue", "integration_platform"}, []string{"ibmmq", "ibm-mq", "mqseries"}, nil, "ibm-mq", []LogProfile{lp("ibm-mq", "generic", false, "/var/mqm/qmgrs/*/errors/*.LOG", "/var/mqm/errors/*.LOG")}),
	prof("nats", "NATS", "message_queue", []string{"message_queue"}, []string{"nats", "nats-server"}, nil, "nats", []LogProfile{lp("nats", "generic", false, "/var/log/nats/nats.log")}),
	prof("activemq", "ActiveMQ", "message_queue", []string{"message_queue"}, []string{"activemq"}, nil, "activemq", []LogProfile{lp("activemq", "generic", false, "/var/log/activemq/activemq.log")}),
	prof("tibco_ems", "TIBCO EMS", "message_queue", []string{"message_queue", "integration_platform"}, []string{"tibco", "tibco-ems", "ems"}, nil, "tibco-ems", []LogProfile{lp("tibco-ems", "generic", false, "/opt/tibco/ems/*/logs/*.log")}),
	prof("solace", "Solace PubSub+", "message_queue", []string{"message_queue", "integration_platform"}, []string{"solace", "pubsub"}, nil, "solace", []LogProfile{lp("solace", "generic", false, "/var/lib/solace/diags/logs/*.log")}),
	prof("oracle_messaging", "Oracle Enterprise Messaging", "message_queue", []string{"message_queue", "integration_platform"}, []string{"oracle-aq", "oracle-messaging", "oracle-enterprise-messaging"}, nil, "oracle-messaging", nil),
	prof("mosquitto", "Mosquitto", "message_queue", []string{"message_queue"}, []string{"mosquitto"}, nil, "mosquitto", []LogProfile{lp("mosquitto", "generic", false, "/var/log/mosquitto/mosquitto.log")}),
	prof("oracle_fusion_middleware", "Oracle Fusion Middleware/SOA", "integration", []string{"app_node", "integration_platform"}, []string{"oracle-fusion-middleware", "soa-suite", "bpel", "oracle-soa"}, nil, "oracle-fusion-middleware", []LogProfile{lp("oracle-fusion-middleware", "generic", false, "/u01/oracle/user_projects/domains/*/servers/*/logs/*.log")}),
	prof("ibm_api_connect", "IBM API Connect", "api_gateway", []string{"integration_platform", "cloud_runtime"}, []string{"api-connect", "ibm-api-connect", "apic"}, nil, "ibm-api-connect", nil),
	prof("prometheus", "Prometheus", "monitoring", []string{"monitoring_server"}, []string{"prometheus"}, nil, "prometheus", []LogProfile{lp("prometheus", "generic", false, "/var/log/prometheus/prometheus.log")}),
	prof("grafana", "Grafana", "monitoring", []string{"monitoring_server"}, []string{"grafana", "grafana-server"}, nil, "grafana", []LogProfile{lp("grafana", "generic", false, "/var/log/grafana/grafana.log")}),
	prof("telegraf", "Telegraf", "monitoring", []string{"monitoring_server"}, []string{"telegraf"}, nil, "telegraf", []LogProfile{lp("telegraf", "generic", false, "/var/log/telegraf/telegraf.log")}),
	prof("node_exporter", "Prometheus node exporter", "monitoring", []string{"monitoring_server"}, []string{"node-exporter", "node_exporter", "prometheus-node-exporter"}, nil, "node-exporter", nil),
	prof("datadog", "Datadog Agent", "monitoring", []string{"monitoring_server"}, []string{"datadog", "datadog-agent"}, nil, "datadog", []LogProfile{lp("datadog-agent", "generic", false, "/var/log/datadog/agent.log")}),
	prof("zabbix", "Zabbix", "monitoring", []string{"monitoring_server"}, []string{"zabbix", "zabbix-agent", "zabbix-server"}, nil, "zabbix", []LogProfile{lp("zabbix", "generic", false, "/var/log/zabbix/zabbix_server.log", "/var/log/zabbix/zabbix_agentd.log")}),
	prof("nagios", "Nagios", "monitoring", []string{"monitoring_server"}, []string{"nagios", "nagios4"}, nil, "nagios", []LogProfile{lp("nagios", "generic", false, "/var/log/nagios/nagios.log")}),
	prof("kubernetes", "Kubernetes", "container_platform", []string{"container_platform"}, []string{"kubernetes", "kubelet", "kubectl", "kubeadm"}, nil, "kubernetes", []LogProfile{lp("kubernetes", "generic", false, "/var/log/kubelet.log", "/var/log/pods/*/*/*.log", "/var/log/containers/*.log")}),
	prof("docker", "Docker", "container_platform", []string{"container_platform"}, []string{"docker", "docker-ce", "containerd"}, nil, "docker", []LogProfile{lp("docker", "generic", false, "/var/log/docker.log", "/var/log/containers/*.log")}),
	prof("openshift", "OpenShift", "container_platform", []string{"container_platform"}, []string{"openshift", "oc", "cri-o"}, nil, "openshift", []LogProfile{lp("openshift", "generic", false, "/var/log/crio/crio.log", "/var/log/pods/*/*/*.log")}),
	prof("tanzu", "VMware Tanzu", "container_platform", []string{"container_platform"}, []string{"tanzu", "tkgi", "pivotal"}, nil, "tanzu", nil),
	prof("aws_ecs", "AWS ECS", "cloud_runtime", []string{"cloud_runtime"}, []string{"amazon-ecs-agent", "ecs-agent"}, nil, "aws-ecs", []LogProfile{lp("aws-ecs", "generic", false, "/var/log/ecs/ecs-agent.log")}),
	prof("aws_eks", "AWS EKS", "cloud_runtime", []string{"cloud_runtime", "container_platform"}, []string{"eks", "aws-iam-authenticator"}, nil, "aws-eks", nil),
	prof("azure_aks", "Azure AKS", "cloud_runtime", []string{"cloud_runtime", "container_platform"}, []string{"aks", "azure-aks"}, nil, "azure-aks", nil),
	prof("google_gke", "Google GKE", "cloud_runtime", []string{"cloud_runtime", "container_platform"}, []string{"gke", "google-gke"}, nil, "google-gke", nil),
	prof("oci", "Oracle Cloud Infrastructure", "cloud_runtime", []string{"cloud_runtime"}, []string{"oci", "oracle-cloud-agent", "oci-utils"}, nil, "oci", []LogProfile{lp("oci", "generic", false, "/var/log/oracle-cloud-agent/*.log")}),
	prof("azure", "Microsoft Azure", "cloud_runtime", []string{"cloud_runtime"}, []string{"waagent", "azure-linux-agent", "azure-cli"}, nil, "azure", []LogProfile{lp("azure", "generic", false, "/var/log/waagent.log")}),
	prof("terraform", "Terraform", "infrastructure_as_code", []string{"infrastructure_as_code"}, []string{"terraform"}, marker("*.tf"), "terraform", nil),
	prof("helm", "Helm", "infrastructure_as_code", []string{"infrastructure_as_code", "container_platform"}, []string{"helm"}, marker("Chart.yaml", "values.yaml"), "helm", nil),
	prof("keycloak", "Keycloak", "identity", []string{"app_node", "security_service"}, []string{"keycloak"}, nil, "keycloak", []LogProfile{lp("keycloak", "generic", false, "/opt/keycloak/data/log/keycloak.log")}),
	prof("vault", "HashiCorp Vault", "security", []string{"security_service"}, []string{"vault"}, nil, "vault", []LogProfile{lp("vault", "generic", false, "/var/log/vault/vault.log")}),
	prof("consul", "HashiCorp Consul", "service_discovery", []string{"security_service"}, []string{"consul"}, nil, "consul", []LogProfile{lp("consul", "generic", false, "/var/log/consul/consul.log")}),
	prof("etcd", "etcd", "database", []string{"db_node"}, []string{"etcd"}, nil, "etcd", []LogProfile{lp("etcd", "generic", false, "/var/log/etcd/etcd.log")}),
	prof("minio", "MinIO", "storage", []string{"storage_service"}, []string{"minio"}, nil, "minio", []LogProfile{lp("minio", "generic", false, "/var/log/minio/minio.log")}),
}
