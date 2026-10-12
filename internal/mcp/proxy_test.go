package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type echoHandler struct {
	mu    sync.Mutex
	calls []string
	block chan struct{} // when set, tool calls wait on it
}

func (h *echoHandler) Handle(ctx context.Context, req *agents.Request) *agents.Response {
	h.mu.Lock()
	h.calls = append(h.calls, req.Method)
	b := h.block
	h.mu.Unlock()
	if b != nil {
		select {
		case <-b:
		case <-ctx.Done():
		}
	}
	return &agents.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"echo": req.Method}}
}

func (h *echoHandler) called() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

type proxyHarness struct {
	in   *io.PipeWriter
	out  *bufio.Scanner
	done chan error
}

func startProxy(t *testing.T, endpoint, token string) *proxyHarness {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	h := &proxyHarness{in: inW, done: make(chan error, 1)}
	go func() {
		err := ProxyStdio(ctx, ProxyConfig{Endpoint: endpoint, Token: token, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, inR, outW)
		_ = outW.Close()
		h.done <- err
	}()
	h.out = bufio.NewScanner(outR)
	h.out.Buffer(make([]byte, 1<<20), 16<<20)
	t.Cleanup(func() { _ = inW.Close(); _ = outR.Close() })
	return h
}

func (h *proxyHarness) send(t *testing.T, s string) {
	t.Helper()
	_, err := io.WriteString(h.in, s+"\n")
	require.NoError(t, err)
}

func (h *proxyHarness) recv(t *testing.T) map[string]any {
	t.Helper()
	require.True(t, h.out.Scan(), "proxy closed stdout before replying: %v", h.out.Err())
	var m map[string]any
	require.NoError(t, json.Unmarshal(h.out.Bytes(), &m), "every stdout line must be JSON: %q", h.out.Text())
	return m
}

const initReq = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

func startSSE(t *testing.T, h Handler) *Server {
	t.Helper()
	srv := New(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, srv.Start("127.0.0.1:0"))
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

func TestProxyStdio_RoundTrip(t *testing.T) {
	eh := &echoHandler{}
	srv := startSSE(t, eh)
	p := startProxy(t, "http://"+srv.Addr()+"/sse", srv.Token())

	p.send(t, initReq)
	res, ok := p.recv(t)["result"].(map[string]any)
	require.True(t, ok)
	assert.NotNil(t, res["serverInfo"])
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	p.send(t, `{"jsonrpc":"2.0","id":"abc","method":"tools/list"}`)
	reply := p.recv(t)
	assert.Equal(t, "abc", reply["id"], "string ids survive the bridge")
	tools, ok := reply["result"].(map[string]any)["tools"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, tools)

	p.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"introspect_schema","arguments":{}}}`)
	assert.NotNil(t, p.recv(t)["result"])
	assert.Contains(t, eh.called(), "introspect_schema")

	require.NoError(t, p.in.Close()) // the client disconnects
	select {
	case err := <-p.done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the proxy did not end with stdin")
	}
}

func TestProxyStdio_ConcurrentRequestsDoNotBlockEachOther(t *testing.T) {
	eh := &echoHandler{block: make(chan struct{})}
	srv := startSSE(t, eh)
	p := startProxy(t, "http://"+srv.Addr()+"/sse", srv.Token())
	p.send(t, initReq)
	p.recv(t)
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	// A slow tool call (e.g. waiting on an approval card in Studio) must not
	// hold up a ping behind it.
	p.send(t, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"introspect_schema","arguments":{}}}`)
	p.send(t, `{"jsonrpc":"2.0","id":11,"method":"ping"}`)
	first := p.recv(t)
	assert.EqualValues(t, 11, first["id"])
	close(eh.block)
	assert.EqualValues(t, 10, p.recv(t)["id"])
}

