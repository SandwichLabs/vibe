package agents

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2" // DuckDB driver, for the dedicated parser connection below
)

// --- the catalog: tables, views, databases, functions ---------------------

// catEntry is one table or view of the live catalogs.
type catEntry struct {
	view bool
	sql  string // a view's CREATE VIEW text
}

// catalog is a snapshot of every table and view that exists, loaded fresh for
// each classification (the decision must never rest on a stale schema) and
// only when a reference needs it.
type catalog struct {
	defaultCatalog string
	defaultSchema  string
	entries        map[string]catEntry // catalogKey(catalog, schema, table)
	byName         map[string]catEntry // catalogKey(catalog, "", table): any schema
}

func catalogKey(catalogName, schema, table string) string {
	return strings.ToLower(catalogName) + "\x00" + strings.ToLower(schema) + "\x00" + strings.ToLower(table)
}

// allowedSystemObjects is a CLOSED allow-list of DuckDB's own introspection
// views a statement may read (keyed "schema.table", lower-cased).
// duckdb_databases and pragma_database_list are deliberately absent: they list
// every attached database's file path.
var allowedSystemObjects = map[string]bool{
	"information_schema.tables":                  true,
	"information_schema.columns":                 true,
	"information_schema.schemata":                true,
	"information_schema.views":                   true,
	"information_schema.table_constraints":       true,
	"information_schema.key_column_usage":        true,
	"information_schema.referential_constraints": true,
	"main.duckdb_tables":                         true,
	"main.duckdb_columns":                        true,
	"main.duckdb_views":                          true,
	"main.duckdb_constraints":                    true,
	"main.duckdb_indexes":                        true,
	"main.duckdb_sequences":                      true,
	"main.duckdb_types":                          true,
}

// deniedCatalogObjects is a redundant belt-and-braces deny for system objects a
// security review called out by name.
var deniedCatalogObjects = map[string]bool{
	"pg_settings": true,
	"duckdb_logs": true,
}

func systemObjectKey(schema, table string) string {
	return strings.ToLower(schema) + "." + strings.ToLower(table)
}

// lookup finds the table or view a reference names, applying DuckDB's own
// current-database and current-schema defaults. System objects resolve only
// when they are on allowedSystemObjects.
func (c *catalog) lookup(catalogName, schema, table string) (catEntry, bool) {
	if c == nil || table == "" || deniedCatalogObjects[strings.ToLower(table)] {
		return catEntry{}, false
	}
	type cand struct{ cat, schema string }
	var cands []cand
	switch {
	case catalogName != "":
		cands = append(cands, cand{catalogName, schema})
	case schema != "":
		cands = append(cands, cand{c.defaultCatalog, schema}, cand{schema, ""}, cand{"temp", schema}, cand{"system", schema})
	default:
		cands = append(cands, cand{c.defaultCatalog, c.defaultSchema}, cand{"temp", "main"}, cand{"system", "main"})
	}
	for _, cd := range cands {
		var e catEntry
		var ok bool
		if cd.schema == "" {
			e, ok = c.byName[catalogKey(cd.cat, "", table)]
		} else {
			e, ok = c.entries[catalogKey(cd.cat, cd.schema, table)]
		}
		if !ok {
			continue
		}
		if strings.EqualFold(cd.cat, "system") {
			sch := cd.schema
			if sch == "" {
				sch = "main"
			}
			if !allowedSystemObjects[systemObjectKey(sch, table)] {
				continue
			}
		}
		return e, true
	}
	return catEntry{}, false
}

// loadCatalog queries the user's own DuckDB for every table and view. On error
// the catalog is empty and resolves nothing (fail closed).
func loadCatalog(ctx context.Context, userDB queryDB) (*catalog, error) {
	c := &catalog{entries: map[string]catEntry{}, byName: map[string]catEntry{}}
	if nilDB(userDB) {
		return c, errors.New("no duckdb connection available")
	}
	if err := userDB.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&c.defaultCatalog, &c.defaultSchema); err != nil {
		return &catalog{}, err
	}
	load := func(query string, view bool) error {
		rows, err := userDB.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck
		for rows.Next() {
			var dbName, schema, name string
			var sqlText sql.NullString
			if view {
				if err := rows.Scan(&dbName, &schema, &name, &sqlText); err != nil {
					return err
				}
			} else if err := rows.Scan(&dbName, &schema, &name); err != nil {
				return err
			}
			e := catEntry{view: view, sql: sqlText.String}
			c.entries[catalogKey(dbName, schema, name)] = e
			c.byName[catalogKey(dbName, "", name)] = e
		}
		return rows.Err()
	}
	if err := load("SELECT database_name, schema_name, table_name FROM duckdb_tables()", false); err != nil {
		return &catalog{}, err
	}
	if err := load("SELECT database_name, schema_name, view_name, sql FROM duckdb_views()", true); err != nil {
		return &catalog{}, err
	}
	return c, nil
}

// dbRow is one attached database as DuckDB reports it. The path is never read.
type dbRow struct {
	name     string
	typ      string
	readOnly bool
	internal bool
}

func loadDatabases(ctx context.Context, userDB queryDB) (map[string]dbRow, error) {
	out := map[string]dbRow{}
	if nilDB(userDB) {
		return out, errors.New("no duckdb connection available")
	}
	rows, err := userDB.QueryContext(ctx, "SELECT database_name, type, readonly, internal FROM duckdb_databases()")
	if err != nil {
		return out, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var r dbRow
		var typ sql.NullString
		if err := rows.Scan(&r.name, &typ, &r.readOnly, &r.internal); err != nil {
			return out, err
		}
		r.typ = typ.String
		out[strings.ToLower(r.name)] = r
	}
	return out, rows.Err()
}

// funcInfo is what the live catalog says about function names: which are table
// functions (a table position needs the allow-list) and which are user macros
// (whose definition is analysed).
type funcInfo struct {
	table  map[string]bool
	macros map[string]string // lower name -> definition (scalar: an expression; table macro: a query)
	tmacro map[string]bool
}

func loadFunctions(ctx context.Context, userDB queryDB) (*funcInfo, error) {
	f := &funcInfo{table: map[string]bool{}, macros: map[string]string{}, tmacro: map[string]bool{}}
	if nilDB(userDB) {
		return f, errors.New("no duckdb connection available")
	}
	rows, err := userDB.QueryContext(ctx, `SELECT lower(function_name), function_type, macro_definition, internal FROM duckdb_functions()
		WHERE function_type IN ('table','table_macro','pragma','macro')`)
	if err != nil {
		return f, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var name, typ string
		var def sql.NullString
		var internal bool
		if err := rows.Scan(&name, &typ, &def, &internal); err != nil {
			return f, err
		}
		switch typ {
		case "table", "pragma":
			f.table[name] = true
		case "table_macro":
			f.table[name] = true
			if !internal { // built-in macros are DuckDB's own and not analysed
				f.tmacro[name] = true
				f.macros[name] = def.String
			}
		case "macro":
			if !internal {
				f.macros[name] = def.String
			}
		}
	}
	return f, rows.Err()
}
