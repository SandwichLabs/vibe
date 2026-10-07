package engine

import (
	"database/sql"
	"fmt"
	"reflect"
)

// ScanRows reads rows from a sql.Rows into a ResultSet. It is the single
// funnel for ExecuteQuery, GetTableData*, RunA2UIQuery (notebooks and A2UI
// tables/charts), run_query/preview_query, MCP, and the TUI, so a fix here
// (like DATE formatting, or the wire contract's value normalization) covers
// all of those consumers at once. It is not the ONLY code that scans a
// *sql.Rows: the CLI export/dump commands (cmd/export.go, cmd/dump.go — a
// pre-existing bug, they hit SQLite not DuckDB) and the import preview
// (importer/inspect.go, which stages everything as VARCHAR text) scan
// independently and are out of this funnel's scope.
//
// maxRows <= 0 means unlimited: every row is scanned and returned. A
// positive maxRows keeps only the first maxRows rows but keeps calling
// rows.Next() (without Scan) for the remainder, so the returned total is
// still the true row count while the Go-side ResultSet (and what reaches
// the webview) stays bounded by maxRows. This does NOT bound DuckDB's own
// memory: go-duckdb runs non-streaming PendingPrepared+ExecutePending, so
// the full result is still materialized natively before scanning ever
// starts — a true streaming cap (and cancel) is #84's.
func ScanRows(rows *sql.Rows, maxRows int) (*ResultSet, int, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get column types: %w", err)
	}

	columns := make([]Column, len(colTypes))
	dbTypes := make([]string, len(colTypes))
	for i, ct := range colTypes {
		dbTypes[i] = ct.DatabaseTypeName()
		columns[i] = Column{
			Name: ct.Name(),
			Type: dbTypes[i],
		}
	}

	var resultRows [][]any
	total := 0
	for rows.Next() {
		total++
		if maxRows > 0 && total > maxRows {
			continue
		}

		scanDest := makeScanDest(len(columns))
		if err := rows.Scan(scanDest...); err != nil {
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]any, len(columns))
		for i, v := range scanDest {
			row[i] = deref(v, dbTypes[i])
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	return &ResultSet{
		Columns: columns,
		Rows:    resultRows,
	}, total, nil
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

// deref dereferences a pointer to any, returning the underlying value
// normalized for downstream consumers (see NormalizeColumnValue).
func deref(v any, dbType string) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		return NormalizeColumnValue(rv.Elem().Interface(), dbType)
	}
	return NormalizeColumnValue(v, dbType)
}