func TestProxyStdio_WrongTokenIsRefused(t *testing.T) {
	srv := startSSE(t, &echoHandler{})
	inR, inW := io.Pipe()
	defer inW.Close() //nolint:errcheck
	var out strings.Builder
	err := ProxyStdio(t.Context(), ProxyConfig{Endpoint: "http://" + srv.Addr() + "/sse", Token: "wrong-token"}, inR, &out)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "wrong-token", "a token never appears in an error")
	assert.Empty(t, out.String(), "nothing but protocol bytes reaches stdout")
}

func TestProxyStdio_UpstreamDeathAnswersAndExits(t *testing.T) {
	eh := &echoHandler{}
	srv := startSSE(t, eh)
	p := startProxy(t, "http://"+srv.Addr()+"/sse", srv.Token())
	p.send(t, initReq)
	p.recv(t)
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	require.NoError(t, srv.Stop()) // Studio quits
	p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	got := make(chan map[string]any, 1)
	go func() {
		if p.out.Scan() {
			var m map[string]any
			_ = json.Unmarshal(p.out.Bytes(), &m)
			got <- m
		}
		close(got)
	}()
	select {
	case m, ok := <-got:
		if ok { // the stream may also just close, but when it speaks it is JSON-RPC
			assert.EqualValues(t, 2, m["id"])
			e, isErr := m["error"].(map[string]any)
			require.True(t, isErr, "an in-flight request is answered with a JSON-RPC error")
			assert.Contains(t, e["message"], "stopped serving")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no answer after the upstream died")
	}
	select {
	case err := <-p.done:
		assert.ErrorIs(t, err, ErrUpstreamLost)
	case <-time.After(15 * time.Second):
		t.Fatal("the proxy did not exit after the upstream died")
	}
}

func TestProxyStdio_IdleUpstreamDeathExits(t *testing.T) {
	srv := startSSE(t, &echoHandler{})
	p := startProxy(t, "http://"+srv.Addr()+"/sse", srv.Token())
	p.send(t, initReq)
	p.recv(t)
	require.NoError(t, srv.Stop())
	select {
	case err := <-p.done:
		assert.ErrorIs(t, err, ErrUpstreamLost)
	case <-time.After(15 * time.Second):
		t.Fatal("the proxy kept running with no upstream")
	}
}

// fakeUpstream is a hand-rolled SSE MCP endpoint, for the cases the real
// server never produces: server-initiated requests, failing POSTs, a 401
// mid-session.
type fakeUpstream struct {
	srv    *httptest.Server
	events chan string // raw "event: ...\ndata: ...\n\n" blocks to stream
	mu     sync.Mutex
	posts  []string
	getHdr http.Header
	onPost func(w http.ResponseWriter, body string) bool // true = handled
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{events: make(chan string, 16)}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.getHdr = r.Header.Clone()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "event: endpoint\ndata: /message?sessionId=1\n\n")
		fl.Flush()
		for {
			select {
			case ev := <-f.events:
				_, _ = io.WriteString(w, ev)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.posts = append(f.posts, string(b))
		h := f.onPost
		f.mu.Unlock()
		if h != nil && h(w, string(b)) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpstream) message(js string) { f.events <- "event: message\ndata: " + js + "\n\n" }
func (f *fakeUpstream) postBodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posts...)
}

