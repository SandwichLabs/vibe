package agents

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/skills"
)

const agentPage = `<!doctype html><html><head><link rel="stylesheet" href="assets/style.css"></head><body>
<table>{{ range (query "rows").Rows }}<tr>{{ range . }}<td>{{ . }}</td>{{ end }}</tr>{{ end }}</table>
</body></html>`

func pageManifest() map[string]any {
	m := artifactManifest(rowsQuery(okSQL))
	m["view"] = "html"
	return m
}

func (fx *artifactFixture) writePage(name string, extra map[string]any) *Response {
	fx.t.Helper()
	p := map[string]any{"name": name, "manifest": pageManifest()}
	for k, v := range extra {
		p[k] = v
	}
	return fx.call("write_artifact", p)
}

func TestWriteArtifact_HTMLPageLandsAsFilesAndIsInert(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	resp := fx.writePage("sales", map[string]any{"html_files": map[string]any{
		"index.html.tmpl":  agentPage,
		"assets/style.css": "td { color: red; }",
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, true, res["saved"])
	assert.Equal(t, "static", res["page_kind"])
	assert.Equal(t, []string{"rows"}, res["queries_used"])
	assert.Contains(t, asString(t, res["note"]), "allows it", "the agent is told that nothing renders it")

	got, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "sales", "html", "index.html.tmpl"))
	require.NoError(t, err)
	assert.Equal(t, agentPage, string(got))
	assert.NoFileExists(t, filepath.Join(fx.root, "artifacts", "sales", "view.a2ui.json"))
}

func TestGetArtifact_ReturnsAPagesFilesAsText(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	require.Nil(t, fx.writePage("sales", map[string]any{"html_files": map[string]any{
		"index.html.tmpl": agentPage, "assets/style.css": "td { color: red; }",
	}}).Error)
	resp := fx.call("get_artifact", map[string]any{"name": "sales"})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, "html", res["view_kind"])
	assert.NotContains(t, res, "view")
	page := mapOf(t, res["page"])
	assert.Equal(t, "static", page["kind"])
	files := asRows(t, page["files"])
	require.Len(t, files, 2)
	byPath := map[string]map[string]any{}
	for _, f := range files {
		byPath[asString(t, f["path"])] = f
	}
	assert.Equal(t, agentPage, byPath["index.html.tmpl"]["text"])
	assert.Equal(t, "entry", byPath["index.html.tmpl"]["role"])
	assert.Equal(t, "td { color: red; }", byPath["assets/style.css"]["text"])

	list := fx.call("list_artifacts", nil)
	require.Nil(t, list.Error)
	row := asRows(t, mapOf(t, list.Result)["artifacts"])[0]
	assert.Equal(t, "html", row["view"])
	assert.Equal(t, "static", row["page_kind"])
}

