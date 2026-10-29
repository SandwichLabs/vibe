package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SandwichLabs/vibe/internal/editor"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/lsp"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/spf13/cobra"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Serve SQL completion for a workspace to your editor (Language Server Protocol, stdio)",
	Long: `Run a language server on standard input and output for any editor with an LSP
client (Neovim, Helix, VS Code, Zed, Emacs). In .sql files under a workspace it
completes connections, schemas, tables, views, columns (through aliases too),
DuckDB functions and keywords, and hover shows a table's columns or a column's
type. Editor setup: editors/README.md.

The server opens no database and writes no file: DuckDB lets one process write
a .duckdb file or several read it, never both, and an editor's language server
runs for hours. It reads a snapshot of the catalog in .vibe-local/catalog.json
and picks up changes to it. The TUI (vibe studio) writes the snapshot when it
opens and after each statement; "vibe lsp snapshot" writes it on demand.
Completion is empty until one of them has written a snapshot.

The workspace is --workspace, else $VIBE_WORKSPACE (set by the TUI and
"vibe query --edit" for the editor they open), else the editor's root folder,
else the nearest folder above the working directory that holds a vibe.yaml.`,
	Args:         cobra.NoArgs,
	RunE:         runLSP,
	SilenceUsage: true,
}

var lspSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Write the catalog snapshot vibe lsp completes from",
	Long: `Connect the workspace's connections and write their schemas, tables, views
and columns (names and types only) to .vibe-local/catalog.json for "vibe lsp".
-c limits it to the named connections.

It takes the workspace briefly, as "vibe query" does. While the TUI or Studio
owns the workspace it skips local DuckDB files (their owner keeps those current)
and reads the other connections. A connection that cannot be connected is
skipped with a warning and keeps what an earlier snapshot knew; the command
fails only when no connection could be snapshotted. With -c, an unknown name is a usage
error (before the hub is opened or the snapshot written), and a named connection
that cannot be connected is warned about, the others are still written, and the
command then exits non-zero. A local file skipped because the workspace is open
elsewhere is not a failure.`,
	Args:         cobra.NoArgs,
	RunE:         runLSPSnapshot,
	SilenceUsage: true,
}

func init() {
	addWorkspaceFlag(lspCmd)
	addWorkspaceFlag(lspSnapshotCmd)
	lspSnapshotCmd.Flags().StringSliceP("connection", "c", nil, "connections to snapshot (default: all)")
	lspCmd.AddCommand(lspSnapshotCmd)
	rootCmd.AddCommand(lspCmd)
}

func runLSP(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	dir := flagString(cmd, "workspace")
	if dir == "" {
		dir = os.Getenv(editor.EnvWorkspace)
	}
	errW := cmd.ErrOrStderr()
	srv := lsp.NewServer(cmd.InOrStdin(), cmd.OutOrStdout(),
		lsp.WithLogger(logger), lsp.WithVersion(version),
		lsp.WithRootHook(func(clientRoot string) *lsp.Source {
			root, err := lspRoot(dir, clientRoot)
			if err != nil {
				fmt.Fprintln(errW, "vibe lsp:", err)
				return nil
			}
			return lsp.NewSource(lsp.SnapshotPath(root))
		}))
	return srv.Run(ctx)
}

// lspRoot picks the workspace: the flag or $VIBE_WORKSPACE, then the editor's
// root folder, then the working directory.
func lspRoot(dir, clientRoot string) (string, error) {
	if dir != "" {
		return hub.ResolveRoot(dir, "")
	}
	if clientRoot != "" {
		if root, err := hub.ResolveRoot(clientRoot, ""); err == nil {
			return root, nil
		}
	}
	return hub.ResolveRoot("", "")
}

