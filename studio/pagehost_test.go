package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/pagehost"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// TestMain lets the test binary be the helper too: the live test (and anything
// that launches os.Executable()) starts it with --view-host.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == pagehost.HelperArg {
		os.Exit(runPageHelper())
	}
	if len(os.Args) > 1 && os.Args[1] == "--gate-probe" {
		os.Exit(gateProbe(os.Args[2:]))
	}
	os.Exit(m.Run())
}

const scriptedIndex = `<!doctype html><html><head><meta charset="utf-8"><title>Orders</title></head><body>
<h1>min {{ param "min_qty" }}</h1>
<script type="application/json" id="d">{{ json (query "big").Rows }}</script>
<a href="{{ link "min_qty" 50 }}">fifty</a>
<script src="app.js"></script></body></html>`

// fakeHelper is a helper that is a test: Studio's writes are parsed into got,
// and the test posts the page's side with send.
type fakeHelper struct {
	fromHelperR *io.PipeReader
	fromHelperW *io.PipeWriter
	toHelperR   *io.PipeReader
	toHelperW   *io.PipeWriter
	got         chan *pagehost.Message
	done        chan struct{}
	killed      atomic.Bool
	once        sync.Once
}

func newFakeHelper() *fakeHelper {
	f := &fakeHelper{got: make(chan *pagehost.Message, 4096), done: make(chan struct{})}
	f.fromHelperR, f.fromHelperW = io.Pipe()
	f.toHelperR, f.toHelperW = io.Pipe()
	go func() {
		lr := pagehost.NewLineReader(f.toHelperR, pagehost.ToHelper)
		for {
			line, err := lr.Next()
			if err != nil {
				return
			}
			if m, err := pagehost.Decode(line, pagehost.ToHelper); err == nil {
				f.got <- m
			}
		}
	}()
	return f
}

func (f *fakeHelper) Read(b []byte) (int, error)  { return f.fromHelperR.Read(b) }
func (f *fakeHelper) Write(b []byte) (int, error) { return f.toHelperW.Write(b) }
func (f *fakeHelper) Done() <-chan struct{}       { return f.done }
func (f *fakeHelper) Kill() {
	f.killed.Store(true)
	f.once.Do(func() {
		close(f.done)
		_ = f.fromHelperW.Close()
		_ = f.toHelperW.Close()
	})
}

// send posts one line as the helper.
func (f *fakeHelper) send(line string) { _, _ = io.WriteString(f.fromHelperW, line+"\n") }

// until reads Studio's messages until pred matches one.
func (f *fakeHelper) until(t *testing.T, pred func(*pagehost.Message) bool) *pagehost.Message {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-f.got:
			if pred(m) {
				return m
			}
		case <-timeout:
			t.Fatal("timed out waiting for a message to the helper")
		}
	}
}

func typed(typ string) func(*pagehost.Message) bool {
	return func(m *pagehost.Message) bool { return m.T == typ }
}

// events collects pagewindow:state events.
type events struct {
	mu  sync.Mutex
	all []map[string]any
}

func (e *events) add(name string, data ...any) {
	if name != "pagewindow:state" || len(data) != 1 {
		return
	}
	if m, ok := data[0].(map[string]any); ok {
		e.mu.Lock()
		e.all = append(e.all, m)
		e.mu.Unlock()
	}
}

func (e *events) states() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var s []string
	for _, m := range e.all {
		st, _ := m["state"].(string)
		s = append(s, st)
	}
	return s
}

func (e *events) waitFor(t *testing.T, state string) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, s := range e.states() {
			if s == state {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "state %s, saw %v", state, e.states())
}

