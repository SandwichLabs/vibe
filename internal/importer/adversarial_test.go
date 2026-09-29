package importer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// This file covers adversarial import cases: spaced/mixed-case headers,
// reserved table names, text-staged keys against an integer PK,
// ambiguous/mixed-format dates, timestamp offsets, empty files, and
// case-different table names. Each test is a regression for a specific
// silent-data-loss or raw-DuckDB-error case the pre-flight checks exist to
// catch.

// A key column whose file header isn't a valid identifier ("Order ID")
// must not break create/replace with a raw "Referenced column not found".
func TestImport_KeyHeaderNeedsNormalization(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "k.csv", "Order ID,Amount\n1,1.50\n2,2.50\n")

	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "order_id",
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var pk []any
	require.NoError(t, db.QueryRow(
		`SELECT constraint_column_names FROM duckdb_constraints() WHERE table_name='t' AND constraint_type='PRIMARY KEY'`,
	).Scan(&pk))
	require.Equal(t, []any{"order_id"}, pk)
}

// Upserting text keys "1" and "01" into a BIGINT primary key must be
// pre-flighted and blocked (both cast to the same BIGINT 1 — DuckDB's own
// ON CONFLICT silently collapses this, which is exactly the case the
// duplicate-key pre-check exists to catch), never silently drop a row while
// reporting it as inserted.
func TestImport_UpsertKeyCastCollision(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	_, err := db.Exec(`CREATE TABLE t (id BIGINT PRIMARY KEY, v VARCHAR)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO t VALUES (5, 'old')`)
	require.NoError(t, err)

	path := writeCSV(t, dir, "b.csv", "id,v\n1,a\n01,b\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.False(t, sum.OK, "%+v", sum)
	require.NotEmpty(t, sum.BadValues)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count, "nothing must be written when the cast collision is blocked")
}

// Appending the same text-staged-key file into an integer-PK table must
// not raise a raw "Cannot compare VARCHAR and BIGINT" error — the collision/
// duplicate checks must compare the CAST value, not the raw staged text.
func TestImport_AppendKeyCastMismatch(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	_, err := db.Exec(`CREATE TABLE t (id BIGINT PRIMARY KEY, v VARCHAR)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO t VALUES (5, 'old')`)
	require.NoError(t, err)

	path := writeCSV(t, dir, "b.csv", "id,v\n1,a\n01,b\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Mode: ModeAppend})
	require.NoError(t, err, "must be a clean Summary, never a raw type-mismatch error")
	require.False(t, sum.OK)
}

// Two file headers that normalize to the same target column
// ("Order Date" and "order_date") must not let the second silently
// overwrite the first's match — matchExistingColumns must dedupe the file
// side the same way create/replace's own column list does, reporting the
// collided header as Extra instead of dropping its values.
func TestImport_AppendNormalizedHeaderCollision(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	_, err := db.Exec(`CREATE TABLE t (id BIGINT PRIMARY KEY, order_date DATE)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO t VALUES (1, '2020-01-01')`)
	require.NoError(t, err)

	path := writeCSV(t, dir, "a.csv", "id,Order Date,order_date\n1,2025-01-01,2024-12-31\n2,2025-01-02,2024-12-30\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	require.NoError(t, err)
	require.False(t, sum.OK, "%+v", sum)
	// The first file column to normalize to "order_date" wins the match
	// (consistent with create/replace's own dedupeName pass, also
	// first-come); the second, later one is reported as Extra instead of
	// silently overwriting the first's match and vanishing.
	require.Contains(t, sum.Extra, "order_date")
}

// Upserting into a table whose name is a reserved word or mixed case
// ("order", "select", "Order") must not raise a raw parser syntax error —
// every reference to the table name in the write path must be quoted.
func TestImport_UpsertReservedOrMixedCaseTableName(t *testing.T) {
	for _, tn := range []string{"order", "select", "Order"} {
		t.Run(tn, func(t *testing.T) {
			db := openDuckDB(t)
			dir := t.TempDir()
			path1 := writeCSV(t, dir, "a.csv", "id,v\n1,a\n2,b\n")
			sum, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: tn, Key: "id"})
			require.NoError(t, err)
			require.True(t, sum.OK, "%+v", sum)

			path2 := writeCSV(t, dir, "b.csv", "id,v\n1,z\n3,c\n")
			sum, err = Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: tn, Mode: ModeUpsert})
			require.NoError(t, err, "must not be a raw parser error")
			require.True(t, sum.OK, "%+v", sum)
			require.Equal(t, 1, sum.Inserted)
			require.Equal(t, 1, sum.Updated)
		})
	}
}

