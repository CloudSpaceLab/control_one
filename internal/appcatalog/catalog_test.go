package appcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePackagePurposesUsesSharedCatalog(t *testing.T) {
	got := ResolvePackagePurposes([]string{
		"postgresql-16",
		"haproxy",
		"redis-server",
		"tomcat9",
		"prometheus-node-exporter",
		"rabbitmq-server",
	})
	seen := map[string]PurposeDetection{}
	for _, row := range got {
		seen[row.Purpose] = row
	}
	for _, want := range []string{"db_node", "load_balancer", "cache_server", "app_node", "monitoring_server", "message_queue"} {
		row, ok := seen[want]
		if !ok {
			t.Fatalf("missing purpose %q from %#v", want, got)
		}
		if row.Confidence <= 0 || len(row.Evidence) == 0 {
			t.Fatalf("purpose %q missing confidence/evidence: %#v", want, row)
		}
	}
}

func TestDetectRootPrefersSpecificFrameworkProfile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"composer.json", "artisan"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}
	got := DetectRoot(dir, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	})
	if got.ProfileID != "laravel" {
		t.Fatalf("ProfileID = %q, want laravel: %#v", got.ProfileID, got)
	}
	if got.ParserProfileID == "" || got.CoverageState != "profile_available" {
		t.Fatalf("expected parser profile coverage, got %#v", got)
	}
	if got.CatalogVersion != CatalogVersion() {
		t.Fatalf("CatalogVersion = %q, want %q", got.CatalogVersion, CatalogVersion())
	}
}

func TestDetectRootUsesDependencyHintsForCommonFrameworks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"@nestjs/core":"^10.0.0","express":"^4.18.0"}}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	got := DetectRootWithFS(dir, statExists, readFile)
	if got.ProfileID != "nestjs" {
		t.Fatalf("ProfileID = %q, want nestjs: %#v", got.ProfileID, got)
	}
	if len(got.StackTags) == 0 {
		t.Fatalf("expected stack tags for nestjs, got %#v", got)
	}
	if got.CatalogVersion == "" {
		t.Fatalf("expected catalog version, got %#v", got)
	}
}

func TestDetectRootUsesBankingPathHintsWithoutMisclassifyingGenericFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "finastra-fusion-api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "appsettings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write appsettings: %v", err)
	}
	got := DetectRootWithFS(dir, statExists, readFile)
	if got.ProfileID != "finastra_fusion" {
		t.Fatalf("ProfileID = %q, want finastra_fusion: %#v", got.ProfileID, got)
	}
}

func TestBankingAndMessagingLogProfilesAreExplicitButNotAutocollected(t *testing.T) {
	if paths := LogPathCandidates("temenos-t24", "linux"); len(paths) == 0 {
		t.Fatalf("expected Temenos log path candidates")
	}
	if paths := LogPathCandidates("oracle-flexcube", "linux"); len(paths) == 0 {
		t.Fatalf("expected Flexcube log path candidates")
	}
	if paths := LogPathCandidates("ibm-mq", "linux"); len(paths) == 0 {
		t.Fatalf("expected IBM MQ log path candidates")
	}
	auto := map[string]bool{}
	for _, program := range DefaultLogProgramOrder() {
		auto[program] = true
	}
	for _, program := range []string{"temenos-t24", "oracle-flexcube", "finacle", "ibm-mq", "weblogic"} {
		if auto[program] {
			t.Fatalf("%s should require explicit enablement, auto order=%#v", program, DefaultLogProgramOrder())
		}
	}
}

func TestDetectRootUsesSignedContentCatalogOverlay(t *testing.T) {
	contentRoot := t.TempDir()
	activeCatalogDir := filepath.Join(contentRoot, "active", "app_catalog")
	if err := os.MkdirAll(activeCatalogDir, 0o755); err != nil {
		t.Fatalf("mkdir catalog: %v", err)
	}
	writeCatalogOverlay(t, filepath.Join(activeCatalogDir, "bank-core.json"), Catalog{
		Version: "2026.05.18",
		Profiles: []Profile{{
			ID:                 "bank_core_custom",
			Name:               "Bank Core Custom",
			Category:           "core_banking",
			StackTags:          []string{"banking", "java"},
			Purposes:           []string{"app_node"},
			PackageAliases:     []string{"bank-core-custom"},
			RootMarkers:        marker("bank-core.properties"),
			ParserProfileID:    "bank-core-custom",
			RemediationSkillID: "bank-core-custom-remediation",
			LogProfiles: []LogProfile{{
				Program:   "bank-core-custom",
				Formatter: "generic",
				Paths:     []string{"/opt/bank-core/logs/*.log"},
			}},
		}},
	})
	if err := os.WriteFile(filepath.Join(activeCatalogDir, "bank-core.json.receipt.json"), []byte(`{"bundle_id":"c1-airgap","bundle_version":"2026.05.18","version":"catalog-1"}`), 0o644); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	t.Setenv(appCatalogRootEnv, contentRoot)

	appRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(appRoot, "bank-core.properties"), []byte("name=core"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	got := DetectRootWithFS(appRoot, statExists, readFile)
	if got.ProfileID != "bank_core_custom" {
		t.Fatalf("ProfileID = %q, want bank_core_custom: %#v", got.ProfileID, got)
	}
	if got.CatalogVersion != "c1-airgap@2026.05.18" {
		t.Fatalf("CatalogVersion = %q, want bundle provenance", got.CatalogVersion)
	}
	if paths := LogPathCandidates("bank-core-custom", "linux"); len(paths) != 1 || paths[0] != "/opt/bank-core/logs/*.log" {
		t.Fatalf("overlay log paths = %#v", paths)
	}
	if profiles := ResolvePackagePurposes([]string{"bank-core-custom"}); len(profiles) == 0 || profiles[0].Purpose != "app_node" {
		t.Fatalf("overlay package purpose missing: %#v", profiles)
	}
}

func TestDefaultLogProfilesKeepAutocollectNarrow(t *testing.T) {
	order := DefaultLogProgramOrder()
	seen := map[string]bool{}
	for _, name := range order {
		seen[name] = true
	}
	for _, want := range []string{"nginx", "apache", "lighttpd", "tomcat", "haproxy", "mysql", "postgresql", "redis", "kafka"} {
		if !seen[want] {
			t.Fatalf("default log order missing %q from %#v", want, order)
		}
	}
	if seen["weblogic"] || seen["iis"] {
		t.Fatalf("high-risk enterprise profiles should not autocollect without an explicit source: %#v", order)
	}
}

func statExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFile(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	return data, err == nil
}

func writeCatalogOverlay(t *testing.T, path string, catalog Catalog) {
	t.Helper()
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
}
