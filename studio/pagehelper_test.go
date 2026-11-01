package main

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/pagehost"
)

type fakeNative struct {
	mu        sync.Mutex
	responses map[uint64]fakeResponse
	evals     []string
	quits     int
	inputs    int // physical inputs the fake person has given
	changed   chan struct{}
}

type fakeResponse struct {
	ok   bool
	ct   string
	body string
}

func newFakeNative() *fakeNative {
	return &fakeNative{responses: map[uint64]fakeResponse{}, changed: make(chan struct{}, 64)}
}

func (n *fakeNative) respond(id uint64, ok bool, ct string, body []byte) {
	n.mu.Lock()
	n.responses[id] = fakeResponse{ok, ct, string(body)}
	n.mu.Unlock()
	n.changed <- struct{}{}
}
func (n *fakeNative) eval(js string) {
	n.mu.Lock()
	n.evals = append(n.evals, js)
	n.mu.Unlock()
	n.changed <- struct{}{}
}
func (n *fakeNative) takeInput() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.inputs > 0 {
		n.inputs--
		return true
	}
	return false
}
func (n *fakeNative) quit() {
	n.mu.Lock()
	n.quits++
	n.mu.Unlock()
	n.changed <- struct{}{}
}
func (n *fakeNative) response(id uint64) (fakeResponse, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	r, ok := n.responses[id]
	return r, ok
}

func (n *fakeNative) waitResponse(t *testing.T, id uint64) fakeResponse {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if r, ok := n.response(id); ok {
			return r
		}
		select {
		case <-n.changed:
		case <-deadline:
			t.Fatalf("no response to request %d", id)
		}
	}
}

// helperRig is a pageHelper wired to in-memory pipes: studio writes what
// Studio would send, toStudio holds what the helper sent.
type helperRig struct {
	h        *pageHelper
	nat      *fakeNative
	studio   *pagehost.Writer
	toStudio chan *pagehost.Message
	closeIn  func()
}

func newHelperRig(t *testing.T) *helperRig {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &pageHelper{
		in:      pagehost.NewLineReader(inR, pagehost.ToHelper),
		out:     pagehost.NewWriter(outW, pagehost.FromHelper),
		files:   map[string]*servedFile{},
		partial: map[string]*chunkAssembly{},
		docs:    map[int64]*pendingDoc{},
	}
	rig := &helperRig{h: h, nat: newFakeNative(), studio: pagehost.NewWriter(inW, pagehost.ToHelper), toStudio: make(chan *pagehost.Message, 64)}
	h.nat = rig.nat
	rig.closeIn = func() { _ = inW.Close() }
	go func() {
		lr := pagehost.NewLineReader(outR, pagehost.FromHelper)
		for {
			line, err := lr.Next()
			if err != nil {
				return
			}
			if m, err := pagehost.Decode(line, pagehost.FromHelper); err == nil {
				rig.toStudio <- m
			}
		}
	}()
	t.Cleanup(func() { _ = inW.Close(); _ = outW.Close() })
	return rig
}

func (r *helperRig) setup(t *testing.T, query string, files map[string]string) {
	t.Helper()
	go func() {
		send := func(m *pagehost.Message) { _ = r.studio.Send(m) }
		send(&pagehost.Message{T: pagehost.TypeOpen, Name: "pg", Title: "T", W: 800, H: 600, Params: json.RawMessage(`{}`), Q: query})
		for p, body := range files {
			send(&pagehost.Message{T: pagehost.TypeFile, Path: p, CT: jsType, Last: true, Data: []byte(body)})
		}
		send(&pagehost.Message{T: pagehost.TypeDocr, Status: 200, Last: true, Data: []byte("<p>doc0</p>")})
		send(&pagehost.Message{T: pagehost.TypeGo})
	}()
	require.NoError(t, r.h.receiveSetup())
}

func TestHelper_SetupHoldsWhatStudioSent(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "a=1", map[string]string{"app.js": "1", "assets/x.js": "2"})
	assert.Equal(t, "pg", r.h.open.Name)
	assert.Equal(t, "1", string(r.h.files["app.js"].data))
	assert.Equal(t, "<p>doc0</p>", string(r.h.doc0.data))
}

