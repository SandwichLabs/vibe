package importer

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/SandwichLabs/vibe/internal/session"
	"path/filepath"
	"strings"
)

// Source identifies one file/sheet to import.
type Source struct {
	Path      string `json:"path"`
	Sheet     string `json:"sheet"`
	HasHeader bool   `json:"has_header"`
}

// kind reports "csv" or "xlsx" from the file extension, or an error for
// anything else.
func (s Source) kind() (string, error) {
	switch strings.ToLower(filepath.Ext(s.Path)) {
	case ".csv", ".tsv", ".txt":
		return "csv", nil
	case ".xlsx":
		return "xlsx", nil
	default:
		return "", fmt.Errorf("unsupported file type %q (only .csv, .tsv, and .xlsx are supported)", filepath.Ext(s.Path))
	}
}

// stageTable is the fixed TEMP table name the whole import pipeline stages
// into. It lives on a single pinned *sql.Conn (container/duckdb.go's pool is
// unbounded, and TEMP tables are per-connection) — see stage's doc comment.
const stageTable = "_vibe_import_stage"

// stagedCol describes one column of the staged file: its header as written
// in the file, the DuckDB type it actually landed as in the stage table
// (after any VARCHAR forcing), and the type DuckDB's sniffer/reader first
// inferred before forcing (used by inspection to decide the friendly type).
// Promote marks a column that was forced to VARCHAR specifically so
// inference can re-derive its friendly type from a lossless round-trip
// check (castExpr's fits) instead of trusting the sniffer's own
// type/format guess — see inferType's doc comment for why the sniffer's
// guess isn't good enough for money or dates.
type stagedCol struct {
	SourceName  string
	StagedType  string
	SniffedType string
	Promote     bool
}

// stage reads the whole file into a fresh TEMP TABLE _vibe_import_stage on
// conn, with an added row_number() OVER () AS __row column that preserves
// file order for every downstream INSERT ... ORDER BY __row.
//
// CSV columns DuckDB's sniffer calls DOUBLE/FLOAT/DATE/TIMESTAMP are forced
// back to VARCHAR (keeping the file's original text): DOUBLE loses the
// exact decimal text a money column needs ("100.32" round-trips through
// DOUBLE fine, but not every value does), and the sniffer's date/timestamp
// format guess is a per-FILE format lock that silently reinterprets
// ambiguous values ("01/02/2025" becomes either Jan 2 or Feb 1 depending on
// which format the sniffer happened to guess) instead of erroring or
// leaving them as text. Forcing these to VARCHAR means inference (inferType)
// re-derives the friendly type itself from a lossless round-trip check
// (castExpr's fits) rather than trusting the sniffer's guess. textCols
// (normalized names the caller wants to force to text, e.g. a type override
// to Text) are forced the same way.
//
// If the sniffer itself can't produce one consistent per-file read (e.g. a
// column mixing "2025-01-01" and other layouts inconsistently enough to
// trip the sniffer, or an xlsx column mixing numbers and text — the excel
// extension has no per-cell type, so this fails on the whole column), the
// whole file is re-read with every column forced to VARCHAR/all_varchar
// instead of surfacing the raw DuckDB parse error. Every column is then
// Promote=true, so inference re-derives friendly types (including numeric
// and date/time) straight from that text — see inferType.
func stage(ctx context.Context, conn *sql.Conn, s Source, textCols map[string]bool) ([]stagedCol, error) {
	kind, err := s.kind()
	if err != nil {
		return nil, err
	}
	var plan stagePlan
	switch kind {
	case "csv":
		plan = planCSVStage(ctx, conn, s, textCols)
	case "xlsx":
		plan, err = planXLSXStage(ctx, conn, s)
		if err != nil {
			return nil, err
		}
	}
	if err := plan.materialize(ctx, conn, s, kind); err != nil {
		return nil, err
	}

	// DESCRIBE the stage table itself for both the final type and file order
	// (Go maps don't preserve order) — one pass, not a second sniff of expr.
	staged, err := describeColumns(ctx, conn, stageTable)
	if err != nil {
		return nil, fmt.Errorf("failed to describe staged data: %w", err)
	}

	cols := make([]stagedCol, 0, len(staged))
	for _, dc := range staged {
		sc := stagedCol{SourceName: dc.Name, StagedType: dc.Type, SniffedType: plan.sniffed[dc.Name], Promote: plan.promote[dc.Name] || plan.allPromoted}
		if sc.SniffedType == "" {
			sc.SniffedType = sc.StagedType
		}
		cols = append(cols, sc)
	}
	return cols, nil
}

