package artifacts

import (
	"bytes"
	"context"
	"errors"
	"html"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunner answers a page's queries from a table and counts the runs.
type fakeRunner struct {
	rows   map[string]*PageRows
	err    error
	runs   map[string]int
	values []map[string]any
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{rows: map[string]*PageRows{
		"by_region": {Columns: []string{"region", "revenue"}, Rows: [][]any{{"EU", int64(1200)}, {"US", int64(3400)}}},
		"regions":   {Columns: []string{"region"}, Rows: [][]any{{"EU"}, {"US"}}},
	}, runs: map[string]int{}}
}

func (f *fakeRunner) run(_ context.Context, query string, values map[string]any) (*PageRows, error) {
	f.runs[query]++
	f.values = append(f.values, values)
	if f.err != nil {
		return nil, f.err
	}
	r, ok := f.rows[query]
	if !ok {
		return nil, errors.New("no rows")
	}
	return r, nil
}

// pageOf builds a loaded page without the save-time vet, so a test can render
// markup the vet would refuse (the renderer must hold on its own).
func pageOf(t *testing.T, kind string, files map[string]string) *Artifact {
	t.Helper()
	return &Artifact{Name: "pg", Manifest: *pageManifest(t, ""), Files: toFiles(files), Page: &PageInfo{Kind: kind, Entry: DefaultEntry}}
}

func renderOf(t *testing.T, a *Artifact, f *fakeRunner, values map[string]any) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := RenderHTMLPage(context.Background(), f.run, a, values, &out)
	return out.String(), err
}

func entry(tmpl string) map[string]string { return map[string]string{"index.html.tmpl": tmpl} }

func TestRender_GoodPageRunsEachQueryOnceAndLazily(t *testing.T) {
	f := newFakeRunner()
	info, err := VetPage(pageManifest(t, ""), toFiles(goodPageFiles()))
	require.NoError(t, err)
	a := &Artifact{Name: "pg", Manifest: *pageManifest(t, ""), Files: toFiles(goodPageFiles()), Page: info}
	out, err := renderOf(t, a, f, map[string]any{"top": int64(5)})
	require.NoError(t, err)
	assert.Contains(t, out, "<td>EU</td><td>1,200</td>")
	assert.Contains(t, out, "<h1>5 regions</h1>")
	assert.Contains(t, out, `href="page?top=5"`)
	assert.Equal(t, map[string]int{"by_region": 1, "regions": 1}, f.runs, "once per query, and only the ones the page asks for")
	for _, v := range f.values {
		assert.Equal(t, map[string]any{"top": int64(5)}, v, "the runner gets the supplied values, which Bind checks again")
	}
}

func TestRender_AQueryTheTemplateNeverCallsNeverRuns(t *testing.T) {
	f := newFakeRunner()
	out, err := renderOf(t, pageOf(t, PageStatic, entry(`<p>{{ if false }}{{ query "regions" }}{{ end }}hello</p>`)), f, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "hello")
	assert.Empty(t, f.runs)
}

func TestRender_HostileRowValuesComeOutAsText(t *testing.T) {
	f := newFakeRunner()
	f.rows["by_region"] = &PageRows{Columns: []string{"a"}, Rows: [][]any{
		{`<img src=x onerror=alert(1)>`}, {`</td><script>alert(2)</script>`}, {`" onmouseover="alert(3)`}, {`javascript:alert(4)`},
	}}
	a := pageOf(t, PageStatic, entry(`<table>{{ range (query "by_region").Rows }}<tr><td title="{{ index . 0 }}">{{ index . 0 }}</td></tr>{{ end }}</table>`+
		`<a href="{{ index (index (query "by_region").Rows 3) 0 }}">x</a>`))
	out, err := renderOf(t, a, f, nil)
	require.NoError(t, err)
	assert.NotContains(t, out, "<img")
	assert.NotContains(t, out, "<script")
	assert.NotContains(t, out, `" onmouseover`)
	assert.Contains(t, out, "&lt;img src=x onerror=alert(1)&gt;")
	assert.Contains(t, out, `href="#ZgotmplZ"`, "a javascript: URL from data is refused by html/template")
	assert.NotContains(t, out, `href="javascript`)
}

