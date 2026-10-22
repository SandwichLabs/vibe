package main

// Capabilities of a scripted page (docs/plans/html-artifacts.md, H5, step P5):
// state, download and ask. A capability exists for a window only if the manifest
// declared it (so the allow decision and the PageHash cover it) and Studio
// granted it at open; every call is checked against that set, whatever the page
// sends. Each one is a method behind the pipe: none is an agent tool, none is
// reachable from MCP, and the artifact is the window's, never the message's.

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/pagehost"
	"github.com/SandwichLabs/vibe/internal/pagestate"
)

// askPerMinute is the `ask` rate for one artifact.
const askPerMinute = 10

// maxUploads is how many chunked arguments a window may be assembling at once.
const maxUploads = 2

// pageServices is what the capabilities need from the App. Tests give fakes.
type pageServices struct {
	// state returns the artifact's store (the artifact is fixed by the closure).
	state func() (*pagestate.Store, error)
	// save asks the person where to put a file (the native save dialog) and
	// returns the chosen path, "" when they cancel.
	save func(ctx context.Context, title, filename, ext string) (string, error)
	// ask is one tool-less completion; configured is false when no provider is set.
	ask func(ctx context.Context, system, prompt string) (text string, configured bool, err error)
	// askOn is the separate `ask` toggle at this page hash.
	askOn func(hash string) bool
	// activity records a use in the agent activity log.
	activity func(ActivityEntry)
}

