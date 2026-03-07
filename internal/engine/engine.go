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
	if m.duckdb == nil {
		return nil, fmt.Errorf("duckdb engine is not available")
	}

	m.logger.DebugContext(ctx, "executing query",
		slog.String("engine", "duckdb"),
		slog.String("query", query),
	)

	rs, err := m.duckdb.Execute(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query on duckdb: %w", err)
	}

	m.logger.DebugContext(ctx, "query completed",
		slog.String("engine", "duckdb"),
		slog.Int("rows", rs.RowCount()),
	)

	return rs, nil
}
