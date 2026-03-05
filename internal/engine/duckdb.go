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
	boundSQL, args, err := BindParams(query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to bind parameters: %w", err)
	}

	rows, err := e.db.QueryContext(ctx, boundSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute duckdb query: %w", err)
	}
	defer rows.Close()

	return scanResultSet(rows)
}

// Close is a no-op for DuckDBEngine since the db is managed externally.
func (e *DuckDBEngine) Close() error {
	return nil
}