func TestHelper_SetupRefusesWhatIsOutOfOrder(t *testing.T) {
	cases := map[string][]*pagehost.Message{
		"go first":     {{T: pagehost.TypeGo}},
		"go no doc":    {{T: pagehost.TypeOpen, Name: "pg", Title: "T", W: 800, H: 600, Params: json.RawMessage(`{}`)}, {T: pagehost.TypeGo}},
		"two opens":    {{T: pagehost.TypeOpen, Name: "pg", Title: "T", W: 800, H: 600, Params: json.RawMessage(`{}`)}, {T: pagehost.TypeOpen, Name: "pg", Title: "T", W: 800, H: 600, Params: json.RawMessage(`{}`)}},
		"a reply":      {{T: pagehost.TypeReply, ID: 1, OK: boolp(false), Code: pagehost.CodeLimit}},
		"a request id": {{T: pagehost.TypeDocr, ID: 4, Status: 200, Last: true}},
		"chunk skip":   {{T: pagehost.TypeFile, Path: "a.js", CT: jsType, Seq: 1, Last: true}},
		"ct change": {
			{T: pagehost.TypeFile, Path: "a.js", CT: jsType, Seq: 0, Data: []byte("x")},
			{T: pagehost.TypeFile, Path: "a.js", CT: cssType, Seq: 1, Last: true},
		},
		"dup file": {
			{T: pagehost.TypeFile, Path: "a.js", CT: jsType, Last: true},
			{T: pagehost.TypeFile, Path: "a.js", CT: jsType, Last: true},
		},
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			r := newHelperRig(t)
			go func() {
				for _, m := range msgs {
					_ = r.studio.Send(m)
				}
			}()
			assert.Error(t, r.h.receiveSetup())
		})
	}
}

func boolp(b bool) *bool { return &b }

func TestHelper_ServeAnswersOnlyWhatStudioSent(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", map[string]string{"app.js": "alert(1)", "assets/x.js": "x"})
	h := r.h
	for i, tc := range []struct {
		url  string
		ok   bool
		body string
	}{
		{"assay-page://pg/", true, "<p>doc0</p>"},
		{"assay-page://PG/app.js", true, "alert(1)"},
		{"assay-page://pg/assets/x.js", true, "x"},
		{"assay-page://pg/app.js?x=1", false, ""},
		{"assay-page://pg/missing.js", false, ""},
		{"assay-page://pg/index.html", false, ""},
		{"assay-page://pg/../app.js", false, ""},
		{"assay-page://other/app.js", false, ""},
		{"https://pg/app.js", false, ""},
		{"assay-page://user@pg/app.js", false, ""},
		{"assay-page://pg/%61pp.js", true, "alert(1)"}, // the same file, decoded
		{"assay-page://pg/%2e%2e/app.js", false, ""},
	} {
		id := uint64(100 + i)
		h.serve(id, tc.url)
		got := r.nat.waitResponse(t, id)
		assert.Equal(t, tc.ok, got.ok, tc.url)
		assert.Equal(t, tc.body, got.body, tc.url)
	}
}

func TestHelper_ANavigationAsksStudioForTheNewDocument(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", nil)
	go r.h.pump()
	r.h.serve(7, "assay-page://pg/?region=east")
	var ask *pagehost.Message
	select {
	case ask = <-r.toStudio:
	case <-time.After(3 * time.Second):
		t.Fatal("no doc request")
	}
	require.Equal(t, pagehost.TypeDoc, ask.T)
	assert.Equal(t, "region=east", ask.Q)
	_, answered := r.nat.response(7)
	assert.False(t, answered, "it waits for Studio")

	require.NoError(t, r.studio.Send(&pagehost.Message{T: pagehost.TypeDocr, ID: ask.ID, Status: 200, Seq: 0, Data: []byte("<p>east")}))
	require.NoError(t, r.studio.Send(&pagehost.Message{T: pagehost.TypeDocr, ID: ask.ID, Status: 200, Seq: 1, Last: true, Data: []byte("</p>")}))
	got := r.nat.waitResponse(t, 7)
	assert.True(t, got.ok)
	assert.Equal(t, "<p>east</p>", got.body)
	assert.Equal(t, docType, got.ct)
}

