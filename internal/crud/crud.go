// Package crud provides the transactional, type-coercing, validation-enforcing
// write-back core shared by the Studio (Wails) bindings and the embedded HTTP
// server's form-submission pipeline (parity plan §1.1).
//
// All-string inputs (from HTTP forms / the Vue datasheet) are coerced to each
// column's DuckDB type via CAST(? AS <type>) read from information_schema, and
// the Access-style _vibe_field_meta required + validation_rule constraints are
// enforced server-side, returning a field-keyed *WriteError so the UI can
// highlight the offending controls instead of surfacing a raw SQL string.
package crud

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/schema"
)

// WriteResult reports the outcome of a single CRUD write (rows affected).
type WriteResult struct {
	Affected int64 `json:"affected"`
}

// WriteError is a structured, field-keyed CRUD failure suitable for per-field
// UI feedback. Fields maps a column name to a human-readable problem; Message
// is a fallback for non-field-specific failures. It must be returned instead of
// a raw SQL error string so the form UI can highlight the offending controls.
type WriteError struct {
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
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
	rows, err := q.Query(
		"SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'main' AND table_name = $1",
		table)
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

// FieldMetaMap loads the Access-style field metadata for a table keyed by
// column name. It tolerates a missing/empty meta table.
func FieldMetaMap(store *schema.Store, table string) (map[string]schema.FieldMeta, error) {
	metas, err := store.GetFieldMeta(table)
	if err != nil {
		return nil, err
	}
	out := make(map[string]schema.FieldMeta, len(metas))
	for _, m := range metas {
		out[m.ColumnName] = m
	}
	return out, nil
}

// isEmptyValue reports whether v is a "missing" value for required-field
// purposes (nil or an empty/whitespace string).
func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// validateRequired enforces _vibe_field_meta.required server-side, returning a
// field-keyed *WriteError naming every missing required column. For inserts,
// required columns absent from values are violations; for updates, only
// columns present in the payload are checked (partial updates are allowed).
func validateRequired(metas map[string]schema.FieldMeta, values map[string]any, isInsert bool) *WriteError {
	we := &WriteError{Fields: map[string]string{}}
	for col, m := range metas {
		if !m.Required {
			continue
		}
		v, present := values[col]
		if isInsert {
			if !present || isEmptyValue(v) {
				we.Fields[col] = requiredMessage(m)
			}
		} else if present && isEmptyValue(v) {
			we.Fields[col] = requiredMessage(m)
		}
	}
	if len(we.Fields) == 0 {
		return nil
	}
	we.Message = "validation failed"
	return we
}

func requiredMessage(m schema.FieldMeta) string {
	label := m.Caption
	if label == "" {
		label = m.ColumnName
	}
	if label == "" {
		return "This field is required"
	}
	return fmt.Sprintf("%s is required", label)
}

// candidateRow builds a single-row SELECT that casts every supplied value to
// its column type, exposing each column by name. It is used to evaluate
// validation_rule expressions against the coerced candidate row inside the
// transaction, without touching the target table.
func candidateRow(colTypes map[string]string, values map[string]any) (string, []any) {
	cols := sortedKeys(values)
	exprs := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for _, c := range cols {
		exprs = append(exprs, fmt.Sprintf("CAST(? AS %s) AS %q", castType(colTypes, c), c))
		args = append(args, values[c])
	}
	return "SELECT " + strings.Join(exprs, ", "), args
}

// validateRules evaluates each column's validation_rule against the candidate
// row. A rule that evaluates to FALSE (NULL passes, matching SQL CHECK
// semantics) yields a field-keyed *WriteError.
func validateRules(exec TxExecer, colTypes map[string]string, metas map[string]schema.FieldMeta, values map[string]any) (*WriteError, error) {
	we := &WriteError{Fields: map[string]string{}}
	for _, col := range sortedKeys(metas) {
		m := metas[col]
		rule := strings.TrimSpace(m.ValidationRule)
		if rule == "" {
			continue
		}
		base, args := candidateRow(colTypes, values)
		// COALESCE so a NULL rule result (unknown) is treated as passing.
		query := fmt.Sprintf("SELECT COALESCE((%s), TRUE) FROM (%s) AS candidate", rule, base)
		var ok bool
		if err := exec.QueryRow(query, args...).Scan(&ok); err != nil {
			return nil, fmt.Errorf("failed to evaluate validation rule for %q.%q: %w", m.TableName, col, err)
		}
		if !ok {
			we.Fields[col] = validationMessage(m)
		}
	}
	if len(we.Fields) == 0 {
		return nil, nil
	}
	we.Message = "validation failed"
	return we, nil
}

func validationMessage(m schema.FieldMeta) string {
	if msg := strings.TrimSpace(m.ValidationMsg); msg != "" {
		return msg
	}
	return fmt.Sprintf("%s is invalid", labelFor(m))
}

func labelFor(m schema.FieldMeta) string {
	if m.Caption != "" {
		return m.Caption
	}
	return m.ColumnName
}

// InsertRowTx coerces, validates and inserts a single row within a transaction.
func InsertRowTx(tx TxExecer, colTypes map[string]string, metas map[string]schema.FieldMeta, table string, values map[string]any) (*WriteResult, error) {
	if we := validateRequired(metas, values, true); we != nil {
		return nil, we
	}
	if we, err := validateRules(tx, colTypes, metas, values); err != nil {
		return nil, err
	} else if we != nil {
		return nil, we
	}

	cols := sortedKeys(values)
	colSQL := make([]string, 0, len(cols))
	placeholders := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for _, c := range cols {
		colSQL = append(colSQL, fmt.Sprintf("%q", c))
		placeholders = append(placeholders, fmt.Sprintf("CAST(? AS %s)", castType(colTypes, c)))
		args = append(args, values[c])
	}

	query := fmt.Sprintf("INSERT INTO %q (%s) VALUES (%s)", table, strings.Join(colSQL, ", "), strings.Join(placeholders, ", "))
	res, err := tx.Exec(query, args...)
	if err != nil {
		return nil, &WriteError{Message: fmt.Sprintf("failed to insert into %q: %v", table, err)}
	}
	affected, _ := res.RowsAffected()
	return &WriteResult{Affected: affected}, nil
}

// UpdateRowTx coerces, validates and updates a single row within a transaction.
// A 0-affected-row outcome (PK mismatch) is surfaced as an error rather than a
// silent no-op.
func UpdateRowTx(tx TxExecer, colTypes map[string]string, metas map[string]schema.FieldMeta, table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	if we := validateRequired(metas, values, false); we != nil {
		return nil, we
	}
	if we, err := validateRules(tx, colTypes, metas, values); err != nil {
		return nil, err
	} else if we != nil {
		return nil, we
	}

	cols := sortedKeys(values)
	setClauses := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols)+1)
	for _, c := range cols {
		setClauses = append(setClauses, fmt.Sprintf("%q = CAST(? AS %s)", c, castType(colTypes, c)))
		args = append(args, values[c])
	}
	args = append(args, pkVal)

	query := fmt.Sprintf("UPDATE %q SET %s WHERE %q = CAST(? AS %s)",
		table, strings.Join(setClauses, ", "), pkCol, castType(colTypes, pkCol))
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
	query := fmt.Sprintf("DELETE FROM %q WHERE %q = CAST(? AS %s)",
		table, pkCol, castType(colTypes, pkCol))
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
