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
// SQLite, Postgres, and MySQL are all implemented.
func DateTrunc(driver, column string) string {
	switch driver {
	case SQLite:
		return fmt.Sprintf("strftime('%%Y-%%m-%%d', %s)", column)
	case Postgres:
		// to_char(..., 'YYYY-MM-DD') rather than
		// date_trunc('day', column)::text: date_trunc keeps a full
		// timestamp (e.g. "2026-08-10 00:00:00"), which is not the
		// "2006-01-02"-shaped string every caller here scans into a
		// Go string field and expects to match SQLite's strftime
		// output above. to_char produces that exact shape directly.
		return fmt.Sprintf("to_char(%s, 'YYYY-MM-DD')", column)
	case MySQL:
		// DATE(column) alone is a DATE-typed result. Every caller of
		// this function scans results into a Go string field (see
		// PageviewPoint.Date and countRow.Value), but with
		// parseTime=True set on the DSN (required elsewhere so direct
		// datetime columns scan into time.Time), go-sql-driver/mysql
		// can ONLY scan a DATE/DATETIME result into time.Time — not
		// string or []byte — and errors out on anything else.
		// DATE_FORMAT explicitly converts it to a VARCHAR-typed result
		// at the SQL level first, sidestepping that driver rule
		// entirely and giving the same "2006-01-02" text SQLite's
		// strftime already produces above.
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
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
// SQLite, Postgres, and MySQL are all implemented.
func JSONExtract(driver, column, path string) string {
	switch driver {
	case SQLite:
		return fmt.Sprintf("json_extract(%s, '$.%s')", column, path)
	case Postgres:
		// properties is stored as plain TEXT (see storage.Event),
		// not a native json/jsonb column, so it has to be cast
		// before the -> / ->> operators apply at all. ->> ("get as
		// text") unwraps quoting itself, no JSON_UNQUOTE-style
		// second step needed. It also already returns real SQL NULL
		// for both a missing key and an explicit JSON null value —
		// Postgres has no equivalent of the MySQL "null" -> string
		// "null" quirk documented in the MySQL case below, so no
		// extra JSON_TYPE-style guard is needed here.
		// The cast is safe against malformed/empty JSON because
		// every row's properties is written as either real payload
		// JSON or the literal "{}" default, never "" (see
		// ingest/handler.go), so ::json never errors on a stored row.
		return fmt.Sprintf("(%s::json->>'%s')", column, path)
	case MySQL:
		// MySQL's JSON_EXTRACT returns the value still JSON-quoted
		// (e.g. "\"/pricing\"" instead of "/pricing"), so JSON_UNQUOTE
		// is required to get a plain string back — otherwise every
		// value in a GROUP BY or WHERE would carry literal quote
		// characters and never match/group the way SQLite's
		// already-unquoted json_extract does.
		//
		// Separately: when the key exists but its value is JSON null
		// (e.g. {"referrer": null}), JSON_EXTRACT returns the JSON
		// null literal, not SQL NULL. JSON_UNQUOTE then turns that
		// into the four-character *string* "null" — not an empty or
		// NULL value. SQLite's json_extract has no such quirk; it
		// already returns real SQL NULL for a JSON null value, which
		// is why this only ever surfaces on MySQL. We check
		// JSON_TYPE(...) = 'NULL' explicitly and force real SQL NULL
		// in that case, so downstream COALESCE/blank-filtering logic
		// (which assumes SQL NULL or '', never the string "null")
		// keeps working the same across both drivers.
		return fmt.Sprintf(
			"IF(JSON_TYPE(JSON_EXTRACT(%[1]s, '$.%[2]s')) = 'NULL', NULL, JSON_UNQUOTE(JSON_EXTRACT(%[1]s, '$.%[2]s')))",
			column, path,
		)
	default:
		panic("dialect.JSONExtract: unknown driver " + driver)
	}
}
