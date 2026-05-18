package logs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/appcatalog"
	"github.com/CloudSpaceLab/control_one/internal/config"
)

func TestExplicitCatalogPresetCarriesParserMetadata(t *testing.T) {
	sources := PrepareSources([]config.LogSourceConfig{{Program: "temenos-t24"}})
	var got config.LogSourceConfig
	for _, source := range sources {
		if source.Program == "temenos-t24" {
			got = source
			break
		}
	}
	if got.Program == "" {
		t.Fatalf("temenos-t24 source missing from prepared sources: %#v", sources)
	}
	if len(got.Paths) == 0 {
		t.Fatalf("expected Temenos path candidates: %#v", got)
	}
	if got.Labels["parser_profile"] != "temenos-t24" {
		t.Fatalf("parser profile label missing: %#v", got.Labels)
	}
	if got.Labels["catalog_version"] != appcatalog.CatalogVersion() {
		t.Fatalf("catalog version label = %q, want %q", got.Labels["catalog_version"], appcatalog.CatalogVersion())
	}
}

func TestExplicitOfflineCatalogPresetResolvesWithoutPackageInit(t *testing.T) {
	root := t.TempDir()
	activeCatalogDir := filepath.Join(root, "active", "app_catalog")
	if err := os.MkdirAll(activeCatalogDir, 0o755); err != nil {
		t.Fatalf("mkdir catalog: %v", err)
	}
	catalog := appcatalog.Catalog{
		Version: "2026.05.18",
		Profiles: []appcatalog.Profile{{
			ID:              "bank-switch",
			Name:            "Bank Switch",
			Category:        "integration",
			ParserProfileID: "bank-switch",
			LogProfiles: []appcatalog.LogProfile{{
				Program:   "bank-switch",
				Formatter: "generic",
				Paths:     []string{"/opt/bank-switch/logs/*.log"},
			}},
		}},
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeCatalogDir, "bank-switch.json"), data, 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeCatalogDir, "bank-switch.json.receipt.json"), []byte(`{"bundle_id":"c1-airgap","bundle_version":"2026.05.18"}`), 0o644); err != nil {
		t.Fatalf("write receipt: %v", err)
	}
	t.Setenv("CONTROL_ONE_APP_CATALOG_ROOT", root)

	sources := PrepareSources([]config.LogSourceConfig{{Program: "bank-switch"}})
	var got config.LogSourceConfig
	for _, source := range sources {
		if source.Program == "bank-switch" {
			got = source
			break
		}
	}
	if got.Program == "" {
		t.Fatalf("bank-switch source missing from prepared sources: %#v", sources)
	}
	if got.Formatter != "generic" || len(got.Paths) == 0 || filepath.ToSlash(got.Paths[0]) != "/opt/bank-switch/logs/*.log" {
		t.Fatalf("overlay source not resolved: %#v", got)
	}
	if got.Labels["catalog_version"] != "c1-airgap@2026.05.18" {
		t.Fatalf("catalog version label = %q", got.Labels["catalog_version"])
	}
}
