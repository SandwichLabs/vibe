package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/usersql"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   `export <[conn.]table> | --sql "<one SELECT>"  [-o file]`,
	Short: "Export a table or a query's result to csv, parquet, json or xlsx",
	Long: `Export a table (table, conn.table or conn.schema.table) or the result of a
query (exactly one SELECT) to a file through the workspace's DuckDB session.
The format comes from
the -o file's extension: .csv, .parquet, .json or .xlsx (Excel needs DuckDB's
excel extension, fetched once on first use). An existing file is only replaced
with --force; a symbolic link at the destination is also only used with --force
(and then written through).

Without -o, the rows are printed to standard output as --format csv (default)
or json. --read-only exports without taking ownership of the workspace and
reaches the workspace's connections other than local DuckDB files, which the
owner keeps locked.`,
	Args:         cobra.MaximumNArgs(1),
	RunE:         runExport,
	SilenceUsage: true,
}

func init() {
	exportCmd.Flags().String("sql", "", "export this query's result instead of a table")
	exportCmd.Flags().StringP("output", "o", "", "destination file (.csv, .parquet, .json, .xlsx)")
	exportCmd.Flags().StringP("connection", "c", "", "with --sql: connection whose catalog unqualified names use")
	exportCmd.Flags().String("format", "csv", "format for standard output when -o is not given: csv or json")
	exportCmd.Flags().Bool("force", false, "replace the destination file if it exists")
	exportCmd.Flags().Bool("read-only", false, "read without taking ownership of the workspace")
	addWorkspaceFlag(exportCmd)
	rootCmd.AddCommand(exportCmd)
}

// exportFormats maps a file extension to its DuckDB COPY options.
var exportFormats = map[string]string{
	".csv":     "FORMAT csv, HEADER true",
	".parquet": "FORMAT parquet",
	".json":    "FORMAT json, ARRAY true",
	".xlsx":    "FORMAT xlsx, HEADER true",
}

// exportOpts are runExport's flags.
type exportOpts struct {
	dir, sql, outFile, conn, format string
	force, readOnly                 bool
}

func exportFlags(cmd *cobra.Command) exportOpts {
	var e exportOpts
	e.dir, _ = cmd.Flags().GetString("workspace")
	e.sql, _ = cmd.Flags().GetString("sql")
	e.outFile, _ = cmd.Flags().GetString("output")
	e.conn, _ = cmd.Flags().GetString("connection")
	e.format, _ = cmd.Flags().GetString("format")
	e.force, _ = cmd.Flags().GetBool("force")
	e.readOnly, _ = cmd.Flags().GetBool("read-only")
	return e
}

func runExport(cmd *cobra.Command, args []string) error {
	e := exportFlags(cmd)
	errW := cmd.ErrOrStderr()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	copyOpts, ext, err := e.validate(args)
	if err != nil {
		return err
	}
	o, err := openWorkspace(ctx, e.dir, e.readOnly, true, "vibe export", errW)
	if err != nil {
		return err
	}
	defer o.Close()

	source, conn, err := e.source(ctx, o, args, errW)
	if err != nil {
		return err
	}
	if e.outFile == "" {
		rs, _, err := o.Eng.ExecuteCappedIn(ctx, conn, source, nil, 0)
		if err != nil {
			return err
		}
		return writeResult(cmd.OutOrStdout(), rs, e.format)
	}

	if ext == ".xlsx" {
		if err := session.EnsureExtension(ctx, o.Sess.DB(), "excel", "Exporting to Excel"); err != nil {
			return err
		}
	}
	var qopts []session.QueryOption
	if conn != "" {
		qopts = append(qopts, session.WithCatalog(conn))
	}
	copySQL := session.CopyToSQL(source, e.outFile, copyOpts)
	if _, err := o.Sess.ExecWith(ctx, copySQL, nil, qopts...); err != nil {
		return fmt.Errorf("failed to export: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Exported to %s\n", e.outFile)
	return nil
}

// validate checks the arguments and flags before the workspace is opened, and
// returns the COPY options and the lower-cased extension of the destination.
func (e exportOpts) validate(args []string) (copyOpts, ext string, err error) {
	if len(args) == 1 && isVibeTableArg(args[0]) {
		return "", "", errLegacyFileArg
	}
	switch {
	case e.sql != "" && len(args) == 1:
		return "", "", fmt.Errorf("give a table or --sql, not both")
	case e.sql == "" && len(args) == 0:
		return "", "", fmt.Errorf("give a table to export (conn.table) or --sql \"<query>\"")
	}
	if e.sql == "" && e.conn != "" {
		return "", "", fmt.Errorf("-c only applies with --sql; name the connection in the table (conn.table)")
	}
	if e.sql != "" {
		if err := usersql.Check(e.sql, e.readOnly); err != nil {
			return "", "", err
		}
	}
	if e.outFile == "" {
		if err := validateFormat(e.format); err != nil || e.format == "table" {
			return "", "", fmt.Errorf("unsupported --format %q for export: use csv or json", e.format)
		}
		return "", "", nil
	}
	ext = strings.ToLower(filepath.Ext(e.outFile))
	copyOpts, ok := exportFormats[ext]
	if !ok {
		return "", "", fmt.Errorf("unsupported file type %q: use .csv, .parquet, .json or .xlsx", filepath.Ext(e.outFile))
	}
	// Lstat, so a dangling symlink counts as existing: writing would follow
	// it to somewhere else. There is a small window between this check and
	// DuckDB opening the file; the destination is chosen by the person at
	// the keyboard, so this guards against mistakes, not an attacker.
	if _, err := os.Lstat(e.outFile); err == nil && !e.force {
		return "", "", fmt.Errorf("%s already exists (or is a symbolic link); use --force to replace it", e.outFile)
	}
	return copyOpts, ext, nil
}

// source returns the SELECT to export and the connection whose catalog its
// unqualified names use ("" for a table, which is named in full).
func (e exportOpts) source(ctx context.Context, o *openedWorkspace, args []string, errW io.Writer) (source, conn string, err error) {
	if e.sql == "" {
		ref, err := o.resolveTarget(ctx, args[0], errW)
		if err != nil {
			return "", "", err
		}
		return ref.SelectAllSQL(), "", nil
	}
	catalog := e.conn
	if catalog == "" {
		catalog = o.WS.DefaultConnection()
	} else if _, ok := o.knownConnection(catalog); !ok {
		return "", "", fmt.Errorf("unknown connection %q; this workspace has: %s", catalog, connectionNames(o))
	}
	if err := o.requireConnection(catalog, errW); err != nil {
		return "", "", err
	}
	// The text goes inside COPY (...), so it must be exactly one SELECT:
	// nothing else can be smuggled in after a closing parenthesis.
	if err := usersql.RequireSelect(ctx, o.Sess.DB(), e.sql); err != nil {
		return "", "", err
	}
	return e.sql, e.conn, nil
}
