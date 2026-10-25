package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for #222: what the agent is told when run_query's SQL is refused.

func TestKeywordAliasHint(t *testing.T) {
	_, err := countStatements("SELECT count(DISTINCT 1) days")
	require.Error(t, err)
	got := parseFailure(err, "SELECT count(DISTINCT 1) days")
	assert.Contains(t, got, `syntax error at or near "days"`)
	assert.Contains(t, got, "AS days")
	assert.Contains(t, got, "quote it")

	// Several bare keyword aliases in one statement are all named.
	text := "SELECT 1 hours, 2 minutes, 3 AS ok FROM (SELECT 1) t"
	_, err = countStatements(text)
	require.Error(t, err)
	got = parseFailure(err, text)
	assert.Contains(t, got, `"hours"`)
	assert.Contains(t, got, `"minutes"`)
}

func TestKeywordAliasHint_NotOnOtherParseErrors(t *testing.T) {
	for _, text := range []string{
		"SELECT FROM WHERE",
		"SELECT * FORM t",
		"SELECT a b c FROM t",
		"SELEC 1",
		"SELECT 1 FROM",
		"SELECT (1",
	} {
		_, err := countStatements(text)
		require.Error(t, err, text)
		assert.NotContains(t, parseFailure(err, text), "hint", text)
	}
}

func TestStripExplainWrapper(t *testing.T) {
	msg := "Binder Error: Referenced column \"x\" not found!\n\nLINE 1: EXPLAIN (FORMAT JSON) SELECT x FROM t\n                                     ^"
	got := stripExplainWrapper(msg)
	assert.NotContains(t, got, "EXPLAIN")
	assert.Contains(t, got, "LINE 1: SELECT x FROM t\n               ^")

	// An elided line carries no wrapper and is left alone.
	elided := "Binder Error: nope\n\nLINE 1: ...FROM t WHERE x\n                       ^"
	assert.Equal(t, elided, stripExplainWrapper(elided))
}

func TestPlanErrorShowsTheAgentsOwnSQL(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQuery(handler, "SELECT nosuchcol FROM t")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "nosuchcol")
	assert.NotContains(t, resp.Error.Message, "EXPLAIN")
	assert.Contains(t, resp.Error.Message, "LINE 1: SELECT nosuchcol FROM t")
}

func TestUntypedParamGetsACastHint(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQueryWithParams(handler, "SELECT date_trunc('month', $from)", map[string]any{"from": "2026-01-15"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "$from::DATE")
	assert.NotContains(t, resp.Error.Message, "EXPLAIN")

	// The cast the hint suggests works.
	resp = runQueryWithParams(handler, "SELECT date_trunc('month', $from::DATE) AS m", map[string]any{"from": "2026-01-15"})
	require.Nil(t, resp.Error)

	// No parameter, no hint (the same binder error without params).
	resp = runQuery(handler, "SELECT date_trunc('month', 'x') AS m")
	if resp.Error != nil {
		assert.NotContains(t, resp.Error.Message, "Hint: a ?")
	}
}

func TestPositionalPlaceholderPointsToNamedParams(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQueryWithParams(handler, "SELECT ? + 1", map[string]any{"1": 30})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "$name")
	assert.Contains(t, resp.Error.Message, "?")
	assert.NotContains(t, resp.Error.Message, "do not line up")

	// ? inside a string literal is not a placeholder.
	resp = runQuery(handler, "SELECT '?' AS q")
	require.Nil(t, resp.Error)

	// A $name inside a string is not a parameter either, and says so.
	resp = runQueryWithParams(handler, "SELECT 'a $a' AS q", map[string]any{"a": 1})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "string literal")
}

