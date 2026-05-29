package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type AppDependencyInfo struct {
	AppRoot        string         `json:"app_root,omitempty"`
	Ecosystem      string         `json:"ecosystem"`
	Name           string         `json:"name"`
	Version        string         `json:"version,omitempty"`
	PackageManager string         `json:"package_manager,omitempty"`
	ManifestPath   string         `json:"manifest_path,omitempty"`
	Scope          string         `json:"scope,omitempty"`
	License        string         `json:"license,omitempty"`
	PURL           string         `json:"purl,omitempty"`
	CPE            string         `json:"cpe,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type appDependenciesPayload struct {
	Dependencies []AppDependencyInfo `json:"dependencies"`
}

type appDependencyScanOptions struct {
	ScanRoots              []string
	IncludeDevDependencies bool
	MaxDepth               int
	MaxManifests           int
	MaxFileBytes           int64
}

type appDependencyCollectorOptions struct {
	appDependencyScanOptions
	Interval time.Duration
	NodeID   string
}

func defaultAppDependencyScanRoots() []string {
	if runtime.GOOS == "windows" {
		return []string{
			`C:\inetpub\wwwroot`,
			`C:\apps`,
			`C:\ProgramData\ControlOne\apps`,
		}
	}
	return []string{"/srv", "/opt", "/var/www"}
}

func runAppDependencyCollector(ctx context.Context, client *api.Client, log *zap.Logger, opts appDependencyCollectorOptions) {
	logger := log.Named("app-dependencies")
	interval := opts.Interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if opts.NodeID == "" || client == nil {
		logger.Warn("app dependency collector not started: missing client or node id")
		return
	}
	logger.Info("starting app dependency collector",
		zap.String("node_id", opts.NodeID),
		zap.Duration("interval", interval),
		zap.Strings("scan_roots", opts.ScanRoots),
	)

	first := time.NewTimer(20 * time.Second)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	tick := func() {
		deps, err := collectAppDependencies(opts.appDependencyScanOptions)
		if err != nil {
			logger.Debug("collect app dependencies failed", zap.Error(err))
			return
		}
		if err := postAppDependencies(ctx, client, logger, opts.NodeID, deps); err != nil {
			logger.Debug("post app dependencies failed", zap.Error(err))
			return
		}
		logger.Debug("app dependencies posted", zap.Int("count", len(deps)))
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("app dependency collector stopped")
			return
		case <-first.C:
			tick()
		case <-ticker.C:
			tick()
		}
	}
}

func collectAppDependencies(opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	opts = normalizeAppDependencyScanOptions(opts)
	var out []AppDependencyInfo
	manifestCount := 0
	for _, root := range opts.ScanRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		if st, err := os.Stat(cleanRoot); err != nil || !st.IsDir() {
			continue
		}
		err := filepath.WalkDir(cleanRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			rel, err := filepath.Rel(cleanRoot, path)
			if err != nil {
				return nil
			}
			depth := pathDepth(rel)
			if d.IsDir() {
				if rel != "." && shouldSkipAppDependencyDir(d.Name()) {
					return filepath.SkipDir
				}
				if depth > opts.MaxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if manifestCount >= opts.MaxManifests {
				return filepath.SkipAll
			}
			if depth > opts.MaxDepth || !isAppDependencyManifest(path) {
				return nil
			}
			deps, err := parseAppDependencyManifest(path, opts)
			if err != nil {
				return nil
			}
			if len(deps) > 0 {
				manifestCount++
				out = append(out, deps...)
			}
			return nil
		})
		if err != nil && err != filepath.SkipAll {
			return nil, err
		}
		if manifestCount >= opts.MaxManifests {
			break
		}
	}
	return dedupeAppDependencies(out), nil
}

func normalizeAppDependencyScanOptions(opts appDependencyScanOptions) appDependencyScanOptions {
	if len(opts.ScanRoots) == 0 {
		opts.ScanRoots = defaultAppDependencyScanRoots()
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 8
	}
	if opts.MaxManifests <= 0 {
		opts.MaxManifests = 512
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = 1 << 20
	}
	return opts
}

func pathDepth(rel string) int {
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

func shouldSkipAppDependencyDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".git", ".hg", ".svn", ".idea", ".vscode",
		"node_modules", "vendor", "target", "dist", "build",
		"bin", "obj", ".venv", "venv", "__pycache__", ".tox":
		return true
	default:
		return false
	}
}

func isAppDependencyManifest(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "package-lock.json", "npm-shrinkwrap.json", "package.json",
		"requirements.txt", "requirements-prod.txt", "requirements.lock",
		"go.mod", "pom.xml", "packages.config", "build.gradle", "build.gradle.kts",
		"bom.json", "sbom.json", "cyclonedx.json", "spdx.json":
		return true
	}
	return strings.HasSuffix(base, ".csproj") ||
		strings.HasSuffix(base, ".cdx.json") ||
		strings.HasSuffix(base, ".spdx.json")
}

func parseAppDependencyManifest(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == "package-lock.json" || base == "npm-shrinkwrap.json":
		return parseNpmLock(path, opts)
	case base == "package.json":
		return parseNpmPackageJSON(path, opts)
	case strings.HasPrefix(base, "requirements"):
		return parseRequirementsTxt(path, opts)
	case base == "go.mod":
		return parseGoMod(path, opts)
	case base == "pom.xml":
		return parseMavenPOM(path, opts)
	case base == "packages.config":
		return parseNuGetPackagesConfig(path, opts)
	case strings.HasSuffix(base, ".csproj"):
		return parseCSProj(path, opts)
	case base == "build.gradle" || base == "build.gradle.kts":
		return parseGradleBuild(path, opts)
	case strings.Contains(base, "bom") || strings.Contains(base, "sbom") || strings.Contains(base, "cyclonedx") || strings.Contains(base, "spdx"):
		return parseSBOMJSON(path, opts)
	default:
		return nil, nil
	}
}

func postAppDependencies(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, deps []AppDependencyInfo) error {
	body, err := json.Marshal(appDependenciesPayload{Dependencies: deps})
	if err != nil {
		return fmt.Errorf("marshal app dependencies: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := client.Do(callCtx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/app-dependencies", body)
	if err != nil {
		return fmt.Errorf("post app dependencies: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("app dependencies status %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}

func parseNpmLock(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	var lock struct {
		Packages     map[string]npmLockPackage `json:"packages"`
		Dependencies map[string]npmLockDep     `json:"dependencies"`
	}
	if err := decodeJSONFile(path, opts.MaxFileBytes, &lock); err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	for key, pkg := range lock.Packages {
		name := npmPackageNameFromLockKey(key)
		if name == "" || strings.TrimSpace(pkg.Version) == "" {
			continue
		}
		if !opts.IncludeDevDependencies && (pkg.Dev || pkg.DevOptional) {
			continue
		}
		out = append(out, newAppDependency(filepath.Dir(path), "npm", name, pkg.Version, "npm", path, npmScope(pkg.Dev, pkg.Optional), pkg.License, nil))
	}
	for name, dep := range lock.Dependencies {
		out = append(out, npmDepsFromLockV1(filepath.Dir(path), path, name, dep, opts.IncludeDevDependencies)...)
	}
	return out, nil
}

type npmLockPackage struct {
	Version     string `json:"version"`
	Dev         bool   `json:"dev"`
	Optional    bool   `json:"optional"`
	DevOptional bool   `json:"devOptional"`
	License     string `json:"license"`
}

type npmLockDep struct {
	Version      string                `json:"version"`
	Dev          bool                  `json:"dev"`
	Optional     bool                  `json:"optional"`
	License      string                `json:"license"`
	Dependencies map[string]npmLockDep `json:"dependencies"`
}

func npmPackageNameFromLockKey(key string) string {
	key = strings.Trim(strings.TrimSpace(key), "/")
	if key == "" {
		return ""
	}
	const marker = "node_modules/"
	idx := strings.LastIndex(key, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(key[idx+len(marker):])
}

func npmDepsFromLockV1(appRoot, manifestPath, name string, dep npmLockDep, includeDev bool) []AppDependencyInfo {
	var out []AppDependencyInfo
	if strings.TrimSpace(name) != "" && strings.TrimSpace(dep.Version) != "" && (includeDev || !dep.Dev) {
		out = append(out, newAppDependency(appRoot, "npm", name, dep.Version, "npm", manifestPath, npmScope(dep.Dev, dep.Optional), dep.License, nil))
	}
	for childName, child := range dep.Dependencies {
		out = append(out, npmDepsFromLockV1(appRoot, manifestPath, childName, child, includeDev)...)
	}
	return out
}

func parseNpmPackageJSON(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	var manifest struct {
		Dependencies         map[string]string `json:"dependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
	}
	if err := decodeJSONFile(path, opts.MaxFileBytes, &manifest); err != nil {
		return nil, err
	}
	out := npmPackageJSONDeps(filepath.Dir(path), path, manifest.Dependencies, "production")
	out = append(out, npmPackageJSONDeps(filepath.Dir(path), path, manifest.OptionalDependencies, "optional")...)
	if opts.IncludeDevDependencies {
		out = append(out, npmPackageJSONDeps(filepath.Dir(path), path, manifest.DevDependencies, "development")...)
	}
	return out, nil
}

