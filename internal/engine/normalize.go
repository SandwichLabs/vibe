package engine

import (
	"fmt"
	"math"
	"math/big"

	duckdb "github.com/marcboeker/go-duckdb/v2"
)

// NormalizeValue maps DuckDB driver-specific scan types onto plain Go values
// that JSON-serialize the way every consumer (Studio bindings, page renderer,
// reports) expects. Without this, a DECIMAL column reaches the frontend as the
// raw duckdb.Decimal struct — `{"Width":10,"Scale":2,"Value":999}` instead of
// 9.99 — and HUGEINT arrives as a *big.Int the JSON encoder renders uselessly.
//
// It also sanitizes non-finite floats (see sanitizeFloat), RECURSIVELY: a
// NaN/Inf buried inside a LIST/ARRAY ([]any), STRUCT (map[string]any), MAP
// (duckdb.Map), or UNION (duckdb.Union) crashed the app exactly like a bare
// top-level one did — `SELECT ['nan'::DOUBLE, 1.0] AS l` scans as
// []any{float64(NaN), float64(1)}, and the old non-recursive version only
// ever looked at the top-level value, missing the NaN nested one level (or
// more — LIST-of-STRUCT-of-MAP nests arbitrarily) inside. Every composite
// case below normalizes its own elements/values through NormalizeValue
// again, so nesting of any depth or combination is covered.
//
// This is the one place scanResultSet (shared by DuckDBEngine and
// SQLiteEngine) funnels every scanned cell through, so ExecuteQuery,
// GetTableData, RunA2UIQuery, and the report/export paths are all covered by
// a single fix rather than needing their own guard against a value
// encoding/json cannot marshal at all.
func NormalizeValue(v any) any {
	switch t := v.(type) {
	case duckdb.Decimal:
		return sanitizeFloat(t.Float64())
	case *duckdb.Decimal:
		if t == nil {
			return nil
		}
		return sanitizeFloat(t.Float64())
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
	case float64:
		return sanitizeFloat(t)
	case float32:
		return sanitizeFloat(float64(t))
	case duckdb.Map:
		// duckdb.Map is map[any]any: a DuckDB MAP key need not be a string,
		// but encoding/json can only marshal a map keyed by string, an
		// integer type, or an encoding.TextMarshaler — normalize the key
		// through fmt.Sprint so this is always JSON-safe regardless of the
		// MAP's declared key type.
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(NormalizeValue(k))] = NormalizeValue(val)
		}
		return out
	case duckdb.Union:
		return NormalizeValue(t.Value)
	case []any:
		// LIST/ARRAY.
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = NormalizeValue(val)
		}
		return out
	case map[string]any:
		// STRUCT.
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = NormalizeValue(val)
		}
		return out
	}
	return v
}

// sanitizeFloat maps a non-finite float64 (NaN, +Inf, -Inf) to nil (JSON
// null).
//
// encoding/json.Marshal does not silently drop a NaN/Inf float — it returns
// a hard *json.UnsupportedValueError, and left unhandled up through Wails'
// own result-marshaling bridge that took the whole Studio process down
// (observed live: `SELECT 'nan'::DOUBLE` reaching RunA2UIQuery/ExecuteQuery
// crashed the app rather than erroring the one call). DuckDB produces these
// readily from ordinary SQL (a bare 'nan'/'inf' cast, 1.0/0, log(-1), an
// empty AVG(), ...), so every row leaving either engine needs this before it
// ever reaches a JSON boundary.
//
// nil rather than the strings "NaN"/"Infinity": every existing consumer
// already has null-handling (formatCell's "—", A2UI stat's null check, a
// table cell's blank), so a sanitized value degrades gracefully instead of
// a caller silently treating the string "NaN" as if it parsed to a number.
func sanitizeFloat(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}