func TestProxyStdio_ForwardsEverythingVerbatim(t *testing.T) {
	f := newFakeUpstream(t)
	p := startProxy(t, f.srv.URL+"/sse", "tok")

	// A server-initiated request reaches the client unchanged, and the
	// client's reply goes back upstream.
	f.message(`{"jsonrpc":"2.0","id":"srv-1","method":"sampling/createMessage","params":{"x":1}}`)
	got := p.recv(t)
	assert.Equal(t, "sampling/createMessage", got["method"])
	p.send(t, `{"jsonrpc":"2.0","id":"srv-1","result":{"ok":true}}`)
	// Notifications keep their params (cancellations name a request id).
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"user"}}`)
	waitUntil(t, func() bool { return len(f.postBodies()) == 2 })
	posts := f.postBodies()
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":"srv-1","result":{"ok":true}}`, posts[0])
	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"user"}}`, posts[1])

	f.mu.Lock()
	assert.Equal(t, "Bearer tok", f.getHdr.Get("Authorization"))
	assert.Empty(t, f.getHdr.Get("Origin"))
	assert.Empty(t, f.getHdr.Get(SandboxHeader), "no --sandbox, no header")
	f.mu.Unlock()
}

func TestProxyStdio_ARequestHasNoClientSideTimeout(t *testing.T) {
	// mcp-go's SSE client gave up on a request after a fixed 60s, which killed
	// the session while a person was still reading an approval card. The proxy
	// only POSTs (202) and then waits for the stream, however long.
	f := newFakeUpstream(t)
	p := startProxy(t, f.srv.URL+"/sse", "tok")
	p.send(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"run_query"}}`)
	waitUntil(t, func() bool { return len(f.postBodies()) == 1 })
	time.Sleep(1500 * time.Millisecond) // a long wait: still no error, no exit
	select {
	case err := <-p.done:
		t.Fatalf("the proxy gave up on a pending request: %v", err)
	default:
	}
	f.message(`{"jsonrpc":"2.0","id":5,"result":{"content":[]}}`)
	assert.EqualValues(t, 5, p.recv(t)["id"])
}

func TestProxyStdio_APostFailureAnswersOnlyThatRequest(t *testing.T) {
	f := newFakeUpstream(t)
	f.onPost = func(w http.ResponseWriter, body string) bool {
		if strings.Contains(body, `"id":1`) {
			http.Error(w, "boom", http.StatusInternalServerError)
			return true
		}
		return false
	}
	p := startProxy(t, f.srv.URL+"/sse", "tok")
	p.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	first := p.recv(t)
	assert.EqualValues(t, 1, first["id"])
	assert.NotNil(t, first["error"])
	// The session carries on.
	p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	f.message(`{"jsonrpc":"2.0","id":2,"result":{}}`)
	assert.EqualValues(t, 2, p.recv(t)["id"])
	select {
	case err := <-p.done:
		t.Fatalf("one failed POST ended the session: %v", err)
	default:
	}
}