func TestRender_HostileValueInJSONBlockIsAJSONString(t *testing.T) {
	f := newFakeRunner()
	f.rows["by_region"] = &PageRows{Columns: []string{"a"}, Rows: [][]any{{`</script><script>alert(1)</script>`}, {" x"}}}
	a := pageOf(t, PageScripted, entry(`<script type="application/json">{{ json (query "by_region").Rows }}</script>`))
	out, err := renderOf(t, a, f, nil)
	require.NoError(t, err)
	assert.NotContains(t, out, "</script><script>alert")
	assert.Equal(t, 1, strings.Count(out, "</script>"), "the data cannot close the block")
	assert.Contains(t, out, `</script>`)
}

func TestRender_AStaticPageRefusesAScriptThatAppearsInTheOutput(t *testing.T) {
	// Not reachable through a vetted page; the output check is the second layer.
	f := newFakeRunner()
	_, err := renderOf(t, pageOf(t, PageStatic, entry(`<p>x</p><script>alert(1)</script>`)), f, nil)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderTemplate, re.Kind)
	_, err = renderOf(t, pageOf(t, PageStatic, entry(`<img src=x onerror="alert(1)">`)), f, nil)
	require.ErrorAs(t, err, &re)
}

func TestCheckRendered(t *testing.T) {
	ok := []string{
		`<a href="page?top=5&amp;region=EU">x</a><a href="page">y</a><a href="#end">z</a><img src="assets/a.png">`,
		`<p>&lt;script&gt;alert(1)&lt;/script&gt; onclick=1 href=http://x</p>`,
		`<link rel="stylesheet" href="assets/style.css"><a href="#ZgotmplZ">x</a>`,
		`<svg><a href="#x"><rect width="1"/></a></svg>`,
	}
	for _, s := range ok {
		assert.NoError(t, checkRendered([]byte(s), false, nil), s)
	}
	bad := []string{
		`<script>1</script>`, `<iframe src="assets/a.html"></iframe>`, `<p onclick="x">`, `<a href="http://x.test/">x</a>`,
		`<a href="//x.test/">x</a>`, `<a href="javascript:alert(1)">x</a>`, `<a href="/">x</a>`, `<a href="/index.html">x</a>`,
		`<a href="pagex">x</a>`, `<a target="_blank" href="#a">x</a>`, `<a ping="x" href="#a">x</a>`, `<link rel="preconnect" href="assets/x">`,
		`<meta http-equiv="refresh" content="0;url=/">`, `<base href="assets/">`, `<img srcset="assets/a.png 1x, http://x.test/b.png 2x">`,
		`<p style="background:url(http://x.test/a)">x</p>`, `<svg><script>1</script></svg>`, `<svg><a xlink:href="http://x.test/">x</a></svg>`,
		`<object data="assets/x.svg"></object>`, `<a download href="assets/a">x</a>`,
	}
	for _, s := range bad {
		assert.Error(t, checkRendered([]byte(s), false, nil), s)
	}
	assert.NoError(t, checkRendered([]byte(`<script>1</script><p onclick="x">`), true, nil), "a scripted page's own code is what makes it scripted")
	assert.Error(t, checkRendered([]byte(`<a href="http://x.test/">x</a>`), true, nil), "but nothing else loosens")

	// A scripted page's own script file and a vendored library may be loaded; nothing else may.
	files := map[string]bool{"app.js": true, "assets/x.js": true, "index.html.tmpl": true}
	assert.NoError(t, checkRendered([]byte(`<script src="app.js"></script><script src="vibe-lib/d3-array.js"></script>`), true, files))
	for _, bad := range []string{
		`<script src="missing.js"></script>`, `<script src="http://x.test/a.js"></script>`, `<script src="vibe-lib/../x.js"></script>`,
		`<script src="vibe-lib/x.css"></script>`, `<script src="//x.test/a.js"></script>`,
		`<img src="app.js">`, `<img src="vibe-lib/d3.js">`, `<script src="app.js?x=1"></script>`,
	} {
		assert.Error(t, checkRendered([]byte(bad), true, files), bad)
	}
	assert.Error(t, checkRendered([]byte(`<script src="app.js"></script>`), false, files), "a static page runs no script")
}

