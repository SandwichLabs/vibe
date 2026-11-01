package main

// Helper mode: the Studio binary started as `assay-studio --view-host` by Studio
// itself (docs/plans/html-artifacts.md, H4). It never starts Wails. It reads
// one `open`, the page's files and `go` from Studio over a dedicated pipe,
// holds them in memory, and runs a native web view that serves them under
// assay-page://<name>/. It is given no workspace path, connection, credential or
// store: a page's data requests go back over the pipe, to Studio, which owns
// the artifact. The page and the pipe are both untrusted from here: every
// string the page posts is shape-checked again before it is forwarded, and only
// four message types may come from a page.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/assay/internal/pagehost"
)

// helperExit codes (the helper's own; Studio only looks at "it stopped").
const (
	helperOK       = 0
	helperRefused  = 3
	helperBadSetup = 4
)

// Exit codes the macOS helper uses when it cannot make its own layers
// (pagewindow_darwin.m): the rule list does not compile, or the proxy socket
// cannot be bound. Studio shows these as a setup failure, not as a hang.
const (
	helperRulesFailed = 5
	helperNoProxyPort = 6
)

// isSetupFailure says a helper's exit code means it could not set itself up.
func isSetupFailure(code int) bool { return code == helperRulesFailed || code == helperNoProxyPort }

// docWait is how long a navigation waits for Studio to render its document.
const docWait = 35 * time.Second

// forcedExitAfter bounds how long a helper outlives its Studio.
const forcedExitAfter = 2 * time.Second

type servedFile struct {
	ct   string
	data []byte
}

// chunkAssembly collects the chunks of one file or document, in order.
type chunkAssembly struct {
	next int
	buf  []byte
	ct   string
}

type pendingDoc struct {
	native uint64
	asm    chunkAssembly
	timer  *time.Timer
}

// pageHelper is the helper's state: what Studio sent and what the page asked.
type pageHelper struct {
	in  *pagehost.LineReader
	out *pagehost.Writer
	nat nativeOps
	// exit ends the process; nil in tests.
	exit func(int)

	mu      sync.Mutex
	open    *pagehost.Message
	files   map[string]*servedFile
	partial map[string]*chunkAssembly
	total   int
	// doc0 is the entry document for open.q, the one the window loads first.
	doc0    *servedFile
	doc0Asm chunkAssembly
	docs    map[int64]*pendingDoc
	nextDoc int64
	loaded  bool
}

func newPageHelper(in, out *os.File) *pageHelper {
	return &pageHelper{
		nat:     realNative{},
		exit:    os.Exit,
		in:      pagehost.NewLineReader(in, pagehost.ToHelper),
		out:     pagehost.NewWriter(out, pagehost.FromHelper),
		files:   map[string]*servedFile{},
		partial: map[string]*chunkAssembly{},
		docs:    map[int64]*pendingDoc{},
	}
}

// helperPreflight is the platform's check that the helper is where it must be.
// It is a variable only so the test binary, which is also the helper in the
// live tests, can run its control without the gate.
var helperPreflight = platformHelperPreflight

// runPageHelper is the whole of helper mode. It returns the process exit code.
func runPageHelper() int {
	in, out, err := claimHelperPipes()
	if err != nil {
		return helperRefused
	}
	if !nativeAvailable || helperPreflight() != nil {
		return helperRefused
	}
	sweepStaleRules()
	h := newPageHelper(in, out)
	if err := h.receiveSetup(); err != nil {
		return helperBadSetup
	}
	go h.pump()
	cfg := nativeConfig{
		name: h.open.Name, title: h.open.Title + " — Assay page", width: h.open.W, height: h.open.H,
		script: pagehost.APIScript(h.open.Params, h.open.Caps), query: h.open.Q,
	}
	if err := nativeRun(cfg, h); err != nil {
		return helperRefused
	}
	return helperOK
}

// receiveSetup reads messages until `go`, which must follow an `open` and the
// initial document.
func (h *pageHelper) receiveSetup() error {
	for {
		m, err := h.next()
		if err != nil {
			return err
		}
		if m.T == pagehost.TypeGo {
			if h.open == nil || h.doc0 == nil {
				return errors.New("go before the page was sent")
			}
			return nil
		}
		if err := h.accept(m); err != nil {
			return err
		}
	}
}

