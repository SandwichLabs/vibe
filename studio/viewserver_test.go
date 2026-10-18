package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

const testOrigin = "http://localhost:34115"

func testViewServer(allowed func(*viewGrant) bool) (*viewServer, http.Handler, *int) {
	libs := fstest.MapFS{"d3-array.js": {Data: []byte("/* d3-array */")}, "notjs.txt": {Data: []byte("x")}}
	vs := newViewServer([]byte("/* worker */"), libs, allowed)
	hits := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++; w.WriteHeader(http.StatusTeapot) })
	return vs, vs.Middleware()(next), &hits
}

func get(h http.Handler, method, target string) *http.Response {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return string(b)
}

func requireHardened(t *testing.T, r *http.Response, target string) {
	t.Helper()
	assert.NotEmpty(t, r.Header.Get("Content-Security-Policy"), target)
	assert.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"), target)
	assert.Equal(t, "no-store", r.Header.Get("Cache-Control"), target)
	assert.NotContains(t, r.Header.Get("Content-Security-Policy"), "unsafe-eval", target)
}

func TestViewServer_EveryResponseCarriesTheHeaders(t *testing.T) {
	vs, h, hits := testViewServer(func(*viewGrant) bool { return true })
	tok, err := vs.issue(&viewGrant{name: "a", hash: "h", origin: testOrigin, script: []byte("function draw(){}")})
	require.NoError(t, err)

	targets := []string{
		"/vibe-view/" + tok + "/worker.js",
		"/vibe-view/" + tok + "/view.js",
		"/vibe-view/lib/d3-array.js",
		"/vibe-view/lib/missing.js",
		"/vibe-view/lib/notjs.txt",
		"/vibe-view/" + strings.Repeat("0", 64) + "/view.js",
		"/vibe-view/nothex/view.js",
		"/vibe-view/" + tok + "/other.js",
		"/vibe-view/" + tok + "/",
		"/vibe-view/" + tok + "/index.html",
		"/vibe-view/",
		"/vibe-view/lib/",
		"/vibe-view",
		"/vibe-view/lib/../" + tok + "/view.js",
		"/vibe-view/lib//d3-array.js",
		"/vibe-view/lib/%2e%2e/x.js",
		"/vibe-view/lib/a%5cb.js",
	}
	for _, target := range targets {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := get(h, method, target)
			requireHardened(t, r, method+" "+target)
		}
	}
	r := get(h, http.MethodPost, "/vibe-view/lib/d3-array.js")
	requireHardened(t, r, "POST")
	assert.Equal(t, http.StatusMethodNotAllowed, r.StatusCode)
	assert.Zero(t, *hits, "nothing under /vibe-view/ reaches the asset handler")
}

func TestViewServer_ScriptCSPNamesOnlyTheTokensViewAndTheLibraries(t *testing.T) {
	vs, h, _ := testViewServer(func(*viewGrant) bool { return true })
	tok, err := vs.issue(&viewGrant{name: "a", hash: "h", origin: "wails://wails.localhost:34115", script: []byte("x")})
	require.NoError(t, err)
	r := get(h, http.MethodGet, "/vibe-view/"+tok+"/worker.js")
	require.Equal(t, http.StatusOK, r.StatusCode)
	csp := r.Header.Get("Content-Security-Policy")
	assert.Equal(t, "default-src 'none'; script-src wails://wails.localhost:34115/vibe-view/"+tok+"/view.js wails://wails.localhost:34115/vibe-view/lib/"+
		"; connect-src 'none'; worker-src 'none'; img-src 'none'; base-uri 'none'; form-action 'none'", csp)
	assert.Equal(t, "/* worker */", body(t, r))
	assert.Contains(t, r.Header.Get("Content-Type"), "javascript")
}

func TestViewServer_InjectionMatchPathsAreBadRequestNotNotFound(t *testing.T) {
	_, h, hits := testViewServer(func(*viewGrant) bool { return true })
	for _, target := range []string{"/vibe-view/", "/vibe-view/lib/", "/vibe-view/x/index.html", "/vibe-view/index.html", "/vibe-view/lib/index.html", "/vibe-view/a/b/"} {
		r := get(h, http.MethodGet, target)
		assert.Equal(t, http.StatusBadRequest, r.StatusCode, target)
	}
	assert.Zero(t, *hits)
	// A path outside /vibe-view/ is none of this server's business.
	r := get(h, http.MethodGet, "/vibe-viewer/x")
	assert.Equal(t, http.StatusTeapot, r.StatusCode)
	assert.Equal(t, 1, *hits)
}

