package agents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- run_query carries DuckDB type names, and DECIMAL/BIGINT values
// reach the model/MCP client with full precision. internal/mcp needs no
// code of its own for this (it just json.Marshals the handler's result),
// so this is the one place the wire shape is worth pinning. ---

// TestHandler_RunQuery_TypesAndPrecision proves the result carries a
// "types" array parallel to "columns" (DuckDB names, not Go scan types),
// and that the JSON actually sent to the model/MCP client contains
// DECIMAL/BIGINT text with full precision — a BIGINT above 2^53 as an
// exact string, a DECIMAL with its declared scale as an exact json.Number.
func TestHandler_RunQuery_TypesAndPrecision(t *testing.T) {
	handler, db := newRealDuckDBHandler(t, ModeFull)
	_, err := db.Exec(`CREATE TABLE probe (id BIGINT, amt DECIMAL(12,2));
		INSERT INTO probe VALUES (9007199254740993, 15675.30)`)
	require.NoError(t, err)

	resp := runQuery(handler, "SELECT id, amt FROM probe")
	require.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"id", "amt"}, result["columns"])
	assert.Equal(t, []string{"BIGINT", "DECIMAL(12,2)"}, result["types"])

	b, err := json.Marshal(resp.Result)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"9007199254740993"`, "a BIGINT past the safe-integer boundary must reach the model as an exact string")
	assert.Contains(t, string(b), "15675.30", "a DECIMAL must reach the model as its exact scale-padded number")
}
