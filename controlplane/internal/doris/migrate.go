package doris

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Migrations are vendored as numbered .sql files under the embedded fs below.
// We track applied versions in a tiny `doris_migrations` table living in the
// same Doris database; each row records the script's checksum so a changed
// file fails loud instead of silently re-running with skewed history.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationDir = "migrations"

type MigrationOptions struct {
	ReplicationNum int
	// BucketCount rewrites Doris table BUCKETS clauses for small single-node
	// deployments. Leave zero for production defaults when ReplicationNum > 1.
	BucketCount int
}

var addColumnIfNotExistsRE = regexp.MustCompile(`(?is)^ALTER\s+TABLE\s+([A-Za-z_][A-Za-z0-9_]*)\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+([A-Za-z_][A-Za-z0-9_]*)\s+(.+)$`)
var bucketsRE = regexp.MustCompile(`(?i)\bBUCKETS\s+[0-9]+`)
var dynamicPartitionBucketsRE = regexp.MustCompile(`(?i)"dynamic_partition\.buckets"\s*=\s*"[0-9]+"`)

// migrationFile is one parsed entry from the embed FS.
type migrationFile struct {
	Version int
	Name    string
	SQL     string
	Hash    string
}

// loadMigrations reads + sorts the embedded migration set.
func loadMigrations() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	out := make([]migrationFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		// Filename format: NNNN_name.up.sql — extract the leading number.
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("malformed migration filename: %q (expected NNNN_name.up.sql)", name)
		}
		version, err := parsePositiveInt(parts[0])
		if err != nil {
			return nil, fmt.Errorf("parse version from %q: %w", name, err)
		}
		body, err := fs.ReadFile(migrationsFS, migrationDir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		out = append(out, migrationFile{
			Version: version,
			Name:    name,
			SQL:     string(body),
			Hash:    hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// ApplyMigrations creates the bookkeeping table and runs every pending
// migration against the Doris cluster. Idempotent: already-applied versions
// are skipped, and a checksum mismatch on an applied version aborts the run.
func ApplyMigrations(ctx context.Context, c *Client) error {
	return ApplyMigrationsWithOptions(ctx, c, MigrationOptions{})
}

func ApplyMigrationsWithOptions(ctx context.Context, c *Client, opts MigrationOptions) error {
	if c == nil || c.db == nil {
		return errors.New("doris client not initialised")
	}
	opts = normalizeMigrationOptions(opts)
	if err := ensureMigrationTable(ctx, c, opts); err != nil {
		return err
	}
	files, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := loadApplied(ctx, c)
	if err != nil {
		return err
	}
	for _, mig := range files {
		prior, ok := applied[mig.Version]
		if ok {
			if prior.Hash != mig.Hash {
				return fmt.Errorf("doris migration %d hash drift: applied=%s, file=%s",
					mig.Version, prior.Hash, mig.Hash)
			}
			continue
		}
		if err := runMigration(ctx, c, mig, opts); err != nil {
			return fmt.Errorf("apply doris migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}
	return nil
}

func normalizeMigrationOptions(opts MigrationOptions) MigrationOptions {
	if opts.ReplicationNum <= 0 {
		opts.ReplicationNum = 1
	}
	if opts.BucketCount <= 0 && opts.ReplicationNum == 1 {
		opts.BucketCount = 1
	}
	return opts
}

func ensureMigrationTable(ctx context.Context, c *Client, opts MigrationOptions) error {
	ddl := renderMigrationSQL(`
CREATE TABLE IF NOT EXISTS doris_migrations (
  version BIGINT,
  name VARCHAR(255),
  hash VARCHAR(64),
  applied_at DATETIME(3)
)
DUPLICATE KEY (version)
DISTRIBUTED BY HASH (version) BUCKETS 1
PROPERTIES ("replication_num" = "1")
`, opts)
	_, err := c.db.ExecContext(ctx, ddl)
	return err
}

type appliedRecord struct {
	Hash      string
	AppliedAt time.Time
}

func loadApplied(ctx context.Context, c *Client) (map[int]appliedRecord, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT version, hash, applied_at FROM doris_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query applied: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int]appliedRecord)
	for rows.Next() {
		var version int64
		var hash string
		var appliedAt time.Time
		if err := rows.Scan(&version, &hash, &appliedAt); err != nil {
			return nil, err
		}
		out[int(version)] = appliedRecord{Hash: hash, AppliedAt: appliedAt}
	}
	return out, rows.Err()
}

func runMigration(ctx context.Context, c *Client, mig migrationFile, opts MigrationOptions) error {
	for _, stmt := range splitStatements(renderMigrationSQL(mig.SQL, opts)) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		rewritten, skip, err := prepareMigrationStatement(ctx, c, stmt)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		stmt = rewritten
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec stmt: %w\n--- statement ---\n%s\n--- end ---", err, stmt)
		}
	}
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO doris_migrations (version, name, hash, applied_at)
		VALUES (?, ?, ?, NOW())
	`, mig.Version, mig.Name, mig.Hash)
	return err
}

func renderMigrationSQL(sql string, opts MigrationOptions) string {
	opts = normalizeMigrationOptions(opts)
	repl := fmt.Sprintf(`"replication_num" = "%d"`, opts.ReplicationNum)
	sql = strings.ReplaceAll(sql, `"replication_num" = "1"`, repl)
	if opts.BucketCount > 0 {
		sql = bucketsRE.ReplaceAllString(sql, fmt.Sprintf("BUCKETS %d", opts.BucketCount))
		sql = dynamicPartitionBucketsRE.ReplaceAllString(sql, fmt.Sprintf(`"dynamic_partition.buckets" = "%d"`, opts.BucketCount))
	}
	return sql
}

func prepareMigrationStatement(ctx context.Context, c *Client, stmt string) (string, bool, error) {
	table, column, rewritten, ok := rewriteAddColumnIfNotExists(stmt)
	if !ok {
		return stmt, false, nil
	}
	exists, err := columnExists(ctx, c, table, column)
	if err != nil {
		return "", false, fmt.Errorf("check Doris column %s.%s: %w", table, column, err)
	}
	return rewritten, exists, nil
}

func rewriteAddColumnIfNotExists(stmt string) (table string, column string, rewritten string, ok bool) {
	match := addColumnIfNotExistsRE.FindStringSubmatch(strings.TrimSpace(stmt))
	if len(match) != 4 {
		return "", "", "", false
	}
	table = match[1]
	column = match[2]
	definition := strings.TrimSpace(match[3])
	return table, column, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition), true
}

func columnExists(ctx context.Context, c *Client, table string, column string) (bool, error) {
	var count int
	err := c.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = ?
		  AND table_name = ?
		  AND column_name = ?
	`, c.cfg.Database, table, column).Scan(&count)
	return count > 0, err
}

// splitStatements splits a multi-statement Doris SQL script on bare `;`
// terminators that aren't inside quoted strings or block comments. Doris's
// JDBC driver does not support multi-statement execution in one Exec, so we
// have to chop them ourselves.
func splitStatements(sql string) []string {
	var out []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	inLineComment := false
	inBlockComment := false
	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		if inLineComment {
			cur.WriteRune(r)
			if r == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			cur.WriteRune(r)
			if r == '*' && next == '/' {
				cur.WriteRune(next)
				i++
				inBlockComment = false
			}
			continue
		}
		if !inSingle && !inDouble {
			if r == '-' && next == '-' {
				inLineComment = true
				cur.WriteRune(r)
				continue
			}
			if r == '/' && next == '*' {
				inBlockComment = true
				cur.WriteRune(r)
				continue
			}
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			cur.WriteRune(r)
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			cur.WriteRune(r)
			continue
		}
		if r == ';' && !inSingle && !inDouble {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-numeric character in version: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("version must be > 0: %q", s)
	}
	return n, nil
}