func TestRender_AFailingQueryIsAFixedBlockAndWritesNothing(t *testing.T) {
	f := newFakeRunner()
	f.err = errors.New("SECRET-POLICY-REASON /home/zac/vibe-workspace/orders.sql")
	var out bytes.Buffer
	err := RenderHTMLPage(context.Background(), f.run, pageOf(t, PageStatic, entry(`<p>before</p>{{ range (query "by_region").Rows }}x{{ end }}`)), nil, &out)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderQuery, re.Kind)
	assert.Equal(t, "by_region", re.Query)
	assert.Empty(t, out.String(), "no partial document")
	assert.NotContains(t, err.Error(), "SECRET")
	block := string(PageErrorHTML(err))
	assert.NotContains(t, block, "SECRET")
	assert.NotContains(t, block, "/home")
	assert.Contains(t, block, `The query &#34;by_region&#34; could not run.`)
	assert.NoError(t, checkRendered([]byte(block), false, nil), "the error block holds the page rules too")
}

func TestPageErrorHTML_FixedTextPerKind(t *testing.T) {
	for kind, want := range map[RenderKind]string{
		RenderBadValues: "A parameter value is not valid", RenderTooLarge: "10 MB", RenderTooSlow: "too long",
		RenderCanceled: "stopped", RenderTemplate: "template has an error",
	} {
		assert.Contains(t, string(PageErrorHTML(&RenderError{Kind: kind, cause: errors.New("LEAK")})), want)
		assert.NotContains(t, string(PageErrorHTML(&RenderError{Kind: kind, cause: errors.New("LEAK")})), "LEAK")
	}
	assert.Contains(t, string(PageErrorHTML(errors.New("LEAK"))), "could not be drawn")
	assert.NotContains(t, string(PageErrorHTML(errors.New("LEAK"))), "LEAK")
}

func TestRender_ParametersAreCheckedBeforeAnyQuery(t *testing.T) {
	f := newFakeRunner()
	a := pageOf(t, PageStatic, entry(`{{ query "by_region" }}`))
	for _, bad := range []map[string]any{{"top": int64(1000)}, {"top": "5"}, {"nope": "x"}, {"region": 3}} {
		var out bytes.Buffer
		err := RenderHTMLPage(context.Background(), f.run, a, bad, &out)
		var re *RenderError
		require.ErrorAs(t, err, &re, "%v", bad)
		assert.Equal(t, RenderBadValues, re.Kind, "%v", bad)
	}
	assert.Empty(t, f.runs)
}

