// Package importer provides backend helpers for reading external data files
// (CSV, Excel) into the DuckDB engine that backs a .vibe container. It mirrors
// the existing CSV import path and is the data layer beneath the Studio
// Import Spreadsheet Wizard (parity plan Phase 6, §6.1/§6.2).
package importer

import (
	"archive/zip"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// previewRowLimit bounds the number of sample rows surfaced by PreviewXLSX for
// the wizard's preview grid.
const previewRowLimit = 50

// XLSXColumn describes a single column discovered while previewing an .xlsx
// sheet: its name (from the header row when header detection is on, or a
// synthesized name like "A1"/"B1" when off) and the DuckDB type inferred by
// the excel extension (e.g. DOUBLE, VARCHAR). The inferred type feeds the
// field-type-mapping step of the Import Spreadsheet Wizard (§6.1).
type XLSXColumn struct {
	Name string
	Type string
}

// XLSXPreview is the result of PreviewXLSX: the discovered columns plus a
// bounded sample of rows for the wizard's preview grid.
type XLSXPreview struct {
	Sheet   string
	Columns []XLSXColumn
	Rows    [][]any
}

// ensureExcelExtension best-effort loads DuckDB's excel extension. The
// extension is bundled/autoloadable in this build, so failures here are not
// fatal — the subsequent read_xlsx call surfaces a clear error if it is truly
// unavailable.
func ensureExcelExtension(ddb *sql.DB) {
	_, _ = ddb.Exec("INSTALL excel")
	_, _ = ddb.Exec("LOAD excel")
}

// readXLSXExpr builds a read_xlsx(...) table expression for the given path and
// sheet, escaping single quotes to keep the SQL literals well-formed. An empty
// sheet selects the workbook's default/first sheet.
func readXLSXExpr(path, sheet string, hasHeader bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "read_xlsx('%s'", strings.ReplaceAll(path, "'", "''"))
	if sheet != "" {
		fmt.Fprintf(&b, ", sheet='%s'", strings.ReplaceAll(sheet, "'", "''"))
	}
	fmt.Fprintf(&b, ", header=%t)", hasHeader)
	return b.String()
}

// describeXLSXColumns runs DESCRIBE against the read_xlsx expression to recover
// each column's name and the type inferred by the excel extension.
func describeXLSXColumns(ddb *sql.DB, expr string) ([]XLSXColumn, error) {
	rows, err := ddb.Query("DESCRIBE SELECT * FROM " + expr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var cols []XLSXColumn
	for rows.Next() {
		dest := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		// DESCRIBE returns (column_name, column_type, null, key, default, extra).
		col := XLSXColumn{}
		if len(dest) > 0 {
			col.Name = asString(dest[0])
		}
		if len(dest) > 1 {
			col.Type = asString(dest[1])
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

// asString coerces a DuckDB scan value to a string for column metadata.
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// PreviewXLSX reads an .xlsx file through DuckDB's excel extension (read_xlsx),
// returning the column names + inferred types and the first N sample rows for
// the import preview. When hasHeader is true the first row supplies column
// names; otherwise synthesized names are used. An empty sheet selects the
// workbook's default/first sheet.
func PreviewXLSX(ddb *sql.DB, path, sheet string, hasHeader bool) (*XLSXPreview, error) {
	if ddb == nil {
		return nil, fmt.Errorf("preview xlsx: DuckDB not available")
	}
	ensureExcelExtension(ddb)

	expr := readXLSXExpr(path, sheet, hasHeader)

	cols, err := describeXLSXColumns(ddb, expr)
	if err != nil {
		return nil, wrapXLSXError("preview", sheet, err)
	}

	sampleSQL := fmt.Sprintf("SELECT * FROM %s LIMIT %d", expr, previewRowLimit)
	rows, err := ddb.Query(sampleSQL)
	if err != nil {
		return nil, wrapXLSXError("preview", sheet, err)
	}
	defer func() { _ = rows.Close() }()

	data, err := scanRows(rows)
	if err != nil {
		return nil, wrapXLSXError("preview", sheet, err)
	}

	return &XLSXPreview{Sheet: sheet, Columns: cols, Rows: data}, nil
}

// ReadXLSXSheet reads all rows of the named sheet from an .xlsx file via the
// excel extension, returning the column names and the full row set for a typed
// import. A missing file or unknown sheet name yields a clear, wrapped error.
func ReadXLSXSheet(ddb *sql.DB, path, sheet string, hasHeader bool) ([]XLSXColumn, [][]any, error) {
	if ddb == nil {
		return nil, nil, fmt.Errorf("read xlsx: DuckDB not available")
	}
	ensureExcelExtension(ddb)

	expr := readXLSXExpr(path, sheet, hasHeader)

	cols, err := describeXLSXColumns(ddb, expr)
	if err != nil {
		return nil, nil, wrapXLSXError("read", sheet, err)
	}

	rows, err := ddb.Query("SELECT * FROM " + expr)
	if err != nil {
		return nil, nil, wrapXLSXError("read", sheet, err)
	}
	defer func() { _ = rows.Close() }()

	data, err := scanRows(rows)
	if err != nil {
		return nil, nil, wrapXLSXError("read", sheet, err)
	}

	return cols, data, nil
}

// ListXLSXSheets reads the workbook part of an .xlsx (which is a zip archive)
// and returns the ordered list of worksheet names. This drives the wizard's
// sheet picker without depending on a DuckDB sheet-enumeration function. A
// missing or malformed workbook yields a clear, wrapped error.
func ListXLSXSheets(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open xlsx %q: %w", path, err)
	}
	defer func() { _ = zr.Close() }()

	var workbook io.ReadCloser
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			workbook, err = f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open workbook part: %w", err)
			}
			break
		}
	}
	if workbook == nil {
		return nil, fmt.Errorf("xlsx %q has no workbook part", path)
	}
	defer func() { _ = workbook.Close() }()

	var doc struct {
		Sheets struct {
			Sheet []struct {
				Name string `xml:"name,attr"`
			} `xml:"sheet"`
		} `xml:"sheets"`
	}
	if err := xml.NewDecoder(workbook).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to parse workbook part of %q: %w", path, err)
	}

	names := make([]string, 0, len(doc.Sheets.Sheet))
	for _, s := range doc.Sheets.Sheet {
		names = append(names, s.Name)
	}
	return names, nil
}

// scanRows materializes all rows from a query result into [][]any.
func scanRows(rows *sql.Rows) ([][]any, error) {
	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]any
	for rows.Next() {
		dest := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		out = append(out, dest)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// wrapXLSXError wraps a DuckDB read_xlsx error with operation context, always
// referencing the "xlsx" operation and the "sheet" when one was requested so
// callers (and tests) get a clear, attributable message.
func wrapXLSXError(op, sheet string, err error) error {
	if sheet != "" {
		return fmt.Errorf("failed to %s xlsx sheet %q: %w", op, sheet, err)
	}
	return fmt.Errorf("failed to %s xlsx: %w", op, err)
}
