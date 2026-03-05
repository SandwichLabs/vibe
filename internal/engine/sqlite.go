package engine

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
)

// SQLiteEngine executes queries against a SQLite/SQLCipher database.
type SQLiteEngine struct {
	db       *sql.DB
	readOnly bool
}

// NewSQLiteEngine creates a new SQLite engine wrapping the given database connection.
func NewSQLiteEngine(db *sql.DB, readOnly bool) *SQLiteEngine {
	return &SQLiteEngine{
		db:       db,
		readOnly: readOnly,
	}
}

// Execute runs a SQL query against the SQLite database and returns a ResultSet.
func (e *SQLiteEngine) Execute(ctx context.Context, query string, params map[string]any) (*ResultSet, error) {
	boundSQL, args, err := BindParams(query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to bind parameters: %w", err)
	}

	if e.readOnly {
		return e.executeQuery(ctx, boundSQL, args)
	}

	// Attempt as a query first (SELECT, etc). If it returns columns, it's a query.
	// Otherwise, fall back to exec for statements like INSERT/UPDATE/DELETE.
	return e.executeQuery(ctx, boundSQL, args)
}

// executeQuery runs a SQL query that returns rows.
func (e *SQLiteEngine) executeQuery(ctx context.Context, query string, args []any) (*ResultSet, error) {
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute sqlite query: %w", err)
	}
	defer rows.Close()

	return scanResultSet(rows)
}

// Close is a no-op for SQLiteEngine since the db is managed externally.
func (e *SQLiteEngine) Close() error {
	return nil
}

// scanResultSet reads all rows from a sql.Rows into a ResultSet.
func scanResultSet(rows *sql.Rows) (*ResultSet, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	columns := make([]Column, len(colTypes))
	for i, ct := range colTypes {
		columns[i] = Column{
			Name: ct.Name(),
			Type: goTypeName(ct),
		}
	}

	var resultRows [][]any
	for rows.Next() {
		scanDest := makeScanDest(len(columns))
		if err := rows.Scan(scanDest...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]any, len(columns))
		for i, v := range scanDest {
			row[i] = deref(v)
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return &ResultSet{
		Columns: columns,
		Rows:    resultRows,
	}, nil
}

// makeScanDest creates a slice of pointers to any for use with rows.Scan.
func makeScanDest(n int) []any {
	dest := make([]any, n)
	for i := range dest {
		var v any
		dest[i] = &v
	}
	return dest
}

// deref dereferences a pointer to any, returning the underlying value.
func deref(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		return rv.Elem().Interface()
	}
	return v
}

// goTypeName returns the Go type name for a sql.ColumnType.
func goTypeName(ct *sql.ColumnType) string {
	if st := ct.ScanType(); st != nil {
		return st.Name()
	}
	return "any"
}
