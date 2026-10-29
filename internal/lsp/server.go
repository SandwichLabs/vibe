// Package lsp is `vibe lsp`: a Language Server Protocol server over stdio that
// completes and describes a workspace's connections, schemas, tables, views
// and columns in any editor with an LSP client (Neovim, Helix, VS Code, Zed,
// Emacs).
//
// The server opens no database and writes no file. DuckDB lets one process
// write a .duckdb file or many read it, never both, and an editor's language
// server runs for hours: holding even a read-only attach would stop the TUI or
// Studio from opening the workspace. It reads a catalog snapshot,
// .vibe-local/catalog.json, that the TUI writes (on start and after each
// statement) or `vibe lsp snapshot` writes on demand, and reloads it when it
// changes. Until one exists, completion is empty.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

// Server is one LSP session on a reader and a writer (the editor's pipes).
type Server struct {
	in     *bufio.Reader
	out    io.Writer
	outMu  sync.Mutex
	logger *slog.Logger

	version string
	src     *Source
	// onRoot picks the snapshot source once the client names its root folder.
	onRoot func(clientRoot string) *Source

	docs     map[string]string
	utf8     bool // positions count UTF-8 bytes (negotiated), else UTF-16 units
	shutdown bool
}

// Option configures NewServer.
type Option func(*Server)

// WithSource fixes the catalog snapshot the server reads.
func WithSource(src *Source) Option { return func(s *Server) { s.src = src } }

// WithRootHook is called on initialize with the client's root folder ("" when
// it sent none) and returns the snapshot source to use, unless WithSource set one.
func WithRootHook(fn func(clientRoot string) *Source) Option {
	return func(s *Server) { s.onRoot = fn }
}

// WithLogger logs protocol trouble (to stderr: stdout is the protocol).
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.logger = l } }

// WithVersion is reported as serverInfo.version.
func WithVersion(v string) Option { return func(s *Server) { s.version = v } }

// NewServer builds a server on r and w.
func NewServer(r io.Reader, w io.Writer, opts ...Option) *Server {
	s := &Server{in: bufio.NewReader(r), out: w, logger: slog.Default(), docs: map[string]string{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// errExit ends Run after the exit notification.
var errExit = errors.New("exit")

// Run serves until the client sends exit, closes the input, or ctx ends.
func (s *Server) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		body, err := readMessage(s.in)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if errors.Is(err, errBadFrame) || errors.Is(err, errTooLarge) {
			s.logger.Warn("lsp: skipping a frame", "error", err)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to read an LSP message: %w", err)
		}
		if err := s.safeHandle(body); errors.Is(err, errExit) {
			return nil
		} else if err != nil {
			s.logger.Warn("lsp: message failed", "error", err)
		}
	}
}

// request is a JSON-RPC request or notification (no ID).
type request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInternalError  = -32603
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInvalidRequest = -32600
)

// safeHandle is handle with a recover: a bug in the completer must answer the
// request with an internal error, not take down the editor's server.
func (s *Server) safeHandle(body []byte) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		s.logger.Error("lsp: handler panicked", "panic", r)
		var req request
		if json.Unmarshal(body, &req) == nil && len(req.ID) > 0 && string(req.ID) != "null" {
			err = s.reply(req.ID, nil, &rpcError{Code: codeInternalError, Message: "internal error"})
			return
		}
		err = fmt.Errorf("handler panicked: %v", r)
	}()
	return s.handle(body)
}

func (s *Server) handle(body []byte) error {
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		if rerr := s.reply(json.RawMessage("null"), nil, &rpcError{Code: codeParseError, Message: "parse error"}); rerr != nil {
			return rerr
		}
		return fmt.Errorf("bad JSON-RPC message: %w", err)
	}
	isRequest := len(req.ID) > 0 && string(req.ID) != "null"
	if req.Method == "exit" {
		return errExit
	}
	if s.shutdown && isRequest {
		return s.reply(req.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "server is shut down"})
	}
	result, rerr := s.dispatch(req)
	if !isRequest {
		return nil
	}
	return s.reply(req.ID, result, rerr)
}