func TestParsePageQuery(t *testing.T) {
	m := pageManifest(t, "")
	got, err := ParsePageQuery(m, url.Values{"top": {"5"}, "share": {"0.25"}, "live": {"false"}, "note": {"a&b c"}, "from": {"2026-01-02"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"top": int64(5), "share": 0.25, "live": false, "note": "a&b c", "from": "2026-01-02"}, got)

	for name, q := range map[string]url.Values{
		"unknown":  {"nope": {"1"}},
		"twice":    {"top": {"1", "2"}},
		"not int":  {"top": {"5.5"}},
		"not num":  {"share": {"abc"}},
		"not bool": {"live": {"maybe"}},
		"hex":      {"top": {"0x10"}},
	} {
		_, err := ParsePageQuery(m, q)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
	// Limits are Bind's job, run again on every query.
	vals, err := ParsePageQuery(m, url.Values{"top": {"1000"}})
	require.NoError(t, err)
	_, _, _, err = Bind(m, "by_region", vals, time.Now())
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestRender_LinkOnlyEverMakesThisRoute(t *testing.T) {
	f := newFakeRunner()
	a := pageOf(t, PageStatic, entry(`<a href="{{ link "note" "a&b c/../x" "top" 7 }}">1</a><a href="{{ link }}">2</a>`))
	out, err := renderOf(t, a, f, map[string]any{"region": "EU"})
	require.NoError(t, err)
	first := html.UnescapeString(strings.Split(strings.Split(out, `href="`)[1], `"`)[0])
	u, perr := url.Parse(first)
	require.NoError(t, perr)
	assert.Empty(t, u.Scheme+u.Host, "no scheme and no host: only this route")
	assert.Equal(t, "page", u.Path)
	assert.Equal(t, "a&b c/../x", u.Query().Get("note"))
	assert.Equal(t, "7", u.Query().Get("top"))
	assert.Equal(t, "EU", u.Query().Get("region"), "the current values stay")
	assert.Contains(t, out, `href="page?region=EU"`)

	for _, call := range []string{`link "top" 1000`, `link "nope" 1`, `link "top"`, `link "top" (query "regions")`, `link 1 2`} {
		_, err := renderOf(t, pageOf(t, PageStatic, entry(`<a href="{{ `+call+` }}">x</a>`)), f, nil)
		assert.Error(t, err, call)
	}
}

func TestRender_OutputCap(t *testing.T) {
	f := newFakeRunner()
	big := make([][]any, 20)
	for i := range big {
		big[i] = []any{"x"}
	}
	f.rows["by_region"] = &PageRows{Columns: []string{"a"}, Rows: big}
	var out bytes.Buffer
	err := RenderHTMLPage(context.Background(), f.run, pageOf(t, PageStatic, entry(`{{ range (query "by_region").Rows }}<p>{{ printf "%1000000s" "x" }}</p>{{ end }}`)), nil, &out)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderTooLarge, re.Kind)
	assert.Zero(t, out.Len())
}

func TestRender_LoopsAreBoundedByTheBudgetNotByLuck(t *testing.T) {
	f := newFakeRunner()
	f.rows["regions"] = &PageRows{Columns: []string{"n"}, Rows: [][]any{{int64(3_000_000_000)}}}
	f.rows["by_region"].Rows = [][]any{{"a", int64(1)}}
	cases := map[string]string{
		"empty range over a constant":       `{{ range 3000000000 }}{{ end }}`,
		"constant with variables":           `{{ range $i := 3000000000 }}{{ end }}`,
		"nested constants":                  `{{ range 2000 }}{{ range 2000 }}{{ range 2000 }}{{ end }}{{ end }}{{ end }}`,
		"a count from data":                 `{{ range (index (index (query "regions").Rows 0) 0) }}{{ end }}`,
		"a loop inside an if inside a with": `{{ with 1 }}{{ if true }}{{ range 3000000000 }}{{ end }}{{ end }}{{ end }}`,
		"in the else of a range":            `{{ range 0 }}{{ else }}{{ range 3000000000 }}{{ end }}{{ end }}`,
	}
	for name, tmpl := range cases {
		start := time.Now()
		_, err := renderOf(t, pageOf(t, PageStatic, entry(tmpl)), f, nil)
		var re *RenderError
		require.ErrorAs(t, err, &re, name)
		assert.Equal(t, RenderTooSlow, re.Kind, name)
		assert.Less(t, time.Since(start), 3*time.Second, name)
	}
}

func TestRender_ARangeStillWorksNormally(t *testing.T) {
	f := newFakeRunner()
	out, err := renderOf(t, pageOf(t, PageStatic, entry(
		`{{ range $i, $r := (query "regions").Rows }}{{ if $i }},{{ end }}{{ index $r 0 }}{{ else }}none{{ end }}|{{ range 3 }}{{ . }}{{ end }}|{{ range (query "regions").Rows }}{{ break }}{{ end }}`)), f, nil)
	require.NoError(t, err)
	assert.Equal(t, "EU,US|012|", out)
}

func TestRender_TemplateCallsAreBoundedAndRecursionIsRefused(t *testing.T) {
	f := newFakeRunner()
	// A chain that doubles at every level never loops, and would never finish.
	files := entry(`{{ template "l0" }}`)
	var defs strings.Builder
	for i := 0; i < 60; i++ {
		defs.WriteString(`{{ define "l` + string(rune('0'+i/10)) + string(rune('0'+i%10)) + `" }}{{ template "l` + string(rune('0'+(i+1)/10)) + string(rune('0'+(i+1)%10)) + `" }}{{ template "l` + string(rune('0'+(i+1)/10)) + string(rune('0'+(i+1)%10)) + `" }}{{ end }}`)
	}
	defs.WriteString(`{{ define "l60" }}x{{ end }}`)
	files["index.html.tmpl"] = `{{ template "l00" }}`
	files["defs.tmpl"] = defs.String()
	start := time.Now()
	_, err := renderOf(t, pageOf(t, PageStatic, files), f, nil)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderTooSlow, re.Kind)
	assert.Less(t, time.Since(start), 3*time.Second)

	for name, files := range map[string]map[string]string{
		"direct":   {"index.html.tmpl": `{{ define "a" }}{{ template "a" }}{{ end }}{{ template "a" }}`},
		"indirect": {"index.html.tmpl": `{{ define "a" }}{{ template "b" }}{{ end }}{{ define "b" }}{{ template "a" }}{{ end }}{{ template "a" }}`},
		"entry":    {"index.html.tmpl": `{{ template "index.html.tmpl" }}`},
	} {
		_, err := renderOf(t, pageOf(t, PageStatic, files), f, nil)
		require.ErrorAs(t, err, &re, name)
		assert.Equal(t, RenderTemplate, re.Kind, name)
	}
}

func TestRender_TemplatesCannotNameTheGuards(t *testing.T) {
	f := newFakeRunner()
	for _, tmpl := range []string{`{{ vibeRange__ 1 }}`, `{{ $x := vibeStep__ }}`} {
		_, err := renderOf(t, pageOf(t, PageStatic, entry(tmpl)), f, nil)
		var re *RenderError
		require.ErrorAs(t, err, &re, tmpl)
		assert.Equal(t, RenderTemplate, re.Kind, tmpl)
	}
	// And the save-time vet already refuses them, being unknown functions.
	_, err := vetIndex(t, `{{ vibeRange__ 1 }}`)
	require.Error(t, err)
}

func TestRender_ErrorsAreClassified(t *testing.T) {
	f := newFakeRunner()
	_, err := renderOf(t, pageOf(t, PageStatic, entry(`{{ .missing }}`)), f, nil)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderTemplate, re.Kind)
	_, err = renderOf(t, pageOf(t, PageStatic, entry(`{{ query "nope" }}`)), f, nil)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderQuery, re.Kind)
	_, err = renderOf(t, pageOf(t, PageStatic, entry(`{{ if }}`)), f, nil)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderTemplate, re.Kind)
	assert.NotContains(t, err.Error(), "{{")
}