// scriptedWindow writes, allows and opens a scripted page against a fake helper.
func scriptedWindow(t *testing.T) (*App, *fakeHelper, *events) {
	t.Helper()
	app := pageApp(t)
	writePage(t, app, "pg", scriptedIndex, map[string]string{"app.js": "window.x = 1", "assets/style.css": "body{margin:0}"})
	st, err := app.ArtifactViewState("pg")
	require.NoError(t, err)
	require.Equal(t, "scripted", st.PageKind)
	_, err = app.AllowArtifactView("pg", st.Hash)
	require.NoError(t, err)
	// The fixtures' own writes reach the watcher late and would end the window.
	if app.watchDone != nil {
		close(app.watchDone)
		app.watchDone = nil
	}
	if app.watcher != nil {
		_ = app.watcher.Close()
		app.watcher = nil
	}
	fh := newFakeHelper()
	ev := &events{}
	app.emit = ev.add
	app.pages.preflight = func() error { return nil }
	app.pages.launch = func(_ context.Context, _ *slog.Logger) (helperConn, error) { return fh, nil }
	t.Cleanup(app.pages.closeAll)
	return app, fh, ev
}

func TestPageWindow_OpenSendsThePinnedSetupInOrder(t *testing.T) {
	app, fh, _ := scriptedWindow(t)
	info, err := app.OpenArtifactPageWindow("pg", "min_qty=50", map[string]string{"--slaw-bg": "#ffffff", "--slaw-theme-type": "light"})
	require.NoError(t, err)
	assert.Equal(t, "pg", info.Name)
	assert.NotEmpty(t, info.Hash)

	open := fh.until(t, typed(pagehost.TypeOpen))
	assert.Equal(t, "pg", open.Name, "the host name is the pinned artifact")
	assert.Equal(t, "Orders", open.Title, "the suffix is the helper's")
	assert.Equal(t, "min_qty=50", open.Q)
	assert.JSONEq(t, `{"min_qty":{"type":"integer","label":"","default":0,"value":50},"since":{"type":"date","label":"","default":"`+
		time.Now().AddDate(0, 0, -30).Format("2006-01-02")+`","value":"`+time.Now().AddDate(0, 0, -30).Format("2006-01-02")+`"}}`, string(open.Params))

	sent := map[string]string{}
	var doc strings.Builder
	var order []string
	for {
		m := <-fh.got
		order = append(order, m.T)
		if m.T == pagehost.TypeFile {
			sent[m.Path] += string(m.Data)
		}
		if m.T == pagehost.TypeDocr {
			doc.Write(m.Data)
		}
		if m.T == pagehost.TypeGo {
			break
		}
	}
	assert.Equal(t, pagehost.TypeGo, order[len(order)-1])
	assert.Equal(t, "window.x = 1", sent["app.js"])
	assert.Equal(t, "body{margin:0}", sent["assets/style.css"])
	assert.Contains(t, sent[themePath], "--slaw-bg:#ffffff;")
	assert.Contains(t, sent[themePath], "color-scheme:light;")
	for p := range sent {
		assert.False(t, strings.HasSuffix(p, ".tmpl"), "a template is never sent: %s", p)
	}
	assert.Contains(t, doc.String(), `<link rel="stylesheet" href="_slaw/theme.css">`)
	assert.Contains(t, doc.String(), "<h1>min 50</h1>")
	assert.Contains(t, doc.String(), `href="page?min_qty=50"`)
}

func TestPageWindow_RefusesWhatIsNotAnAllowedScriptedPage(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	launched := false
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { launched = true; return newFakeHelper(), nil }

	// Not allowed (revoked).
	_, err := app.RevokeArtifactView("pg")
	require.NoError(t, err)
	_, err = app.OpenArtifactPageWindow("pg", "", nil)
	assert.ErrorIs(t, err, errViewNotAllowed)

	// A static page has no window.
	writePage(t, app, "st", pageIndex, nil)
	st, err := app.ArtifactViewState("st")
	require.NoError(t, err)
	_, err = app.AllowArtifactView("st", st.Hash)
	require.NoError(t, err)
	_, err = app.OpenArtifactPageWindow("st", "", nil)
	assert.ErrorIs(t, err, errPageNotScripted)

	// Not a page at all, and nothing at all.
	_, err = app.OpenArtifactPageWindow("orders", "", nil)
	assert.Error(t, err)
	_, err = app.OpenArtifactPageWindow("nope", "", nil)
	assert.Error(t, err)

	// A bad parameter value.
	_, err = app.AllowArtifactView("pg", mustHash(t, app, "pg"))
	require.NoError(t, err)
	_, err = app.OpenArtifactPageWindow("pg", "min_qty=abc", nil)
	assert.EqualError(t, err, "a parameter value is not valid")
	assert.False(t, launched, "nothing was started for any of them")
}

