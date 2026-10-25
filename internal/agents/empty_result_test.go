package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatementShape_Predicates(t *testing.T) {
	shape, ok := statementShape(context.Background(), `SELECT * FROM users u WHERE u.region = 'north' AND status IN ('active', 'pending') AND 'k' = flag AND id = 1`)
	require.True(t, ok)
	require.Len(t, shape.preds, 3)
	assert.Equal(t, "u", shape.preds[0].qualifier)
	assert.Equal(t, "region", shape.preds[0].column)
	assert.Equal(t, []string{"north"}, shape.preds[0].values)
	assert.Equal(t, "status", shape.preds[1].column)
	assert.ElementsMatch(t, []string{"active", "pending"}, shape.preds[1].values)
	assert.Equal(t, "flag", shape.preds[2].column)
	assert.Equal(t, []tableSource{{table: "users", alias: "u"}}, shape.sources)
	assert.False(t, shape.opaque)
}

func TestStatementShape_None(t *testing.T) {
	shape, ok := statementShape(context.Background(), "SELECT * FROM users WHERE id = 1")
	require.True(t, ok)
	assert.Empty(t, shape.preds)
	_, ok = statementShape(context.Background(), "SELECT FROM WHERE")
	assert.False(t, ok, "a statement that does not parse has no shape")
}

func TestStatementShape_OpaqueAndCount(t *testing.T) {
	shape, ok := statementShape(context.Background(), `WITH c AS (SELECT 1 AS x) SELECT count(*) FROM c, t WHERE name = 'a'`)
	require.True(t, ok)
	assert.True(t, shape.opaque, "a CTE is a source that is not a plain table")
	assert.True(t, shape.countOnly)
	shape, _ = statementShape(context.Background(), `SELECT count(*) FROM t WHERE name = 'a' GROUP BY id`)
	assert.False(t, shape.countOnly, "a grouped count is not a single 0")
}

// TestHandleRunQuery_EmptyResultExplainsLiteralMismatch drives the empty
// result path through the real Handler -> real DuckDB, matching the issue's
// live-QA scenario: WHERE region = 'north' against a table that only has
// 'east'/'west' should come back with a note naming the actual values.
func TestHandleRunQuery_EmptyResultExplainsLiteralMismatch(t *testing.T) {
	handler, db := newRealDuckDBHandler(t, ModeReadOnly)
	_, err := db.Exec(`CREATE TABLE sales (id INTEGER, region VARCHAR); INSERT INTO sales VALUES (1, 'east'), (2, 'west');`)
	require.NoError(t, err)

	resp := runQuery(handler, "SELECT * FROM sales WHERE region = 'north'")
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)

	rows, ok := result["rows"].([][]any)
	require.True(t, ok)
	assert.Empty(t, rows)

	note, ok := result["note"].(string)
	require.True(t, ok, "an empty result with a literal mismatch must carry a note")
	assert.Contains(t, note, "region")
	assert.Contains(t, note, "'north'")
	assert.Contains(t, note, "east")
	assert.Contains(t, note, "west")
}

func TestHandleRunQuery_EmptyResultNoLiteralMismatchNoNote(t *testing.T) {
	handler, db := newRealDuckDBHandler(t, ModeReadOnly)
	_, err := db.Exec(`CREATE TABLE sales2 (id INTEGER, region VARCHAR); INSERT INTO sales2 VALUES (1, 'east');`)
	require.NoError(t, err)

	resp := runQuery(handler, "SELECT * FROM sales2 WHERE id = 999")
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	_, hasNote := result["note"]
	assert.False(t, hasNote, "a genuinely-empty result (no literal string mismatch) should not get a fabricated note")
}

func TestHandleRunQuery_NonEmptyResultNoNote(t *testing.T) {
	handler, _ := newRealDuckDBHandler(t, ModeReadOnly)
	resp := runQuery(handler, "SELECT * FROM users WHERE name = 'Alice'")
	require.Nil(t, resp.Error)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	_, hasNote := result["note"]
	assert.False(t, hasNote)
}

// emptyNote runs sql through the handler and returns the empty-result note.
func emptyNote(t *testing.T, handler *Handler, sqlText string) (string, bool) {
	t.Helper()
	resp := runQuery(handler, sqlText)
	require.Nil(t, resp.Error, sqlText)
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	note, has := result["note"].(string)
	return note, has
}

