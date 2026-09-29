package importer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func colByName(insp *Inspection, name string) *Column {
	for i := range insp.Columns {
		if insp.Columns[i].Name == name {
			return &insp.Columns[i]
		}
	}
	return nil
}

func TestInspect_PanelShapedCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order_items.csv")
	csv := "item_id,order_id,unit_price,line_total,discount,order_date,note\n" +
		"1,100,12.50,25.00,0.10,2025-01-03,first\n" +
		"2,100,9.99,19.98,0.00,2025-01-03,second\n" +
		"3,101,100.32,100.32,0.00,2025-02-14,third\n"
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o644))

	db := openDuckDB(t)
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)

	require.Equal(t, TypeInteger, colByName(insp, "item_id").Inferred.Type)
	require.Equal(t, ColType{Type: TypeDecimal, Scale: 2}, colByName(insp, "unit_price").Inferred)
	require.Equal(t, ColType{Type: TypeDecimal, Scale: 2}, colByName(insp, "line_total").Inferred)
	require.Equal(t, ColType{Type: TypeDecimal, Scale: 2}, colByName(insp, "discount").Inferred)
	require.Equal(t, TypeDate, colByName(insp, "order_date").Inferred.Type)
	require.Equal(t, TypeText, colByName(insp, "note").Inferred.Type)
	require.Equal(t, "item_id", insp.SuggestedKey)
	require.Equal(t, 3, insp.RowCount)

	// No stage table left behind.
	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM duckdb_tables() WHERE table_name = '_vibe_import_stage'").Scan(&n))
	require.Equal(t, 0, n)
}

func writeXLSXFixture(t *testing.T, db *sql.DB, dir, name, csvBody string) string {
	t.Helper()
	csvPath := filepath.Join(dir, name+"_src.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvBody), 0o644))
	xlsxPath := filepath.Join(dir, name+".xlsx")
	_, err := db.Exec("INSTALL excel; LOAD excel;")
	require.NoError(t, err)
	_, err = db.Exec("COPY (FROM read_csv_auto($1)) TO $2 (FORMAT xlsx, HEADER true)", csvPath, xlsxPath)
	require.NoError(t, err)
	return xlsxPath
}

func TestInspect_XLSX(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	csv := "customer_id,name,credit_limit,joined\n1,Kim Park,1200.50,2025-01-03\n2,Ana Diaz,999.99,2025-02-14\n"
	xlsxPath := writeXLSXFixture(t, db, dir, "customers", csv)

	insp, err := Inspect(context.Background(), db, Source{Path: xlsxPath, HasHeader: true})
	require.NoError(t, err)

	require.Contains(t, insp.Sheets, insp.Sheets[0])
	require.Equal(t, TypeInteger, colByName(insp, "customer_id").Inferred.Type, "xlsx ids must not stay DOUBLE")
	require.Equal(t, ColType{Type: TypeDecimal, Scale: 2}, colByName(insp, "credit_limit").Inferred)
	require.Equal(t, TypeDate, colByName(insp, "joined").Inferred.Type)
	require.Equal(t, "customer_id", insp.SuggestedKey)
}

func TestInspect_TextAndNumberEdgeCases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "odd.csv")
	csv := "zip,mdate,micro,threedec,money\n" +
		"02134,1/3/2025,1.234567,1.234,\"$1,200.50\"\n" +
		"02135,1/4/2025,2.5,2.500,\"$50.00\"\n"
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o644))

	db := openDuckDB(t)
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)

	require.Equal(t, TypeText, colByName(insp, "zip").Inferred.Type, "leading-zero zip must stay text")
	// m/d/Y is locale-ambiguous (DuckDB's sniffer would pick a day/month
	// order and silently reinterpret half these files) — the CSV date
	// path only trusts an ISO round-trip (TRY_CAST AS DATE), so a
	// non-ISO date correctly stays Text rather than a silently
	// reinterpreted Date (see castExpr's Date fits).
	require.Equal(t, TypeText, colByName(insp, "mdate").Inferred.Type, "ambiguous m/d/Y must stay text, not be silently reinterpreted")
	require.Equal(t, TypeNumber, colByName(insp, "micro").Inferred.Type, "6-decimal column must be Number")
	require.Equal(t, ColType{Type: TypeDecimal, Scale: 3}, colByName(insp, "threedec").Inferred)
	require.Equal(t, TypeText, colByName(insp, "money").Inferred.Type, "$-formatted text stays text")
}

