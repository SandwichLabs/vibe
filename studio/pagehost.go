package main

// The page windows of scripted HTML pages (docs/plans/html-artifacts.md, H4,
// step P4). A scripted page does not run in Studio's window: it runs in a native
// web view owned by a helper process (this same binary with --view-host), under
// a network gate, and Studio is the only party that can talk to it. This file is
// the manager: it opens a window for an allowed, scripted page, pins the
// artifact by name, and ends the window on a missed heartbeat, a crash, a change
// to the artifact, a workspace switch or a person closing it. It is a helper,
// not a binding. The bridge (a page's requests) is pagehost_bridge.go, the file
// set it sends pagehost_files.go, and how a helper is started
// pagehost_launch_unix.go and pagehost_gate_*.go.

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/pagehost"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// Limits of the manager.
const (
	// maxPageWindows is how many page windows may be open at once.
	maxPageWindows = 4
	// pageBeatTimeout is how long the page may go without its heartbeat (which
	// the injected script posts every 500 ms) before the window is killed.
	pageBeatTimeout = 4 * time.Second
	// pageStartGrace is how long a window may take to post its first beat.
	pageStartGrace = 25 * time.Second
	// pageSetupTimeout is how long a new helper may take to read its setup (the
	// page's files and libraries) before it is killed.
	pageSetupTimeout = 10 * time.Second
	// pageVerdictCache is how long a positive "still allowed" is reused by the
	// bridge, as the static route does.
	pageVerdictCache = 500 * time.Millisecond
)

// Window states, the "state" of a pagewindow:state event.
const (
	pageStateOpen    = "open"    // the page loaded
	pageStateStopped = "stopped" // it stopped responding, crashed or broke the protocol
	pageStateChanged = "changed" // the artifact or its decision changed
	pageStateClosed  = "closed"  // the person closed it, or a new window replaced it
)

// helperConn is a running helper: its two pipes (read what it sends, write what
// Studio sends), a tree kill, and a channel that closes when it has exited.
type helperConn interface {
	io.Reader
	io.Writer
	Kill()
	Done() <-chan struct{}
}

// helperLauncher starts a helper. The real one is launcherFor; tests give
// in-memory fakes.
type helperLauncher func(ctx context.Context, logger *slog.Logger) (helperConn, error)

// PageWindowInfo is what opening a window returns.
type PageWindowInfo struct {
	Name string `json:"name"`
	// Hash is the PageHash the window was opened for.
	Hash string `json:"hash"`
}

// pageHost owns the open windows.
type pageHost struct {
	launch    helperLauncher
	preflight func() error
	logger    *slog.Logger
	emit      func(event string, data ...any)
	libs      fs.FS
	beatLimit time.Duration
	grace     time.Duration

	// openMu serialises opens, so the cap and one-window-per-artifact hold. gen
	// moves when closeAll ends every window, and gens[name] when a revoke or a
	// change ends one artifact's, so a window that was being started when that
	// happened is not kept (and an open of another artifact is not disturbed).
	openMu sync.Mutex
	mu     sync.Mutex
	wins   map[string]*pageWindow
	gen    uint64
	gens   map[string]uint64
}

func newPageHost(logger *slog.Logger, libs fs.FS, emit func(event string, data ...any)) *pageHost {
	return &pageHost{
		launch: launcherFor(recordExe()), preflight: pageHostPreflight, logger: logger, emit: emit, libs: libs,
		beatLimit: pageBeatTimeout, grace: pageStartGrace, wins: map[string]*pageWindow{}, gens: map[string]uint64{},
	}
}

// pageOpen is what opening a window needs from the App: the workspace to read
// the held page from, how to build the query runner pinned to its manifest, how
// to re-check the decision, and what the person's window looks like.
type pageOpen struct {
	ws      workspace.Workspace
	name    string
	query   string
	theme   map[string]string
	runner  func(*artifacts.Manifest) artifacts.PageRunner
	allowed func(hash string) bool
	base    context.Context
}

// pageWindow is one open window. The artifact is pinned here, at open: no
// message from the page names one.
type pageWindow struct {
	host    *pageHost
	name    string
	hash    string
	page    *artifacts.Artifact
	conn    helperConn
	out     *pagehost.Writer
	run     artifacts.PageRunner
	allowed func(hash string) bool
	ctx     context.Context
	cancel  context.CancelFunc

	limit   pagehost.Limiter
	flood   pagehost.Flood
	started time.Time
	beat    atomic.Int64

	verdictMu sync.Mutex
	verdictAt time.Time

	once  sync.Once
	final string
}

// open starts a window for the named artifact, which must be an allowed,
// scripted HTML page. The window outlives this call; its end is reported by the
// pagewindow:state event.
func (h *pageHost) open(req pageOpen) (*PageWindowInfo, error) {
	if err := h.preflight(); err != nil {
		return nil, err
	}
	page, hash, err := heldScriptedPage(req.ws, req.name)
	if err != nil {
		return nil, err
	}
	h.openMu.Lock()
	defer h.openMu.Unlock()
	h.endWindow(req.name, pageStateClosed)
	h.mu.Lock()
	gen, own, n := h.gen, h.gens[req.name], len(h.wins)
	h.mu.Unlock()
	if n >= maxPageWindows {
		return nil, errPageTooMany
	}
	base := req.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	run := req.runner(&page.Manifest)
	setup, err := buildWindowSetup(ctx, run, page, req, h.libs)
	if err != nil {
		cancel()
		return nil, err
	}
	conn, err := h.launch(ctx, h.logger)
	if err != nil {
		cancel()
		return nil, err
	}
	w := &pageWindow{host: h, name: req.name, hash: hash, page: page, conn: conn, out: pagehost.NewWriter(conn, pagehost.ToHelper),
		run: run, allowed: req.allowed, ctx: ctx, cancel: cancel, started: time.Now()}
	// A helper that wedges before reading its setup would hold openMu, and with it
	// every later open: give it pageSetupTimeout, then kill it.
	stall := time.AfterFunc(pageSetupTimeout, w.kill)
	err = w.send(setup)
	if !stall.Stop() && err == nil {
		err = errPageWindowSetup
	}
	if err != nil {
		if h.logger != nil {
			h.logger.Debug("page window: the helper stopped before it was sent the page", "error", err)
		}
		w.kill()
		cancel()
		return nil, errPageWindowSetup
	}
	if !h.register(w, gen, own) {
		w.kill()
		cancel()
		return nil, errViewNotAllowed
	}
	go w.serve()
	go w.watch()
	go w.reap()
	return &PageWindowInfo{Name: req.name, Hash: hash}, nil
}