func (s *Server) dispatch(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params)
	case "shutdown":
		s.shutdown = true
		return nil, nil
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		if json.Unmarshal(req.Params, &p) == nil {
			s.docs[p.TextDocument.URI] = p.TextDocument.Text
		}
		return nil, nil
	case "textDocument/didChange":
		s.didChange(req.Params)
		return nil, nil
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if json.Unmarshal(req.Params, &p) == nil {
			delete(s.docs, p.TextDocument.URI)
		}
		return nil, nil
	case "textDocument/completion":
		return s.completion(req.Params)
	case "textDocument/hover":
		return s.hover(req.Params)
	}
	if strings.HasPrefix(req.Method, "$/") || req.Method == "initialized" {
		return nil, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not supported: " + req.Method}
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	var p struct {
		RootURI      string `json:"rootUri"`
		RootPath     string `json:"rootPath"`
		Capabilities struct {
			General struct {
				PositionEncodings []string `json:"positionEncodings"`
			} `json:"general"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	for _, e := range p.Capabilities.General.PositionEncodings {
		if e == "utf-8" {
			s.utf8 = true
		}
	}
	if s.src == nil && s.onRoot != nil {
		root := p.RootPath
		if p.RootURI != "" {
			root = uriPath(p.RootURI)
		}
		s.src = s.onRoot(root)
	}
	enc := "utf-16"
	if s.utf8 {
		enc = "utf-8"
	}
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding": enc,
			"textDocumentSync": map[string]any{"openClose": true, "change": 1},
			"completionProvider": map[string]any{
				"triggerCharacters": []string{"."},
			},
			"hoverProvider": true,
		},
		"serverInfo": map[string]any{"name": "vibe", "version": s.version},
	}, nil
}

func (s *Server) didChange(params json.RawMessage) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.ContentChanges) == 0 {
		return
	}
	// Full sync (change: 1): the last change is the whole document.
	s.docs[p.TextDocument.URI] = p.ContentChanges[len(p.ContentChanges)-1].Text
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type positionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position position `json:"position"`
}

// at decodes a position request into the document text and a byte offset.
func (s *Server) at(params json.RawMessage) (string, int, *rpcError) {
	var p positionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return "", 0, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	text := s.docs[p.TextDocument.URI]
	return text, byteOffset(text, p.Position, s.utf8), nil
}

func (s *Server) completion(params json.RawMessage) (any, *rpcError) {
	text, off, rerr := s.at(params)
	if rerr != nil {
		return nil, rerr
	}
	items, _, incomplete := completeAt(text, off, s.src.Catalog())
	out := make([]map[string]any, 0, len(items))
	for i, it := range items {
		ci := map[string]any{
			"label":    it.Label,
			"kind":     lspKind(it.Kind),
			"sortText": fmt.Sprintf("%d%05d", it.Kind.Rank(), i),
		}
		if it.Detail != "" {
			ci["detail"] = it.Detail
		}
		if it.Insert != "" {
			ci["insertText"] = it.Insert
		}
		out = append(out, ci)
	}
	return map[string]any{"isIncomplete": incomplete, "items": out}, nil
}

func (s *Server) hover(params json.RawMessage) (any, *rpcError) {
	text, off, rerr := s.at(params)
	if rerr != nil {
		return nil, rerr
	}
	md := Hover(text, off, s.src.Catalog())
	if md == "" {
		return nil, nil
	}
	return map[string]any{"contents": map[string]any{"kind": "markdown", "value": md}}, nil
}

// lspKind maps an item kind to LSP's CompletionItemKind.
func lspKind(k ItemKind) int {
	switch k {
	case KindConnection:
		return 19 // Folder
	case KindSchema:
		return 9 // Module
	case KindTable:
		return 22 // Struct
	case KindView:
		return 8 // Interface
	case KindColumn:
		return 5 // Field
	case KindFunction:
		return 3 // Function
	case KindAlias:
		return 6 // Variable
	case KindMetric:
		return 12 // Value
	default:
		return 14 // Keyword
	}
}

func (s *Server) reply(id json.RawMessage, result any, rerr *rpcError) error {
	b, err := json.Marshal(response{JSONRPC: "2.0", ID: id, Result: result, Error: rerr})
	if err != nil {
		return fmt.Errorf("failed to encode a response: %w", err)
	}
	return writeMessage(s.out, &s.outMu, b)
}

// maxMessage bounds one message body. A client names the length, so without a
// cap a bogus header could reserve all of memory.
const maxMessage = 32 << 20

// errBadFrame is a frame without a usable Content-Length: skippable, since its
// header block is consumed and the next frame starts clean.
var errBadFrame = errors.New("bad frame")

// errTooLarge is a message over maxMessage. Its body has been drained (not
// parsed, so it has no id to answer), and the next frame starts clean.
var errTooLarge = errors.New("message too large")

// readMessage reads one Content-Length framed message. A body over maxMessage
// is drained and refused with errTooLarge, so the server keeps serving.
func readMessage(r *bufio.Reader) ([]byte, error) {
	hdr, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(hdr.Get("Content-Length"))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%w: Content-Length %q", errBadFrame, hdr.Get("Content-Length"))
	}
	if n > maxMessage {
		if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %d bytes is over the %d byte limit", errTooLarge, n, maxMessage)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeMessage(w io.Writer, mu *sync.Mutex, body []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// byteOffset converts an LSP position to a byte offset in text, counting
// characters in UTF-8 bytes or UTF-16 units. Past the end clamps to the end.
func byteOffset(text string, pos position, isUTF8 bool) int {
	off := 0
	for line := 0; line < pos.Line; line++ {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	if isUTF8 {
		end := off + pos.Character
		if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 && end > off+nl {
			end = off + nl
		}
		return min(end, len(text))
	}
	line := text[off:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	return off + utf16ToByte(line, pos.Character)
}

// utf16ToByte converts a count of UTF-16 code units into a byte offset in
// text, clamping to the end.
func utf16ToByte(text string, units int) int {
	off, n := 0, 0
	for off < len(text) && n < units {
		r, size := utf8.DecodeRuneInString(text[off:])
		n += utf16.RuneLen(r)
		off += size
	}
	return off
}

// byteToUTF16 converts a byte offset in text into UTF-16 code units, clamping
// to the end.
func byteToUTF16(text string, b int) int {
	b = min(max(b, 0), len(text))
	n := 0
	for off := 0; off < b; {
		r, size := utf8.DecodeRuneInString(text[off:])
		n += utf16.RuneLen(r)
		off += size
	}
	return n
}

// uriPath turns a file:// URI into a path ("" for any other scheme).
func uriPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}
