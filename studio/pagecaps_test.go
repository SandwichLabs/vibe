package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/pagehost"
)

const allCaps = "html:\n  capabilities:\n    state: {scope: local}\n    download: {}\n    ask: {}\n"

// capWindow writes, allows and opens artifact name as a scripted page that
// declares caps (manifest text after the base), with services edited by edit.
func capWindow(t *testing.T, app *App, name, caps string, edit func(*pageServices)) *fakeHelper {
	t.Helper()
	writePage(t, app, name, scriptedIndex, map[string]string{"app.js": "window.x = 1"})
	mf := filepath.Join(app.ws.Root(), artifacts.Dir, name, artifacts.ManifestFile)
	require.NoError(t, os.WriteFile(mf, []byte(pageManifest+caps), 0o644))
	st, err := app.ArtifactViewState(name)
	require.NoError(t, err)
	require.Empty(t, st.Error)
	_, err = app.AllowArtifactView(name, st.Hash)
	require.NoError(t, err)
	if app.watchDone != nil {
		close(app.watchDone)
		app.watchDone = nil
	}
	if app.watcher != nil {
		_ = app.watcher.Close()
		app.watcher = nil
	}
	fh := newFakeHelper()
	app.emit = (&events{}).add
	app.pages.preflight = func() error { return nil }
	app.pages.launch = func(_ context.Context, _ *slog.Logger) (helperConn, error) { return fh, nil }
	t.Cleanup(app.pages.closeAll)
	svc := app.pageServices(app.ws, name)
	if edit != nil {
		edit(&svc)
	}
	_, err = app.pages.open(pageOpen{
		ws: app.ws, name: name, base: context.Background(), svc: svc,
		runner: func(m *artifacts.Manifest, pins map[string]string) artifacts.PageRunner {
			return app.pageRunner(name, m, pins)
		},
		allowed: func(h string) bool { return app.pageStillAllowed(app.ws, name, h) },
	})
	require.NoError(t, err)
	return fh
}

func (f *fakeHelper) call(id int, capName, op, arg string) *pagehost.Message {
	line := `{"t":"call","id":` + itoa(id) + `,"cap":"` + capName + `","op":"` + op + `"`
	if arg != "" {
		line += `,"arg":` + arg
	}
	f.send(line + "}")
	return nil
}

func (f *fakeHelper) callWait(t *testing.T, id int, capName, op, arg string) *pagehost.Message {
	t.Helper()
	f.call(id, capName, op, arg)
	return f.until(t, replyTo(int64(id)))
}

func okResult(t *testing.T, m *pagehost.Message) string {
	t.Helper()
	require.NotNil(t, m.OK)
	require.True(t, *m.OK, "code %q", m.Code)
	return string(m.Result)
}

func openMsg(t *testing.T, fh *fakeHelper) *pagehost.Message {
	return fh.until(t, typed(pagehost.TypeOpen))
}

func TestCaps_OnlyDeclaredCapabilitiesAreGrantedAndCallable(t *testing.T) {
	app := pageApp(t)
	var called atomic.Bool
	fh := capWindow(t, app, "pg", "html:\n  capabilities:\n    state: {scope: local}\n", func(s *pageServices) {
		s.save = func(context.Context, string, string, string) (string, error) { called.Store(true); return "", nil }
		s.ask = func(context.Context, string, string) (string, bool, error) { called.Store(true); return "x", true, nil }
		s.askOn = func(string) bool { return true }
	})
	assert.Equal(t, []string{"state"}, openMsg(t, fh).Caps, "only what the manifest declared")
	for i, c := range [][3]string{{"download", "save", `{"filename":"a","type":"csv","enc":"text","data":"x"}`}, {"ask", "ask", `{"prompt":"hi"}`}, {"db", "get", `{}`}} {
		r := fh.callWait(t, i+1, c[0], c[1], c[2])
		assert.Equal(t, pagehost.CodeUnavailable, r.Code, c[0])
	}
	assert.False(t, called.Load(), "no service ran for an undeclared capability")
}

