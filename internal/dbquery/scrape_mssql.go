package dbquery

import (
	"context"
	"database/sql"
	"strings"
)

// scrapeMSSQL pulls per-statement metrics from sys.dm_exec_query_stats
// joined to sys.dm_exec_sql_text + sys.dm_exec_sessions for source
// attribution. Each tick the collector keeps the latest snapshot keyed by
// the SQL handle's hex form so the manager's diff loop emits one db.query
// event per new/changed statement, just like the Postgres path.
//
// Required permission: VIEW SERVER STATE on the SQL Server instance.
// `GRANT VIEW SERVER STATE TO controlone;` is the minimum needed; no
// data-plane access is required.
//
// Note on encryption: the DSN should pass `encrypt=true` so the agent
// connects over TLS. We do not enforce it in code so on-prem instances
// without a cert can still be observed during deploy bring-up.
func scrapeMSSQL(ctx context.Context, db *sql.DB) ([]queryState, error) {
	const q = `
SELECT TOP 200
    CONVERT(VARCHAR(64), HASHBYTES('MD5', CAST(qs.sql_handle AS VARBINARY(MAX))), 2) AS query_hash,
    SUBSTRING(st.text, qs.statement_start_offset/2 + 1,
              ((CASE qs.statement_end_offset
                  WHEN -1 THEN DATALENGTH(st.text)
                  ELSE qs.statement_end_offset
                END - qs.statement_start_offset)/2) + 1)              AS query_text,
    DB_NAME(st.dbid)                                                  AS database_name,
    ISNULL(s.login_name, '')                                          AS user_name,
    ISNULL(s.client_net_address, '')                                  AS client_ip,
    qs.execution_count                                                AS calls,
    qs.total_rows                                                     AS rows_total,
    qs.total_elapsed_time / 1000.0                                    AS total_time_ms
FROM sys.dm_exec_query_stats qs
CROSS APPLY sys.dm_exec_sql_text(qs.sql_handle) AS st
LEFT JOIN sys.dm_exec_sessions s
       ON s.session_id = (SELECT TOP 1 session_id FROM sys.dm_exec_requests WHERE sql_handle = qs.sql_handle)
WHERE qs.last_execution_time >= DATEADD(MINUTE, -5, GETDATE())
ORDER BY qs.last_execution_time DESC;
`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]queryState, 0, 64)
	for rows.Next() {
		var s queryState
		var totalTime sql.NullFloat64
		if err := rows.Scan(
			&s.queryHash,
			&s.queryText,
			&s.dbName,
			&s.userName,
			&s.clientIP,
			&s.calls,
			&s.rows,
			&totalTime,
		); err != nil {
			return nil, err
		}
		s.queryText = strings.TrimSpace(s.queryText)
		if totalTime.Valid {
			s.totalTimeMS = totalTime.Float64
		}
		s.tables = extractTables(s.queryText)
		out = append(out, s)
	}
	return out, rows.Err()
}

// scrapeMSSQLLongRunning hits sys.dm_exec_requests to find currently-active
// statements that have been running > 30 s. Emitted as
// `db.query.long_running` so a single hung query during exfil shows up
// even before it finishes (and thus before it lands in dm_exec_query_stats).
func scrapeMSSQLLongRunning(ctx context.Context, db *sql.DB) ([]queryState, error) {
	const q = `
SELECT
    CONVERT(VARCHAR(64), HASHBYTES('MD5', CAST(r.sql_handle AS VARBINARY(MAX))), 2) AS query_hash,
    SUBSTRING(qt.text, r.statement_start_offset/2 + 1,
              (CASE r.statement_end_offset
                  WHEN -1 THEN DATALENGTH(qt.text)
                  ELSE r.statement_end_offset
                END - r.statement_start_offset)/2 + 1)               AS query_text,
    DB_NAME(qt.dbid)                                                 AS database_name,
    ISNULL(s.login_name, '')                                         AS user_name,
    ISNULL(s.client_net_address, '')                                 AS client_ip,
    1                                                                AS calls,
    r.row_count                                                      AS rows_total,
    r.total_elapsed_time                                             AS total_time_ms
FROM sys.dm_exec_requests r
CROSS APPLY sys.dm_exec_sql_text(r.sql_handle) AS qt
LEFT JOIN sys.dm_exec_sessions s ON s.session_id = r.session_id
WHERE r.session_id > 50
  AND r.total_elapsed_time > 30000;
`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]queryState, 0, 16)
	for rows.Next() {
		var s queryState
		var totalTime sql.NullFloat64
		if err := rows.Scan(
			&s.queryHash,
			&s.queryText,
			&s.dbName,
			&s.userName,
			&s.clientIP,
			&s.calls,
			&s.rows,
			&totalTime,
		); err != nil {
			return nil, err
		}
		if totalTime.Valid {
			s.totalTimeMS = totalTime.Float64
		}
		s.queryText = strings.TrimSpace(s.queryText)
		s.tables = extractTables(s.queryText)
		out = append(out, s)
	}
	return out, rows.Err()
}
