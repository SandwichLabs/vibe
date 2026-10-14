package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/session/sessiontest"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeValueDecimal proves the wire contract for DECIMAL: a
// small unscaled magnitude gives an exact json.Number with the scale-padded
// text, not the old lossy float64(d.Float64()) conversion.
func TestNormalizeValueDecimal(t *testing.T) {
	d := duckdb.Decimal{Width: 10, Scale: 2, Value: big.NewInt(999)}
	assert.Equal(t, json.Number("9.99"), NormalizeValue(d))
	assert.Equal(t, json.Number("9.99"), NormalizeValue(&d))

	var nilDec *duckdb.Decimal
	assert.Nil(t, NormalizeValue(nilDec))
}

// TestNormalizeValueBigInt proves the wire contract for HUGEINT: a
// value that fits a JS double exactly stays an int64 (a JSON number), and
// one that doesn't crosses as its exact decimal string, whether or not it
// also fits an int64.
func TestNormalizeValueBigInt(t *testing.T) {
	assert.Equal(t, int64(42), NormalizeValue(big.NewInt(42)))

	huge, ok := new(big.Int).SetString("170141183460469231731687303715884105727", 10)
	require.True(t, ok)
	assert.Equal(t, "170141183460469231731687303715884105727", NormalizeValue(huge))

	// Fits int64 but not the JS safe-integer range: still a string.
	assert.Equal(t, "9007199254740993", NormalizeValue(big.NewInt(9007199254740993)))
	assert.Equal(t, int64(9007199254740991), NormalizeValue(big.NewInt(9007199254740991)))

	var nilBig *big.Int
	assert.Nil(t, NormalizeValue(nilBig))
}

func TestNormalizeValuePassthrough(t *testing.T) {
	assert.Equal(t, "x", NormalizeValue("x"))
	assert.Equal(t, 1.5, NormalizeValue(1.5))
	assert.Nil(t, NormalizeValue(nil))
}

// TestNormalizeValueNonFiniteFloat covers the crash a NaN or ±Inf query
// result caused: encoding/json.Marshal errors
// hard on any of the three (not a silent omission), which took the whole
// Studio process down when it surfaced unhandled through Wails' own
// result-marshaling bridge (RunA2UIQuery/ExecuteQuery). NormalizeValue maps
// all three to DuckDB's own ::VARCHAR text ("nan"/"inf"/"-inf") before a
// row ever reaches a JSON boundary — a string, not nil, per this package's
// wire rule (a real value that a JSON number can't hold crosses as DuckDB's
// own text, the same treatment an out-of-range BIGINT/DECIMAL gets).
func TestNormalizeValueNonFiniteFloat(t *testing.T) {
	assert.Equal(t, "nan", NormalizeValue(math.NaN()))
	assert.Equal(t, "inf", NormalizeValue(math.Inf(1)))
	assert.Equal(t, "-inf", NormalizeValue(math.Inf(-1)))
	assert.Equal(t, "nan", NormalizeValue(float32(math.NaN())))
	// A finite float, including a plain float32, passes through unchanged
	// (as a float64 — Go's database/sql driver values are never float32 in
	// practice, but NormalizeValue upconverts if it ever saw one).
	assert.InDelta(t, 1.5, NormalizeValue(float32(1.5)), 0.0001)
}

