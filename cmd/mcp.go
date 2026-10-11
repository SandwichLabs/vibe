package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/spf13/cobra"
)

// mcpCmd is the parent for MCP-related subcommands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol server commands",
}

// mcpServeCmd serves a workspace's tool surface (the same registry Studio's
// chat uses, internal/agents, filtered to ModeMCP) to an external MCP client
// over stdio (Claude Desktop / Claude Code's launch model) or SSE.
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve a workspace's tools over MCP (stdio or SSE)",
	Long: `Serve a workspace's tools over MCP (Model Context Protocol), using the same
tool registry (internal/agents) and sandbox gating Studio's chat uses.

The workspace is --workspace DIR, or the nearest folder above the current
directory that holds a vibe.yaml. .vibe files are no longer opened directly.

One process owns a workspace's DuckDB hub at a time (.vibe-local/hub.lock):

  - If nothing has it open, this process owns it and serves the tools itself.
  - If Vibe Studio (or another owner) has it open and is serving MCP, this
    process passes stdio through to that server, so the client never sees a
    DuckDB lock error and Studio's approval prompts apply.
  - If the owner is not serving MCP, this exits with a message naming it.

--sandbox can only TIGHTEN the workspace's own effective agent policy, never
loosen it. A headless owner has no one to approve changes, so a write that
needs approval is refused; changes to any connection other than the workspace's
local database always need Vibe Studio.

stdio speaks JSON-RPC on stdin/stdout, one message per line. stdout is the
protocol channel: nothing else is ever written there. Notices and logs go to
stderr.

Writes go straight to their sources; there is nothing to save on exit.`,
	Args:         cobra.MaximumNArgs(1),
	RunE:         runMCPServe,
	SilenceUsage: true,
}

func init() {
	mcpServeCmd.Flags().String("workspace", "", "workspace folder (default: the nearest folder above the current directory with a vibe.yaml)")
	mcpServeCmd.Flags().String("transport", "stdio", "transport: stdio or sse (sse needs this process to own the workspace)")
	mcpServeCmd.Flags().String("addr", "127.0.0.1:0", "listen address for --transport sse (127.0.0.1 only; a non-loopback address is rejected)")
	mcpServeCmd.Flags().String("sandbox", "", "sandbox mode: read-only, read-write, or full. Can only TIGHTEN the workspace's own agent policy, never loosen it")
	mcpCmd.AddCommand(mcpServeCmd)
	rootCmd.AddCommand(mcpCmd)
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		if strings.EqualFold(filepath.Ext(strings.TrimSpace(args[0])), ".vibe") {
			return hub.ErrLegacyFile
		}
		return fmt.Errorf("vibe mcp serve no longer takes a file argument; use --workspace %s", args[0])
	}
	dir, _ := cmd.Flags().GetString("workspace")
	transport, _ := cmd.Flags().GetString("transport")
	addr, _ := cmd.Flags().GetString("addr")
	sandbox, _ := cmd.Flags().GetString("sandbox")

	if transport != "stdio" && transport != "sse" {
		return fmt.Errorf("invalid --transport %q: want stdio or sse", transport)
	}
	if transport == "sse" {
		if ok, addrErr := isLoopbackAddr(addr); addrErr != nil {
			return addrErr
		} else if !ok {
			return fmt.Errorf("--addr %q is not a loopback address; only 127.0.0.1/localhost is permitted", addr)
		}
	}

	var out = cmd.OutOrStdout()
	if transport == "stdio" {
		// Before anything else runs: stdout becomes the protocol channel and
		// every other writer to fd 1 (library noise, a C printf, MotherDuck's
		// SSO link) is pointed at stderr.
		proto, err := hub.ProtectStdout()
		if err != nil {
			return err
		}
		out = proto
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	err := hub.Serve(ctx, hub.ServeOptions{
		Workspace: dir,
		Transport: transport,
		Addr:      addr,
		Sandbox:   sandbox,
		In:        os.Stdin,
		Out:       out,
		Err:       os.Stderr,
		Logger:    logger,
	})
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	return err
}

// isLoopbackAddr reports whether addr's host part is the loopback interface.
// Only an explicit 127.0.0.1/localhost/::1 host is accepted; an empty host
// ("all interfaces") is not assumed safe.
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
