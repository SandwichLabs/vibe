package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/session"
	"github.com/sandwich-labs/vibe-app/internal/usersql"
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

func runExport(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("workspace")
	sqlFlag, _ := cmd.Flags().GetString("sql")
	outFile, _ := cmd.Flags().GetString("output")
	conn, _ := cmd.Flags().GetString("connection")
	format, _ := cmd.Flags().GetString("format")
	force, _ := cmd.Flags().GetBool("force")
	readOnly, _ := cmd.Flags().GetBool("read-only")
	errW := cmd.ErrOrStderr()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if len(args) == 1 && isVibeTableArg(args[0]) {
		return errLegacyFileArg
	}
	switch {
	case sqlFlag != "" && len(args) == 1:
		return fmt.Errorf("give a table or --sql, not both")
	case sqlFlag == "" && len(args) == 0:
		return fmt.Errorf("give a table to export (conn.table) or --sql \"<query>\"")
	}
	if sqlFlag == "" && conn != "" {
		return fmt.Errorf("-c only applies with --sql; name the connection in the table (conn.table)")
	}
	if sqlFlag != "" {
		if err := usersql.Check(sqlFlag, readOnly); err != nil {
			return err
		}
	}
	var copyOpts, ext string
	if outFile != "" {
		ext = strings.ToLower(filepath.Ext(outFile))
		var ok bool
		if copyOpts, ok = exportFormats[ext]; !ok {
			return fmt.Errorf("unsupported file type %q: use .csv, .parquet, .json or .xlsx", filepath.Ext(outFile))
		}
		// Lstat, so a dangling symlink counts as existing: writing would follow
		// it to somewhere else. There is a small window between this check and
		// DuckDB opening the file; the destination is chosen by the person at
		// the keyboard, so this guards against mistakes, not an attacker.
		if _, err := os.Lstat(outFile); err == nil && !force {
			return fmt.Errorf("%s already exists (or is a symbolic link); use --force to replace it", outFile)
		}
	} else if err := validateFormat(format); err != nil || format == "table" {
		return fmt.Errorf("unsupported --format %q for export: use csv or json", format)
	}

	o, err := openWorkspace(ctx, dir, readOnly, true, "vibe export", errW)
	if err != nil {
		return err
	}
	defer o.Close()

	source := sqlFlag
	if sqlFlag == "" {
		ref, err := o.resolveTarget(ctx, args[0], errW)
		if err != nil {
			return err
		}
		source = "SELECT * FROM " + ref.SQL()
		conn = ""
	} else {
		catalog := conn
		if catalog == "" {
			catalog = o.WS.DefaultConnection()
		} else if _, ok := o.knownConnection(catalog); !ok {
			return fmt.Errorf("unknown connection %q; this workspace has: %s", catalog, connectionNames(o))
		}
		if err := o.requireConnection(catalog, errW); err != nil {
			return err
		}
		// The text goes inside COPY (...), so it must be exactly one SELECT:
		// nothing else can be smuggled in after a closing parenthesis.
		if err := usersql.RequireSelect(ctx, o.Sess.DB(), sqlFlag); err != nil {
			return err
		}
	}

	if outFile == "" {
		rs, _, err := o.Eng.ExecuteCappedIn(ctx, conn, source, nil, 0)
		if err != nil {
			return err
		}
		return writeResult(cmd.OutOrStdout(), rs, format)
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
	copySQL := fmt.Sprintf("COPY (%s\n) TO %s WITH (%s)",
		strings.TrimRight(source, " \t\r\n;"), session.QuoteLiteral(outFile), copyOpts)
	if _, err := o.Sess.ExecWith(ctx, copySQL, nil, qopts...); err != nil {
		return fmt.Errorf("failed to export: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Exported to %s\n", outFile)
	return nil
}
