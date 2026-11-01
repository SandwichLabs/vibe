package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/artifacts"
)

const agentScript = "function draw(ctx) { return '<svg xmlns=\"http://www.w3.org/2000/svg\"/>'; }\n"

func scriptManifest() map[string]any {
	m := artifactManifest(rowsQuery(okSQL))
	m["view"] = "script"
	return m
}

func (fx *artifactFixture) writeScript(name string, extra map[string]any) *Response {
	fx.t.Helper()
	p := map[string]any{"name": name, "manifest": scriptManifest()}
	for k, v := range extra {
		p[k] = v
	}
	return fx.call("write_artifact", p)
}

func TestWriteArtifact_ScriptViewLandsAsViewJS(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	resp := fx.writeScript("chart", map[string]any{"view_source": agentScript})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, true, res["saved"])
	assert.Contains(t, asString(t, res["note"]), "allows it", "the agent is told it cannot run yet")

	js, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "chart", "view.js"))
	require.NoError(t, err)
	assert.Equal(t, agentScript, string(js))
	assert.NoFileExists(t, filepath.Join(fx.root, "artifacts", "chart", "view.a2ui.json"))

	got := fx.call("get_artifact", map[string]any{"name": "chart"})
	require.Nil(t, got.Error, errMsg(got))
	g := mapOf(t, got.Result)
	assert.Equal(t, agentScript, g["view_source"])
	assert.NotContains(t, g, "view")
	assert.Equal(t, "script", mapOf(t, g["manifest"])["view"])

	list := fx.call("list_artifacts", nil)
	require.Nil(t, list.Error)
	assert.Equal(t, "script", asRows(t, mapOf(t, list.Result)["artifacts"])[0]["view"])
}

func TestWriteArtifact_ScriptViewParameterRules(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	cases := map[string]map[string]any{
		"view with a script manifest": {"view": viewMap(t, artifactView)},
		"no source":                   {},
		"source is not a string":      {"view_source": 5},
		"empty source":                {"view_source": ""},
		"over the cap":                {"view_source": strings.Repeat("a", artifacts.MaxScriptBytes+1)},
		"source and keep":             {"view_source": agentScript, "keep_view_source": true},
		"keep without a rev":          {"keep_view_source": true},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			resp := fx.writeScript("chart", extra)
			require.NotNil(t, resp.Error, "must be refused")
			assert.NoDirExists(t, filepath.Join(fx.root, "artifacts", "chart"))
		})
	}
	t.Run("view_source with an a2ui manifest", func(t *testing.T) {
		resp := fx.call("write_artifact", map[string]any{"name": "plain", "manifest": artifactManifest(rowsQuery(okSQL)),
			"view": viewMap(t, artifactView), "view_source": agentScript})
		require.NotNil(t, resp.Error)
		assert.NoDirExists(t, filepath.Join(fx.root, "artifacts", "plain"))
	})
}

func TestWriteArtifact_ScriptEditsNeedTheRevAndKeepViewSourceKeepsTheScript(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	resp := fx.writeScript("chart", map[string]any{"view_source": agentScript})
	require.Nil(t, resp.Error, errMsg(resp))
	rev := asString(t, mapOf(t, resp.Result)["rev"])

	// Without a rev an existing artifact is refused, as for every artifact.
	again := fx.writeScript("chart", map[string]any{"view_source": agentScript + "// x\n"})
	require.NotNil(t, again.Error)

	kept := fx.writeScript("chart", map[string]any{"keep_view_source": true, "rev": rev})
	require.Nil(t, kept.Error, errMsg(kept))
	js, _ := os.ReadFile(filepath.Join(fx.root, "artifacts", "chart", "view.js"))
	assert.Equal(t, agentScript, string(js))
}

func TestGetArtifact_LongScriptIsOmittedNotTruncated(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	long := "// " + strings.Repeat("x", artifactViewBudgetBytes) + "\n" + agentScript
	resp := fx.writeScript("big", map[string]any{"view_source": long})
	require.Nil(t, resp.Error, errMsg(resp))
	rev := asString(t, mapOf(t, resp.Result)["rev"])

	got := fx.call("get_artifact", map[string]any{"name": "big"})
	require.Nil(t, got.Error)
	g := mapOf(t, got.Result)
	assert.Nil(t, g["view_source"])
	assert.Equal(t, true, g["view_source_omitted"])
	assert.Greater(t, g["view_source_length"], artifactViewBudgetBytes)

	kept := fx.writeScript("big", map[string]any{"keep_view_source": true, "rev": rev})
	require.Nil(t, kept.Error, errMsg(kept))
	js, _ := os.ReadFile(filepath.Join(fx.root, "artifacts", "big", "view.js"))
	assert.Equal(t, long, string(js), "kept byte for byte")
}

func TestWriteArtifact_ScriptViewStillVetsItsQueries(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	m := artifactManifest(rowsQuery("DELETE FROM t"))
	m["view"] = "script"
	resp := fx.call("write_artifact", map[string]any{"name": "evil", "manifest": m, "view_source": agentScript})
	require.NotNil(t, resp.Error)
	assert.NoDirExists(t, filepath.Join(fx.root, "artifacts", "evil"))
}

func TestWriteArtifact_ApprovalCardNamesTheScriptView(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true), withApprover())
	resp := fx.writeScript("chart", map[string]any{"view_source": agentScript})
	require.Nil(t, resp.Error, errMsg(resp))
	require.Equal(t, 1, fx.approver.count())
	assert.Contains(t, fx.approver.asked[0].Summary, "script view")
	assert.Contains(t, fx.approver.asked[0].Summary, "until you allow it")
}

// The agent can write a view but can never allow one: no tool, no MCP method.
func TestNoToolCanAllowAView(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)allow|deny|revoke|trust|approve_view|view_hash|views_json`)
	for _, td := range ListTools() {
		assert.False(t, forbidden.MatchString(td.Name), "tool %q", td.Name)
		for _, p := range td.Params {
			assert.False(t, regexp.MustCompile(`(?i)hash|allow|trust`).MatchString(p.Name), "tool %q param %q", td.Name, p.Name)
		}
	}
}

// tools/list is the registry: only write_artifact's schema (and get_artifact's
// description, which now mentions view_source) changed with this feature.
func TestToolRegistry_WriteArtifactSchemaCarriesViewSource(t *testing.T) {
	td := GetTool("write_artifact")
	require.NotNil(t, td)
	byName := map[string]ParamDef{}
	for _, p := range td.Params {
		byName[p.Name] = p
	}
	require.Contains(t, byName, "view_source")
	assert.Equal(t, "string", byName["view_source"].Type)
	assert.False(t, byName["view_source"].Required)
	assert.False(t, byName["view"].Required, "view is for a2ui only now")
	require.Contains(t, byName, "keep_view_source")
}
