package artifacts

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
)

// These tests pin, on the embedded DuckDB, the behaviours D3 of
// docs/plans/data-artifacts.md rests on (verified on DuckDB 1.5.6, duckdb-go
// v2.10506.0). A DuckDB bump that changes one of them must fail here, loudly,
// instead of silently changing what a bound date or limit means.

// runBound binds the query like production does (Bind, then
// engine.BindParams) and runs it on db, returning the first column as strings.
func runBound(t *testing.T, db *sql.DB, m *Manifest, query string, values map[string]any) ([]string, error) {
	t.Helper()
	text, _, params, err := Bind(m, query, values, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	rewritten, args, err := engine.BindParams(text, params)
	require.NoError(t, err)
	rows, err := db.QueryContext(context.Background(), rewritten, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v.String)
	}
	return out, rows.Err()
}

func duckManifest(t *testing.T) *Manifest {
	return mustDecode(t, `version: 1
title: DuckDB pins
parameters:
  - name: from
    type: date
    default: 2026-03-01
  - name: to
    type: date
    default: 2026-03-31
  - name: n
    type: integer
    default: "2"
  - name: txt
    type: text
    default: "5 OR 1=1"
queries:
  - name: between
    sql: SELECT strftime(d, '%Y-%m-%d') FROM t WHERE d BETWEEN $from AND $to ORDER BY d
  - name: gte
    sql: SELECT strftime(d, '%Y-%m-%d') FROM t WHERE d >= $from ORDER BY d
  - name: eq
    sql: SELECT strftime(d, '%Y-%m-%d') FROM t WHERE d = $from
  - name: bare_arith
    sql: SELECT $from - INTERVAL 1 DAY
  - name: cast_arith
    sql: SELECT ($from::DATE - INTERVAL 1 DAY)::DATE::VARCHAR
  - name: bare_trunc
    sql: SELECT date_trunc('month', $from)
  - name: cast_trunc
    sql: SELECT date_trunc('month', $from::DATE)::DATE::VARCHAR
  - name: limit
    sql: SELECT x::VARCHAR FROM range(10) r(x) ORDER BY x LIMIT $n
  - name: text_vs_int
    sql: SELECT x::VARCHAR FROM range(10) r(x) WHERE x = $txt
  - name: literal
    sql: SELECT '$txt'
  - name: orderby
    sql: SELECT x FROM range(3) r(x) ORDER BY $n
`)
}

func TestDuckDB_BoundDateBehaviour(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	_, err = db.Exec(`CREATE TABLE t AS SELECT DATE '2026-02-28' AS d UNION ALL SELECT DATE '2026-03-01' UNION ALL SELECT DATE '2026-03-15' UNION ALL SELECT DATE '2026-04-01'`)
	require.NoError(t, err)
	m := duckManifest(t)

	// An ISO string bound to ? takes DATE from a comparison.
	got, err := runBound(t, db, m, "between", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-03-01", "2026-03-15"}, got, "d BETWEEN ? AND ? compares as DATE")
	got, err = runBound(t, db, m, "gte", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-03-01", "2026-03-15", "2026-04-01"}, got)
	got, err = runBound(t, db, m, "eq", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-03-01"}, got)

	// It does not in a function or arithmetic context: the parameter is a
	// VARCHAR there, so the cast is required.
	_, err = runBound(t, db, m, "bare_arith", nil)
	require.Error(t, err, "$from - INTERVAL 1 DAY is a binder error")
	assert.Contains(t, err.Error(), "Binder Error")
	got, err = runBound(t, db, m, "cast_arith", nil)
	require.NoError(t, err, "$from::DATE - INTERVAL 1 DAY works")
	assert.Equal(t, []string{"2026-02-28"}, got)
	_, err = runBound(t, db, m, "bare_trunc", nil)
	require.Error(t, err, "date_trunc('month', $from) is a binder error")
	assert.Contains(t, err.Error(), "Binder Error")
	got, err = runBound(t, db, m, "cast_trunc", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-03-01"}, got)

	// DuckDB itself refuses what is not a real date (Bind refuses it first; this
	// pins that the engine would too).
	for _, bad := range []string{"not-a-date", "2026-02-30"} {
		_, err = db.Query(`SELECT d FROM t WHERE d = ?`, bad)
		assert.Error(t, err, bad)
	}
}

func TestDuckDB_BoundLimitAndText(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	m := duckManifest(t)

	got, err := runBound(t, db, m, "limit", nil)
	require.NoError(t, err, "LIMIT $n takes a bound int64")
	assert.Equal(t, []string{"0", "1"}, got)
	got, err = runBound(t, db, m, "limit", map[string]any{"n": 3})
	require.NoError(t, err)
	assert.Equal(t, []string{"0", "1", "2"}, got)
	_, err = db.Query(`SELECT 1 LIMIT ?`, int64(-1))
	assert.Error(t, err, "a negative limit is a binder error")

	// A value that looks like SQL, bound against an integer column, is a
	// conversion error and never SQL.
	_, err = runBound(t, db, m, "text_vs_int", nil)
	require.Error(t, err)

	// duckdb-go ignores extra args: a $name inside a string literal is rewritten
	// by BindParams to '?', one arg is bound and silently dropped, and the
	// query returns the literal "?". This is why the vet in agents repeats a
	// lexer check (step 3); the pin makes the hazard visible.
	got, err = runBound(t, db, m, "literal", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"?"}, got)

	// A parameter cannot choose a sort column.
	_, err = runBound(t, db, m, "orderby", nil)
	require.Error(t, err, "ORDER BY ? is refused by DuckDB")
}