func TestRunQuery_MultiStatement(t *testing.T) {
	handler, db := newRealDuckDBHandler(t, ModeReadWrite)

	resp := runQuery(handler, "SELECT 1 AS a; SELECT 2 AS b")
	require.NotNil(t, resp.Error, "the first result would be silently dropped")
	assert.Contains(t, resp.Error.Message, "last statement only")
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)

	// A trailing semicolon is one statement.
	for _, q := range []string{"SELECT 1 AS a;", "SELECT 1 AS a ;  ", "SELECT 1 AS a; -- done"} {
		resp = runQuery(handler, q)
		require.Nil(t, resp.Error, q)
	}

	// Writes may still be scripted, ending in the read that shows the effect.
	resp = runQuery(handler, "INSERT INTO t VALUES (7, 'Zed'); SELECT count(*) AS n FROM t")
	require.Nil(t, resp.Error)
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 2, n)

	// A read in front of a write is dropped too.
	resp = runQuery(handler, "SELECT 1; INSERT INTO t VALUES (8, 'Yan')")
	require.NotNil(t, resp.Error)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 2, n, "a refused script must not run any statement")
}

func TestRunQuery_SQLMustBeAString(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	for name, v := range map[string]any{"number": float64(123), "array": []any{"SELECT 1"}, "object": map[string]any{}, "bool": true} {
		resp := handler.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: "1", Method: "run_query", Params: map[string]any{"sql": v}})
		require.NotNil(t, resp.Error, name)
		assert.Contains(t, resp.Error.Message, "sql must be a string", name)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, name)
	}
	// An empty or missing sql is still the empty-statement refusal.
	resp := runQuery(handler, "   ")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "empty")
}

func TestPolicy_UnpivotStatementPointsToFromForm(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	for _, q := range []string{
		"UNPIVOT (SELECT 1 AS a, 2 AS b) ON COLUMNS(*) INTO NAME m VALUE g",
		"UNPIVOT t ON id INTO NAME m VALUE g",
		"PIVOT t ON name USING count(*)",
	} {
		resp := runQuery(handler, q)
		require.NotNil(t, resp.Error, q)
		assert.Contains(t, resp.Error.Message, "FROM <table> UNPIVOT", q)
		assert.NotContains(t, resp.Error.Message, "allow-list", q)
	}
	resp := runQuery(handler, "FROM (SELECT 1 AS a, 2 AS b) UNPIVOT (g FOR m IN (a, b))")
	require.Nil(t, resp.Error)
}

func TestPolicy_PgCatalogHasACatalogViewMessage(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	for _, q := range []string{
		"SELECT count(*) FROM pg_catalog.pg_tables",
		"SELECT * FROM system.pg_catalog.pg_tables",
	} {
		resp := runQuery(handler, q)
		require.NotNil(t, resp.Error, q)
		assert.Contains(t, resp.Error.Message, "pg_catalog", q)
		assert.Contains(t, resp.Error.Message, "information_schema.tables", q)
		assert.NotContains(t, resp.Error.Message, "files and URLs", q)
	}
}

func TestPolicy_PragmaStorageInfoTableFunction(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQuery(handler, "SELECT column_name, segment_type FROM pragma_storage_info('t') LIMIT 3")
	require.Nil(t, resp.Error, "pragma_storage_info is the table-function form of the allowed PRAGMA storage_info")
	resp = runQuery(handler, "PRAGMA storage_info('t')")
	require.Nil(t, resp.Error)

	// The siblings stay refused, and so does a name that names no allowed function.
	for _, q := range []string{
		"SELECT * FROM pragma_database_list()",
		"SELECT * FROM pragma_storage_info_x('t')",
	} {
		resp = runQuery(handler, q)
		require.NotNil(t, resp.Error, q)
	}
}

