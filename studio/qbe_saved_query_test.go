package main

// TDD RED — Phase 2 / unit P2.6: "Save filter/QBE as named query (queries
// registry integration)" (parity plan §2.4 — "save it as a named query").
//
// This is a REAL integration test: it opens a real workspace,
// materializes DuckDB, seeds a typed table, builds a structured FilterSpec
// (the P2.5 output of Filter-by-Form / the QBE grid), and drives the production
// Wails-bound persistence path. No mocks; no parallel query store.
//
// Acceptance behavior asserted end-to-end:
//   - generateFilterSQL renders a FilterSpec (+ optional selected columns) to a
//     deterministic SQL string WITHOUT saving anything to the registry
//   - SaveFilterAsQuery renders the same deterministic SQL and persists it via
//     the EXISTING queries store (the same store ListQueries/GetQuery read)
//   - the saved query, when executed via ExecuteQuery, returns rows equivalent
//     to the live GetTableDataFiltered call for the same spec
//   - the persisted SQL round-trips across close + reopen

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filteredIDs runs GetTableDataFiltered and returns the matching id column
// values as a sorted []int64, so it can be compared against the saved-query
// execution regardless of row ordering.
func filteredIDs(t *testing.T, app *App, spec FilterSpec) []int64 {
	t.Helper()
	td, err := app.GetTableDataFiltered("tickets", spec, 1, 1000, "id", "ASC")
	require.NoError(t, err)
	idIdx := findCol(t, td, "id")
	ids := toInt64s(t, td.Rows, idIdx)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// queryResultIDs runs a saved SQL string via ExecuteQuery and returns the id
// column values as a sorted []int64.
func queryResultIDs(t *testing.T, app *App, sqlStr string) []int64 {
	t.Helper()
	res, err := app.ExecuteQuery(sqlStr)
	require.NoError(t, err, "stored query SQL must execute cleanly")
	idIdx := -1
	for i, c := range res.Columns {
		if c.Name == "id" {
			idIdx = i
		}
	}
	require.GreaterOrEqual(t, idIdx, 0, "query result must include the id column")
	ids := toInt64s(t, res.Rows, idIdx)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func toInt64s(t *testing.T, rows [][]any, idx int) []int64 {
	t.Helper()
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		switch v := row[idx].(type) {
		case int64:
			out = append(out, v)
		case int32:
			out = append(out, int64(v))
		case int:
			out = append(out, int64(v))
		default:
			t.Fatalf("unexpected id type %T", row[idx])
		}
	}
	return out
}

// TestGenerateFilterSQL_NoSaveSideEffect proves the preview path generates SQL
// deterministically and does NOT write anything to the queries registry.
func TestGenerateFilterSQL_NoSaveSideEffect(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{
			{Column: "status", Op: "eq", Value: "open"},
			{Column: "amount", Op: "gte", Value: "100"},
		},
	}

	sqlStr, err := app.generateFilterSQL("tickets", spec, nil)
	require.NoError(t, err, "preview must render SQL without error")
	require.NotEmpty(t, sqlStr, "preview must return a non-empty SQL string")

	// Deterministic: calling preview again yields the identical string.
	sqlStr2, err := app.generateFilterSQL("tickets", spec, nil)
	require.NoError(t, err)
	assert.Equal(t, sqlStr, sqlStr2, "generated SQL must be deterministic")

	// The previewed SQL must be runnable and reference the table.
	assert.Contains(t, sqlStr, "tickets", "generated SQL must select from the table")
	require.NoError(t, func() error { _, e := app.ExecuteQuery(sqlStr); return e }(),
		"previewed SQL must be valid, executable DuckDB")

	// Preview must NOT have persisted anything to the registry.
	qs, err := app.ListQueries()
	require.NoError(t, err)
	assert.Empty(t, qs, "preview must have no save side effect on the registry")
}