func TestInspect_KeyCandidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k.csv")
	csv := "customer_id,name,paid\n1,Ann,true\n2,Ann,false\n3,Bob,true\n"
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o644))

	db := openDuckDB(t)
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)

	require.True(t, colByName(insp, "customer_id").Candidate)
	require.False(t, colByName(insp, "name").Candidate, "name is not unique here")
	require.Equal(t, "customer_id", insp.SuggestedKey)

	// A unique but non-id-looking column is a candidate but not suggested.
	path2 := filepath.Join(dir, "k2.csv")
	csv2 := "widget,name,paid\n1,Ann,true\n2,Bob,false\n"
	require.NoError(t, os.WriteFile(path2, []byte(csv2), 0o644))
	insp2, err := Inspect(context.Background(), db, Source{Path: path2, HasHeader: true})
	require.NoError(t, err)
	require.True(t, colByName(insp2, "name").Candidate)
	require.NotEqual(t, "name", insp2.SuggestedKey)
	require.NotEqual(t, "paid", insp2.SuggestedKey)

	// A duplicate-or-null id column is not a candidate; no candidates -> KeyAuto.
	path3 := filepath.Join(dir, "k3.csv")
	csv3 := "customer_id,name\n1,Ann\n1,Bob\n"
	require.NoError(t, os.WriteFile(path3, []byte(csv3), 0o644))
	insp3, err := Inspect(context.Background(), db, Source{Path: path3, HasHeader: true})
	require.NoError(t, err)
	require.False(t, colByName(insp3, "customer_id").Candidate)
	require.Equal(t, KeyAuto, insp3.SuggestedKey)
}

func TestInspect_CamelCaseIDSuggested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cust.csv")
	csv := "customerId,name\n1,Ann\n2,Bob\n"
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o644))

	db := openDuckDB(t)
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)
	require.Equal(t, "customerId", insp.SuggestedKey, "customerId is already a valid identifier and is kept as-is")
}

func TestNormalizeName(t *testing.T) {
	require.Equal(t, "order_date", NormalizeName("Order Date"))
	require.Equal(t, "_2024_sales", NormalizeName("2024 Sales"))
	require.Equal(t, "column", NormalizeName(""))
	require.Equal(t, "customer_id", NormalizeName("customer_id"))
}

// TestNormalizeName_SharedNameTable is the Go half of a name table also
// asserted in studio/frontend/src/lib/importTypes.spec.ts (normalizeIdentifier)
// — every entry must produce the exact same result on both sides, since
// both derive default table/column names from the same rule. Covers
// whitespace-only/empty input, leading digits, an already-valid identifier
// kept as-is (including case), non-ASCII letters/diacritics, and the one
// input (İstanbul) that needed a matching fix on the TS side: JS's
// String.prototype.toLowerCase() decomposes U+0130 into two codepoints
// where Go's strings.ToLower collapses it to one, so TS folds that one
// character before lower-casing to agree with Go here.
func TestNormalizeName_SharedNameTable(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Order Date", "order_date"},
		{"order_date", "order_date"},
		{"Customers", "Customers"},
		{"2024 Sales", "_2024_sales"},
		{"  x  ", "x"},
		{"", "column"},
		{"___", "___"},
		{"Zoë", "zo"},
		{"ÄÖÜ name", "name"},
		{"İstanbul", "istanbul"},
		{"Straße", "stra_e"},
		{"ǅemal", "emal"},
		{"a-b-c", "a_b_c"},
		{"A__B", "A__B"},
		{"_vibe_x", "_vibe_x"},
		{"select", "select"},
		{"   ", "column"},
		{"日本", "column"},
		{"x\ty", "x_y"},
		{"Ω", "column"},
		{"ſ", "column"},
		{"K", "K"},
		{"Ǆ", "column"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, NormalizeName(c.in), "NormalizeName(%q)", c.in)
	}
}

func TestDedupeName(t *testing.T) {
	used := map[string]bool{}
	require.Equal(t, "name", dedupeName("name", used))
	require.Equal(t, "name_2", dedupeName("name", used))
	require.Equal(t, "column_1", dedupeName("column", used))
	require.Equal(t, "column_2", dedupeName("column", used))
}

func TestInspect_PreviewRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.csv")
	csv := "id,name\n1,\"  Kim Park\"\n2,\n"
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o644))

	db := openDuckDB(t)
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)
	require.Len(t, insp.Rows, 2)
	require.NotNil(t, insp.Rows[0][1])
	require.Equal(t, "  Kim Park", *insp.Rows[0][1], "leading spaces must be preserved")
	require.Nil(t, insp.Rows[1][1], "NULL must render as nil, distinguishable from empty text")
}
