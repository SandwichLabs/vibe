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

	"github.com/SandwichLabs/assay/internal/artifacts"
	"github.com/SandwichLabs/assay/internal/pagehost"
	"github.com/SandwichLabs/assay/internal/pagestate"
	"github.com/SandwichLabs/assay/internal/workspace"
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
	// pageQuitGrace is how long a helper is given to exit after being asked to
	// quit (the person closed the window), before the tree is killed.
	pageQuitGrace = time.Second
	// helperReapWait is how long the manager waits, when a helper's pipe closes,
	// for the process to be reaped so its exit code can be read.
	helperReapWait = 500 * time.Millisecond
	// pageVerdictCache is how long a positive "still allowed" is reused by the
	// bridge, as the static route does.
	pageVerdictCache = 500 * time.Millisecond
)

// Window states, the "state" of a pagewindow:state event.
const (
	pageStateOpen    = "open"    // the page loaded
	pageStateStopped = "stopped" // it stopped responding, crashed or broke the protocol
	pageStateSetup   = "setup"   // the helper could not make its own network gate (macOS)
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
	quitGrace time.Duration

	// openMu serialises opens, so the cap and one-window-per-artifact hold. gen
	// moves when closeAll ends every window, and gens[name] when a revoke or a
	// change ends one artifact's, so a window that was being started when that
	// happened is not kept (and an open of another artifact is not disturbed).
	openMu sync.Mutex
	mu     sync.Mutex
	wins   map[string]*pageWindow
	// closing are windows the person closed that are within their quit grace: out of
	// wins, but a revoke, a change or a close-all still kills them at once.
	closing map[*pageWindow]struct{}
	gen     uint64
	gens    map[string]uint64
	// stores are the state stores by workspace and artifact, asks the ask
	// limiter by artifact (pagecaps.go).
	stores stateStores
	asks   map[string]*pagehost.Rate
	saves  map[string]*saveThrottle
	// putLimit is the bytes of upload chunks a window may send in a second (0: the default).
	putLimit int
}

