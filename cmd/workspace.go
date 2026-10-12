package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/templates"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Create and inspect workspaces",
	Long: `A workspace is a folder (optionally a git repository) holding notebooks,
saved queries and skills as plain files, plus a vibe.yaml that marks it and
lists its connections. Data stays where it lives; a workspace only describes
how to reach it. See docs/reference/workspaces.md.`,
}

const initLong = `Turn a folder into a Vibe workspace: a vibe.yaml marker, notebooks/,
queries/, skills/ and data/ folders, a .gitignore and .gitattributes (merged
into any that exist), and a default "local" DuckDB connection at
data/local.duckdb.

The folder is created when it does not exist, defaults to the current
directory, and may not already be (or sit inside) a workspace. With --git it
also runs "git init" when git is on your PATH. --template seeds the local
database with a starter template's sample tables and its notebook; the
available templates are: %s.`

func newWorkspaceInitCmd(use, short string) *cobra.Command {
	c := &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         fmt.Sprintf(initLong, strings.Join(templates.List(), ", ")),
		Args:         cobra.MaximumNArgs(1),
		RunE:         runWorkspaceInit,
		SilenceUsage: true,
	}
	c.Flags().String("name", "", "workspace name (default: the folder name)")
	c.Flags().Bool("git", false, "run `git init` in the folder when git is installed")
	c.Flags().String("template", "", "seed the local database and a notebook from a starter template ("+strings.Join(templates.List(), ", ")+")")
	return c
}

var workspaceInitCmd = newWorkspaceInitCmd("init [dir]", "Create a workspace in a folder")

// initCmd keeps the old spelling working as an alias of `workspace init`.
var initCmd = newWorkspaceInitCmd("init [dir]", "Create a workspace (alias for `workspace init`)")

var workspaceInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show a workspace: connections, content counts, agent policy, hub owner",
	Long: `Show the workspace's root, id and name; its connections (scope, status,
read-only, and a table count for each one that is attached); how many
notebooks, saved queries and skills it holds; the effective agent policy; and
which process currently owns its DuckDB hub, if any.

info never takes the hub and never creates a data file: it reads connections
read-only, so a connection another process has open for writing shows as
locked rather than counted.`,
	Args:         cobra.NoArgs,
	RunE:         runWorkspaceInfo,
	SilenceUsage: true,
}

func init() {
	workspaceInfoCmd.Flags().Bool("json", false, "output as JSON")
	addWorkspaceFlag(workspaceInfoCmd)
	workspaceCmd.AddCommand(workspaceInitCmd, workspaceInfoCmd)
	rootCmd.AddCommand(workspaceCmd, initCmd)
}

func runWorkspaceInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	if isVibeFile(dir) {
		return errLegacyFileArg
	}
	name, _ := cmd.Flags().GetString("name")
	git, _ := cmd.Flags().GetBool("git")
	tmplName, _ := cmd.Flags().GetString("template")
	out := cmd.OutOrStdout()

	var tmpl templates.Template
	if tmplName != "" {
		t, ok := templates.Get(tmplName)
		if !ok {
			return fmt.Errorf("unknown template %q; available templates: %s", tmplName, strings.Join(templates.List(), ", "))
		}
		tmpl = t
	}

	res, err := workspace.Init(dir, workspace.WithName(name), workspace.WithGit(git), workspace.WithInitLogger(logger))
	if err != nil {
		return err
	}
	ws := res.Workspace
	defer ws.Close() //nolint:errcheck

	if tmplName != "" {
		if err := seedWorkspaceTemplate(cmd.Context(), ws, tmpl); err != nil {
			return fmt.Errorf("the workspace was created at %s, but seeding template %q failed: %w", ws.Root(), tmplName, err)
		}
	}

	fmt.Fprintf(out, "Created workspace %q at %s\n", ws.Name(), ws.Root())
	if res.GitInitialized {
		fmt.Fprintln(out, "Initialized a git repository.")
	}
	for _, n := range res.Notes {
		fmt.Fprintln(out, "Note:", n)
	}
	if tmplName != "" {
		fmt.Fprintf(out, "Seeded template %q: %d table(s) in data/local.duckdb and 1 notebook.\n", tmplName, len(tmpl.Tables))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next steps:")
	if rel := displayDir(dir); rel != "." {
		fmt.Fprintf(out, "  cd %s\n", rel)
	}
	fmt.Fprintln(out, "  vibe workspace info                         see connections and counts")
	fmt.Fprintln(out, `  vibe query "SELECT 42 AS answer"            run SQL against the default connection`)
	fmt.Fprintln(out, "  vibe import data.csv --into my_table        load a CSV or Excel file")
	fmt.Fprintln(out, "  vibe studio                                 open the terminal UI (`--gui` opens the desktop app)")
	fmt.Fprintln(out, "  vibe mcp serve                              let Claude and other MCP clients use it")
	return nil
}

