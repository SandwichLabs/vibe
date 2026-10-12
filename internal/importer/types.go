package importer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
)

// Type is one of the closed, friendly column types the import wizard offers.
// This is the one list mirrored in studio/frontend/src/lib/importTypes.ts —
// #87 reuses it for the Create-table dialog and schema tree.
type Type string

const (
	TypeText      Type = "text"
	TypeInteger   Type = "integer"
	TypeDecimal   Type = "decimal"
	TypeNumber    Type = "number"
	TypeDate      Type = "date"
	TypeTimestamp Type = "timestamp"
	TypeBoolean   Type = "boolean"
)

// Types is the closed, ordered list of friendly types offered by the wizard.
var Types = []Type{TypeText, TypeInteger, TypeDecimal, TypeNumber, TypeDate, TypeTimestamp, TypeBoolean}

// maxDecimalScale is DECIMAL(18,s)'s own ceiling — DuckDB rejects a scale
// bigger than the (fixed, 18) precision/width with a raw "Scale cannot be
// bigger than width" parser error. Import validates a caller-supplied
// Options.Types override against this before it ever reaches DuckDB.
const maxDecimalScale = 18

// ColType is a friendly type plus the scale a Decimal needs. Scale is only
// meaningful (and only serialized) for TypeDecimal.
type ColType struct {
	Type  Type `json:"type"`
	Scale int  `json:"scale,omitempty"`
}

// DuckDB renders the DuckDB column type for c. Decimal always uses precision
// 18 (comfortably covers currency and typical business data) with the
// inferred/chosen scale.
func (c ColType) DuckDB() string {
	switch c.Type {
	case TypeText:
		return "VARCHAR"
	case TypeInteger:
		return "BIGINT"
	case TypeDecimal:
		scale := c.Scale
		if scale <= 0 {
			scale = 2
		}
		return fmt.Sprintf("DECIMAL(18,%d)", scale)
	case TypeNumber:
		return "DOUBLE"
	case TypeDate:
		return "DATE"
	case TypeTimestamp:
		return "TIMESTAMP"
	case TypeBoolean:
		return "BOOLEAN"
	default:
		return "VARCHAR"
	}
}

var decimalTypeRE = regexp.MustCompile(`^DECIMAL\((\d+),\s*(\d+)\)$`)

// FromDuckDB maps a DuckDB type name (as reported by information_schema or
// DESCRIBE) onto the closed friendly type list. ok is false for a type with
// no friendly mapping (TIME, BLOB, LIST, STRUCT, ...) — callers append/upsert
// into such a column by casting straight to its raw DuckDB type instead of
// going through a ColType (see castExprRaw).
func FromDuckDB(t string) (ColType, bool) {
	up := strings.ToUpper(strings.TrimSpace(t))
	switch {
	case engine.IsIntegerType(up):
		return ColType{Type: TypeInteger}, true
	case up == "VARCHAR" || up == "TEXT" || up == "STRING":
		return ColType{Type: TypeText}, true
	case up == "DATE":
		return ColType{Type: TypeDate}, true
	case strings.HasPrefix(up, "TIMESTAMP"):
		// Includes TIMESTAMPTZ — DuckDB converts using the session TimeZone.
		// An edge case, not a feature.
		return ColType{Type: TypeTimestamp}, true
	case up == "BOOLEAN" || up == "BOOL":
		return ColType{Type: TypeBoolean}, true
	case up == "DOUBLE" || up == "FLOAT" || up == "REAL" || up == "FLOAT4" || up == "FLOAT8":
		return ColType{Type: TypeNumber}, true
	default:
		if m := decimalTypeRE.FindStringSubmatch(up); m != nil {
			scale, _ := strconv.Atoi(m[2])
			return ColType{Type: TypeDecimal, Scale: scale}, true
		}
		return ColType{}, false
	}
}

