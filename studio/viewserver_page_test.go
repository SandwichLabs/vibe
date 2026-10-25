package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

const pageManifest = studioArtifactManifest + "view: html\n"

const pageIndex = `<!doctype html><html><head><meta charset="utf-8"><title>Orders</title>
<link rel="stylesheet" href="assets/style.css"></head><body>
<h1>min {{ param "min_qty" }}</h1>
<table>{{ range (query "big").Rows }}<tr><td>{{ index . 0 }}</td><td>{{ index . 1 }}</td></tr>{{ end }}</table>
<a href="{{ link "min_qty" 50 }}">fifty</a>
</body></html>`

// writePage writes artifacts/<name>/ as a static page (extra files by path under html/).
func writePage(t *testing.T, app *App, name, index string, extra map[string]string) {
	t.Helper()
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, name)
	require.NoError(t, os.RemoveAll(filepath.Join(dir, artifacts.HTMLDir)))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, artifacts.HTMLDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(pageManifest), 0o644))
	files := map[string]string{"index.html.tmpl": index}
	for p, s := range extra {
		files[p] = s
	}
	for p, s := range files {
		full := filepath.Join(dir, artifacts.HTMLDir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(s), 0o644))
	}
}

// allowedPage writes, allows and opens a page. The re-check runs on every
// request so a test sees a change at once.
func allowedPage(t *testing.T, app *App, name, index string, extra map[string]string) (string, http.Handler) {
	t.Helper()
	writePage(t, app, name, index, extra)
	st, err := app.ArtifactViewState(name)
	require.NoError(t, err)
	require.Empty(t, st.Error)
	_, err = app.AllowArtifactView(name, st.Hash)
	require.NoError(t, err)
	app.views.verdictTTL = 0
	g, err := app.OpenArtifactPage(name, testOrigin)
	require.NoError(t, err)
	return g.Token, app.views.Middleware()(http.NotFoundHandler())
}

func pageApp(t *testing.T) *App {
	t.Helper()
	app := newOpenApp(t)
	_, err := app.sess.DB().Exec("CREATE TABLE t (id INTEGER, qty INTEGER)")
	require.NoError(t, err)
	_, err = app.sess.DB().Exec("INSERT INTO t VALUES (1, 5), (2, 50), (3, 500)")
	require.NoError(t, err)
	return app
}

func wantPageCSP(tok string) string {
	a := testOrigin + "/vibe-view/" + tok + "/assets/"
	return "sandbox; default-src 'none'; script-src 'none'; style-src " + a + " 'unsafe-inline'; img-src " + a + " data:; font-src " + a +
		"; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"
}

func TestStaticPage_RendersWithH3sExactHeaders(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, map[string]string{"assets/style.css": "body{margin:0}"})
	r := get(h, http.MethodGet, "/vibe-view/"+tok+"/page")
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Equal(t, wantPageCSP(tok), r.Header.Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "no-store", r.Header.Get("Cache-Control"))
	assert.Equal(t, "text/html; charset=utf-8", r.Header.Get("Content-Type"))
	out := body(t, r)
	assert.Contains(t, out, "<td>1</td><td>5</td>")
	assert.Contains(t, out, "<td>3</td><td>500</td>")
	assert.Contains(t, out, "<h1>min 0</h1>")
	assert.Contains(t, out, `href="page?min_qty=50"`)

	a := get(h, http.MethodGet, "/vibe-view/"+tok+"/assets/style.css")
	require.Equal(t, http.StatusOK, a.StatusCode)
	assert.Equal(t, "text/css; charset=utf-8", a.Header.Get("Content-Type"))
	assert.Equal(t, "body{margin:0}", body(t, a))
	assert.Equal(t, wantPageCSP(tok), a.Header.Get("Content-Security-Policy"))
}

