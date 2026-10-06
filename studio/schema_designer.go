package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

// AddColumn adds a new column to an existing table (P2.4 §2.2). DuckDB-native
// ALTER TABLE ... ADD COLUMN. Identifier-validated, mutex-guarded, wrapped errors.
func (a *App) AddColumn(table, column, colType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(table)
	if err != nil {
		return fmt.Errorf("invalid table name %q: %w", table, err)
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name %q", column)
	}
	if !isValidColumnType(colType) {
		return fmt.Errorf("invalid column type %q", colType)
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tref.SQL(), engine.QuoteIdent(column), colType)
	if _, err := ddb.Exec(query); err != nil {
		return fmt.Errorf("failed to add column %q to %q: %w", column, table, err)
	}
	a.logger.Info("added column", "table", table, "column", column, "type", colType)
	return nil
}

// AddIndex creates an index over one or more columns (P2.4 §2.2). When unique
// is true a UNIQUE index is created, enforcing uniqueness on subsequent inserts.
func (a *App) AddIndex(table, indexName string, columns []string, unique bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(table)
	if err != nil {
		return fmt.Errorf("invalid table name %q: %w", table, err)
	}
	if !isValidIdentifier(indexName) {
		return fmt.Errorf("invalid index name %q", indexName)
	}
	if len(columns) == 0 {
		return fmt.Errorf("index %q must reference at least one column", indexName)
	}

	quoted := make([]string, 0, len(columns))
	for _, col := range columns {
		if !isValidIdentifier(col) {
			return fmt.Errorf("invalid column name %q", col)
		}
		quoted = append(quoted, engine.QuoteIdent(col))
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	unq := ""
	if unique {
		unq = "UNIQUE "
	}
	query := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unq, engine.QuoteIdent(indexName), tref.SQL(), strings.Join(quoted, ", "))
	if _, err := ddb.Exec(query); err != nil {
		return fmt.Errorf("failed to create index %q on %q: %w", indexName, table, err)
	}
	a.logger.Info("added index", "table", table, "index", indexName, "columns", columns, "unique", unique)
	return nil
}

// DropColumn removes a column from a table (P2.4 §2.2).
func (a *App) DropColumn(table, column string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(table)
	if err != nil {
		return fmt.Errorf("invalid table name %q: %w", table, err)
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name %q", column)
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tref.SQL(), engine.QuoteIdent(column))
	if err := execWithoutIndexes(ddb, tref.String(), column, query); err != nil {
		return fmt.Errorf("failed to drop column %q from %q: %w", column, table, err)
	}
	a.logger.Info("dropped column", "table", table, "column", column)
	return nil
}

// AlterColumnType changes a column's data type via DuckDB ALTER (P2.4 §2.2),
// preserving existing data via an implicit cast where possible.
func (a *App) AlterColumnType(table, column, newType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(table)
	if err != nil {
		return fmt.Errorf("invalid table name %q: %w", table, err)
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name %q", column)
	}
	if !isValidColumnType(newType) {
		return fmt.Errorf("invalid column type %q", newType)
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	query := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tref.SQL(), engine.QuoteIdent(column), newType)
	if err := execWithoutIndexes(ddb, tref.String(), column, query); err != nil {
		return fmt.Errorf("failed to alter column %q on %q to %q: %w", column, table, newType, err)
	}
	a.logger.Info("altered column type", "table", table, "column", column, "type", newType)
	return nil
}

// indexDDLer is the minimal DB surface execWithoutIndexes needs; *sql.DB and
// *sql.Tx both satisfy it.
type indexDDLer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// execWithoutIndexes runs a DDL statement that DuckDB would otherwise reject
// because an index depends on the table (DuckDB's "Cannot alter entry ...
// because there are entries that depend on it"). It captures the table's index
// DDL, drops the indexes, runs the statement, then recreates the indexes so
// uniqueness constraints survive the schema change. Indexes whose recreation
// fails because they reference a now-dropped column are skipped (a dropped
// column legitimately invalidates an index over it).
func execWithoutIndexes(ddb indexDDLer, table, column, ddl string) error {
	// table may be catalog-qualified. The captured index DDL (and the
	// statement itself, for a bare name) is replayed unqualified, so for a
	// table outside the default catalog everything runs on one connection
	// pinned to that catalog and schema with USE.
	tref := tableRefOf(table)
	if db, ok := ddb.(*sql.DB); ok && (tref.Catalog != "" || tref.Schema != "") {
		return execWithoutIndexesPinned(db, tref, table, column, ddl)
	}
	return execWithoutIndexesOn(ddb, tref, table, column, ddl)
}

// connDDL adapts a pinned *sql.Conn to indexDDLer.
type connDDL struct{ c *sql.Conn }