func mustHash(t *testing.T, app *App, name string) string {
	t.Helper()
	st, err := app.ArtifactViewState(name)
	require.NoError(t, err)
	return st.Hash
}

func TestPageWindow_FailsClosedWithoutTheGate(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	launched := false
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { launched = true; return newFakeHelper(), nil }
	app.pages.preflight = func() error { return errPageNoGate }
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	assert.ErrorIs(t, err, errPageNoGate)
	assert.False(t, launched, "no helper starts when the gate cannot be built")
	assert.Equal(t, errPageNoGate.Error(), app.PageWindowSupport())
}

func (f *fakeHelper) run(id int, q, values string) {
	v := ""
	if values != "" {
		v = `,"v":` + values
	}
	f.send(`{"t":"run","id":` + itoa(id) + `,"q":"` + q + `"` + v + `}`)
}

func replyTo(id int64) func(*pagehost.Message) bool {
	return func(m *pagehost.Message) bool { return m.T == pagehost.TypeReply && m.ID == id }
}

func TestPageWindow_RunAnswersOnlyTheWindowsManifestQueries(t *testing.T) {
	app, fh, _ := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)

	fh.run(1, "big", `{"min_qty":50}`)
	r := fh.until(t, replyTo(1))
	require.NotNil(t, r.OK)
	require.True(t, *r.OK)
	assert.JSONEq(t, `{"columns":["id","qty"],"rows":[[2,50],[3,500]],"truncated":false}`, string(r.Result))

	// A query the manifest does not have, and a value that does not bind.
	fh.run(2, "other", "")
	r = fh.until(t, replyTo(2))
	assert.False(t, *r.OK)
	assert.Equal(t, pagehost.CodeQuery, r.Code)
	fh.run(3, "big", `{"min_qty":"1; DROP TABLE t"}`)
	r = fh.until(t, replyTo(3))
	assert.False(t, *r.OK)
	assert.Equal(t, pagehost.CodeQuery, r.Code)
	var n int
	require.NoError(t, app.sess.DB().QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 3, n)

	// A result over the byte cap is "too large", not a failed query.
	fh.run(4, "fat", "")
	r = fh.until(t, replyTo(4))
	assert.False(t, *r.OK)
	assert.Equal(t, pagehost.CodeTooLarge, r.Code)
}

func TestPageWindow_NoMessageCanNameAnotherArtifact(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	writePage(t, app, "other", pageIndex, nil)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	// The protocol has no field for it: the line is a violation and ends the window.
	fh.send(`{"t":"run","id":1,"q":"big","artifact":"other"}`)
	ev.waitFor(t, pageStateStopped)
	assert.True(t, fh.killed.Load())
}