// newTwoCatalogHandler has the issue's #216 shape: a default catalog whose
// sources table has a name column, and an attached catalog "archive" whose
// races table has one too, holding different values.
func newTwoCatalogHandler(t *testing.T) *Handler {
	t.Helper()
	handler, db := newRealDuckDBHandler(t, ModeReadOnly)
	_, err := db.Exec(`
		CREATE TABLE sources (source_id INTEGER, name VARCHAR);
		INSERT INTO sources VALUES (1, 'Fitbit'), (2, 'Manual');
		ATTACH ':memory:' AS archive;
		CREATE TABLE archive.main.races (race_id INTEGER, name VARCHAR, label VARCHAR);
		INSERT INTO archive.main.races VALUES (1, '10k', 'road'), (2, 'marathon', 'road');`)
	require.NoError(t, err)
	return handler
}

// The note must describe the table the statement reads, not another table
// that happens to have a column of the same name.
func TestEmptyResultNote_UsesTheStatementsOwnCatalog(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	note, has := emptyNote(t, handler, "SELECT * FROM archive.main.races WHERE name = 'half'")
	require.True(t, has)
	assert.Contains(t, note, "'10k'")
	assert.Contains(t, note, "'marathon'")
	assert.NotContains(t, note, "Fitbit", "values of local.main.sources must not leak into a note about archive.main.races")
	assert.Contains(t, note, "archive.main.races")
}

func TestEmptyResultNote_ResolvesAnAlias(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	note, has := emptyNote(t, handler, "SELECT * FROM archive.main.races r WHERE r.name = 'half'")
	require.True(t, has)
	assert.Contains(t, note, "'marathon'")
	assert.NotContains(t, note, "Fitbit")

	// Two sources with the column, told apart by their aliases.
	note, has = emptyNote(t, handler, "SELECT * FROM sources s JOIN archive.main.races r ON s.source_id = r.race_id WHERE s.name = 'Zzz'")
	require.True(t, has)
	assert.Contains(t, note, "'Fitbit'")
	assert.NotContains(t, note, "marathon")
}

func TestEmptyResultNote_AmbiguousColumnIsSilent(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	// Both tables hold "name" and the bare name is compared in both arms:
	// which table a literal belongs to is unknowable, so there is no note.
	_, has := emptyNote(t, handler, "SELECT name FROM sources WHERE name = 'Zzz' UNION ALL SELECT name FROM archive.main.races WHERE name = 'half'")
	assert.False(t, has)
}

func TestEmptyResultNote_BareColumnWithOneOwnerInAJoin(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	// "label" exists only in archive.main.races, so a bare reference is unambiguous.
	note, has := emptyNote(t, handler, "SELECT * FROM sources JOIN archive.main.races ON sources.source_id = races.race_id WHERE label = 'zzz'")
	require.True(t, has)
	assert.Contains(t, note, "archive.main.races")
	assert.Contains(t, note, "'road'")
	assert.NotContains(t, note, "Fitbit")
}

func TestEmptyResultNote_UnresolvableIsSilent(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	// A subquery source: a bare column could belong to it.
	_, has := emptyNote(t, handler, "SELECT * FROM (SELECT 1 AS id, 'a' AS name) q WHERE name = 'zzz'")
	assert.False(t, has)
	// A CTE shadowing the table name.
	_, has = emptyNote(t, handler, "WITH sources AS (SELECT 'q' AS name) SELECT * FROM sources WHERE name = 'zzz'")
	assert.False(t, has)
}

func TestEmptyResultNote_ZeroCountAggregate(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	note, has := emptyNote(t, handler, "SELECT count(*) FROM archive.main.races WHERE name = 'half'")
	require.True(t, has, "an ungrouped count of 0 is as empty as no rows")
	assert.Contains(t, note, "counted 0 rows")
	assert.Contains(t, note, "'marathon'")
	assert.NotContains(t, note, "Fitbit")

	// A count that found rows carries no note.
	_, has = emptyNote(t, handler, "SELECT count(*) FROM archive.main.races WHERE name = '10k'")
	assert.False(t, has)
	// A sum of nothing is NULL, not a zero count.
	_, has = emptyNote(t, handler, "SELECT sum(race_id) FROM archive.main.races WHERE name = 'half'")
	assert.False(t, has)
}

func TestEmptyResultNote_SchemaPartThatNamesACatalog(t *testing.T) {
	handler := newTwoCatalogHandler(t)
	note, has := emptyNote(t, handler, "SELECT * FROM archive.races WHERE name = 'half'")
	require.True(t, has)
	assert.Contains(t, note, "archive.main.races")
	assert.NotContains(t, note, "Fitbit")
}