func (d connDDL) Query(q string, args ...any) (*sql.Rows, error) {
	return d.c.QueryContext(context.Background(), q, args...)
}

func (d connDDL) Exec(q string, args ...any) (sql.Result, error) {
	return d.c.ExecContext(context.Background(), q, args...)
}

func execWithoutIndexesPinned(db *sql.DB, tref session.TableRef, table, column, ddl string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	var origCat, origSchema string
	if err := conn.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&origCat, &origSchema); err != nil {
		return fmt.Errorf("failed to read current catalog: %w", err)
	}
	cat := tref.Catalog
	if cat == "" {
		cat = origCat
	}
	target := session.QuoteIdent(cat)
	if tref.Schema != "" {
		target += "." + session.QuoteIdent(tref.Schema)
	}
	if _, err := conn.ExecContext(ctx, "USE "+target); err != nil {
		return fmt.Errorf("failed to select %q: %w", table, err)
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, "USE "+session.QuoteQualified(origCat, origSchema)); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	// Inside the pinned connection the table is addressed by its bare name.
	return execWithoutIndexesOn(connDDL{conn}, session.TableRef{Name: tref.Name}, table, column, ddl)
}

func execWithoutIndexesOn(ddb indexDDLer, tref session.TableRef, table, column, ddl string) error {
	cond, condArgs := session.RefFilter(tref, "database_name", "schema_name", "table_name", 1)
	rows, err := ddb.Query(
		"SELECT database_name, schema_name, index_name, sql FROM duckdb_indexes() WHERE "+cond,
		condArgs...)
	if err != nil {
		return fmt.Errorf("failed to inspect indexes on %q: %w", table, err)
	}
	type idxDef struct{ catalog, schema, name, sql string }
	var indexes []idxDef
	for rows.Next() {
		var catalog, schema, name string
		var idxSQL *string
		if err := rows.Scan(&catalog, &schema, &name, &idxSQL); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan index on %q: %w", table, err)
		}
		if idxSQL == nil || *idxSQL == "" {
			rows.Close()
			return fmt.Errorf("cannot recreate index %q on %q: no DDL available", name, table)
		}
		indexes = append(indexes, idxDef{catalog: catalog, schema: schema, name: name, sql: *idxSQL})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to read indexes on %q: %w", table, err)
	}
	rows.Close()

	for _, idx := range indexes {
		if _, err := ddb.Exec(fmt.Sprintf("DROP INDEX %s", session.QuoteQualified(idx.catalog, idx.schema, idx.name))); err != nil {
			return fmt.Errorf("failed to drop index %q: %w", idx.name, err)
		}
	}

	if _, err := ddb.Exec(ddl); err != nil {
		// Best-effort: restore the indexes we dropped before reporting failure.
		for _, idx := range indexes {
			_, _ = ddb.Exec(idx.sql)
		}
		return err
	}

	for _, idx := range indexes {
		if _, recreateErr := ddb.Exec(idx.sql); recreateErr != nil {
			// An index that referenced the dropped column can no longer be
			// rebuilt; that is expected, so skip it rather than fail the op.
			if column != "" && indexReferencesColumn(idx.sql, column) {
				continue
			}
			return fmt.Errorf("failed to recreate index %q: %w", idx.name, recreateErr)
		}
	}
	return nil
}

// indexReferencesColumn reports whether an index's CREATE INDEX DDL references
// the given column name (quoted), used to decide whether a recreate failure is
// expected after a DROP COLUMN.
func indexReferencesColumn(idxSQL, column string) bool {
	return strings.Contains(idxSQL, engine.QuoteIdent(column)) ||
		strings.Contains(strings.ToLower(idxSQL), strings.ToLower(column))
}

// isValidColumnType performs a conservative syntactic check on a SQL column
// type string before it is interpolated into DDL. It permits letters, digits,
// spaces, parentheses and commas (covering VARCHAR(64), DECIMAL(10,2), etc.)
// while rejecting statement separators and other injection vectors. DuckDB
// still validates the type semantically (an unknown type name like NOT_A_TYPE
// is rejected at Exec time).
func isValidColumnType(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	hasAlpha := false
	for _, c := range t {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasAlpha = true
		case c >= '0' && c <= '9':
		case c == ' ' || c == '(' || c == ')' || c == ',':
		default:
			return false
		}
	}
	return hasAlpha
}

// tableRefOf parses a table argument already validated by engine.ParseTable;
// a name that does not parse is treated as a single bare name.
func tableRefOf(table string) session.TableRef {
	ref, err := session.ParseTableRef(table)
	if err != nil {
		return session.TableRef{Name: table}
	}
	return ref
}
