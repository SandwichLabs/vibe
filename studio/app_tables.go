package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/crud"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
)

// --- Tables ---

// tableRefArg validates a table identifier arriving from the frontend: a bare
// name (default catalog) or schema.table / catalog.schema.table (an attached
// connection). Every part is validated and quoted when SQL is built.
//
// The returned reference is fully resolved (catalog, schema, table) through the
// session, so the SQL built from it never depends on the connection's current
// catalog or on a same-named temp table shadowing a bare name. An unknown
// connection ("system", "temp", a typo) is refused. The caller holds a.mu and
// has checked a.ws != nil.
func (a *App) tableRefArg(name string) (session.TableRef, error) {
	ref, err := engine.ParseTable(name)
	if err != nil {
		return ref, fmt.Errorf("invalid table name")
	}
	if sess := a.sess; sess != nil {
		resolved, rerr := sess.Resolve(a.ctxOrBackground(), ref)
		if rerr != nil {
			return ref, rerr
		}
		ref = resolved
	}
	return ref, nil
}

// isDefaultTarget reports whether ref is in the session's default catalog and
// default schema (where a bare sequence name resolves).
func (a *App) isDefaultTarget(ref session.TableRef) bool {
	sess := a.sess
	if sess == nil {
		return false
	}
	schema, err := sess.DefaultSchema(a.ctxOrBackground(), "")
	return err == nil && ref.Catalog == sess.DefaultCatalog() && ref.Schema == schema
}

// ListTables returns all user tables of the default catalog (non _vibe_ tables).
func (a *App) ListTables() ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	sess := a.sess
	if sess == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	tabs, err := sess.ListTables(a.ctxOrBackground(), "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	var tables []string
	for _, t := range tabs {
		tables = append(tables, t.Name)
	}
	return tables, nil
}

// ColumnInfo describes a table column.
type ColumnInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"not_null"`
	PK      bool   `json:"pk"`
	// FK is read-only output from DescribeTable (see engine.ColumnInfo.FK);
	// CreateTable/AddColumn ignore it on input.
	FK bool `json:"fk"`
}

// TableInfo describes a table's schema.
type TableInfo struct {
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns"`
}

// DescribeTable returns schema info for a table.
func (a *App) DescribeTable(name string) (*TableInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	tref, err := a.tableRefArg(name)
	if err != nil {
		return nil, err
	}

	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	// Shared introspection (internal/schema): fills real PRIMARY KEY membership
	// from duckdb_constraints() — the previous inline query always reported
	// PK=false, leaving the schema tree / ER diagram to guess by column name.
	cols, err := engine.TableColumns(db, tref)
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	info := &TableInfo{Name: name}
	for _, c := range cols {
		info.Columns = append(info.Columns, ColumnInfo{
			Name:    c.Name,
			Type:    c.Type,
			NotNull: c.NotNull,
			PK:      c.PK,
			FK:      c.FK,
		})
	}
	return info, nil
}