func TestGetArtifact_APageFileTooLongComesBackWithoutText(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	big := "td { color: red; }\n" + strings.Repeat("/* pad */\n", artifactPageFileBudgetBytes/9)
	require.Nil(t, fx.writePage("sales", map[string]any{"html_files": map[string]any{
		"index.html.tmpl": agentPage, "assets/style.css": big,
	}}).Error)
	res := mapOf(t, fx.call("get_artifact", map[string]any{"name": "sales"}).Result)
	var css map[string]any
	for _, f := range asRows(t, mapOf(t, res["page"])["files"]) {
		if f["path"] == "assets/style.css" {
			css = f
		}
	}
	require.NotNil(t, css)
	assert.Nil(t, css["text"])
	assert.Equal(t, true, css["text_omitted"])

	// Sending {"keep": true} keeps the stored text, with the rev.
	rev := asString(t, res["rev"])
	resp := fx.writePage("sales", map[string]any{"rev": rev, "html_files": map[string]any{
		"index.html.tmpl":  agentPage + "<p>edited</p>",
		"assets/style.css": map[string]any{"keep": true},
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	kept, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "sales", "html", "assets", "style.css"))
	require.NoError(t, err)
	assert.Equal(t, big, string(kept))

	// Without the rev there is nothing to keep.
	resp = fx.writePage("other", map[string]any{"html_files": map[string]any{
		"index.html.tmpl": agentPage, "assets/style.css": map[string]any{"keep": true},
	}})
	require.NotNil(t, resp.Error)
}

func TestWriteArtifact_HTMLPageRefusals(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	cases := map[string]map[string]any{
		"no files":         {},
		"empty files":      {"html_files": map[string]any{}},
		"a view as well":   {"html_files": map[string]any{"index.html.tmpl": agentPage}, "view": viewMap(t, artifactView)},
		"view_source":      {"html_files": map[string]any{"index.html.tmpl": agentPage}, "view_source": "x"},
		"a binary asset":   {"html_files": map[string]any{"index.html.tmpl": agentPage, "assets/logo.png": "x"}},
		"a path escape":    {"html_files": map[string]any{"index.html.tmpl": agentPage, "../x.tmpl": "x"}},
		"a dotfile":        {"html_files": map[string]any{"index.html.tmpl": agentPage, ".env": "x"}},
		"an outside link":  {"html_files": map[string]any{"index.html.tmpl": `<a href="https://example.com/">x</a>`}},
		"a preconnect":     {"html_files": map[string]any{"index.html.tmpl": `<link rel="preconnect" href="https://example.com">`}},
		"an unknown func":  {"html_files": map[string]any{"index.html.tmpl": `{{ safeHTML "x" }}`}},
		"a computed query": {"html_files": map[string]any{"index.html.tmpl": `{{ query (printf "r%s" "ows") }}`}},
		"a non-text value": {"html_files": map[string]any{"index.html.tmpl": float64(3)}},
		"the wrong keep":   {"html_files": map[string]any{"index.html.tmpl": map[string]any{"keep": false}}},
		"a missing entry":  {"html_files": map[string]any{"other.html.tmpl": "x"}},
		"html_files, a2ui": nil,
	}
	for name, extra := range cases {
		if extra == nil {
			continue
		}
		resp := fx.writePage("page", extra)
		require.NotNil(t, resp.Error, name)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, name)
		assert.NotContains(t, resp.Error.Message, fx.root, name)
	}
	_, err := fx.store.Get(context.Background(), "page")
	assert.ErrorIs(t, err, artifacts.ErrNotFound, "nothing was written")

	// html_files belongs to view: html only.
	resp := fx.call("write_artifact", map[string]any{
		"name": "plain", "manifest": artifactManifest(rowsQuery(okSQL)), "view": viewMap(t, artifactView),
		"html_files": map[string]any{"index.html.tmpl": agentPage},
	})
	require.NotNil(t, resp.Error)
}

func TestWriteArtifact_HTMLPageQueriesAreVettedLikeAnyOther(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	m := artifactManifest(rowsQuery("DELETE FROM t"))
	m["view"] = "html"
	resp := fx.call("write_artifact", map[string]any{"name": "bad", "manifest": m, "html_files": map[string]any{"index.html.tmpl": agentPage}})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
}

