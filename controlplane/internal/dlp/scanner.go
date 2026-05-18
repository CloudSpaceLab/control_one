// Package dlp provides column-level data loss prevention scanning for database columns.
package dlp

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Scanner performs column-level PII detection using regex patterns.
type Scanner struct {
	rules      []storage.DataClassificationRule
	logger     *zap.Logger
	db         *sql.DB
	schema     string
	sampleSize int
}

// ScannerConfig configures the DLP scanner.
type ScannerConfig struct {
	DB         *sql.DB
	Schema     string
	SampleSize int // Number of rows to sample per column (default: 100)
	Logger     *zap.Logger
}

// NewScanner creates a new DLP column scanner.
func NewScanner(ctx context.Context, store storage.Store, tenantID string, cfg ScannerConfig) (*Scanner, error) {
	if cfg.SampleSize <= 0 {
		cfg.SampleSize = 100
	}
	if cfg.Schema == "" {
		cfg.Schema = "public"
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	// Parse tenantID as UUID
	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant_id: %w", err)
	}

	rules, err := store.ListDataClassificationRules(ctx, tenantUUID)
	if err != nil {
		return nil, fmt.Errorf("load dlp rules: %w", err)
	}

	// Filter to enabled rules only
	enabledRules := make([]storage.DataClassificationRule, 0, len(rules))
	for _, r := range rules {
		if r.Enabled {
			enabledRules = append(enabledRules, r)
		}
	}

	return &Scanner{
		rules:      enabledRules,
		logger:     cfg.Logger,
		db:         cfg.DB,
		schema:     cfg.Schema,
		sampleSize: cfg.SampleSize,
	}, nil
}

// Column represents a database column to scan.
type Column struct {
	DatabaseName string
	SchemaName   string
	TableName    string
	ColumnName   string
	DataType     string
}

// ScanResult contains the classification result for a column.
type ScanResult struct {
	Column         Column
	PIIType        *string
	Encrypted      *bool
	EncryptionKind *string
	MinValueLength *int
	MaxValueLength *int
	SampleCount    int
	MatchedRules   []string
	Severity       string
}

// ScanColumn scans a single column for PII patterns.
func (s *Scanner) ScanColumn(ctx context.Context, col Column) (*ScanResult, error) {
	result := &ScanResult{
		Column:      col,
		SampleCount: 0,
		Severity:    "none",
	}

	// Query sample data from the column
	query := fmt.Sprintf(`SELECT %s FROM %s.%s LIMIT %d`,
		quoteIdentifier(col.ColumnName),
		quoteIdentifier(col.SchemaName),
		quoteIdentifier(col.TableName),
		s.sampleSize)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		s.logger.Warn("failed to query column sample",
			zap.String("table", col.TableName),
			zap.String("column", col.ColumnName),
			zap.Error(err))
		return result, nil // Return empty result on query failure
	}
	defer func() { _ = rows.Close() }()

	var samples []string
	var minLen, maxLen int

	for rows.Next() {
		var val sql.NullString
		if err := rows.Scan(&val); err != nil {
			continue
		}
		if val.Valid && val.String != "" {
			samples = append(samples, val.String)
			l := len(val.String)
			if result.SampleCount == 0 || l < minLen {
				minLen = l
			}
			if l > maxLen {
				maxLen = l
			}
			result.SampleCount++
		}
	}

	if result.SampleCount == 0 {
		return result, nil
	}

	result.MinValueLength = &minLen
	result.MaxValueLength = &maxLen

	// Check each sample against all rules
	matchedRules := make(map[string]bool)
	maxSeverity := "none"

	for _, rule := range s.rules {
		if rule.Regex == "" {
			continue
		}

		re, err := regexp.Compile(rule.Regex)
		if err != nil {
			s.logger.Warn("invalid regex in rule",
				zap.String("rule", rule.Name),
				zap.Error(err))
			continue
		}

		matches := 0
		for _, sample := range samples {
			if re.MatchString(sample) {
				matches++
			}
		}

		// If more than 10% of samples match, classify the column
		if matches > result.SampleCount/10 {
			matchedRules[rule.Name] = true
			if compareSeverity(rule.Severity, maxSeverity) > 0 {
				maxSeverity = rule.Severity
			}
			result.PIIType = &rule.PIIType
		}
	}

	if len(matchedRules) > 0 {
		ruleNames := make([]string, 0, len(matchedRules))
		for name := range matchedRules {
			ruleNames = append(ruleNames, name)
		}
		result.MatchedRules = ruleNames
		result.Severity = maxSeverity
	}

	return result, nil
}

// ScanTable scans all columns in a table.
func (s *Scanner) ScanTable(ctx context.Context, dbName, schemaName, tableName string) ([]*ScanResult, error) {
	// Get column list
	columns, err := s.getTableColumns(ctx, dbName, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	results := make([]*ScanResult, len(columns))
	errChan := make(chan error, len(columns))

	for i, col := range columns {
		wg.Add(1)
		go func(idx int, c Column) {
			defer wg.Done()
			result, err := s.ScanColumn(ctx, c)
			if err != nil {
				errChan <- err
				return
			}
			results[idx] = result
		}(i, col)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	if err := <-errChan; err != nil {
		return nil, err
	}

	return results, nil
}

// getTableColumns retrieves column metadata for a table.
func (s *Scanner) getTableColumns(ctx context.Context, dbName, schemaName, tableName string) ([]Column, error) {
	query := `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("query table columns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []Column
	for rows.Next() {
		var colName, dataType string
		if err := rows.Scan(&colName, &dataType); err != nil {
			continue
		}
		columns = append(columns, Column{
			DatabaseName: dbName,
			SchemaName:   schemaName,
			TableName:    tableName,
			ColumnName:   colName,
			DataType:     dataType,
		})
	}

	return columns, nil
}

// quoteIdentifier safely quotes a SQL identifier.
func quoteIdentifier(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

// compareSeverity returns 1 if a > b, -1 if a < b, 0 if equal.
// Severity order: critical > high > medium > low > none
func compareSeverity(a, b string) int {
	severityOrder := map[string]int{
		"none":     0,
		"low":      1,
		"medium":   2,
		"high":     3,
		"critical": 4,
	}
	av := severityOrder[a]
	bv := severityOrder[b]
	if av > bv {
		return 1
	} else if av < bv {
		return -1
	}
	return 0
}