func (h *pageHelper) next() (*pagehost.Message, error) {
	line, err := h.in.Next()
	if err != nil {
		return nil, fmt.Errorf("failed to read from Studio: %w", err)
	}
	return pagehost.Decode(line, pagehost.ToHelper)
}

// accept handles a setup message.
func (h *pageHelper) accept(m *pagehost.Message) error {
	switch m.T {
	case pagehost.TypeOpen:
		if h.open != nil {
			return errors.New("a second open")
		}
		h.open = m
		return nil
	case pagehost.TypeFile:
		return h.addFileChunk(m)
	case pagehost.TypeDocr:
		if m.ID != 0 {
			return errors.New("a reply to a request nobody made")
		}
		return h.addDoc0Chunk(m)
	}
	return fmt.Errorf("unexpected %q during setup", m.T)
}

func (a *chunkAssembly) add(m *pagehost.Message, limit int) (done bool, err error) {
	if m.Seq != a.next {
		return false, errors.New("chunks out of order")
	}
	a.next++
	if len(a.buf)+len(m.Data) > limit {
		return false, errors.New("file too large")
	}
	a.buf = append(a.buf, m.Data...)
	return m.Last, nil
}

func (h *pageHelper) addFileChunk(m *pagehost.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	asm := h.partial[m.Path]
	if asm == nil {
		if _, dup := h.files[m.Path]; dup || len(h.files)+len(h.partial) >= pagehost.MaxFiles {
			return errors.New("too many or repeated files")
		}
		asm = &chunkAssembly{ct: m.CT}
		h.partial[m.Path] = asm
	}
	if asm.ct != m.CT {
		return errors.New("content type changed between chunks")
	}
	done, err := asm.add(m, pagehost.MaxFileBytes)
	if err != nil {
		return err
	}
	if h.total += len(m.Data); h.total > pagehost.MaxTotalBytes {
		return errors.New("too many bytes")
	}
	if done {
		h.files[m.Path] = &servedFile{ct: asm.ct, data: asm.buf}
		delete(h.partial, m.Path)
	}
	return nil
}

func (h *pageHelper) addDoc0Chunk(m *pagehost.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	done, err := h.doc0Asm.add(m, pagehost.MaxDocBytes)
	if err != nil {
		return err
	}
	if h.total += len(m.Data); h.total > pagehost.MaxTotalBytes {
		return errors.New("too many bytes")
	}
	if done {
		h.doc0 = &servedFile{ct: docType, data: h.doc0Asm.buf}
	}
	return nil
}

const docType = "text/html; charset=utf-8"

// pump reads Studio's messages while the window runs. When the pipe closes,
// Studio is gone, and so is the window.
func (h *pageHelper) pump() {
	for {
		m, err := h.next()
		if err != nil {
			h.nat.quit()
			// Studio is gone. The quit above is only queued on the window's loop; if
			// the loop is wedged, this still ends the process.
			if h.exit != nil {
				time.AfterFunc(forcedExitAfter, func() { h.exit(helperOK) })
			}
			return
		}
		switch m.T {
		case pagehost.TypeReply:
			h.reply(m)
		case pagehost.TypePush:
			h.nat.eval(pagehost.EventScript(m.Cap, m.Arg))
		case pagehost.TypeDocr:
			h.docChunk(m)
		case pagehost.TypeQuit:
			h.nat.quit()
			return
		default:
			h.nat.quit()
			return
		}
	}
}

func (h *pageHelper) reply(m *pagehost.Message) {
	if *m.OK {
		h.nat.eval(pagehost.ReplyScript(m.ID, true, m.Result, ""))
		return
	}
	h.nat.eval(pagehost.ReplyScript(m.ID, false, nil, m.Code))
}

// docChunk collects a navigation's document and answers the waiting request.
func (h *pageHelper) docChunk(m *pagehost.Message) {
	h.mu.Lock()
	d := h.docs[m.ID]
	if d == nil {
		h.mu.Unlock()
		return
	}
	done, err := d.asm.add(m, pagehost.MaxDocBytes)
	if err != nil || done {
		delete(h.docs, m.ID)
		d.timer.Stop()
	}
	h.mu.Unlock()
	switch {
	case err != nil:
		h.nat.respond(d.native, false, "", nil)
	case done:
		h.nat.respond(d.native, true, docType, d.asm.buf)
	}
}

