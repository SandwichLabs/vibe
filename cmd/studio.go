package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/server"
	"github.com/sandwich-labs/vibe-app/internal/tui"
	"github.com/spf13/cobra"
)

var studioCmd = &cobra.Command{
	Use:   "studio <file>",
	Short: "Launch the TUI editor (Vibe Studio)",
	Long: `Open a .vibe file in the interactive terminal UI for editing pages,
browsing tables, running queries, and managing the container.

If the file does not exist, it will be created.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

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
			return fmt.Errorf("failed to open container: %w", err)
		}
		defer c.Close()

		// Create the engine manager (DuckDB-only).
		eng := engine.NewManager(
			c.DuckDBDB(),
			engine.WithLogger(logger),
		)

		// Start web server if --web flag is set.
		webEnabled, _ := cmd.Flags().GetBool("web")
		if webEnabled {
			host, _ := cmd.Flags().GetString("host")
			port, _ := cmd.Flags().GetInt("port")
			allowSQL, _ := cmd.Flags().GetBool("allow-sql")
			addr := fmt.Sprintf("%s:%d", host, port)

			srv := server.New(
				server.WithAddress(addr),
				server.WithContainer(c),
				server.WithEngine(eng),
				server.WithAllowSQL(allowSQL),
				server.WithLogger(logger),
			)

			if err := srv.Start(); err != nil {
				return fmt.Errorf("failed to start web server: %w", err)
			}
			defer srv.Stop(context.Background()) //nolint:errcheck

			fmt.Fprintf(os.Stderr, "Web server started at http://%s\n", srv.Addr())
		}

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

func init() {
	studioCmd.Flags().Bool("web", false, "start the embedded web server")
	studioCmd.Flags().Int("port", 3456, "web server port")
	studioCmd.Flags().String("host", "localhost", "web server host")
	studioCmd.Flags().Bool("allow-sql", false, "allow ad-hoc SQL queries via the web API")
	studioCmd.Flags().String("agent-cmd", "", "agent command to spawn (stdio transport)")
	studioCmd.Flags().String("agent-listen", "", "agent socket address to listen on")
	studioCmd.Flags().String("agent-mode", "read-only", "agent sandbox mode: read-only, read-write, full")
	rootCmd.AddCommand(studioCmd)
}
