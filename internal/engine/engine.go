package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// Engine executes queries against a specific database backend.
type Engine interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*ResultSet, error)
	Close() error
}

// Manager routes queries to DuckDB.
type Manager struct {
	duckdb *DuckDBEngine
	logger *slog.Logger
}

// Option configures a Manager.
type Option func(*Manager)

// WithLogger sets the logger for the Manager.
func WithLogger(logger *slog.Logger) Option {
	return func(m *Manager) {
		m.logger = logger
	}
}

// NewManager creates a new query Manager backed by DuckDB.
// The duckdbDB parameter may be nil if DuckDB is not available.
func NewManager(duckdbDB *sql.DB, opts ...Option) *Manager {
	m := &Manager{
		logger: slog.Default(),
	}

	if duckdbDB != nil {
		m.duckdb = NewDuckDBEngine(duckdbDB)
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Execute runs a query on DuckDB and returns the result set.
func (m *Manager) Execute(ctx context.Context, query string, params map[string]any) (*ResultSet, error) {
	rs, _, err := m.execute(ctx, query, params, 0)
	return rs, err
}

// ExecuteCapped runs a query on DuckDB but only keeps the first maxRows
// rows in the Go-side result (maxRows <= 0 means unlimited), also
// returning the true total row count. See DuckDBEngine.ExecuteCapped's doc
// comment: this bounds the Go/webview copy, not DuckDB's own memory.
func (m *Manager) ExecuteCapped(ctx context.Context, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	return m.execute(ctx, query, params, maxRows)
}

// execute is the shared implementation behind Execute and ExecuteCapped: one
// place for the "duckdb engine is not available" guard, the wrap text, and
// the debug logging.
func (m *Manager) execute(ctx context.Context, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	if m.duckdb == nil {
		return nil, 0, fmt.Errorf("duckdb engine is not available")
	}

	m.logger.DebugContext(ctx, "executing query",
		slog.String("engine", "duckdb"),
		slog.String("query", query),
	)

	rs, total, err := m.duckdb.ExecuteCapped(ctx, query, params, maxRows)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute query on duckdb: %w", err)
	}

	m.logger.DebugContext(ctx, "query completed",
		slog.String("engine", "duckdb"),
		slog.Int("rows", rs.RowCount()),
	)

	return rs, total, nil
}
