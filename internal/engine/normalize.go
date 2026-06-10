package engine

import (
	"math/big"

	duckdb "github.com/marcboeker/go-duckdb/v2"
)

// NormalizeValue maps DuckDB driver-specific scan types onto plain Go values
// that JSON-serialize the way every consumer (Studio bindings, page renderer,
// reports) expects. Without this, a DECIMAL column reaches the frontend as the
// raw duckdb.Decimal struct — `{"Width":10,"Scale":2,"Value":999}` instead of
// 9.99 — and HUGEINT arrives as a *big.Int the JSON encoder renders uselessly.
func NormalizeValue(v any) any {
	switch t := v.(type) {
	case duckdb.Decimal:
		return t.Float64()
	case *duckdb.Decimal:
		if t == nil {
			return nil
		}
		return t.Float64()
	case *big.Int:
		if t == nil {
			return nil
		}
		if t.IsInt64() {
			return t.Int64()
		}
		return t.String()
	case big.Int:
		if t.IsInt64() {
			return t.Int64()
		}
		return t.String()
	}
	return v
}
