package cmd

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/usersql"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   `query "<sql>"`,
	Short: "Run one SQL statement against a workspace",
	Long: `Run one SQL statement through the workspace's DuckDB session and print the
result. Unqualified names resolve against the workspace's default connection;
-c names another one, and any connection can also be named in the SQL
(sales_pg.public.orders).

By default this process takes ownership of the workspace's database (the hub
lock), so a statement that writes is allowed on a connection that is not
read-only. If Vibe Studio or "vibe mcp serve" already has the workspace open,
this says who. The owner keeps the workspace's local DuckDB files locked for
writing and DuckDB cannot read a file a writer holds, so --read-only (read
without taking ownership, every connection attached read-only, no COPY ... TO)
only reaches the workspace's other connections, such as MotherDuck or Postgres.

This is you acting directly, not the agent, so SQL runs as written. As a
courtesy, ATTACH, DETACH, INSTALL, LOAD, SET and CREATE SECRET are refused with
an explanation (connections come from the workspace, credentials from your
keychain). That is a check for common slips, not a security boundary.

Pass "-" as the statement to read it from standard input. Results are capped
at --limit rows (default 1000, 0 for no cap) and the cap is reported on
standard error.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runQuery,
	SilenceUsage: true,
}

func init() {
	queryCmd.Flags().StringP("connection", "c", "", "connection whose catalog unqualified names use (default: the workspace's default connection)")
	queryCmd.Flags().String("format", "table", "output format: table, csv, json")
	queryCmd.Flags().Int("limit", defaultQueryLimit, "maximum rows to print (0 = no cap)")
	queryCmd.Flags().Bool("read-only", false, "read without taking ownership of the workspace; every connection is attached read-only")
	addWorkspaceFlag(queryCmd)
	rootCmd.AddCommand(queryCmd)
}

const defaultQueryLimit = 1000

func runQuery(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("workspace")
	conn, _ := cmd.Flags().GetString("connection")
	format, _ := cmd.Flags().GetString("format")
	limit, _ := cmd.Flags().GetInt("limit")
	readOnly, _ := cmd.Flags().GetBool("read-only")
	errW := cmd.ErrOrStderr()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := validateFormat(format); err != nil {
		return err
	}
	if limit < 0 {
		return fmt.Errorf("--limit must be 0 or more")
	}
	sqlText := args[0]
	if sqlText == "-" {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("failed to read the statement from standard input: %w", err)
		}
		sqlText = string(b)
	}
	if strings.TrimSpace(sqlText) == "" {
		return fmt.Errorf("no SQL given")
	}
	if err := usersql.Check(sqlText, readOnly); err != nil {
		return err
	}

	o, err := openWorkspace(ctx, dir, readOnly, true, "vibe query", errW)
	if err != nil {
		return err
	}
	defer o.Close()

	target := conn
	if target == "" {
		target = o.WS.DefaultConnection()
	} else if _, ok := o.knownConnection(target); !ok {
		return fmt.Errorf("unknown connection %q; this workspace has: %s", target, connectionNames(o))
	}
	if err := o.requireConnection(target, errW); err != nil {
		return err
	}

	rs, total, err := o.Eng.ExecuteCappedIn(ctx, conn, sqlText, nil, limit)
	if err != nil {
		return friendlyQueryError(err, readOnly)
	}
	if err := writeResult(cmd.OutOrStdout(), rs, format); err != nil {
		return err
	}
	if limit > 0 && total > rs.RowCount() {
		fmt.Fprintf(errW, "showing the first %d of %d rows (--limit 0 for all)\n", rs.RowCount(), total)
	}
	return nil
}

func connectionNames(o *openedWorkspace) string {
	conns := o.WS.Connections()
	names := make([]string, 0, len(conns))
	for _, c := range conns {
		names = append(names, c.Name)
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// friendlyQueryError adds the one hint that is always right: a write against a
// read-only attach.
func friendlyQueryError(err error, readOnly bool) error {
	if engine.IsReadOnlyViolation(err) {
		if readOnly {
			return fmt.Errorf("%w\n(--read-only attaches every connection read-only)", err)
		}
		return fmt.Errorf("%w\n(that connection is read-only; set `read_only: false` on it in vibe.yaml or your connections file to allow writes)", err)
	}
	return err
}

func validateFormat(f string) error {
	switch f {
	case "table", "csv", "json":
		return nil
	}
	return fmt.Errorf("unsupported format %q: use table, csv or json", f)
}

// writeResult prints a result set as an aligned table, CSV or a JSON array of
// objects (column order preserved).
func writeResult(out io.Writer, rs *engine.ResultSet, format string) error {
	switch format {
	case "csv":
		return writeCSV(out, rs)
	case "json":
		return writeJSON(out, rs)
	default:
		return writeTable(out, rs)
	}
}

func writeCSV(out io.Writer, rs *engine.ResultSet) error {
	w := csv.NewWriter(out)
	names := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		names[i] = c.Name
	}
	if err := w.Write(names); err != nil {
		return fmt.Errorf("failed to write CSV: %w", err)
	}
	for _, row := range rs.Rows {
		rec := make([]string, len(rs.Columns))
		for i := range rec {
			if i < len(row) && row[i] != nil {
				rec[i] = cellText(row[i])
			}
		}
		if err := w.Write(rec); err != nil {
			return fmt.Errorf("failed to write CSV: %w", err)
		}
	}
	w.Flush()
	return w.Error()
}

// writeJSON prints the rows as an array of objects with the columns in order.
func writeJSON(out io.Writer, rs *engine.ResultSet) error {
	var b bytes.Buffer
	b.WriteString("[")
	for ri, row := range rs.Rows {
		if ri > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n  ")
		writeJSONRow(&b, rs.Columns, row)
	}
	if len(rs.Rows) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("]\n")
	_, err := out.Write(b.Bytes())
	return err
}

func writeJSONRow(b *bytes.Buffer, cols []engine.Column, row []any) {
	b.WriteString("{")
	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		k, _ := json.Marshal(c.Name)
		var v any
		if i < len(row) {
			v = row[i]
		}
		vb, err := json.Marshal(v)
		if err != nil {
			vb, _ = json.Marshal(cellText(v))
		}
		b.Write(k)
		b.WriteString(": ")
		b.Write(vb)
	}
	b.WriteString("}")
}

func cellText(v any) string { return engine.CellText(v, "") }

func writeTable(out io.Writer, rs *engine.ResultSet) error {
	if len(rs.Columns) == 0 {
		fmt.Fprintln(out, "OK")
		return nil
	}
	widths := make([]int, len(rs.Columns))
	cells := make([][]string, len(rs.Rows))
	for i, c := range rs.Columns {
		widths[i] = len([]rune(cleanCell(c.Name)))
	}
	for ri, row := range rs.Rows {
		cells[ri] = make([]string, len(rs.Columns))
		for i := range rs.Columns {
			s := "NULL"
			if i < len(row) && row[i] != nil {
				s = cleanCell(cellText(row[i]))
			}
			cells[ri][i] = s
			if n := len([]rune(s)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(vals []string) {
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = v + strings.Repeat(" ", widths[i]-len([]rune(v)))
		}
		fmt.Fprintln(out, strings.TrimRight(strings.Join(parts, "  "), " "))
	}
	names := make([]string, len(rs.Columns))
	seps := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		names[i] = cleanCell(c.Name)
		seps[i] = strings.Repeat("-", widths[i])
	}
	line(names)
	line(seps)
	for _, r := range cells {
		line(r)
	}
	fmt.Fprintf(out, "(%d row%s)\n", len(rs.Rows), plural(len(rs.Rows)))
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cleanCell keeps a value on one line and strips terminal control characters
// so data in a table cannot move the cursor or recolor the screen.
func cleanCell(s string) string {
	return engine.TerminalText(s, engine.TerminalOpts{StripC1: true})
}
