// Package dialect isolates the small handful of SQL fragments that
// genuinely differ across SQLite, Postgres, and MySQL. Everything else
// in Gnat's queries (WHERE, GROUP BY, COUNT, MIN, IN, joins) is already
// portable through GORM as-is and needs no help from this package.
//
// Only two operations actually differ per engine:
//   - truncating a timestamp down to a calendar day for grouping
//   - extracting a field out of a JSON-text column
//
// Every query that needs either of these calls DateTrunc or
// JSONExtract instead of hardcoding SQL syntax, so adding a new driver
// is a matter of filling in one case in each function rather than
// hunting through every query file in internal/query.
package dialect

import "fmt"

// SQLite, Postgres, and MySQL driver name constants, matching what
// GORM's Dialector.Name() returns and what config.DatabaseConfig.Driver
// is set to.
const (
	SQLite   = "sqlite"
	Postgres = "postgres"
	MySQL    = "mysql"
)

// DateTrunc returns a SQL expression that truncates the named timestamp
// column down to its calendar day, suitable for use in a GROUP BY to
// bucket rows per day. column should be a bare column name, e.g.
// "timestamp" — callers are responsible for any table-qualification.
//
// Only SQLite is implemented today, since that's the only driver
// actually exercised so far. Postgres/MySQL panic deliberately rather
// than silently returning SQLite syntax that would fail at query time
// with a much more confusing error — this is meant to be the very
// first thing that breaks, loudly, the moment either driver is
// switched on, so it's obvious what still needs implementing.
func DateTrunc(driver, column string) string {
	switch driver {
	case SQLite:
		return fmt.Sprintf("strftime('%%Y-%%m-%%d', %s)", column)
	case Postgres:
		panic("dialect.DateTrunc: postgres not yet implemented")
	case MySQL:
		panic("dialect.DateTrunc: mysql not yet implemented")
	default:
		panic("dialect.DateTrunc: unknown driver " + driver)
	}
}

// JSONExtract returns a SQL expression that pulls the value at path out
// of a JSON-text column, e.g. JSONExtract("sqlite", "properties",
// "referrer") for the JSON field "referrer" inside the properties
// column. path is a bare field name, not a full JSON-path expression;
// callers needing nested paths will need to extend this signature when
// that need actually arises.
//
// Same deliberate-panic behavior as DateTrunc for unimplemented
// drivers, and for the same reason.
func JSONExtract(driver, column, path string) string {
	switch driver {
	case SQLite:
		return fmt.Sprintf("json_extract(%s, '$.%s')", column, path)
	case Postgres:
		panic("dialect.JSONExtract: postgres not yet implemented")
	case MySQL:
		panic("dialect.JSONExtract: mysql not yet implemented")
	default:
		panic("dialect.JSONExtract: unknown driver " + driver)
	}
}