func TestHelper_MessagesFromThePageAreCheckedAgain(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", nil)
	h := r.h
	next := func() *pagehost.Message {
		select {
		case m := <-r.toStudio:
			return m
		case <-time.After(time.Second):
			return nil
		}
	}

	h.message(`{"t":"hb"}`)
	assert.Equal(t, pagehost.TypeHeartbeat, next().T)
	h.message(`{"t":"run","id":3,"q":"by_region","v":{"a":1}}`)
	m := next()
	require.NotNil(t, m)
	assert.Equal(t, "by_region", m.Q)

	// A page cannot speak as the helper (ready, closed, gone, doc) or as Studio, and
	// cannot add a field. Each of these is refused; the ones with an id are told so.
	for _, bad := range []string{
		`{"t":"ready"}`, `{"t":"closed"}`, `{"t":"gone"}`, `{"t":"doc","id":9,"q":""}`, `{"t":"quit"}`,
		`{"t":"run","id":10,"q":"x","artifact":"other"}`, `{"t":"run","id":11,"q":"x y"}`, `not json`, `{"t":"run","id":12}`,
	} {
		h.message(bad)
	}
	assert.Nil(t, next(), "nothing reaches Studio")

	r.nat.mu.Lock()
	defer r.nat.mu.Unlock()
	joined := strings.Join(r.nat.evals, "\n")
	for _, id := range []string{"9", "10", "11", "12"} {
		assert.Contains(t, joined, "window.__assayReply("+id+",false,\"refused\")", id)
	}

	h.message(`{"t":"run","id":13,"q":"x","v":{"k":"` + strings.Repeat("a", pagehost.MaxFromHelper) + `"}}`)
	assert.NotContains(t, strings.Join(r.nat.evals, "\n"), "(13,", "an oversized message is dropped")
}

func TestHelper_RepliesBecomeScriptsAndQuitQuits(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", nil)
	go r.h.pump()
	require.NoError(t, r.studio.Send(&pagehost.Message{T: pagehost.TypeReply, ID: 4, OK: boolp(true), Result: json.RawMessage(`{"rows":[[1]]}`)}))
	require.NoError(t, r.studio.Send(&pagehost.Message{T: pagehost.TypeReply, ID: 5, OK: boolp(false), Code: pagehost.CodeLimit}))
	require.NoError(t, r.studio.Send(&pagehost.Message{T: pagehost.TypeQuit}))
	require.Eventually(t, func() bool { r.nat.mu.Lock(); defer r.nat.mu.Unlock(); return r.nat.quits == 1 }, 3*time.Second, 5*time.Millisecond)
	r.nat.mu.Lock()
	defer r.nat.mu.Unlock()
	require.Len(t, r.nat.evals, 2)
	assert.Contains(t, r.nat.evals[0], "window.__assayReply(4,true,JSON.parse(")
	assert.Equal(t, `window.__assayReply(5,false,"limit")`, r.nat.evals[1])
}

func TestHelper_StudioGoingAwayQuitsTheWindow(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", nil)
	go r.h.pump()
	r.closeIn()
	require.Eventually(t, func() bool { r.nat.mu.Lock(); defer r.nat.mu.Unlock(); return r.nat.quits == 1 }, 3*time.Second, 5*time.Millisecond)
}

func TestHelper_EventsBecomeMessagesOnce(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", nil)
	r.h.event(evLoaded)
	r.h.event(evLoaded)
	r.h.event(evWebGone)
	r.h.event(evClosed)
	var got []string
	for i := 0; i < 3; i++ {
		select {
		case m := <-r.toStudio:
			got = append(got, m.T)
		case <-time.After(time.Second):
			t.Fatal("missing event")
		}
	}
	assert.Equal(t, []string{pagehost.TypeReady, pagehost.TypeGone, pagehost.TypeClosed}, got)
}

// The gesture of a save is the native input the helper saw, not the page's say-so:
// a page that posts the call straight to the message handler (no injected API)
// gets nothing through without a physical input, and one input pays for one call.
func TestHelper_ADownloadCallNeedsANativeInputAndConsumesIt(t *testing.T) {
	r := newHelperRig(t)
	r.setup(t, "", nil)
	h := r.h
	call := func(id int) string {
		return `{"t":"call","id":` + itoa(id) + `,"cap":"download","op":"save","arg":{"filename":"a","type":"csv","enc":"text","data":"x"}}`
	}
	next := func() *pagehost.Message {
		select {
		case m := <-r.toStudio:
			return m
		case <-time.After(300 * time.Millisecond):
			return nil
		}
	}
	h.message(call(1))
	assert.Nil(t, next(), "no physical input: nothing reaches Studio")
	require.Eventually(t, func() bool { return len(r.nat.evalsCopy()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Contains(t, r.nat.evalsCopy()[0], `"gesture"`, "the page is told, with the fixed code")

	r.nat.mu.Lock()
	r.nat.inputs = 1
	r.nat.mu.Unlock()
	h.message(call(2))
	m := next()
	require.NotNil(t, m)
	assert.Equal(t, "download", m.Cap)
	h.message(call(3))
	assert.Nil(t, next(), "one input paid for one save")
	// Other capabilities do not ask for one.
	h.message(`{"t":"call","id":4,"cap":"state","op":"get","arg":{"key":"a"}}`)
	require.NotNil(t, next())
}

func (n *fakeNative) evalsCopy() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.evals...)
}
