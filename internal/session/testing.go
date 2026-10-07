package session

import (
	"context"
	"database/sql"
)

// UnhardenedForTests wraps a bare *sql.DB (for example sql.Open("duckdb", "")) in a
// Session WITHOUT the hardening read-back or the configuration lock. It exists
// so unit tests of dependent packages can seed data through their own handle;
// production code must use New (only package sessiontest may call this), which fails closed.
func UnhardenedForTests(db *sql.DB, opts ...Option) Session {
	c := newConfig(opts)
	var def, schema string
	if err := db.QueryRowContext(context.Background(), "SELECT current_database(), current_schema()").Scan(&def, &schema); err != nil {
		def, schema = "memory", "" // not a DuckDB handle (a fake): schema lookups will simply fail
	}
	return newHub(db, c, def, schema, false)
}
