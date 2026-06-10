package engine

import (
	"context"
	"database/sql"
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