// ActivityEntry is one line of the agent activity log: what a page asked of the
// model, never the text itself.
type ActivityEntry struct {
	At          time.Time `json:"at"`
	Source      string    `json:"source"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	PromptBytes int       `json:"promptBytes"`
	ReplyBytes  int       `json:"replyBytes"`
	Outcome     string    `json:"outcome"`
}

// grantedCaps is the capabilities a window gets: declared by the manifest, known,
// and for ask also switched on by the person.
func grantedCaps(a *artifacts.Artifact, svc pageServices, hash string) []string {
	var out []string
	for _, n := range artifacts.CapabilityNames(&a.Manifest) {
		switch n {
		case artifacts.CapState, artifacts.CapDownload:
			out = append(out, n)
		case artifacts.CapAsk:
			if svc.askOn != nil && svc.askOn(hash) {
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// upload is a call argument arriving in chunks.
type upload struct {
	next    int
	buf     []byte
	done    bool
	started time.Time
}

// uploadTTL is how long an upload may wait to be claimed by a call.
const uploadTTL = time.Minute

// putBytesPerSec is the bytes of upload chunks a window may send in a second.
func (h *pageHost) putBytesPerSec() int {
	if h.putLimit > 0 {
		return h.putLimit
	}
	return pagehost.MaxPutBytesPerSecond
}

// handlePut takes one chunk of an upload and acknowledges it. A chunk is only
// ever sent by the injected API of a window that was granted a capability, in
// order, within the caps: anything else is a helper that is not behaving, and
// the window ends.
func (w *pageWindow) handlePut(m *pagehost.Message) {
	w.upMu.Lock()
	now := time.Now()
	for id, old := range w.uploads {
		if now.Sub(old.started) > uploadTTL {
			delete(w.uploads, id) // never claimed by a call
		}
	}
	u := w.uploads[m.Up]
	bad := len(w.caps) == 0
	if u == nil && !bad {
		bad = m.Seq != 0 || len(w.uploads) >= maxUploads
		u = &upload{started: now}
		w.uploads[m.Up] = u
	}
	if !bad {
		bad = u.done || m.Seq != u.next || len(u.buf)+len(m.Text) > pagehost.MaxUploadBytes
	}
	if bad {
		delete(w.uploads, m.Up)
		w.upMu.Unlock()
		w.finish(pageStateStopped)
		return
	}
	u.next++
	u.buf = append(u.buf, m.Text...)
	u.done = m.Last
	w.upMu.Unlock()
	w.succeed(m.ID, []byte("{}"))
}

// takeArg is the call's argument: inline, or the finished upload it names.
func (w *pageWindow) takeArg(m *pagehost.Message) ([]byte, bool) {
	if m.Up == 0 {
		return m.Arg, true
	}
	w.upMu.Lock()
	defer w.upMu.Unlock()
	u := w.uploads[m.Up]
	delete(w.uploads, m.Up)
	if u == nil || !u.done {
		return nil, false
	}
	return u.buf, true
}

// handleCall runs a capability call: declared and granted, within the window's
// limits, and the page still allowed. Anything else gets a fixed code.
func (w *pageWindow) handleCall(m *pagehost.Message) {
	// A call that names an upload takes it first, so whatever else is refused
	// below, the upload does not stay.
	arg, ok := w.takeArg(m)
	if !w.caps[m.Cap] {
		w.fail(m.ID, pagehost.CodeUnavailable)
		return
	}
	if !w.limit.Acquire(time.Now()) {
		w.fail(m.ID, pagehost.CodeLimit)
		return
	}
	defer w.limit.Release()
	if !w.stillAllowed() {
		w.fail(m.ID, pagehost.CodeRefused)
		return
	}
	if !ok {
		w.fail(m.ID, pagehost.CodeRefused)
		return
	}
	var res []byte
	var code string
	switch m.Cap {
	case artifacts.CapState:
		res, code = w.callState(m.Op, arg)
	case artifacts.CapDownload:
		res, code = w.callDownload(m.Op, arg)
	case artifacts.CapAsk:
		res, code = w.callAsk(m.Op, arg)
	default:
		code = pagehost.CodeUnavailable
	}
	if code != "" {
		w.fail(m.ID, code)
		return
	}
	if len(res) > resultCap/2 {
		w.fail(m.ID, pagehost.CodeTooLarge)
		return
	}
	w.succeed(m.ID, res)
}

// decodeArg reads a capability argument strictly: known fields only.
func decodeArg(raw []byte, into any) bool {
	if len(raw) == 0 {
		return false
	}
	dec := json.NewDecoder(bytesReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(into) == nil && !dec.More()
}

// stateStores caches one store per artifact for the host.
type stateStores struct {
	mu sync.Mutex
	m  map[string]*pagestate.Store
}

// stateStore returns the artifact's store in the workspace at root.
func (h *pageHost) stateStore(root, name string, onErr func(error)) (*pagestate.Store, error) {
	h.stores.mu.Lock()
	defer h.stores.mu.Unlock()
	key := root + "\x00" + name
	if s := h.stores.m[key]; s != nil {
		return s, nil
	}
	s, err := pagestate.Open(root, name, pagestate.WithErrorHook(onErr))
	if err != nil {
		return nil, err
	}
	if h.stores.m == nil {
		h.stores.m = map[string]*pagestate.Store{}
	}
	h.stores.m[key] = s
	return s, nil
}

// flushStates writes every store's pending changes; forget also drops them.
func (h *pageHost) flushStates(forget bool) {
	if h == nil {
		return
	}
	h.stores.mu.Lock()
	all := make([]*pagestate.Store, 0, len(h.stores.m))
	for _, s := range h.stores.m {
		all = append(all, s)
	}
	if forget {
		h.stores.m = nil
	}
	h.stores.mu.Unlock()
	for _, s := range all {
		if err := s.Flush(); err != nil && h.logger != nil {
			h.logger.Debug("page state not flushed", "error", err)
		}
	}
}

// askRate is the artifact's ask limiter.
func (h *pageHost) askRate(name string) *pagehost.Rate {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asks == nil {
		h.asks = map[string]*pagehost.Rate{}
	}
	r := h.asks[name]
	if r == nil {
		r = pagehost.NewRate(askPerMinute, time.Minute)
		h.asks[name] = r
	}
	return r
}

// saveThrottle bounds the save dialogs one artifact may open: at most 3 a
// minute, and none for 10 seconds after the person cancels one, so a page that
// posts straight to the message handler cannot keep a dialog in front of them.
type saveThrottle struct {
	mu        sync.Mutex
	opened    []time.Time
	cancelled time.Time
}

const (
	saveCooldown = 10 * time.Second
	savesPerMin  = 3
)

func (t *saveThrottle) allow(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.cancelled.IsZero() && now.Sub(t.cancelled) < saveCooldown {
		return false
	}
	keep := t.opened[:0]
	for _, o := range t.opened {
		if now.Sub(o) < time.Minute {
			keep = append(keep, o)
		}
	}
	t.opened = keep
	if len(t.opened) >= savesPerMin {
		return false
	}
	t.opened = append(t.opened, now)
	return true
}

func (t *saveThrottle) cancel(now time.Time) {
	t.mu.Lock()
	t.cancelled = now
	t.mu.Unlock()
}

func (h *pageHost) saveThrottleFor(name string) *saveThrottle {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.saves == nil {
		h.saves = map[string]*saveThrottle{}
	}
	t := h.saves[name]
	if t == nil {
		t = &saveThrottle{}
		h.saves[name] = t
	}
	return t
}
