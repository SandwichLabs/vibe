package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiteralPredicates(t *testing.T) {
	preds := literalPredicates(`SELECT * FROM users WHERE region = 'north' AND status IN ('active', 'pending')`)
	require.Len(t, preds, 2)
	assert.Equal(t, "region", preds[0].column)
	assert.Equal(t, []string{"north"}, preds[0].values)
	assert.Equal(t, "status", preds[1].column)
	assert.ElementsMatch(t, []string{"active", "pending"}, preds[1].values)
}

func TestLiteralPredicates_None(t *testing.T) {
	assert.Empty(t, literalPredicates("SELECT * FROM users WHERE id = 1"))
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
