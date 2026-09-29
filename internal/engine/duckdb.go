package engine

import (
	"context"
	"database/sql"
	"fmt"
)

// DuckDBEngine executes queries against a DuckDB database.
type DuckDBEngine struct {
	db *sql.DB
}

// NewDuckDBEngine creates a new DuckDB engine wrapping the given database connection.
func NewDuckDBEngine(db *sql.DB) *DuckDBEngine {
	return &DuckDBEngine{
		db: db,
	}
}

// Execute runs a SQL query against the DuckDB database and returns a ResultSet.
func (e *DuckDBEngine) Execute(ctx context.Context, query string, params map[string]any) (*ResultSet, error) {
	rs, _, err := e.execute(ctx, query, params, 0)
	return rs, err
}

// ExecuteCapped runs a SQL query but only keeps the first maxRows rows in
// the returned ResultSet (maxRows <= 0 means unlimited), also returning the
// true total row count. #91: bounds the Go-side/webview copy for a query
// like `SELECT * FROM range(100000)` without a crash there — it does NOT
// bound DuckDB's own memory, since go-duckdb still materializes the full
// result natively before this ever starts scanning; a true streaming cap
// and cancel are #84's.
func (e *DuckDBEngine) ExecuteCapped(ctx context.Context, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	return e.execute(ctx, query, params, maxRows)
}

// execute is the shared implementation behind Execute and ExecuteCapped.
func (e *DuckDBEngine) execute(ctx context.Context, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	boundSQL, args, err := BindParams(query, params)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to bind parameters: %w", err)
	}

	rows, err := e.db.QueryContext(ctx, boundSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute duckdb query: %w", err)
	}
	defer rows.Close()

	return ScanRows(rows, maxRows)
}

// Close is a no-op for DuckDBEngine since the db is managed externally.
func (e *DuckDBEngine) Close() error {
	return nil
}
