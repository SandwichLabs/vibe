package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/tui"
	"github.com/spf13/cobra"
)

var studioCmd = &cobra.Command{
	Use:   "studio <file>",
	Short: "Launch the TUI editor (Vibe Studio)",
	Long: `Open a .vibe file in the interactive terminal UI: a SQL REPL, a
read-only table browser, and a notebook list (source only — no rendering).
Notebooks, skills, and A2UI dashboards are authored in the Studio desktop
app (--gui), not here.

If the file does not exist, it will be created.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// Launch the desktop GUI if --gui is set.
		gui, _ := cmd.Flags().GetBool("gui")
		if gui {
			return launchGUI(filePath)
		}

		// Determine if creating or opening.
		_, statErr := os.Stat(filePath)
		isNew := os.IsNotExist(statErr)

		var passphrase string
		var err error

		if isNew {
			fmt.Fprintf(cmd.OutOrStdout(), "File %s does not exist. Creating new container.\n", filePath)
			passphrase, err = promptPassphraseConfirm()
		} else {
			passphrase, err = promptPassphrase("Enter passphrase: ")
		}
		if err != nil {
			return err
		}

		var c *container.Container
		if isNew {
			c, err = container.Create(filePath,
				container.WithPassphrase(passphrase),
				container.WithLogger(logger),
			)
		} else {
			c, err = container.Open(filePath,
				container.WithPassphrase(passphrase),
				container.WithLogger(logger),
			)
		}
		if err != nil {
			return err
		}
		defer c.Close()

		// Legacy content (pages, banded reports) becomes visible to the TUI's
		// notebook list the same way it would in Studio or `vibe mcp serve` —
		// every entry point that opens a .vibe file runs the same one-time
		// migration. `vibe studio` has no read-only-open flag, so this always
		// persists.
		notebooks.MigrateOnOpen(c.SQLiteDB(), false, logger)

		// Create the engine manager (DuckDB-only).
		eng := engine.NewManager(
			c.Session(),
			engine.WithLogger(logger),
		)

		// Start agent if --agent-cmd or --agent-listen is set.
		agentCmd, _ := cmd.Flags().GetString("agent-cmd")
		agentListen, _ := cmd.Flags().GetString("agent-listen")
		agentModeStr, _ := cmd.Flags().GetString("agent-mode")

		if agentCmd != "" || agentListen != "" {
			sandboxCfg := agents.DefaultSandboxConfig()
			sandboxCfg.Mode = parseSandboxMode(agentModeStr)

			handler := agents.NewHandler(c, eng, &sandboxCfg, logger)

			agentCtx, agentCancel := context.WithCancel(context.Background())
			defer agentCancel()

			if agentCmd != "" {
				srv, proc, startErr := startAgentCmd(agentCmd, handler, agentCtx)
				if startErr != nil {
					return fmt.Errorf("failed to start agent command: %w", startErr)
				}
				defer func() {
					srv.Stop()  //nolint:errcheck
					proc.Kill() //nolint:errcheck
				}()
			}

			if agentListen != "" {
				srv, startErr := startAgentListener(agentListen, handler, agentCtx)
				if startErr != nil {
					return fmt.Errorf("failed to start agent listener: %w", startErr)
				}
				defer srv.Stop() //nolint:errcheck
			}
		}

		// Create and load the TUI app.
		app := tui.NewApp(c, eng, logger)
		app.LoadData()

		// Run the BubbleTea program.
		p := tea.NewProgram(app, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}

		return nil
	},
}

// parseSandboxMode converts a string flag value to a SandboxMode.
func parseSandboxMode(mode string) agents.SandboxMode {
	switch strings.ToLower(mode) {
	case "read-write":
		return agents.ModeReadWrite
	case "full":
		return agents.ModeFull
	default:
		return agents.ModeReadOnly
	}
}

// startAgentCmd spawns a subprocess and connects via stdio transport.
func startAgentCmd(cmdStr string, handler *agents.Handler, ctx context.Context) (*agents.AgentServer, *os.Process, error) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("empty agent command")
	}

	proc := exec.CommandContext(ctx, parts[0], parts[1:]...)
	stdout, err := proc.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stdin, err := proc.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	if err := proc.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start agent process: %w", err)
	}

	transport := agents.NewStdioTransport(stdout, stdin)
	srv := agents.NewAgentServer(handler, transport, logger)

	go func() {
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			logger.Error("agent server error", "error", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "Agent started (stdio): %s\n", cmdStr)
	return srv, proc.Process, nil
}

// startAgentListener starts a socket transport for incoming agent connections.
func startAgentListener(addr string, handler *agents.Handler, ctx context.Context) (*agents.AgentServer, error) {
	transport, err := agents.NewSocketTransport(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket transport: %w", err)
	}

	srv := agents.NewAgentServer(handler, transport, logger)

	go func() {
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			logger.Error("agent listener error", "error", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "Agent listening on %s\n", addr)
	return srv, nil
}

// launchGUI finds and executes the vibe-studio binary, replacing the current process.
func launchGUI(filePath string) error {
	const binName = "vibe-studio"
	var searched []string

	// First, look next to the current executable.
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), binName)
		searched = append(searched, beside)
		if _, err := os.Stat(beside); err == nil {
			return syscall.Exec(beside, []string{binName, filePath}, os.Environ())
		}
	}

	// Then, try PATH.
	if found, err := exec.LookPath(binName); err == nil {
		return syscall.Exec(found, []string{binName, filePath}, os.Environ())
	}
	searched = append(searched, "$PATH")

	return fmt.Errorf("vibe-studio binary not found; searched: %s", strings.Join(searched, ", "))
}

func init() {
	studioCmd.Flags().Bool("gui", false, "launch the desktop GUI instead of the TUI")
	studioCmd.Flags().String("agent-cmd", "", "agent command to spawn (stdio transport)")
	studioCmd.Flags().String("agent-listen", "", "agent socket address to listen on")
	studioCmd.Flags().String("agent-mode", "read-only", "agent sandbox mode: read-only, read-write, full")
	rootCmd.AddCommand(studioCmd)
}
