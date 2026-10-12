// Package importer provides backend helpers for reading external data files
// (CSV, Excel) into a workspace connection's DuckDB session: the
// import wizard's inspect/preview and its create/replace/append/upsert
// write path (#82).
package importer

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// readXLSXExpr builds a read_xlsx(...) table expression for the given path and
// sheet, escaping single quotes to keep the SQL literals well-formed. An empty
// sheet selects the workbook's default/first sheet.
func readXLSXExpr(path, sheet string, hasHeader bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "read_xlsx(%s", session.QuoteLiteral(path))
	if sheet != "" {
		fmt.Fprintf(&b, ", sheet=%s", session.QuoteLiteral(sheet))
	}
	fmt.Fprintf(&b, ", header=%t)", hasHeader)
	return b.String()
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

var xlsxCellErrRe = regexp.MustCompile(`cell '([A-Z]+)([0-9]+)'`)
var xlsxValueErrRe = regexp.MustCompile(`Could not convert string '(.*)' to ([A-Z]+)$`)

// wrapXLSXMaterializeError turns a raw read_xlsx materialize failure (e.g.
// "read_xlsx: Failed to parse cell 'B4': Could not convert string 'abc' to
// DOUBLE") into a plain sentence naming the sheet, the offending value, the
// spreadsheet cell (always exact, unlike a column name, which can't be
// mapped reliably when the data doesn't start in column A). Falls
// back to wrapping the raw error when the message doesn't match the shape
// DuckDB's excel extension is known to produce, rather than guessing.
func wrapXLSXMaterializeError(sheet string, err error) error {
	msg := err.Error()
	cellCol, cellRow := "", ""
	if m := xlsxCellErrRe.FindStringSubmatch(msg); m != nil {
		cellCol, cellRow = m[1], m[2]
	}
	value, wantType := "", ""
	if m := xlsxValueErrRe.FindStringSubmatch(msg); m != nil {
		value, wantType = m[1], strings.ToLower(m[2])
	}
	if cellCol == "" || value == "" {
		if sheet != "" {
			return fmt.Errorf("failed to read xlsx sheet %q: %w", sheet, err)
		}
		return fmt.Errorf("failed to read xlsx file: %w", err)
	}

	cell := cellCol + cellRow
	sheetDesc := "the sheet"
	if sheet != "" {
		sheetDesc = fmt.Sprintf("sheet %q", sheet)
	}
	return fmt.Errorf("%s has %q at cell %s, which doesn't fit a %s column; fix the cell or save the sheet as CSV",
		sheetDesc, value, cell, wantType)
}
