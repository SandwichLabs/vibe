package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrUpstreamLost is returned by ProxyStdio when the server it was bridging to
// went away (Studio quit, MCP turned off, the token stopped working). Callers
// exit non-zero on it.
var ErrUpstreamLost = errors.New("the Vibe workspace owner stopped serving MCP")

// jsonRPCUpstreamLost is the JSON-RPC error code ProxyStdio answers requests
// with once the upstream is gone (-32000..-32099 is the implementation range).
const jsonRPCUpstreamLost = -32001

// ProxyConfig configures ProxyStdio.
type ProxyConfig struct {
	// Endpoint is the owner's SSE URL; Token its bearer token. The token is
	// sent only as an Authorization header, never in a URL, log or error.
	Endpoint string
	Token    string
	// Sandbox, when set, is sent as SandboxHeader on the stream so the owner
	// caps this session at that mode (tighten-only).
	Sandbox string
	// DrainTimeout bounds how long, after the client's stdin closes, the proxy
	// waits for replies to requests still in flight (default 2s).
	DrainTimeout time.Duration
	Logger       *slog.Logger
}

// ProxyStdio bridges a stdio MCP client (in/out: one JSON-RPC message per
// line) to a Vibe MCP server over SSE. It is a transparent byte pipe, not an
// MCP client: every `message` event on the SSE stream is copied verbatim to
// out (responses, notifications and server-to-client requests alike), and every
// line from in is POSTed verbatim to the server's message endpoint (requests,
// notifications and the client's replies to server requests). It therefore has
// no request timeout of its own: a tool call may wait minutes for a person to
// approve it in Studio, and the proxy does not give up on it.
//
// Only the stream ending, a 401, or a refused connection counts as the
// upstream being lost. A POST that fails for another reason answers just that
// request with a JSON-RPC error and carries on.
//
// out carries protocol bytes and nothing else: diagnostics are the caller's
// business (stderr).
//
// It returns nil when in reaches EOF (after a short drain) or ctx is
// cancelled. When the upstream is lost it answers every request still in
// flight with a JSON-RPC error and returns ErrUpstreamLost.
func ProxyStdio(ctx context.Context, cfg ProxyConfig, in io.Reader, out io.Writer) error {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 2 * time.Second
	}
	base, err := url.Parse(cfg.Endpoint)
	if err != nil || base.Host == "" {
		return errors.New("invalid MCP endpoint")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	p := &proxy{cfg: cfg, base: base, out: out, logger: logger, pending: map[string]json.RawMessage{}, client: &http.Client{}}
	streamDone, err := p.openStream(ctx)
	if err != nil {
		if errors.Is(err, errProxyCancelled) {
			return nil
		}
		return err
	}
	lines, scanErr := scanLines(ctx, in)
	return p.serve(ctx, cancel, lines, scanErr, streamDone)
}

// errProxyCancelled marks a context cancelled while the proxy was still
// connecting: ProxyStdio returns nil for it.
var errProxyCancelled = errors.New("cancelled")

// openStream opens the SSE stream, waits for the server to name its message
// endpoint (stored in p.postURL), and returns a channel closed when the stream
// ends.
func (p *proxy) openStream(ctx context.Context) (<-chan struct{}, error) {
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.Endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid MCP endpoint")
	}
	p.authorize(streamReq)
	streamReq.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(streamReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the workspace's MCP server: %s", scrub(err, p.cfg.Token))
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("the workspace's MCP server refused the connection (HTTP %d)", resp.StatusCode)
	}

	endpointCh := make(chan string, 1)
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		defer resp.Body.Close() //nolint:errcheck
		p.readStream(resp.Body, endpointCh)
	}()
	var postURL string
	select {
	case postURL = <-endpointCh:
	case <-streamDone:
		return nil, fmt.Errorf("the workspace's MCP server closed the connection before it was ready: %w", ErrUpstreamLost)
	case <-time.After(10 * time.Second):
		return nil, errors.New("the workspace's MCP server did not answer in time")
	case <-ctx.Done():
		return nil, errProxyCancelled
	}
	target, err := p.base.Parse(postURL)
	if err != nil || target.Host != p.base.Host {
		return nil, errors.New("the workspace's MCP server named an unexpected message endpoint")
	}
	p.postURL = target.String()
	return streamDone, nil
}