func TestCaps_AStaticPageCannotDeclareCapabilities(t *testing.T) {
	_, err := artifacts.Decode([]byte(pageManifest + allCaps))
	require.NoError(t, err, "the manifest alone is fine; the vet refuses it without script")
	app := pageApp(t)
	writePage(t, app, "st", pageIndex, nil)
	mf := filepath.Join(app.ws.Root(), artifacts.Dir, "st", artifacts.ManifestFile)
	require.NoError(t, os.WriteFile(mf, []byte(pageManifest+allCaps), 0o644))
	st, err := app.ArtifactViewState("st")
	require.NoError(t, err)
	assert.Empty(t, st.Hash, "capabilities need script")
}

func TestCaps_StateRoundTripsAndLandsInVibeLocalOnly(t *testing.T) {
	app := pageApp(t)
	fh := capWindow(t, app, "pg", allCaps, nil)
	assert.Equal(t, "true", okResult(t, fh.callWait(t, 1, "state", "set", `{"key":"filters/a","value":{"x":[1,2]}}`)))
	assert.Equal(t, "true", okResult(t, fh.callWait(t, 2, "state", "set", `{"key":"filters/b","value":"<script>"}`)))
	assert.JSONEq(t, `{"x":[1,2]}`, okResult(t, fh.callWait(t, 3, "state", "get", `{"key":"filters/a"}`)))
	assert.Equal(t, "null", okResult(t, fh.callWait(t, 4, "state", "get", `{"key":"missing"}`)))
	assert.JSONEq(t, `["filters/a","filters/b"]`, okResult(t, fh.callWait(t, 5, "state", "list", `{"prefix":"filters/"}`)))
	assert.Equal(t, "true", okResult(t, fh.callWait(t, 6, "state", "delete", `{"key":"filters/b"}`)))
	assert.JSONEq(t, `["filters/a"]`, okResult(t, fh.callWait(t, 7, "state", "list", `{"prefix":""}`)))

	app.pages.closeAll() // flushes
	root := app.ws.Root()
	b, err := os.ReadFile(filepath.Join(root, ".vibe-local", "artifacts", "pg", "state.json"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "filters/a")
	assert.NoFileExists(t, filepath.Join(root, "artifacts", "pg", "state.json"))
	entries, err := os.ReadDir(filepath.Join(root, "artifacts", "pg"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), "state", "nothing of the state is in the folder that git carries")
	}
}

func TestCaps_StateRefusesBadInput(t *testing.T) {
	app := pageApp(t)
	fh := capWindow(t, app, "pg", allCaps, nil)
	for i, arg := range []string{`{"key":""}`, `{"key":"a","artifact":"other"}`, `{"nope":1}`, `[]`} {
		r := fh.callWait(t, i+1, "state", "get", arg)
		assert.Equal(t, pagehost.CodeRefused, r.Code, arg)
	}
	r := fh.callWait(t, 10, "state", "set", `{"key":"a"}`)
	assert.Equal(t, pagehost.CodeRefused, r.Code, "a set without a value")
}

// upload sends text as put chunks the way the injected API does, then calls.
func (f *fakeHelper) uploadCall(t *testing.T, id int, up int, capName, op, text string, chunk int) *pagehost.Message {
	t.Helper()
	n, pid := 0, 1000*id
	for pos := 0; pos < len(text) || n == 0; n++ {
		end := min(len(text), pos+chunk)
		lit, _ := json.Marshal(text[pos:end])
		line := `{"t":"put","id":` + itoa(pid+n) + `,"up":` + itoa(up) + `,"s":` + string(lit)
		if n > 0 {
			line += `,"seq":` + itoa(n)
		}
		if end >= len(text) {
			line += `,"last":true`
		}
		f.send(line + "}")
		r := f.until(t, replyTo(int64(pid+n)))
		require.True(t, *r.OK, "chunk %d: %s", n, r.Code)
		pos = end
		if end >= len(text) {
			break
		}
	}
	f.send(`{"t":"call","id":` + itoa(id) + `,"cap":"` + capName + `","op":"` + op + `","up":` + itoa(up) + `}`)
	return f.until(t, replyTo(int64(id)))
}