// stagePlan is how stage reads a file: the table expression, what the sniffer
// said per column, which columns are read as text so inference re-derives
// their type, and whether every column is.
type stagePlan struct {
	expr        string
	sniffed     map[string]string
	promote     map[string]bool
	allPromoted bool
}

// planCSVStage sniffs a CSV and forces the lossy-sniffed and text-overridden
// columns to VARCHAR.
func planCSVStage(ctx context.Context, conn *sql.Conn, s Source, textCols map[string]bool) stagePlan {
	p := stagePlan{promote: map[string]bool{}}
	base := csvExpr(s.Path, s.HasHeader, nil)
	sniffed, err := describeExpr(ctx, conn, base)
	if err != nil {
		// The full-file sniff itself couldn't settle on one type per
		// column (a raw DuckDB parse/format error). Fall back to
		// reading every column as text; inference then decides each
		// column's friendly type independently.
		p.sniffed = map[string]string{}
		p.expr = csvExprAllVarchar(s.Path, s.HasHeader)
		p.allPromoted = true
		return p
	}
	p.sniffed = sniffed
	overrides := map[string]string{}
	for name, typ := range sniffed {
		up := strings.ToUpper(typ)
		forceText := up == "DOUBLE" || up == "FLOAT" || up == "REAL" ||
			up == "DATE" || strings.HasPrefix(up, "TIMESTAMP")
		if !forceText && textCols != nil {
			forceText = textCols[NormalizeName(name)]
		}
		if forceText {
			overrides[name] = "VARCHAR"
			p.promote[name] = true
		}
	}
	p.expr = csvExpr(s.Path, s.HasHeader, overrides)
	return p
}

// planXLSXStage describes the sheet; the excel extension has no per-cell type.
func planXLSXStage(ctx context.Context, conn *sql.Conn, s Source) (stagePlan, error) {
	p := stagePlan{sniffed: map[string]string{}, promote: map[string]bool{}}
	if err := session.EnsureExtension(ctx, conn, "excel", "Importing an Excel file"); err != nil {
		return p, err
	}
	p.expr = readXLSXExpr(s.Path, s.Sheet, s.HasHeader)
	xlsxCols, derr := describeColumns(ctx, conn, p.expr)
	if derr != nil {
		return p, fmt.Errorf("failed to read %q: %w", s.Path, derr)
	}
	for _, c := range xlsxCols {
		p.sniffed[c.Name] = c.Type
	}
	return p, nil
}

