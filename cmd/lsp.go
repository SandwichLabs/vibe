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
fails only when no connection could be snapshotted.`,
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
	// A running owner has the local files open for writing: do not fight it,
	// read what is reachable without them. Otherwise take the hub for the
	// duration, as "vibe query" does, so a Studio starting now meets "held".
	mode := hub.ModeReadWrite
	if _, herr := hub.ReadHolder(root); !errors.Is(herr, hub.ErrNotHeld) {
		mode = hub.ModeRemoteOnly
		fmt.Fprintln(errW, "note: the workspace is open elsewhere; skipping local DuckDB files (their owner keeps those current)")
	}
	o, err := cliOpen(ctx, dir, mode, false, "vibe lsp snapshot", errW)
	if err != nil {
		return err
	}
	defer o.Close()
	names, _ := cmd.Flags().GetStringSlice("connection")
	return snapshotWorkspace(ctx, o, root, names, errW, cmd.OutOrStdout())
}

// snapshotWorkspace connects names (nil = every connection, "" = the default
// one) and merges their catalog into the snapshot. A connection that cannot be
// connected is skipped with a warning; it fails only when none could be
// snapshotted.
func snapshotWorkspace(ctx context.Context, o *openedWorkspace, root string, names []string, errW, out io.Writer) error {
	for _, n := range snapshotTargets(o.Sess, names) {
		if err := o.Sess.Connect(ctx, n); err != nil {
			fmt.Fprintf(errW, "warning: skipped %s: %v\n", n, err)
		}
	}
	n, err := lsp.Snapshot(ctx, o.Sess, root)
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
	return nil
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