// serve answers a request to assay-page://<name>/...: the entry document, or a
// file Studio sent. Nothing else exists.
func (h *pageHelper) serve(id uint64, rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "assay-page" || !strings.EqualFold(u.Host, h.open.Name) || u.User != nil {
		h.nat.respond(id, false, "", nil)
		return
	}
	if u.Path == "/" || u.Path == "" {
		h.serveDoc(id, u.RawQuery)
		return
	}
	rel := strings.TrimPrefix(u.Path, "/")
	h.mu.Lock()
	f := h.files[rel]
	h.mu.Unlock()
	if f == nil || u.RawQuery != "" {
		h.nat.respond(id, false, "", nil)
		return
	}
	h.nat.respond(id, true, f.ct, f.data)
}

// serveDoc answers the entry document: the one sent with the page when the
// query is the opening one, else Studio renders it for the new query string.
func (h *pageHelper) serveDoc(id uint64, query string) {
	if query == h.open.Q {
		h.nat.respond(id, true, h.doc0.ct, h.doc0.data)
		return
	}
	h.mu.Lock()
	if len(h.docs) >= pagehost.MaxInFlight {
		h.mu.Unlock()
		h.nat.respond(id, false, "", nil)
		return
	}
	h.nextDoc++
	did := h.nextDoc
	d := &pendingDoc{native: id}
	d.timer = time.AfterFunc(docWait, func() { h.docTimeout(did) })
	h.docs[did] = d
	h.mu.Unlock()
	if err := h.out.Send(&pagehost.Message{T: pagehost.TypeDoc, ID: did, Q: query}); err != nil {
		h.docTimeout(did)
	}
}

func (h *pageHelper) docTimeout(id int64) {
	h.mu.Lock()
	d := h.docs[id]
	delete(h.docs, id)
	h.mu.Unlock()
	if d != nil {
		d.timer.Stop()
		h.nat.respond(d.native, false, "", nil)
	}
}

// message handles a string the page posted. It is untrusted: only a heartbeat,
// a run, a capability call and a chunk of a call's argument may come from a page, each shape-checked again,
// and what fails the check is answered with a fixed code.
func (h *pageHelper) message(s string) {
	if len(s) > pagehost.MaxFromHelper {
		return
	}
	m, err := pagehost.Decode([]byte(s), pagehost.FromHelper)
	if err != nil || (m.T != pagehost.TypeHeartbeat && m.T != pagehost.TypeRun && m.T != pagehost.TypeCall && m.T != pagehost.TypePut) {
		h.refuse(s)
		return
	}
	// The gesture of a save is the person's own input, seen by the native
	// widget: the page's say-so (or its API's check) is not enough, since a page
	// can post to this handler directly. One input pays for one save.
	if m.T == pagehost.TypeCall && m.Cap == "download" && !h.nat.takeInput() {
		h.nat.eval(pagehost.ReplyScript(m.ID, false, nil, pagehost.CodeGesture))
		return
	}
	if err := h.out.Send(m); err != nil {
		h.nat.quit()
	}
}

// refuse tells a page whose request was malformed that it was refused, when it
// has an id to be told by; it never echoes the page's text.
func (h *pageHelper) refuse(s string) {
	var probe struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal([]byte(s), &probe) == nil && probe.ID > 0 && probe.ID <= 1<<31 {
		h.nat.eval(pagehost.ReplyScript(probe.ID, false, nil, pagehost.CodeRefused))
	}
}

// event reports a window event to Studio.
func (h *pageHelper) event(ev nativeEvent) {
	switch ev {
	case evLoaded:
		h.mu.Lock()
		first := !h.loaded
		h.loaded = true
		h.mu.Unlock()
		if first {
			_ = h.out.Send(&pagehost.Message{T: pagehost.TypeReady})
		}
	case evWebGone:
		_ = h.out.Send(&pagehost.Message{T: pagehost.TypeGone})
	case evClosed:
		_ = h.out.Send(&pagehost.Message{T: pagehost.TypeClosed})
	}
}
