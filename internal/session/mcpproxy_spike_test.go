package session_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubHandler struct{ calls []string }

func (h *stubHandler) Handle(_ context.Context, req *agents.Request) *agents.Response {
	h.calls = append(h.calls, req.Method)
	return &agents.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"echo": req.Method}}
}

// stdioToSSE is the whole proxy: raw JSON-RPC lines on in/out, forwarded over
// mcp-go's SSE client transport with a bearer header. ~25 lines, no new deps.
func stdioToSSE(ctx context.Context, baseURL, token string, in io.Reader, out io.Writer) error {
	up, err := transport.NewSSE(baseURL, transport.WithHeaders(map[string]string{"Authorization": "Bearer " + token}))
	if err != nil {
		return err
	}
	if err := up.Start(ctx); err != nil {
		return err
	}
	defer up.Close()
	enc := json.NewEncoder(out)
	// Server -> client notifications (progress, list_changed) flow back to stdout.
	up.SetNotificationHandler(func(n mcp.JSONRPCNotification) { _ = enc.Encode(n) })
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var msg struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Params json.RawMessage  `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		if msg.ID == nil { // notification
			var params any
			_ = json.Unmarshal(msg.Params, &params)
			_ = up.SendNotification(ctx, mcp.JSONRPCNotification{JSONRPC: "2.0", Notification: mcp.Notification{Method: msg.Method}})
			continue
		}
		var id mcp.RequestId
		_ = json.Unmarshal(*msg.ID, &id)
		var params any
		_ = json.Unmarshal(msg.Params, &params)
		resp, err := up.SendRequest(ctx, transport.JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: msg.Method, Params: params})
		if err != nil {
			return err
		}
		_ = enc.Encode(resp)
	}
	return sc.Err()
}

// Spike #107: can `vibe mcp serve` proxy stdio to Studio's SSE server using
// only mcp-go primitives, including the bearer token and Origin rules?
func TestSpikeMCPStdioToSSEProxy(t *testing.T) {
	h := &stubHandler{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := vibemcp.New(h, logger)
	require.NoError(t, srv.Start("127.0.0.1:0"))
	t.Cleanup(func() { _ = srv.Stop() })
	base := "http://" + srv.Addr() + "/sse"

	t.Run("wrong token is rejected", func(t *testing.T) {
		up, err := transport.NewSSE(base, transport.WithHeaders(map[string]string{"Authorization": "Bearer nope"}))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		err = up.Start(ctx)
		t.Logf("Start with wrong token: %v", err)
		if err == nil {
			defer up.Close()
			_, err = up.SendRequest(ctx, transport.JSONRPCRequest{JSONRPC: "2.0", ID: mcp.NewRequestId(int64(1)), Method: "ping"})
		}
		assert.Error(t, err)
	})

	t.Run("proxy round trip", func(t *testing.T) {
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- stdioToSSE(ctx, base, srv.Token(), inR, outW) }()
		out := bufio.NewScanner(outR)
		out.Buffer(make([]byte, 1<<20), 16<<20)
		send := func(s string) { _, _ = io.WriteString(inW, s+"\n") }
		recv := func() map[string]any {
			require.True(t, out.Scan())
			var m map[string]any
			require.NoError(t, json.Unmarshal(out.Bytes(), &m))
			return m
		}
		send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"proxy-spike","version":"0"}}}`)
		init := recv()
		initRes, ok := init["result"].(map[string]any)
		require.True(t, ok)
		t.Logf("initialize -> serverInfo=%v", initRes["serverInfo"])
		send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		listRes, ok := recv()["result"].(map[string]any)
		require.True(t, ok)
		tools, ok := listRes["tools"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, tools)
		t.Logf("tools/list through proxy: %d tools", len(tools))
		first, ok := tools[0].(map[string]any)
		require.True(t, ok)
		name, ok := first["name"].(string)
		require.True(t, ok)
		send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`)
		res := recv()
		assert.Contains(t, h.calls, name, "call reached the Studio-side handler")
		t.Logf("tools/call %s -> %v", name, res["result"])
		_ = inW.Close()
		select {
		case err := <-done:
			t.Logf("proxy exit on stdin EOF: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("proxy did not exit on stdin EOF")
		}
	})

	t.Run("upstream death surfaces as an error, not a hang", func(t *testing.T) {
		h2 := &stubHandler{}
		srv2 := vibemcp.New(h2, logger)
		require.NoError(t, srv2.Start("127.0.0.1:0"))
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		done := make(chan error, 1)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		go func() { done <- stdioToSSE(ctx, "http://"+srv2.Addr()+"/sse", srv2.Token(), inR, outW) }()
		go func() { _, _ = io.Copy(io.Discard, outR) }()
		_, _ = io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"x","version":"0"}}}`+"\n")
		time.Sleep(300 * time.Millisecond)
		require.NoError(t, srv2.Stop()) // Studio quits
		_, _ = io.WriteString(inW, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
		select {
		case err := <-done:
			t.Logf("proxy after Studio exit: %v", err)
			assert.Error(t, err)
		case <-time.After(15 * time.Second):
			t.Logf("proxy still blocked 15s after Studio exit (needs its own request timeout)")
		}
	})

	t.Run("Origin header is refused even with a valid token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, base, nil)
		req.Header.Set("Authorization", "Bearer "+srv.Token())
		req.Header.Set("Origin", "http://evil.example")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}
