package renderer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormat_Nil(t *testing.T) {
	assert.Equal(t, "", Format(nil, "num0"))
	assert.Equal(t, "", Format(nil, "usd2"))
	assert.Equal(t, "", Format(nil, "pct1"))
	assert.Equal(t, "", Format(nil, ""))
}

func TestFormat_NumberFormats(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		code   string
		expect string
	}{
		{"num0 integer", 1234, "num0", "1,234"},
		{"num0 float", 1234.56, "num0", "1,235"},
		{"num1", 1234.56, "num1", "1,234.6"},
		{"num2", 1234.56, "num2", "1,234.56"},
		{"num0 small", 42, "num0", "42"},
		{"num2 zero", 0.0, "num2", "0.00"},
		{"num0 negative", -1234, "num0", "-1,234"},
		{"num2 negative", -1234.56, "num2", "-1,234.56"},
		{"num0 large", 1234567, "num0", "1,234,567"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, Format(tt.value, tt.code))
		})
	}
}

func TestFormat_SuffixFormats(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		code   string
		expect string
	}{
		{"num0k", 1500.0, "num0k", "2k"},
		{"num1k", 1234.0, "num1k", "1.2k"},
		{"num0m", 1500000.0, "num0m", "2M"},
		{"num1m", 1234567.0, "num1m", "1.2M"},
		{"num0b", 1500000000.0, "num0b", "2B"},
		{"num1b", 1234567890.0, "num1b", "1.2B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, Format(tt.value, tt.code))
		})
	}
}

func TestFormat_USDFormats(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		code   string
		expect string
	}{
		{"usd0", 1234, "usd0", "$1,234"},
		{"usd1", 1234.5, "usd1", "$1,234.5"},
		{"usd2", 1234.56, "usd2", "$1,234.56"},
		{"usd0k", 1500.0, "usd0k", "$2k"},
		{"usd1k", 1234.0, "usd1k", "$1.2k"},
		{"usd0m", 1500000.0, "usd0m", "$2M"},
		{"usd1m", 1234567.0, "usd1m", "$1.2M"},
		{"usd0b", 1500000000.0, "usd0b", "$2B"},
		{"usd1b", 1234567890.0, "usd1b", "$1.2B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, Format(tt.value, tt.code))
		})
	}
}

func TestFormat_EURFormats(t *testing.T) {
	assert.Equal(t, "\u20ac1,234", Format(1234, "eur0"))
	assert.Equal(t, "\u20ac1,234.5", Format(1234.5, "eur1"))
	assert.Equal(t, "\u20ac1,234.56", Format(1234.56, "eur2"))
	assert.Equal(t, "\u20ac1k", Format(1000.0, "eur0k"))
	assert.Equal(t, "\u20ac1.2k", Format(1234.0, "eur1k"))
	assert.Equal(t, "\u20ac1M", Format(1000000.0, "eur0m"))
	assert.Equal(t, "\u20ac1.2M", Format(1234567.0, "eur1m"))
}

func TestFormat_GBPFormats(t *testing.T) {
	assert.Equal(t, "\u00a31,234", Format(1234, "gbp0"))
	assert.Equal(t, "\u00a31,234.5", Format(1234.5, "gbp1"))
	assert.Equal(t, "\u00a31,234.56", Format(1234.56, "gbp2"))
	assert.Equal(t, "\u00a31k", Format(1000.0, "gbp0k"))
	assert.Equal(t, "\u00a31.2k", Format(1234.0, "gbp1k"))
}

func TestFormat_PercentFormats(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		code   string
		expect string
	}{
		{"pct0", 0.12, "pct0", "12%"},
		{"pct1", 0.123, "pct1", "12.3%"},
		{"pct2", 0.1234, "pct2", "12.34%"},
		{"pct0 whole", 1.0, "pct0", "100%"},
		{"pct2 small", 0.005, "pct2", "0.50%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, Format(tt.value, tt.code))
		})
	}
}

func TestFormat_DateTimeFormats(t *testing.T) {
	ref := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)

	assert.Equal(t, "Jan 2, 2006", Format(ref, "shortdate"))
	assert.Equal(t, "January 2, 2006", Format(ref, "longdate"))
	assert.Equal(t, "3:04:05 PM", Format(ref, "hms"))
	assert.Equal(t, "3:04 PM", Format(ref, "hm"))
}

func TestFormat_DateTimeFromString(t *testing.T) {
	assert.Equal(t, "Jan 2, 2006", Format("2006-01-02T15:04:05Z", "shortdate"))
	assert.Equal(t, "January 2, 2006", Format("2006-01-02", "longdate"))
	assert.Equal(t, "3:04 PM", Format("2006-01-02T15:04:05", "hm"))
}

func TestFormat_DateTimeUnparseable(t *testing.T) {
	// Unparseable strings are returned as-is
	assert.Equal(t, "not-a-date", Format("not-a-date", "shortdate"))
}

func TestFormat_ID(t *testing.T) {
	assert.Equal(t, "42", Format(42, "id"))
	assert.Equal(t, "hello", Format("hello", "id"))
	assert.Equal(t, "3.14", Format(3.14, "id"))
}

func TestFormat_EmptyCode(t *testing.T) {
	assert.Equal(t, "42", Format(42, ""))
	assert.Equal(t, "hello", Format("hello", ""))
}

func TestFormat_UnknownCode(t *testing.T) {
	assert.Equal(t, "42", Format(42, "unknown"))
	assert.Equal(t, "hello", Format("hello", "unknown"))
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		expect float64
	}{
		{"float64", 3.14, 3.14},
		{"float32", float32(3.14), float64(float32(3.14))},
		{"int", 42, 42.0},
		{"int64", int64(42), 42.0},
		{"int32", int32(42), 42.0},
		{"int16", int16(42), 42.0},
		{"int8", int8(42), 42.0},
		{"uint", uint(42), 42.0},
		{"uint64", uint64(42), 42.0},
		{"uint32", uint32(42), 42.0},
		{"uint16", uint16(42), 42.0},
		{"uint8", uint8(42), 42.0},
		{"string", "3.14", 3.14},
		{"string invalid", "abc", 0.0},
		{"nil", nil, 0.0},
		{"bool", true, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expect, ToFloat64(tt.value), 0.001)
		})
	}
}

func TestFormat_TypeConversions(t *testing.T) {
	// Various numeric types should work with number formats
	assert.Equal(t, "42", Format(int32(42), "num0"))
	assert.Equal(t, "42", Format(int64(42), "num0"))
	assert.Equal(t, "42.00", Format(float32(42), "num2"))
	assert.Equal(t, "1,234", Format("1234", "num0"))
}