// TestSaveFilterAsQuery_PersistsAndRoundTrips is the core acceptance case:
// a FilterSpec is saved as a named query via the existing registry, is listed
// and retrievable, executes to rows equivalent to the live filter, and survives
// a close + reopen.
func TestSaveFilterAsQuery_PersistsAndRoundTrips(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{
			{Column: "status", Op: "eq", Value: "open"},
			{Column: "amount", Op: "gte", Value: "100"},
		},
	}

	// Save the visual filter as a named query through the production path.
	id, err := app.SaveFilterAsQuery("tickets", spec, nil, "Open tickets >= 100")
	require.NoError(t, err, "saving a filter as a named query must succeed")
	require.NotEmpty(t, id, "save must return the new query id")

	// The save must use the SAME registry that ListQueries/GetQuery read.
	qs, err := app.ListQueries()
	require.NoError(t, err)
	require.Len(t, qs, 1, "exactly one query must be persisted via the registry")
	assert.Equal(t, "Open tickets >= 100", qs[0].Title)
	assert.Equal(t, id, qs[0].ID)

	stored, err := app.GetQuery(id)
	require.NoError(t, err, "the saved query must be retrievable by id")
	require.NotNil(t, stored)
	require.NotEmpty(t, stored.SQL, "the persisted query must carry generated SQL")

	// The stored SQL must equal the deterministic preview output (no parallel
	// rendering path).
	preview, err := app.generateFilterSQL("tickets", spec, nil)
	require.NoError(t, err)
	assert.Equal(t, preview, stored.SQL,
		"saved SQL must equal the preview/generate output (single rendering path)")

	// Executing the stored query returns rows equivalent to the live filter.
	wantIDs := filteredIDs(t, app, spec)         // ids 1,4,5,6
	gotIDs := queryResultIDs(t, app, stored.SQL) // via ExecuteQuery
	assert.Equal(t, wantIDs, gotIDs,
		"saved query rows must equal the equivalent GetTableDataFiltered rows")
	require.Equal(t, []int64{1, 4, 5, 6}, wantIDs, "sanity: the live filter matches the expected ids")

	// --- persistence across close + reopen ---
	reopenApp(t, app)

	persisted, err := app.GetQuery(id)
	require.NoError(t, err, "saved query must survive close + reopen")
	require.NotNil(t, persisted)
	assert.Equal(t, stored.SQL, persisted.SQL, "persisted SQL must round-trip unchanged")

	// And it must still execute to the same rows after the reopen.
	gotAfter := queryResultIDs(t, app, persisted.SQL)
	assert.Equal(t, []int64{1, 4, 5, 6}, gotAfter, "reopened saved query executes to the same rows")
}

// TestSaveFilterAsQuery_SelectedColumns proves the QBE-grid "selected columns"
// projection flows into the generated SQL (column projection, not just SELECT *).
func TestSaveFilterAsQuery_SelectedColumns(t *testing.T) {
	app := newOpenApp(t)
	seedTicketsTable(t, app)

	spec := FilterSpec{
		Combinator: "AND",
		Conditions: []FilterCondition{{Column: "status", Op: "eq", Value: "open"}},
	}

	sqlStr, err := app.generateFilterSQL("tickets", spec, []string{"id", "status"})
	require.NoError(t, err)

	res, err := app.ExecuteQuery(sqlStr)
	require.NoError(t, err)

	names := make([]string, 0, len(res.Columns))
	for _, c := range res.Columns {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"id", "status"}, names,
		"selected-columns QBE projection must restrict the result columns")

	// An unknown selected column must be rejected (no SQL injection via ident).
	_, err = app.generateFilterSQL("tickets", spec, []string{"id; DROP TABLE tickets"})
	require.Error(t, err, "an unknown/invalid selected column must be rejected")
}

// TestSaveFilterAsQuery_NoFileOpen guards the no-file-open path, matching the
// other binding methods.
func TestSaveFilterAsQuery_NoFileOpen(t *testing.T) {
	app := NewApp(testLogger())
	app.ctx = context.Background()

	_, err := app.SaveFilterAsQuery("tickets", FilterSpec{}, nil, "x")
	require.Error(t, err, "must error when no file is open")

	_, err = app.generateFilterSQL("tickets", FilterSpec{}, nil)
	require.Error(t, err, "preview must error when no file is open")
}

// TestGenerateFilterSQL_TextStable pins the exact SQL text SaveFilterAsQuery
// stores (U1 moved the renderer into internal/queries; saved queries on disk
// must not change shape), including a catalog-qualified table.
func TestGenerateFilterSQL_TextStable(t *testing.T) {
	app := appWithSecondCatalog(t)
	const local = `"local"."main"."items"`

	cases := []struct {
		name    string
		table   string
		spec    FilterSpec
		columns []string
		want    string
	}{
		{"no conditions", "items", FilterSpec{}, nil, "SELECT * FROM " + local},
		{"string eq, default AND", "items", FilterSpec{Conditions: []FilterCondition{{Column: "sku", Op: "eq", Value: "it's"}}}, nil,
			"SELECT * FROM " + local + ` WHERE "sku" = 'it''s'`},
		{"OR with null checks and like", "items", FilterSpec{Combinator: " or ", Conditions: []FilterCondition{
			{Column: "sku", Op: "is_null"}, {Column: "sku", Op: "is_not_null"}, {Column: "sku", Op: "like", Value: "a%"}}}, nil,
			"SELECT * FROM " + local + ` WHERE "sku" IS NULL OR "sku" IS NOT NULL OR "sku" LIKE 'a%'`},
		{"qualified table, projection, typed values", "other.main.items", FilterSpec{Combinator: "AND", Conditions: []FilterCondition{
			{Column: "id", Op: "gte", Value: float64(1)}, {Column: "name", Op: "neq", Value: nil}}}, []string{"name", "id"},
			`SELECT "name", "id" FROM "other"."main"."items" WHERE "id" >= 1 AND "name" <> NULL`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := app.generateFilterSQL(c.table, c.spec, c.columns)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
	_, err := app.generateFilterSQL("other.main.items", FilterSpec{Conditions: []FilterCondition{{Column: "nope", Op: "eq", Value: 1}}}, nil)
	assert.Error(t, err)
}
