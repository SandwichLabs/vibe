// Package crud provides the transactional, type-coercing write-back core
// behind Studio's (Wails) datasheet bindings.
//
// All-string inputs (from the Vue datasheet) are coerced to each column's
// DuckDB type via CAST(? AS <type>) read from information_schema. Failures
// come back as a structured *WriteError rather than a raw SQL error string.
package crud

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// tableRef parses a table argument: a bare name, or "schema.table" /
// "catalog.schema.table" for a table in an attached connection. Every part is
// quoted when the SQL is built (engine.TableSQL). A name that does not parse
// falls back to a single identifier so it can never splice SQL.
func tableRef(table string) session.TableRef {
	ref, err := session.ParseTableRef(table)
	if err != nil {
		return session.TableRef{Name: table}
	}
	return ref
}

// WriteResult reports the outcome of a single CRUD write (rows affected).
type WriteResult struct {
	Affected int64 `json:"affected"`
}

// WriteError is a structured, field-keyed CRUD failure suitable for per-field
// UI feedback. Fields maps a column name to a human-readable problem; Message
// is a fallback for non-field-specific failures. It must be returned instead of
// a raw SQL error string so the form UI can highlight the offending controls.
type WriteError struct {
	Message string `json:"message"`
}

func (e *WriteError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// RowQuerier is the subset of *sql.DB / *sql.Tx used for reads.
type RowQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// TxExecer is the subset of *sql.Tx used by the write core.
type TxExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// ColumnTypes returns a map of column name -> DuckDB data type for the given
// table, read from information_schema. It is used to build typed CAST(...)
// placeholders so all-string form inputs are coerced to the column's type
// before a write (Access-style bound-control coercion).
func ColumnTypes(q RowQuerier, table string) (map[string]string, error) {
	// Unqualified parts resolve against the connection's current catalog and
	// schema (the session default), so no catalog name is assumed.
	cond, args := session.RefFilter(tableRef(table), "table_catalog", "table_schema", "table_name", 1)
	rows, err := q.Query(
		"SELECT column_name, data_type FROM information_schema.columns WHERE "+cond, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read column types for %q: %w", table, err)
	}
	defer rows.Close() //nolint:errcheck

	out := make(map[string]string)
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, fmt.Errorf("failed to scan column type for %q: %w", table, err)
		}
		out[name] = typ
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate column types for %q: %w", table, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("table %q does not exist or has no columns", table)
	}
	return out, nil
}

// isTextType reports whether a DuckDB type stores text — the only family where
// an empty string is a meaningful value rather than a blank form control.
func isTextType(t string) bool {
	u := strings.ToUpper(t)
	return strings.HasPrefix(u, "VARCHAR") || strings.HasPrefix(u, "CHAR") ||
		strings.HasPrefix(u, "TEXT") || strings.HasPrefix(u, "STRING")
}

// InsertRowTx coerces and inserts a single row within a transaction.
// Nil values — and empty strings bound for non-text columns (a blank form
// control) — are omitted from the INSERT so column DEFAULTs apply; in
// particular an auto-number primary key fills itself in when the form leaves
// id blank, instead of failing the NOT NULL constraint with an explicit NULL.
func InsertRowTx(tx TxExecer, colTypes map[string]string, table string, values map[string]any) (*WriteResult, error) {
	kept := make(map[string]any, len(values))
	for c, v := range values {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" && !isTextType(castType(colTypes, c)) {
			continue
		}
		kept[c] = v
	}
	values = kept
	if len(values) == 0 {
		res, err := tx.Exec(fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", engine.TableSQL(table)))
		if err != nil {
			return nil, &WriteError{Message: fmt.Sprintf("failed to insert into %q: %v", table, err)}
		}
		affected, _ := res.RowsAffected()
		return &WriteResult{Affected: affected}, nil
	}

	cols := sortedKeys(values)
	colSQL := make([]string, 0, len(cols))
	placeholders := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for _, c := range cols {
		colSQL = append(colSQL, session.QuoteIdent(c))
		placeholders = append(placeholders, fmt.Sprintf("CAST(? AS %s)", castType(colTypes, c)))
		args = append(args, values[c])
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", engine.TableSQL(table), strings.Join(colSQL, ", "), strings.Join(placeholders, ", "))
	res, err := tx.Exec(query, args...)
	if err != nil {
		return nil, &WriteError{Message: fmt.Sprintf("failed to insert into %q: %v", table, err)}
	}
	affected, _ := res.RowsAffected()
	return &WriteResult{Affected: affected}, nil
}

// UpdateRowTx coerces and updates a single row within a transaction.
// A 0-affected-row outcome (PK mismatch) is surfaced as an error rather than a
// silent no-op.
func UpdateRowTx(tx TxExecer, colTypes map[string]string, table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	cols := sortedKeys(values)
	setClauses := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols)+1)
	for _, c := range cols {
		setClauses = append(setClauses, fmt.Sprintf("%s = CAST(? AS %s)", session.QuoteIdent(c), castType(colTypes, c)))
		args = append(args, values[c])
	}
	args = append(args, pkVal)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = CAST(? AS %s)",
		engine.TableSQL(table), strings.Join(setClauses, ", "), session.QuoteIdent(pkCol), castType(colTypes, pkCol))
	res, err := tx.Exec(query, args...)
	if err != nil {
		return nil, &WriteError{Message: fmt.Sprintf("failed to update %q: %v", table, err)}
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, &WriteError{Message: fmt.Sprintf("no row in %q matched %s = %v", table, pkCol, pkVal)}
	}
	return &WriteResult{Affected: affected}, nil
}

// DeleteRowTx deletes a single row by primary key within a transaction. The
// pkVal is coerced to the PK column's type via CAST(? AS <type>) (mirroring
// UpdateRowTx), and a 0-affected-row outcome (PK mismatch) is surfaced as a
// structured *WriteError rather than a silent no-op.
func DeleteRowTx(tx TxExecer, colTypes map[string]string, table, pkCol string, pkVal any) (*WriteResult, error) {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = CAST(? AS %s)",
		engine.TableSQL(table), session.QuoteIdent(pkCol), castType(colTypes, pkCol))
	res, err := tx.Exec(query, pkVal)
	if err != nil {
		return nil, &WriteError{Message: fmt.Sprintf("failed to delete from %q: %v", table, err)}
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, &WriteError{Message: fmt.Sprintf("no row in %q matched %s = %v", table, pkCol, pkVal)}
	}
	return &WriteResult{Affected: affected}, nil
}

// castType returns the DuckDB cast target for a column, defaulting to VARCHAR
// for unknown columns so the statement still parses (an unknown column will
// fail at the INSERT/UPDATE with a clear error).
func castType(colTypes map[string]string, col string) string {
	if t, ok := colTypes[col]; ok && t != "" {
		return t
	}
	return "VARCHAR"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
