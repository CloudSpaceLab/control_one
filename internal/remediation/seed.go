package remediation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// seedScriptsFS embeds the curated starter-pack of CIS/SOC2 remediation
// scripts shipped with the binary. Files are read via Load() at startup so
// the database stays canonical but the scripts remain reviewable on disk.
//
//go:embed seeds/linux/*.sh seeds/windows/*.ps1 seeds/catalog.json
var seedScriptsFS embed.FS

// SeedScript is a single entry in the starter-pack catalog. It couples the
// script body + optional rollback body with the metadata that lets the
// execution engine route it to the right rule and platform.
type SeedScript struct {
	RuleID          string         `json:"rule_id"`
	Platform        string         `json:"platform"`    // "linux" | "windows" | "all"
	ScriptType      string         `json:"script_type"` // "shell" | "powershell"
	ContentFile     string         `json:"content_file"`
	RollbackFile    string         `json:"rollback_file"`
	ScriptContent   string         `json:"-"`
	RollbackContent string         `json:"-"`
	Metadata        map[string]any `json:"metadata"`
}

// SeedCatalog is the JSON manifest shape. It exists so operators can review
// the whole pack in one file without chasing `embed.FS` ordering.
type SeedCatalog struct {
	Version int          `json:"version"`
	Scripts []SeedScript `json:"scripts"`
}

// validPlatforms is the enum the remediation_scripts schema enforces.
var validPlatforms = map[string]struct{}{
	"linux":   {},
	"windows": {},
	"all":     {},
}

// validScriptTypes lists the runtimes the engine can dispatch to.
var validScriptTypes = map[string]struct{}{
	"shell":      {},
	"bash":       {},
	"sh":         {},
	"powershell": {},
	"ps1":        {},
	"ansible":    {},
}

// LoadSeedCatalog reads the embedded catalog.json plus each referenced script
// file and returns fully-hydrated SeedScript records. It validates the
// catalog eagerly so an invalid entry surfaces at startup, not at scan-time.
func LoadSeedCatalog() ([]SeedScript, error) {
	raw, err := seedScriptsFS.ReadFile("seeds/catalog.json")
	if err != nil {
		return nil, fmt.Errorf("read seed catalog: %w", err)
	}

	var catalog SeedCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("parse seed catalog: %w", err)
	}

	seen := make(map[string]struct{}, len(catalog.Scripts))
	out := make([]SeedScript, 0, len(catalog.Scripts))
	for i, entry := range catalog.Scripts {
		if err := validateEntry(i, &entry); err != nil {
			return nil, err
		}

		key := entry.RuleID + "|" + entry.Platform
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("seed catalog entry %d: duplicate rule_id/platform %q", i, key)
		}
		seen[key] = struct{}{}

		body, err := readScriptFile(entry.ContentFile)
		if err != nil {
			return nil, fmt.Errorf("seed catalog entry %d (%s): %w", i, entry.RuleID, err)
		}
		entry.ScriptContent = body

		rollback, err := readScriptFile(entry.RollbackFile)
		if err != nil {
			return nil, fmt.Errorf("seed catalog entry %d (%s): rollback: %w", i, entry.RuleID, err)
		}
		entry.RollbackContent = rollback

		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}
		// Stash the source path so operators can trace a DB row back to disk.
		entry.Metadata["source_file"] = entry.ContentFile
		entry.Metadata["rollback_source_file"] = entry.RollbackFile

		out = append(out, entry)
	}

	// Stable order — callers may iterate for tests, audit logs, etc.
	sort.Slice(out, func(i, j int) bool {
		if out[i].RuleID == out[j].RuleID {
			return out[i].Platform < out[j].Platform
		}
		return out[i].RuleID < out[j].RuleID
	})

	return out, nil
}