func TestPageWindow_LimitsFiveInFlightAndSixtyAMinute(t *testing.T) {
	app, fh, _ := scriptedWindow(t)
	release := make(chan struct{})
	inRun := make(chan struct{}, 128)
	base := app.pages
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	w := base.wins["pg"]
	w.run = func(ctx context.Context, _ string, _ map[string]any) (*artifacts.PageRows, error) {
		inRun <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &artifacts.PageRows{Columns: []string{"a"}, Rows: [][]any{{1}}}, nil
	}
	for i := 1; i <= pagehost.MaxInFlight; i++ {
		fh.run(i, "big", "")
		<-inRun
	}
	fh.run(99, "big", "")
	r := fh.until(t, replyTo(99))
	assert.False(t, *r.OK)
	assert.Equal(t, pagehost.CodeLimit, r.Code, "a fifth in flight")
	close(release)
	remaining := pagehost.MaxInFlight
	fh.until(t, func(m *pagehost.Message) bool {
		if m.T == pagehost.TypeReply && m.ID >= 1 && m.ID <= pagehost.MaxInFlight {
			remaining--
		}
		return remaining == 0
	})

	// 60 a minute: the four above count too.
	for i := 100; i < 100+pagehost.MaxPerMinute-pagehost.MaxInFlight; i++ {
		fh.run(i, "big", "")
		r := fh.until(t, replyTo(int64(i)))
		require.True(t, *r.OK, "run %d", i)
	}
	fh.run(500, "big", "")
	r = fh.until(t, replyTo(500))
	assert.Equal(t, pagehost.CodeLimit, r.Code, "the 61st in a minute")
}

func TestPageWindow_ACallToAnUndeclaredCapabilityIsUnavailable(t *testing.T) {
	app, fh, _ := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	fh.send(`{"t":"call","id":7,"cap":"state","op":"get","arg":{"key":"a"}}`)
	r := fh.until(t, replyTo(7))
	assert.False(t, *r.OK)
	assert.Equal(t, pagehost.CodeUnavailable, r.Code)
}

func TestPageWindow_DocRendersANavigationAndRefusesABadValue(t *testing.T) {
	app, fh, _ := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	collect := func(id int64) (int, string) {
		var b strings.Builder
		status := 0
		for {
			m := fh.until(t, func(m *pagehost.Message) bool { return m.T == pagehost.TypeDocr && m.ID == id })
			status = m.Status
			b.Write(m.Data)
			if m.Last {
				return status, b.String()
			}
		}
	}
	fh.send(`{"t":"doc","id":5,"q":"min_qty=500"}`)
	status, doc := collect(5)
	assert.Equal(t, 200, status)
	assert.Contains(t, doc, "<h1>min 500</h1>")
	assert.Contains(t, doc, "_slaw/theme.css")

	fh.send(`{"t":"doc","id":6,"q":"min_qty=abc"}`)
	status, doc = collect(6)
	assert.Equal(t, 500, status)
	assert.Contains(t, doc, "A parameter value is not valid")
	assert.NotContains(t, doc, "abc", "an error page quotes nothing the page sent")
}

func TestPageWindow_WatchdogKillsAMissedHeartbeat(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.beatLimit = 300 * time.Millisecond
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	fh.send(`{"t":"ready"}`)
	ev.waitFor(t, pageStateOpen)
	for i := 0; i < 3; i++ {
		fh.send(`{"t":"hb"}`)
		time.Sleep(100 * time.Millisecond)
	}
	assert.False(t, fh.killed.Load(), "beats keep it alive")
	ev.waitFor(t, pageStateStopped)
	assert.True(t, fh.killed.Load(), "the tree is killed")
}

func TestPageWindow_NeverStartingIsStopped(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	app.pages.grace = 300 * time.Millisecond
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	ev.waitFor(t, pageStateStopped)
	assert.True(t, fh.killed.Load())
}

func TestPageWindow_AChangeToTheArtifactEndsTheWindow(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	app.pages.artifactChanged("other")
	assert.False(t, fh.killed.Load(), "another artifact's change leaves it")
	app.onDiskChange(app.ws, workspace.Change{Kind: workspace.KindArtifact, ID: "pg", Op: workspace.OpChanged, Path: "artifacts/pg/html/app.js"})
	ev.waitFor(t, pageStateChanged)
	assert.True(t, fh.killed.Load())
	assert.Zero(t, pageCount(app.pages))
}

