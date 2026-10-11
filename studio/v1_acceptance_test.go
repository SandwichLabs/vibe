package main

// Phase 6 / unit P6.10 (parity plan §5): the v1.0 acceptance scenario.
//
// This is the end-to-end "messy-spreadsheet-to-relational-app" loop that the
// v1.0 cut must satisfy, composing P6.2-P6.7 against the REAL Wails bindings and
// a REAL workspace (DuckDB hub) seeded from the bundled order-entry
// template (P6.6):
//
//  1. Start from the order-entry template (customers / orders / order_items with
//     declared FK relationships).
//  2. Generate a messy multi-sheet .xlsx fixture: a "LineItems" sheet whose data
//     is preceded by an extra blank header row and whose price column is
//     mixed-type (numbers stored as strings), plus a second "Notes" sheet.
//  3. InspectImport the messy sheet, then override the messy price column
//     to Decimal.
//  4. ImportFile mode=append into the FK-constrained `order_items` template
//     table (order_id -> orders.id): first with an orphan child row (missing
//     parent order) present — the whole import must be REJECTED (OK=false,
//     nothing written; #82 replaced the old per-row-skip behavior with
//     all-or-nothing pre-flighted imports) — then again with a fixture that
//     omits the orphan, which must land cleanly.
//  5. close + reopen and assert persistence of the imported rows.
//
// No mocks: the real bindings + real DuckDB enforce referential integrity.

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/importer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMessyLineItemsXLSXFixture builds a two-sheet workbook for the v1.0
// acceptance scenario. The "LineItems" sheet is deliberately messy:
//   - an extra blank row precedes the real header row,
//   - the header is id / order_id / product / quantity / price,
//   - the `price` column is stored as strings ("99.50", "12.00") — a mixed/typed
//     column the user must coerce to DOUBLE during the mapping step.
//
// Data rows reference orders seeded by the order-entry template. The third data
// row references a NON-EXISTENT order (id 99999) to exercise FK rejection on
// append.
//
// A second "Notes" sheet exists only to assert multi-sheet preview.
func writeMessyLineItemsXLSXFixture(t *testing.T, path string, validOrderID, orphanOrderID int, includeOrphan bool) {
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
<sheet name="LineItems" sheetId="1" r:id="rId1"/>
<sheet name="Notes" sheetId="2" r:id="rId2"/>
</sheets>
</workbook>`)

	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
</Relationships>`)

	// Sheet 1 "LineItems": blank row 1, header row 2, data rows 3-5.
	// price column values are inline strings to model a mixed/typed column.
	validOrder := itoa(validOrderID)
	orphanOrder := itoa(orphanOrderID)
	orphanRow := ""
	if includeOrphan {
		orphanRow = `<row r="5"><c r="A5"><v>5003</v></c><c r="B5"><v>` + orphanOrder + `</v></c><c r="C5" t="inlineStr"><is><t>Orphan</t></is></c><c r="D5"><v>1</v></c><c r="E5" t="inlineStr"><is><t>7.25</t></is></c></row>`
	}
	add("xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t></t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>id</t></is></c><c r="B2" t="inlineStr"><is><t>order_id</t></is></c><c r="C2" t="inlineStr"><is><t>product</t></is></c><c r="D2" t="inlineStr"><is><t>quantity</t></is></c><c r="E2" t="inlineStr"><is><t>price</t></is></c></row>
<row r="3"><c r="A3"><v>5001</v></c><c r="B3"><v>`+validOrder+`</v></c><c r="C3" t="inlineStr"><is><t>Gadget</t></is></c><c r="D3"><v>1</v></c><c r="E3" t="inlineStr"><is><t>99.50</t></is></c></row>
<row r="4"><c r="A4"><v>5002</v></c><c r="B4"><v>`+validOrder+`</v></c><c r="C4" t="inlineStr"><is><t>Sprocket</t></is></c><c r="D4"><v>2</v></c><c r="E4" t="inlineStr"><is><t>12.00</t></is></c></row>
`+orphanRow+`
</sheetData>
</worksheet>`)

	add("xl/worksheets/sheet2.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>note</t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>migrated from legacy spreadsheet</t></is></c></row>
</sheetData>
</worksheet>`)

	require.NoError(t, zw.Close())
}