func TestCaps_StateCapsValueTotalAndChunkedArguments(t *testing.T) {
	app := pageApp(t)
	fh := capWindow(t, app, "pg", allCaps, nil)
	big := func(n int) string { return `{"key":"k","value":"` + strings.Repeat("a", n) + `"}` }
	// Over 64 KB: refused (arrives as an upload, since it cannot go inline).
	r := fh.uploadCall(t, 1, 1, "state", "set", big(64<<10), 20000)
	assert.Equal(t, pagehost.CodeTooLarge, r.Code, "a value over 64 KB")
	// Just under it is stored, through chunks.
	r = fh.uploadCall(t, 2, 2, "state", "set", big(60<<10), 20000)
	assert.Equal(t, "true", okResult(t, r))
	got := fh.callWait(t, 3, "state", "get", `{"key":"k"}`)
	assert.Len(t, okResult(t, got), 60<<10+2)
	// The total is 1 MB: fill up with 60 KB values until refused.
	var refused bool
	for i := 0; i < 20; i++ {
		key := `{"key":"big` + itoa(i) + `","value":"` + strings.Repeat("b", 60<<10) + `"}`
		r := fh.uploadCall(t, 10+i, 10+i, "state", "set", key, 30000)
		if !*r.OK {
			assert.Equal(t, pagehost.CodeTooLarge, r.Code)
			refused = true
			break
		}
	}
	assert.True(t, refused, "the store is capped at 1 MB in total")
}