// A value written in scientific notation must infer Number, never
// Integer with the exponent silently evaluated away.
func TestImport_ExponentInfersNumber(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "e.csv", "id,v\n1,1e3\n2,2\n")
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)
	require.Equal(t, TypeNumber, colByName(insp, "v").Inferred.Type)
}

// An ambiguous locale date ("01/02/2025" — Jan 2 or Feb 1 depending on
// which format DuckDB's sniffer happens to guess) must stay Text rather
// than silently landing as a Date with the day/month order the sniffer
// picked. TRY_CAST AS DATE without a session date_style only ever accepts
// ISO-8601, so this is a direct consequence of inference reusing castExpr's
// own fits check.
func TestImport_AmbiguousDateStaysText(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "d.csv", "id,d\n1,01/02/2025\n2,03/04/2025\n")
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)
	require.Equal(t, TypeText, colByName(insp, "d").Inferred.Type)

	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	var d string
	require.NoError(t, db.QueryRow("SELECT d FROM t WHERE id=1").Scan(&d))
	require.Equal(t, "01/02/2025", d, "the original text must round-trip unchanged, not a reinterpreted date")
}

// A column mixing "2025-01-01" and a differently-laid-out date must
// not raise a raw DuckDB sniff/parse error — Inspect must still return a
// clean result (Text for that column, since it doesn't consistently fit
// any one friendly type).
func TestImport_MixedDateFormatsDoesNotError(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "d.csv", "id,d\n1,2025-01-01\n2,01/02/2025\n")
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err, "a mixed-format column must never surface a raw DuckDB parse error")
	require.NotNil(t, colByName(insp, "d"))
}

// A timestamp with a UTC offset must not silently fit Timestamp —
// TRY_CAST(... AS TIMESTAMP) discards the offset instead of applying it
// (proven against DuckDB 1.4.1), so accepting it would silently change the
// moment in time the value represents.
func TestImport_TimestampOffsetRejected(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "tz.csv", "id,t\n1,2025-01-01 10:00:00+05:00\n")
	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id",
		Types: map[string]ColType{"t": {Type: TypeTimestamp}},
	})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.NotEmpty(t, sum.BadValues)
	require.Equal(t, "t", sum.BadValues[0].Column)
}

// Non-blocking: an empty or header-only file must be blocked with a clear
// message, not silently create a junk table.
func TestImport_EmptyFileBlocked(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "empty.csv", "")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t"})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.Contains(t, sum.Blocked, "no rows")

	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='t'").Scan(&n))
	require.Equal(t, 0, n)
}

func TestImport_HeaderOnlyFileBlocked(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "hdronly.csv", "id,a\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t"})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.Contains(t, sum.Blocked, "no rows")
}

// Non-blocking: the table-existence check must be case-insensitive, since
// DuckDB's own quoted-identifier catalog lookup is (a table stored as
// "Customers" IS the same table as "customers" once quoted).
func TestImport_TableExistsCaseInsensitive(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	_, err := db.Exec(`CREATE TABLE "Customers" (id BIGINT PRIMARY KEY, v VARCHAR)`)
	require.NoError(t, err)

	path := writeCSV(t, dir, "c.csv", "id,v\n1,a\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "customers", Key: "id", DryRun: true})
	require.NoError(t, err)
	require.True(t, sum.Exists, "a dry run must see the existing table regardless of case")

	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "customers", Mode: ModeUpsert})
	require.NoError(t, err, "must resolve to the existing table, not raise a raw catalog 'already exists' error")
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, "Customers", sum.Table, "Summary.Table must be the catalog's own spelling, not the caller's")
}