func TestProxyStdio_UnauthorizedMidSessionIsUpstreamLost(t *testing.T) {
	f := newFakeUpstream(t)
	f.onPost = func(w http.ResponseWriter, _ string) bool { http.Error(w, "no", http.StatusUnauthorized); return true }
	p := startProxy(t, f.srv.URL+"/sse", "tok")
	p.send(t, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`)
	reply := p.recv(t)
	assert.EqualValues(t, 9, reply["id"])
	assert.NotNil(t, reply["error"])
	select {
	case err := <-p.done:
		assert.ErrorIs(t, err, ErrUpstreamLost)
	case <-time.After(10 * time.Second):
		t.Fatal("no exit after 401")
	}
}

func TestProxyStdio_ClientEOFDrainsInFlightReplies(t *testing.T) {
	f := newFakeUpstream(t)
	p := startProxy(t, f.srv.URL+"/sse", "tok")
	p.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	waitUntil(t, func() bool { return len(f.postBodies()) == 1 })
	require.NoError(t, p.in.Close())
	time.Sleep(200 * time.Millisecond)
	f.message(`{"jsonrpc":"2.0","id":3,"result":{}}`)
	assert.EqualValues(t, 3, p.recv(t)["id"], "a reply that arrives shortly after EOF is still delivered")
	select {
	case err := <-p.done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("no exit after EOF")
	}
}

func TestProxyStdio_SendsSandboxHeaderOnTheStream(t *testing.T) {
	f := newFakeUpstream(t)
	inR, inW := io.Pipe()
	defer inW.Close() //nolint:errcheck
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- ProxyStdio(ctx, ProxyConfig{Endpoint: f.srv.URL + "/sse", Token: "tok", Sandbox: "read-only"}, inR, io.Discard)
	}()
	waitUntil(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.getHdr != nil })
	f.mu.Lock()
	assert.Equal(t, "read-only", f.getHdr.Get(SandboxHeader))
	f.mu.Unlock()
	cancel()
	<-done
}

func TestServer_RejectsUnknownSandboxHeader(t *testing.T) {
	srv := startSSE(t, &echoHandler{})
	req, _ := http.NewRequest(http.MethodGet, "http://"+srv.Addr()+"/sse", nil)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	req.Header.Set(SandboxHeader, "godmode")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Without the token the answer stays 401: validation never leaks past auth.
	req2, _ := http.NewRequest(http.MethodGet, "http://"+srv.Addr()+"/sse", nil)
	req2.Header.Set(SandboxHeader, "godmode")
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close() //nolint:errcheck
	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProxyStdio_MultiLineDataEventsKeepStdoutFraming(t *testing.T) {
	f := newFakeUpstream(t)
	p := startProxy(t, f.srv.URL+"/sse", "tok")
	// A message split over two data: lines is one message, on one line.
	f.events <- "event: message\ndata: {\"jsonrpc\":\"2.0\",\ndata: \"id\":1,\"result\":{}}\n\n"
	// A non-JSON event is dropped, not forwarded as a broken frame.
	f.events <- "event: message\ndata: not json at all\n\n"
	f.message(`{"jsonrpc":"2.0","id":2,"result":{}}`)
	first := p.recv(t)
	assert.EqualValues(t, 1, first["id"])
	assert.NotContains(t, p.out.Text(), "\n")
	assert.EqualValues(t, 2, p.recv(t)["id"], "the bad event was skipped, the stream carries on")
}

func TestProxyStdio_MalformedAndOversizeLinesGetAParseError(t *testing.T) {
	f := newFakeUpstream(t)
	p := startProxy(t, f.srv.URL+"/sse", "tok")

	p.send(t, `{this is not json`)
	e := p.recv(t)
	assert.Nil(t, e["id"])
	errObj, _ := e["error"].(map[string]any)
	assert.EqualValues(t, -32700, errObj["code"])

	// A line over 16 MB is skipped with a parse error, not the end of the session.
	big := strings.Repeat("x", maxLineBytes+1024)
	go func() {
		_, _ = io.WriteString(p.in, `{"jsonrpc":"2.0","id":1,"method":"m","params":{"p":"`+big+`"}}`+"\n")
	}()
	e = p.recv(t)
	errObj, _ = e["error"].(map[string]any)
	assert.EqualValues(t, -32700, errObj["code"])
	assert.Empty(t, f.postBodies(), "neither bad line was forwarded")

	p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	f.message(`{"jsonrpc":"2.0","id":2,"result":{}}`)
	assert.EqualValues(t, 2, p.recv(t)["id"])
}

func TestProxyStdio_RequestIdsMatchAfterNormalisation(t *testing.T) {
	f := newFakeUpstream(t)
	p := startProxy(t, f.srv.URL+"/sse", "tok")
	p.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	waitUntil(t, func() bool { return len(f.postBodies()) == 1 })
	require.NoError(t, p.in.Close())
	f.message(`{"jsonrpc":"2.0","id":1.0,"result":{}}`) // same id, spelled differently
	p.recv(t)
	select {
	case <-p.done: // the pending entry matched, so the drain ended at once
	case <-time.After(1 * time.Second):
		t.Fatal("the reply did not clear the pending request: the drain waited its full timeout")
	}
}

func TestIsConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	_, err = net.Dial("tcp", addr)
	require.Error(t, err)
	assert.True(t, isConnRefused(err), "a real refused dial: %v", err)
	assert.True(t, isConnRefused(&url.Error{Op: "Post", Err: err}), "wrapped as net/http returns it")

	assert.False(t, isConnRefused(nil))
	assert.False(t, isConnRefused(errors.New("connection refused")), "message text is not the signal")
	assert.False(t, isConnRefused(context.DeadlineExceeded))
}
