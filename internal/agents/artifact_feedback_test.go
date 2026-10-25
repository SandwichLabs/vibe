package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pivotView(rows, columns, value string) string {
	return fmt.Sprintf(`{"title":"T","components":[{"id":"s","type":"stat","label":"n","value":1},`+
		`{"id":"p","type":"pivot","query":"rows","rows":[%s],"columns":%q,"value":%q}]}`, rows, columns, value)
}

func TestWriteArtifact_ReturnsEachQuerysColumns(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull, withExtraConns())
	m := artifactManifest(rowsQuery(okSQL), map[string]any{"name": "tot", "connection": "other", "sql": "SELECT sum(total) AS revenue, count(*) AS n FROM orders"})
	resp := fx.write("sales", m, artifactView, "")
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	qs, ok := res["queries"].([]ArtifactQueryColumns)
	require.True(t, ok, "queries: %T", res["queries"])
	require.Len(t, qs, 2)
	assert.Equal(t, "rows", qs[0].Name)
	assert.Equal(t, []ArtifactColumn{{"id", "INTEGER"}, {"name", "VARCHAR"}}, qs[0].Columns)
	assert.Equal(t, "tot", qs[1].Name)
	assert.Equal(t, []string{"revenue", "n"}, []string{qs[1].Columns[0].Name, qs[1].Columns[1].Name})
}

func TestWriteArtifact_ColumnsAreReadWithoutReadingRows(t *testing.T) {
	// A query whose rows would fail (division by zero) still has columns: the
	// zero-row limit never evaluates the projection over a row.
	fx := newArtifactFixture(t, ModeFull)
	resp := fx.write("boom", artifactManifest(rowsQuery("SELECT id, 1 // 0 AS bad FROM t WHERE id >= $min_id AND $who = $who")), artifactView, "")
	require.Nil(t, resp.Error, errMsg(resp))
	qs, ok := mapOf(t, resp.Result)["queries"].([]ArtifactQueryColumns)
	require.True(t, ok)
	require.Len(t, qs, 1)
	assert.Equal(t, "bad", qs[0].Columns[1].Name)
}

// Every check also runs in a dry run, so a refused-for-approval call reports
// the same feedback as a call that is allowed to write.
func TestWriteArtifact_ShapeChecks(t *testing.T) {
	good := pivotView(`"id"`, "name", "total")
	cases := []struct {
		name, view string
		want       []string // empty: must pass
	}{
		{"valid pivot", good, nil},
		{"row key missing", pivotView(`"activity"`, "name", "id"), []string{`components[1].rows[0]`, `"activity"`, "Columns: id, name, total"}},
		{"column key missing", pivotView(`"id"`, "kind", "total"), []string{`components[1].columns`, `"kind"`}},
		{"value missing", pivotView(`"id"`, "name", "minutes"), []string{`components[1].value`, `"minutes"`}},
		{"drill column missing", `{"title":"T","components":[{"id":"t","type":"table","query":"rows","drill":{"param":"who","column":"person"}}]}`,
			[]string{`components[0].drill.column`, `"person"`, "Columns: id, name, total"}},
		{"chart needs two columns", `{"title":"T","components":[{"id":"c","type":"barChart","query":"one"}]}`,
			[]string{`components[0]`, "barChart", "at least 2 column(s)", "returns 1"}},
		{"heatmap needs three", `{"title":"T","components":[{"id":"c","type":"heatmap","query":"one"}]}`, []string{"heatmap", "at least 3"}},
		{"stat one column is fine", `{"title":"T","components":[{"id":"c","type":"stat","label":"x","query":"one"}]}`, nil},
	}
	for _, ask := range []bool{false, true} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("ask=%v/%s", ask, c.name), func(t *testing.T) {
				fx := newArtifactFixture(t, ModeReadWrite, withAsk(ask))
				m := artifactManifest(rowsQuery("SELECT id, name, id AS total FROM t WHERE id >= $min_id AND ($who = '' OR name = $who)"), map[string]any{"name": "one", "sql": "SELECT count(*) AS n FROM t"})
				resp := fx.write("v", m, c.view, "")
				if len(c.want) == 0 {
					if ask {
						require.NotNil(t, resp.Error)
						assert.Equal(t, ErrApprovalNeeded, resp.Error.Code, "a valid request reaches the gate: %s", resp.Error.Message)
					} else {
						assert.Nil(t, resp.Error, errMsg(resp))
					}
					return
				}
				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrInvalidParams, resp.Error.Code)
				for _, w := range c.want {
					assert.Contains(t, resp.Error.Message, w)
				}
				list, err := fx.store.List(context.Background())
				require.NoError(t, err)
				assert.Empty(t, list)
			})
		}
	}
}