// scanLines reads in line by line on a goroutine. A line over maxLineBytes is
// reported with tooLong set instead of its bytes. lines is closed at the end
// of in; scanErr then carries the read error (nil for a clean EOF).
func scanLines(ctx context.Context, in io.Reader) (<-chan inLine, <-chan error) {
	lines := make(chan inLine)
	scanErr := make(chan error, 1)
	go func() {
		br := bufio.NewReaderSize(in, 64<<10)
		var buf []byte
		tooLong := false
		for {
			frag, isPrefix, err := br.ReadLine()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					scanErr <- err
				} else {
					scanErr <- nil
				}
				close(lines)
				return
			}
			if !tooLong {
				if len(buf)+len(frag) > maxLineBytes {
					tooLong, buf = true, nil // discard the rest of this line
				} else {
					buf = append(buf, frag...)
				}
			}
			if isPrefix {
				continue
			}
			msg := inLine{data: bytes.TrimSpace(buf), tooLong: tooLong}
			buf, tooLong = nil, false
			if len(msg.data) == 0 && !msg.tooLong {
				continue
			}
			select {
			case lines <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return lines, scanErr
}

const upstreamLostMessage = "Vibe stopped serving this workspace over MCP (Studio quit or turned MCP off). Reconnect to continue."

// serve forwards the client's lines until stdin ends, ctx is cancelled or the
// upstream is lost.
func (p *proxy) serve(ctx context.Context, cancel context.CancelFunc, lines <-chan inLine, scanErr <-chan error, streamDone <-chan struct{}) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-streamDone:
			if ctx.Err() != nil {
				return nil
			}
			// The upstream went away. Answer what is still waiting.
			p.failPending(upstreamLostMessage)
			return ErrUpstreamLost
		case line, ok := <-lines:
			if !ok {
				// The client went away: give in-flight replies a moment.
				p.drain(ctx, streamDone, p.cfg.DrainTimeout)
				cancel()
				<-streamDone
				return <-scanErr
			}
			if p.forwardLine(ctx, line) {
				p.failPending(upstreamLostMessage)
				cancel()
				return ErrUpstreamLost
			}
		}
	}
}

// forwardLine sends one client line upstream, answering a bad line itself. It
// reports whether the upstream was lost.
func (p *proxy) forwardLine(ctx context.Context, line inLine) bool {
	if line.tooLong {
		p.writeParseError("The message is larger than 16 MB and was not sent.")
		return false
	}
	if !json.Valid(line.data) {
		p.writeParseError("The message is not valid JSON.")
		return false
	}
	return errors.Is(p.forward(ctx, line.data), ErrUpstreamLost)
}

// maxLineBytes bounds one client message. A longer line is skipped (and
// answered with a parse error) instead of ending the session.
const maxLineBytes = 16 << 20

type inLine struct {
	data    []byte
	tooLong bool
}

type proxy struct {
	cfg     ProxyConfig
	base    *url.URL
	postURL string
	client  *http.Client
	logger  *slog.Logger

	outMu sync.Mutex
	out   io.Writer

	mu      sync.Mutex
	pending map[string]json.RawMessage // raw request id -> raw id, for requests awaiting a reply
}

func (p *proxy) authorize(r *http.Request) {
	r.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	if p.cfg.Sandbox != "" {
		r.Header.Set(SandboxHeader, p.cfg.Sandbox)
	}
}

func (p *proxy) write(data []byte) {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	_, _ = p.out.Write(append(bytes.TrimRight(data, "\n"), '\n'))
}

