package session

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"
)

// openDSN opens an in-memory DuckDB with the given DSN query (e.g. "?a=b").
//
// Default-tag tests must be offline-safe and must not touch $HOME, so unless
// the caller says otherwise: no autoinstall/autoload and a temp extension_directory.
func openDSN(t testing.TB, dsn string) *sql.DB {
	t.Helper()
	dsn = withOfflineDefaults(t, dsn)
	c, err := duckdb.NewConnector(dsn, nil)
	require.NoError(t, err, "open %q", dsn)
	db := sql.OpenDB(c)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func withOfflineDefaults(t testing.TB, dsn string) string {
	t.Helper()
	q, err := url.ParseQuery(strings.TrimPrefix(dsn, "?"))
	require.NoError(t, err)
	for k, v := range map[string]string{
		"autoinstall_known_extensions": "false",
		"autoload_known_extensions":    "false",
	} {
		if q.Get(k) == "" {
			q.Set(k, v)
		}
	}
	if q.Get("extension_directory") == "" {
		q.Set("extension_directory", t.TempDir())
	}
	return "?" + q.Encode()
}

// setting returns current_setting(name) as text.
func setting(t testing.TB, db *sql.DB, name string) string {
	t.Helper()
	var v sql.NullString
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT CAST(current_setting('%s') AS VARCHAR)", name)).Scan(&v))
	return v.String
}

// scalar runs a query returning one value rendered as text.
func scalar(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, q string) (string, error) {
	var v sql.NullString
	err := db.QueryRowContext(ctx, "SELECT CAST(("+q+") AS VARCHAR)").Scan(&v)
	return v.String, err
}

// rows runs a query and returns each row as a "|"-joined string.
func rows(db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, q string) ([]string, error) {
	r, err := db.QueryContext(context.Background(), q)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	cols, _ := r.Columns()
	var out []string
	for r.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := r.Scan(ptrs...); err != nil {
			return out, err
		}
		parts := make([]string, len(cols))
		for i, v := range vals {
			parts[i] = fmt.Sprint(v)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out, r.Err()
}

// tryOpen is openDSN without the testing dependency, returning the error.
func tryOpen(dsn string) (*sql.DB, error) {
	for _, k := range []string{"autoinstall_known_extensions", "autoload_known_extensions"} {
		if !strings.Contains(dsn, k+"=") {
			if dsn == "" {
				dsn = "?"
			} else {
				dsn += "&"
			}
			dsn += k + "=false"
		}
	}
	c, err := duckdb.NewConnector(dsn, nil)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(c)
	// Force a real connection so config errors surface.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