func TestViewServer_UnknownRevokedOrChangedTokenGetsNoBytes(t *testing.T) {
	allowed := true
	vs, h, _ := testViewServer(func(*viewGrant) bool { return allowed })
	tok, err := vs.issue(&viewGrant{name: "a", hash: "h", origin: testOrigin, script: []byte("SECRET-VIEW")})
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, get(h, http.MethodGet, "/vibe-view/"+tok+"/view.js").StatusCode)
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+strings.Repeat("a", 64)+"/view.js").StatusCode)

	allowed = false // revoked, denied or changed
	for _, f := range []string{"view.js", "worker.js"} {
		r := get(h, http.MethodGet, "/vibe-view/"+tok+"/"+f)
		assert.Equal(t, http.StatusNotFound, r.StatusCode, f)
		assert.NotContains(t, body(t, r), "SECRET")
	}
	allowed = true // and the dropped token does not come back by itself
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/view.js").StatusCode)
}

func TestViewServer_NoTraversalAndOnlyLibraryFiles(t *testing.T) {
	vs, h, _ := testViewServer(func(*viewGrant) bool { return true })
	tok, _ := vs.issue(&viewGrant{name: "a", hash: "h", origin: testOrigin, script: []byte("x")})
	for _, target := range []string{
		"/vibe-view/lib/../../etc/passwd",
		"/vibe-view/lib/..%2f..%2fetc%2fpasswd",
		"/vibe-view/lib/sub/d3-array.js",
		"/vibe-view/lib/.hidden.js",
		"/vibe-view/" + tok + "/../" + tok + "/view.js",
		"/vibe-view/" + tok + "/view.js/..",
		"/vibe-view/" + tok + "/sub/view.js",
	} {
		r := get(h, http.MethodGet, target)
		assert.GreaterOrEqual(t, r.StatusCode, 400, target)
		assert.NotContains(t, body(t, r), "root:", target)
	}
	assert.Equal(t, "/* d3-array */", body(t, get(h, http.MethodGet, "/vibe-view/lib/d3-array.js")))
}

func TestViewServer_ATokenServesOnlyItsOwnArtifact(t *testing.T) {
	vs, h, _ := testViewServer(func(*viewGrant) bool { return true })
	a, err := vs.issue(&viewGrant{name: "a", hash: "ha", origin: testOrigin, script: []byte("VIEW-A")})
	require.NoError(t, err)
	b, err := vs.issue(&viewGrant{name: "b", hash: "hb", origin: testOrigin, script: []byte("VIEW-B")})
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	assert.Equal(t, "VIEW-A", body(t, get(h, http.MethodGet, "/vibe-view/"+a+"/view.js")))
	assert.Equal(t, "VIEW-B", body(t, get(h, http.MethodGet, "/vibe-view/"+b+"/view.js")))
	// No path under A's token reaches B.
	r := get(h, http.MethodGet, "/vibe-view/"+a+"/../"+b+"/view.js")
	assert.Equal(t, http.StatusBadRequest, r.StatusCode)
	assert.NotContains(t, body(t, r), "VIEW-B")
}

func TestViewServer_NewTokenReplacesTheArtifactsOldOneAndTheSetIsBounded(t *testing.T) {
	vs, h, _ := testViewServer(func(*viewGrant) bool { return true })
	first, _ := vs.issue(&viewGrant{name: "a", origin: testOrigin, script: []byte("1")})
	second, _ := vs.issue(&viewGrant{name: "a", origin: testOrigin, script: []byte("2")})
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+first+"/view.js").StatusCode)
	assert.Equal(t, "2", body(t, get(h, http.MethodGet, "/vibe-view/"+second+"/view.js")))
	for i := 0; i < maxViewGrants+5; i++ {
		_, err := vs.issue(&viewGrant{name: "n" + string(rune('a'+i%26)) + string(rune('a'+i/26)), origin: testOrigin, script: []byte("x")})
		require.NoError(t, err)
	}
	vs.mu.Lock()
	defer vs.mu.Unlock()
	assert.LessOrEqual(t, len(vs.grants), maxViewGrants)
}

func TestViewServer_OriginIsValidatedBeforeItReachesAHeader(t *testing.T) {
	vs, _, _ := testViewServer(nil)
	for _, bad := range []string{"", "localhost:34115", "http://a b", "http://x\r\nX-Evil: 1", "http://x; script-src *", "javascript://x", "http://x/path", "http://x'"} {
		_, err := vs.issue(&viewGrant{name: "a", origin: bad})
		assert.ErrorIs(t, err, errBadViewOrigin, bad)
	}
	for _, good := range []string{"http://localhost:34115", "wails://wails", "wails://wails.localhost:34115", "http://wails.localhost"} {
		_, err := vs.issue(&viewGrant{name: "a", origin: good})
		assert.NoError(t, err, good)
	}
}