// readStream copies SSE `message` events to out until the stream ends, and
// reports the `endpoint` event once.
func (p *proxy) readStream(r io.Reader, endpointCh chan<- string) {
	br := bufio.NewReaderSize(r, 64<<10)
	var event string
	var data []string
	flush := func() {
		defer func() { event, data = "", nil }()
		if len(data) == 0 {
			return
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(strings.Join(data, "\n"))); err != nil && (event == "message" || event == "") {
			// One message is one line on stdout; never forward something that
			// would break the framing.
			p.logger.Warn("proxy: dropped a malformed message from the server")
			return
		}
		payload := compact.String()
		if event == "endpoint" {
			payload = strings.Join(data, "\n")
		}
		switch event {
		case "endpoint":
			select {
			case endpointCh <- payload:
			default:
			}
		case "message", "":
			p.noteReply([]byte(payload))
			p.write([]byte(payload))
		}
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if line != "" {
				data = append(data, strings.TrimPrefix(strings.TrimSpace(line), "data:"))
			}
			flush()
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"): // comment / keep-alive
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
}

// idKey normalises a JSON-RPC id so 1, 1.0 and 1e0 (or differently escaped
// strings) match between a request and its reply.
func idKey(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

type envelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

// noteReply clears the pending entry a reply from the server answers.
func (p *proxy) noteReply(msg []byte) {
	var e envelope
	if json.Unmarshal(msg, &e) != nil || len(e.ID) == 0 || e.Method != "" {
		return
	}
	p.mu.Lock()
	delete(p.pending, idKey(e.ID))
	p.mu.Unlock()
}

// forward POSTs one client line. A request is tracked until its reply streams
// back. Only a lost upstream is returned as an error.
func (p *proxy) forward(ctx context.Context, line []byte) error {
	var e envelope
	if json.Unmarshal(line, &e) != nil {
		return nil // not JSON-RPC: nothing to forward
	}
	isRequest := len(e.ID) > 0 && e.Method != ""
	if isRequest {
		p.mu.Lock()
		p.pending[idKey(e.ID)] = e.ID
		p.mu.Unlock()
	}
	postCtx, cancel := context.WithTimeout(ctx, 30*time.Second) // the POST only; the reply is awaited unbounded
	defer cancel()
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, p.postURL, bytes.NewReader(line))
	if err != nil {
		return nil
	}
	p.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if isConnRefused(err) {
			return ErrUpstreamLost
		}
		p.answerError(e, "Vibe could not pass this message to the workspace's MCP server.")
		return nil
	}
	defer resp.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUpstreamLost
	case resp.StatusCode/100 != 2:
		p.answerError(e, fmt.Sprintf("The workspace's MCP server rejected this message (HTTP %d).", resp.StatusCode))
	}
	return nil
}

// answerError replies to a request whose POST failed, then stops tracking it.
func (p *proxy) answerError(e envelope, msg string) {
	if len(e.ID) == 0 || e.Method == "" {
		return
	}
	p.mu.Lock()
	delete(p.pending, idKey(e.ID))
	p.mu.Unlock()
	p.writeError(e.ID, msg)
}

// writeParseError answers a line that is not a JSON-RPC message: -32700 with a
// null id, as JSON-RPC prescribes when the id cannot be known.
func (p *proxy) writeParseError(msg string) {
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error":   map[string]any{"code": -32700, "message": msg},
	})
	p.write(data)
}

func (p *proxy) writeError(id json.RawMessage, msg string) {
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": jsonRPCUpstreamLost, "message": msg},
	})
	p.write(data)
}

// failPending answers every request still waiting for a reply.
func (p *proxy) failPending(msg string) {
	p.mu.Lock()
	ids := make([]json.RawMessage, 0, len(p.pending))
	for k, id := range p.pending {
		ids = append(ids, id)
		delete(p.pending, k)
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.writeError(id, msg)
	}
}

// drain waits up to d for in-flight requests to be answered.
func (p *proxy) drain(ctx context.Context, streamDone <-chan struct{}, d time.Duration) {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		p.mu.Lock()
		n := len(p.pending)
		p.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-deadline.C:
			return
		case <-streamDone:
			return
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// wsaeConnRefused is Windows' WSAECONNREFUSED, which syscall.ECONNREFUSED does
// not match there.
const wsaeConnRefused = syscall.Errno(10061)

func isConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, wsaeConnRefused)
}

// scrub removes the token from an error text (net/http does not include
// headers, but nothing here should ever be able to print one).
func scrub(err error, token string) string {
	s := err.Error()
	if token != "" {
		s = strings.ReplaceAll(s, token, "***")
	}
	return s
}