func TestPageWindow_RevokingTheDecisionEndsTheWindow(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	_, err = app.RevokeArtifactView("pg")
	require.NoError(t, err)
	ev.waitFor(t, pageStateChanged)
	assert.True(t, fh.killed.Load())
}

func TestPageWindow_ClosingTheWorkspaceEndsEveryWindow(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	require.NoError(t, app.CloseWorkspace())
	ev.waitFor(t, pageStateClosed)
	assert.True(t, fh.killed.Load())
}

func TestPageWindow_TheHelperSayingClosedOrGoneOrDyingEndsIt(t *testing.T) {
	app, _, ev := scriptedWindow(t)
	app.pages.quitGrace = 10 * time.Millisecond
	seen := 0
	for _, c := range []struct {
		do   func(*fakeHelper)
		want string
	}{
		{func(f *fakeHelper) { f.send(`{"t":"closed"}`) }, pageStateClosed},
		{func(f *fakeHelper) { f.send(`{"t":"gone"}`) }, pageStateStopped},
		{func(f *fakeHelper) { _ = f.fromHelperW.Close() }, pageStateStopped},
		// A flood of messages, and a line that is not the protocol.
		{func(f *fakeHelper) {
			w := bufio.NewWriter(f.fromHelperW)
			go func() {
				for i := 0; i < pagehost.MaxLinesPerSecond*3; i++ {
					_, _ = w.WriteString(`{"t":"hb"}` + "\n")
				}
				_ = w.Flush()
			}()
		}, pageStateStopped},
		{func(f *fakeHelper) { f.send(`{"t":"quit"}`) }, pageStateStopped},
		{func(f *fakeHelper) { f.send(`not json`) }, pageStateStopped},
	} {
		fh := newFakeHelper()
		app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { return fh, nil }
		_, err := app.OpenArtifactPageWindow("pg", "", nil)
		require.NoError(t, err)
		c.do(fh)
		require.Eventually(t, func() bool { return len(ev.states()) > seen }, 5*time.Second, 10*time.Millisecond)
		seen = len(ev.states())
		assert.Equal(t, c.want, ev.states()[seen-1])
		assert.Eventually(t, fh.killed.Load, 2*time.Second, 5*time.Millisecond, "the tree is killed")
		assert.Zero(t, pageCount(app.pages))
	}
}

func TestPageWindow_ReopeningReplacesTheOldWindow(t *testing.T) {
	app, fh, ev := scriptedWindow(t)
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	second := newFakeHelper()
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { return second, nil }
	_, err = app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	ev.waitFor(t, pageStateClosed)
	assert.True(t, fh.killed.Load())
	assert.False(t, second.killed.Load())
	assert.Equal(t, 1, pageCount(app.pages))
}

func TestPageWindow_AtMostFourWindows(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { return newFakeHelper(), nil }
	for i := 0; i < maxPageWindows; i++ {
		name := "p" + itoa(i)
		writePage(t, app, name, scriptedIndex, map[string]string{"app.js": "1"})
		_, err := app.AllowArtifactView(name, mustHash(t, app, name))
		require.NoError(t, err)
		_, err = app.OpenArtifactPageWindow(name, "", nil)
		require.NoError(t, err)
	}
	writePage(t, app, "fifth", scriptedIndex, map[string]string{"app.js": "1"})
	_, err := app.AllowArtifactView("fifth", mustHash(t, app, "fifth"))
	require.NoError(t, err)
	_, err = app.OpenArtifactPageWindow("fifth", "", nil)
	assert.ErrorIs(t, err, errPageTooMany)
}