func npmPackageJSONDeps(appRoot, manifestPath string, deps map[string]string, scope string) []AppDependencyInfo {
	out := make([]AppDependencyInfo, 0, len(deps))
	for name, spec := range deps {
		name = strings.TrimSpace(name)
		spec = strings.TrimSpace(spec)
		if name == "" || spec == "" {
			continue
		}
		version := ""
		meta := map[string]any{"version_spec": spec}
		if looksPinnedSemver(spec) {
			version = strings.TrimPrefix(spec, "v")
			meta = nil
		}
		out = append(out, newAppDependency(appRoot, "npm", name, version, "npm", manifestPath, scope, "", meta))
	}
	return out
}

var requirementExactRE = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)(?:\[[^\]]+\])?\s*={2,3}\s*([^\s;]+)`)

func parseRequirementsTxt(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	data, err := readLimitedFile(path, opts.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(stripInlineComment(scanner.Text()))
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "git+") || strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			continue
		}
		match := requirementExactRE.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		name := canonicalPyPIName(match[1])
		version := strings.TrimSpace(match[2])
		out = append(out, newAppDependency(filepath.Dir(path), "pypi", name, version, "pip", path, "production", "", nil))
	}
	return out, scanner.Err()
}

func parseGoMod(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	data, err := readLimitedFile(path, opts.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	inRequire := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "require (") {
			inRequire = true
			continue
		}
		if inRequire && raw == ")" {
			inRequire = false
			continue
		}
		line := raw
		if !inRequire {
			line = strings.TrimPrefix(line, "require ")
			if line == raw {
				continue
			}
		}
		indirect := strings.Contains(line, "// indirect")
		line = stripGoComment(line)
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		scope := "direct"
		if indirect {
			scope = "indirect"
		}
		out = append(out, newAppDependency(filepath.Dir(path), "go", fields[0], fields[1], "go", path, scope, "", nil))
	}
	return out, scanner.Err()
}

type csprojProject struct {
	ItemGroups []struct {
		PackageReferences []csprojPackageReference `xml:"PackageReference"`
	} `xml:"ItemGroup"`
}

type csprojPackageReference struct {
	Include       string `xml:"Include,attr"`
	Update        string `xml:"Update,attr"`
	VersionAttr   string `xml:"Version,attr"`
	VersionElem   string `xml:"Version"`
	PrivateAssets string `xml:"PrivateAssets"`
}

func parseCSProj(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	var project csprojProject
	if err := decodeXMLFile(path, opts.MaxFileBytes, &project); err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	for _, group := range project.ItemGroups {
		for _, ref := range group.PackageReferences {
			name := strings.TrimSpace(firstNonEmptyAppDep(ref.Include, ref.Update))
			version := strings.TrimSpace(firstNonEmptyAppDep(ref.VersionAttr, ref.VersionElem))
			if name == "" {
				continue
			}
			scope := "production"
			if strings.EqualFold(strings.TrimSpace(ref.PrivateAssets), "all") {
				scope = "private_assets"
			}
			out = append(out, newAppDependency(filepath.Dir(path), "nuget", name, version, "dotnet", path, scope, "", nil))
		}
	}
	return out, nil
}

type nugetPackagesConfig struct {
	Packages []struct {
		ID      string `xml:"id,attr"`
		Version string `xml:"version,attr"`
	} `xml:"package"`
}

func parseNuGetPackagesConfig(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	var config nugetPackagesConfig
	if err := decodeXMLFile(path, opts.MaxFileBytes, &config); err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	for _, pkg := range config.Packages {
		if strings.TrimSpace(pkg.ID) == "" {
			continue
		}
		out = append(out, newAppDependency(filepath.Dir(path), "nuget", pkg.ID, pkg.Version, "nuget", path, "production", "", nil))
	}
	return out, nil
}

type mavenProject struct {
	Dependencies []mavenDependency `xml:"dependencies>dependency"`
}

type mavenDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}

func parseMavenPOM(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	var project mavenProject
	if err := decodeXMLFile(path, opts.MaxFileBytes, &project); err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	for _, dep := range project.Dependencies {
		groupID := strings.TrimSpace(dep.GroupID)
		artifactID := strings.TrimSpace(dep.ArtifactID)
		if groupID == "" || artifactID == "" {
			continue
		}
		scope := strings.TrimSpace(dep.Scope)
		if scope == "" {
			scope = "compile"
		}
		if !opts.IncludeDevDependencies && (scope == "test" || scope == "provided") {
			continue
		}
		version := strings.TrimSpace(dep.Version)
		meta := map[string]any{"group_id": groupID, "artifact_id": artifactID}
		if strings.Contains(version, "${") {
			meta["version_expression"] = version
			version = ""
		}
		out = append(out, newAppDependency(filepath.Dir(path), "maven", groupID+":"+artifactID, version, "maven", path, scope, "", meta))
	}
	return out, nil
}

var gradleDepRE = regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9_]*)\s*(?:\(?\s*)["']([^:"']+):([^:"']+):([^"']+)["']`)