func TestRender_CanceledAndTimedOut(t *testing.T) {
	f := newFakeRunner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	err := RenderHTMLPage(ctx, f.run, pageOf(t, PageStatic, entry(`{{ range (query "regions").Rows }}x{{ end }}`)), nil, &out)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Contains(t, []RenderKind{RenderCanceled, RenderQuery}, re.Kind)
	assert.Zero(t, out.Len())

	// A deadline that ends while a query runs stops the render as too slow.
	slow := func(ctx context.Context, _ string, _ map[string]any) (*PageRows, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer dcancel()
	err = RenderHTMLPage(dctx, slow, pageOf(t, PageStatic, entry(`{{ query "regions" }}`)), nil, &out)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderQuery, re.Kind, "the query that did not finish is named")
}

func TestRender_ParamAndParamsAreTheBoundValues(t *testing.T) {
	f := newFakeRunner()
	a := pageOf(t, PageStatic, entry(`{{ param "top" }}/{{ param "share" }}/{{ param "live" }}/{{ range params }}[{{ .Name }}:{{ .Type }}]{{ end }}`))
	out, err := renderOf(t, a, f, map[string]any{"top": int64(7)})
	require.NoError(t, err)
	assert.Equal(t, "7/0.5/true/[from:date][to:date][region:enum][top:integer][note:text][share:number][live:boolean]", out)
	_, err = renderOf(t, pageOf(t, PageStatic, entry(`{{ param "nope" }}`)), f, nil)
	assert.Error(t, err)
}

