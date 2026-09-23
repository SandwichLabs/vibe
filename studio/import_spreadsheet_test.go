package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTwoSheetXLSXFixture builds a minimal, valid two-sheet .xlsx workbook on
// disk for the Import Spreadsheet Wizard backend tests (parity plan §6.3). It
// mirrors the fixture used by internal/importer's xlsx_test.go.
//
// Sheet "People" has columns id/name with two data rows; sheet "Codes" has
// columns code/label with one data row (code=10, label="widget").
func writeTwoSheetXLSXFixture(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	add := func(name, body string) {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}

	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`)

	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`)

	add("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets>
<sheet name="People" sheetId="1" r:id="rId1"/>
<sheet name="Codes" sheetId="2" r:id="rId2"/>
</sheets>
</workbook>`)

	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
</Relationships>`)

	add("xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>id</t></is></c><c r="B1" t="inlineStr"><is><t>name</t></is></c></row>
<row r="2"><c r="A2"><v>1</v></c><c r="B2" t="inlineStr"><is><t>alpha</t></is></c></row>
<row r="3"><c r="A3"><v>2</v></c><c r="B3" t="inlineStr"><is><t>beta</t></is></c></row>
</sheetData>
</worksheet>`)

	add("xl/worksheets/sheet2.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>code</t></is></c><c r="B1" t="inlineStr"><is><t>label</t></is></c></row>
<row r="2"><c r="A2"><v>10</v></c><c r="B2" t="inlineStr"><is><t>widget</t></is></c></row>
</sheetData>
</worksheet>`)

	require.NoError(t, zw.Close())
}

// TestPreviewSpreadsheet asserts the Wails PreviewSpreadsheet binding surfaces
// the workbook's sheet list, per-sheet header detection, sample rows, and
// suggested DuckDB column types for the field-type-mapping step (§6.3).
func TestPreviewSpreadsheet(t *testing.T) {
	app := newOpenApp(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSXFixture(t, path)

	prev, err := app.PreviewSpreadsheet(path, "People", true)
	require.NoError(t, err)
	require.NotNil(t, prev)

	// Sheet list must include both sheets in the workbook.
	assert.Contains(t, prev.Sheets, "People")
	assert.Contains(t, prev.Sheets, "Codes")

	// Detected headers for the requested sheet.
	require.Len(t, prev.Columns, 2)
	assert.Equal(t, "id", prev.Columns[0].Name)
	assert.Equal(t, "name", prev.Columns[1].Name)

	// Suggested types are surfaced for the mapping step.
	assert.NotEmpty(t, prev.Columns[0].Type, "suggested type for id should be set")
	assert.NotEmpty(t, prev.Columns[1].Type, "suggested type for name should be set")
	assert.Contains(t, strings.ToUpper(prev.Columns[0].Type), "DOUBLE")
	assert.Contains(t, strings.ToUpper(prev.Columns[1].Type), "VARCHAR")

	// Sample rows: first data row is (1, "alpha").
	require.GreaterOrEqual(t, len(prev.Rows), 1)
	require.Len(t, prev.Rows[0], 2)
	assert.EqualValues(t, "alpha", prev.Rows[0][1])
}

// TestImportSpreadsheetCreateThenAppend asserts mode=create builds a table with
// the user-supplied column-type mapping and the expected row count, and that a
// subsequent mode=append loads more rows into the same table without
// recreating it (§6.3).
func TestImportSpreadsheetCreateThenAppend(t *testing.T) {
	app := newOpenApp(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSXFixture(t, path)

	opts := ImportSpreadsheetOptions{
		Path:      path,
		Sheet:     "People",
		HasHeader: true,
		TableName: "people",
		Mode:      "create",
		Columns: []ImportColumnMapping{
			{Source: "id", Target: "id", Type: "INTEGER"},
			{Source: "name", Target: "full_name", Type: "VARCHAR"},
		},
	}

	res, err := app.ImportSpreadsheet(opts)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "people", res.TableName)
	assert.Equal(t, 2, res.RowsImported, "People sheet has two data rows")

	// The new table must use the user-supplied type mapping.
	info, err := app.DescribeTable("people")
	require.NoError(t, err)
	require.Len(t, info.Columns, 2)
	assert.Equal(t, "id", info.Columns[0].Name)
	assert.Equal(t, "full_name", info.Columns[1].Name)
	assert.Contains(t, strings.ToUpper(info.Columns[0].Type), "INT")
	assert.Contains(t, strings.ToUpper(info.Columns[1].Type), "VARCHAR")

	// Append the same sheet again; rows must accumulate without recreating.
	appendOpts := opts
	appendOpts.Mode = "append"
	res2, err := app.ImportSpreadsheet(appendOpts)
	require.NoError(t, err)
	require.NotNil(t, res2)
	assert.Equal(t, 2, res2.RowsImported)

	data, err := app.GetTableData("people", 1, 100, "", "")
	require.NoError(t, err)
	assert.Equal(t, 4, data.TotalRows, "append accumulates rows on top of the created table")
}

// TestImportSpreadsheetRejectsInvalidIdentifier asserts an invalid table name
// is rejected, matching ImportCSV's identifier guard.
func TestImportSpreadsheetRejectsInvalidIdentifier(t *testing.T) {
	app := newOpenApp(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSXFixture(t, path)

	_, err := app.ImportSpreadsheet(ImportSpreadsheetOptions{
		Path:      path,
		Sheet:     "People",
		HasHeader: true,
		TableName: "bad name!",
		Mode:      "create",
		Columns: []ImportColumnMapping{
			{Source: "id", Target: "id", Type: "INTEGER"},
		},
	})
	require.Error(t, err, "invalid identifier must be rejected")
	assert.Contains(t, strings.ToLower(err.Error()), "invalid")
}
