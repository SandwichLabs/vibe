package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsWriteCall covers the classification IsWriteCall moved from
// cmd/mcp_helpers.go's saveHookHandler.wroteData (#81), table-driven so
// Studio's dirty tracking and `vibe mcp serve`'s debounced saver share one
// answer for every tool.
func TestIsWriteCall(t *testing.T) {
	db := newClassifierTestDB(t)

	cases := []struct {
		name   string
		method string
		params map[string]any
		want   bool
	}{
		{"run_query SELECT is a read", "run_query", map[string]any{"sql": "SELECT * FROM t"}, false},
		{"run_query INSERT is a write", "run_query", map[string]any{"sql": "INSERT INTO t VALUES (3, 'Zed')"}, true},
		{"run_query CREATE is a write", "run_query", map[string]any{"sql": "CREATE TABLE z (a INTEGER)"}, true},
		{"create_record is unconditionally a write", "create_record", map[string]any{}, true},
		{"create_table is unconditionally a write", "create_table", map[string]any{}, true},
		{"add_query_cell (content) writes SQLite only, not a write", "add_query_cell", map[string]any{}, false},
		{"create_or_update_skill (content) writes SQLite only, not a write", "create_or_update_skill", map[string]any{}, false},
		{"introspect_schema is never a write", "introspect_schema", map[string]any{}, false},
		{"unknown method is never a write", "no_such_tool", map[string]any{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{Method: tc.method, Params: tc.params}
			assert.Equal(t, tc.want, IsWriteCall(context.Background(), db, req))
		})
	}
}

// TestIsWriteCall_NilDBFailsTowardWrite: run_query with no db to classify
// against must fail toward "this was a write" rather than silently letting
// an unclassifiable write go unmarked.
func TestIsWriteCall_NilDBFailsTowardWrite(t *testing.T) {
	req := &Request{Method: "run_query", Params: map[string]any{"sql": "SELECT 1"}}
	assert.True(t, IsWriteCall(context.Background(), nil, req))
}