// TestNormalizeValueRecursesThroughComposites is the pure-unit counterpart
// to TestNaNNestedInCompositeThroughDuckDBExecute, exercising NormalizeValue
// directly for the composite shapes go-duckdb can hand back, including
// duckdb.Map/duckdb.Union — which this driver version happens to scan as
// plain map[string]any/passthrough instead (see the DuckDB-backed test),
// but NormalizeValue must still handle them explicitly in case a future
// driver version (or a different code path) does hand one back.
func TestNormalizeValueRecursesThroughComposites(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		got := NormalizeValue([]any{math.NaN(), 1.0, "x"})
		assert.Equal(t, []any{"nan", 1.0, "x"}, got)
	})
	t.Run("nested list", func(t *testing.T) {
		got := NormalizeValue([]any{[]any{math.Inf(1), 2.0}})
		assert.Equal(t, []any{[]any{"inf", 2.0}}, got)
	})
	t.Run("struct (map[string]any)", func(t *testing.T) {
		got := NormalizeValue(map[string]any{"a": math.NaN(), "b": "ok"})
		assert.Equal(t, map[string]any{"a": "nan", "b": "ok"}, got)
	})
	t.Run("list of struct", func(t *testing.T) {
		got := NormalizeValue([]any{map[string]any{"x": math.NaN()}})
		assert.Equal(t, []any{map[string]any{"x": "nan"}}, got)
	})
	t.Run("duckdb.Map", func(t *testing.T) {
		got := NormalizeValue(duckdb.Map{"k": math.NaN()})
		assert.Equal(t, map[string]any{"k": "nan"}, got)
	})
	t.Run("duckdb.Union", func(t *testing.T) {
		got := NormalizeValue(duckdb.Union{Tag: "d", Value: math.Inf(-1)})
		assert.Equal(t, "-inf", got)
	})
	t.Run("struct containing a Decimal and a big.Int", func(t *testing.T) {
		got := NormalizeValue(map[string]any{
			"price": duckdb.Decimal{Width: 10, Scale: 2, Value: big.NewInt(999)},
			"big":   big.NewInt(42),
		})
		m, ok := got.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, json.Number("9.99"), m["price"])
		assert.Equal(t, int64(42), m["big"])
	})
}

// TestNaNThroughDuckDBExecute proves the fix works end-to-end through a real
// DuckDB connection and Execute, not just the pure NormalizeValue unit —
// matching exactly how RunA2UIQuery/ExecuteQuery hit this (SELECT
// 'nan'::DOUBLE, 1.0/0, -1.0/0), and that the wire value is DuckDB's own
// text, matching ::VARCHAR/CSV export exactly rather than collapsing a
// real value to NULL.
func TestNaNThroughDuckDBExecute(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(sessiontest.ForTests(db))

	for _, tc := range []struct {
		name string
		sql  string
		want any
	}{
		{"nan", "SELECT 'nan'::DOUBLE", "nan"},
		{"+inf", "SELECT 'inf'::DOUBLE", "inf"},
		{"-inf", "SELECT '-inf'::DOUBLE", "-inf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := eng.Execute(context.Background(), tc.sql, nil)
			require.NoError(t, err)
			require.Len(t, rs.Rows, 1)
			assert.Equal(t, tc.want, rs.Rows[0][0])

			var varcharText string
			require.NoError(t, db.QueryRowContext(context.Background(), tc.sql+"::VARCHAR").Scan(&varcharText))
			assert.Equal(t, tc.want, varcharText, "wire text must match DuckDB's own ::VARCHAR text")
		})
	}
}

// TestNaNNestedInCompositeThroughDuckDBExecute is the review fix (round 3,
// blocker 2): a NaN/Inf buried inside a LIST/ARRAY, STRUCT, or MAP crashed
// the app exactly like a bare top-level one — the pre-fix NormalizeValue
// only ever looked at the top-level scanned value, never recursing into a
// []any or map[string]any to sanitize what's inside. Each case here also
// round-trips the result through json.Marshal (the actual failure mode
// live: a NaN unmarshalable value returned from a Wails binding took the
// whole process down, not just this one call) to prove the fix closes that
// loop, not just that NormalizeValue itself returns something marshalable.
// The sanitized value is DuckDB's own text ("nan"/"inf"), not JSON null.
func TestNaNNestedInCompositeThroughDuckDBExecute(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(sessiontest.ForTests(db))

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"list", "SELECT ['nan'::DOUBLE, 1.0] AS l"},
		{"struct", "SELECT {'a': 'inf'::DOUBLE} AS s"},
		{"map", "SELECT MAP {'k': 'nan'::DOUBLE} AS m"},
		{"list of struct", "SELECT [{'x':'nan'::DOUBLE}] AS ls"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := eng.Execute(context.Background(), tc.sql, nil)
			require.NoError(t, err)
			require.Len(t, rs.Rows, 1)

			// Must not panic, and must actually be JSON-marshalable — the
			// literal failure mode this fix closes.
			var data []byte
			require.NotPanics(t, func() {
				data, err = json.Marshal(rs.Rows[0][0])
			})
			require.NoError(t, err, "normalized value must be JSON-marshalable: %#v", rs.Rows[0][0])
			assert.Regexp(t, `"(nan|inf)"`, string(data), "the NaN/Inf element must have sanitized to DuckDB's own text somewhere in %s", string(data))
		})
	}
}