func TestRender_NoGoroutinesAreLeftBehind(t *testing.T) {
	var live atomic.Int32
	f := newFakeRunner()
	run := func(ctx context.Context, q string, v map[string]any) (*PageRows, error) {
		live.Add(1)
		defer live.Add(-1)
		return f.run(ctx, q, v)
	}
	var out bytes.Buffer
	for i := 0; i < 5; i++ {
		_ = RenderHTMLPage(context.Background(), run, pageOf(t, PageStatic, entry(`{{ range 3000000000 }}{{ end }}`)), nil, &out)
	}
	assert.Zero(t, live.Load())
}

func TestPageFromText_SealsTheHashOverTheBytes(t *testing.T) {
	files := toFiles(goodPageFiles())
	text := []byte(pageManifestText(""))
	a, err := PageFromText("pg", text, files)
	require.NoError(t, err)
	assert.Equal(t, PageStatic, a.Page.Kind)
	want, err := PageHash(text, files, a.Page)
	require.NoError(t, err)
	assert.Equal(t, want, a.Page.Hash)

	changed := append([]PageFile(nil), files...)
	changed[0] = PageFile{Path: changed[0].Path, Data: append([]byte("<!-- -->"), changed[0].Data...)}
	other, err := PageFromText("pg", text, goodMutated(files))
	require.NoError(t, err)
	assert.NotEqual(t, a.Page.Hash, other.Page.Hash)

	_, err = PageFromText("pg", []byte(goodManifest), files)
	require.Error(t, err, "not a page manifest")
	_, err = PageFromText("pg", text, []PageFile{{Path: "index.html.tmpl", Data: []byte(`<a href="http://x.test/">`)}})
	require.Error(t, err)
}

func goodMutated(files []PageFile) []PageFile {
	out := append([]PageFile(nil), files...)
	for i := range out {
		if out[i].Path == "assets/style.css" {
			out[i] = PageFile{Path: out[i].Path, Data: []byte("body{margin:0}")}
		}
	}
	return out
}

func TestRender_DeadlineAndCancelAreEnforcedInsideALoopBody(t *testing.T) {
	// Nothing here prints or builds a string, so only the per-action check can stop it: the loop is
	// 2 million long and before a check ran per action it ran for minutes past the deadline.
	body := `{{ range 1999000 }}` + strings.Repeat(`{{ $x := len (slice "abcdef" 1 4) }}`, 20) + `{{ end }}ok`
	f := newFakeRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	var out bytes.Buffer
	err := RenderHTMLPage(ctx, f.run, pageOf(t, PageStatic, entry(body)), nil, &out)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderTooSlow, re.Kind)
	assert.Less(t, time.Since(start), time.Second)
	assert.Zero(t, out.Len())

	// A cancel from outside (a revoke, a new token) stops it the same way.
	cctx, ccancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); ccancel() }()
	start = time.Now()
	err = RenderHTMLPage(cctx, f.run, pageOf(t, PageStatic, entry(body)), nil, &out)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderCanceled, re.Kind)
	assert.Less(t, time.Since(start), time.Second)

	// The same through if, with and nested template calls inside the loop.
	files := map[string]string{
		"index.html.tmpl": `{{ range 1999000 }}{{ if true }}{{ with 1 }}{{ template "w" }}{{ end }}{{ end }}{{ end }}`,
		"w.tmpl":          `{{ define "w" }}{{ printf "%0100000d" 1 }}{{ end }}`,
	}
	ctx2, c2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer c2()
	start = time.Now()
	err = RenderHTMLPage(ctx2, f.run, pageOf(t, PageStatic, files), nil, &out)
	require.ErrorAs(t, err, &re)
	assert.Less(t, time.Since(start), time.Second)
}

// memAlloc is the bytes allocated so far (cumulative), for bounding a render's memory.
func memAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