// materialize creates the TEMP stage table from the plan, retrying a CSV that
// fails mid-read as all text.
func (p *stagePlan) materialize(ctx context.Context, conn *sql.Conn, s Source, kind string) error {
	if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+stageTable); err != nil {
		return fmt.Errorf("failed to drop stale stage table: %w", err)
	}
	create := func() error {
		_, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE TEMP TABLE %s AS SELECT row_number() OVER () AS __row, * FROM %s", stageTable, p.expr))
		return err
	}
	err := create()
	if err == nil {
		return nil
	}
	switch {
	case p.allPromoted:
		// Already tried the all-text fallback; nothing more to try.
		return fmt.Errorf("failed to read %q: %w", s.Path, err)
	case kind == "csv":
		// A column type DuckDB was confident enough about to DESCRIBE
		// (it only samples) still failed once every row was actually
		// read. Retry with every column forced to text — lossless
		// for CSV, since the original text is exactly what a VARCHAR
		// column keeps.
		p.expr = csvExprAllVarchar(s.Path, s.HasHeader)
		p.allPromoted = true
		if err2 := create(); err2 != nil {
			return fmt.Errorf("failed to read %q: %w", s.Path, err)
		}
		return nil
	default: // xlsx
		// NOT retried with all_varchar=true: unlike CSV text, an xlsx
		// numeric/date cell has no separate "original text" —
		// all_varchar renders a DATE cell as its underlying serial
		// number ("45659.0") and a number as its raw text, both
		// silently wrong rather than merely conservative. A clean, named
		// error is the only safe response to a column DuckDB itself
		// can't type consistently (typically numbers mixed with text
		// in one column, which read_xlsx has no per-column override
		// for, unlike read_csv's `types=`).
		return wrapXLSXMaterializeError(s.Sheet, err)
	}
}

// dropStage removes the TEMP stage table. It must always run (deferred) once
// a pipeline is done with a pooled connection — sql.Open("duckdb", ...) is an
// unbounded pool (container/duckdb.go), and a TEMP table left behind on a
// connection that returns to the pool would leak into whatever the next
// caller runs on that same connection.
func dropStage(ctx context.Context, conn *sql.Conn) {
	_, _ = conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+stageTable)
}

// csvExpr builds a read_csv(...) table expression with a full-file sniff
// (sample_size=-1) and an optional per-column type override map (column name
// -> DuckDB type, used to force VARCHAR on columns that must keep their
// original text).
func csvExpr(path string, hasHeader bool, overrides map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "read_csv(%s, header=%t, sample_size=-1", session.QuoteLiteral(path), hasHeader)
	if len(overrides) > 0 {
		b.WriteString(", types={")
		first := true
		for name, typ := range overrides {
			if !first {
				b.WriteString(", ")
			}
			first = false
			fmt.Fprintf(&b, "%s: '%s'", session.QuoteLiteral(name), typ)
		}
		b.WriteString("}")
	}
	b.WriteString(")")
	return b.String()
}

// csvExprAllVarchar builds a read_csv(...) expression that reads every
// column as VARCHAR, bypassing the sniffer's type/format guessing
// entirely — the fallback used when the sniffer can't settle on one
// consistent type/format for a column across the whole file.
func csvExprAllVarchar(path string, hasHeader bool) string {
	return fmt.Sprintf("read_csv(%s, header=%t, all_varchar=true)", session.QuoteLiteral(path), hasHeader)
}

// describeCol is one row of a DESCRIBE result, in the query's own column
// order (which Go maps don't preserve).
type describeCol struct {
	Name string
	Type string
}

// describeColumns runs DESCRIBE against a table expression and returns its
// columns in order, excluding the synthetic __row column stage() adds.
func describeColumns(ctx context.Context, conn *sql.Conn, expr string) ([]describeCol, error) {
	rows, err := conn.QueryContext(ctx, "DESCRIBE SELECT * FROM "+expr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []describeCol
	for rows.Next() {
		var name, typ, nullable, key, def, extra sql.NullString
		if err := rows.Scan(&name, &typ, &nullable, &key, &def, &extra); err != nil {
			return nil, err
		}
		if name.String == "__row" {
			continue
		}
		out = append(out, describeCol{Name: name.String, Type: typ.String})
	}
	return out, rows.Err()
}

// describeExpr is describeColumns collapsed into a column-name -> DuckDB-type
// map, for callers that only need lookups (not order).
func describeExpr(ctx context.Context, conn *sql.Conn, expr string) (map[string]string, error) {
	cols, err := describeColumns(ctx, conn, expr)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(cols))
	for _, c := range cols {
		out[c.Name] = c.Type
	}
	return out, nil
}