// heldScriptedPage rebuilds the page from the files the decision was made over
// and checks it is allowed, scripted, and still at the hash the person saw.
func heldScriptedPage(ws workspace.Workspace, name string) (*artifacts.Artifact, string, error) {
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return nil, "", viewError(err)
	}
	if st.Kind != artifacts.ViewHTML || st.State != string(stateAllowed) || st.Hash == "" {
		return nil, "", errViewNotAllowed
	}
	if st.PageKind != artifacts.PageScripted {
		return nil, "", errPageNotScripted
	}
	page, err := artifacts.PageFromText(name, []byte(st.Manifest), st.PageFiles)
	if err != nil || page.Page.Hash != st.Hash {
		return nil, "", errViewNotAllowed
	}
	return page, st.Hash, nil
}

// register keeps the window unless all windows, or this artifact's, were ended
// since gen and own were read.
func (h *pageHost) register(w *pageWindow, gen, own uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gen != gen || h.gens[w.name] != own {
		return false
	}
	h.wins[w.name] = w
	return true
}

// closeArtifact ends the named artifact's window, if there is one, with state.
func (h *pageHost) closeArtifact(name, state string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.gens[name]++
	h.mu.Unlock()
	h.endWindow(name, state)
}

// endWindow ends the named artifact's window without disturbing opens in flight.
func (h *pageHost) endWindow(name, state string) {
	h.mu.Lock()
	w := h.wins[name]
	h.mu.Unlock()
	if w != nil {
		w.finish(state)
	}
}

// closeAll ends every window (the workspace closed or Studio is quitting).
func (h *pageHost) closeAll() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.gen++
	all := make([]*pageWindow, 0, len(h.wins))
	for _, w := range h.wins {
		all = append(all, w)
	}
	h.mu.Unlock()
	for _, w := range all {
		w.finish(pageStateClosed)
	}
}

// artifactChanged is the watcher's word that a file of the artifact changed on
// disk: its window ends at once, and the person is asked again on Reopen.
func (h *pageHost) artifactChanged(name string) { h.closeArtifact(name, pageStateChanged) }

func (w *pageWindow) send(msgs []*pagehost.Message) error {
	for _, m := range msgs {
		if err := w.out.Send(m); err != nil {
			return err
		}
	}
	return nil
}

// kill ends the helper and everything under it.
func (w *pageWindow) kill() { w.conn.Kill() }

// finish ends the window once, whatever the cause: the first state wins.
func (w *pageWindow) finish(state string) {
	w.once.Do(func() {
		w.final = state
		w.cancel()
		w.kill()
		w.host.mu.Lock()
		if w.host.wins[w.name] == w {
			delete(w.host.wins, w.name)
		}
		w.host.mu.Unlock()
		w.host.report(w, state)
	})
}

func (h *pageHost) report(w *pageWindow, state string) {
	if h.logger != nil {
		h.logger.Debug("page window ended", "artifact", w.name, "state", state)
	}
	if h.emit != nil {
		h.emit("pagewindow:state", map[string]any{"name": w.name, "state": state, "hash": w.hash})
	}
}

// reap notices the helper exiting on its own.
func (w *pageWindow) reap() {
	select {
	case <-w.conn.Done():
		w.finish(pageStateStopped)
	case <-w.ctx.Done():
	}
}

// watch is the watchdog: the page posts a beat every 500 ms, and a window that
// misses them (a page spinning in a loop) or never starts is killed with its
// whole process tree.
func (w *pageWindow) watch() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case now := <-t.C:
			last := w.beat.Load()
			switch {
			case last == 0 && now.Sub(w.started) > w.host.grace:
				w.finish(pageStateStopped)
			case last != 0 && now.Sub(time.Unix(0, last)) > w.host.beatLimit:
				w.finish(pageStateStopped)
			}
		}
	}
}

// stillAllowed is the bridge's per-request check that the page is still the
// allowed one, cached for half a second.
func (w *pageWindow) stillAllowed() bool {
	w.verdictMu.Lock()
	defer w.verdictMu.Unlock()
	if time.Since(w.verdictAt) < pageVerdictCache {
		return true
	}
	if !w.allowed(w.hash) {
		return false
	}
	w.verdictAt = time.Now()
	return true
}

// windowTitle is the artifact's title as the native title shows it: one line of
// plain text. The helper adds " — Vibe page", and nothing the page does changes it.
func windowTitle(m *artifacts.Manifest, name string) string {
	t := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, m.Title)
	t = strings.Join(strings.Fields(t), " ")
	// Cut by bytes on a rune boundary, to the protocol's cap (CJK is three bytes a rune).
	for len(t) > pagehost.MaxTitle {
		_, size := utf8.DecodeLastRuneInString(t)
		t = t[:len(t)-size]
	}
	t = strings.TrimSpace(t)
	if t == "" {
		t = name
	}
	return t
}
