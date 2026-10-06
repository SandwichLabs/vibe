package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	vibemcp "github.com/sandwich-labs/vibe-app/internal/mcp"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/spf13/cobra"
)

// mcpCmd is the parent for MCP-related subcommands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol server commands",
}

// mcpServeCmd implements #64: `vibe mcp serve <file.vibe>` exposes a
// container's tool surface (the exact same registry Studio's chat and SSE
// MCP server use — internal/agents, filtered to ModeMCP) to an external MCP
// client, over stdio (Claude Desktop's launch model) or SSE.
var mcpServeCmd = &cobra.Command{
	Use:   "serve <file.vibe>",
	Short: "Serve a .vibe file's tools over MCP (stdio or SSE)",
	Long: `Serve a .vibe file's tools over MCP (Model Context Protocol) — the same tool
registry (internal/agents) and sandbox gating Studio's chat and SSE server use.

--sandbox can only TIGHTEN the file's own agent_sandbox setting, never loosen
it: passing --sandbox full against a file saved as read-only has no effect —
the file's own setting still applies. This stops a client's launch config
from silently gaining more access than the file's owner granted it in Studio.

stdio (the default, and what Claude Desktop expects) speaks JSON-RPC on
stdin/stdout, one message per line. stdout is the protocol channel: nothing
else — no prompts, no log lines — is ever written there; passphrase prompts
and all logging go to stderr.

The container is checkpointed and saved when the session ends, whether that
is stdin closing (the client disconnected), SIGINT, or SIGTERM.

A .vibe file can be open for writing by only one process at a time (#80): if
Studio or another writable "vibe mcp serve" already has it open, this command
exits with an error naming the holder. Pass --read-only-file to read the
file's last saved state alongside that writer instead — it forces a
read-only sandbox and never saves.`,
	Args: cobra.ExactArgs(1),
	RunE: runMCPServe,
	// A SIGINT/SIGTERM shutdown is a normal, expected way for this long-running
	// command to end — never a usage error — so Cobra must not dump the
	// command's usage text for it. runMCPServe itself also maps a clean
	// ctx.Done() shutdown (context.Canceled) to a nil error / exit 0; this
	// only covers the (already rare, now unreachable for that specific case)
	// path of a genuine runtime error still not looking like a CLI misuse.
	SilenceUsage: true,
}

// mcpInUseHint is appended to the #80 in-use error so a Claude Desktop-style
// launch config knows what to do instead of just retrying. It names the
// actual holder: when it's Studio, that means Studio's own MCP server;
// otherwise (another vibe mcp serve, or the holder is unknown) the
// Studio-specific suggestion doesn't apply, so only --read-only-file is
// offered.
func mcpInUseHint(inUse *container.InUseError) string {
	if inUse.Holder != nil && inUse.Holder.App == "Vibe Studio" {
		return "To use a file that is open in Vibe Studio, start Studio's MCP server (Settings → MCP Server) and connect to that instead, or pass --read-only-file to read its last saved state."
	}
	return "Pass --read-only-file to read its last saved state alongside the process that holds it."
}

func init() {
	mcpServeCmd.Flags().String("transport", "stdio", "transport: stdio or sse")
	mcpServeCmd.Flags().String("addr", "127.0.0.1:0", "listen address for --transport sse (127.0.0.1 only; a non-loopback address is rejected)")
	mcpServeCmd.Flags().String("sandbox", "", "sandbox mode: read-only, read-write, or full — can only TIGHTEN the file's own agent_sandbox setting, never loosen it (default: the file's own setting)")
	mcpServeCmd.Flags().Bool("read-only-file", false, "open the container itself read-only, alongside a writer (Studio or another vibe mcp serve): implies a read-only sandbox and never saves on exit")
	mcpCmd.AddCommand(mcpServeCmd)
	rootCmd.AddCommand(mcpCmd)
}

// resolveSandbox computes the effective sandbox: the file's own resolved
// mode, tightened — never loosened — by --sandbox. fileMode always arrives
// already valid (buildMCPHandler passes agents.ReadFilePolicy's Mode, which
// owns the one "what does an unset/unknown mode default to" decision — see
// its doc comment), so this function's only job is parsing and validating
// the flag string and comparing the two via agents.Tighter (the same helper
// studio/app.go uses for its own ceiling/file-mode combination) — no second
// default, no string round-trip.
func resolveSandbox(fileMode agents.SandboxMode, flagSandbox string) (agents.SandboxMode, error) {
	if flagSandbox == "" {
		return fileMode, nil
	}
	flagMode, err := agents.ParseSandboxMode(flagSandbox)
	if err != nil {
		return "", fmt.Errorf("invalid --sandbox %q: want read-only, read-write, or full", flagSandbox)
	}
	return agents.Tighter(fileMode, flagMode), nil
}