func newPageHost(logger *slog.Logger, libs fs.FS, emit func(event string, data ...any)) *pageHost {
	return &pageHost{
		launch: launcherFor(recordExe()), preflight: pageHostPreflight, logger: logger, emit: emit, libs: libs,
		beatLimit: pageBeatTimeout, grace: pageStartGrace, quitGrace: pageQuitGrace, wins: map[string]*pageWindow{}, closing: map[*pageWindow]struct{}{}, gens: map[string]uint64{},
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
	runner  func(m *artifacts.Manifest, pins map[string]string) artifacts.PageRunner
	allowed func(hash string) bool
	base    context.Context
	// svc is what the capabilities call (pagecaps.go).
	svc pageServices
}

// pageWindow is one open window. The artifact is pinned here, at open: no
// message from the page names one.
type pageWindow struct {
	host    *pageHost
	ws      any // the workspace it was opened in
	name    string
	hash    string
	page    *artifacts.Artifact
	conn    helperConn
	out     *pagehost.Writer
	run     artifacts.PageRunner
	allowed func(hash string) bool
	ctx     context.Context
	cancel  context.CancelFunc

	svc     pageServices
	caps    map[string]bool
	store   *pagestate.Store
	unsub   func()
	upMu    sync.Mutex
	uploads map[int64]*upload

	limit pagehost.Limiter
	flood pagehost.Flood
	// putFlood counts upload chunks apart from other lines (pagehost_bridge.go).
	putFlood pagehost.Flood
	putBytes pagehost.Flood
	started  time.Time
	beat     atomic.Int64

	verdictMu sync.Mutex
	verdictAt time.Time

	once  sync.Once
	final string
	// escalate closes when something else ends a closing window at once.
	escalate     chan struct{}
	escalateOnce sync.Once
}

// open starts a window for the named artifact, which must be an allowed,
// scripted HTML page. The window outlives this call; its end is reported by the
// pagewindow:state event.
func (h *pageHost) open(req pageOpen) (*PageWindowInfo, error) {
	if err := h.preflight(); err != nil {
		return nil, err
	}
	page, hash, pins, err := heldScriptedPage(req.base, req.ws, req.name)
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
	run := req.runner(&page.Manifest, pins)
	granted := grantedCaps(page, req.svc, hash)
	setup, err := buildWindowSetup(ctx, run, page, req, h.libs, granted)
	if err != nil {
		cancel()
		return nil, err
	}
	conn, err := h.launch(ctx, h.logger)
	if err != nil {
		cancel()
		return nil, err
	}
	w := &pageWindow{host: h, ws: req.ws, name: req.name, hash: hash, page: page, conn: conn, out: pagehost.NewWriter(conn, pagehost.ToHelper),
		run: run, allowed: req.allowed, ctx: ctx, cancel: cancel, started: time.Now(),
		escalate: make(chan struct{}), svc: req.svc, caps: map[string]bool{}, uploads: map[int64]*upload{}}
	for _, n := range granted {
		w.caps[n] = true
	}
	if w.caps[artifacts.CapState] && req.svc.state != nil {
		store, serr := req.svc.state()
		if serr != nil {
			cancel()
			conn.Kill()
			return nil, errPageWindowSetup
		}
		w.store = store
	}
	if w.store == nil {
		delete(w.caps, artifacts.CapState)
	}
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
	w.subscribeState()
	go w.serve()
	go w.watch()
	go w.reap()
	return &PageWindowInfo{Name: req.name, Hash: hash}, nil
}

// heldScriptedPage rebuilds the page from the files the decision was made over
// and checks it is allowed, scripted, and still at the hash the person saw.
func heldScriptedPage(ctx context.Context, ws workspace.Workspace, name string) (*artifacts.Artifact, string, map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	st, err := ws.ArtifactViewStateCtx(ctx, name)
	if err != nil {
		return nil, "", nil, viewError(err)
	}
	if st.Kind != artifacts.ViewHTML || st.State != string(stateAllowed) || st.Hash == "" {
		return nil, "", nil, errViewNotAllowed
	}
	if st.PageKind != artifacts.PageScripted {
		return nil, "", nil, errPageNotScripted
	}
	page, err := artifacts.PageFromText(name, []byte(st.Manifest), st.PageFiles)
	if err != nil || page.Page.Hash != st.ContentHash {
		return nil, "", nil, errViewNotAllowed
	}
	return page, st.Hash, st.Pins, nil
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

// closeWindowByPerson ends the named window because the person pressed Close
// window: the helper is asked to quit first (so it can clean up after itself), and
// killed if it has not gone within pageQuitGrace.
func (h *pageHost) closeWindowByPerson(name string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.gens[name]++
	w := h.wins[name]
	h.mu.Unlock()
	if w != nil {
		w.end(pageStateClosed, true)
	}
}

// endWindow ends the named artifact's window without disturbing opens in flight.
func (h *pageHost) endWindow(name, state string) {
	h.mu.Lock()
	w := h.wins[name]
	var hurry []*pageWindow
	for c := range h.closing {
		if c.name == name {
			hurry = append(hurry, c)
		}
	}
	h.mu.Unlock()
	for _, c := range hurry {
		c.hurry()
	}
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
	hurry := make([]*pageWindow, 0, len(h.closing))
	for c := range h.closing {
		hurry = append(hurry, c)
	}
	h.mu.Unlock()
	for _, c := range hurry {
		c.hurry()
	}
	for _, w := range all {
		w.finish(pageStateClosed)
	}
	h.flushStates(true)
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

// quitThenKill asks the helper to exit and kills the tree if it has not within
// the grace. The request is sent apart, so a helper that does not read cannot
// hold up the kill.
func (w *pageWindow) quitThenKill() {
	go func() { _ = w.out.Send(&pagehost.Message{T: pagehost.TypeQuit}) }()
	t := time.NewTimer(w.host.quitGrace)
	defer t.Stop()
	select {
	case <-w.conn.Done():
	case <-w.escalate:
	case <-t.C:
	}
	w.kill()
	w.host.mu.Lock()
	delete(w.host.closing, w)
	w.host.mu.Unlock()
}

// hurry ends a window that is within its quit grace now.
func (w *pageWindow) hurry() { w.escalateOnce.Do(func() { close(w.escalate) }) }

// finish ends the window once, whatever the cause: the first state wins. The
// helper's tree is killed at once.
func (w *pageWindow) finish(state string) { w.end(state, false) }

// end is finish, with the option to let the helper exit by itself for a moment
// first (the person closed the window: nothing is wrong, so there is no hurry).
func (w *pageWindow) end(state string, graceful bool) {
	w.once.Do(func() {
		w.final = state
		w.cancel()
		if graceful {
			w.host.mu.Lock()
			if w.host.closing == nil {
				w.host.closing = map[*pageWindow]struct{}{}
			}
			w.host.closing[w] = struct{}{}
			w.host.mu.Unlock()
			go w.quitThenKill()
		} else {
			w.kill()
		}
		if w.unsub != nil {
			w.unsub()
		}
		if w.store != nil {
			if err := w.store.Flush(); err != nil && w.host.logger != nil {
				w.host.logger.Debug("page state not flushed", "artifact", w.name, "error", err)
			}
		}
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
		w.finish(endedState(w.conn))
	case <-w.ctx.Done():
	}
}

// helperGone ends the window because the helper's pipe closed. A helper's pipe
// closes just before the process is reaped, so it waits a moment for the exit
// code, which tells a failed setup from a crash.
func (w *pageWindow) helperGone() {
	t := time.NewTimer(helperReapWait)
	defer t.Stop()
	select {
	case <-w.conn.Done():
	case <-t.C:
	}
	w.finish(endedState(w.conn))
}

// endedState is the state of a helper that exited on its own: stopped, unless
// it exited with one of the codes for "could not set up its network gate".
func endedState(c helperConn) string {
	select {
	case <-c.Done():
	default:
		return pageStateStopped // still running (or not yet reaped): no exit code to read
	}
	if e, ok := c.(interface{ ExitCode() int }); ok && isSetupFailure(e.ExitCode()) {
		return pageStateSetup
	}
	return pageStateStopped
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
// plain text. The helper adds " — Assay page", and nothing the page does changes it.
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
