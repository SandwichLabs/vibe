package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/assay/internal/bundle"
	"github.com/SandwichLabs/assay/internal/filestore"
)

// bundlePassphraseEnv is the script-friendly way to give a bundle passphrase.
// An environment variable is visible to other processes of the same user, so
// the prompt is the safer path; this exists for CI and shell scripts.
const bundlePassphraseEnv = "ASSAY_BUNDLE_PASSPHRASE"

var exportBundleCmd = &cobra.Command{
	Use:   "bundle [-o file.assay]",
	Short: "Export the workspace's notebooks, queries, skills and data artifacts as a .assay bundle",
	Long: `Write a .assay bundle: a zip of notebooks, queries, skills and data artifacts (and, with
--with-connections, connection definitions). A bundle holds no data and no
credentials; connection definitions are stripped of sign-in methods and of paths
outside the workspace.

With no --notebooks, --queries, --skills or --artifacts the bundle takes everything. Name
some of them (comma separated ids) to take only those.

--encrypt wraps the whole bundle in a passphrase envelope (age, scrypt). The
passphrase is asked for twice on a terminal. For scripts, set ` + bundlePassphraseEnv + `
(less safe: other processes of yours can read the environment).`,
	Args:         cobra.NoArgs,
	RunE:         runExportBundle,
	SilenceUsage: true,
}

var importBundleCmd = &cobra.Command{
	Use:   "bundle <file.assay>",
	Short: "Import a .assay bundle into the workspace",
	Long: `Import a .assay bundle into the current workspace. Every entry is verified
before anything is written. An item that already exists is renamed (-2, -3, ...)
unless --on-conflict says overwrite or skip.

Connection definitions in the bundle are only added with --with-connections
(all of them, or a comma separated list of names). They go to "This computer"
for this workspace, never to the committed assay.yaml, and arrive without
credentials: add them with "assay conn credentials set <name>".

An encrypted bundle asks for its passphrase on a terminal, or reads
` + bundlePassphraseEnv + `.`,
	Args:         cobra.ExactArgs(1),
	RunE:         func(cmd *cobra.Command, args []string) error { return runImportBundle(cmd, args[0]) },
	SilenceUsage: true,
}

var exportNotebookCmd = &cobra.Command{
	Use:   "notebook <id> [-o file.asnb]",
	Short: "Export one notebook as a .asnb file",
	Long: `Write one notebook's file (notebooks/<id>.asnb) to -o (default <id>.asnb in the
current directory). It is the same bytes a bundle holds.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runExportNotebook,
	SilenceUsage: true,
}

var importNotebookCmd = &cobra.Command{
	Use:   "notebook <file.asnb>",
	Short: "Import one notebook file into the workspace",
	Long: `Import a .asnb file (or Studio's single-notebook export) through the same checks
and conflict handling as a bundle. The file name, without its extension, is the
notebook's id. An existing notebook is renamed (-2, -3, ...) unless
--on-conflict says overwrite or skip.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runImportNotebook,
	SilenceUsage: true,
}

func runExportNotebook(cmd *cobra.Command, args []string) error {
	out, _ := cmd.Flags().GetString("output")
	force, _ := cmd.Flags().GetBool("force")
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	id := args[0]
	if !filestore.ValidID(id) {
		return fmt.Errorf("%q is not a notebook id", id)
	}
	data, _, err := filestore.Read(ws.Root(), "notebooks/"+id+".asnb")
	if err != nil {
		return fmt.Errorf("failed to read notebook %q: %w", id, err)
	}
	// The exported file is held to the rule an import applies to it.
	if _, _, err := bundle.CheckNotebook(data); err != nil {
		return fmt.Errorf("notebook %q cannot be exported: %w", id, err)
	}
	if out == "" {
		out = id + ".asnb"
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	if _, err := os.Lstat(out); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to replace it", out)
	}
	f, err := os.OpenFile(out, flags, 0o644) //nolint:gosec // a file meant to be shared
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", out, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write %s: %w", out, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", out, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Exported notebook %s to %s.\n", id, out)
	return nil
}

func runImportNotebook(cmd *cobra.Command, args []string) error {
	mode, _ := cmd.Flags().GetString("on-conflict")
	action := bundle.Action(mode)
	switch action {
	case bundle.ActionRename, bundle.ActionOverwrite, bundle.ActionSkip:
	default:
		return fmt.Errorf("invalid --on-conflict %q: use rename, overwrite or skip", mode)
	}
	data, err := bundle.ReadFile(args[0])
	if err != nil {
		return err
	}
	pkg, err := bundle.NotebookPackage(args[0], data)
	if err != nil {
		return err
	}
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	pkg.Preview(cmdCtx(cmd), ws) // note the revision an overwrite would replace
	res, err := pkg.Import(cmdCtx(cmd), ws, bundle.ImportOptions{OnConflict: action})
	if err != nil {
		return err
	}
	printImportResult(cmd.OutOrStdout(), res)
	if res.Failed() > 0 {
		return fmt.Errorf("the notebook could not be imported")
	}
	return nil
}