func TestWriteArtifact_AllProblemsInOnePass(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull)
	m := artifactManifest(
		map[string]any{"name": "rows", "sql": "SELECT id FROM t WHERE id > $nope"},
		map[string]any{"name": "two", "sql": "SELECT 1 WHERE $alsonot = 1"},
	)
	m["title"] = ""
	m["parameters"] = append(asSlice(t, m["parameters"]), map[string]any{"name": "bad", "type": "wat", "default": "x"})
	resp := fx.write("x", m, `{"title":"T","components":[{"id":"a","type":"table","query":"gone"}]}`, "")
	require.NotNil(t, resp.Error)
	msg := resp.Error.Message
	assert.Contains(t, msg, "problems found")
	for _, w := range []string{"title is required", `type "wat"`, "$nope", "$alsonot", `"gone" is not a query`} {
		assert.Contains(t, msg, w)
	}
	assert.Contains(t, msg, "1. ")
	assert.Contains(t, msg, "5. ")
}

func TestWriteArtifact_ManyProblemsAreCapped(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull)
	ps := make([]any, 0, 25)
	for i := 0; i < 25; i++ {
		ps = append(ps, map[string]any{"name": fmt.Sprintf("p%d", i), "type": "wat", "default": "x"})
	}
	m := artifactManifest(rowsQuery("SELECT 1"))
	m["parameters"] = ps
	resp := fx.write("x", m, artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "25 problems found")
	assert.Contains(t, resp.Error.Message, "20. ")
	assert.NotContains(t, resp.Error.Message, "21. ")
	assert.Contains(t, resp.Error.Message, "and 5 more")
}

func TestWriteArtifact_PolicyOnlyFindingsStayPermissionErrors(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull)
	resp := fx.write("x", artifactManifest(rowsQuery("DELETE FROM t WHERE id = $min_id")), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
}

func TestWriteArtifact_AcceptsViewAndManifestAsJSONText(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	resp := fx.call("write_artifact", map[string]any{"name": "s", "manifest": `{"title":"T","queries":[{"name":"rows","sql":"SELECT id, name FROM t"}]}`, "view": artifactView})
	require.Nil(t, resp.Error, errMsg(resp))
	resp = fx.call("write_artifact", map[string]any{"name": "s2", "manifest": artifactManifest(rowsQuery(okSQL)), "view": artifactView})
	require.Nil(t, resp.Error, errMsg(resp))
	resp = fx.call("write_artifact", map[string]any{"name": "s3", "manifest": artifactManifest(rowsQuery(okSQL)), "view": "not json"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "JSON text")
}

func TestArtifacts_ParameterDefaultsKeepTheirTypes(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	m := artifactManifest(rowsQuery(okSQL))
	m["parameters"] = append(asSlice(t, m["parameters"]),
		map[string]any{"name": "ratio", "type": "number", "default": 1.5},
		map[string]any{"name": "live", "type": "boolean", "default": true},
		map[string]any{"name": "code", "type": "text", "default": "007"},
	)
	require.Nil(t, fx.write("typed", m, artifactView, "").Error)
	yml, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "typed", "artifact.yaml"))
	require.NoError(t, err)
	for _, w := range []string{"default: 0\n", "default: 1.5\n", "default: true\n", `default: "007"`} {
		assert.Contains(t, string(yml), w)
	}
	resp := fx.call("get_artifact", map[string]any{"name": "typed"})
	require.Nil(t, resp.Error, errMsg(resp))
	ps := asSlice(t, mapOf(t, mapOf(t, resp.Result)["manifest"])["parameters"])
	defaults := map[string]any{}
	for _, p := range ps {
		pm := mapOf(t, p)
		defaults[asString(t, pm["name"])] = pm["default"]
	}
	assert.Equal(t, int64(0), defaults["min_id"])
	assert.Equal(t, 1.5, defaults["ratio"])
	assert.Equal(t, true, defaults["live"])
	assert.Equal(t, "007", defaults["code"])
	assert.Equal(t, "", defaults["who"])
}

