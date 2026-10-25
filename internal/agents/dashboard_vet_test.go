package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dashboardPage(sqls ...string) map[string]any {
	comps := []any{}
	for i, s := range sqls {
		comps = append(comps, map[string]any{"id": string(rune('a' + i)), "type": "stat", "label": "L", "sql": s})
	}
	return map[string]any{"page": map[string]any{"title": "D", "components": comps}}
}

// #215: create_dashboard vets each sql binding like add_query_cell, reports
// every failure by component path, and does so on a dry run too (a headless
// client with "Ask before changes" on).
func TestCreateDashboard_VetsSQLBindings(t *testing.T) {
	bad := []struct{ name, sql, want string }{
		{"bad column", "SELECT nope FROM t", "nope"},
		{"delete", "DELETE FROM t", "read-only"},
		{"getenv", "SELECT getenv('HOME')", "getenv"},
	}
	for _, mode := range []struct {
		name string
		ask  bool
	}{{"dry run (ask on, headless)", true}, {"real write (ask off)", false}} {
		t.Run(mode.name, func(t *testing.T) {
			for _, c := range bad {
				fx := newArtifactFixture(t, ModeReadWrite, withAsk(mode.ask))
				resp := fx.call("create_dashboard", dashboardPage("SELECT COUNT(*) FROM t", c.sql))
				require.NotNil(t, resp.Error, c.name)
				assert.Equal(t, ErrInvalidParams, resp.Error.Code, c.name)
				assert.Contains(t, resp.Error.Message, "components[1].sql", c.name)
				assert.NotContains(t, resp.Error.Message, "components[0]", c.name)
				assert.Contains(t, resp.Error.Message, c.want, c.name)
				assert.Equal(t, 0, notebookCount(t, fx), c.name)
			}
		})
	}
}

func TestCreateDashboard_ReportsEveryFailure(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	resp := fx.call("create_dashboard", dashboardPage("SELECT nope FROM t", "SELECT 1", "DELETE FROM t"))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "components[0].sql")
	assert.NotContains(t, resp.Error.Message, "components[1]")
	assert.Contains(t, resp.Error.Message, "components[2].sql")
}

func TestCreateDashboard_ValidSQLPassesTheDryRun(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	resp := fx.call("create_dashboard", dashboardPage("SELECT COUNT(*) FROM t"))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "valid and ready")
}

// A component naming a connection is vetted on that connection.
func TestCreateDashboard_VetsOnTheComponentsConnection(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(false))
	comp := map[string]any{"id": "a", "type": "stat", "label": "L", "sql": "SELECT COUNT(*) FROM orders", "connection": "other"}
	page := map[string]any{"title": "D", "components": []any{comp}}
	require.Nil(t, fx.call("create_dashboard", map[string]any{"page": page}).Error)
	comp["connection"] = "nope"
	resp := fx.call("create_dashboard", map[string]any{"page": page})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "components[0].sql")
}

func TestReadSkill_A2UIOverTheHandler(t *testing.T) {
	handler, _, _ := newSkillsTestHandler(t, ModeReadOnly)
	resp := handler.Handle(t.Context(), &Request{JSONRPC: "2.0", ID: 1, Method: "read_skill", Params: map[string]any{"name": "a2ui"}})
	require.Nil(t, resp.Error)
	result, _ := resp.Result.(map[string]any)
	body, _ := result["instructions"].(string)
	for _, want := range []string{"sankeyChart", "dateRange", "pivot", "drill", "notebook dashboard", "data artifact view"} {
		assert.Contains(t, body, want)
	}
}
