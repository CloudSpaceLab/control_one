package dbquery

// Anonymous driver imports — required by database/sql to register the
// drivers we dispatch through driverFor(). Without these, sql.Open()
// errors with "unknown driver".
//
// Postgres + MySQL drivers are also pulled in elsewhere in the binary,
// but importing them here keeps dbquery self-contained.
import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
)