// TestNormalizeColumnValueDate proves DATE renders as "YYYY-MM-DD" through
// the full Execute path (#91), and that a DATE column really does report
// "DATE" from the driver's DatabaseTypeName() — the assumption
// NormalizeColumnValue is keyed on.
func TestNormalizeColumnValueDate(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(sessiontest.ForTests(db))

	rows, err := db.QueryContext(context.Background(), "SELECT DATE '2025-04-06' AS d, TIMESTAMP '2025-04-06 13:45:00' AS ts, NULL::DATE AS n")
	require.NoError(t, err)
	colTypes, err := rows.ColumnTypes()
	require.NoError(t, err)
	require.Equal(t, "DATE", colTypes[0].DatabaseTypeName())
	require.NoError(t, rows.Close())

	rs, err := eng.Execute(context.Background(), "SELECT DATE '2025-04-06' AS d, TIMESTAMP '2025-04-06 13:45:00' AS ts, NULL::DATE AS n", nil)
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)
	assert.Equal(t, "2025-04-06", rs.Rows[0][0])
	// TIMESTAMP renders as DuckDB's own text too (column-aware,
	// via NormalizeColumnValue), not the raw time.Time Go would otherwise
	// JSON-marshal as a false-"Z" RFC3339 string.
	assert.Equal(t, "2025-04-06 13:45:00", rs.Rows[0][1])
	assert.Nil(t, rs.Rows[0][2])
}

// TestDecimalScansAsExactNumber proves the full Execute path returns a
// json.Number carrying DECIMAL's exact scale-padded text, not the
// old lossy float64 (issue #42's original fix, which itself lost precision
// and trailing zeros — {"Width":10,"Scale":2,"Value":999} became 9.99 but
// 15675.30 became 15675.3).
func TestDecimalScansAsExactNumber(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(sessiontest.ForTests(db))

	_, err = db.Exec("CREATE TABLE t42 (price DECIMAL(10,2))")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO t42 VALUES (9.99)")
	require.NoError(t, err)

	rs, err := eng.Execute(context.Background(), "SELECT price FROM t42", nil)
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)

	n, isNumber := rs.Rows[0][0].(json.Number)
	require.True(t, isNumber, "DECIMAL should scan as json.Number, got %T", rs.Rows[0][0])
	assert.Equal(t, json.Number("9.99"), n)
}

