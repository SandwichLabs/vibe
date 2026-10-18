package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// client drives a Server over pipes the way an editor does.
type client struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Reader
	id  int
}

func startServer(t *testing.T, opts ...Option) *client {
	t.Helper()
	cr, sw := io.Pipe() // server -> client
	sr, cw := io.Pipe() // client -> server
	done := make(chan error, 1)
	go func() { done <- NewServer(sr, sw, opts...).Run(context.Background()) }()
	t.Cleanup(func() {
		_ = cw.Close()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return &client{t: t, in: cw, out: bufio.NewReader(cr)}
}

func (c *client) send(method string, params any, withID bool) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if withID {
		c.id++
		msg["id"] = c.id
	}
	b, err := json.Marshal(msg)
	require.NoError(c.t, err)
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	require.NoError(c.t, err)
}

func (c *client) call(method string, params any) json.RawMessage {
	c.send(method, params, true)
	body, err := readMessage(c.out)
	require.NoError(c.t, err)
	var resp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	require.NoError(c.t, json.Unmarshal(body, &resp))
	require.Equal(c.t, c.id, resp.ID)
	require.Nil(c.t, resp.Error)
	return resp.Result
}

func TestServer_CompletionAndHoverRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, fixture().Save(path))
	var gotRoot string
	c := startServer(t, WithRootHook(func(root string) *Source {
		gotRoot = root
		return NewSource(path)
	}))

	var init struct {
		Capabilities struct {
			PositionEncoding   string `json:"positionEncoding"`
			CompletionProvider struct {
				TriggerCharacters []string `json:"triggerCharacters"`
			} `json:"completionProvider"`
			HoverProvider bool `json:"hoverProvider"`
		} `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(c.call("initialize", map[string]any{
		"rootUri":      "file:///work/sales",
		"capabilities": map[string]any{},
	}), &init))
	assert.Equal(t, "/work/sales", gotRoot)
	assert.Equal(t, "utf-16", init.Capabilities.PositionEncoding)
	assert.Equal(t, []string{"."}, init.Capabilities.CompletionProvider.TriggerCharacters)
	assert.True(t, init.Capabilities.HoverProvider)
	c.send("initialized", map[string]any{}, false)

	uri := "file:///work/sales/queries/q.sql"
	c.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "sql", "version": 1, "text": "SELECT 1",
	}}, false)
	// "é" is one UTF-16 unit and two UTF-8 bytes: positions must convert.
	text := "-- café\nSELECT o. FROM orders o"
	c.send("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": text}},
	}, false)

	var list struct {
		Items []struct {
			Label string `json:"label"`
			Kind  int    `json:"kind"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 9},
	}), &list))
	require.Len(t, list.Items, 3)
	assert.Equal(t, "id", list.Items[0].Label)
	assert.Equal(t, 5, list.Items[0].Kind, "columns are LSP Fields")

	var hover struct {
		Contents struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(c.call("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 20},
	}), &hover))
	assert.Equal(t, "markdown", hover.Contents.Kind)
	assert.Contains(t, hover.Contents.Value, "local.main.orders")

	assert.Equal(t, "null", string(c.call("shutdown", nil)))
	c.send("exit", nil, false)
}

func TestServer_UnknownMethodIsAnError(t *testing.T) {
	c := startServer(t)
	c.call("initialize", map[string]any{"capabilities": map[string]any{}})
	c.send("textDocument/definition", map[string]any{}, true)
	body, err := readMessage(c.out)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"code":-32601`)
}

func TestByteOffset(t *testing.T) {
	text := "ab\n😀x\n"
	assert.Equal(t, 1, byteOffset(text, position{0, 1}, false))
	assert.Equal(t, 3+4, byteOffset(text, position{1, 2}, false), "an emoji is two UTF-16 units")
	assert.Equal(t, 3+4, byteOffset(text, position{1, 4}, true), "and four UTF-8 bytes")
	assert.Equal(t, 2, byteOffset(text, position{0, 99}, false), "clamped to the line")
	assert.Equal(t, len(text), byteOffset(text, position{9, 0}, false))
}

func TestReadMessage_CapsTheBody(t *testing.T) {
	_, err := readMessage(bufio.NewReader(strings.NewReader("Content-Length: 9223372036854775807\r\n\r\n")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
	_, err = readMessage(bufio.NewReader(strings.NewReader("Content-Length: 33554433\r\n\r\n")))
	require.Error(t, err)
	body, err := readMessage(bufio.NewReader(strings.NewReader("Content-Length: 2\r\n\r\n{}")))
	require.NoError(t, err)
	assert.Equal(t, "{}", string(body))
}

func frame(s string) string { return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(s), s) }

func TestServer_PanicInAHandlerIsAnInternalError(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(strings.NewReader(
		frame(`{"jsonrpc":"2.0","id":1,"method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///a.sql","text":"x"}}}`)+
			frame(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)), &out)
	srv.docs = nil // didOpen now writes to a nil map: a panic
	require.NoError(t, srv.Run(context.Background()))
	assert.Contains(t, out.String(), `"id":1`)
	assert.Contains(t, out.String(), "-32603")
	assert.Contains(t, out.String(), `"id":2`, "the server kept serving after the panic")
}

func TestServer_BadJSONAndMissingLengthAreSurvivable(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(strings.NewReader(
		"X-Other: 1\r\n\r\n"+frame(`{not json`)+frame(`{"jsonrpc":"2.0","id":7,"method":"shutdown"}`)), &out)
	require.NoError(t, srv.Run(context.Background()))
	assert.Contains(t, out.String(), "-32700")
	assert.Contains(t, out.String(), `"id":7`)
}
