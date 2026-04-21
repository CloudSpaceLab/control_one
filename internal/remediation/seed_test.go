package remediation

import (
	"context"
	"strings"
	"testing"
)

// TestSeeder_NilDB ensures we fail fast when a caller wires up the seeder
// with a nil handle instead of panicking deep inside ExecContext.
func TestSeeder_NilDB(t *testing.T) {
	seeder := NewSeeder(nil)
	_, err := seeder.Seed(context.Background())
	if err == nil {
		t.Fatal("expected error when seeding with nil db")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadSeedCatalog_ReturnsExpectedShape covers the pure load path: the
// catalog parses, every referenced file is non-empty, and the expected
// 15 Linux + 10 Windows split is preserved.
func TestLoadSeedCatalog_ReturnsExpectedShape(t *testing.T) {
	scripts, err := LoadSeedCatalog()
	if err != nil {
		t.Fatalf("LoadSeedCatalog: %v", err)
	}

	if want, got := 25, len(scripts); want != got {
		t.Fatalf("expected %d seed scripts, got %d", want, got)
	}

	var linuxCount, windowsCount int
	for _, s := range scripts {
		switch s.Platform {
		case "linux":
			linuxCount++
		case "windows":
			windowsCount++
		}
	}
	if linuxCount != 15 {
		t.Fatalf("expected 15 linux scripts, got %d", linuxCount)
	}
	if windowsCount != 10 {
		t.Fatalf("expected 10 windows scripts, got %d", windowsCount)
	}
}

// TestLoadSeedCatalog_MetadataIsValid makes sure we never ship a catalog
// entry with an empty script body, empty rollback body, or an invalid
// platform / script_type enum. This is the test the task contract calls out
// directly: "each script's metadata is valid".
func TestLoadSeedCatalog_MetadataIsValid(t *testing.T) {
	scripts, err := LoadSeedCatalog()
	if err != nil {
		t.Fatalf("LoadSeedCatalog: %v", err)
	}

	for _, s := range scripts {
		if strings.TrimSpace(s.ScriptContent) == "" {
			t.Errorf("%s/%s: empty script_content", s.RuleID, s.Platform)
		}
		if strings.TrimSpace(s.RollbackContent) == "" {
			t.Errorf("%s/%s: empty rollback_content (required for Gap 2.2)", s.RuleID, s.Platform)
		}
		if _, ok := validPlatforms[s.Platform]; !ok {
			t.Errorf("%s: invalid platform %q", s.RuleID, s.Platform)
		}
		if _, ok := validScriptTypes[s.ScriptType]; !ok {
			t.Errorf("%s: invalid script_type %q", s.RuleID, s.ScriptType)
		}

		// Metadata should carry description + frameworks so operators can see
		// what each row does without opening the script.
		if _, ok := s.Metadata["description"]; !ok {
			t.Errorf("%s/%s: metadata missing 'description'", s.RuleID, s.Platform)
		}
		if _, ok := s.Metadata["frameworks"]; !ok {
			t.Errorf("%s/%s: metadata missing 'frameworks'", s.RuleID, s.Platform)
		}
	}
}

// TestLoadSeedCatalog_ScriptTypeMatchesPlatform is a smoke check: linux
// scripts must be shell-family, windows scripts must be powershell-family.
// Catching a mis-wire here prevents the engine from dispatching a bash
// string to powershell.exe.
func TestLoadSeedCatalog_ScriptTypeMatchesPlatform(t *testing.T) {
	scripts, err := LoadSeedCatalog()
	if err != nil {
		t.Fatalf("LoadSeedCatalog: %v", err)
	}

	linuxTypes := map[string]struct{}{"shell": {}, "bash": {}, "sh": {}, "ansible": {}}
	windowsTypes := map[string]struct{}{"powershell": {}, "ps1": {}}

	for _, s := range scripts {
		switch s.Platform {
		case "linux":
			if _, ok := linuxTypes[s.ScriptType]; !ok {
				t.Errorf("%s: linux script with non-linux script_type %q", s.RuleID, s.ScriptType)
			}
		case "windows":
			if _, ok := windowsTypes[s.ScriptType]; !ok {
				t.Errorf("%s: windows script with non-windows script_type %q", s.RuleID, s.ScriptType)
			}
		}
	}
}

// TestLoadSeedCatalog_ContentMatchesFile verifies the loader actually reads
// each script file (vs. returning the catalog defaults) by asserting the
// expected shebang/preamble is present.
func TestLoadSeedCatalog_ContentMatchesFile(t *testing.T) {
	scripts, err := LoadSeedCatalog()
	if err != nil {
		t.Fatalf("LoadSeedCatalog: %v", err)
	}

	for _, s := range scripts {
		switch s.Platform {
		case "linux":
			if !strings.HasPrefix(s.ScriptContent, "#!/usr/bin/env bash") {
				t.Errorf("%s: linux script missing bash shebang", s.RuleID)
			}
			if !strings.HasPrefix(s.RollbackContent, "#!/usr/bin/env bash") {
				t.Errorf("%s: linux rollback missing bash shebang", s.RuleID)
			}
		case "windows":
			// PowerShell scripts don't use a shebang but should set the error
			// action preference near the top so failures are deterministic.
			if !strings.Contains(s.ScriptContent, "$ErrorActionPreference") {
				t.Errorf("%s: powershell script missing $ErrorActionPreference", s.RuleID)
			}
			if !strings.Contains(s.RollbackContent, "$ErrorActionPreference") {
				t.Errorf("%s: powershell rollback missing $ErrorActionPreference", s.RuleID)
			}
		}
	}
}

// TestLoadSeedCatalog_RuleIDsUnique guards against double-registering the
// same rule_id for the same platform (which would break the UNIQUE(rule_id,
// platform, version) constraint on the DB side and leave orphan rows).
func TestLoadSeedCatalog_RuleIDsUnique(t *testing.T) {
	scripts, err := LoadSeedCatalog()
	if err != nil {
		t.Fatalf("LoadSeedCatalog: %v", err)
	}

	seen := make(map[string]struct{})
	for _, s := range scripts {
		key := s.RuleID + "|" + s.Platform
		if _, ok := seen[key]; ok {
			t.Errorf("duplicate rule_id+platform: %s", key)
		}
		seen[key] = struct{}{}
	}
}
