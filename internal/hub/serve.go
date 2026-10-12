package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/agents"
	vibemcp "github.com/SandwichLabs/vibe/internal/mcp"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// Holder app names, as written into hub.lock.
const (
	AppStudio = "Vibe Studio"
	AppServe  = "vibe mcp serve"
)

// ErrLegacyFile is returned when `vibe mcp serve` is pointed at a .vibe file.
var ErrLegacyFile = errors.New(".vibe files are no longer opened directly; open or create a workspace folder, then run `vibe mcp serve --workspace <folder>`")

// ServeOptions configures Serve.
type ServeOptions struct {
	// Workspace is the folder to serve (or any folder inside it). Empty walks
	// up from Cwd (default: the current directory).
	Workspace string
	Cwd       string
	// Transport is "stdio" (default) or "sse". sse is only meaningful when
	// this process owns the hub.
	Transport string
	// Addr is the SSE listen address (loopback only).
	Addr string
	// Sandbox tightens (never loosens) the workspace's effective policy.
	Sandbox string

	// In and Out are the stdio protocol channel. Out must carry protocol bytes
	// only: use ProtectStdout's handle for the real process.
	In  io.Reader
	Out io.Writer
	// Err gets human-readable notices. Never the protocol channel.
	Err    io.Writer
	Logger *slog.Logger
	// App overrides the holder name in hub.lock (default AppServe).
	App string
}

// ResolveRoot turns a --workspace value (or "" for the working directory) into
// a workspace root, refusing .vibe files with ErrLegacyFile.
func ResolveRoot(dir, cwd string) (string, error) {
	if dir != "" && strings.EqualFold(filepath.Ext(dir), ".vibe") {
		return "", ErrLegacyFile
	}
	start := dir
	if start == "" {
		start = cwd
		if start == "" {
			wd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("failed to find the current directory: %w", err)
			}
			start = wd
		}
	}
	if fi, err := os.Stat(start); err == nil && fi.Mode().IsRegular() && dir != "" {
		return "", fmt.Errorf("%q is a file, not a workspace folder", dir)
	}
	root, err := workspace.Discover(start)
	if err != nil {
		if errors.Is(err, workspace.ErrNotWorkspace) {
			return "", fmt.Errorf("no Vibe workspace found at or above %s (a workspace is a folder with a vibe.yaml); pass --workspace <folder>", start)
		}
		return "", err
	}
	return root, nil
}