func displayDir(dir string) string {
	if dir == "" {
		return "."
	}
	return filepath.Clean(dir)
}

// seedWorkspaceTemplate writes a template's tables into the workspace's
// default connection and its notebook into notebooks/. It takes the hub for
// the duration, like any other writer.
func seedWorkspaceTemplate(ctx context.Context, ws workspace.Workspace, tmpl templates.Template) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Open the workspace again through the one open path (the init handle only
	// supplies the path and is closed by the caller). No .env question here:
	// a brand-new workspace has no .env to ask about.
	o, err := hub.Open(ctx, ws.Root(), hub.OpenOptions{
		App: "vibe workspace init", Logger: logger, Resolver: cliResolver(), Redactor: cliRedactor,
	})
	if err != nil {
		var oe *hub.OpenError
		if errors.As(err, &oe) && oe.Step == hub.StepHub {
			return heldMessage(ws.Root(), oe.Err, false)
		}
		return err
	}
	defer o.Close()
	sess, issues := o.Sess, o.Issues
	for _, i := range issues {
		if i.Connection == o.WS.DefaultConnection() {
			return fmt.Errorf("the default connection is not available: %s", i.Message)
		}
	}
	err = templates.SeedTables(ctx, func(ctx context.Context, q string) error {
		_, err := sess.Exec(ctx, q)
		return err
	}, tmpl)
	if err != nil {
		return err
	}
	if _, _, err := o.WS.Notebooks().Insert(ctx, tmpl.Book); err != nil {
		return fmt.Errorf("failed to write the starter notebook: %w", err)
	}
	return nil
}