// TestWireTextMatchesDuckDB is the oracle test for the wire contract: for
// every scalar literal below, the value ScanRows hands back must print
// (via fmt.Sprint — a plain number or a string either way) exactly what
// DuckDB's own `::VARCHAR` cast produces for the same value. This is the
// ground truth the whole normalization is built to match, not a
// hand-picked expectation.
//
// Composite (LIST/STRUCT) values are intentionally excluded: DuckDB's own
// VARCHAR text for a composite ("[1.50, 2.25]") differs from this
// package's JSON-shaped wire value ([]any{json.Number("1.50"), ...}) by
// design (see the wire contract table in normalize.go), so a textual
// comparison would not be meaningful there. TIMETZ is excluded too: the
// driver drops the zone offset on scan (a documented gap), so its wire
// text can never match DuckDB's ::VARCHAR, which still has the offset.
func TestWireTextMatchesDuckDB(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	literals := []string{
		"9007199254740991::BIGINT",
		"9007199254740993::BIGINT",
		"(-9007199254740993)::BIGINT",
		"18446744073709551615::UBIGINT",
		"170141183460469231731687303715884105727::HUGEINT",
		"(SELECT SUM(x) FROM (VALUES (1),(2)) t(x))",
		"12345678901234567.89::DECIMAL(20,2)",
		"15675.30::DECIMAL(12,2)",
		"0.05::DECIMAL(4,3)",
		"(-12.50)::DECIMAL(10,2)",
		"12.5::DECIMAL(18,4)",
		"(SELECT SUM(x) FROM (VALUES (1.50::DECIMAL(12,2))) t(x))",
		"0.1::FLOAT",
		"1.25::FLOAT",
		"1.5::DOUBLE",
		"(0.1::DOUBLE + 0.2)",
		"1e20::DOUBLE",
		"DATE '2025-04-06'",
		"TIME '13:45:00'",
		"TIME '13:45:00.123456'",
		"TIME '23:59:59.5'",
		"TIMESTAMP '2025-04-06 13:45:00'",
		"TIMESTAMP '2025-04-06 13:45:00.5'",
		"TIMESTAMP_S '2025-04-06 13:45:00'",
		"TIMESTAMP_MS '2025-04-06 13:45:00'",
		"TIMESTAMP_NS '2025-04-06 13:45:00'",
		"INTERVAL '3 days'",
		"INTERVAL '-3 days'",
		"INTERVAL '1 day'",
		"INTERVAL '-1 day'",
		"INTERVAL '1 month'",
		"INTERVAL '-1 year -2 months'",
		"INTERVAL '-90 minutes'",
		"INTERVAL '25 hours'",
		"INTERVAL '1.5 seconds'",
		"INTERVAL '0 seconds'",
		"INTERVAL '1 year 2 months 3 days 04:05:06'",
		"age(TIMESTAMP '2025-04-06', TIMESTAMP '2024-01-01')",
		"'4ac7a9e9-607c-4c8a-84f3-843f0191e3fd'::UUID",
		`'\x00\xFFab'::BLOB`,
		`'a\x5Cb''c"d'::BLOB`,
		"TRUE",
		"FALSE",
		"NULL::VARCHAR",
		"''",
		"'  Kim Park'",
		"'happy'::ENUM('happy','sad')",
	}

	for _, lit := range literals {
		t.Run(lit, func(t *testing.T) {
			rows, err := db.Query(fmt.Sprintf("SELECT (%s) AS v, (%s)::VARCHAR AS t", lit, lit))
			require.NoError(t, err)
			rs, _, err := ScanRows(rows, 0)
			require.NoError(t, err)
			require.Len(t, rs.Rows, 1)

			want, got := rs.Rows[0][1], rs.Rows[0][0]
			if want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, want, fmt.Sprint(got))
		})
	}
}

// TestTimestampTZWireZones covers the TIMESTAMPTZ oracle case separately
// from TestWireTextMatchesDuckDB: it needs to vary time.Local (what
// timestampTZText formats into) alongside DuckDB's own session TimeZone,
// which a single shared *sql.DB in the table-driven loop above can't do
// per-case. One whole-hour zone (Chicago, "-05") and one half-hour zone
// (Kolkata, "+05:30") cover FormatInterval-adjacent §2 offset formatting.
func TestTimestampTZWireZones(t *testing.T) {
	for _, tc := range []struct {
		zone string
		want string
	}{
		{"America/Chicago", "2025-04-06 06:45:00-05"},
		{"Asia/Kolkata", "2025-04-06 17:15:00+05:30"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.zone)
			require.NoError(t, err)
			orig := time.Local
			time.Local = loc
			t.Cleanup(func() { time.Local = orig })

			db, err := sql.Open("duckdb", "")
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			_, err = db.Exec(fmt.Sprintf("SET TimeZone = '%s'", tc.zone))
			require.NoError(t, err)

			rows, err := db.Query("SELECT TIMESTAMPTZ '2025-04-06 13:45:00+02' AS tstz")
			require.NoError(t, err)
			rs, _, err := ScanRows(rows, 0)
			require.NoError(t, err)
			require.Len(t, rs.Rows, 1)
			assert.Equal(t, tc.want, rs.Rows[0][0])
		})
	}
}

