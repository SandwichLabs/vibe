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

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/session"
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

func tableSQL(table string) string { return engine.TableSQL(table) }

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

// FieldMetaMap loads the Access-style field metadata for a table keyed by
// column name. It tolerates a missing/empty meta table.
func FieldMetaMap(store *FieldMetaStore, table string) (map[string]FieldMeta, error) {
	metas, err := store.GetFieldMeta(table)
	if err != nil {
		return nil, err
	}
	out := make(map[string]FieldMeta, len(metas))
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
func validateRequired(metas map[string]FieldMeta, values map[string]any, isInsert bool) *WriteError {
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

func requiredMessage(m FieldMeta) string {
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
		exprs = append(exprs, fmt.Sprintf("CAST(? AS %s) AS %s", castType(colTypes, c), engine.QuoteIdent(c)))
		args = append(args, values[c])
	}
	return "SELECT " + strings.Join(exprs, ", "), args
}

// validateRules evaluates each column's validation_rule against the candidate
// row. A rule that evaluates to FALSE (NULL passes, matching SQL CHECK
// semantics) yields a field-keyed *WriteError.
func validateRules(exec TxExecer, colTypes map[string]string, metas map[string]FieldMeta, values map[string]any) (*WriteError, error) {
	we := &WriteError{Fields: map[string]string{}}
	for _, col := range sortedKeys(metas) {
		m := metas[col]
		rule := strings.TrimSpace(m.ValidationRule)
		if rule == "" {
			continue
		}
		// A column absent from the payload is NULL in the candidate row, and a
		// NULL rule result passes (SQL CHECK semantics) — but the candidate
		// SELECT can't even reference it. Skip instead of erroring, so partial
		// writes don't trip rules on untouched columns. Blank values skip too:
		// format rules judge entered values, blankness is Required's domain
		// (Access semantics — a NULL/empty field passes the validation rule).
		if v, present := values[col]; !present || isEmptyValue(v) {
			continue
		}
		base, args := candidateRow(colTypes, values)
		// Access-style rules may be column-relative (e.g. LIKE '%@%' or > 0),
		// in which case the column is the implicit left operand. Full
		// expressions that already name a column (e.g. qty > 0) are used as-is.
		expr := normalizeRule(col, rule)
		// COALESCE so a NULL rule result (unknown) is treated as passing.
		query := fmt.Sprintf("SELECT COALESCE((%s), TRUE) FROM (%s) AS candidate", expr, base)
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

// normalizeRule turns an Access-style validation rule into a complete SQL
// boolean expression. A rule that begins with a comparison/predicate operator
// (LIKE, IN, BETWEEN, IS, NOT, or a relational operator) is column-relative:
// the column is its implicit left operand, so we prepend the quoted column
// reference. A rule that already starts with anything else (e.g. an identifier
// as in "qty > 0", or a function call) is treated as a full expression.
func normalizeRule(col, rule string) string {
	upper := strings.ToUpper(rule)
	relativePrefixes := []string{"LIKE", "IN ", "IN(", "BETWEEN", "IS ", "NOT ", "NOT(", "<>", "!=", ">=", "<=", "=", "<", ">"}
	for _, p := range relativePrefixes {
		if strings.HasPrefix(upper, p) {
			return fmt.Sprintf("%s %s", engine.QuoteIdent(col), rule)
		}
	}
	return rule
}

func validationMessage(m FieldMeta) string {
	if msg := strings.TrimSpace(m.ValidationMsg); msg != "" {
		return msg
	}
	return fmt.Sprintf("%s is invalid", labelFor(m))
}

func labelFor(m FieldMeta) string {
	if m.Caption != "" {
		return m.Caption
	}
	return m.ColumnName
}

// isTextType reports whether a DuckDB type stores text — the only family where
// an empty string is a meaningful value rather than a blank form control.
func isTextType(t string) bool {
	u := strings.ToUpper(t)
	return strings.HasPrefix(u, "VARCHAR") || strings.HasPrefix(u, "CHAR") ||
		strings.HasPrefix(u, "TEXT") || strings.HasPrefix(u, "STRING")
}

// InsertRowTx coerces, validates and inserts a single row within a transaction.
// Nil values — and empty strings bound for non-text columns (a blank form
// control) — are omitted from the INSERT so column DEFAULTs apply; in
// particular an auto-number primary key fills itself in when the form leaves
// id blank, instead of failing the NOT NULL constraint with an explicit NULL.
func InsertRowTx(tx TxExecer, colTypes map[string]string, metas map[string]FieldMeta, table string, values map[string]any) (*WriteResult, error) {
	if we := validateRequired(metas, values, true); we != nil {
		return nil, we
	}
	if we, err := validateRules(tx, colTypes, metas, values); err != nil {
		return nil, err
	} else if we != nil {
		return nil, we
	}

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
		res, err := tx.Exec(fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", tableSQL(table)))
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
		colSQL = append(colSQL, engine.QuoteIdent(c))
		placeholders = append(placeholders, fmt.Sprintf("CAST(? AS %s)", castType(colTypes, c)))
		args = append(args, values[c])
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableSQL(table), strings.Join(colSQL, ", "), strings.Join(placeholders, ", "))
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
func UpdateRowTx(tx TxExecer, colTypes map[string]string, metas map[string]FieldMeta, table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
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
		setClauses = append(setClauses, fmt.Sprintf("%s = CAST(? AS %s)", engine.QuoteIdent(c), castType(colTypes, c)))
		args = append(args, values[c])
	}
	args = append(args, pkVal)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = CAST(? AS %s)",
		tableSQL(table), strings.Join(setClauses, ", "), engine.QuoteIdent(pkCol), castType(colTypes, pkCol))
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
		tableSQL(table), engine.QuoteIdent(pkCol), castType(colTypes, pkCol))
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