// infoConnection is one connection in `workspace info`.
type infoConnection struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Scope    string `json:"scope"`
	ReadOnly bool   `json:"read_only"`
	Default  bool   `json:"default,omitempty"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	Tables   *int   `json:"tables,omitempty"`
}

type infoReport struct {
	Root        string           `json:"root"`
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Connections []infoConnection `json:"connections"`
	Notebooks   int              `json:"notebooks"`
	Queries     int              `json:"queries"`
	Skills      int              `json:"skills"`
	Agent       struct {
		Sandbox         string `json:"sandbox"`
		AskBeforeWrites bool   `json:"ask_before_writes"`
	} `json:"agent"`
	// DuckDB is the engine version; Extensions are the downloaded extensions
	// installed in Vibe's own extension directory (never ~/.duckdb).
	DuckDB     string   `json:"duckdb,omitempty"`
	Extensions []string `json:"extensions"`
	HubHolder  string   `json:"hub_holder"`
	Warnings   []string `json:"warnings,omitempty"`
}

func runWorkspaceInfo(cmd *cobra.Command, _ []string) error {
	dir, _ := cmd.Flags().GetString("workspace")
	asJSON, _ := cmd.Flags().GetBool("json")
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	o, err := openWorkspace(ctx, dir, true, false, "vibe workspace info", io.Discard)
	if err != nil {
		return err
	}
	defer o.Close()
	rep := buildInfo(ctx, o)
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printInfo(out, rep)
	return nil
}

func buildInfo(ctx context.Context, o *openedWorkspace) *infoReport {
	ws := o.WS
	rep := &infoReport{Root: ws.Root(), ID: ws.ID(), Name: ws.Name(), Warnings: ws.Warnings()}

	for _, st := range o.States() {
		ic := infoConnection{
			Name: st.Name, Type: st.Type, Scope: string(st.Scope), ReadOnly: st.ReadOnly,
			Default: st.Name == ws.DefaultConnection(), Status: string(st.Status), Message: st.Detail,
		}
		switch {
		case st.Locked():
			ic.Message = lockedMessage(ws.Root())
		case st.Status == session.StatusOK:
			if n, err := countTables(ctx, o.Sess, st.Name); err == nil {
				ic.Tables = &n
			}
		}
		rep.Connections = append(rep.Connections, ic)
	}

	if l, err := ws.Notebooks().List(ctx); err == nil {
		rep.Notebooks = len(l)
	}
	if l, err := ws.Queries().List(ctx); err == nil {
		rep.Queries = len(l)
	}
	if l, err := ws.Skills().List(ctx); err == nil {
		rep.Skills = len(l)
	}

	q := session.FromSession(o.Sess)
	rep.DuckDB, rep.Extensions = session.EngineVersion(ctx, q), session.InstalledExtensions(ctx, q)

	pol := agents.PolicyFrom(ws)
	rep.Agent.Sandbox, rep.Agent.AskBeforeWrites = string(pol.Mode), pol.AskBeforeWrites

	holder, err := hub.ReadHolder(ws.Root())
	switch {
	case err == nil:
		rep.HubHolder = holder.String()
	case errors.Is(err, hub.ErrNotHeld):
	default:
		rep.HubHolder = "unknown (its lock file cannot be trusted)"
	}
	return rep
}

// countTables counts the tables and views of every schema of one catalog.
func countTables(ctx context.Context, sess session.Session, catalog string) (int, error) {
	ts, err := session.AllTables(ctx, sess, catalog)
	return len(ts), err
}

func printInfo(out io.Writer, r *infoReport) {
	fmt.Fprintf(out, "Workspace:   %s\n", r.Name)
	fmt.Fprintf(out, "Root:        %s\n", r.Root)
	fmt.Fprintf(out, "ID:          %s\n", r.ID)
	fmt.Fprintln(out, "Connections:")
	if len(r.Connections) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	for _, c := range r.Connections {
		flags := []string{c.Type, c.Scope}
		if c.ReadOnly {
			flags = append(flags, "read-only")
		} else {
			flags = append(flags, "read-write")
		}
		if c.Default {
			flags = append(flags, "default")
		}
		line := fmt.Sprintf("  %-14s %s  [%s]", c.Name, c.Status, strings.Join(flags, ", "))
		if c.Tables != nil {
			line += fmt.Sprintf("  %d table(s)", *c.Tables)
		}
		fmt.Fprintln(out, line)
		if c.Message != "" {
			fmt.Fprintf(out, "  %-14s   %s\n", "", c.Message)
		}
	}
	fmt.Fprintf(out, "Notebooks:   %d\n", r.Notebooks)
	fmt.Fprintf(out, "Queries:     %d\n", r.Queries)
	fmt.Fprintf(out, "Skills:      %d\n", r.Skills)
	ask := "on"
	if !r.Agent.AskBeforeWrites {
		ask = "off"
	}
	fmt.Fprintf(out, "Agent:       sandbox %s, ask before writes %s\n", r.Agent.Sandbox, ask)
	if r.DuckDB != "" {
		ext := "none installed (they download on consent)"
		if len(r.Extensions) > 0 {
			ext = strings.Join(r.Extensions, ", ")
		}
		fmt.Fprintf(out, "DuckDB:      %s; extensions: %s\n", r.DuckDB, ext)
	}
	holder := r.HubHolder
	if holder == "" {
		holder = "none (nothing has the database open)"
	}
	fmt.Fprintf(out, "Hub owner:   %s\n", holder)
	for _, w := range r.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
}