func TestRender_StringsBuiltInVariablesAreBudgeted(t *testing.T) {
	f := newFakeRunner()
	ten := func(v string) string { return `printf "` + strings.Repeat("%s", 10) + `"` + strings.Repeat(" "+v, 10) }
	chain := `{{ $a := printf "%01000000d" 1 }}{{ $b := ` + ten("$a") + ` }}{{ $c := ` + ten("$b") + ` }}{{ $d := ` + ten("$c") + ` }}{{ $e := ` + ten("$d") + ` }}{{ len $e }}`
	cases := map[string]string{
		"printf x10 chain":   chain,
		"print chain":        `{{ $a := printf "%01000000d" 1 }}{{ $b := print $a $a $a $a $a $a $a $a $a $a }}{{ $c := print $b $b $b $b $b $b $b $b $b $b }}{{ $d := print $c $c $c $c $c $c $c $c $c $c }}{{ len $d }}`,
		"println chain":      `{{ $a := printf "%01000000d" 1 }}{{ $b := println $a $a $a $a $a $a $a $a $a $a }}{{ $c := println $b $b $b $b $b $b $b $b $b $b }}{{ $d := println $c $c $c $c $c $c $c $c $c $c }}{{ len $d }}`,
		"js doubles":         `{{ $a := printf "%01000000d" 1 }}{{ $b := js $a $a $a $a $a $a $a $a $a $a }}{{ $c := js $b $b $b $b $b $b $b $b $b $b }}{{ len $c }}`,
		"html grows":         `{{ $a := printf "%0200000d" 1 }}{{ $b := html $a $a $a $a $a $a $a $a $a $a }}{{ $c := html $b $b $b $b $b $b $b $b $b $b }}{{ $d := html $c $c $c $c $c $c $c $c $c $c }}{{ len $d }}`,
		"urlquery grows":     `{{ $a := printf "%0200000d" 1 }}{{ $b := urlquery $a $a $a $a $a $a $a $a $a $a }}{{ $c := urlquery $b $b $b $b $b $b $b $b $b $b }}{{ len $c }}`,
		"many wide verbs":    `{{ len (printf "` + strings.Repeat("%999999d", 5000) + `" 1) }}`,
		"repeated arg index": `{{ $a := printf "%01000000d" 1 }}{{ len (printf "` + strings.Repeat("%[1]s", 1000) + `" $a) }}`,
		"extra arguments":    `{{ $a := printf "%01000000d" 1 }}{{ len (printf "x" ` + strings.Repeat("$a ", 500) + `) }}`,
		"slow accumulation":  `{{ range 200 }}{{ $x := printf "%01000000d" 1 }}{{ end }}`,
		"json of big data":   `{{ $a := printf "%01000000d" 1 }}{{ range 100 }}{{ $j := json (print $a $a) }}{{ end }}`,
	}
	for name, tmpl := range cases {
		before := memAlloc()
		start := time.Now()
		_, err := renderOf(t, pageOf(t, PageStatic, entry(tmpl)), f, nil)
		var re *RenderError
		require.ErrorAs(t, err, &re, name)
		assert.Equal(t, RenderTooLarge, re.Kind, name)
		assert.Contains(t, err.Error(), "too large", name)
		assert.Less(t, time.Since(start), 3*time.Second, name)
		assert.Less(t, memAlloc()-before, uint64(400<<20), name+": allocation stays bounded")
	}
}

func TestRender_MeteredFunctionsStillWorkNormally(t *testing.T) {
	f := newFakeRunner()
	out, err := renderOf(t, pageOf(t, PageStatic, entry(
		`{{ printf "%05.1f|%s|%d" 3.14159 "x" 7 }} {{ print "a" 1 "b" }} {{ js "<a>" }} {{ html "<b>" }} {{ urlquery "a b&c" }} {{ number 1234.5 1 }} {{ percent 0.25 }} {{ truncate "abcdef" 3 }} {{ bytes 2048 }} {{ date "2026-01-02" "short" }}`)), f, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "003.1|x|7")
	assert.Contains(t, out, "a1b")
	assert.Contains(t, out, "1,234.5")
	assert.Contains(t, out, "25.0%")
	assert.Contains(t, out, "ab…")
	assert.Contains(t, out, "2.0 KiB")
	assert.Contains(t, out, "Jan 2, 2026")
}

