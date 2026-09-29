package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// AddColumn adds a new column to an existing table (P2.4 §2.2). DuckDB-native
// ALTER TABLE ... ADD COLUMN. Identifier-validated, mutex-guarded, wrapped errors.
func (a *App) AddColumn(table, column, colType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name %q", column)
	}
	if !isValidColumnType(colType) {
		return fmt.Errorf("invalid column type %q", colType)
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", engine.QuoteIdent(table), engine.QuoteIdent(column), colType)
	if _, err := ddb.Exec(query); err != nil {
		return fmt.Errorf("failed to add column %q to %q: %w", column, table, err)
	}
	a.markDirty()
	a.logger.Info("added column", "table", table, "column", column, "type", colType)
	return nil
}

// AddIndex creates an index over one or more columns (P2.4 §2.2). When unique
// is true a UNIQUE index is created, enforcing uniqueness on subsequent inserts.
func (a *App) AddIndex(table, indexName string, columns []string, unique bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name %q", table)
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

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	unq := ""
	if unique {
		unq = "UNIQUE "
	}
	query := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unq, engine.QuoteIdent(indexName), engine.QuoteIdent(table), strings.Join(quoted, ", "))
	if _, err := ddb.Exec(query); err != nil {
		return fmt.Errorf("failed to create index %q on %q: %w", indexName, table, err)
	}
	a.markDirty()
	a.logger.Info("added index", "table", table, "index", indexName, "columns", columns, "unique", unique)
	return nil
}

// DropColumn removes a column from a table (P2.4 §2.2).
func (a *App) DropColumn(table, column string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name %q", column)
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", engine.QuoteIdent(table), engine.QuoteIdent(column))
	if err := execWithoutIndexes(ddb, table, column, query); err != nil {
		return fmt.Errorf("failed to drop column %q from %q: %w", column, table, err)
	}
	a.markDirty()
	a.logger.Info("dropped column", "table", table, "column", column)
	return nil
}

// AlterColumnType changes a column's data type via DuckDB ALTER (P2.4 §2.2),
// preserving existing data via an implicit cast where possible.
func (a *App) AlterColumnType(table, column, newType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if !isValidIdentifier(table) {
		return fmt.Errorf("invalid table name %q", table)
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name %q", column)
	}
	if !isValidColumnType(newType) {
		return fmt.Errorf("invalid column type %q", newType)
	}

	ddb := a.container.DuckDBDB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}

	query := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", engine.QuoteIdent(table), engine.QuoteIdent(column), newType)
	if err := execWithoutIndexes(ddb, table, column, query); err != nil {
		return fmt.Errorf("failed to alter column %q on %q to %q: %w", column, table, newType, err)
	}
	a.markDirty()
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
	rows, err := ddb.Query(
		"SELECT index_name, sql FROM duckdb_indexes() WHERE schema_name = 'main' AND table_name = $1",
		table)
	if err != nil {
		return fmt.Errorf("failed to inspect indexes on %q: %w", table, err)
	}
	type idxDef struct{ name, sql string }
	var indexes []idxDef
	for rows.Next() {
		var name string
		var idxSQL *string
		if err := rows.Scan(&name, &idxSQL); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan index on %q: %w", table, err)
		}
		if idxSQL == nil || *idxSQL == "" {
			rows.Close()
			return fmt.Errorf("cannot recreate index %q on %q: no DDL available", name, table)
		}
		indexes = append(indexes, idxDef{name: name, sql: *idxSQL})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to read indexes on %q: %w", table, err)
	}
	rows.Close()

	for _, idx := range indexes {
		if _, err := ddb.Exec(fmt.Sprintf("DROP INDEX %s", engine.QuoteIdent(idx.name))); err != nil {
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