// Serve runs `vibe mcp serve` for one workspace until in reaches EOF, ctx is
// cancelled, or (owner mode) another process asks it to release the hub.
//
//   - Owner mode: the hub lock is free, so this process takes it, opens the
//     workspace and its DuckDB hub, and serves the agent tools over opts.Transport.
//     Over stdio it also serves loopback SSE and publishes the endpoint in
//     hub.lock, so a second `vibe mcp serve` (another Claude client) proxies to
//     this process instead of failing.
//   - Proxy mode: a live holder (Studio, or another owner) advertises MCP, so
//     stdio is bridged to it.
func Serve(ctx context.Context, opts ServeOptions) error {
	if err := opts.applyDefaults(); err != nil {
		return err
	}
	// Validate before touching anything so a bad flag never takes the lock.
	if _, err := ResolveSandbox(agents.ModeFull, opts.Sandbox); err != nil {
		return err
	}
	root, err := ResolveRoot(opts.Workspace, opts.Cwd)
	if err != nil {
		return err
	}

	var releasing = make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	onRelease := func() {
		select {
		case <-releasing:
		default:
			close(releasing)
		}
		cancel()
	}

	// A holder that is just exiting can briefly look held and then not, and
	// one that is just starting has not published its endpoint yet: keep
	// deciding for a while before giving up.
	deadline := time.Now().Add(startupWait)
	var lastErr error
	for {
		opened, err := Open(ctx, root, OpenOptions{
			App: opts.App, Logger: opts.Logger,
			HubOptions:    []Option{WithReleaseHandler(onRelease), WithPendingMCP()},
			BeforeSession: func(ws workspace.Workspace) { serveNotices(ws, opts) },
		})
		if err == nil {
			return serveOwner(ctx, opened, opts, releasing)
		}
		held, fatal := heldError(root, err)
		if fatal != nil {
			return fatal
		}
		lastErr = held
		holder, rerr := ReadHolder(root)
		proxy, err := holderDecision(root, holder, rerr)
		if err != nil {
			return err
		}
		if proxy {
			return serveProxy(ctx, root, holder, opts)
		}
		if time.Now().After(deadline) {
			return startupTimeout(lastErr, holder, rerr)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// applyDefaults fills in the unset options and checks the transport.
func (opts *ServeOptions) applyDefaults() error {
	if opts.Transport == "" {
		opts.Transport = "stdio"
	}
	if opts.Transport != "stdio" && opts.Transport != "sse" {
		return fmt.Errorf("invalid --transport %q: want stdio or sse", opts.Transport)
	}
	if opts.Err == nil {
		opts.Err = io.Discard
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(opts.Err, nil))
	}
	if opts.App == "" {
		opts.App = AppServe
	}
	return nil
}

// heldError says why Open did not make this process the owner. It returns the
// error to remember while waiting for the holder (the workspace is held), or a
// fatal error as its second result.
func heldError(root string, err error) (held, fatal error) {
	var he *HeldError
	if errors.As(err, &he) {
		return err, nil
	}
	var oe *OpenError
	if !errors.As(err, &oe) || oe.Step != StepOpen {
		return nil, err
	}
	// A workspace that does not parse can still be proxied to: the
	// holder has it open, and passing MCP through never reads the yaml.
	if _, rerr := ReadHolder(root); rerr != nil {
		return nil, fmt.Errorf("failed to open the workspace: %w", oe.Err)
	}
	return fmt.Errorf("failed to open the workspace: %w", oe.Err), nil
}

// holderDecision says what to do about whoever holds the workspace: proxy to
// it, or (false, nil) wait and try again. An unreadable lock is an error.
func holderDecision(root string, holder *Holder, rerr error) (proxy bool, err error) {
	switch {
	case errors.Is(rerr, ErrNotHeld):
		return false, nil
	case rerr != nil:
		return false, fmt.Errorf("%w: another process has workspace %s open but its hub lock cannot be read safely (%v); close it, or remove %s if no Vibe process is running",
			ErrHubHeld, filepath.Base(root), rerr, LockPath(root))
	case holder.MCP != nil && holder.MCP.Pending, holder.MCP == nil && holder.App == AppServe:
		// Another `vibe mcp serve` is still opening the workspace.
		return false, nil
	}
	return true, nil
}

// startupTimeout is the error once the wait for the holder runs out.
func startupTimeout(lastErr error, holder *Holder, rerr error) error {
	if errors.Is(rerr, ErrNotHeld) {
		return lastErr
	}
	if holder != nil {
		return fmt.Errorf("%w: %s did not finish opening the workspace in time", ErrHubHeld, holder)
	}
	return lastErr
}

// startupWait is how long a second `vibe mcp serve` waits for the first to
// finish opening the workspace and publish its endpoint.
const startupWait = 15 * time.Second

func serveProxy(ctx context.Context, root string, holder *Holder, opts ServeOptions) error {
	if opts.Transport != "stdio" {
		return fmt.Errorf("%w: %s already has this workspace open, so `--transport sse` cannot serve it from here; connect to its MCP server instead", ErrHubHeld, holder)
	}
	if holder.MCP == nil {
		return fmt.Errorf("%w: %s", ErrHubHeld, noMCPMessage(holder))
	}
	if opts.Sandbox != "" && !holder.MCP.Has(CapSandboxHeader) {
		return fmt.Errorf("%w: %s cannot apply --sandbox to a proxied session (it is an older Vibe); update it, or start without --sandbox", ErrHubHeld, holder)
	}
	fmt.Fprintf(opts.Err, "Workspace %s is open in %s; passing MCP through to it.\n", filepath.Base(root), holder)
	err := vibemcp.ProxyStdio(ctx, vibemcp.ProxyConfig{
		Endpoint: holder.MCP.URL, Token: holder.MCP.Token, Sandbox: opts.Sandbox, Logger: opts.Logger,
	}, opts.In, opts.Out)
	if errors.Is(err, vibemcp.ErrUpstreamLost) {
		return fmt.Errorf("%w (%s); reconnect your MCP client to continue", err, holder)
	}
	return err
}

func noMCPMessage(h *Holder) string {
	if h.App == AppStudio {
		return fmt.Sprintf("Studio (pid %d) has this workspace open but its MCP server is off. Turn it on in Settings → Workspace → MCP, or close Studio.", h.PID)
	}
	return fmt.Sprintf("%s has this workspace open and is not sharing MCP with other processes. Close it first.", h)
}

// serveNotices is what `mcp serve` says once it owns the hub, before it opens
// the session: the workspace's warnings and the state of its .env.
func serveNotices(ws workspace.Workspace, opts ServeOptions) {
	for _, w := range ws.Warnings() {
		fmt.Fprintln(opts.Err, "warning:", w)
	}
	if st := ws.DotEnv(); st.State != "none" && st.State != "allowed" {
		// Headless: never prompt, use only a saved allow. Names only.
		opts.Logger.Info("mcp serve: this workspace has a .env that is not allowed, so it is not loaded; run `vibe workspace env allow` to use it",
			"state", st.State, "variables", st.Names)
	}
}

func serveOwner(ctx context.Context, opened *Opened, opts ServeOptions, releasing <-chan struct{}) (err error) {
	// Session, then the lock (the next owner may open the DuckDB files the
	// moment it is free), then the workspace.
	defer opened.Close()
	ws, sess, hold, issues := opened.WS, opened.Sess, opened.Hold, opened.Issues
	for _, i := range issues {
		fmt.Fprintf(opts.Err, "connection %s is not available: %s\n", i.Connection, i.Message)
	}

	handler, policy, err := NewAgentHandler(ws, sess, opts.Sandbox, opts.Logger)
	if err != nil {
		return err
	}
	opts.Logger.Info("mcp serve: sandbox resolved", "flag_sandbox", opts.Sandbox, "effective", policy.Mode, "ask_before_writes", policy.AskBeforeWrites)
	if policy.AskBeforeWrites && policy.Mode != agents.ModeReadOnly {
		fmt.Fprintln(opts.Err, askNotice)
	}

	// Publish an SSE endpoint so a second `vibe mcp serve` can proxy to us.
	addr := opts.Addr
	if opts.Transport == "stdio" {
		addr = "127.0.0.1:0"
	}
	share, serr := StartWorkspaceMCP(ws, sess, hold, WithMCPHandler(handler), WithMCPLogger(opts.Logger), WithMCPAddr(addr))
	if serr != nil {
		if opts.Transport == "sse" {
			return fmt.Errorf("failed to start the SSE server: %w", serr)
		}
		_ = hold.SetMCP(nil)
		fmt.Fprintf(opts.Err, "warning: could not share this workspace with other MCP clients: %v\n", serr)
	}
	defer func() {
		if share != nil {
			_ = share.Stop()
		}
	}()

	switch opts.Transport {
	case "stdio":
		opts.Logger.Info("mcp serve: stdio transport ready")
		err = vibemcp.New(handler, opts.Logger).ServeStdio(ctx, opts.In, opts.Out)
	case "sse":
		fmt.Fprintf(opts.Err, "MCP SSE server listening on %s\n", share.URL())
		fmt.Fprintf(opts.Err, "Auth token (send as `Authorization: Bearer <token>`, see docs/reference/mcp.md): %s\n", share.Token())
		<-ctx.Done()
	}
	select {
	case <-releasing:
		fmt.Fprintln(opts.Err, "Another Vibe process asked for this workspace; releasing it and exiting. Reconnect your MCP client to continue through that process.")
	default:
	}
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	return err
}

const askNotice = `Ask before changes is on for this workspace: MCP clients can read but changes will be refused (no one is here to approve them). Changes to a connection other than this workspace's local database always need Vibe Studio. Turn the gate off in Vibe Studio → Settings → Agent.`