func TestRender_NonStringArgumentsAreSizedByWhatTheyHold(t *testing.T) {
	f := newFakeRunner()
	// One 5 MB cell is under the per-query cap; repeating the row set 200 times is not.
	f.rows["regions"] = &PageRows{Columns: []string{"x"}, Rows: [][]any{{strings.Repeat("x", 5_000_000)}}}
	refs := strings.Repeat("%[1]v", 200)
	args := strings.Repeat(" $r", 200)
	cases := map[string]string{
		"printf %[1]v x200":        `{{ $r := (query "regions").Rows }}{{ $x := printf "` + refs + `" $r }}{{ len $x }}`,
		"print x200":               `{{ $r := (query "regions").Rows }}{{ $x := print` + args + ` }}{{ len $x }}`,
		"println x200":             `{{ $r := (query "regions").Rows }}{{ $x := println` + args + ` }}{{ len $x }}`,
		"printf with one row":      `{{ $r := index (query "regions").Rows 0 }}{{ $x := printf "` + refs + `" $r }}{{ len $x }}`,
		"html of the row set":      `{{ $r := (query "regions").Rows }}{{ $x := html` + args + ` }}{{ len $x }}`,
		"js of a row x200":         `{{ $r := index (query "regions").Rows 0 }}{{ $x := js` + args + ` }}{{ len $x }}`,
		"extra args that are rows": `{{ $r := (query "regions").Rows }}{{ len (printf "x"` + args + `) }}`,
	}
	for name, tmpl := range cases {
		before := memAlloc()
		_, err := renderOf(t, pageOf(t, PageStatic, entry(tmpl)), f, nil)
		var re *RenderError
		require.ErrorAs(t, err, &re, name)
		assert.Equal(t, RenderTooLarge, re.Kind, name)
		assert.Less(t, memAlloc()-before, uint64(250<<20), name+": allocation stays bounded")
	}
}

func TestRender_SizingManyWideArgumentsIsBounded(t *testing.T) {
	f := newFakeRunner()
	// 500 rows of 100 integer cells: about 1.6 MB as sized, walked cell by cell.
	rows := make([][]any, 500)
	for i := range rows {
		rows[i] = make([]any, 100)
		for j := range rows[i] {
			rows[i][j] = i * j
		}
	}
	f.rows["regions"] = &PageRows{Columns: []string{"x"}, Rows: rows}
	args := strings.Repeat(" $r", 80_000)
	for name, tmpl := range map[string]string{
		"printf": `{{ $r := (query "regions").Rows }}{{ $x := printf "x"` + args + ` }}{{ len $x }}`,
		"print":  `{{ $r := (query "regions").Rows }}{{ $x := print` + args + ` }}{{ len $x }}`,
	} {
		start := time.Now()
		_, err := renderOf(t, pageOf(t, PageStatic, entry(tmpl)), f, nil)
		var re *RenderError
		require.ErrorAs(t, err, &re, name)
		assert.Equal(t, RenderTooLarge, re.Kind, name)
		assert.Less(t, time.Since(start), 3*time.Second, name+": the sizing walk stops at the budget")
	}
}

func TestRender_AQueryResultOverTheCapIsAFixedFailure(t *testing.T) {
	f := newFakeRunner()
	f.rows["regions"] = &PageRows{Columns: []string{"x"}, Rows: [][]any{{strings.Repeat("x", MaxQueryResultBytes+1)}}}
	_, err := renderOf(t, pageOf(t, PageStatic, entry(`{{ query "regions" }}`)), f, nil)
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, RenderQuery, re.Kind)
	assert.Equal(t, "regions", re.Query)
	assert.NotContains(t, err.Error(), "xxxx")
	f.rows["regions"] = &PageRows{Columns: []string{"x"}, Rows: [][]any{{strings.Repeat("x", 1<<20)}}}
	_, err = renderOf(t, pageOf(t, PageStatic, entry(`{{ len (index (index (query "regions").Rows 0) 0) }}`)), f, nil)
	require.NoError(t, err)
}
