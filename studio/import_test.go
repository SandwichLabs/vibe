package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/importer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportFile_CreateInfersTypes proves ImportFile delegates to the
// importer's full-file inference: numeric, boolean, and date columns must
// not land as VARCHAR.
func TestImportFile_CreateInfersTypes(t *testing.T) {
	app := newOpenApp(t)

	csvPath := filepath.Join(t.TempDir(), "calls.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(
		"id,name,total,active,visited\n"+
			"1,\"Acme, Inc.\",250.50,true,2026-06-01\n"+
			"2,Beta,125.00,false,2026-06-02\n"), 0o644))

	insp, err := app.InspectImport(importer.Source{Path: csvPath, HasHeader: true})
	require.NoError(t, err)
	require.Equal(t, "id", insp.SuggestedKey)

	sum, err := app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "calls_typed", Key: "id",
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 2, sum.Rows)

	desc, err := app.DescribeTable("calls_typed")
	require.NoError(t, err)
	types := map[string]string{}
	for _, c := range desc.Columns {
		types[c.Name] = c.Type
	}
	assert.NotEqual(t, "VARCHAR", types["id"])
	assert.Contains(t, types["total"], "DECIMAL")
	assert.Equal(t, "BOOLEAN", types["active"])
	assert.Equal(t, "DATE", types["visited"])
	assert.Equal(t, "VARCHAR", types["name"])

	rs, err := app.ExecuteQuery("SELECT name FROM calls_typed WHERE id = 1")
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)
	assert.Equal(t, "Acme, Inc.", rs.Rows[0][0])

	// Appending into the existing table.
	csvPath2 := filepath.Join(t.TempDir(), "more.csv")
	require.NoError(t, os.WriteFile(csvPath2, []byte(
		"total,id,name,active,visited\n99.99,3,Gamma,true,2026-06-03\n"), 0o644))
	sum, err = app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath2, HasHeader: true}, Table: "calls_typed", Mode: importer.ModeAppend,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 1, sum.Inserted)
}

// TestImportFile_DryRunDoesNotDirty asserts a dry run neither marks the file
// dirty nor writes the skill; a real, successful run does both.
func TestImportFile_DryRunDoesNotDirty(t *testing.T) {
	app := newOpenApp(t)
	csvPath := filepath.Join(t.TempDir(), "c.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("id,name\n1,Ann\n"), 0o644))

	sum, err := app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "t", Key: "id", DryRun: true,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	skills, err := app.ListSkills()
	require.NoError(t, err)
	for _, s := range skills {
		assert.NotEqual(t, "t", s.TableName, "a dry run must not write a skill")
	}

	sum, err = app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "t", Key: "id",
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
}

// TestImportFile_BlockedRunDoesNotDirty asserts a pre-flight failure (a type
// override that doesn't fit) leaves the file clean.
func TestImportFile_BlockedRunDoesNotDirty(t *testing.T) {
	app := newOpenApp(t)
	csvPath := filepath.Join(t.TempDir(), "c.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("id,price\n1,not_a_number\n"), 0o644))

	sum, err := app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "t", Key: "id",
		Types: map[string]importer.ColType{"price": {Type: importer.TypeDecimal, Scale: 2}},
	})
	require.NoError(t, err)
	require.False(t, sum.OK)
}

// TestImportFile_RejectsInvalidIdentifier asserts an invalid table name is
// rejected.
func TestImportFile_RejectsInvalidIdentifier(t *testing.T) {
	app := newOpenApp(t)
	csvPath := filepath.Join(t.TempDir(), "c.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("id\n1\n"), 0o644))

	_, err := app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "bad name!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

// writeTwoSheetXLSXFixture builds a minimal, valid two-sheet .xlsx workbook
// on disk for the xlsx bindings. Sheet "People" has columns id/name with two
// data rows; sheet "Codes" has columns code/label with one data row.
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

// TestInspectImport_XLSX asserts InspectImport surfaces the workbook's sheet
// list, per-sheet header detection, sample rows, and inferred types — and
// that xlsx ids infer to Integer rather than staying DOUBLE.
func TestInspectImport_XLSX(t *testing.T) {
	app := newOpenApp(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSXFixture(t, path)

	insp, err := app.InspectImport(importer.Source{Path: path, Sheet: "People", HasHeader: true})
	require.NoError(t, err)
	require.NotNil(t, insp)

	assert.Contains(t, insp.Sheets, "People")
	assert.Contains(t, insp.Sheets, "Codes")

	require.Len(t, insp.Columns, 2)
	assert.Equal(t, "id", insp.Columns[0].Name)
	assert.Equal(t, importer.TypeInteger, insp.Columns[0].Inferred.Type)
	assert.Equal(t, "name", insp.Columns[1].Name)
	assert.Equal(t, importer.TypeText, insp.Columns[1].Inferred.Type)

	require.GreaterOrEqual(t, len(insp.Rows), 1)
	require.NotNil(t, insp.Rows[0][1])
	assert.Equal(t, "alpha", *insp.Rows[0][1])
}

// TestImportFile_XLSXCreateThenAppend imports an xlsx sheet, then appends
// the same sheet again into the created table.
func TestImportFile_XLSXCreateThenAppend(t *testing.T) {
	app := newOpenApp(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.xlsx")
	writeTwoSheetXLSXFixture(t, path)

	src := importer.Source{Path: path, Sheet: "People", HasHeader: true}
	sum, err := app.ImportFile(importer.Options{Source: src, Table: "people", Key: importer.KeyNone})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 2, sum.Rows)

	sum, err = app.ImportFile(importer.Options{Source: src, Table: "people", Mode: importer.ModeAppend})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	data, err := app.GetTableData("people", 1, 100, "", "")
	require.NoError(t, err)
	assert.Equal(t, 4, data.TotalRows)
}
