package hub

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/SandwichLabs/vibe/internal/agents"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// MCPOption configures StartWorkspaceMCP.
type MCPOption func(*mcpOptions)

type mcpOptions struct {
	logger   *slog.Logger
	wrap     func(vibemcp.Handler) vibemcp.Handler
	addr     string
	handler  *agents.Handler
	remember bool
}

// WithMCPLogger sets the logger.
func WithMCPLogger(l *slog.Logger) MCPOption { return func(o *mcpOptions) { o.logger = l } }

// WithMCPHandlerWrap wraps the tool handler, e.g. so Studio can take its own
// lock around every call.
func WithMCPHandlerWrap(f func(vibemcp.Handler) vibemcp.Handler) MCPOption {
	return func(o *mcpOptions) { o.wrap = f }
}

// WithMCPHandler serves an existing handler (Studio's own, with its approver
// attached) instead of building one from the workspace.
func WithMCPHandler(h *agents.Handler) MCPOption { return func(o *mcpOptions) { o.handler = h } }

// WithMCPAddr sets the listen address (loopback only; default is the port
// remembered for this workspace, else any free port on 127.0.0.1).
func WithMCPAddr(addr string) MCPOption { return func(o *mcpOptions) { o.addr = addr } }

// WithMCPRememberPort stores the chosen port in the per-person workspace
// settings so a client's saved address survives a restart (UX T11). Studio
// passes it; headless serve does not.
func WithMCPRememberPort() MCPOption { return func(o *mcpOptions) { o.remember = true } }

// WorkspaceMCP is a running SSE MCP server for one workspace, published in the
// hub lock so `vibe mcp serve` can proxy to it.
type WorkspaceMCP struct {
	// Handler is the tool handler being served. The owner may call
	// SetApprover on it (Studio's approval card); nil only when the caller
	// supplied a handler that is not an *agents.Handler.
	Handler *agents.Handler
	policy  agents.FilePolicy
	srv     *vibemcp.Server
	hold    *Hold
}

// StartWorkspaceMCP serves ws's agent tools over SSE on a loopback port and
// writes the URL and a fresh bearer token into hold's hub.lock. This is the one
// call Studio makes to turn MCP on for the workspace it owns. Stop turns it
// off and clears the endpoint from hub.lock.
func StartWorkspaceMCP(ws workspace.Workspace, sess session.Session, hold *Hold, opts ...MCPOption) (*WorkspaceMCP, error) {
	var o mcpOptions
	for _, f := range opts {
		f(&o)
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	if hold == nil {
		return nil, errors.New("the MCP server needs the workspace's hub lock")
	}
	m := &WorkspaceMCP{hold: hold}
	h := o.handler
	if h == nil {
		var err error
		h, m.policy, err = NewAgentHandler(ws, sess, "", o.logger)
		if err != nil {
			return nil, err
		}
	}
	m.Handler = h
	var handler vibemcp.Handler = h
	if o.wrap != nil {
		handler = o.wrap(handler)
	}
	m.srv = vibemcp.New(handler, o.logger)

	port := ws.Local().MCPPort
	addr := o.addr
	if addr == "" {
		addr = "127.0.0.1:0"
		if port > 0 {
			addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		}
	}
	if err := m.srv.Start(addr); err != nil {
		if o.addr != "" || port == 0 {
			return nil, err
		}
		// The remembered port is taken by something else: any free one will do.
		if err := m.srv.Start("127.0.0.1:0"); err != nil {
			return nil, err
		}
	}
	if err := hold.SetMCP(&MCPEndpoint{URL: "http://" + m.srv.Addr() + "/sse", Token: m.srv.Token(), Caps: []string{CapSandboxHeader}}); err != nil {
		_ = m.srv.Stop()
		return nil, fmt.Errorf("failed to publish the MCP endpoint: %w", err)
	}
	if o.remember {
		m.rememberPort(ws, port, o.logger)
	}
	return m, nil
}

func (m *WorkspaceMCP) rememberPort(ws workspace.Workspace, saved int, logger *slog.Logger) {
	_, p, err := net.SplitHostPort(m.srv.Addr())
	if err != nil {
		return
	}
	port, err := strconv.Atoi(p)
	if err != nil || port == saved || !ws.LocalTrusted() {
		return
	}
	local := ws.Local()
	local.MCPPort = port
	if err := ws.SaveLocal(local); err != nil {
		logger.Warn("could not remember the MCP port", "error", err)
	}
}

// Policy is the policy the handler was built with (zero when a handler was
// supplied).
func (m *WorkspaceMCP) Policy() agents.FilePolicy { return m.policy }

// Addr is the host:port being served.
func (m *WorkspaceMCP) Addr() string { return m.srv.Addr() }

// URL is the SSE endpoint.
func (m *WorkspaceMCP) URL() string { return "http://" + m.srv.Addr() + "/sse" }

// Token is the bearer token. A secret: show it only to the person who owns the
// workspace, never log it.
func (m *WorkspaceMCP) Token() string { return m.srv.Token() }

// Stop stops serving and clears the endpoint from hub.lock.
func (m *WorkspaceMCP) Stop() error {
	cerr := m.hold.SetMCP(nil)
	serr := m.srv.Stop()
	if serr != nil {
		return serr
	}
	return cerr
}
