package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agent's create_table / add_column take literal DEFAULTs only and no CHECK
// (#21, #67). MCP dispatches to this same handler.
func TestSchemaTools_DefaultLiteralOnly_NoCheck(t *testing.T) {
	h, db := newTestHandler(t, ModeFull)
	create := func(typ string) *Response {
		return call(h, "create_table", map[string]any{"name": "t_" + t.Name(), "columns": []any{map[string]any{"name": "x", "type": typ}}})
	}
	drop := func() { _, _ = db.Exec(`DROP TABLE IF EXISTS "t_` + t.Name() + `"`) }

	for _, bad := range []string{
		"VARCHAR DEFAULT chr(39)||chr(120)", "VARCHAR DEFAULT concat('a','b')", "VARCHAR DEFAULT repeat('x',3)", "INTEGER CHECK (x > 0)",
	} {
		drop()
		resp := create(bad)
		require.NotNil(t, resp.Error, bad)
		assert.Contains(t, resp.Error.Message, "allowed:", bad)
		var n int
		assert.Error(t, db.QueryRow(`SELECT count(*) FROM "t_`+t.Name()+`"`).Scan(&n), "table must not exist: %s", bad)

		add := call(h, "add_column", map[string]any{"table": "users", "name": "c", "type": bad})
		require.NotNil(t, add.Error, bad)
	}

	for i, ok := range []string{"INTEGER DEFAULT 0", "VARCHAR DEFAULT 'abc'", "VARCHAR DEFAULT 'it''s'", "VARCHAR DEFAULT NULL", "TIMESTAMP DEFAULT CURRENT_TIMESTAMP", "INTEGER NOT NULL"} {
		drop()
		resp := create(ok)
		require.Nil(t, resp.Error, "%s: %v", ok, resp.Error)
		if ok == "INTEGER NOT NULL" {
			continue // DuckDB cannot ADD COLUMN with a constraint; that is not our refusal
		}
		add := call(h, "add_column", map[string]any{"table": "users", "name": "c" + string(rune('a'+i)), "type": ok})
		require.Nil(t, add.Error, "%s: %v", ok, add.Error)
	}
	drop()
	require.Nil(t, create("VARCHAR DEFAULT 'it''s'").Error)
	_, err := db.Exec(`INSERT INTO "t_` + t.Name() + `" DEFAULT VALUES`)
	require.NoError(t, err)
	var got string
	require.NoError(t, db.QueryRow(`SELECT x FROM "t_`+t.Name()+`"`).Scan(&got))
	assert.Equal(t, "it's", got)
}

func TestSchemaTools_RefuseTopLevelComma(t *testing.T) {
	h, _ := newTestHandler(t, ModeFull)
	bad := "INTEGER, ALTER name TYPE VARCHAR USING concat(name, 'x')"
	r := call(h, "create_table", map[string]any{"name": "tc", "columns": []any{map[string]any{"name": "x", "type": bad}}})
	require.NotNil(t, r.Error)
	r = call(h, "add_column", map[string]any{"table": "users", "name": "c", "type": bad})
	require.NotNil(t, r.Error)
}