func TestStaticPage_ParametersComeFromTheQueryStringThroughBind(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	out := body(t, get(h, http.MethodGet, "/vibe-view/"+tok+"/page?min_qty=50"))
	assert.Contains(t, out, "<td>2</td><td>50</td>")
	assert.NotContains(t, out, "<td>1</td><td>5</td>")
	assert.Contains(t, out, "<h1>min 50</h1>")

	for _, q := range []string{
		"min_qty=-5", "min_qty=abc", "min_qty=5.5", "min_qty=1&min_qty=2", "nope=1", "since=2026-13-45", "since=2026-01-01'%20OR%201=1",
		"min_qty=1;DROP", "min_qty=%00", "since=" + strings.Repeat("a", 100),
	} {
		r := get(h, http.MethodGet, "/vibe-view/"+tok+"/page?"+q)
		assert.Equal(t, http.StatusUnprocessableEntity, r.StatusCode, q)
		assert.Equal(t, wantPageCSP(tok), r.Header.Get("Content-Security-Policy"), q)
		assert.Contains(t, body(t, r), "A parameter value is not valid", q)
	}
	r := get(h, http.MethodGet, "/vibe-view/"+tok+"/page?x="+strings.Repeat("a", 9000))
	assert.Equal(t, http.StatusRequestURITooLong, r.StatusCode)
	assert.NotEmpty(t, r.Header.Get("Content-Security-Policy"))
}

func TestStaticPage_EveryResponseCarriesTheSandboxCSP(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, map[string]string{"assets/style.css": "x", "assets/d.json": "{}"})
	other := strings.Repeat("0", 64)
	targets := []string{
		"/vibe-view/" + tok + "/page", "/vibe-view/" + tok + "/page?min_qty=x", "/vibe-view/" + tok + "/page/",
		"/vibe-view/" + tok + "/index.html", "/vibe-view/" + tok + "/assets/", "/vibe-view/" + tok + "/assets/index.html",
		"/vibe-view/" + tok + "/assets/style.css", "/vibe-view/" + tok + "/assets/style.css?x=1", "/vibe-view/" + tok + "/assets/missing.css",
		"/vibe-view/" + tok + "/assets/d.json", "/vibe-view/" + tok + "/assets/../index.html.tmpl", "/vibe-view/" + tok + "/assets//style.css",
		"/vibe-view/" + tok + "/assets/index.html.tmpl", "/vibe-view/" + tok + "/other", "/vibe-view/" + tok + "/view.js",
		"/vibe-view/" + tok + "/worker.js", "/vibe-view/" + other + "/page", "/vibe-view/" + other + "/assets/style.css",
		"/vibe-view/nothex/page", "/vibe-view/" + tok + "/page/x", "/vibe-view/" + tok, "/vibe-view/",
	}
	for _, target := range targets {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			r := get(h, method, target)
			requireHardened(t, r, method+" "+target)
			if strings.Contains(target, "/assets/") || strings.HasSuffix(target, "/page") || strings.Contains(target, "/page?") {
				assert.True(t, strings.HasPrefix(r.Header.Get("Content-Security-Policy"), "sandbox; "), method+" "+target+": "+r.Header.Get("Content-Security-Policy"))
			}
		}
	}
	// The worker and view.js routes never answer for a page's token.
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/view.js").StatusCode)
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/worker.js").StatusCode)
}

func TestStaticPage_PathsThatInjectTheWailsRuntimeAreBadRequest(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	for _, target := range []string{"/vibe-view/" + tok + "/page/", "/vibe-view/" + tok + "/index.html", "/vibe-view/" + tok + "/assets/", "/vibe-view/" + tok + "/assets/a/index.html", "/vibe-view/" + tok + "/"} {
		assert.Equal(t, http.StatusBadRequest, get(h, http.MethodGet, target).StatusCode, target)
	}
}