func TestWindowTitleIsOneLineOfText(t *testing.T) {
	m := &artifacts.Manifest{Title: "Sales ‮\n\tQ1\x00 report"}
	assert.Equal(t, "Sales Q1 report", windowTitle(m, "sales"))
	assert.Equal(t, "sales", windowTitle(&artifacts.Manifest{}, "sales"))
	assert.LessOrEqual(t, len(windowTitle(&artifacts.Manifest{Title: strings.Repeat("é", 500)}, "x")), pagehost.MaxTitle)
	cjk := windowTitle(&artifacts.Manifest{Title: strings.Repeat("日", 200)}, "x")
	assert.LessOrEqual(t, len(cjk), pagehost.MaxTitle)
	assert.True(t, utf8.ValidString(cjk))
	_, err := pagehost.Encode(&pagehost.Message{T: pagehost.TypeOpen, Name: "x", Title: cjk, W: 800, H: 600, Params: json.RawMessage(`{}`)}, pagehost.ToHelper)
	assert.NoError(t, err, "a long CJK title still opens")
}

func TestThemeStylesheetAcceptsRolesOnly(t *testing.T) {
	css := themeStylesheet(map[string]string{
		"--slaw-bg": "#fff", "--slaw-font-body": `"Fraunces", serif`, "--slaw-theme-type": "dark",
		"color": "red", "--other": "1", "--slaw-x": "red;} body{background:url(http://x)}", "--slaw-y": "url(http://x)",
		"--slaw-z": `"unbalanced`, "--slaw-w": "expression(1)", "--slaw-v": "red /* c */",
	})
	assert.Contains(t, css, "--slaw-bg:#fff;")
	assert.Contains(t, css, `--slaw-font-body:"Fraunces", serif;`)
	assert.Contains(t, css, "color-scheme:dark;")
	for _, bad := range []string{"color:red", "--other", "url(", "--slaw-x", "--slaw-z", "--slaw-w", "--slaw-v", "}"} {
		assert.NotContains(t, strings.TrimSuffix(strings.TrimSpace(css), "}"), bad)
	}
	assert.True(t, strings.HasPrefix(css, ":root{"))
}

func TestPageFileSetSendsTheLibrariesThePageNames(t *testing.T) {
	libs := fstest.MapFS{"d3-array.js": {Data: []byte("a")}, "vega.js": {Data: []byte("v")}, "unused.js": {Data: []byte("u")}}
	page := &artifacts.Artifact{Files: []artifacts.PageFile{
		{Path: "index.html.tmpl", Data: []byte("x")},
		{Path: "app.js", Data: []byte(`loadScript("slaw-lib/vega.js"); import "slaw-lib/missing.js"`)},
		{Path: "assets/a.css", Data: []byte("c")},
	}}
	files, err := pageFileSet(page, []byte(`<script src="slaw-lib/d3-array.js"></script>`), ":root{}", libs)
	require.NoError(t, err)
	var paths []string
	for _, f := range files {
		paths = append(paths, f.path)
	}
	assert.ElementsMatch(t, []string{"app.js", "assets/a.css", themePath, "slaw-lib/d3-array.js", "slaw-lib/vega.js"}, paths)
}

// TestNativeTitleIsNeverBoundToTheDocument guards what a Linux window and a Mac
// window cannot show here: the native title is set once, from Studio's title, and
// nothing in either native file reads the web view's own title (H4: the window
// cannot pass for Studio).
func TestNativeTitleIsNeverBoundToTheDocument(t *testing.T) {
	for _, f := range []string{"pagewindow_linux.c", "pagewindow_darwin.m"} {
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		code := string(src)
		for _, forbidden := range []string{"notify::title", "webkit_web_view_get_title", "addObserver", "observeValueForKeyPath", "view.title", "setTitle", ".title ="} {
			if f == "pagewindow_darwin.m" && forbidden == ".title =" {
				continue // the one assignment is the window's, from Studio's title (checked below)
			}
			assert.NotContains(t, code, forbidden, "%s must not follow the document's title", f)
		}
	}
	linux, _ := os.ReadFile("pagewindow_linux.c")
	assert.Equal(t, 1, strings.Count(string(linux), "gtk_window_set_title("))
	mac, _ := os.ReadFile("pagewindow_darwin.m")
	assert.Equal(t, 1, strings.Count(string(mac), "w.title = title;"))
}

