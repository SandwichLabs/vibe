package session

import (
	"context"
	"database/sql"
	"fmt"
)

// Queryer is the read surface shared by *sql.DB, *sql.Conn and *sql.Tx.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// RefFilter builds a WHERE fragment (and its args, numbered from firstArg)
// selecting one table in a catalog view whose columns are named catCol /
// schemaCol / nameCol (information_schema.*: table_catalog, table_schema,
// table_name; duckdb_*(): database_name, schema_name, table_name). Parts the
// TableRef leaves empty resolve against the executing connection's current
// catalog and schema, so an unqualified name follows whatever default the
// connection has (the session default, or a pinned USE) without any catalog
// name hard-coded. Table names compare case-insensitively, as the legacy
// callers did.
func RefFilter(ref TableRef, catCol, schemaCol, nameCol string, firstArg int) (string, []any) {
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", firstArg+len(args)-1)
	}
	cond := catCol + " = current_database()"
	if ref.Catalog != "" {
		cond = catCol + " = " + next(ref.Catalog)
	}
	if ref.Schema != "" {
		cond += " AND " + schemaCol + " = " + next(ref.Schema)
	} else {
		cond += " AND " + schemaCol + " = current_schema()"
	}
	cond += " AND lower(" + nameCol + ") = lower(" + next(ref.Name) + ")"
	return cond, args
}

func tableFilter(ref TableRef, firstArg int) (string, []any) {
	return RefFilter(ref, "table_catalog", "table_schema", "table_name", firstArg)
}

// ColumnsOf returns a table's columns in ordinal order, or none when the
// table does not exist. ref may be unqualified (current catalog and schema).
func ColumnsOf(ctx context.Context, q Queryer, ref TableRef) ([]ColumnInfo, error) {
	cond, args := tableFilter(ref, 1)
	rows, err := q.QueryContext(ctx,
		`SELECT column_name, data_type, is_nullable = 'NO', ordinal_position
		   FROM information_schema.columns WHERE `+cond+` ORDER BY ordinal_position`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read columns of %q: %w", ref.String(), err)
	}
	defer rows.Close() //nolint:errcheck
	var out []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.DataType, &c.NotNull, &c.Position); err != nil {
			return nil, fmt.Errorf("failed to scan column of %q: %w", ref.String(), err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read columns of %q: %w", ref.String(), err)
	}
	return out, nil
}

// LookupTable finds a table or view (case-insensitively) and returns its
// resolved reference with the stored spelling. ok is false when absent.
func LookupTable(ctx context.Context, q Queryer, ref TableRef) (TableInfo, bool, error) {
	cond, args := tableFilter(ref, 1)
	rows, err := q.QueryContext(ctx,
		`SELECT table_catalog, table_schema, table_name, table_type
		   FROM information_schema.tables WHERE `+cond+` ORDER BY table_name LIMIT 1`, args...)
	if err != nil {
		return TableInfo{}, false, fmt.Errorf("failed to look up table %q: %w", ref.String(), err)
	}
	defer rows.Close() //nolint:errcheck
	if !rows.Next() {
		return TableInfo{}, false, rows.Err()
	}
	var t TableInfo
	if err := rows.Scan(&t.Catalog, &t.Schema, &t.Name, &t.Type); err != nil {
		return TableInfo{}, false, fmt.Errorf("failed to scan table %q: %w", ref.String(), err)
	}
	return t, true, nil
}

// AllTables lists the tables and views of every user schema of catalog
// ("" = the default catalog).
func AllTables(ctx context.Context, s Session, catalog string) ([]TableInfo, error) {
	schemas, err := s.ListSchemas(ctx, catalog)
	if err != nil {
		return nil, err
	}
	var out []TableInfo
	for _, sc := range schemas {
		ts, err := s.ListTables(ctx, catalog, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, ts...)
	}
	return out, nil
}

// TableKey names a table within one catalog.
type TableKey struct{ Schema, Name string }

// EstimatedRows reads DuckDB's table statistics (free, approximate) for every
// table of catalog. Only tables with a positive estimate are present: remote
// catalogs often have none, and an empty or unanalysed table reports 0, so the
// map can be empty and a caller wanting an exact figure must count.
func EstimatedRows(ctx context.Context, db *sql.DB, catalog string) map[TableKey]int64 {
	out := map[TableKey]int64{}
	if db == nil {
		return out
	}
	rows, err := db.QueryContext(ctx,
		`SELECT schema_name, table_name, estimated_size FROM duckdb_tables() WHERE database_name = $1`, catalog)
	if err != nil {
		return out
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var k TableKey
		var sz int64
		if rows.Scan(&k.Schema, &k.Name, &sz) == nil && sz > 0 {
			out[k] = sz
		}
	}
	return out
}
