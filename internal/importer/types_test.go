package importer

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"
)

func openDuckDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// evalFitsValue evaluates castExpr's value/fits expressions against a single
// literal source SQL expression (e.g. "'5.5'" or "5.5::DOUBLE"), returning
// whether it fits and, if so, the converted value rendered as text.
func evalFitsValue(t *testing.T, db *sql.DB, srcLiteral, srcDuckType string, to ColType) (fits bool, value string) {
	t.Helper()
	valExpr, fitsExpr := castExpr("src", srcDuckType, to)
	query := fmt.Sprintf("SELECT %s, CAST(%s AS VARCHAR) FROM (SELECT %s AS src)", fitsExpr, valExpr, srcLiteral)
	var fitsVal sql.NullBool
	var valVal sql.NullString
	require.NoError(t, db.QueryRow(query).Scan(&fitsVal, &valVal))
	return fitsVal.Valid && fitsVal.Bool, valVal.String
}

func TestCastExpr_Integer(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeInteger}

	fits, _ := evalFitsValue(t, db, "'5.5'", "VARCHAR", to)
	require.False(t, fits, "'5.5' should not fit integer")

	fits, _ = evalFitsValue(t, db, "5.5::DOUBLE", "DOUBLE", to)
	require.False(t, fits, "5.5 (double) should not fit integer")

	fits, val := evalFitsValue(t, db, "'5.0'", "VARCHAR", to)
	require.True(t, fits)
	require.Equal(t, "5", val)

	fits, val = evalFitsValue(t, db, "42", "BIGINT", to)
	require.True(t, fits)
	require.Equal(t, "42", val)

	// NULL always fits.
	fits, _ = evalFitsValue(t, db, "NULL::VARCHAR", "VARCHAR", to)
	require.True(t, fits)
}

func TestCastExpr_Decimal(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeDecimal, Scale: 2}

	fits, _ := evalFitsValue(t, db, "'1.234'", "VARCHAR", to)
	require.False(t, fits, "'1.234' should not fit decimal(2)")

	fits, val := evalFitsValue(t, db, "'100.32'", "VARCHAR", to)
	require.True(t, fits)
	require.Equal(t, "100.32", val)

	fits, val = evalFitsValue(t, db, "100.32::DOUBLE", "DOUBLE", to)
	require.True(t, fits)
	require.Equal(t, "100.32", val)

	fits, val = evalFitsValue(t, db, "0.30000000000000004::DOUBLE", "DOUBLE", to)
	require.True(t, fits)
	require.Equal(t, "0.30", val)

	fits, _ = evalFitsValue(t, db, "'12345678901234567.89'", "VARCHAR", to)
	require.False(t, fits, "overflow must not silently pass")
}

func TestCastExpr_Date(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeDate}

	fits, _ := evalFitsValue(t, db, "'2025-01-01 10:00'", "VARCHAR", to)
	require.False(t, fits, "a date with a time part must not fit")

	fits, val := evalFitsValue(t, db, "'2025-01-01'", "VARCHAR", to)
	require.True(t, fits)
	require.Equal(t, "2025-01-01", val)
}

func TestCastExpr_Boolean(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeBoolean}

	fits, _ := evalFitsValue(t, db, "'2'", "VARCHAR", to)
	require.False(t, fits)

	fits, _ = evalFitsValue(t, db, "2::DOUBLE", "DOUBLE", to)
	require.False(t, fits)

	fits, val := evalFitsValue(t, db, "1::BIGINT", "BIGINT", to)
	require.True(t, fits)
	require.Equal(t, "true", val)
}

func TestCastExpr_Text(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeText}

	fits, val := evalFitsValue(t, db, "2134::DOUBLE", "DOUBLE", to)
	require.True(t, fits)
	require.Equal(t, "2134", val, "whole doubles must not render with a trailing .0")

	fits, val = evalFitsValue(t, db, "2134.5::DOUBLE", "DOUBLE", to)
	require.True(t, fits)
	require.Equal(t, "2134.5", val)
}

func TestCastExpr_Number(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeNumber}
	fits, val := evalFitsValue(t, db, "'3.14159265'", "VARCHAR", to)
	require.True(t, fits)
	require.Equal(t, "3.14159265", val)
}

// TestCastExpr_Number_RejectsPastDoubleRoundTrip proves a text value that
// can't round-trip losslessly through a double (more significant digits
// than a double guarantees, ~15-17) never silently "fits" Number — a
// 20-digit account id and 9007199254740993 (2^53+1, not exactly
// representable) both silently landed on a NEARBY double before this fix,
// with no error and no indication either value changed.
func TestCastExpr_Number_RejectsPastDoubleRoundTrip(t *testing.T) {
	db := openDuckDB(t)
	to := ColType{Type: TypeNumber}

	fits, _ := evalFitsValue(t, db, "'12345678901234567890'", "VARCHAR", to)
	require.False(t, fits, "a 20-digit id must not silently fit Number")

	fits, _ = evalFitsValue(t, db, "'9007199254740993'", "VARCHAR", to)
	require.False(t, fits, "9007199254740993 (2^53+1) is not exactly representable as a double")

	// A native DOUBLE source (xlsx: no separate original text) has no MORE
	// precision to lose than it already has, so the round-trip check is
	// only applied to text sources — Number always fits here.
	fits, _ = evalFitsValue(t, db, "9007199254740993::DOUBLE", "DOUBLE", to)
	require.True(t, fits)

	// Ordinary decimals well within a double's exact range still fit.
	fits, val := evalFitsValue(t, db, "'100.32'", "VARCHAR", to)
	require.True(t, fits)
	require.Equal(t, "100.32", val)
}

func TestFromDuckDB_RoundTrip(t *testing.T) {
	cases := []struct {
		duck string
		want ColType
	}{
		{"VARCHAR", ColType{Type: TypeText}},
		{"BIGINT", ColType{Type: TypeInteger}},
		{"INTEGER", ColType{Type: TypeInteger}},
		{"DECIMAL(18,2)", ColType{Type: TypeDecimal, Scale: 2}},
		{"DECIMAL(12,2)", ColType{Type: TypeDecimal, Scale: 2}},
		{"DOUBLE", ColType{Type: TypeNumber}},
		{"DATE", ColType{Type: TypeDate}},
		{"TIMESTAMP", ColType{Type: TypeTimestamp}},
		{"TIMESTAMPTZ", ColType{Type: TypeTimestamp}},
		{"BOOLEAN", ColType{Type: TypeBoolean}},
	}
	for _, c := range cases {
		got, ok := FromDuckDB(c.duck)
		require.True(t, ok, c.duck)
		require.Equal(t, c.want, got, c.duck)
	}

	_, ok := FromDuckDB("TIME")
	require.False(t, ok)
	_, ok = FromDuckDB("BLOB")
	require.False(t, ok)
}

func TestColType_DuckDB(t *testing.T) {
	require.Equal(t, "VARCHAR", ColType{Type: TypeText}.DuckDB())
	require.Equal(t, "BIGINT", ColType{Type: TypeInteger}.DuckDB())
	require.Equal(t, "DECIMAL(18,2)", ColType{Type: TypeDecimal, Scale: 2}.DuckDB())
	require.Equal(t, "DOUBLE", ColType{Type: TypeNumber}.DuckDB())
	require.Equal(t, "DATE", ColType{Type: TypeDate}.DuckDB())
	require.Equal(t, "TIMESTAMP", ColType{Type: TypeTimestamp}.DuckDB())
	require.Equal(t, "BOOLEAN", ColType{Type: TypeBoolean}.DuckDB())
}
