package renderer

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Format formats a value according to an Evidence-compatible format code.
// Returns empty string for nil values. Unknown codes fall back to fmt.Sprintf("%v", value).
func Format(value any, fmtCode string) string {
	if value == nil {
		return ""
	}

	switch fmtCode {
	// Number formats
	case "num0":
		return formatNumberWithDecimals(ToFloat64(value), 0)
	case "num1":
		return formatNumberWithDecimals(ToFloat64(value), 1)
	case "num2":
		return formatNumberWithDecimals(ToFloat64(value), 2)
	case "num0k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 0, "")
	case "num1k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 1, "")
	case "num0m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 0, "")
	case "num1m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 1, "")
	case "num0b":
		return formatSuffix(ToFloat64(value), 1_000_000_000, "B", 0, "")
	case "num1b":
		return formatSuffix(ToFloat64(value), 1_000_000_000, "B", 1, "")

	// USD formats
	case "usd0":
		return "$" + formatNumberWithDecimals(ToFloat64(value), 0)
	case "usd1":
		return "$" + formatNumberWithDecimals(ToFloat64(value), 1)
	case "usd2":
		return "$" + formatNumberWithDecimals(ToFloat64(value), 2)
	case "usd0k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 0, "$")
	case "usd1k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 1, "$")
	case "usd0m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 0, "$")
	case "usd1m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 1, "$")
	case "usd0b":
		return formatSuffix(ToFloat64(value), 1_000_000_000, "B", 0, "$")
	case "usd1b":
		return formatSuffix(ToFloat64(value), 1_000_000_000, "B", 1, "$")

	// EUR formats
	case "eur0":
		return "\u20ac" + formatNumberWithDecimals(ToFloat64(value), 0)
	case "eur1":
		return "\u20ac" + formatNumberWithDecimals(ToFloat64(value), 1)
	case "eur2":
		return "\u20ac" + formatNumberWithDecimals(ToFloat64(value), 2)
	case "eur0k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 0, "\u20ac")
	case "eur1k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 1, "\u20ac")
	case "eur0m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 0, "\u20ac")
	case "eur1m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 1, "\u20ac")

	// GBP formats
	case "gbp0":
		return "\u00a3" + formatNumberWithDecimals(ToFloat64(value), 0)
	case "gbp1":
		return "\u00a3" + formatNumberWithDecimals(ToFloat64(value), 1)
	case "gbp2":
		return "\u00a3" + formatNumberWithDecimals(ToFloat64(value), 2)
	case "gbp0k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 0, "\u00a3")
	case "gbp1k":
		return formatSuffix(ToFloat64(value), 1_000, "k", 1, "\u00a3")
	case "gbp0m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 0, "\u00a3")
	case "gbp1m":
		return formatSuffix(ToFloat64(value), 1_000_000, "M", 1, "\u00a3")

	// Percent formats (multiply by 100)
	case "pct0":
		return formatNumberWithDecimals(ToFloat64(value)*100, 0) + "%"
	case "pct1":
		return formatNumberWithDecimals(ToFloat64(value)*100, 1) + "%"
	case "pct2":
		return formatNumberWithDecimals(ToFloat64(value)*100, 2) + "%"

	// Date/time formats
	case "shortdate":
		return formatTime(value, "Jan 2, 2006")
	case "longdate":
		return formatTime(value, "January 2, 2006")
	case "hms":
		return formatTime(value, "3:04:05 PM")
	case "hm":
		return formatTime(value, "3:04 PM")

	// Special
	case "id":
		return fmt.Sprint(value)

	default:
		// Empty or unknown code: default formatting
		return fmt.Sprintf("%v", value)
	}
}

// ToFloat64 attempts to convert a value to float64.
// Handles int, int32, int64, float32, float64, and string types.
func ToFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case int16:
		return float64(val)
	case int8:
		return float64(val)
	case uint:
		return float64(val)
	case uint64:
		return float64(val)
	case uint32:
		return float64(val)
	case uint16:
		return float64(val)
	case uint8:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	case interface{ Float64() float64 }:
		// Covers numeric wrapper types like duckdb.Decimal (and *Decimal),
		// which expose a Float64() accessor.
		return val.Float64()
	case fmt.Stringer:
		// Last-resort numeric parse for stringer-backed numeric types.
		var f float64
		fmt.Sscanf(val.String(), "%f", &f)
		return f
	default:
		return 0
	}
}

// formatNumberWithDecimals formats a float64 with comma separators and the specified number of decimal places.
func formatNumberWithDecimals(v float64, decimals int) string {
	negative := v < 0
	v = math.Abs(v)

	// Round half away from zero (matching spreadsheet/Access conventions) rather
	// than Go's default round-half-to-even, so e.g. num0(1234.5) == "1,235".
	pow := math.Pow(10, float64(decimals))
	v = math.Floor(v*pow+0.5) / pow

	// Format with the specified decimal places
	s := fmt.Sprintf("%.*f", decimals, v)

	// Split into integer and decimal parts
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]

	// Add comma separators to the integer part
	intPart = addCommas(intPart)

	if negative {
		intPart = "-" + intPart
	}

	if decimals > 0 && len(parts) == 2 {
		return intPart + "." + parts[1]
	}
	return intPart
}

// addCommas inserts comma separators into an integer string.
func addCommas(s string) string {
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(ch)
	}
	return result.String()
}

// formatSuffix divides a value by the divisor and appends the suffix (k, M, B).
func formatSuffix(v float64, divisor float64, suffix string, decimals int, prefix string) string {
	divided := v / divisor
	s := fmt.Sprintf("%.*f", decimals, divided)
	return prefix + s + suffix
}

// formatTime attempts to convert a value to a time.Time and format it.
func formatTime(value any, layout string) string {
	switch v := value.(type) {
	case time.Time:
		return v.Format(layout)
	case string:
		// Try common date/time formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
			time.RFC1123,
			time.RFC822,
		}
		for _, f := range formats {
			if t, err := time.Parse(f, v); err == nil {
				return t.Format(layout)
			}
		}
		// If parsing fails, return the string as-is
		return v
	default:
		return fmt.Sprintf("%v", value)
	}
}
