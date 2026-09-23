package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"math/big"
	"testing"

	duckdb "github.com/marcboeker/go-duckdb/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeValueDecimal(t *testing.T) {
	d := duckdb.Decimal{Width: 10, Scale: 2, Value: big.NewInt(999)}
	assert.InDelta(t, 9.99, NormalizeValue(d), 0.0001)
	assert.InDelta(t, 9.99, NormalizeValue(&d), 0.0001)

	var nilDec *duckdb.Decimal
	assert.Nil(t, NormalizeValue(nilDec))
}

func TestNormalizeValueBigInt(t *testing.T) {
	assert.Equal(t, int64(42), NormalizeValue(big.NewInt(42)))

	huge, ok := new(big.Int).SetString("170141183460469231731687303715884105727", 10)
	require.True(t, ok)
	assert.Equal(t, "170141183460469231731687303715884105727", NormalizeValue(huge))

	var nilBig *big.Int
	assert.Nil(t, NormalizeValue(nilBig))
}

func TestNormalizeValuePassthrough(t *testing.T) {
	assert.Equal(t, "x", NormalizeValue("x"))
	assert.Equal(t, 1.5, NormalizeValue(1.5))
	assert.Nil(t, NormalizeValue(nil))
}

// TestNormalizeValueNonFiniteFloat is the review fix for the crash a NaN or
// ±Inf query result caused: encoding/json.Marshal errors hard on any of the
// three (not a silent omission), which took the whole Studio process down
// when it surfaced unhandled through Wails' own result-marshaling bridge
// (RunA2UIQuery/ExecuteQuery). NormalizeValue must map all three to nil
// before a row ever reaches a JSON boundary.
func TestNormalizeValueNonFiniteFloat(t *testing.T) {
	assert.Nil(t, NormalizeValue(math.NaN()))
	assert.Nil(t, NormalizeValue(math.Inf(1)))
	assert.Nil(t, NormalizeValue(math.Inf(-1)))
	assert.Nil(t, NormalizeValue(float32(math.NaN())))
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
		assert.Equal(t, []any{nil, 1.0, "x"}, got)
	})
	t.Run("nested list", func(t *testing.T) {
		got := NormalizeValue([]any{[]any{math.Inf(1), 2.0}})
		assert.Equal(t, []any{[]any{nil, 2.0}}, got)
	})
	t.Run("struct (map[string]any)", func(t *testing.T) {
		got := NormalizeValue(map[string]any{"a": math.NaN(), "b": "ok"})
		assert.Equal(t, map[string]any{"a": nil, "b": "ok"}, got)
	})
	t.Run("list of struct", func(t *testing.T) {
		got := NormalizeValue([]any{map[string]any{"x": math.NaN()}})
		assert.Equal(t, []any{map[string]any{"x": nil}}, got)
	})
	t.Run("duckdb.Map", func(t *testing.T) {
		got := NormalizeValue(duckdb.Map{"k": math.NaN()})
		assert.Equal(t, map[string]any{"k": nil}, got)
	})
	t.Run("duckdb.Union", func(t *testing.T) {
		got := NormalizeValue(duckdb.Union{Tag: "d", Value: math.Inf(-1)})
		assert.Nil(t, got)
	})
	t.Run("struct containing a Decimal and a big.Int", func(t *testing.T) {
		got := NormalizeValue(map[string]any{
			"price": duckdb.Decimal{Width: 10, Scale: 2, Value: big.NewInt(999)},
			"big":   big.NewInt(42),
		})
		m, ok := got.(map[string]any)
		require.True(t, ok)
		assert.InDelta(t, 9.99, m["price"], 0.0001)
		assert.Equal(t, int64(42), m["big"])
	})
}

// TestNaNThroughDuckDBExecute proves the fix works end-to-end through a real
// DuckDB connection and Execute, not just the pure NormalizeValue unit —
// matching exactly how RunA2UIQuery/ExecuteQuery hit this (SELECT
// 'nan'::DOUBLE, 1.0/0, -1.0/0).
func TestNaNThroughDuckDBExecute(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(db)

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"nan", "SELECT 'nan'::DOUBLE"},
		{"+inf", "SELECT 'inf'::DOUBLE"},
		{"-inf", "SELECT '-inf'::DOUBLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := eng.Execute(context.Background(), tc.sql, nil)
			require.NoError(t, err)
			require.Len(t, rs.Rows, 1)
			assert.Nil(t, rs.Rows[0][0])
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
// loop, not just that NormalizeValue itself returns nil somewhere.
func TestNaNNestedInCompositeThroughDuckDBExecute(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(db)

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
			assert.Contains(t, string(data), "null", "the NaN/Inf element must have sanitized to JSON null somewhere in %s", string(data))
		})
	}
}

// TestDecimalScansAsFloat proves the full Execute path returns a plain float64
// for a DECIMAL column (issue #42: it previously surfaced as duckdb.Decimal,
// which JSON-marshals as {"Width":10,"Scale":2,"Value":999}).
func TestDecimalScansAsFloat(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	eng := NewDuckDBEngine(db)

	_, err = db.Exec("CREATE TABLE t42 (price DECIMAL(10,2))")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO t42 VALUES (9.99)")
	require.NoError(t, err)

	rs, err := eng.Execute(context.Background(), "SELECT price FROM t42", nil)
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)

	v, isFloat := rs.Rows[0][0].(float64)
	require.True(t, isFloat, "DECIMAL should scan as float64, got %T", rs.Rows[0][0])
	assert.InDelta(t, 9.99, v, 0.0001)
}
