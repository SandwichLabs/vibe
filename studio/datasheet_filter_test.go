package main

// TDD RED — Phase 2 / unit P2.5: "Datasheet filter-spec -> SQL backend
// (Filter-by-Selection / Filter-by-Form)" (parity plan §2.3).
//
// This is a REAL integration test: it opens a real encrypted .vibe container,
// materializes DuckDB, seeds a typed table, and drives the production
// Wails-bound retrieval path (GetTableDataFiltered) with a structured
// FilterSpec. No mocks; no hand-written SQL string from the caller.
//
// It asserts the acceptance behavior end-to-end:
//   - a FilterSpec is translated to a PARAMETERIZED WHERE clause; values are
//     bound (a value containing a single quote matches literally and does not
//     break or inject into the query)
//   - operators eq/neq/lt/gt/lte/gte/like/is_null/is_not_null are supported
//   - conditions combine with AND, plus OR-grouping
//   - filtered rows are returned together with an accurate total-row count
//     (the record-count bar) independent of pagination
//   - an unknown column identifier in the spec is rejected with a wrapped error
//     (validated against the table's actual columns — no SQL injection via ident)

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTicketsTable creates a typed table that exercises every operator path,
// including a NULL assignee, a literal single-quote in a value, and a numeric
// amount column for range operators.
func seedTicketsTable(t *testing.T, app *App) {
	t.Helper()
	ddb := app.sess.DB()
	require.NotNil(t, ddb)

	_, err := ddb.Exec(`CREATE TABLE tickets (
		id INTEGER PRIMARY KEY,
		status VARCHAR,
		assignee VARCHAR,
		amount DECIMAL(10,2)
	);`)
	require.NoError(t, err, "create typed tickets table")

	_, err = ddb.Exec(`INSERT INTO tickets (id, status, assignee, amount) VALUES
		(1, 'open',   'alice',     150.00),
		(2, 'open',   'bob',        50.00),
		(3, 'closed', 'alice',     200.00),
		(4, 'open',   NULL,        300.00),
		(5, 'open',   'O''Brien',  120.00),
		(6, 'open',   'carol',     999.00);`)
	require.NoError(t, err, "seed tickets rows")
}

// findCol returns the index of a column name in a TableData result.
func findCol(t *testing.T, td *TableData, name string) int {
	t.Helper()
	for i, c := range td.Columns {
		if c == name {
			return i
		}
	}
	t.Fatalf("column %q not found in result columns %v", name, td.Columns)
	return -1
}

// TestGetTableDataFiltered_AndConditions is the core acceptance case:
// Filter-by-Form generating [status eq 'open'] AND [amount gte 100].
func TestGetTableDataFiltered_AndConditions(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{
			{Column: "status", Op: "eq", Value: "open"},
			{Column: "amount", Op: "gte", Value: "100"},
		},
	}

	td, err := app.GetTableDataFiltered("tickets", spec, 1, 50, "id", "ASC")
	require.NoError(t, err, "valid filter spec must execute")
	require.NotNil(t, td)

	// Matching ids: 1 (open,150), 4 (open,300), 5 (open,120), 6 (open,999).
	// Excluded: 2 (amount<100), 3 (closed).
	idIdx := findCol(t, td, "id")
	var gotIDs []int64
	for _, row := range td.Rows {
		switch v := row[idIdx].(type) {
		case int64:
			gotIDs = append(gotIDs, v)
		case int32:
			gotIDs = append(gotIDs, int64(v))
		case int:
			gotIDs = append(gotIDs, int64(v))
		default:
			t.Fatalf("unexpected id type %T", row[idIdx])
		}
	}
	assert.ElementsMatch(t, []int64{1, 4, 5, 6}, gotIDs, "only AND-matching rows returned")
	assert.Equal(t, 4, td.TotalRows, "total count reflects the filter, not the whole table")
}

// TestGetTableDataFiltered_LikeWildcard exercises the LIKE operator with a
// wildcard, driving Filter-by-Selection style partial matching.
func TestGetTableDataFiltered_LikeWildcard(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{
			{Column: "assignee", Op: "like", Value: "a%"}, // alice
		},
	}
	td, err := app.GetTableDataFiltered("tickets", spec, 1, 50, "id", "ASC")
	require.NoError(t, err)
	assert.Equal(t, 2, td.TotalRows, "LIKE 'a%%' matches both alice rows (ids 1,3)")
}

// TestGetTableDataFiltered_IsNull exercises IS NULL / IS NOT NULL (value-less
// operators that must NOT bind a parameter).
func TestGetTableDataFiltered_IsNull(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	nullSpec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{{Column: "assignee", Op: "is_null"}},
	}
	td, err := app.GetTableDataFiltered("tickets", nullSpec, 1, 50, "id", "ASC")
	require.NoError(t, err)
	assert.Equal(t, 1, td.TotalRows, "exactly one row (id 4) has a NULL assignee")

	notNullSpec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{{Column: "assignee", Op: "is_not_null"}},
	}
	td, err = app.GetTableDataFiltered("tickets", notNullSpec, 1, 50, "id", "ASC")
	require.NoError(t, err)
	assert.Equal(t, 5, td.TotalRows, "the other five rows have an assignee")
}

// TestGetTableDataFiltered_OrGroup exercises OR combination of conditions.
func TestGetTableDataFiltered_OrGroup(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "OR",
		Conditions: []FilterCondition{
			{Column: "status", Op: "eq", Value: "closed"}, // id 3
			{Column: "amount", Op: "gt", Value: "900"},    // id 6
		},
	}
	td, err := app.GetTableDataFiltered("tickets", spec, 1, 50, "id", "ASC")
	require.NoError(t, err)
	assert.Equal(t, 2, td.TotalRows, "OR-group matches the union (ids 3 and 6)")
}

// TestGetTableDataFiltered_BoundParameters proves values are bound, not
// interpolated: a value containing a single quote must match LITERALLY and must
// not break the query (no SQL injection via values).
func TestGetTableDataFiltered_BoundParameters(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{
			{Column: "assignee", Op: "eq", Value: "O'Brien"},
		},
	}
	td, err := app.GetTableDataFiltered("tickets", spec, 1, 50, "id", "ASC")
	require.NoError(t, err, "a quote in a value must be safely bound, not break the query")
	assert.Equal(t, 1, td.TotalRows, "the literal O'Brien row (id 5) matches exactly")

	idIdx := findCol(t, td, "id")
	require.Len(t, td.Rows, 1)
	switch v := td.Rows[0][idIdx].(type) {
	case int64:
		assert.Equal(t, int64(5), v)
	case int32:
		assert.Equal(t, int32(5), v)
	case int:
		assert.Equal(t, 5, v)
	default:
		t.Fatalf("unexpected id type %T", td.Rows[0][idIdx])
	}
}

// TestGetTableDataFiltered_RejectsUnknownColumn proves identifiers are
// validated against the table's real columns and rejected with a wrapped error.
func TestGetTableDataFiltered_RejectsUnknownColumn(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{
			{Column: "status; DROP TABLE tickets", Op: "eq", Value: "open"},
		},
	}
	_, err := app.GetTableDataFiltered("tickets", spec, 1, 50, "id", "ASC")
	require.Error(t, err, "an unknown/invalid column identifier must be rejected")
	assert.True(t,
		strings.Contains(strings.ToLower(err.Error()), "column"),
		"error should be a wrapped, column-related error, got: %v", err)

	// The table must be untouched by the rejected, would-be-injected spec.
	ddb := app.sess.DB()
	var count int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM tickets;`).Scan(&count))
	assert.Equal(t, 6, count, "rejected spec must not have mutated the table")
}