func TestCaps_AnUploadThatBreaksTheRulesEndsTheWindow(t *testing.T) {
	cases := map[string][]string{
		"does not start at zero": {`{"t":"put","id":1,"up":5,"s":"{","seq":1}`},
		"a third upload at once": {`{"t":"put","id":1,"up":7,"s":"x"}`, `{"t":"put","id":2,"up":8,"s":"x"}`, `{"t":"put","id":3,"up":9,"s":"x"}`},
		"a chunk after the last": {`{"t":"put","id":1,"up":7,"s":"x","last":true}`, `{"t":"put","id":2,"up":7,"s":"y","seq":1}`},
		"out of order":           {`{"t":"put","id":1,"up":7,"s":"x"}`, `{"t":"put","id":2,"up":7,"s":"y","seq":2}`},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			app := pageApp(t)
			fh := capWindow(t, app, "pg", allCaps, nil)
			for _, l := range lines {
				fh.send(l)
			}
			require.Eventually(t, func() bool { return pageCount(app.pages) == 0 }, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestCaps_NoUploadWithoutACapability(t *testing.T) {
	app := pageApp(t)
	// A page with no capability has no use for a chunk: it ends the window.
	fh := capWindow(t, app, "pg", "", nil)
	fh.send(`{"t":"put","id":1,"up":5,"s":"x"}`)
	require.Eventually(t, func() bool { return pageCount(app.pages) == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestCaps_NoUploadIsLeftBehind(t *testing.T) {
	app := pageApp(t)
	// A call that is refused still takes the upload it names, and a call naming
	// one that is unfinished is refused.
	fh := capWindow(t, app, "pg", "html:\n  capabilities:\n    state: {scope: local}\n", nil)
	fh.send(`{"t":"put","id":1,"up":5,"s":"{\"key\":\"a\"}","last":true}`)
	okResult(t, fh.until(t, replyTo(1)))
	fh.send(`{"t":"call","id":2,"cap":"ask","op":"ask","up":5}`) // not granted
	assert.Equal(t, pagehost.CodeUnavailable, fh.until(t, replyTo(2)).Code)
	var w *pageWindow
	app.pages.mu.Lock()
	w = app.pages.wins["pg"]
	app.pages.mu.Unlock()
	require.NotNil(t, w)
	w.upMu.Lock()
	assert.Empty(t, w.uploads, "the refused call took its upload")
	w.upMu.Unlock()
	fh.send(`{"t":"put","id":3,"up":6,"s":"{"}`)
	okResult(t, fh.until(t, replyTo(3)))
	fh.send(`{"t":"call","id":4,"cap":"state","op":"get","up":6}`)
	assert.Equal(t, pagehost.CodeRefused, fh.until(t, replyTo(4)).Code)

	// An upload nobody claims is dropped after a while.
	fh.send(`{"t":"put","id":5,"up":8,"s":"{","last":true}`)
	okResult(t, fh.until(t, replyTo(5)))
	w.upMu.Lock()
	w.uploads[8].started = time.Now().Add(-2 * uploadTTL)
	w.upMu.Unlock()
	fh.send(`{"t":"put","id":6,"up":9,"s":"{"}`)
	okResult(t, fh.until(t, replyTo(6)))
	w.upMu.Lock()
	_, stale := w.uploads[8]
	w.upMu.Unlock()
	assert.False(t, stale)
}

func TestCaps_UploadBytesPerSecondAreCapped(t *testing.T) {
	app := pageApp(t)
	app.pages.putLimit = 100 << 10
	fh := capWindow(t, app, "pg", allCaps, nil)
	chunk := strings.Repeat("a", 40<<10)
	for i := 1; i <= 4; i++ {
		fh.send(`{"t":"put","id":` + itoa(i) + `,"up":` + itoa(i) + `,"s":"` + chunk + `"}`)
	}
	require.Eventually(t, func() bool { return pageCount(app.pages) == 0 }, 5*time.Second, 10*time.Millisecond, "over the bytes a second")
}

func TestSaveThrottle(t *testing.T) {
	var th saveThrottle
	now := time.Unix(1000, 0)
	assert.True(t, th.allow(now))
	th.cancel(now)
	assert.False(t, th.allow(now.Add(5*time.Second)), "none for 10 seconds after a cancel")
	assert.True(t, th.allow(now.Add(11*time.Second)))
	assert.True(t, th.allow(now.Add(12*time.Second)))
	assert.False(t, th.allow(now.Add(13*time.Second)), "at most 3 a minute")
	assert.True(t, th.allow(now.Add(72*time.Second)), "the first aged out")
}

func TestCaps_TwoWindowsWritingTheSameStateKeepBothChanges(t *testing.T) {
	app := pageApp(t)
	fh := capWindow(t, app, "pg", allCaps, nil)
	okResult(t, fh.callWait(t, 1, "state", "set", `{"key":"mine","value":1}`))
	// Another window (here another store over the same file) writes first.
	other := app.pages.stateStoreForTest(t, app.ws.Root(), "pg")
	require.NoError(t, other.Set("theirs", json.RawMessage(`2`), nil))
	require.NoError(t, other.Flush())
	app.pages.closeAll()
	b, err := os.ReadFile(filepath.Join(app.ws.Root(), ".vibe-local", "artifacts", "pg", "state.json"))
	require.NoError(t, err)
	assert.Contains(t, string(b), `"mine":1`)
	assert.Contains(t, string(b), `"theirs":2`)
}

func TestCaps_AnotherWindowsStateWriteIsPushedToThePage(t *testing.T) {
	app := pageApp(t)
	fh := capWindow(t, app, "pg", allCaps, nil)
	okResult(t, fh.callWait(t, 1, "state", "set", `{"key":"a","value":1}`))
	other, err := app.pages.stateStore(app.ws.Root(), "pg", nil)
	require.NoError(t, err)
	require.NoError(t, other.Set("b", json.RawMessage(`{"n":2}`), "another window"))
	p := fh.until(t, typed(pagehost.TypePush))
	assert.Equal(t, "state", p.Cap)
	assert.JSONEq(t, `{"key":"b","deleted":false,"value":{"n":2}}`, string(p.Arg))
	// The page's own write is not echoed back to it.
	okResult(t, fh.callWait(t, 2, "state", "set", `{"key":"c","value":3}`))
	select {
	case m := <-fh.got:
		assert.NotEqual(t, pagehost.TypePush, m.T)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCaps_StateOfOneArtifactIsNotReachableFromAnother(t *testing.T) {
	app := pageApp(t)
	fa := capWindow(t, app, "aa", allCaps, nil)
	okResult(t, fa.callWait(t, 1, "state", "set", `{"key":"secret","value":"from-a"}`))
	fb := capWindow(t, app, "bb", allCaps, nil)
	assert.Equal(t, "null", okResult(t, fb.callWait(t, 1, "state", "get", `{"key":"secret"}`)), "b has its own store")
	assert.Equal(t, pagehost.CodeRefused, fb.callWait(t, 2, "state", "get", `{"key":"secret","artifact":"aa"}`).Code)
	assert.Equal(t, "null", okResult(t, fb.callWait(t, 3, "state", "get", `{"key":"../aa/secret"}`)), "a key is only a key, never a path")
}

func TestCaps_DownloadCancelWritesNothingAndSaveWritesWhereChosen(t *testing.T) {
	app := pageApp(t)
	dir := t.TempDir()
	var path atomic.Value
	path.Store("")
	var title, name string
	fh := capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.save = func(_ context.Context, ti, fn, _ string) (string, error) {
			title, name = ti, fn
			p, _ := path.Load().(string)
			return p, nil
		}
	})
	arg := `{"filename":"../../etc/pa\u202Essw/rd.csv","type":"text/csv","enc":"text","data":"a,b\n1,2\n"}`
	r := fh.callWait(t, 1, "download", "save", arg) // cancel: path ""
	assert.JSONEq(t, `{"saved":false}`, okResult(t, r))
	entries, _ := os.ReadDir(dir)
	assert.Empty(t, entries)
	assert.Equal(t, "rd.csv", name, "no path parts and no bidi characters reach the dialog")
	assert.Contains(t, title, "Orders")

	// The person just cancelled: another dialog does not open at once.
	assert.Equal(t, pagehost.CodeLimit, fh.callWait(t, 3, "download", "save", arg).Code)
	app.pages.saveThrottleFor("pg").cancel(time.Time{})
	out := filepath.Join(dir, "out.csv")
	path.Store(out)
	assert.JSONEq(t, `{"saved":true}`, okResult(t, fh.callWait(t, 2, "download", "save", arg)))
	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "a,b\n1,2\n", string(b))
}

func TestCaps_DownloadRefusesWhatIsNotAllowed(t *testing.T) {
	app := pageApp(t)
	var saved atomic.Bool
	app.pages.putLimit = 1 << 30 // the 28 MB below is sent as fast as a test can
	fh := capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.save = func(context.Context, string, string, string) (string, error) { saved.Store(true); return "", nil }
	})
	png := base64.StdEncoding.EncodeToString([]byte("not a png"))
	for i, arg := range []string{
		`{"filename":"a","type":"exe","enc":"text","data":"x"}`,
		`{"filename":"a","type":"png","enc":"base64","data":"` + png + `"}`,
		`{"filename":"a","type":"csv","enc":"base64","data":"!!!"}`,
		`{"filename":"a","type":"csv","enc":"hex","data":"00"}`,
		`{"filename":"a","type":"csv","enc":"text","data":"x","path":"/etc/x"}`,
	} {
		assert.Equal(t, pagehost.CodeRefused, fh.callWait(t, i+1, "download", "save", arg).Code, arg)
	}
	// 20 MB + 1 as base64 text, through an upload.
	raw := strings.Repeat("A", base64.StdEncoding.EncodedLen(maxDownloadBytes+3))
	r := fh.uploadCall(t, 50, 50, "download", "save", `{"filename":"a","type":"pdf","enc":"base64","data":"`+raw+`"}`, 50000)
	assert.False(t, *r.OK)
	assert.False(t, saved.Load(), "no dialog opened for a refused file")
}

func TestCaps_DownloadStopsWhenThePageIsNoLongerAllowed(t *testing.T) {
	app := pageApp(t)
	path := filepath.Join(t.TempDir(), "x.csv")
	fh := capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.save = func(context.Context, string, string, string) (string, error) {
			// The person took a while; meanwhile the page was revoked.
			_, err := app.RevokeArtifactView("pg")
			require.NoError(t, err)
			return path, nil
		}
	})
	fh.call(1, "download", "save", `{"filename":"a","type":"csv","enc":"text","data":"x"}`)
	require.Eventually(t, func() bool { return pageCount(app.pages) == 0 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	assert.NoFileExists(t, path)
}

func TestSanitizeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"report":                 "report.csv",
		"report.CSV":             "report.csv",
		"../../x/y.csv":          "y.csv",
		`C:\Users\z\a.csv`:       "a.csv",
		"a\x00b\u202Ec.csv":      "abc.csv",
		"  ..hidden.csv":         "hidden.csv",
		"a<b>:c|d?.csv":          "a_b__c_d_.csv",
		"":                       "download.csv",
		"...":                    "download.csv",
		"con":                    "download.csv",
		strings.Repeat("é", 200): strings.Repeat("é", 60) + ".csv",
		"name.csv.csv":           "name.csv.csv",
	} {
		assert.Equal(t, want, sanitizeFilename(in, ".csv"), "%q", in)
	}
}

func TestCaps_AskIsNullWithoutAProviderAndSendsNoToolsFromTheService(t *testing.T) {
	app := pageApp(t)
	var configured atomic.Bool
	var mu sync.Mutex
	var entries []ActivityEntry
	var sawSystem, sawPrompt string
	fh := capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.askOn = func(string) bool { return true }
		s.ask = func(_ context.Context, sys, p string) (string, bool, error) {
			sawSystem, sawPrompt = sys, p
			return "hello", configured.Load(), nil
		}
		s.activity = func(e ActivityEntry) { mu.Lock(); entries = append(entries, e); mu.Unlock() }
	})
	assert.Contains(t, openMsg(t, fh).Caps, "ask")
	assert.Equal(t, "null", okResult(t, fh.callWait(t, 1, "ask", "ask", `{"prompt":"hi"}`)))
	configured.Store(true)
	assert.JSONEq(t, `{"text":"hello"}`, okResult(t, fh.callWait(t, 2, "ask", "ask", `{"prompt":"hi","system":"be brief"}`)))
	assert.Equal(t, "be brief", sawSystem)
	assert.Equal(t, "hi", sawPrompt)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, entries, 2)
	assert.Equal(t, "pg", entries[1].Name, "the use is logged with the page's name")
	assert.Equal(t, "page", entries[1].Source)
}