func addBundleImportFlags(c *cobra.Command) {
	c.Flags().String("on-conflict", "rename", "what to do with an item that already exists: rename, overwrite or skip")
	c.Flags().StringSlice("with-connections", nil, `import connection definitions into "This computer": "all" or a list of names`)
	addWorkspaceFlag(c)
}

func init() {
	f := exportBundleCmd.Flags()
	f.StringP("output", "o", "", "destination file (default: <workspace name>.assay in the current directory)")
	f.StringSlice("notebooks", nil, "notebook ids to include")
	f.StringSlice("queries", nil, "query ids to include")
	f.StringSlice("skills", nil, "skill names to include")
	f.StringSlice("artifacts", nil, "data artifact names to include")
	f.StringSlice("semantics", nil, `semantic definitions to include, as "metrics/net_revenue", "entities/customer", "joins/orders_customers" or "glossary"`)
	f.Bool("with-connections", false, "include the committed (team) connection definitions")
	f.StringSlice("with-local-connections", nil, `include these "This computer" connection definitions`)
	f.Bool("encrypt", false, "wrap the bundle in a passphrase envelope")
	f.Bool("force", false, "replace the destination file if it exists")
	addWorkspaceFlag(exportBundleCmd)
	exportCmd.AddCommand(exportBundleCmd)

	exportNotebookCmd.Flags().StringP("output", "o", "", "destination file (default: <id>.asnb)")
	exportNotebookCmd.Flags().Bool("force", false, "replace the destination file if it exists")
	addWorkspaceFlag(exportNotebookCmd)
	exportCmd.AddCommand(exportNotebookCmd)
	importNotebookCmd.Flags().String("on-conflict", "rename", "what to do if the notebook already exists: rename, overwrite or skip")
	addWorkspaceFlag(importNotebookCmd)
	importCmd.AddCommand(importNotebookCmd)

	addBundleImportFlags(importBundleCmd)
	importBundleCmd.Flags().Bool("dry-run", false, "show what would happen; write nothing")
	importCmd.AddCommand(importBundleCmd)
	// `assay import <file.assay>` routes here too.
	importCmd.Flags().String("on-conflict", "rename", "with a .assay bundle: rename, overwrite or skip an item that already exists")
	importCmd.Flags().StringSlice("with-connections", nil, `with a .assay bundle: import connection definitions into "This computer": "all" or names`)
}

// readBundlePassphrase gets a passphrase: the environment variable, else a
// prompt on a terminal (twice when confirm is set).
func readBundlePassphrase(cmd *cobra.Command, confirm bool) (string, error) {
	if v := os.Getenv(bundlePassphraseEnv); v != "" {
		cliRedactor.Add(v)
		return v, nil
	}
	if !stdinIsTerminal() {
		return "", fmt.Errorf("a passphrase is needed: run on a terminal to be asked, or set %s", bundlePassphraseEnv)
	}
	lr := newLineReader(cmd.InOrStdin())
	errW := cmd.ErrOrStderr()
	p1, err := lr.line(errW, "Passphrase: ", true)
	if err != nil {
		return "", err
	}
	if p1 == "" {
		return "", errors.New("the passphrase is empty")
	}
	cliRedactor.Add(p1)
	if confirm {
		p2, err := lr.line(errW, "Repeat passphrase: ", true)
		if err != nil {
			return "", err
		}
		if p1 != p2 {
			return "", errors.New("the two passphrases do not match")
		}
	}
	return p1, nil
}

func runExportBundle(cmd *cobra.Command, _ []string) error {
	f := cmd.Flags()
	nbs, _ := f.GetStringSlice("notebooks")
	qs, _ := f.GetStringSlice("queries")
	sks, _ := f.GetStringSlice("skills")
	withConns, _ := f.GetBool("with-connections")
	localConns, _ := f.GetStringSlice("with-local-connections")
	encrypt, _ := f.GetBool("encrypt")
	force, _ := f.GetBool("force")
	out, _ := f.GetString("output")

	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()

	if out == "" {
		out = filestore.Slug(ws.Name()) + ".assay"
	}
	if _, err := os.Lstat(out); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to replace it", out)
	}
	arts, _ := f.GetStringSlice("artifacts")
	sems, _ := f.GetStringSlice("semantics")
	sel := bundle.Selection{Notebooks: nbs, Queries: qs, Skills: sks, Artifacts: arts, Semantics: sems, SharedConnections: withConns, LocalConnections: localConns}
	sel.All = len(nbs)+len(qs)+len(sks)+len(arts)+len(sems) == 0
	var pass string
	if encrypt {
		if pass, err = readBundlePassphrase(cmd, true); err != nil {
			return err
		}
	}
	res, err := bundle.Export(cmdCtx(cmd), ws, bundle.ExportOptions{Selection: sel, Passphrase: pass, AppVersion: "assay " + version})
	if err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(out, flags, 0o644) //nolint:gosec // a file the person asked for, meant to be shared
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", out, err)
	}
	if _, err := file.Write(res.Data); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to write %s: %w", out, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", out, err)
	}
	w := cmd.OutOrStdout()
	counts := exportCounts(res.Manifest.Contents)
	fmt.Fprintf(w, "Exported %d notebooks, %d queries, %d skills, %d data artifacts, %d semantic definitions to %s", counts[bundle.KindNotebook], counts[bundle.KindQuery], counts[bundle.KindSkill], counts[bundle.KindArtifact], counts[bundle.KindSemantics], out)
	if counts[bundle.KindConnections] > 0 {
		fmt.Fprint(w, " (with connection definitions)")
	}
	if res.Encrypted {
		fmt.Fprint(w, ", encrypted")
	}
	fmt.Fprintln(w, ".")
	fmt.Fprintln(w, "The bundle contains no data and no credentials.")
	for _, m := range res.Manifest.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m)
	}
	return nil
}

