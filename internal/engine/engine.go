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

// Manager routes queries to the appropriate engine.
type Manager struct {
	sqlite *SQLiteEngine
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

// WithReadOnlySQLite configures the SQLite engine in read-only mode.
func WithReadOnlySQLite(readOnly bool) Option {
	return func(m *Manager) {
		if m.sqlite != nil {
			m.sqlite.readOnly = readOnly
		}
	}
}

// NewManager creates a new query Manager that routes queries to
// SQLite or DuckDB engines. The duckdbDB parameter may be nil if
// DuckDB is not available.
func NewManager(sqliteDB, duckdbDB *sql.DB, opts ...Option) *Manager {
	m := &Manager{
		sqlite: NewSQLiteEngine(sqliteDB, false),
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

// Execute runs a query on the specified engine and returns the result set.
// The engineName parameter should be "sqlite" or "duckdb".
func (m *Manager) Execute(ctx context.Context, query string, engineName string, params map[string]any) (*ResultSet, error) {
	eng, err := m.Engine(engineName)
	if err != nil {
		return nil, err
	}

	m.logger.DebugContext(ctx, "executing query",
		slog.String("engine", engineName),
		slog.String("query", query),
	)

	rs, err := eng.Execute(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query on %s: %w", engineName, err)
	}

	m.logger.DebugContext(ctx, "query completed",
		slog.String("engine", engineName),
		slog.Int("rows", rs.RowCount()),
	)

	return rs, nil
}

// Engine returns the engine with the given name.
func (m *Manager) Engine(name string) (Engine, error) {
	switch name {
	case "sqlite":
		return m.sqlite, nil
	case "duckdb":
		if m.duckdb == nil {
			return nil, fmt.Errorf("duckdb engine is not available")
		}
		return m.duckdb, nil
	default:
		return nil, fmt.Errorf("unknown engine %q: must be \"sqlite\" or \"duckdb\"", name)
	}
}
