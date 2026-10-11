package mcp

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSecurityTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sandbox := &agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := agents.NewHandler(&mockAccess{db: db}, &mockEngine{}, sandbox, logger)

	srv := New(handler, logger)
	require.NoError(t, srv.Start("127.0.0.1:0"))
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

// TestSSE_RequestWithoutToken_Unauthorized (#64/#70 review): a bare request
// with no Authorization header or ?token= must be rejected outright, since
// the SSE server otherwise listens on localhost with no auth at all.
func TestSSE_RequestWithoutToken_Unauthorized(t *testing.T) {
	srv := newSecurityTestServer(t)
	resp, err := http.Get(fmt.Sprintf("http://%s/sse", srv.Addr()))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestSSE_RequestWithBrowserOrigin_Forbidden: any request carrying an
// Origin header (the signature of a browser fetch/XHR/EventSource call,
// which a legitimate MCP client never sets) must be rejected even with a
// valid token, since the point is to stop a malicious page loaded in the
// user's own browser from reaching the server at all (CSRF/DNS rebinding).
func TestSSE_RequestWithBrowserOrigin_Forbidden(t *testing.T) {
	srv := newSecurityTestServer(t)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/sse", srv.Addr()), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	req.Header.Set("Origin", "http://evil.example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestSSE_RequestWithLocalhostOrigin_StillForbidden: even a same-origin
// looking Origin (http://localhost:...) is rejected — legitimate MCP
// clients never set Origin at all, so its mere presence is the signal, not
// its value (a page can set whatever Origin it wants... no, actually a
// browser sets Origin truthfully, but a value the server can't verify
// belongs to a trusted app is not worth trying to allowlist).
func TestSSE_RequestWithLocalhostOrigin_StillForbidden(t *testing.T) {
	srv := newSecurityTestServer(t)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/sse", srv.Addr()), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	req.Header.Set("Origin", "http://localhost:5173")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestSSE_ValidTokenNoOrigin_Works confirms a well-formed request (no
// Origin, valid bearer token, loopback Host) is let through to the SSE
// handler — proven by getting an SSE stream open (200 with the right
// content type) rather than a 401/403.
func TestSSE_ValidTokenNoOrigin_Works(t *testing.T) {
	srv := newSecurityTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/sse", srv.Addr()), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+srv.Token())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestSSE_WrongToken_Unauthorized guards against an off-by-one where any
// non-empty Authorization header would pass.
func TestSSE_WrongToken_Unauthorized(t *testing.T) {
	srv := newSecurityTestServer(t)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/sse", srv.Addr()), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer not-the-real-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestSSE_QueryToken_GetOnly is the review fix for finding 9: `?token=` is
// accepted on a GET (the initial SSE connection, for a client like a raw
// EventSource that can't set headers) but rejected on a POST (a tool call),
// which must always use the Authorization header.
func TestSSE_QueryToken_GetOnly(t *testing.T) {
	srv := newSecurityTestServer(t)

	getReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/sse?token=%s", srv.Addr(), srv.Token()), nil)
	require.NoError(t, err)
	getResp, err := http.DefaultClient.Do(getReq)
	require.NoError(t, err)
	defer getResp.Body.Close()
	assert.Equal(t, http.StatusOK, getResp.StatusCode, "a GET with ?token= must be accepted")

	postReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/message?token=%s", srv.Addr(), srv.Token()), strings.NewReader("{}"))
	require.NoError(t, err)
	postResp, err := http.DefaultClient.Do(postReq)
	require.NoError(t, err)
	defer postResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, postResp.StatusCode, "a POST must never accept ?token= — only the Authorization header")
}

// TestStop_ReturnsPromptlyWithClientConnected is the review fix (round 2)
// for the SSE shutdown hang: a GET /sse connection is, by design, long-held
// and never returns on its own — Stop() must still return within
// stopTimeout-ish bounds instead of blocking on http.Server.Shutdown
// forever waiting for that handler to finish naturally.
func TestStop_ReturnsPromptlyWithClientConnected(t *testing.T) {
	srv := newSecurityTestServer(t)

	// Open a GET /sse connection and start reading its body in the
	// background, exactly like a real long-lived client — and leave it
	// open (never cancelled by the test) so Stop() has to actively
	// terminate it rather than the client hanging up first.
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/sse", srv.Addr()), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	go func() { _, _ = io.Copy(io.Discard, resp.Body) }()

	// Give the server a moment to register the session.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	err = srv.Stop()
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Lessf(t, elapsed, 3*time.Second, "Stop() took %s with a client still connected — it must never hang on an open SSE connection", elapsed)
}

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8080":        true,
		"127.0.0.1":             true,
		"localhost:8080":        true,
		"localhost":             true,
		"[::1]:8080":            true,
		"evil.example.com:8080": false,
		"0.0.0.0:8080":          false,
		"":                      false,
	}
	for host, want := range cases {
		assert.Equal(t, want, isLoopbackHost(host), "host=%q", host)
	}
}

// TestServeStdio_JSONOnlyOnStdout drives the stdio transport end to end: an
// initialize + tools/list request written to a pipe standing in for stdin
// must produce ONLY newline-delimited JSON-RPC on the output writer, and
// tools/list must never include a task-only tool like write_cell (ModeMCP
// filtering — #64's "assert write_cell/preview_query absent" QA step,
// proven here at the unit level rather than only live).
func TestServeStdio_JSONOnlyOnStdout(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	sandbox := &agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := agents.NewHandler(&mockAccess{db: db}, &mockEngine{}, sandbox, logger)
	srv := New(handler, logger)

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n",
	)
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.ServeStdio(ctx, in, &out)
	assert.NoError(t, err)

	scanner := bufio.NewScanner(&out)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	sawToolsList := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		assert.Truef(t, strings.HasPrefix(strings.TrimSpace(line), "{"), "stdout line is not JSON: %q", line)
		if strings.Contains(line, `"tools":`) {
			sawToolsList = true
			assert.NotContains(t, line, `"write_cell"`, "write_cell is task-only and must not appear in the MCP tool list")
			assert.NotContains(t, line, `"preview_query"`, "preview_query is task-only and must not appear in the MCP tool list")
		}
	}
	assert.True(t, sawToolsList, "expected a tools/list response on stdout")
}