// itoa avoids importing strconv just for a couple of small ints in fixture XML.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestV1Acceptance_MessySpreadsheetToRelationalApp is the P6.10 v1.0 acceptance
// scenario end-to-end through the real bindings.
func TestV1Acceptance_MessySpreadsheetToRelationalApp(t *testing.T) {
	// 1. Start from the bundled order-entry template (P6.6).
	app := newAppFromOrderEntryTemplate(t)

	ddb := app.sess.DB()
	require.NotNil(t, ddb)

	// Determine a real order id (the template seeds orders) and the
	// order_items-table row count before import. order_items.order_id -> orders.id
	// is the FK relationship we exercise.
	var validOrderID int
	require.NoError(t, ddb.QueryRow(`SELECT id FROM orders ORDER BY id LIMIT 1`).Scan(&validOrderID))
	const orphanOrderID = 99999
	var hasOrphan int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE id = ?`, orphanOrderID).Scan(&hasOrphan))
	require.Zero(t, hasOrphan, "the orphan order id must not exist in the template")

	var itemsBefore int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items`).Scan(&itemsBefore))

	// 2. Generate the messy multi-sheet xlsx fixture, orphan row included.
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "messy-lineitems.xlsx")
	writeMessyLineItemsXLSXFixture(t, xlsxPath, validOrderID, orphanOrderID, true)

	// 3. InspectImport must surface both sheets and detect the real header
	// row despite the leading blank row.
	src := importer.Source{Path: xlsxPath, Sheet: "LineItems", HasHeader: true}
	insp, err := app.InspectImport(src)
	require.NoError(t, err)
	require.NotNil(t, insp)
	assert.Contains(t, insp.Sheets, "LineItems")
	assert.Contains(t, insp.Sheets, "Notes")

	colNames := make([]string, 0, len(insp.Columns))
	for _, c := range insp.Columns {
		colNames = append(colNames, c.Name)
	}
	assert.ElementsMatch(t, []string{"id", "order_id", "product", "quantity", "price"}, colNames,
		"the real header row must be detected past the leading blank row")

	// 4. Append into the FK-constrained `order_items` template table
	// (order_id -> orders.id). #82 replaced the old per-row-skip behavior
	// with an all-or-nothing pre-flighted import: with the orphan row
	// present the WHOLE import must be rejected, not just that one row.
	sum, err := app.ImportFile(importer.Options{Source: src, Table: "order_items", Mode: importer.ModeAppend})
	require.NoError(t, err)
	require.False(t, sum.OK, "an orphan child row must block the whole import, not just itself")
	require.NotEmpty(t, sum.BadValues)
	assert.Equal(t, "missing_parent", sum.BadValues[0].Reason)

	var itemsUnchanged int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items`).Scan(&itemsUnchanged))
	assert.Equal(t, itemsBefore, itemsUnchanged, "a blocked import must leave the table untouched")

	// Re-import a fixture that omits the orphan row: it must land cleanly.
	xlsxPath2 := filepath.Join(dir, "clean-lineitems.xlsx")
	writeMessyLineItemsXLSXFixture(t, xlsxPath2, validOrderID, orphanOrderID, false)
	src2 := importer.Source{Path: xlsxPath2, Sheet: "LineItems", HasHeader: true}
	sum, err = app.ImportFile(importer.Options{Source: src2, Table: "order_items", Mode: importer.ModeAppend})
	require.NoError(t, err, "append import into the FK-constrained order_items table should succeed for valid rows")
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 2, sum.Inserted)

	var itemsAfter int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items`).Scan(&itemsAfter))
	assert.Equal(t, itemsBefore+2, itemsAfter, "only the two FK-valid rows must be appended")

	var orphanLanded int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items WHERE id = 5003`).Scan(&orphanLanded))
	assert.Zero(t, orphanLanded, "the orphan child row (missing parent order) must not be persisted")

	// The messy string price must have been coerced to a real number.
	var price float64
	require.NoError(t, ddb.QueryRow(`SELECT price FROM order_items WHERE id = 5001`).Scan(&price))
	assert.InDelta(t, 99.50, price, 0.001, "the messy string price must be coerced to DOUBLE")

	// 5. close + reopen: the imported rows persist across the lifecycle.
	reopenApp(t, app)
	ddb2 := app.sess.DB()
	require.NotNil(t, ddb2)

	var itemsPersisted int
	require.NoError(t, ddb2.QueryRow(`SELECT COUNT(*) FROM order_items`).Scan(&itemsPersisted))
	assert.Equal(t, itemsAfter, itemsPersisted, "imported rows must persist across close + reopen")

	var persistedPrice float64
	require.NoError(t, ddb2.QueryRow(`SELECT price FROM order_items WHERE id = 5001`).Scan(&persistedPrice))
	assert.InDelta(t, 99.50, persistedPrice, 0.001, "the typed value must survive close + reopen")
}

// newAppFromOrderEntryTemplate creates a fresh workspace seeded from the
// order-entry template (P6.6) and returns an App with it open, ready for
// binding-level acceptance assertions.
func newAppFromOrderEntryTemplate(t *testing.T) *App {
	t.Helper()

	app := newBareApp(t)
	res, err := app.CreateWorkspace(CreateWorkspaceRequest{
		Name: "Acceptance", ParentDir: t.TempDir(), Template: "order-entry",
	})
	require.NoError(t, err, "CreateWorkspace(order-entry) should seed a relational app")
	require.Empty(t, res.Warnings)
	return app
}
