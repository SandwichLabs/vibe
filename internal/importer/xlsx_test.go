package importer

import (
	"archive/zip"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "test-passphrase-12345"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// openTestContainer creates and opens a real .vibe container with a live
// DuckDB engine (no mocking) for integration testing.
func openTestContainer(t *testing.T) *container.Container {
	t.Helper()
	dir := t.TempDir()
	c, err := container.Create(filepath.Join(dir, "import_test.vibe"),
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Import Test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// writeTwoSheetXLSX builds a minimal, valid two-sheet .xlsx workbook on disk.
//
// DuckDB's COPY ... TO (FORMAT xlsx) can only write a single sheet per file
// (a second COPY overwrites the file rather than appending a sheet), so the
// fixture is assembled directly as an OOXML zip. Sheet "People" has columns
// id/name with two data rows; sheet "Codes" has columns code/label with one
// data row (code=10, label="widget").
func writeTwoSheetXLSX(t *testing.T, path string) {
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

// TestPreviewXLSX_HeaderDetection asserts that read_xlsx-backed preview returns
// the correct header-derived column names (with inferred types) and the first
// sample rows when header detection is on.
func TestPreviewXLSX_HeaderDetection(t *testing.T) {
	c := openTestContainer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSX(t, path)

	prev, err := PreviewXLSX(c.DuckDBDB(), path, "People", true)
	require.NoError(t, err)
	require.NotNil(t, prev)

	names := columnNames(prev.Columns)
	assert.Equal(t, []string{"id", "name"}, names, "header row should supply column names")

	// Inferred types must be surfaced for the field-type-mapping step (§6.1).
	require.Len(t, prev.Columns, 2)
	for _, col := range prev.Columns {
		assert.NotEmpty(t, col.Type, "inferred column type should be surfaced for %q", col.Name)
	}
	assert.Contains(t, strings.ToUpper(prev.Columns[0].Type), "DOUBLE",
		"numeric id column should infer a numeric DuckDB type")
	assert.Contains(t, strings.ToUpper(prev.Columns[1].Type), "VARCHAR",
		"text name column should infer a string DuckDB type")

	// Sample rows: first data row is (1, "alpha").
	require.GreaterOrEqual(t, len(prev.Rows), 1)
	first := prev.Rows[0]
	require.Len(t, first, 2)
	assert.EqualValues(t, "alpha", first[1])
}

// TestPreviewXLSX_NoHeader asserts synthesized column names when header
// detection is off.
func TestPreviewXLSX_NoHeader(t *testing.T) {
	c := openTestContainer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSX(t, path)

	prev, err := PreviewXLSX(c.DuckDBDB(), path, "People", false)
	require.NoError(t, err)
	require.NotNil(t, prev)

	names := columnNames(prev.Columns)
	assert.Equal(t, []string{"A1", "B1"}, names,
		"with header detection off, column names should be synthesized")
	// The original header row ("id"/"name") becomes data when no header.
	require.GreaterOrEqual(t, len(prev.Rows), 1)
	assert.EqualValues(t, "id", prev.Rows[0][0])
}

// TestReadXLSXSheet_BySheetName asserts sheet selection by name works and that
// a known cell value and row count are returned for the second sheet.
func TestReadXLSXSheet_BySheetName(t *testing.T) {
	c := openTestContainer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSX(t, path)

	cols, rows, err := ReadXLSXSheet(c.DuckDBDB(), path, "Codes", true)
	require.NoError(t, err)

	assert.Equal(t, []string{"code", "label"}, columnNames(cols))
	require.Len(t, rows, 1, "Codes sheet has exactly one data row")
	require.Len(t, rows[0], 2)
	assert.EqualValues(t, "widget", rows[0][1], "known cell value in Codes sheet")
}

// TestReadXLSXSheet_MissingFile asserts a clear, wrapped error for a missing path.
func TestReadXLSXSheet_MissingFile(t *testing.T) {
	c := openTestContainer(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.xlsx")

	_, _, err := ReadXLSXSheet(c.DuckDBDB(), missing, "People", true)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "xlsx",
		"error should reference the xlsx read operation")
}

// TestReadXLSXSheet_NonexistentSheet asserts a wrapped error when the sheet
// name is not present in the workbook.
func TestReadXLSXSheet_NonexistentSheet(t *testing.T) {
	c := openTestContainer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSX(t, path)

	_, _, err := ReadXLSXSheet(c.DuckDBDB(), path, "NoSuchSheet", true)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "sheet",
		"error should reference the missing sheet")
}

func columnNames(cols []XLSXColumn) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}
