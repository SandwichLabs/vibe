package engine

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/session"
)

// DuckDBEngine executes queries against a DuckDB session.
type DuckDBEngine struct {
	sess session.Session
}

// NewDuckDBEngine creates a DuckDB engine over the given session.
func NewDuckDBEngine(sess session.Session) *DuckDBEngine {
	return &DuckDBEngine{
		sess: sess,
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
	return e.executeIn(ctx, "", query, params, maxRows)
}

// executeIn is execute with connection (a catalog name) as the default for
// unqualified names; "" is the session default.
func (e *DuckDBEngine) executeIn(ctx context.Context, connection, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	boundSQL, args, err := BindParams(query, params)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to bind parameters: %w", err)
	}

	var opts []session.QueryOption
	if connection != "" {
		opts = append(opts, session.WithCatalog(connection))
	}
	rows, err := e.sess.QueryWith(ctx, boundSQL, args, opts...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute duckdb query: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return ScanRows(rows.Rows, maxRows)
}

// Close is a no-op for DuckDBEngine since the db is managed externally.
func (e *DuckDBEngine) Close() error {
	return nil
}
