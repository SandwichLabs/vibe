package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/vibe/internal/importer"
	"github.com/SandwichLabs/vibe/internal/skills"
)

var importCmd = &cobra.Command{
	Use:   "import <file> [--into [conn.]table]",
	Short: "Import a CSV, TSV or Excel file into a connection's table",
	Long: `Import a CSV/TSV or Excel (.xlsx) file into a table of a writable connection,
through the same workspace session and the same importer as Studio's Import
wizard: full-file type inference, a primary key, and a lossless pre-flight
before anything is written. Every import is one transaction.

--into names the target as table, conn.table or conn.schema.table. The
connection defaults to the workspace's default connection and must be writable
(read_only: false). Without --into the table is named after the file. Parquet
is not an import format (query it in place with read_parquet('file.parquet')
or attach it as a connection).

If the table does not exist it is created (--mode defaults to create). If it
exists, --mode is required: upsert (update matching rows by key), replace
(drop and recreate) or append (add rows).

The command takes ownership of the workspace's database for its duration; if
Vibe Studio or "vibe mcp serve" has the workspace open it says so and stops.
--dry-run writes no table, but it still takes ownership and, on a fresh
workspace, creates the empty data/local.duckdb file.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runImport,
	SilenceUsage: true,
}

func runImport(cmd *cobra.Command, args []string) error {
	file := args[0]
	if isVibeFile(file) {
		// A .vibe argument is a bundle (#118); an older container is refused
		// by the bundle reader with a plain message.
		return runImportBundle(cmd, file)
	}
	dir, _ := cmd.Flags().GetString("workspace")
	into, _ := cmd.Flags().GetString("into")
	sheet, _ := cmd.Flags().GetString("sheet")
	noHeader, _ := cmd.Flags().GetBool("no-header")
	modeFlag, _ := cmd.Flags().GetString("mode")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	typeFlags, _ := cmd.Flags().GetStringArray("type")
	errW := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	types, err := parseImportTypeFlags(typeFlags)
	if err != nil {
		return err
	}
	switch importer.Mode(modeFlag) {
	case "", importer.ModeCreate, importer.ModeReplace, importer.ModeAppend, importer.ModeUpsert:
	default:
		return fmt.Errorf("invalid --mode %q: use create, replace, append or upsert", modeFlag)
	}
	if into == "" {
		into = importer.NormalizeName(tableNameFromCSVPath(file))
	}

	o, err := openWorkspace(ctx, dir, false, false, "vibe import", errW)
	if err != nil {
		return err
	}
	defer o.Close()

	ref, err := o.resolveTarget(ctx, into, errW)
	if err != nil {
		return err
	}
	if c, ok := o.knownConnection(ref.Catalog); ok && c.IsReadOnly() {
		return fmt.Errorf("connection %s is read-only, so nothing can be imported into it; set `read_only: false` on it in vibe.yaml or your connections file", ref.Catalog)
	}

	src := importer.Source{Path: file, Sheet: sheet, HasHeader: !noHeader}
	key, err := resolveImportKey(ctx, cmd, o, src)
	if err != nil {
		return err
	}
	sum, err := importer.Import(ctx, o.Sess.DB(), importer.Options{
		Source: src,
		Table:  ref.String(),
		Mode:   importer.Mode(modeFlag),
		Key:    key,
		Types:  types,
		DryRun: dryRun,
	})
	if err != nil {
		return err
	}
	if !sum.OK {
		printImportProblems(out, sum)
		return fmt.Errorf("import into %q failed", into)
	}
	if !dryRun {
		o.Sess.InvalidateSchema(ref.Catalog)
		// sum.Table, not into: for append/upsert/replace the importer returns
		// the catalog's own spelling of an existing table, which the skill
		// must be written under.
		if err := skills.WriteImportSkill(o.WS.Skills(), o.Sess, sum.Table); err != nil {
			logger.Warn("failed to write import skill", "table", sum.Table, "error", err)
		}
	}
	fmt.Fprintln(out, importSummaryLine(sum, dryRun))
	return nil
}

// resolveImportKey turns --key (auto|none|<column>, or unset) into an
// importer.Options.Key value. When --key was not given it inspects the file and
// uses the suggested key, the same default the Studio wizard pre-selects.
// Ignored for append/upsert, where the table's own key is used.
func resolveImportKey(ctx context.Context, cmd *cobra.Command, o *openedWorkspace, src importer.Source) (string, error) {
	keyFlag, _ := cmd.Flags().GetString("key")
	if cmd.Flags().Changed("key") {
		switch keyFlag {
		case "auto":
			return importer.KeyAuto, nil
		case "none":
			return importer.KeyNone, nil
		default:
			return keyFlag, nil
		}
	}
	insp, err := importer.Inspect(ctx, o.Sess.DB(), src)
	if err != nil {
		return "", err
	}
	return insp.SuggestedKey, nil
}

// tableNameFromCSVPath derives a table name from a data file path.
func tableNameFromCSVPath(csvPath string) string {
	base := filepath.Base(csvPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	var sb strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		} else {
			sb.WriteRune('_')
		}
	}
	if sb.Len() == 0 {
		return "imported"
	}
	return sb.String()
}

// parseImportTypeFlags parses --type col=decimal (repeatable) into a
// normalized-name -> ColType override map, matching importer.Types' closed
// list.
func parseImportTypeFlags(flags []string) (map[string]importer.ColType, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	out := make(map[string]importer.ColType, len(flags))
	for _, f := range flags {
		col, typ, ok := strings.Cut(f, "=")
		if !ok || col == "" || typ == "" {
			return nil, fmt.Errorf("invalid --type %q (want col=type)", f)
		}
		var ct importer.ColType
		found := false
		for _, t := range importer.Types {
			if string(t) == typ {
				ct, found = importer.ColType{Type: t, Scale: 2}, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("invalid --type %q: unknown type %q (want one of: text, integer, decimal, number, date, timestamp, boolean)", f, typ)
		}
		out[col] = ct
	}
	return out, nil
}

// printImportProblems prints why a non-OK import result wrote nothing: a
// Blocked sentence, or the first bad values with the total count.
func printImportProblems(out io.Writer, sum *importer.Summary) {
	fmtFprintln := func(s string) { fmt.Fprintln(out, s) }
	if sum.Blocked != "" {
		fmtFprintln(strings.ReplaceAll(sum.Blocked, "pass mode ", "pass --mode "))
	}
	if len(sum.Missing) > 0 {
		fmtFprintln("the file is missing: " + strings.Join(sum.Missing, ", "))
	}
	if len(sum.Extra) > 0 {
		fmtFprintln("the file has extra columns: " + strings.Join(sum.Extra, ", "))
	}
	for _, bv := range sum.BadValues {
		fmtFprintln(fmt.Sprintf("  row %d  %s  %q  want %s (%s)", bv.Row, bv.Column, bv.Value, bv.Want, bv.Reason))
	}
	if sum.BadCount > len(sum.BadValues) {
		fmtFprintln(fmt.Sprintf("  ...and %d more", sum.BadCount-len(sum.BadValues)))
	}
}

// importSummaryLine renders the one-line result of an import, matching what
// Studio's success/dry-run summary says. dryRun switches the tense: a dry
// run wrote nothing, so it must never claim past tense ("Imported"/"Added"/
// "Replaced") — those verbs are only true of a real, completed write.
func importSummaryLine(sum *importer.Summary, dryRun bool) string {
	if dryRun {
		switch sum.Mode {
		case importer.ModeUpsert:
			return fmt.Sprintf("Would import %d new, update %d, leave %d unchanged into %s", sum.Inserted, sum.Updated, sum.Unchanged, sum.Table)
		case importer.ModeReplace:
			return fmt.Sprintf("Would replace %d row(s) with %d into %s", sum.Replaced, sum.Rows, sum.Table)
		case importer.ModeAppend:
			s := fmt.Sprintf("Would add %d row(s) into %s", sum.Rows, sum.Table)
			if sum.ExactDupes > 0 {
				s += fmt.Sprintf(" (%d already present exactly)", sum.ExactDupes)
			}
			return s
		default:
			return fmt.Sprintf("Would import %d row(s) into %s", sum.Rows, sum.Table)
		}
	}
	switch sum.Mode {
	case importer.ModeUpsert:
		return fmt.Sprintf("%d new, %d changed, %d unchanged into %s", sum.Inserted, sum.Updated, sum.Unchanged, sum.Table)
	case importer.ModeReplace:
		return fmt.Sprintf("Replaced %d row(s) with %d into %s", sum.Replaced, sum.Rows, sum.Table)
	case importer.ModeAppend:
		s := fmt.Sprintf("Added %d row(s) into %s", sum.Inserted, sum.Table)
		if sum.ExactDupes > 0 {
			s += fmt.Sprintf(" (%d already present exactly)", sum.ExactDupes)
		}
		return s
	default:
		return fmt.Sprintf("Imported %d row(s) into %s", sum.Rows, sum.Table)
	}
}

func init() {
	importCmd.Flags().String("into", "", "target table: table, conn.table or conn.schema.table (default: a table named after the file, in the default connection)")
	importCmd.Flags().String("sheet", "", "xlsx sheet name (default: the workbook's first sheet)")
	importCmd.Flags().Bool("no-header", false, "the file's first row is data, not a header")
	importCmd.Flags().String("mode", "", "create|replace|append|upsert (required when the table already exists)")
	importCmd.Flags().String("key", "", "primary key column, \"auto\" for a generated id, or \"none\" (default: the suggested key)")
	importCmd.Flags().StringArray("type", nil, "column type override, col=type (repeatable); type is one of text|integer|decimal|number|date|timestamp|boolean")
	importCmd.Flags().Bool("dry-run", false, "pre-flight only; write nothing")
	addWorkspaceFlag(importCmd)
	rootCmd.AddCommand(importCmd)
}
