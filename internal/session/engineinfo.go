package session

import (
	"context"
	"database/sql"
	"sort"
)

// RowScanner is what both *sql.Rows and *Rows offer.
type RowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
}

// QueryFunc runs a query and returns its rows. FromSession and FromDB adapt a
// Session and a *sql.DB, so the engine-info helpers below serve the CLI (which
// holds a Session) and Studio (which probes a throwaway database).
type QueryFunc func(ctx context.Context, query string, args ...any) (RowScanner, error)

// FromSession adapts s.Query.
func FromSession(s Session) QueryFunc {
	return func(ctx context.Context, query string, args ...any) (RowScanner, error) {
		r, err := s.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return r, nil
	}
}

// FromDB adapts db.QueryContext.
func FromDB(db *sql.DB) QueryFunc {
	return func(ctx context.Context, query string, args ...any) (RowScanner, error) {
		r, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return r, nil
	}
}

// EngineVersion is the DuckDB version ("" when it cannot be read: best effort).
func EngineVersion(ctx context.Context, q QueryFunc) string {
	version := ""
	if rows, err := q(ctx, "SELECT version()"); err == nil {
		if rows.Next() {
			_ = rows.Scan(&version)
		}
		_ = rows.Close()
	}
	return version
}

// InstalledExtensions lists the extensions the engine has downloaded or
// linked, sorted by name. A failed query returns an empty slice: best effort.
func InstalledExtensions(ctx context.Context, q QueryFunc) []string {
	out := []string{}
	rows, err := q(ctx, "SELECT extension_name FROM duckdb_extensions() WHERE installed AND install_mode <> 'STATICALLY_LINKED' ORDER BY 1")
	if err != nil {
		return out
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// ExtensionsInstalled says, for each of names, whether the engine has that
// extension installed (an alias counts). Best effort: a failed query reports
// false.
func ExtensionsInstalled(ctx context.Context, q QueryFunc, names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		ok := false
		if rows, err := q(ctx, `SELECT coalesce(bool_or(installed), false) FROM duckdb_extensions() WHERE extension_name = $1 OR list_contains(aliases, $1)`, n); err == nil {
			if rows.Next() {
				if rows.Scan(&ok) != nil {
					ok = false
				}
			}
			_ = rows.Close()
		}
		out[n] = ok
	}
	return out
}