// TestImport_CaseDifferentTable_SequenceAndReplaceUseCanonicalName is the
// full round-trip the case-insensitive lookup must not break halfway
// through: an upsert that WRITES a high id (bumping the auto-number
// sequence) and a Replace, both addressed by a different case than the
// table was actually created with. describeExistingTable resolving the
// match case-insensitively is not enough on its own — every step AFTER
// that match (the sequence name, the Replace DROP/recreate, the skill this
// Import's caller writes from Summary.Table) has to keep using the
// catalog's own spelling, or the sequence bump silently no-ops (looking for
// '_vibe_seq_customers_id' in a default of
// nextval('_vibe_seq_Customers_id')) and a manual insert collides with an
// upserted high id.
func TestImport_CaseDifferentTable_SequenceAndReplaceUseCanonicalName(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "id,v\n1,a\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "Customers", Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	path2 := writeCSV(t, dir, "c2.csv", "id,v\n50,z\n")
	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "customers", Mode: ModeUpsert})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	var nextID int64
	require.NoError(t, db.QueryRow(`INSERT INTO "Customers" (v) VALUES ('m') RETURNING id`).Scan(&nextID))
	require.Greater(t, nextID, int64(50), "the sequence must have been bumped past the upserted id 50, addressed as 'customers'")

	path3 := writeCSV(t, dir, "c3.csv", "id,v\n1,new\n")
	sum, err = Import(context.Background(), db, Options{Source: Source{Path: path3, HasHeader: true}, Table: "customers", Mode: ModeReplace, Key: "id"})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, "Customers", sum.Table)

	var n int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_name='Customers'").Scan(&n))
	require.Equal(t, 1, n, "Replace must keep the table's original name, not rename it to the caller's casing")
}

// Non-blocking: a camelCase/PascalCase "CustomerID"-style header must be
// suggested as the key, not just "customerId".
func TestImport_PascalCaseIDSuggested(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "CustomerID,name\n1,Ann\n2,Bob\n")
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)
	require.Equal(t, "CustomerID", insp.SuggestedKey)
}

// Non-blocking: an out-of-range Decimal scale must be rejected cleanly, not
// left to reach DuckDB as a raw "Scale cannot be bigger than width" error.
func TestImport_InvalidDecimalScaleRejected(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "c.csv", "id,v\n1,1.5\n")
	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id",
		Types: map[string]ColType{"v": {Type: TypeDecimal, Scale: 99}},
	})
	require.NoError(t, err)
	require.False(t, sum.OK)
	require.Contains(t, sum.Blocked, "scale")
}

// TestInspect_XLSXMixedNumberText_CleanErrorNotAllVarchar proves that when
// an xlsx column mixes numbers and text (so the typed read_xlsx fails
// partway through — DESCRIBE only samples, the full read doesn't), Inspect
// returns a plain, actionable error naming the cell instead of silently
// falling back to reading the whole sheet as text: all_varchar renders a
// DATE cell as its underlying serial number and turns every number into
// raw text, both silently wrong rather than merely conservative (this is
// why the all-varchar fallback stays CSV-only — see stage()'s doc comment).
func TestInspect_XLSXMixedNumberText_CleanErrorNotAllVarchar(t *testing.T) {
	db := openDuckDB(t)
	_, err := Inspect(context.Background(), db, Source{Path: "testdata/mixed_number_text.xlsx", HasHeader: true})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "Invalid Input Error", "must not leak the raw DuckDB error text")
	require.Contains(t, err.Error(), "abc")
	require.Contains(t, err.Error(), "B4")
}

// TestInspect_XLSXDateColumnSurvivesMixedSiblingColumn proves a real xlsx
// DATE column stages correctly (never becomes a serial-number Text column)
// even when materializing the SHEET fails because a different column mixes
// numbers and text — the whole point of refusing the all-varchar fallback
// for xlsx (that fallback would silently turn every date into its
// underlying serial, e.g. "45659.0").
func TestInspect_XLSXDateColumnSurvivesMixedSiblingColumn(t *testing.T) {
	db := openDuckDB(t)
	_, err := Inspect(context.Background(), db, Source{Path: "testdata/datemix2.xlsx", HasHeader: true})
	require.Error(t, err, "a mixed number/text column must still error rather than silently stage everything as text")
	require.Contains(t, err.Error(), "n") // the offending column, per the fixture
	require.Contains(t, err.Error(), "abc")
	require.NotRegexp(t, `4\d{4}\.0`, err.Error(), "no serial-number-shaped text (a stray '45xxx.0', a DATE cell's underlying serial) must ever appear")
}