func TestPolicy_TempTableStillNeedsFullTier(t *testing.T) {
	rw, _ := newRealDuckDBHandler(t, ModeReadWrite)
	resp := runQuery(rw, "CREATE TEMP TABLE scratch AS SELECT 1 AS x")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "full tier")
	assert.Contains(t, resp.Error.Message, "TEMP table")
	assert.Contains(t, resp.Error.Message, "CTE")

	// A plain CREATE gets the ordinary message, without the TEMP note.
	resp = runQuery(rw, "CREATE TABLE plain AS SELECT 1 AS x")
	require.NotNil(t, resp.Error)
	assert.NotContains(t, resp.Error.Message, "TEMP")

	full, db := newRealDuckDBHandler(t, ModeFull)
	resp = runQuery(full, "CREATE TEMP TABLE scratch AS SELECT 1 AS x")
	require.Nil(t, resp.Error)
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM scratch").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestClassify_DiscardedReadsAreCountedNotRefused(t *testing.T) {
	// Classification of a read script is unchanged (artifacts and notebooks
	// refuse more than one statement on their own); only run_query refuses.
	_, db := newRealDuckDBHandler(t, ModeReadOnly)
	class, reason := classifySQL(context.Background(), db, "SELECT 1; SELECT 2", nil)
	assert.Equal(t, sqlClassRead, class, reason)
	assert.False(t, strings.Contains(reason, "last statement"))
}

// The table a PRAGMA or introspection function names is a table reference, and
// is held to the reference rules (a DuckLake's internal catalog, a MySQL server
// schema, pg_catalog), in every form.
func TestPolicy_TableArgumentsAreVetted(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	for _, q := range []string{
		"SELECT * FROM pragma_storage_info('__ducklake_metadata_lake.main.ducklake_metadata')",
		"SELECT * FROM pragma_table_info('__ducklake_metadata_lake.main.ducklake_metadata')",
		"PRAGMA storage_info('__ducklake_metadata_lake.main.ducklake_metadata')",
		"PRAGMA table_info('__ducklake_metadata_lake.main.ducklake_metadata')",
		"CALL pragma_table_info('__ducklake_metadata_lake.main.ducklake_metadata')",
		`SELECT * FROM pragma_storage_info('"__ducklake_metadata_lake".main."x"')`,
		"SELECT * FROM pragma_storage_info('pg_catalog.pg_tables')",
		"SELECT * FROM pragma_storage_info('a.b.c.d')",
		"SELECT * FROM pragma_storage_info(t.name) , t",
		"SELECT * FROM pragma_storage_info('t', 'x')",
		"SELECT * FROM pragma_table_info()",
		"PRAGMA table_info('t', 'u')",
	} {
		resp := runQuery(handler, q)
		require.NotNil(t, resp.Error, q)
	}
	resp := runQuery(handler, "SELECT * FROM pragma_storage_info('__ducklake_metadata_lake.main.x')")
	assert.Contains(t, resp.Error.Message, "DuckLake")

	for _, q := range []string{
		"SELECT * FROM pragma_storage_info('t') LIMIT 1",
		"PRAGMA table_info('t')",
		"PRAGMA table_info('memory.main.t')",
		"SELECT * FROM pragma_table_info('main.t')",
		"CALL pragma_table_info('t')",
	} {
		resp := runQuery(handler, q)
		require.Nil(t, resp.Error, q)
	}
}

func TestPolicy_BarePgViewNames(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQuery(handler, "SELECT * FROM pg_tables")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "pg_catalog")
}

func TestKeywordAliasHint_Bounds(t *testing.T) {
	text := "SELECT 1 days, 2 days"
	_, err := countStatements(text)
	require.Error(t, err)
	assert.Equal(t, 1, strings.Count(parseFailure(err, text), "AS days"))

	long := "SELECT 1 days /*" + strings.Repeat("x", maxAliasHintBytes) + "*/"
	_, err = countStatements(long)
	require.Error(t, err)
	assert.NotContains(t, parseFailure(err, long), "hint")
}

// A TEMP note belongs to the tier refusal only, not to the verdict's Reason
// that other surfaces (notebook refusals) print.
func TestTempNoteStaysOutOfReason(t *testing.T) {
	_, db := newRealDuckDBHandler(t, ModeReadOnly)
	v := analyzeSQL(context.Background(), db, nil, "CREATE TEMP TABLE s AS SELECT 1 AS x", nil)
	assert.True(t, v.Temp)
	assert.Empty(t, v.Reason)
	v = analyzeSQL(context.Background(), db, nil, "CREATE TABLE s AS SELECT 1 AS x", nil)
	assert.False(t, v.Temp)
}