func TestStaticPage_ATokenIsBoundToItsArtifactNotThePath(t *testing.T) {
	app := pageApp(t)
	a, h := allowedPage(t, app, "alpha", strings.Replace(pageIndex, "Orders", "ALPHA", 1), map[string]string{"assets/style.css": "A-CSS"})
	b, _ := allowedPage(t, app, "beta", strings.Replace(pageIndex, "Orders", "BETA", 1)+"<p>BETA-ONLY</p>", map[string]string{"assets/style.css": "B-CSS", "assets/b.svg": `<svg xmlns="http://www.w3.org/2000/svg"/>`})
	assert.NotEqual(t, a, b)
	assert.Equal(t, "A-CSS", body(t, get(h, http.MethodGet, "/vibe-view/"+a+"/assets/style.css")))
	assert.Equal(t, "B-CSS", body(t, get(h, http.MethodGet, "/vibe-view/"+b+"/assets/style.css")))
	for _, target := range []string{
		"/vibe-view/" + a + "/../" + b + "/page", "/vibe-view/" + a + "/" + b + "/page", "/vibe-view/" + a + "/assets/b.svg",
		"/vibe-view/" + a + "/assets/../../" + b + "/assets/style.css", "/vibe-view/" + a + "/page/" + b,
		"/vibe-view/" + a + "/assets/%2e%2e/" + b + "/page",
	} {
		r := get(h, http.MethodGet, target)
		assert.GreaterOrEqual(t, r.StatusCode, 400, target)
		o := body(t, r)
		assert.NotContains(t, o, "BETA", target)
		assert.NotContains(t, o, "B-CSS", target)
	}
	assert.NotContains(t, body(t, get(h, http.MethodGet, "/vibe-view/"+a+"/page")), "BETA")
	assert.Contains(t, body(t, get(h, http.MethodGet, "/vibe-view/"+b+"/page")), "BETA-ONLY")
}

func TestStaticPage_AssetsServeOnlyFilesUnderAssets(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, map[string]string{
		"assets/style.css": "css", "assets/logo.svg": `<svg xmlns="http://www.w3.org/2000/svg"/>`, "assets/data.json": "{}", "assets/sub/x.css": "sub",
	})
	for path, typ := range map[string]string{
		"style.css": "text/css; charset=utf-8", "logo.svg": "image/svg+xml", "data.json": "application/json; charset=utf-8", "sub/x.css": "text/css; charset=utf-8",
	} {
		r := get(h, http.MethodGet, "/vibe-view/"+tok+"/assets/"+path)
		require.Equal(t, http.StatusOK, r.StatusCode, path)
		assert.Equal(t, typ, r.Header.Get("Content-Type"))
	}
	for _, path := range []string{"index.html.tmpl", "../artifact.yaml", "../../artifacts/pg/artifact.yaml", "sub", "nope.css", ".hidden.css"} {
		r := get(h, http.MethodGet, "/vibe-view/"+tok+"/assets/"+path)
		assert.GreaterOrEqual(t, r.StatusCode, 400, path)
		assert.NotContains(t, body(t, r), "min_qty", path)
	}
}

func TestStaticPage_ASubResourceRequestDoesNotRender(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	for _, c := range []struct{ dest, mode string }{{"image", "no-cors"}, {"style", "no-cors"}, {"script", "no-cors"}, {"font", "cors"}, {"empty", "cors"}, {"object", "no-cors"}, {"", "cors"}} {
		req := httptest.NewRequest(http.MethodGet, "/vibe-view/"+tok+"/page", nil)
		if c.dest != "" {
			req.Header.Set("Sec-Fetch-Dest", c.dest)
		}
		req.Header.Set("Sec-Fetch-Mode", c.mode)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		r := rec.Result()
		assert.Equal(t, http.StatusForbidden, r.StatusCode, c.dest+"/"+c.mode)
		assert.Equal(t, wantPageCSP(tok), r.Header.Get("Content-Security-Policy"))
		assert.NotContains(t, body(t, r), "<td>", c.dest)
	}
	for _, dest := range []string{"iframe", "frame", "document", ""} {
		req := httptest.NewRequest(http.MethodGet, "/vibe-view/"+tok+"/page", nil)
		if dest != "" {
			req.Header.Set("Sec-Fetch-Dest", dest)
			req.Header.Set("Sec-Fetch-Mode", "navigate")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, dest)
	}
}