func runImportBundle(cmd *cobra.Command, file string) error {
	f := cmd.Flags()
	mode, _ := f.GetString("on-conflict")
	dry, _ := f.GetBool("dry-run")
	withConns, _ := f.GetStringSlice("with-connections")
	action := bundle.Action(mode)
	switch action {
	case bundle.ActionRename, bundle.ActionOverwrite, bundle.ActionSkip:
	default:
		return fmt.Errorf("invalid --on-conflict %q: use rename, overwrite or skip", mode)
	}

	data, err := bundle.ReadFile(file)
	if err != nil {
		return err
	}
	info, err := bundle.Peek(data)
	if err != nil {
		return err
	}
	var pass string
	if info.Encrypted {
		if pass, err = readBundlePassphrase(cmd, false); err != nil {
			return err
		}
	}
	pkg, err := bundle.Open(data, pass)
	if err != nil {
		return err
	}
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()

	opts := bundle.ImportOptions{OnConflict: action, DryRun: dry}
	pv := pkg.Preview(cmdCtx(cmd), ws)
	if len(withConns) == 1 && withConns[0] == "all" {
		withConns = nil
		for _, c := range pv.Connections {
			withConns = append(withConns, c.Name)
		}
	}
	opts.IncludeConnections = withConns
	res, err := pkg.Import(cmdCtx(cmd), ws, opts)
	if err != nil {
		return err
	}
	printImportResult(cmd.OutOrStdout(), res)
	errW := cmd.ErrOrStderr()
	for _, w := range res.Warnings {
		fmt.Fprintln(errW, "warning:", w)
	}
	for _, n := range pv.MakerNotes {
		fmt.Fprintln(errW, "note from the person who made this bundle:", n)
	}
	// Everything a definition sets is shown, imported or not.
	for _, c := range pv.Connections {
		fmt.Fprintf(errW, "connection %s (%s)%s\n", c.Name, c.Type, map[bool]string{true: ", asked to be writable (imported read-only)", false: ""}[c.AskedWritable])
		for _, d := range c.Details {
			fmt.Fprintf(errW, "    %s\n", d)
		}
		for _, r := range c.Risks {
			fmt.Fprintf(errW, "    WARNING: %s\n", r)
		}
		if c.Conflict {
			fmt.Fprintf(errW, "    a %s connection named %s exists; this one would be added as %s\n", c.ConflictScope, c.Name, c.RenamedTo)
		}
	}
	if len(pv.Connections) > 0 && len(withConns) == 0 {
		names := make([]string, 0, len(pv.Connections))
		for _, c := range pv.Connections {
			names = append(names, c.Name)
		}
		sort.Strings(names)
		fmt.Fprintf(errW, "note: the bundle has connection definitions (%s); add them with --with-connections. They arrive read-only and without credentials.\n", strings.Join(names, ", "))
	}
	if n := res.Failed(); n > 0 {
		return fmt.Errorf("%d item(s) could not be imported", n)
	}
	return nil
}

func printImportResult(w io.Writer, res bundle.Result) {
	if res.DryRun {
		fmt.Fprintln(w, "Dry run: nothing was written.")
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, o := range res.Outcomes {
		line := o.Result
		if o.FinalID != "" && o.FinalID != o.ID {
			line += " as " + o.FinalID
		}
		if o.Error != "" {
			line += ": " + o.Error
		}
		fmt.Fprintf(tw, "%s\t%s\n", o.Key, line)
	}
	_ = tw.Flush()
	if len(res.Outcomes) == 0 {
		fmt.Fprintln(w, "Nothing to import.")
	}
}

// exportCounts counts what an export holds by item, not by file: a data artifact
// is one item however many files it has (a manifest and a view, or a whole HTML
// page).
func exportCounts(contents []bundle.Entry) map[bundle.ItemKind]int {
	seen := map[string]bool{}
	counts := map[bundle.ItemKind]int{}
	for _, e := range contents {
		key := string(e.Kind) + "/" + e.ID
		if !seen[key] {
			counts[e.Kind]++
		}
		seen[key] = true
	}
	return counts
}