// --- the App path: what is hashed is what is served ---

func allowedScriptApp(t *testing.T) (*App, http.Handler, string) {
	t.Helper()
	app := newOpenApp(t)
	writeStudioScript(t, app, "chart", studioScriptSource)
	st, err := app.ArtifactViewState("chart")
	require.NoError(t, err)
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	return app, app.views.Middleware()(http.NotFoundHandler()), st.Hash
}

func TestOpenArtifactView_RefusesAViewThatIsNotAllowed(t *testing.T) {
	app := newOpenApp(t)
	writeStudioScript(t, app, "chart", studioScriptSource)
	_, err := app.OpenArtifactView("chart", testOrigin)
	require.ErrorIs(t, err, errViewNotAllowed, "undecided")
	st, _ := app.ArtifactViewState("chart")
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	_, err = app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	writeStudioScript(t, app, "chart", studioScriptSource+"// edit\n")
	_, err = app.OpenArtifactView("chart", testOrigin)
	require.ErrorIs(t, err, errViewNotAllowed, "changed")
	_, err = app.DenyArtifactView("chart")
	require.NoError(t, err)
	_, err = app.OpenArtifactView("chart", testOrigin)
	require.ErrorIs(t, err, errViewNotAllowed, "denied")
	_, err = app.OpenArtifactView("nope", testOrigin)
	require.Error(t, err)
	_, err = NewApp(testLogger()).OpenArtifactView("chart", testOrigin)
	require.Error(t, err, "no workspace")
}

func TestOpenArtifactView_ServesTheBytesThatWereHashed(t *testing.T) {
	app, h, hash := allowedScriptApp(t)
	g, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	assert.Equal(t, hash, g.Hash)
	assert.Len(t, g.Token, 64)

	r := get(h, http.MethodGet, "/vibe-view/"+g.Token+"/view.js")
	require.Equal(t, http.StatusOK, r.StatusCode)
	served := body(t, r)
	got, err := artifacts.ViewHash([]byte(studioScriptManifest), []artifacts.ViewPart{{Path: artifacts.ScriptFile, Data: []byte(served)}})
	require.NoError(t, err)
	assert.Equal(t, hash, got, "the served bytes hash to the allowed hash")
	assert.Equal(t, studioScriptSource, served)
	requireHardened(t, r, "view.js")
}

func TestOpenArtifactView_AFileChangedAfterTheTokenGetsNoBytes(t *testing.T) {
	app, h, _ := allowedScriptApp(t)
	g, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)

	// Changed on disk after the token was issued: refused, never the new bytes
	// and never a mix. (The held bytes are what a token would serve, but only
	// while the decision still allows exactly that hash.)
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "chart")
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ScriptFile), []byte("function draw(){ return {svg:'<svg/>'} } // evil\n"), 0o644))
	for _, f := range []string{"view.js", "worker.js"} {
		r := get(h, http.MethodGet, "/vibe-view/"+g.Token+"/"+f)
		assert.Equal(t, http.StatusNotFound, r.StatusCode, f)
		assert.NotContains(t, body(t, r), "evil")
	}
}

func TestOpenArtifactView_ARevokedViewGetsNoBytes(t *testing.T) {
	app, h, _ := allowedScriptApp(t)
	g, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	_, err = app.RevokeArtifactView("chart")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+g.Token+"/view.js").StatusCode)
}

func TestOpenArtifactView_AnotherArtifactsTokenCannotFetchThisOne(t *testing.T) {
	app, h, _ := allowedScriptApp(t)
	writeStudioScript(t, app, "other", "function draw(){ return {svg:'<svg id=\"other\"/>'} }\n")
	st, _ := app.ArtifactViewState("other")
	_, err := app.AllowArtifactView("other", st.Hash)
	require.NoError(t, err)
	a, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	b, err := app.OpenArtifactView("other", testOrigin)
	require.NoError(t, err)

	// Each token serves its own bytes and no path under one reaches the other.
	assert.Equal(t, studioScriptSource, body(t, get(h, http.MethodGet, "/vibe-view/"+a.Token+"/view.js")))
	assert.Contains(t, body(t, get(h, http.MethodGet, "/vibe-view/"+b.Token+"/view.js")), `id="other"`)
	for _, target := range []string{
		"/vibe-view/" + a.Token + "/../" + b.Token + "/view.js",
		"/vibe-view/" + a.Token + "/" + b.Token + "/view.js",
		"/vibe-view/" + a.Token + "/other/view.js",
	} {
		r := get(h, http.MethodGet, target)
		assert.GreaterOrEqual(t, r.StatusCode, 400, target)
		assert.NotContains(t, body(t, r), `id="other"`, target)
	}
	// And a token's queries are its own artifact's: the name is the grant's.
	res, err := app.RunArtifactViewQuery(a.Token, "big", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"x"}, res.Columns)
}