func TestWriteArtifact_APageWriteIsCompareAndWriteAndKeepsBinaryAssets(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	// A page a person committed, with a binary asset the agent cannot send.
	mf, err := artifacts.Decode([]byte("version: 1\ntitle: Page\nqueries:\n  - name: rows\n    sql: SELECT id FROM t\nview: html\n"))
	require.NoError(t, err)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00")
	a := &artifacts.Artifact{Name: "page", Manifest: *mf, Files: []artifacts.PageFile{
		{Path: "index.html.tmpl", Data: []byte(`<p>{{ len (query "rows").Rows }}</p><img src="assets/logo.png">`)},
		{Path: "assets/logo.png", Data: png},
	}}
	require.NoError(t, fx.store.Put(context.Background(), a))

	// Without the rev an existing name is refused.
	m := artifactManifest(rowsQuery(okSQL))
	m["view"] = "html"
	resp := fx.call("write_artifact", map[string]any{"name": "page", "manifest": m, "html_files": map[string]any{"index.html.tmpl": agentPage}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already exists")

	// With a stale rev it is a conflict.
	resp = fx.call("write_artifact", map[string]any{"name": "page", "rev": "stale", "manifest": m, "html_files": map[string]any{"index.html.tmpl": agentPage}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "changed since")

	// get_artifact shows the asset by size only; a write with the rev keeps it.
	res := mapOf(t, fx.call("get_artifact", map[string]any{"name": "page"}).Result)
	var bin map[string]any
	for _, f := range asRows(t, mapOf(t, res["page"])["files"]) {
		if f["path"] == "assets/logo.png" {
			bin = f
		}
	}
	require.NotNil(t, bin)
	assert.Equal(t, true, bin["binary"])
	assert.NotContains(t, bin, "text")
	resp = fx.call("write_artifact", map[string]any{"name": "page", "rev": res["rev"], "manifest": m, "html_files": map[string]any{"index.html.tmpl": agentPage}})
	require.Nil(t, resp.Error, errMsg(resp))
	got, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "page", "html", "assets", "logo.png"))
	require.NoError(t, err)
	assert.Equal(t, png, got)
}

// An edit by the agent changes the page's hash, so the person is asked again.
func TestWriteArtifact_AnAgentEditChangesThePageHash(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	require.Nil(t, fx.writePage("page", map[string]any{"html_files": map[string]any{"index.html.tmpl": agentPage, "assets/style.css": "a{}"}}).Error)
	first, err := fx.store.Get(context.Background(), "page")
	require.NoError(t, err)
	res := mapOf(t, fx.call("get_artifact", map[string]any{"name": "page"}).Result)
	require.Nil(t, fx.writePage("page", map[string]any{"rev": res["rev"], "html_files": map[string]any{"index.html.tmpl": agentPage, "assets/style.css": "a{color:red}"}}).Error)
	second, err := fx.store.Get(context.Background(), "page")
	require.NoError(t, err)
	assert.NotEqual(t, first.Page.Hash, second.Page.Hash)
}

func TestWriteArtifact_AScriptedPageWithCapabilitiesIsStoredNeverRun(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	m := pageManifest()
	m["html"] = map[string]any{"capabilities": map[string]any{"state": map[string]any{"scope": "local"}, "ask": map[string]any{}}}
	resp := fx.call("write_artifact", map[string]any{"name": "app", "manifest": m, "html_files": map[string]any{
		"index.html.tmpl": `<script src="app.js"></script><p>{{ len (query "rows").Rows }}</p>`,
		"app.js":          "vibe.run('rows').then(() => {})",
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, "scripted", res["page_kind"])
	assert.Equal(t, []string{"ask", "state"}, res["capabilities"])
}

// skillPageFiles returns the fenced files of the built-in skill's HTML pages
// section by "lang path" (```html index.html.tmpl), in order.
func skillPageFiles(t *testing.T) []struct{ path, text string } {
	t.Helper()
	var body string
	for _, s := range skills.Builtins() {
		if s.Name == "data-artifacts" {
			body = s.Instructions
		}
	}
	require.Contains(t, body, "## HTML pages")
	body = body[strings.Index(body, "## HTML pages"):]
	re := regexp.MustCompile("(?s)```(?:html|css|js) (\\S+)\n(.*?)```")
	var out []struct{ path, text string }
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, struct{ path, text string }{m[1], m[2]})
	}
	return out
}

// The skill's page examples must be accepted by the tool and render, so the
// instructions never drift from the page rules.
func TestDataArtifactsSkill_HTMLExamplesWriteAndRender(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	ex := skillPageFiles(t)
	require.Len(t, ex, 4, "static: template and stylesheet; scripted: template and script")

	m := artifactManifest(
		map[string]any{"name": "by_region", "sql": "SELECT id, name FROM t WHERE id >= $min_id"},
		map[string]any{"name": "peak", "sql": "SELECT max(id) FROM t"},
	)
	m["view"] = "html"
	resp := fx.call("write_artifact", map[string]any{"name": "static-ex", "manifest": m, "html_files": map[string]any{
		ex[0].path: ex[0].text, ex[1].path: ex[1].text,
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, "static", mapOf(t, resp.Result)["page_kind"])

	a, err := fx.store.Get(context.Background(), "static-ex")
	require.NoError(t, err)
	var out bytes.Buffer
	run := func(_ context.Context, q string, _ map[string]any) (*artifacts.PageRows, error) {
		if q == "peak" {
			return &artifacts.PageRows{Columns: []string{"m"}, Rows: [][]any{{int64(5000)}}}, nil
		}
		return &artifacts.PageRows{Columns: []string{"region", "revenue"}, Rows: [][]any{{"EU", int64(1200)}, {"US", int64(3400)}}}, nil
	}
	require.NoError(t, artifacts.RenderHTMLPage(context.Background(), run, a, nil, &out))
	assert.Contains(t, out.String(), "<rect")
	assert.Contains(t, out.String(), "EU")

	m2 := artifactManifest(map[string]any{"name": "by_region", "sql": "SELECT id, name FROM t WHERE id >= $min_id"})
	m2["view"] = "html"
	resp = fx.call("write_artifact", map[string]any{"name": "scripted-ex", "manifest": m2, "html_files": map[string]any{
		ex[2].path: ex[2].text, ex[3].path: ex[3].text,
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, "scripted", mapOf(t, resp.Result)["page_kind"])
	b, err := fx.store.Get(context.Background(), "scripted-ex")
	require.NoError(t, err)
	out.Reset()
	require.NoError(t, artifacts.RenderHTMLPage(context.Background(), run, b, nil, &out))
	assert.Contains(t, out.String(), `"Rows":[["EU",1200]`, "the script reads the data block as the skill says")
}