func parseGradleBuild(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	data, err := readLimitedFile(path, opts.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	matches := gradleDepRE.FindAllStringSubmatch(string(data), -1)
	out := make([]AppDependencyInfo, 0, len(matches))
	for _, match := range matches {
		if len(match) != 5 {
			continue
		}
		scope := strings.TrimSpace(match[1])
		if !opts.IncludeDevDependencies && strings.Contains(strings.ToLower(scope), "test") {
			continue
		}
		groupID := strings.TrimSpace(match[2])
		artifactID := strings.TrimSpace(match[3])
		version := strings.TrimSpace(match[4])
		meta := map[string]any{"group_id": groupID, "artifact_id": artifactID}
		out = append(out, newAppDependency(filepath.Dir(path), "maven", groupID+":"+artifactID, version, "gradle", path, scope, "", meta))
	}
	return out, nil
}

func parseSBOMJSON(path string, opts appDependencyScanOptions) ([]AppDependencyInfo, error) {
	data, err := readLimitedFile(path, opts.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["bomFormat"]; ok {
		return parseCycloneDXJSON(data, path)
	}
	if _, ok := probe["spdxVersion"]; ok {
		return parseSPDXJSON(data, path)
	}
	return nil, nil
}

type cyclonedxBOM struct {
	BOMFormat  string `json:"bomFormat"`
	Components []struct {
		Group    string `json:"group"`
		Name     string `json:"name"`
		Version  string `json:"version"`
		PURL     string `json:"purl"`
		Scope    string `json:"scope"`
		Licenses []struct {
			Expression string `json:"expression"`
			License    struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"license"`
		} `json:"licenses"`
	} `json:"components"`
}

func parseCycloneDXJSON(data []byte, path string) ([]AppDependencyInfo, error) {
	var bom cyclonedxBOM
	if err := json.Unmarshal(data, &bom); err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	for _, comp := range bom.Components {
		name := strings.TrimSpace(comp.Name)
		if name == "" {
			continue
		}
		ecosystem := ecosystemFromPURL(comp.PURL)
		if ecosystem == "" {
			ecosystem = "other"
		}
		if ecosystem == "maven" && strings.TrimSpace(comp.Group) != "" {
			name = strings.TrimSpace(comp.Group) + ":" + name
		}
		license := ""
		if len(comp.Licenses) > 0 {
			license = firstNonEmptyAppDep(comp.Licenses[0].Expression, comp.Licenses[0].License.ID, comp.Licenses[0].License.Name)
		}
		scope := firstNonEmptyAppDep(comp.Scope, "production")
		dep := newAppDependency(filepath.Dir(path), ecosystem, name, comp.Version, "cyclonedx", path, scope, license, nil)
		if strings.TrimSpace(comp.PURL) != "" {
			dep.PURL = strings.TrimSpace(comp.PURL)
		}
		out = append(out, dep)
	}
	return out, nil
}

type spdxDocument struct {
	SPDXVersion string `json:"spdxVersion"`
	Packages    []struct {
		Name             string `json:"name"`
		VersionInfo      string `json:"versionInfo"`
		LicenseConcluded string `json:"licenseConcluded"`
		ExternalRefs     []struct {
			ReferenceCategory string `json:"referenceCategory"`
			ReferenceType     string `json:"referenceType"`
			ReferenceLocator  string `json:"referenceLocator"`
		} `json:"externalRefs"`
	} `json:"packages"`
}

func parseSPDXJSON(data []byte, path string) ([]AppDependencyInfo, error) {
	var doc spdxDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []AppDependencyInfo
	for _, pkg := range doc.Packages {
		name := strings.TrimSpace(pkg.Name)
		if name == "" {
			continue
		}
		purl := spdxPackagePURL(pkg.ExternalRefs)
		ecosystem := ecosystemFromPURL(purl)
		if ecosystem == "" {
			ecosystem = "other"
		}
		license := strings.TrimSpace(pkg.LicenseConcluded)
		if license == "NOASSERTION" {
			license = ""
		}
		dep := newAppDependency(filepath.Dir(path), ecosystem, name, pkg.VersionInfo, "spdx", path, "production", license, nil)
		if purl != "" {
			dep.PURL = purl
		}
		out = append(out, dep)
	}
	return out, nil
}

func spdxPackagePURL(refs []struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}) string {
	for _, ref := range refs {
		if strings.EqualFold(ref.ReferenceCategory, "PACKAGE-MANAGER") && strings.EqualFold(ref.ReferenceType, "purl") {
			return strings.TrimSpace(ref.ReferenceLocator)
		}
	}
	return ""
}

func newAppDependency(appRoot, ecosystem, name, version, manager, manifestPath, scope, license string, metadata map[string]any) AppDependencyInfo {
	ecosystem = normalizeAppDependencyEcosystem(ecosystem)
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	dep := AppDependencyInfo{
		AppRoot:        filepath.Clean(appRoot),
		Ecosystem:      ecosystem,
		Name:           name,
		Version:        version,
		PackageManager: strings.TrimSpace(manager),
		ManifestPath:   filepath.Clean(manifestPath),
		Scope:          strings.TrimSpace(scope),
		License:        strings.TrimSpace(license),
		Metadata:       metadata,
	}
	if dep.Scope == "" {
		dep.Scope = "production"
	}
	dep.PURL = packageURL(ecosystem, name, version)
	return dep
}

func normalizeAppDependencyEcosystem(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "node", "nodejs", "javascript", "npmjs":
		return "npm"
	case "python", "pip":
		return "pypi"
	case "golang", "gomod":
		return "go"
	case "dotnet":
		return "nuget"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func packageURL(ecosystem, name, version string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(version) == "" {
		return ""
	}
	escapedVersion := url.PathEscape(strings.TrimSpace(version))
	switch normalizeAppDependencyEcosystem(ecosystem) {
	case "npm":
		return "pkg:npm/" + escapePURLName(name) + "@" + escapedVersion
	case "pypi":
		return "pkg:pypi/" + escapePURLName(canonicalPyPIName(name)) + "@" + escapedVersion
	case "go":
		return "pkg:golang/" + escapePURLName(name) + "@" + escapedVersion
	case "nuget":
		return "pkg:nuget/" + escapePURLName(name) + "@" + escapedVersion
	case "maven":
		groupID, artifactID, ok := strings.Cut(name, ":")
		if ok {
			return "pkg:maven/" + escapePURLName(groupID) + "/" + escapePURLName(artifactID) + "@" + escapedVersion
		}
		return "pkg:maven/" + escapePURLName(name) + "@" + escapedVersion
	case "cargo", "gem", "composer":
		return "pkg:" + normalizeAppDependencyEcosystem(ecosystem) + "/" + escapePURLName(name) + "@" + escapedVersion
	default:
		return ""
	}
}

func escapePURLName(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "/")
	for i := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(parts[i]), "@", "%40")
	}
	return strings.Join(parts, "/")
}

func ecosystemFromPURL(purl string) string {
	purl = strings.TrimSpace(purl)
	if !strings.HasPrefix(purl, "pkg:") {
		return ""
	}
	rest := strings.TrimPrefix(purl, "pkg:")
	if idx := strings.IndexAny(rest, "/?@"); idx >= 0 {
		return normalizeAppDependencyEcosystem(rest[:idx])
	}
	return normalizeAppDependencyEcosystem(rest)
}

func npmScope(dev, optional bool) string {
	switch {
	case dev:
		return "development"
	case optional:
		return "optional"
	default:
		return "production"
	}
}

func canonicalPyPIName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"))
}

func looksPinnedSemver(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		return false
	}
	return value[0] >= '0' && value[0] <= '9'
}

func stripInlineComment(line string) string {
	if idx := strings.Index(line, " #"); idx >= 0 {
		return strings.TrimSpace(line[:idx])
	}
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	return line
}

func stripGoComment(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		return strings.TrimSpace(line[:idx])
	}
	return strings.TrimSpace(line)
}

func decodeJSONFile(path string, maxBytes int64, dst any) error {
	data, err := readLimitedFile(path, maxBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func decodeXMLFile(path string, maxBytes int64, dst any) error {
	data, err := readLimitedFile(path, maxBytes)
	if err != nil {
		return err
	}
	return xml.Unmarshal(data, dst)
}

func readLimitedFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxBytes {
		return nil, fmt.Errorf("manifest %s is %d bytes, exceeds %d", path, st.Size(), maxBytes)
	}
	return os.ReadFile(path)
}

func dedupeAppDependencies(in []AppDependencyInfo) []AppDependencyInfo {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]AppDependencyInfo, len(in))
	for _, dep := range in {
		if strings.TrimSpace(dep.Name) == "" || strings.TrimSpace(dep.Ecosystem) == "" {
			continue
		}
		key := strings.ToLower(strings.Join([]string{
			dep.AppRoot,
			dep.Ecosystem,
			dep.Name,
			dep.Version,
			dep.ManifestPath,
		}, "\x00"))
		if existing, ok := seen[key]; ok {
			if existing.Scope == "development" && dep.Scope != "development" {
				seen[key] = dep
			}
			continue
		}
		seen[key] = dep
	}
	out := make([]AppDependencyInfo, 0, len(seen))
	for _, dep := range seen {
		out = append(out, dep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AppRoot != out[j].AppRoot {
			return out[i].AppRoot < out[j].AppRoot
		}
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func firstNonEmptyAppDep(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