func TestStaticPage_RenderRateLimit(t *testing.T) {
	var l renderLimiter
	now := time.Now()
	for i := 0; i < maxRendersInFlight; i++ {
		require.True(t, l.acquire(now))
	}
	assert.False(t, l.acquire(now), "4 in flight")
	l.release()
	assert.True(t, l.acquire(now))
	for i := 0; i < 4; i++ {
		l.release()
	}
	// 60 a minute: starts so far are 5; fill to 60 spaced inside the minute.
	for i := 5; i < maxRendersPerMin; i++ {
		require.True(t, l.acquire(now.Add(time.Duration(i)*time.Second/2)))
		l.release()
	}
	assert.False(t, l.acquire(now.Add(30*time.Second)), "60 a minute")
	assert.True(t, l.acquire(now.Add(61*time.Second)), "the window slides")
}

func TestStaticPage_FourInFlightThenRefusedOverHTTP(t *testing.T) {
	vs, h, _ := testViewServer(func(*viewGrant) bool { return true })
	vs.verdictTTL = 0
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	m, err := artifacts.Decode([]byte(pageManifest))
	require.NoError(t, err)
	page := &artifacts.Artifact{Name: "pg", Manifest: *m, Files: []artifacts.PageFile{{Path: "index.html.tmpl", Data: []byte(`{{ query "big" }}ok`)}}, Page: &artifacts.PageInfo{Kind: artifacts.PageStatic}}
	run := func(ctx context.Context, _ string, _ map[string]any) (*artifacts.PageRows, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &artifacts.PageRows{}, nil
	}
	tok, err := vs.issue(&viewGrant{name: "pg", hash: "h", origin: testOrigin, page: page, run: run})
	require.NoError(t, err)

	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < maxRendersInFlight; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode }()
	}
	for i := 0; i < maxRendersInFlight; i++ {
		<-started
	}
	r := get(h, http.MethodGet, "/vibe-view/"+tok+"/page")
	assert.Equal(t, http.StatusTooManyRequests, r.StatusCode)
	assert.Equal(t, wantPageCSP(tok), r.Header.Get("Content-Security-Policy"))
	assert.Equal(t, "5", r.Header.Get("Retry-After"))
	close(release)
	wg.Wait()
	close(codes)
	for c := range codes {
		assert.Equal(t, http.StatusOK, c)
	}
	assert.Equal(t, http.StatusOK, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode, "the slots come back")
}

func TestStaticPage_HostileRowValuesRenderAsText(t *testing.T) {
	app := pageApp(t)
	_, err := app.sess.DB().Exec("CREATE TABLE h (s VARCHAR)")
	require.NoError(t, err)
	_, err = app.sess.DB().Exec(`INSERT INTO h VALUES ('<img src=x onerror=alert(1)>'), ('</td><script>alert(2)</script>'), ('" onmouseover="alert(3)'), ('javascript:alert(4)')`)
	require.NoError(t, err)
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "hostile")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, artifacts.HTMLDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte("version: 1\ntitle: H\nview: html\nqueries:\n  - name: rows\n    sql: SELECT s FROM h\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.HTMLDir, "index.html.tmpl"),
		[]byte(`<table>{{ range (query "rows").Rows }}<tr><td title="{{ index . 0 }}">{{ index . 0 }}</td></tr>{{ end }}</table>`), 0o644))
	st, err := app.ArtifactViewState("hostile")
	require.NoError(t, err)
	require.Empty(t, st.Error)
	_, err = app.AllowArtifactView("hostile", st.Hash)
	require.NoError(t, err)
	g, err := app.OpenArtifactPage("hostile", testOrigin)
	require.NoError(t, err)
	h := app.views.Middleware()(http.NotFoundHandler())
	out := body(t, get(h, http.MethodGet, "/vibe-view/"+g.Token+"/page"))
	assert.NotContains(t, out, "<img")
	assert.NotContains(t, out, "<script")
	assert.NotContains(t, out, `" onmouseover`)
	assert.Contains(t, out, "&lt;img src=x onerror=alert(1)&gt;")
	assert.Contains(t, out, "&lt;/td&gt;&lt;script&gt;alert(2)&lt;/script&gt;")
	assert.Contains(t, out, "javascript:alert(4)</td>", "as text, in a place a browser does not run it")
}