func TestPageWindow_ARunOutsideTheTemplatesListIsAllowedAndLogged(t *testing.T) {
	app, fh, _ := scriptedWindow(t)
	var logs bytes.Buffer
	app.pages.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	// The manifest has a second query that no template uses.
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "pg")
	manifest := studioArtifactManifest + "  - name: audit\n    sql: SELECT 1 AS one\nview: html\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(manifest), 0o644))
	_, err := app.AllowArtifactView("pg", mustHash(t, app, "pg"))
	require.NoError(t, err)

	_, err = app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	fh.run(1, "big", "")
	r := fh.until(t, replyTo(1))
	require.True(t, *r.OK)
	assert.NotContains(t, logs.String(), "its templates do not use", "a template's own query is not logged")
	fh.run(2, "audit", "")
	r = fh.until(t, replyTo(2))
	require.True(t, *r.OK, "a manifest query the templates do not use still runs")
	assert.Contains(t, logs.String(), "a page ran a query its templates do not use")
	assert.Contains(t, logs.String(), "query=audit")
	assert.Contains(t, logs.String(), "artifact=pg")
}

func pageCount(h *pageHost) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.wins)
}

func TestPageWindow_ConcurrentOpensLeaveNoOrphan(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	var mu sync.Mutex
	var helpers []*fakeHelper
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) {
		f := newFakeHelper()
		mu.Lock()
		helpers = append(helpers, f)
		mu.Unlock()
		return f, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = app.OpenArtifactPageWindow("pg", "", nil) }()
	}
	wg.Wait()
	assert.Equal(t, 1, pageCount(app.pages), "one window per artifact")
	app.pages.closeAll()
	mu.Lock()
	defer mu.Unlock()
	for i, f := range helpers {
		assert.True(t, f.killed.Load(), "helper %d outlived closeAll", i)
	}
}

func TestPageWindow_CapHoldsUnderConcurrentOpens(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) { return newFakeHelper(), nil }
	for i := 0; i < 8; i++ {
		name := "c" + itoa(i)
		writePage(t, app, name, scriptedIndex, map[string]string{"app.js": "1"})
		_, err := app.AllowArtifactView(name, mustHash(t, app, name))
		require.NoError(t, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, _ = app.OpenArtifactPageWindow("c"+itoa(i), "", nil) }(i)
	}
	wg.Wait()
	assert.Equal(t, maxPageWindows, pageCount(app.pages))
}

func TestPageWindow_AnOpenRacingCloseAllIsNotKept(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	fh := newFakeHelper()
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) {
		app.pages.closeAll() // the workspace closes while the helper is starting
		return fh, nil
	}
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	assert.Error(t, err)
	assert.True(t, fh.killed.Load())
	assert.Zero(t, pageCount(app.pages))
}

func TestPageWindow_ARevokeOfAnotherArtifactDoesNotRefuseAnOpen(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	fh := newFakeHelper()
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) {
		app.pages.closeArtifact("other", pageStateClosed) // another artifact changes meanwhile
		return fh, nil
	}
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, pageCount(app.pages))
	app.pages.closeAll()
}

func TestPageWindow_AChangeToTheSameArtifactDuringOpenIsNotKept(t *testing.T) {
	app, _, _ := scriptedWindow(t)
	fh := newFakeHelper()
	app.pages.launch = func(context.Context, *slog.Logger) (helperConn, error) {
		app.pages.closeArtifact("pg", pageStateChanged)
		return fh, nil
	}
	_, err := app.OpenArtifactPageWindow("pg", "", nil)
	assert.Error(t, err)
	assert.True(t, fh.killed.Load())
	assert.Zero(t, pageCount(app.pages))
}