// castExpr is the single place that decides whether a source column's values
// fit losslessly into a target friendly type, and how to convert them.
//
// col is the quoted reference to the source column (e.g. session.QuoteIdent
// applied to the staged column name). srcDuckType is that column's DuckDB
// type as staged (VARCHAR for CSV text, or the sniffed/native type).
//
// value is a SQL expression producing the converted value. fits is a SQL
// boolean expression, true for NULL and for every value that round-trips
// exactly through the target type with no silent rounding or truncation —
// DuckDB's own TRY_CAST silently rounds/truncates by default, so a bare "TRY_CAST(...) IS NOT NULL" is
// NOT enough on its own for integer/decimal/date; each of those adds an
// explicit round-trip or scale check.
func castExpr(col, srcDuckType string, to ColType) (value, fits string) {
	srcUp := strings.ToUpper(strings.TrimSpace(srcDuckType))
	isTextSrc := srcUp == "VARCHAR" || srcUp == "TEXT" || srcUp == "STRING"
	isDoubleSrc := srcUp == "DOUBLE" || srcUp == "FLOAT" || srcUp == "REAL"
	isIntSrc := engine.IsIntegerType(srcUp)

	switch to.Type {
	case TypeInteger:
		value = fmt.Sprintf("TRY_CAST(%s AS BIGINT)", col)
		core := fmt.Sprintf("TRY_CAST(%s AS BIGINT) IS NOT NULL AND TRY_CAST(%s AS DOUBLE) = TRY_CAST(%s AS BIGINT)", col, col, col)
		fits = nullOr(col, core)

	case TypeDecimal:
		scale := to.Scale
		if scale <= 0 {
			scale = 2
		}
		decType := fmt.Sprintf("DECIMAL(18,%d)", scale)
		value = fmt.Sprintf("TRY_CAST(%s AS %s)", col, decType)
		core := fmt.Sprintf("TRY_CAST(%s AS %s) IS NOT NULL", col, decType)
		switch {
		case isTextSrc:
			core += fmt.Sprintf(" AND regexp_full_match(trim(%s), '[-+]?[0-9]*(\\.[0-9]{0,%d})?')", col, scale)
		case isDoubleSrc:
			core += fmt.Sprintf(" AND abs(%s*power(10,%d) - round(%s*power(10,%d))) < 1e-6", col, scale, col, scale)
		}
		fits = nullOr(col, core)

	case TypeNumber:
		value = fmt.Sprintf("TRY_CAST(%s AS DOUBLE)", col)
		core := fmt.Sprintf("TRY_CAST(%s AS DOUBLE) IS NOT NULL", col)
		if !isDoubleSrc {
			// TRY_CAST AS DOUBLE alone silently loses precision past a
			// double's ~15-17 significant decimal digits (a 20-digit id
			// and 9007199254740993 — 2^53+1, from an ordinary BIGINT
			// column, not just text — both parse to A double, just not
			// the exact one the source held, and neither ever errors).
			// Reject any value that needs more than 15 significant
			// digits — a double's SAFE (always-exact) digit count,
			// magnitude-independent since it's counted on the mantissa,
			// stripped of sign/decimal point/exponent/leading zeros — so
			// a value that can't round-trip losslessly never silently
			// becomes Number; it falls through to Text (or is reported
			// as a bad value if the caller explicitly overrode it to
			// Number). Skipped only for a native DOUBLE source (xlsx): a
			// double has no more precision to lose than it already has.
			textCol := col
			if !isTextSrc {
				textCol = fmt.Sprintf("CAST(%s AS VARCHAR)", col)
			}
			core += fmt.Sprintf(
				" AND length(regexp_replace(ltrim(replace(split_part(upper(trim(%s)), 'E', 1), '.', ''), '+-'), '^0+', '')) <= 15",
				textCol)
		}
		fits = nullOr(col, core)

	case TypeDate:
		value = fmt.Sprintf("TRY_CAST(%s AS DATE)", col)
		core := fmt.Sprintf("TRY_CAST(%s AS DATE) IS NOT NULL", col)
		if isTextSrc || strings.HasPrefix(srcUp, "TIMESTAMP") {
			core += fmt.Sprintf(" AND TRY_CAST(%s AS TIMESTAMP) = CAST(TRY_CAST(%s AS DATE) AS TIMESTAMP)", col, col)
		}
		fits = nullOr(col, core)

	case TypeTimestamp:
		value = fmt.Sprintf("TRY_CAST(%s AS TIMESTAMP)", col)
		core := fmt.Sprintf("TRY_CAST(%s AS TIMESTAMP) IS NOT NULL", col)
		if isTextSrc {
			// TRY_CAST(... AS TIMESTAMP) silently DISCARDS a UTC offset
			// instead of applying it or rejecting the value (proven:
			// '2025-01-01 10:00:00+05:00' casts to 10:00:00, not 05:00:00
			// — the offset is dropped, not converted). A plain TIMESTAMP
			// has no offset of its own to store, so any text carrying one
			// must not silently "fit" — the caller needs Text instead
			// (there's no reinterpretation that isn't a silent data
			// change).
			core += fmt.Sprintf(
				" AND NOT regexp_full_match(trim(%s), '.*[+-][0-9]{2}:?[0-9]{2}$') AND NOT regexp_full_match(upper(trim(%s)), '.*Z$')",
				col, col)
		}
		fits = nullOr(col, core)

	case TypeBoolean:
		value = fmt.Sprintf("TRY_CAST(%s AS BOOLEAN)", col)
		core := fmt.Sprintf("TRY_CAST(%s AS BOOLEAN) IS NOT NULL", col)
		if isIntSrc || isDoubleSrc {
			core += fmt.Sprintf(" AND %s IN (0,1)", col)
		}
		fits = nullOr(col, core)

	default: // TypeText
		fits = "true"
		if isDoubleSrc {
			value = fmt.Sprintf(
				"CASE WHEN %s = trunc(%s) THEN CAST(CAST(%s AS BIGINT) AS VARCHAR) ELSE CAST(%s AS VARCHAR) END",
				col, col, col, col)
		} else {
			value = fmt.Sprintf("CAST(%s AS VARCHAR)", col)
		}
	}
	return value, fits
}

// castExprRaw builds a value/fits pair for append/upsert into a target
// column whose DuckDB type has no friendly mapping (FromDuckDB's ok=false
// case, e.g. TIME or BLOB): fits is a plain TRY_CAST round-trip, since there
// is no friendly-type-specific rounding rule to apply.
func castExprRaw(col, toDuckType string) (value, fits string) {
	value = fmt.Sprintf("TRY_CAST(%s AS %s)", col, toDuckType)
	fits = nullOr(col, fmt.Sprintf("TRY_CAST(%s AS %s) IS NOT NULL", col, toDuckType))
	return value, fits
}

// nullOr wraps a core lossless-round-trip check so it also accepts NULL —
// a NULL source value always "fits" (it converts to NULL in every target
// type), the check only needs to gate non-NULL values.
func nullOr(col, core string) string {
	return fmt.Sprintf("(%s IS NULL OR (%s))", col, core)
}