func TestRunArtifactViewQuery_IsBoundToTheGrant(t *testing.T) {
	app, _, _ := allowedScriptApp(t)
	g, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	res, err := app.RunArtifactViewQuery(g.Token, "big", nil)
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)

	_, err = app.RunArtifactViewQuery(strings.Repeat("0", 64), "big", nil)
	require.ErrorIs(t, err, errViewNotAllowed, "unknown token")
	_, err = app.RunArtifactViewQuery(g.Token, "nope", nil)
	require.Error(t, err, "a query the manifest does not have")

	// The manifest is edited after the token was issued: the next run is refused.
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "chart")
	edited := studioScriptManifest + "  - name: evil\n    sql: SELECT 2 AS y\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(edited), 0o644))
	_, err = app.RunArtifactViewQuery(g.Token, "evil", nil)
	require.ErrorIs(t, err, errViewNotAllowed)
	_, err = app.RunArtifactViewQuery(g.Token, "big", nil)
	require.ErrorIs(t, err, errViewNotAllowed, "and the token is dropped")
}

func TestRunArtifactViewQuery_RevokedAndWorkspaceSwitchRefuse(t *testing.T) {
	app, _, _ := allowedScriptApp(t)
	g, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	_, err = app.RevokeArtifactView("chart")
	require.NoError(t, err)
	_, err = app.RunArtifactViewQuery(g.Token, "big", nil)
	require.ErrorIs(t, err, errViewNotAllowed, "revoked")

	// Allow again, open a token, then close the workspace: the token is gone and
	// its queries were cancelled; a same-named artifact in the next workspace
	// does not inherit it.
	st, _ := app.ArtifactViewState("chart")
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	g, err = app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	app.views.mu.Lock()
	grant := app.views.grants[g.Token]
	app.views.mu.Unlock()
	require.NotNil(t, grant)
	require.NoError(t, app.CloseWorkspace())
	assert.Error(t, grant.ctx.Err(), "queries of a dropped grant are cancelled")
	_, err = app.RunArtifactViewQuery(g.Token, "big", nil)
	require.Error(t, err)

	app2 := newOpenApp(t)
	writeStudioScript(t, app2, "chart", studioScriptSource)
	_, err = app2.RunArtifactViewQuery(g.Token, "big", nil)
	require.ErrorIs(t, err, errViewNotAllowed, "a token from another app/workspace")
}

func TestViewServer_ReplacingATokenCancelsTheOldGrant(t *testing.T) {
	vs, _, _ := testViewServer(func(*viewGrant) bool { return true })
	g1 := &viewGrant{name: "a", origin: testOrigin}
	_, err := vs.issue(g1)
	require.NoError(t, err)
	require.NoError(t, g1.ctx.Err())
	_, err = vs.issue(&viewGrant{name: "a", origin: testOrigin})
	require.NoError(t, err)
	assert.Error(t, g1.ctx.Err())
	vs.clear()
	vs.mu.Lock()
	defer vs.mu.Unlock()
	assert.Empty(t, vs.grants)
}

func TestValidateArtifactViewPage(t *testing.T) {
	app := appWithArtifact(t)
	ok, err := app.ValidateArtifactViewPage("orders", `{"title":"T","components":[{"id":"t1","type":"text","text":"hi"}]}`)
	require.NoError(t, err)
	assert.Contains(t, ok, `"hi"`)

	_, err = app.ValidateArtifactViewPage("orders", `{"title":"T","components":[{"id":"t1","type":"iframe","src":"x"}]}`)
	require.Error(t, err)
	_, err = app.ValidateArtifactViewPage("orders", `{"title":"T","components":[{"id":"t1","type":"table","query":"nope"}]}`)
	require.ErrorContains(t, err, "query", "a query the manifest does not have")
	_, err = app.ValidateArtifactViewPage("orders", `not json`)
	require.Error(t, err)
	_, err = app.ValidateArtifactViewPage("missing", `{}`)
	require.Error(t, err)
}