func runLSPSnapshot(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	errW := cmd.ErrOrStderr()
	dir := flagString(cmd, "workspace")
	root, err := hub.ResolveRoot(dir, "")
	if err != nil {
		return err
	}
	names, _ := cmd.Flags().GetStringSlice("connection")
	if err := checkSnapshotNames(root, names); err != nil {
		return err
	}
	// A running owner has the local files open for writing: do not fight it,
	// read what is reachable without them. Otherwise take the hub for the
	// duration, as "vibe query" does, so a Studio starting now meets "held".
	mode := hub.ModeReadWrite
	ownerHasLocal := false
	if _, herr := hub.ReadHolder(root); !errors.Is(herr, hub.ErrNotHeld) {
		mode, ownerHasLocal = hub.ModeRemoteOnly, true
		fmt.Fprintln(errW, "note: the workspace is open elsewhere; skipping local DuckDB files (their owner keeps those current)")
	}
	o, err := cliOpen(ctx, dir, mode, false, "vibe lsp snapshot", errW)
	if err != nil {
		return err
	}
	defer o.Close()
	return snapshotWorkspace(ctx, o, root, names, ownerHasLocal, errW, cmd.OutOrStdout())
}

// checkSnapshotNames refuses a -c name the workspace does not define, before
// the hub is opened or the snapshot written. The error names it and lists nothing else.
func checkSnapshotNames(root string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	ws, err := workspace.Open(root, workspace.WithLogger(logger), workspace.WithResolver(cliResolver()))
	if err != nil {
		return fmt.Errorf("failed to open the workspace: %w", err)
	}
	defer ws.Close() //nolint:errcheck
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue // the default connection
		}
		if _, ok := ws.Connection(n); !ok {
			return fmt.Errorf("unknown connection %q (see `vibe conn list`)", n)
		}
	}
	return nil
}

// snapshotWorkspace connects names (nil = every connection, "" = the default
// one) and merges their catalog into the snapshot. A connection that cannot be
// connected is skipped with a warning. With no names it fails only when none
// could be snapshotted; with names it also fails, after writing the others,
// when one of them could not be connected. With ownerHasLocal (the workspace
// is open elsewhere) local files are left to their owner: not dialled, and not
// a failure.
func snapshotWorkspace(ctx context.Context, o *openedWorkspace, root string, names []string, ownerHasLocal bool, errW, out io.Writer) error {
	var failed []string
	targets, left := snapshotTargets(o.Sess, names), 0
	for _, n := range targets {
		if ownerHasLocal && isLocalFile(o, n) {
			left++
			continue
		}
		if err := o.Sess.Connect(ctx, n); err != nil {
			fmt.Fprintf(errW, "warning: skipped %s: %v\n", n, err)
			failed = append(failed, n)
		}
	}
	if left > 0 && left == len(targets) {
		fmt.Fprintln(errW, "nothing to snapshot: every named connection is a local file left to its owner")
		return nil
	}
	n, err := lsp.Snapshot(ctx, o.Sess, root, lsp.WithMetrics(o.WS.Semantics()))
	if err != nil && n > 0 {
		fmt.Fprintln(errW, "warning:", err)
		err = nil
	}
	if err != nil {
		return fmt.Errorf("nothing could be snapshotted: %w", err)
	}
	if n == 0 {
		return errors.New("nothing could be snapshotted: no connection is connected")
	}
	fmt.Fprintf(out, "wrote %s (%d connections)\n", lsp.SnapshotPath(root), n)
	if len(names) > 0 && len(failed) > 0 {
		return fmt.Errorf("could not connect: %s", strings.Join(failed, ", "))
	}
	return nil
}

// isLocalFile reports whether name is a local DuckDB or SQLite file, which
// ModeRemoteOnly registers without attaching.
func isLocalFile(o *openedWorkspace, name string) bool {
	c, ok := o.WS.Connection(name)
	return ok && (c.Type == "duckdb" || c.Type == "sqlite")
}

func snapshotTargets(sess session.Session, names []string) []string {
	if names == nil {
		var all []string
		for _, st := range sess.Connections() {
			all = append(all, st.Name)
		}
		return all
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			n = sess.DefaultCatalog()
		}
		out = append(out, n)
	}
	return out
}