// TestWireJSONIsJSSafe is the JS-safety half of the wire contract: it
// pins the exact Go value (and, where it matters, the exact JSON text) for
// every numeric case in the wire contract table, so a value crosses only as
// a JSON number when a JS double holds it exactly.
func TestWireJSONIsJSSafe(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	scan := func(t *testing.T, expr string) any {
		t.Helper()
		rows, err := db.Query("SELECT " + expr + " AS v")
		require.NoError(t, err)
		rs, _, err := ScanRows(rows, 0)
		require.NoError(t, err)
		require.Len(t, rs.Rows, 1)
		return rs.Rows[0][0]
	}

	t.Run("BIGINT at the safe-integer boundary stays a number", func(t *testing.T) {
		v := scan(t, "9007199254740991::BIGINT")
		assert.Equal(t, int64(9007199254740991), v)
		b, err := json.Marshal(v)
		require.NoError(t, err)
		assert.Equal(t, "9007199254740991", string(b))
	})

	t.Run("BIGINT one past the safe-integer boundary becomes a string", func(t *testing.T) {
		v := scan(t, "9007199254740993::BIGINT")
		assert.Equal(t, "9007199254740993", v)
		b, err := json.Marshal(v)
		require.NoError(t, err)
		assert.Equal(t, `"9007199254740993"`, string(b))
	})

	t.Run("negative BIGINT beyond the safe range becomes a string", func(t *testing.T) {
		assert.Equal(t, "-9007199254740993", scan(t, "(-9007199254740993)::BIGINT"))
	})

	t.Run("HUGEINT max becomes a string", func(t *testing.T) {
		v := scan(t, "170141183460469231731687303715884105727::HUGEINT")
		assert.Equal(t, "170141183460469231731687303715884105727", v)
	})

	t.Run("HUGEINT SUM in the safe range stays a number", func(t *testing.T) {
		assert.Equal(t, int64(3), scan(t, "(SELECT SUM(x) FROM (VALUES (1),(2)) t(x))"))
	})

	t.Run("DECIMAL small magnitude is an exact json.Number", func(t *testing.T) {
		v := scan(t, "15675.30::DECIMAL(12,2)")
		assert.Equal(t, json.Number("15675.30"), v)
		b, err := json.Marshal(v)
		require.NoError(t, err)
		assert.Equal(t, "15675.30", string(b))
	})

	t.Run("DECIMAL keeps a leading-zero fractional scale", func(t *testing.T) {
		assert.Equal(t, json.Number("0.050"), scan(t, "0.05::DECIMAL(4,3)"))
	})

	t.Run("negative DECIMAL", func(t *testing.T) {
		assert.Equal(t, json.Number("-12.50"), scan(t, "(-12.50)::DECIMAL(10,2)"))
	})

	t.Run("DECIMAL beyond the safe magnitude becomes a string", func(t *testing.T) {
		v := scan(t, "12345678901234567.89::DECIMAL(20,2)")
		assert.Equal(t, "12345678901234567.89", v)
		b, err := json.Marshal(v)
		require.NoError(t, err)
		assert.Equal(t, `"12345678901234567.89"`, string(b))
	})

	t.Run("DECIMAL(38,2) SUM stays a number", func(t *testing.T) {
		v := scan(t, "(SELECT SUM(x) FROM (VALUES (1.50::DECIMAL(12,2))) t(x))")
		assert.Equal(t, json.Number("1.50"), v)
	})

	t.Run("FLOAT uses its shortest decimal text, not float32-binary noise", func(t *testing.T) {
		assert.Equal(t, 0.1, scan(t, "0.1::FLOAT"))
	})

	t.Run("DECIMAL leaves inside a LIST stay exact", func(t *testing.T) {
		assert.Equal(t, []any{json.Number("1.50")}, scan(t, "[1.5::DECIMAL(4,2)]"))
	})

	t.Run("BIGINT leaf inside a STRUCT", func(t *testing.T) {
		assert.Equal(t, map[string]any{"d": int64(1)}, scan(t, "{'d': 1::BIGINT}"))
	})
}

// TestScanRowsColumnTypes proves ScanRows reports the DuckDB type name
// instead of the Go scan-type name — DECIMAL keeps its scale,
// LIST/UUID get a real name instead of the old goTypeName's "any"/"".
func TestScanRowsColumnTypes(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.Query(`SELECT 15675.30::DECIMAL(12,2) AS amt, [1,2,3] AS tags,
		'4ac7a9e9-607c-4c8a-84f3-843f0191e3fd'::UUID AS u,
		TIMESTAMPTZ '2025-04-06 13:45:00+02' AS tstz`)
	require.NoError(t, err)
	rs, _, err := ScanRows(rows, 0)
	require.NoError(t, err)
	require.Len(t, rs.Columns, 4)
	assert.Equal(t, "DECIMAL(12,2)", rs.Columns[0].Type)
	assert.Equal(t, "INTEGER[]", rs.Columns[1].Type)
	assert.Equal(t, "UUID", rs.Columns[2].Type)
	assert.Equal(t, "TIMESTAMPTZ", rs.Columns[3].Type)
}