// TableData holds paginated table data.
type TableData struct {
	Columns []string `json:"columns"`
	// Types is the DuckDB type name of each column, parallel to Columns
	// — the datasheet needs it to pick an editor (date input vs
	// text), align/format cells, and round-trip a DECIMAL's scale.
	Types      []string `json:"types"`
	Rows       [][]any  `json:"rows"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalRows  int      `json:"total_rows"`
	TotalPages int      `json:"total_pages"`
}

// GetTableData returns paginated data from a table.
func (a *App) GetTableData(name string, page, pageSize int, sortCol, sortDir string) (*TableData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}

	tref, err := a.tableRefArg(name)
	if err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	var totalRows int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tref.SQL())).Scan(&totalRows); err != nil {
		return nil, fmt.Errorf("failed to count rows: %w", err)
	}

	orderClause := ""
	if sortCol != "" && engine.ValidIdentifier(sortCol) {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", session.QuoteIdent(sortCol), dir)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %s%s LIMIT ? OFFSET ?", tref.SQL(), orderClause)
	rows, err := db.Query(query, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	rs, _, err := engine.ScanRows(rows, 0)
	if err != nil {
		return nil, err
	}
	columns, types := rs.NamesAndTypes()

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	result := &TableData{
		Columns:    columns,
		Types:      types,
		Rows:       rs.Rows,
		Page:       page,
		PageSize:   pageSize,
		TotalRows:  totalRows,
		TotalPages: totalPages,
	}
	if err := jsonRoundTripGuard(result); err != nil {
		return nil, err
	}
	return result, nil
}

// FilterCondition and FilterSpec are the structured datasheet / QBE filter
// shapes (plan §2.3), defined once in internal/queries with the same JSON.
type (
	FilterCondition = queries.FilterCondition
	FilterSpec      = queries.FilterSpec
)

// GetTableDataFiltered returns paginated, structured-filter-driven table data
// plus an accurate total-row count for the record-count bar (plan §2.3, P2.5).
//
// Translation rules:
//   - each condition's column is validated against the table's real columns
//     (case-sensitive ident allowlist) — no SQL injection via identifier
//   - values are bound as parameters (?), never interpolated — no injection via
//     value (a single quote matches literally)
//   - is_null / is_not_null are value-less and bind no parameter
//   - conditions join with the spec combinator (AND/OR; defaults to AND)
func (a *App) GetTableDataFiltered(name string, spec FilterSpec, page, pageSize int, sortCol, sortDir string) (*TableData, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 50
	}

	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	// Build an allowlist of real columns to validate filter/sort identifiers.
	cols, err := engine.TableColumns(db, tref)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect columns for filter: %w", err)
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c.Name] = true
	}

	whereClause, args, err := buildFilterWhere(spec, known)
	if err != nil {
		return nil, err
	}

	var totalRows int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", tref.SQL(), whereClause)
	if err := db.QueryRow(countQuery, args...).Scan(&totalRows); err != nil {
		return nil, fmt.Errorf("failed to count filtered rows: %w", err)
	}

	orderClause := ""
	if sortCol != "" && engine.ValidIdentifier(sortCol) && known[sortCol] {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", session.QuoteIdent(sortCol), dir)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %s%s%s LIMIT ? OFFSET ?", tref.SQL(), whereClause, orderClause)
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query filtered table: %w", err)
	}
	defer rows.Close()

	rs, _, err := engine.ScanRows(rows, 0)
	if err != nil {
		return nil, err
	}
	columns, types := rs.NamesAndTypes()

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	result := &TableData{
		Columns:    columns,
		Types:      types,
		Rows:       rs.Rows,
		Page:       page,
		PageSize:   pageSize,
		TotalRows:  totalRows,
		TotalPages: totalPages,
	}
	if err := jsonRoundTripGuard(result); err != nil {
		return nil, err
	}
	return result, nil
}

// buildFilterWhere translates a FilterSpec into a parameterized WHERE clause
// (leading " WHERE ...", or "" when there are no conditions) plus its bound
// args. Column identifiers are validated against the known-columns allowlist;
// values are always bound, never interpolated.
func buildFilterWhere(spec FilterSpec, known map[string]bool) (string, []any, error) {
	if len(spec.Conditions) == 0 {
		return "", nil, nil
	}

	combinator := strings.ToUpper(strings.TrimSpace(spec.Combinator))
	if combinator != "OR" {
		combinator = "AND"
	}

	var clauses []string
	var args []any
	for _, c := range spec.Conditions {
		if !known[c.Column] {
			return "", nil, fmt.Errorf("unknown filter column %q", c.Column)
		}
		switch c.Op {
		case "is_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NULL", session.QuoteIdent(c.Column)))
		case "is_not_null":
			clauses = append(clauses, fmt.Sprintf("%s IS NOT NULL", session.QuoteIdent(c.Column)))
		default:
			sqlOp, ok := queries.SQLOperator(c.Op)
			if !ok {
				return "", nil, fmt.Errorf("unsupported filter operator %q", c.Op)
			}
			clauses = append(clauses, fmt.Sprintf("%s %s ?", session.QuoteIdent(c.Column), sqlOp))
			args = append(args, c.Value)
		}
	}

	return " WHERE " + strings.Join(clauses, " "+combinator+" "), args, nil
}

// InsertRow inserts a row into a table, coercing all-string form inputs to the
// column's type (via information_schema). Constraints (NOT NULL, CHECK, keys)
// come from the database itself; a violation is returned as a field-keyed
// *crud.WriteError where it can be attributed, so the UI can highlight the offending
// controls instead of surfacing a raw SQL string.
func (a *App) InsertRow(table string, values map[string]any) (*crud.WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ddb, colTypes, table, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := crud.InsertRowTx(tx, colTypes, table, values)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit insert: %w", err)
	}
	return res, nil
}

// UpdateRow updates a row in a table by primary key, applying the same coercion
// and validation as InsertRow. A 0-affected-row outcome (PK mismatch) is
// surfaced as an error rather than a silent no-op.
func (a *App) UpdateRow(table, pkCol string, pkVal any, values map[string]any) (*crud.WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !engine.ValidIdentifier(pkCol) {
		return nil, fmt.Errorf("invalid primary key column")
	}

	ddb, colTypes, table, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := crud.UpdateRowTx(tx, colTypes, table, pkCol, pkVal, values)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit update: %w", err)
	}
	return res, nil
}

// prepareWrite validates the open container + table and loads the column types
// needed by the transactional write helpers.
func (a *App) prepareWrite(table string) (*sql.DB, map[string]string, string, error) {
	if err := a.requireWorkspace(); err != nil {
		return nil, nil, "", err
	}
	tref, err := a.tableRefArg(table)
	if err != nil {
		return nil, nil, "", err
	}
	ddb := a.sess.DB()
	if ddb == nil {
		return nil, nil, "", fmt.Errorf("DuckDB not available")
	}
	// Writes address the table by its fully resolved name (catalog.schema.table),
	// so neither the connection's current catalog nor a same-named temp table
	// can redirect them.
	resolved := tref.String()
	colTypes, err := crud.ColumnTypes(ddb, resolved)
	if err != nil {
		return nil, nil, "", err
	}
	return ddb, colTypes, resolved, nil
}

// DeleteRow deletes a row from a table by primary key. Consistent with
// InsertRow/UpdateRow, it validates the pk column identifier, runs the DELETE
// inside a transaction with rollback on error, coerces pkVal to the PK column's
// type, and surfaces a 0-affected-row outcome (PK mismatch) as a structured
// *crud.WriteError rather than a raw SQL string.
func (a *App) DeleteRow(table, pkCol string, pkVal any) (*crud.WriteResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !engine.ValidIdentifier(pkCol) {
		return nil, fmt.Errorf("invalid primary key column")
	}

	ddb, colTypes, table, err := a.prepareWrite(table)
	if err != nil {
		return nil, err
	}

	tx, err := ddb.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := crud.DeleteRowTx(tx, colTypes, table, pkCol, pkVal)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit delete: %w", err)
	}
	return res, nil
}

// CreateTable creates a new table with the given columns. Integer primary-key
// columns are made auto-numbering via a backing sequence (the Access AutoNumber
// behavior) so data-entry forms can omit the id.
func (a *App) CreateTable(name string, columns []ColumnInfo) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return err
	}
	for _, col := range columns {
		if !engine.ValidIdentifier(col.Name) {
			return fmt.Errorf("invalid column name %q", col.Name)
		}
	}

	var colDefs, seqDDL []string
	for _, col := range columns {
		def := fmt.Sprintf("%s %s", session.QuoteIdent(col.Name), col.Type)
		if col.PK && engine.IsIntegerType(col.Type) {
			// The sequence lives next to its table, in the same catalog and
			// schema. A bare table keeps the legacy nextval('seq') default text.
			seq := engine.AutoNumberSequence(tref.Name, col.Name)
			seqRef := session.TableRef{Catalog: tref.Catalog, Schema: tref.Schema, Name: seq}
			seqDDL = append(seqDDL, fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s START 1", seqRef.SQL()))
			if a.isDefaultTarget(tref) {
				def += fmt.Sprintf(" DEFAULT nextval('%s')", seq)
			} else {
				def += fmt.Sprintf(" DEFAULT nextval(%s)", session.QuoteLiteral(seqRef.SQL()))
			}
		}
		if col.PK {
			def += " PRIMARY KEY"
		}
		if col.NotNull {
			def += " NOT NULL"
		}
		colDefs = append(colDefs, def)
	}

	query := fmt.Sprintf("CREATE TABLE %s (%s)", tref.SQL(), strings.Join(colDefs, ", "))
	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	for _, ddl := range seqDDL {
		if _, err := ddb.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create id sequence: %w", err)
		}
	}
	_, err = ddb.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	a.emitEvent("data:changed")
	return nil
}

// DropTable drops a table.
func (a *App) DropTable(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.requireWorkspace(); err != nil {
		return err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return err
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return fmt.Errorf("DuckDB not available")
	}
	_, err = ddb.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tref.SQL()))
	if err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}
	a.emitEvent("data:changed")
	return nil
}

// SaveFilterAsQuery renders a FilterSpec into deterministic SQL and persists it
// as a named query via the existing queries store, returning the new query id
// (plan §2.4, P2.6). It reuses the same rendering path as generateFilterSQL so the
// stored SQL equals the previewed SQL.
func (a *App) SaveFilterAsQuery(name string, spec FilterSpec, columns []string, title string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sqlStr, err := a.generateFilterSQL(name, spec, columns)
	if err != nil {
		return "", err
	}

	q := &queries.Query{
		Title:      title,
		SQL:        sqlStr,
		Engine:     "duckdb",
		Parameters: []queries.Param{},
	}
	if err := a.ws.Queries().Create(a.ctxOrBackground(), q); err != nil {
		return "", fmt.Errorf("failed to save filter as query: %w", err)
	}
	a.logger.Info("saved filter as named query", "id", q.ID, "table", name, "title", title)
	return q.ID, nil
}

// generateFilterSQL is the single, deterministic FilterSpec → SQL renderer behind
// both generateFilterSQL and SaveFilterAsQuery. Unlike GetTableDataFiltered (which
// binds parameters at execution time), a saved query is a standalone SQL string,
// so literal values are safely inlined here. Column identifiers (filter columns
// and the optional projection) are validated against the table's real columns —
// no SQL injection via identifier or value. The caller must hold a.mu.
func (a *App) generateFilterSQL(name string, spec FilterSpec, columns []string) (string, error) {
	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	tref, err := a.tableRefArg(name)
	if err != nil {
		return "", err
	}

	db := a.sess.DB()
	if db == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	cols, err := engine.TableColumns(db, tref)
	if err != nil {
		return "", fmt.Errorf("failed to introspect columns for filter: %w", err)
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c.Name] = true
	}

	return queries.BuildFilterSQLFrom(tref.SQL(), spec, columns, known)
}
