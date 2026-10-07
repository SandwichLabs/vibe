package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/hub"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/session"
	"github.com/sandwich-labs/vibe-app/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var studioCmd = &cobra.Command{
	Use:   "studio [dir]",
	Short: "Open Vibe Studio on a workspace (terminal UI; --gui for the desktop app)",
	Long: `Open a workspace in the terminal UI: a SQL REPL on the workspace's DuckDB
session, a read-only table browser across its connections, and a notebook list
(source only). The workspace is dir or the nearest folder above the current
directory that holds a vibe.yaml.

The TUI takes ownership of the workspace's database while it runs, like Studio
does. If something else has it open you are told who; when stdin is a terminal
and the workspace has connections other than local DuckDB files, you are
offered --read-only (reading without ownership). The owner keeps the
workspace's local .duckdb files locked, so read-only cannot reach those.

--gui launches the desktop app instead and opens the workspace folder (dir)
there; a .vibe bundle opens its Import dialog; without dir it starts at the
launcher.

Notebooks, skills and A2UI dashboards are authored in the desktop app.`,
	Args:         cobra.MaximumNArgs(1),
	RunE:         runStudio,
	SilenceUsage: true,
}

func runStudio(cmd *cobra.Command, args []string) error {
	dir := ""
	if len(args) == 1 {
		dir = args[0]
	}
	// The desktop app takes a .vibe bundle too: it opens the Import dialog.
	if gui, _ := cmd.Flags().GetBool("gui"); gui {
		return launchGUI(dir)
	}
	if isVibeFile(dir) {
		return errLegacyFileArg
	}
	return runTUI(cmd, dir, cmd.InOrStdin(), cmd.OutOrStdout())
}

// defaultIsLocalDuckDB reports whether the workspace's default connection is a
// local DuckDB file, which a read-only open cannot reach while an owner holds it.
func defaultIsLocalDuckDB(o *openedWorkspace) bool {
	c, ok := o.knownConnection(o.WS.DefaultConnection())
	return ok && c.Type == "duckdb"
}

func runTUI(cmd *cobra.Command, dir string, in io.Reader, out io.Writer) error {
	readOnly, _ := cmd.Flags().GetBool("read-only")
	agentCmd, _ := cmd.Flags().GetString("agent-cmd")
	agentListen, _ := cmd.Flags().GetString("agent-listen")
	agentMode, _ := cmd.Flags().GetString("agent-mode")
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	errW := cmd.ErrOrStderr()

	o, err := openWorkspace(ctx, dir, readOnly, true, "vibe studio", errW)
	if err != nil && !readOnly && errors.Is(err, hub.ErrHubHeld) && stdinIsTerminal() {
		// Offer read-only only when it can show something: probe first, since
		// a locked default database would leave the TUI empty.
		probe, perr := openWorkspace(ctx, dir, true, true, "vibe studio", io.Discard)
		switch {
		case perr != nil:
		case defaultIsLocalDuckDB(probe) && probe.hasIssue(probe.WS.DefaultConnection()):
			probe.Close()
			return fmt.Errorf("%w\n(read-only would not help: this workspace's own database is a local DuckDB file the owner keeps locked)", err)
		default:
			fmt.Fprintln(errW, err)
			if askYesNo(errW, "Open read-only instead?") {
				readOnly, o, err = true, probe, nil
			} else {
				probe.Close()
			}
		}
	}
	if err != nil {
		return err
	}
	defer o.Close()

	// A default database that is locked would leave an empty TUI: say so now.
	if d := o.WS.DefaultConnection(); o.hasIssue(d) {
		if i, _ := o.issueFor(d); isLockMessage(i.Message) {
			return fmt.Errorf("connection %s is %s", d, lockedMessage(o.WS.Root()))
		}
	}
	// The alternate screen would hide anything printed to stderr from here on,
	// so connection problems go into the REPL's first lines.
	var notices []string
	for _, i := range o.Issues {
		msg := i.Message
		if isLockMessage(msg) {
			msg = lockedMessage(o.WS.Root())
		}
		notices = append(notices, fmt.Sprintf("-- connection %s is not available: %s", i.Connection, msg))
	}

	// An agent over stdio or a socket gets the same tools and gating as MCP.
	// A read-only open never serves one: it does not own the database.
	if agentCmd != "" || agentListen != "" {
		if readOnly {
			return fmt.Errorf("--agent-cmd and --agent-listen need the workspace's database; they cannot run with --read-only")
		}
		handler, _, err := hub.NewAgentHandler(o.WS, o.Sess, agentMode, logger)
		if err != nil {
			return err
		}
		agentCtx, agentCancel := context.WithCancel(ctx)
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

	app := tui.NewApp(tuiWorkspace{o}, o.Eng, logger,
		tui.WithReadOnly(readOnly), tui.WithWorkspaceName(o.WS.Name()), tui.WithNotices(notices...))
	app.LoadData()
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// tuiWorkspace adapts an opened workspace to tui.WorkspaceAccess.
type tuiWorkspace struct{ o *openedWorkspace }

func (w tuiWorkspace) Notebooks() notebooks.Store { return w.o.WS.Notebooks() }
func (w tuiWorkspace) Session() session.Session   { return w.o.Sess }

func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// askYesNo asks a yes/no question on w and reads the answer from stdin; the
// default is no.
func askYesNo(w io.Writer, question string) bool {
	fmt.Fprintf(w, "%s [y/N] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
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

// launchGUI finds and executes the vibe-studio binary, replacing the current
// process. dir is passed through as its one argument, the workspace folder
// Studio opens at launch; empty means no argument.
func launchGUI(dir string) error {
	const binName = "vibe-studio"
	argv := []string{binName}
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve %q: %w", dir, err)
		}
		argv = append(argv, abs)
	}
	var searched []string

	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), binName)
		searched = append(searched, beside)
		if _, err := os.Stat(beside); err == nil {
			return syscall.Exec(beside, argv, os.Environ())
		}
	}

	if found, err := exec.LookPath(binName); err == nil {
		return syscall.Exec(found, argv, os.Environ())
	}
	searched = append(searched, "$PATH")

	return fmt.Errorf("vibe-studio binary not found; searched: %s (`vibe studio` without --gui opens the terminal UI)", strings.Join(searched, ", "))
}

func init() {
	studioCmd.Flags().Bool("gui", false, "launch the desktop app instead of the terminal UI")
	studioCmd.Flags().Bool("tui", false, "the terminal UI (the default; accepted for older scripts)")
	_ = studioCmd.Flags().MarkHidden("tui")
	studioCmd.Flags().Bool("read-only", false, "open without taking ownership of the workspace's database (reaches connections other than local DuckDB files)")
	studioCmd.Flags().String("agent-cmd", "", "agent command to spawn (stdio transport)")
	studioCmd.Flags().String("agent-listen", "", "agent socket address to listen on")
	studioCmd.Flags().String("agent-mode", "read-only", "the agent's sandbox: read-only, read-write or full (can only tighten the workspace's own policy)")
	rootCmd.AddCommand(studioCmd)
}