func TestCaps_AskIsOffWithoutItsToggleAndLimitedToTenAMinute(t *testing.T) {
	app := pageApp(t)
	var on atomic.Bool
	fh := capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.askOn = func(string) bool { return on.Load() }
		s.ask = func(context.Context, string, string) (string, bool, error) { return "ok", true, nil }
	})
	assert.NotContains(t, openMsg(t, fh).Caps, "ask", "the toggle is off: the page gets null from vibe.use")
	assert.Equal(t, pagehost.CodeUnavailable, fh.callWait(t, 1, "ask", "ask", `{"prompt":"hi"}`).Code)

	app.pages.closeAll()
	on.Store(true)
	fh = capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.askOn = func(string) bool { return on.Load() }
		s.ask = func(context.Context, string, string) (string, bool, error) { return "ok", true, nil }
	})
	for i := 1; i <= 10; i++ {
		okResult(t, fh.callWait(t, i, "ask", "ask", `{"prompt":"hi"}`))
		if i%3 == 0 {
			time.Sleep(time.Second / 1000) // keep within the window's own 60 a minute
		}
	}
	assert.Equal(t, pagehost.CodeLimit, fh.callWait(t, 11, "ask", "ask", `{"prompt":"hi"}`).Code, "the 11th in a minute")
	on.Store(false) // revoked while open: the next call is refused at once
	assert.Equal(t, pagehost.CodeUnavailable, fh.callWait(t, 12, "ask", "ask", `{"prompt":"hi"}`).Code)
}

func TestCaps_AskRefusesAnOversizedPromptAndShowsNoDetailOnFailure(t *testing.T) {
	app := pageApp(t)
	fh := capWindow(t, app, "pg", allCaps, func(s *pageServices) {
		s.askOn = func(string) bool { return true }
		s.ask = func(context.Context, string, string) (string, bool, error) {
			return "", true, assertErr("dial tcp 10.0.0.5:8080 sk-SECRET")
		}
	})
	r := fh.uploadCall(t, 1, 1, "ask", "ask", `{"prompt":"`+strings.Repeat("a", 33<<10)+`"}`, 20000)
	assert.Equal(t, pagehost.CodeRefused, r.Code)
	r = fh.callWait(t, 2, "ask", "ask", `{"prompt":"hi"}`)
	assert.Equal(t, pagehost.CodeModel, r.Code)
	line, _ := json.Marshal(r)
	assert.NotContains(t, string(line), "10.0.0.5")
	assert.NotContains(t, string(line), "SECRET")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