func TestWriteArtifact_ScriptSyntax(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"valid", "function draw(ctx) { return vibe.run('rows', {}).then((r) => ({ svg: `<svg>${r.rows.length}</svg>` })) }", ""},
		{"unclosed paren", "function draw( {", "view.js line 1"},
		{"unbalanced brace", "function draw(c) {\n  return 1\n", `"{" is never closed`},
		{"mismatch", "function draw(c) { return (1] }", "closes the"},
		{"no draw", "var x = 1", "defines no draw"},
		{"unterminated string", "function draw(c) { return 'abc }", "never closed"},
	}
	for _, ask := range []bool{false, true} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("ask=%v/%s", ask, c.name), func(t *testing.T) {
				fx := newArtifactFixture(t, ModeReadWrite, withAsk(ask))
				resp := fx.call("write_artifact", map[string]any{"name": "s", "manifest": scriptManifest(), "view_source": c.src})
				if c.want == "" {
					if ask {
						require.NotNil(t, resp.Error)
						assert.Equal(t, ErrApprovalNeeded, resp.Error.Code, resp.Error.Message)
					} else {
						assert.Nil(t, resp.Error, errMsg(resp))
					}
					return
				}
				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrInvalidParams, resp.Error.Code)
				assert.Contains(t, resp.Error.Message, c.want)
			})
		}
	}
}

func TestWriteArtifact_PageChecksRunInTheDryRunToo(t *testing.T) {
	cases := []struct{ name, index, want string }{
		{"date layout", `{{ date (param "who") "2006-01-02" }}`, "not one of iso, short, long, month"},
		{"field on a row", `{{ range (query "rows").Rows }}{{ .name }}{{ end }}`, "array of cells"},
		{"inline script", `<script>alert(1)</script>`, "inline <script> never runs"},
	}
	for _, ask := range []bool{false, true} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("ask=%v/%s", ask, c.name), func(t *testing.T) {
				fx := newArtifactFixture(t, ModeReadWrite, withAsk(ask))
				resp := fx.writePage("p", map[string]any{"html_files": map[string]any{"index.html.tmpl": "<p>" + c.index + "</p>"}})
				require.NotNil(t, resp.Error)
				assert.Equal(t, ErrInvalidParams, resp.Error.Code)
				assert.Contains(t, resp.Error.Message, c.want)
				assert.True(t, strings.Contains(resp.Error.Message, "html_files: "))
			})
		}
	}
}

// A query that ends in a line comment must not swallow the wrapper's closing
// parenthesis, for the column probe or for a run.
func TestWriteArtifact_TrailingCommentQuery(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(false))
	m := artifactManifest(rowsQuery("SELECT id, name FROM t WHERE id >= $min_id -- everything"))
	resp := fx.write("c", m, artifactView, "")
	require.Nil(t, resp.Error, errMsg(resp))
	qs, ok := mapOf(t, resp.Result)["queries"].([]ArtifactQueryColumns)
	require.True(t, ok)
	require.Len(t, qs, 1)
	require.Len(t, qs[0].Columns, 2)
	n, _, err := fx.run("c", "rows", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}