// TestInspect_BigIntTextInfersText proves a 20-digit account-id-shaped text
// column infers Text, not Number — Number would silently store the nearby
// double instead of the exact digits, since a double can't represent all
// 20-digit integers exactly.
func TestInspect_BigIntTextInfersText(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "acct.csv", "acct,v\n12345678901234567890,1\n12345678901234567891,2\n")
	insp, err := Inspect(context.Background(), db, Source{Path: path, HasHeader: true})
	require.NoError(t, err)
	require.Equal(t, TypeText, colByName(insp, "acct").Inferred.Type)

	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "t", Key: KeyNone})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	var acct string
	require.NoError(t, db.QueryRow("SELECT acct FROM t WHERE v='1'").Scan(&acct))
	require.Equal(t, "12345678901234567890", acct, "the exact digits must be preserved, not a rounded double")
}

// TestImport_NumberOverrideOnUnrepresentableInt_Blocks proves an explicit
// Number override on 9007199254740993 (2^53+1 — the smallest positive
// integer a double can't represent exactly) is caught as a bad value, not
// silently stored as the neighboring double (…992).
func TestImport_NumberOverrideOnUnrepresentableInt_Blocks(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "n.csv", "id,v\n1,9007199254740993\n")
	sum, err := Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "t", Key: "id",
		Types: map[string]ColType{"v": {Type: TypeNumber}},
	})
	require.NoError(t, err)
	require.False(t, sum.OK, "%+v", sum)
	require.NotEmpty(t, sum.BadValues)
	require.Equal(t, "v", sum.BadValues[0].Column)

	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='t'").Scan(&n))
	require.Equal(t, 0, n)
}

// TestImport_ReimportAutoKeyTableWarnsExactDupes is the issue's headline
// bug: a file with no id column (people.csv-shaped — auto key, since
// upsert has nothing to match on) imported, then imported again. Upsert
// isn't offered (no key column in the file), so the only structurally
// available mode is append. Without this fix, ExactDupes was only computed
// when the table had NO primary key at all — an auto-key table has one,
// just not supplied by the file — so every row silently duplicated with
// no warning. Comparing the file's rows against the table's MATCHED
// columns only (name, city — never the auto id column, which isn't in the
// file) is what makes this work for an auto-key table at all.
func TestImport_ReimportAutoKeyTableWarnsExactDupes(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path := writeCSV(t, dir, "people.csv", "name,city\nAnn,Springfield\nBob,Shelbyville\n")

	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path, HasHeader: true}, Table: "people", Key: KeyAuto})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.NotContains(t, sum.Modes, ModeUpsert, "no id column in the file: upsert must not be offered")

	sum, err = Import(context.Background(), db, Options{
		Source: Source{Path: path, HasHeader: true}, Table: "people", Mode: ModeAppend, DryRun: true,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	require.Equal(t, sum.Rows, sum.ExactDupes, "a dry-run re-import of the exact same file must flag every row as an exact duplicate")

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM people").Scan(&count))
	require.Equal(t, 2, count, "a dry run must not have written anything")
}

// Non-blocking: BumpAutoNumber must refuse a huge gap rather than hang —
// this is importer-level coverage of engine.TestBumpAutoNumber_CapsHugeGap,
// proving the cap actually surfaces through Import as a rolled-back write
// with a clean error, not a multi-minute stall.
func TestImport_UpsertHugeAutoNumberGapRefused(t *testing.T) {
	db := openDuckDB(t)
	dir := t.TempDir()
	path1 := writeCSV(t, dir, "a.csv", "name\na\n")
	_, err := Import(context.Background(), db, Options{Source: Source{Path: path1, HasHeader: true}, Table: "t", Key: KeyAuto})
	require.NoError(t, err)

	path2 := writeCSV(t, dir, "b.csv", "id,name\n50000000000,c\n")
	sum, err := Import(context.Background(), db, Options{Source: Source{Path: path2, HasHeader: true}, Table: "t", Mode: ModeUpsert})
	// Caught pre-flight, before any write — a clean Blocked result, not a
	// thrown Go error from a rolled-back write. AutoNumberGap never
	// touches the sequence, so the caught-early and would-be-rolled-back
	// cases both leave the table and sequence exactly where they started.
	require.NoError(t, err)
	require.False(t, sum.OK, "%+v", sum)
	require.Contains(t, sum.Blocked, "gap")

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM t").Scan(&count))
	require.Equal(t, 1, count, "a blocked pre-flight must leave the table untouched")
}
