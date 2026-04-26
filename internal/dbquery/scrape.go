package dbquery

import (
	"context"
	"database/sql"
	"strings"
)

// scrape dispatches to the engine-specific query.
func scrape(ctx context.Context, db *sql.DB, t Target) ([]queryState, error) {
	switch t.Engine {
	case EnginePostgres:
		return scrapePostgres(ctx, db)
	case EngineMySQL:
		return scrapeMySQL(ctx, db)
	case EngineMSSQL:
		return scrapeMSSQL(ctx, db)
	}
	return nil, nil
}

// scrapePostgres reads pg_stat_statements + pg_stat_activity. The extension
// must be created on the target DB; the deploy step does this at install.
//
// Cumulative columns from pg_stat_statements:
//   queryid     int8     — stable hash of the normalised query
//   userid      oid      — joined to pg_user
//   dbid        oid      — joined to pg_database
//   query       text     — normalised statement
//   calls       int8     — cumulative call count
//   total_exec_time double — cumulative ms
//   rows        int8     — cumulative rows returned
func scrapePostgres(ctx context.Context, db *sql.DB) ([]queryState, error) {
	const q = `
		SELECT s.queryid::text,
		       s.query,
		       d.datname,
		       u.rolname,
		       s.calls,
		       s.rows,
		       s.total_exec_time
		FROM pg_stat_statements s
		JOIN pg_database d ON d.oid = s.dbid
		JOIN pg_roles    u ON u.oid = s.userid
		WHERE s.calls > 0
		ORDER BY s.total_exec_time DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []queryState
	for rows.Next() {
		var s queryState
		if err := rows.Scan(&s.queryHash, &s.queryText, &s.dbName, &s.userName, &s.calls, &s.rows, &s.totalTimeMS); err != nil {
			return nil, err
		}
		s.tables = extractTables(s.queryText)
		out = append(out, s)
	}
	return out, rows.Err()
}

// scrapeMySQL reads performance_schema.events_statements_summary_by_digest.
func scrapeMySQL(ctx context.Context, db *sql.DB) ([]queryState, error) {
	const q = `
		SELECT DIGEST,
		       DIGEST_TEXT,
		       SCHEMA_NAME,
		       COUNT_STAR,
		       SUM_ROWS_AFFECTED + SUM_ROWS_SENT,
		       SUM_TIMER_WAIT/1000000000.0
		FROM performance_schema.events_statements_summary_by_digest
		WHERE COUNT_STAR > 0
		ORDER BY SUM_TIMER_WAIT DESC
		LIMIT 500
	`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []queryState
	for rows.Next() {
		var (
			digest sql.NullString
			text   sql.NullString
			schema sql.NullString
			calls  int64
			rowsN  int64
			timeMs float64
		)
		if err := rows.Scan(&digest, &text, &schema, &calls, &rowsN, &timeMs); err != nil {
			return nil, err
		}
		s := queryState{
			queryHash:   digest.String,
			queryText:   text.String,
			dbName:      schema.String,
			calls:       calls,
			rows:        rowsN,
			totalTimeMS: timeMs,
		}
		s.tables = extractTables(s.queryText)
		out = append(out, s)
	}
	return out, rows.Err()
}

// extractTables is a deliberately-cheap heuristic that pulls table names out
// of FROM / JOIN / INTO clauses. It misses CTEs and quoted identifiers but
// gives reviewers something useful in the dashboard's "tables touched"
// column without a full SQL parser.
func extractTables(query string) []string {
	tokens := strings.Fields(query)
	out := []string{}
	seen := map[string]struct{}{}
	for i, tk := range tokens {
		t := strings.ToUpper(strings.TrimRight(tk, ",;()"))
		if (t == "FROM" || t == "JOIN" || t == "UPDATE" || t == "INTO") && i+1 < len(tokens) {
			tbl := strings.TrimRight(tokens[i+1], ",;()")
			tbl = strings.Trim(tbl, "\"`'")
			if tbl == "" {
				continue
			}
			lc := strings.ToLower(tbl)
			if _, dup := seen[lc]; dup {
				continue
			}
			seen[lc] = struct{}{}
			out = append(out, lc)
		}
	}
	return out
}
