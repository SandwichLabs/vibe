package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/tui"
	"github.com/SandwichLabs/vibe/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
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

--gui launches the desktop app instead and opens the workspace there: dir (or
--workspace), else the nearest folder above the current directory that holds a
vibe.yaml. A .vibe bundle opens its Import dialog. Outside any workspace it
starts at the launcher; a vibe.yaml in your home folder, above it, or in / is
never opened automatically. Run from a terminal, the desktop app (vibe-studio)
does the same with its own path argument or working directory.

Notebooks, skills and A2UI dashboards are authored in the desktop app.`,
	Args:         cobra.MaximumNArgs(1),
	RunE:         runStudio,
	SilenceUsage: true,
}

func runStudio(cmd *cobra.Command, args []string) error {
	dir := flagString(cmd, "workspace")
	if len(args) == 1 {
		if dir != "" {
			return fmt.Errorf("give the workspace as dir or --workspace, not both")
		}
		dir = args[0]
	}
	// The desktop app takes a .vibe bundle too: it opens the Import dialog.
	if gui, _ := cmd.Flags().GetBool("gui"); gui {
		return launchGUI(guiTarget(dir))
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

	o, readOnly, err := openForTUI(ctx, dir, readOnly, errW)
	if err != nil {
		return err
	}
	defer o.Close()

	// A default database that is locked would leave an empty TUI: say so now.
	if d := o.WS.DefaultConnection(); o.hasIssue(d) {
		if o.isLocked(d) {
			return fmt.Errorf("connection %s is %s", d, lockedMessage(o.WS.Root()))
		}
	}

	// An agent over stdio or a socket gets the same tools and gating as MCP.
	// A read-only open never serves one: it does not own the database.
	if agentCmd != "" || agentListen != "" {
		if readOnly {
			return fmt.Errorf("--agent-cmd and --agent-listen need the workspace's database; they cannot run with --read-only")
		}
		stop, err := startTUIAgent(ctx, o, agentCmd, agentListen, agentMode)
		if err != nil {
			return err
		}
		defer stop()
	}

	app := tui.NewApp(tuiWorkspace{o}, o.Eng, logger,
		tui.WithReadOnly(readOnly), tui.WithWorkspaceName(o.WS.Name()), tui.WithNotices(tuiNotices(o)...))
	app.LoadData()
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// openForTUI opens the workspace for the TUI. When the owner holds it and a
// person is at the keyboard, it offers a read-only open instead (probing first,
// since a locked default database would leave the TUI empty). It returns the
// effective read-only flag.
func openForTUI(ctx context.Context, dir string, readOnly bool, errW io.Writer) (*openedWorkspace, bool, error) {
	o, err := openWorkspace(ctx, dir, readOnly, true, "vibe studio", errW)
	if err == nil || readOnly || !errors.Is(err, hub.ErrHubHeld) || !stdinIsTerminal() {
		return o, readOnly, err
	}
	suppressDotEnvPrompt = true
	probe, perr := openWorkspace(ctx, dir, true, true, "vibe studio", io.Discard)
	suppressDotEnvPrompt = false
	switch {
	case perr != nil:
	case defaultIsLocalDuckDB(probe) && probe.hasIssue(probe.WS.DefaultConnection()):
		probe.Close()
		return nil, readOnly, fmt.Errorf("%w\n(read-only would not help: this workspace's own database is a local DuckDB file the owner keeps locked)", err)
	default:
		fmt.Fprintln(errW, err)
		if askYesNo(errW, "Open read-only instead?") {
			return probe, true, nil
		}
		probe.Close()
	}
	return nil, readOnly, err
}

// tuiNotices turns connection problems into the REPL's first lines: the
// alternate screen would hide anything printed to stderr from here on.
func tuiNotices(o *openedWorkspace) []string {
	var notices []string
	for _, i := range o.Issues {
		msg := i.Message
		if o.isLocked(i.Connection) {
			msg = lockedMessage(o.WS.Root())
		}
		notices = append(notices, fmt.Sprintf("-- connection %s is not available: %s", i.Connection, msg))
	}
	return notices
}

// startTUIAgent serves the workspace's agent tools over a subprocess's stdio
// and/or a socket. The returned stop shuts both down.
func startTUIAgent(ctx context.Context, o *openedWorkspace, agentCmd, agentListen, agentMode string) (stop func(), err error) {
	handler, _, err := hub.NewAgentHandler(o.WS, o.Sess, agentMode, logger)
	if err != nil {
		return nil, err
	}
	agentCtx, agentCancel := context.WithCancel(ctx)
	var stops []func()
	stop = func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
		agentCancel()
	}
	if agentCmd != "" {
		srv, proc, startErr := startAgentCmd(agentCmd, handler, agentCtx)
		if startErr != nil {
			agentCancel()
			return nil, fmt.Errorf("failed to start agent command: %w", startErr)
		}
		stops = append(stops, func() {
			srv.Stop()  //nolint:errcheck
			proc.Kill() //nolint:errcheck
		})
	}
	if agentListen != "" {
		srv, startErr := startAgentListener(agentListen, handler, agentCtx)
		if startErr != nil {
			stop()
			return nil, fmt.Errorf("failed to start agent listener: %w", startErr)
		}
		stops = append(stops, func() { srv.Stop() }) //nolint:errcheck
	}
	return stop, nil
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

// guiTarget is the argument handed to the desktop app. An explicit dir that is
// a folder inside a workspace becomes that workspace's root, and with no dir
// the workspace around the current directory is used (never the home folder, an
// ancestor of it, or /); the app repeats the same discovery from its own
// working directory, so an empty result still lands on the launcher. A .vibe
// bundle or anything unresolvable is passed through for the app to explain.
func guiTarget(dir string) string {
	home, _ := os.UserHomeDir()
	start := dir
	if dir == "" {
		start, _ = os.Getwd()
	} else if isVibeFile(dir) {
		return dir
	}
	if start == "" {
		return dir
	}
	if root, err := workspace.DiscoverForLaunch(start, home); err == nil {
		if dir == "" {
			return root
		}
		if fi, serr := os.Stat(dir); serr == nil && fi.IsDir() {
			return root
		}
	}
	return dir
}

func init() {
	addWorkspaceFlag(studioCmd)
	studioCmd.Flags().Bool("gui", false, "launch the desktop app instead of the terminal UI")
	studioCmd.Flags().Bool("tui", false, "the terminal UI (the default; accepted for older scripts)")
	_ = studioCmd.Flags().MarkHidden("tui")
	studioCmd.Flags().Bool("read-only", false, "open without taking ownership of the workspace's database (reaches connections other than local DuckDB files)")
	studioCmd.Flags().String("agent-cmd", "", "agent command to spawn (stdio transport)")
	studioCmd.Flags().String("agent-listen", "", "agent socket address to listen on")
	studioCmd.Flags().String("agent-mode", "read-only", "the agent's sandbox: read-only, read-write or full (can only tighten the workspace's own policy)")
	rootCmd.AddCommand(studioCmd)
}