func validateEntry(idx int, entry *SeedScript) error {
	entry.RuleID = strings.TrimSpace(entry.RuleID)
	entry.Platform = strings.ToLower(strings.TrimSpace(entry.Platform))
	entry.ScriptType = strings.ToLower(strings.TrimSpace(entry.ScriptType))
	entry.ContentFile = strings.TrimSpace(entry.ContentFile)
	entry.RollbackFile = strings.TrimSpace(entry.RollbackFile)

	if entry.RuleID == "" {
		return fmt.Errorf("seed catalog entry %d: rule_id is required", idx)
	}
	if _, ok := validPlatforms[entry.Platform]; !ok {
		return fmt.Errorf("seed catalog entry %d (%s): invalid platform %q", idx, entry.RuleID, entry.Platform)
	}
	if _, ok := validScriptTypes[entry.ScriptType]; !ok {
		return fmt.Errorf("seed catalog entry %d (%s): invalid script_type %q", idx, entry.RuleID, entry.ScriptType)
	}
	if entry.ContentFile == "" {
		return fmt.Errorf("seed catalog entry %d (%s): content_file is required", idx, entry.RuleID)
	}
	if entry.RollbackFile == "" {
		return fmt.Errorf("seed catalog entry %d (%s): rollback_file is required (Gap 2.2 contract)", idx, entry.RuleID)
	}
	return nil
}

func readScriptFile(rel string) (string, error) {
	// Restrict reads to the embedded seeds/ tree; defence-in-depth against a
	// malformed catalog pointing at "/etc/passwd".
	clean := path.Clean(rel)
	if strings.HasPrefix(clean, "..") || path.IsAbs(clean) {
		return "", fmt.Errorf("illegal seed file path %q", rel)
	}

	data, err := seedScriptsFS.ReadFile(path.Join("seeds", clean))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("embedded file %s not found", clean)
		}
		return "", fmt.Errorf("read %s: %w", clean, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf("embedded file %s is empty", clean)
	}
	return string(data), nil
}

// Seeder pushes the embedded starter-pack into the remediation_scripts table.
// It is idempotent: a UNIQUE(rule_id, platform, version) constraint on the
// table combined with ON CONFLICT DO NOTHING means repeat applies are no-ops,
// which lets Seed() safely run at every boot of the control plane.
type Seeder struct {
	db *sql.DB
}

// NewSeeder wires up a Seeder against an *sql.DB handle. The caller keeps
// ownership of the connection; we never Close() it.
func NewSeeder(db *sql.DB) *Seeder {
	return &Seeder{db: db}
}

// SeedStats captures what a Seed() invocation did. Useful for startup logs
// and integration tests that assert idempotency.
type SeedStats struct {
	Inserted int
	Skipped  int
	Total    int
}

// Seed inserts every catalog entry into remediation_scripts, skipping rows
// that already exist (matched by rule_id + platform + version=1). This makes
// the operation safe to call on every process start.
func (s *Seeder) Seed(ctx context.Context) (SeedStats, error) {
	if s == nil || s.db == nil {
		return SeedStats{}, errors.New("seeder not initialized")
	}

	scripts, err := LoadSeedCatalog()
	if err != nil {
		return SeedStats{}, err
	}

	stats := SeedStats{Total: len(scripts)}

	// Single statement, repeated in a loop, lets us count affected rows so we
	// can distinguish inserts from conflicts at the application layer.
	const insertSQL = `
		INSERT INTO remediation_scripts (
			rule_id, platform, script_type, script_content, checksum,
			rollback_content, rollback_checksum, version, enabled, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 1, TRUE, $8)
		ON CONFLICT (rule_id, platform, version) DO NOTHING
	`

	for _, script := range scripts {
		metaJSON, merr := json.Marshal(script.Metadata)
		if merr != nil {
			return stats, fmt.Errorf("marshal metadata for %s: %w", script.RuleID, merr)
		}
		scriptChecksum := computeSeedChecksum(script.ScriptContent)
		rollbackChecksum := computeSeedChecksum(script.RollbackContent)

		res, err := s.db.ExecContext(ctx, insertSQL,
			script.RuleID,
			script.Platform,
			script.ScriptType,
			script.ScriptContent,
			scriptChecksum,
			script.RollbackContent,
			rollbackChecksum,
			metaJSON,
		)
		if err != nil {
			return stats, fmt.Errorf("insert seed script %s (%s): %w", script.RuleID, script.Platform, err)
		}

		rows, _ := res.RowsAffected()
		if rows > 0 {
			stats.Inserted++
		} else {
			stats.Skipped++
		}
	}

	return stats, nil
}

func computeSeedChecksum(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