func TestStaticPage_AnUnboundedLoopInAVettedPageEndsFast(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "loop", `<p>before</p>{{ range 3000000000 }}{{ end }}`, nil)
	start := time.Now()
	r := get(h, http.MethodGet, "/vibe-view/"+tok+"/page")
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.Equal(t, http.StatusGatewayTimeout, r.StatusCode)
	assert.Equal(t, wantPageCSP(tok), r.Header.Get("Content-Security-Policy"))
	out := body(t, r)
	assert.NotContains(t, out, "before", "no partial page")
	assert.Contains(t, out, "took too long")
}

func TestStaticPage_AFailingQueryIsAFixedBlockNeverThePolicyReason(t *testing.T) {
	app := pageApp(t)
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "bad")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, artifacts.HTMLDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte("version: 1\ntitle: B\nview: html\nqueries:\n  - name: gone\n    sql: SELECT * FROM no_such_table_zz\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.HTMLDir, "index.html.tmpl"), []byte(`{{ query "gone" }}`), 0o644))
	st, _ := app.ArtifactViewState("bad")
	_, err := app.AllowArtifactView("bad", st.Hash)
	require.NoError(t, err)
	g, err := app.OpenArtifactPage("bad", testOrigin)
	require.NoError(t, err)
	r := get(app.views.Middleware()(http.NotFoundHandler()), http.MethodGet, "/vibe-view/"+g.Token+"/page")
	assert.Equal(t, http.StatusInternalServerError, r.StatusCode)
	out := body(t, r)
	assert.Contains(t, out, "could not run")
	assert.NotContains(t, out, "no_such_table_zz")
	assert.NotContains(t, out, app.ws.Root())
	assert.Equal(t, wantPageCSP(g.Token), r.Header.Get("Content-Security-Policy"))
}

func TestStaticPage_ServesWhatWasHashed(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, map[string]string{"assets/style.css": "ORIGINAL"})
	require.Equal(t, http.StatusOK, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)

	// Changed on disk after the token was issued: refused (never the new bytes
	// and never a mix), and the token does not come back by itself.
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "pg", artifacts.HTMLDir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "style.css"), []byte("EVIL"), 0o644))
	for _, target := range []string{"/page", "/assets/style.css"} {
		r := get(h, http.MethodGet, "/vibe-view/"+tok+target)
		assert.Equal(t, http.StatusNotFound, r.StatusCode, target)
		assert.NotContains(t, body(t, r), "EVIL")
		assert.NotContains(t, body(t, r), "ORIGINAL")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "style.css"), []byte("ORIGINAL"), 0o644))
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
}

