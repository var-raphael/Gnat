package dialect

import "fmt"

const (
	SQLite   = "sqlite"
	Postgres = "postgres"
	MySQL    = "mysql"
)

func DateTrunc(driver, column string) string {
	switch driver {
	case SQLite:
		return fmt.Sprintf("strftime('%%Y-%%m-%%d', %s)", column)
	case Postgres:
   return fmt.Sprintf("to_char(%s, 'YYYY-MM-DD')", column)
	case MySQL:
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
	default:
		panic("dialect.DateTrunc: unknown driver " + driver)
	}
}


func JSONExtract(driver, column, path string) string {
	switch driver {
	case SQLite:
		return fmt.Sprintf("json_extract(%s, '$.%s')", column, path)
	case Postgres:

		return fmt.Sprintf("(%s::json->>'%s')", column, path)
	case MySQL:

		return fmt.Sprintf(
			"IF(JSON_TYPE(JSON_EXTRACT(%[1]s, '$.%[2]s')) = 'NULL', NULL, JSON_UNQUOTE(JSON_EXTRACT(%[1]s, '$.%[2]s')))",
			column, path,
		)
	default:
		panic("dialect.JSONExtract: unknown driver " + driver)
	}
}
