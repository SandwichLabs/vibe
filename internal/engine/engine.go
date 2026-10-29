// Package engine runs SQL against a DuckDB connection and normalises the
// result: column metadata, typed values, parameters, row caps and table reads.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// Engine executes queries against a specific database backend.
type Engine interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*ResultSet, error)
	Close() error
}

// Manager is the query layer over a session.Session (the DuckDB hub). Every
// query runs through the session, so it is cancellable, timeout-bound and
// catalog-aware: a notebook cell or binding can name the connection whose
// catalog is the default for its unqualified names.
type Manager struct {
	sess   session.Session
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

// NewManager creates a new query Manager over a session. sess may be nil if
// DuckDB is not available.
func NewManager(sess session.Session, opts ...Option) *Manager {
	m := &Manager{
		logger: slog.Default(),
	}

	if sess != nil {
		m.sess = sess
		m.duckdb = NewDuckDBEngine(sess)
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// ExecuteIn runs a query with connection (a catalog name; "" = the session
// default) as the default for unqualified names. It satisfies
// agents.ConnectionEngine.
func (m *Manager) ExecuteIn(ctx context.Context, connection, query string, params map[string]any) (*ResultSet, error) {
	rs, _, err := m.execute(ctx, connection, query, params, 0)
	return rs, err
}

// Session returns the session the Manager runs on (nil when unavailable).
func (m *Manager) Session() session.Session { return m.sess }

// Execute runs a query on DuckDB and returns the result set.
func (m *Manager) Execute(ctx context.Context, query string, params map[string]any) (*ResultSet, error) {
	rs, _, err := m.execute(ctx, "", query, params, 0)
	return rs, err
}

// ExecuteCapped runs a query on DuckDB but only keeps the first maxRows
// rows in the Go-side result (maxRows <= 0 means unlimited), also
// returning the true total row count. See DuckDBEngine.ExecuteCapped's doc
// comment: this bounds the Go/webview copy, not DuckDB's own memory.
func (m *Manager) ExecuteCapped(ctx context.Context, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	return m.execute(ctx, "", query, params, maxRows)
}

// ExecuteCappedIn is ExecuteCapped with the named connection (catalog) as the
// default for unqualified names, on a pinned connection (notebook format v2:
// a cell's optional "connection"). An empty connection means the session
// default.
func (m *Manager) ExecuteCappedIn(ctx context.Context, connection, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	return m.execute(ctx, connection, query, params, maxRows)
}

// ExecuteOn is ExecuteCappedIn on a connection the caller holds across
// statements (the console): the same lazy attach, parameter binding, wildcard
// expansion, logging and error shaping, on conn instead of a connection pinned
// per call. connection is the catalog unqualified names resolve against on
// conn (the lazy attach needs it; a USE on conn moves it).
func (m *Manager) ExecuteOn(ctx context.Context, conn *session.Conn, connection, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	return m.run(ctx, connection, query, func() (*ResultSet, int, error) {
		return m.duckdb.executeOn(ctx, conn, query, params, maxRows)
	})
}

// execute is the shared implementation behind Execute and ExecuteCapped.
func (m *Manager) execute(ctx context.Context, connection, query string, params map[string]any, maxRows int) (*ResultSet, int, error) {
	return m.run(ctx, connection, query, func() (*ResultSet, int, error) {
		return m.duckdb.executeIn(ctx, connection, query, params, maxRows)
	})
}

// run is the one run path behind every Manager entry point: the "duckdb engine
// is not available" guard, the debug logging, the lazy attach, the
// missing-catalog substitution and the wrap text. exec is the only part that
// differs: which connection the statement runs on.
func (m *Manager) run(ctx context.Context, connection, query string, exec func() (*ResultSet, int, error)) (*ResultSet, int, error) {
	if m.duckdb == nil {
		return nil, 0, fmt.Errorf("duckdb engine is not available")
	}

	// This logs statement text. Credential DDL (CREATE SECRET, #114) must never
	// come through here: it runs on session.CreateTemporarySecret's dedicated,
	// non-logging path, and the agent SQL policy refuses CREATE SECRET.
	m.logger.DebugContext(ctx, "executing query",
		slog.String("engine", "duckdb"),
		slog.String("query", query),
	)

	// Lazy attach: a remote connection the statement names (pg.public.orders)
	// is dialled now, not when the workspace opened. A failure to connect is
	// not fatal by itself (the statement may not need that connection); only
	// when the statement then fails because a catalog or table is missing is
	// the connection's own, redacted problem reported instead of "catalog not
	// found".
	var connectErr error
	if m.sess != nil {
		connectErr = m.sess.ConnectReferenced(ctx, connection, query)
	}

	rs, total, err := exec()
	if err != nil && connectErr != nil && looksLikeMissingCatalog(err) {
		return nil, 0, connectErr
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute query on duckdb: %w", err)
	}

	m.logger.DebugContext(ctx, "query completed",
		slog.String("engine", "duckdb"),
		slog.Int("rows", rs.RowCount()),
	)

	return rs, total, nil
}

// looksLikeMissingCatalog reports a "no such catalog / table / schema" failure,
// the shape a statement takes when the connection it names is not attached.
func looksLikeMissingCatalog(err error) bool {
	m := strings.ToLower(err.Error())
	return (strings.Contains(m, "does not exist") && (strings.Contains(m, "catalog") || strings.Contains(m, "table with name") || strings.Contains(m, "schema with name"))) ||
		strings.Contains(m, "catalog error") || strings.Contains(m, "unknown connection") || strings.Contains(m, "not attached")
}