// buildMCPHandler wires up the JSON-RPC tool handler for a `vibe mcp serve`
// session: resolves the file's agent policy (agents.ReadFilePolicy — mode
// plus whether writes need approval), tightens the mode by --sandbox, and
// builds an *agents.Handler with NO approver attached — a headless process
// has nobody to show an approval card to, so a write that needs one is
// refused outright (#79 D2) rather than hanging. Split out from
// runMCPServe so a unit test can exercise the exact same production wiring.
func buildMCPHandler(c *container.Container, eng *engine.Manager, flagSandbox string, logger *slog.Logger) (*agents.Handler, agents.FilePolicy, error) {
	filePolicy := agents.ReadFilePolicy(c)
	mode, err := resolveSandbox(filePolicy.Mode, flagSandbox)
	if err != nil {
		return nil, agents.FilePolicy{}, err
	}
	policy := agents.FilePolicy{Mode: mode, AskBeforeWrites: filePolicy.AskBeforeWrites}
	sandboxCfg := &agents.SandboxConfig{Mode: policy.Mode, MaxQueryRows: 10000, RequireApproval: policy.AskBeforeWrites}
	return agents.NewHandler(c, eng, sandboxCfg, logger), policy, nil
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	transport, _ := cmd.Flags().GetString("transport")
	addr, _ := cmd.Flags().GetString("addr")
	flagSandbox, _ := cmd.Flags().GetString("sandbox")
	readOnlyFile, _ := cmd.Flags().GetBool("read-only-file")

	if transport != "stdio" && transport != "sse" {
		return fmt.Errorf("invalid --transport %q: want stdio or sse", transport)
	}

	// Passphrase resolution mirrors cmd/export.go and cmd/info.go: env var
	// first (VIBE_PASSPHRASE, non-interactive launch configs like Claude
	// Desktop's), falling back to an interactive terminal prompt. Both paths
	// write only to stderr/the TTY — never stdout, which is the stdio
	// protocol channel.
	passphrase := os.Getenv("VIBE_PASSPHRASE")
	if passphrase == "" {
		var err error
		passphrase, err = promptPassphrase("Enter passphrase: ")
		if err != nil {
			return err
		}
	}

	c, err := container.Open(filePath,
		container.WithPassphrase(passphrase),
		container.WithReadOnly(readOnlyFile),
		container.WithLogger(logger),
		container.WithLockApp("vibe mcp serve"),
	)
	if err != nil {
		var inUse *container.InUseError
		if errors.As(err, &inUse) {
			// err already carries container.Open's own "failed to open
			// container:" prefix — don't add a second one.
			return fmt.Errorf("%w\n%s", err, mcpInUseHint(inUse))
		}
		// Likewise already prefixed by container.Open itself.
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = c.Close()
		}
	}()

	// Legacy content (pages, banded reports) becomes visible to
	// list_notebooks/get_notebook the same way it would in Studio — every
	// entry point that opens a .vibe file runs the same one-time migration.
	// Skipped (not persisted) when --read-only-file — see MigrateOnOpen's
	// doc comment for why.
	notebooks.MigrateOnOpen(c.SQLiteDB(), readOnlyFile, logger)

	// --read-only-file opens the container itself read-only (#80): the
	// sandbox must follow suit, since --sandbox can only TIGHTEN the file's
	// own setting and never loosen it — resolveSandbox has no way to know
	// the container is read-only otherwise.
	if readOnlyFile {
		flagSandbox = string(agents.ModeReadOnly)
	}

	eng := engine.NewManager(c.Session(), engine.WithLogger(logger))
	rpcHandler, policy, err := buildMCPHandler(c, eng, flagSandbox, logger)
	if err != nil {
		return err
	}
	logger.Info("mcp serve: sandbox resolved", "flag_sandbox", flagSandbox, "effective", policy.Mode, "ask_before_writes", policy.AskBeforeWrites)
	// The notice is moot at read-only: there's nothing to write, asked or
	// not, so it would just be noise on every read-only session.
	if policy.AskBeforeWrites && policy.Mode != agents.ModeReadOnly {
		fmt.Fprintln(os.Stderr, `Ask before changes is on for this file: MCP clients can read but changes will be refused. Turn it off in Vibe Studio → Settings → Agent.`)
	}

	// dbMu is shared between every tool dispatch (saveHookHandler.Handle,
	// which takes RLock for the duration of each call) and the debounced/
	// final Save (which takes the exclusive Lock) — review fix (#5/finding
	// 5): without this, a periodic save could run concurrently with an
	// in-flight tool call against the same DuckDB connection. RLock lets
	// concurrent tool calls (stdio's worker pool runs several at once)
	// proceed together; a Save waits for all of them to finish, and a new
	// tool call waits for a Save in progress to finish.
	dbMu := &sync.RWMutex{}

	// Periodic save (review question from #64: "Save also periodically?"):
	// yes, debounced after each tool call that actually WROTE something —
	// review fix (#6/finding 6): a plain SELECT via run_query must never
	// mark the container dirty; wroteData (mcp_helpers.go) classifies the
	// SQL for run_query specifically and only treats the other write
	// categories (page/schema, plus every non-run_query "data"
	// tool) as writes. So a crashed/killed MCP session (no clean stdin EOF
	// or signal) loses at most the last few seconds of REAL writes, and a
	// read-heavy session (the common case) never pays a save cost early at
	// all.
	saver := newDebouncedSaver(c, logger, readOnlyFile, dbMu)
	var handler vibemcp.Handler = rpcHandler
	handler = wrapHandlerWithSaveHook(handler, saver, c.DuckDBDB(), dbMu)

	srv := vibemcp.New(handler, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var sseStarted bool
	var finalizeOnce sync.Once
	// finalize is the single, ordered shutdown sequence (review fix, finding
	// 5): stop accepting new SSE requests FIRST (so nothing can still be
	// mid-dispatch once we start closing), then stop the debounced saver
	// (which blocks until any in-flight periodic save completes — see
	// debouncedSaver.stop), then one final synchronous Save, then Close.
	// sync.Once makes this safe to call both from the normal end-of-function
	// path and from the deferred safety net below (e.g. an early return
	// before the normal path reaches it).
	finalize := func() {
		finalizeOnce.Do(func() {
			if sseStarted {
				if stopErr := srv.Stop(); stopErr != nil {
					logger.Error("mcp serve: SSE stop failed", "error", stopErr)
				}
			}
			saver.stop()
			if !readOnlyFile {
				dbMu.Lock()
				// Only Save if something was actually written this session
				// (#80 checklist item 3): a list-only session must leave
				// the file's bytes and updated_at untouched. needsSave must
				// be checked after taking dbMu.Lock — every tool dispatch
				// holds dbMu.RLock until after its own markDirty, so by the
				// time Lock() is acquired, every completed write has
				// already been counted.
				var saveErr error
				if saver.needsSave() {
					saveErr = c.Save()
				}
				dbMu.Unlock()
				if saveErr != nil {
					logger.Error("mcp serve: final save failed", "error", saveErr)
				}
			}
			if closeErr := c.Close(); closeErr != nil {
				logger.Error("mcp serve: close failed", "error", closeErr)
			}
			closed = true
		})
	}
	defer finalize()

	switch transport {
	case "stdio":
		logger.Info("mcp serve: stdio transport ready")
		err = srv.ServeStdio(ctx, os.Stdin, os.Stdout)
	case "sse":
		if hostOnlyLoopback, addrErr := isLoopbackAddr(addr); addrErr != nil {
			return addrErr
		} else if !hostOnlyLoopback {
			return fmt.Errorf("--addr %q is not a loopback address; only 127.0.0.1/localhost is permitted", addr)
		}
		if startErr := srv.Start(addr); startErr != nil {
			return fmt.Errorf("failed to start SSE server: %w", startErr)
		}
		sseStarted = true
		fmt.Fprintf(os.Stderr, "MCP SSE server listening on http://%s/sse\n", srv.Addr())
		fmt.Fprintf(os.Stderr, "Auth token (send as `Authorization: Bearer <token>`, or `?token=` on the GET /sse connection only — see docs/mcp.md): %s\n", srv.Token())
		<-ctx.Done()
	}

	// Checkpoint + Save + Close on the way out, regardless of which of the
	// three lifecycle events (stdin EOF, SIGINT, SIGTERM) triggered it —
	// ServeStdio/ctx.Done() above already covers all three uniformly.
	finalize()

	// A clean shutdown via SIGINT/SIGTERM (ctx cancelled) is not a failure —
	// review fix (finding 7): ServeStdio's underlying StdioServer.Listen
	// returns ctx.Err() (context.Canceled) when that happens, which must
	// exit 0 with no error text, not exit 1 with a printed error and usage.
	if errors.Is(err, context.Canceled) {
		err = nil
	}

	return err
}

// isLoopbackAddr reports whether addr's host part is the loopback interface
// (or empty, meaning "all interfaces on localhost" is NOT assumed safe —
// only an explicit 127.0.0.1/localhost/::1 host is accepted).
func isLoopbackAddr(addr string) (bool, error) {
	host, _, err := splitHostPortLenient(addr)
	if err != nil {
		return false, fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true, nil
	default:
		return false, nil
	}
}