func TestStaticPage_ARevokeOrDenyEndsTheTokenAtOnceEvenWithTheVerdictCached(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	app.views.verdictTTL = time.Minute
	require.Equal(t, http.StatusOK, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
	_, err := app.RevokeArtifactView("pg")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
}

func TestStaticPage_AnEditIsNoticedAfterTheVerdictTTL(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	app.views.verdictTTL = 50 * time.Millisecond
	require.Equal(t, http.StatusOK, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
	writePage(t, app, "pg", pageIndex+"<p>edit</p>", nil)
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
}

func TestStaticPage_ABodyRenderedWhileTheFilesChangedIsNotDelivered(t *testing.T) {
	vs, h, _ := testViewServer(nil)
	vs.verdictTTL = 0
	allowed := true
	vs.stillAllowed = func(*viewGrant) bool { return allowed }
	m, _ := artifacts.Decode([]byte(pageManifest))
	page := &artifacts.Artifact{Name: "pg", Manifest: *m, Files: []artifacts.PageFile{{Path: "index.html.tmpl", Data: []byte(`{{ query "big" }}SECRET-ROWS`)}}, Page: &artifacts.PageInfo{Kind: artifacts.PageStatic}}
	run := func(context.Context, string, map[string]any) (*artifacts.PageRows, error) {
		allowed = false // the person revokes while the query runs
		return &artifacts.PageRows{}, nil
	}
	tok, _ := vs.issue(&viewGrant{name: "pg", origin: testOrigin, page: page, run: run})
	r := get(h, http.MethodGet, "/vibe-view/"+tok+"/page")
	assert.Equal(t, http.StatusNotFound, r.StatusCode)
	assert.NotContains(t, body(t, r), "SECRET")
}

func TestOpenArtifactPage_RefusesAScriptedPageAndAScriptView(t *testing.T) {
	app := pageApp(t)
	writePage(t, app, "sc", pageIndex+`<script src="app.js"></script>`, map[string]string{"app.js": "1"})
	st, err := app.ArtifactViewState("sc")
	require.NoError(t, err)
	require.Empty(t, st.Error)
	assert.Equal(t, "scripted", st.PageKind)
	_, err = app.AllowArtifactView("sc", st.Hash)
	require.NoError(t, err)
	_, err = app.OpenArtifactPage("sc", testOrigin)
	require.ErrorIs(t, err, errPageScripted)

	writeStudioScript(t, app, "chart", studioScriptSource)
	st, _ = app.ArtifactViewState("chart")
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	_, err = app.OpenArtifactPage("chart", testOrigin)
	require.ErrorIs(t, err, errViewNotAllowed, "a script view has no page")
	g, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	r := get(app.views.Middleware()(http.NotFoundHandler()), http.MethodGet, "/vibe-view/"+g.Token+"/page")
	assert.Equal(t, http.StatusNotFound, r.StatusCode, "a script token renders no page")

	_, err = app.OpenArtifactPage("sc", "http://x; script-src *")
	require.Error(t, err)
	_, err = NewApp(testLogger()).OpenArtifactPage("sc", testOrigin)
	require.Error(t, err, "no workspace")
}

func TestOpenArtifactPage_AnotherWorkspacesGrantIsDropped(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	app.views.clear()
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
}

func TestStaticPage_QueriesRunTheHashedManifestNotTheFileOnDisk(t *testing.T) {
	app := pageApp(t)
	tok, h := allowedPage(t, app, "pg", pageIndex, nil)
	// A pull rewrites the query's SQL after the token was issued. With the verdict
	// cached the page is still served, and it must still run the SQL that was hashed.
	app.views.verdictTTL = time.Minute
	require.Equal(t, http.StatusOK, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
	app.views.mu.Lock()
	for _, g := range app.views.grants {
		g.okUntil = time.Now().Add(time.Minute)
	}
	app.views.mu.Unlock()
	path := filepath.Join(app.ws.Root(), artifacts.Dir, "pg", artifacts.ManifestFile)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(pageManifest, "qty >= $min_qty", "qty < 0", 1)), 0o644))
	out := body(t, get(h, http.MethodGet, "/vibe-view/"+tok+"/page"))
	assert.Contains(t, out, "<td>3</td><td>500</td>", "the held SQL ran, not the edited one (which returns no rows)")
	assert.NotContains(t, out, "qty &lt; 0")
	// Once the verdict is rechecked, the edit is seen and the page is refused.
	app.views.verdictTTL = 0
	assert.Equal(t, http.StatusNotFound, get(h, http.MethodGet, "/vibe-view/"+tok+"/page").StatusCode)
}
