package artifacts

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 15, 30, 0, 0, time.UTC) }

func TestResolveDateDefault(t *testing.T) {
	cases := []struct {
		def  string
		now  time.Time
		want string
	}{
		{"today", day(2026, 10, 16), "2026-10-16"},
		{"today-90d", day(2026, 10, 16), "2026-07-18"},
		{"today-90d", day(2026, 3, 10), "2025-12-10"},
		{"today-1d", day(2026, 3, 1), "2026-02-28"},
		{"today-1d", day(2024, 3, 1), "2024-02-29"},     // leap day
		{"today-366d", day(2024, 12, 31), "2023-12-31"}, // across a leap year
		{"today-365d", day(2025, 3, 1), "2024-03-01"},
		{"today-3660d", day(2026, 10, 16), "2016-10-08"},
		{"start-of-month", day(2026, 10, 16), "2026-10-01"},
		{"start-of-year", day(2026, 10, 16), "2026-01-01"},
		{"2020-02-29", day(2026, 10, 16), "2020-02-29"},
	}
	for _, c := range cases {
		got, err := ResolveDateDefault(c.def, c.now)
		require.NoError(t, err, c.def)
		assert.Equal(t, c.want, got, "%s on %s", c.def, c.now.Format("2006-01-02"))
	}
}

func TestResolveDateDefault_UsesTheLocalClock(t *testing.T) {
	loc := time.FixedZone("UTC-8", -8*3600)
	// 03:00 UTC on the 16th is still the 15th at UTC-8.
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC).In(loc)
	got, err := ResolveDateDefault("today", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-15", got)
	got, err = ResolveDateDefault("start-of-month", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-01", got)
}

func TestResolveDateDefault_Refusals(t *testing.T) {
	for _, bad := range []string{"", "tomorrow", "today-0d", "today-3661d", "today-5", "today+3d", "2026-13-01", "2026-02-30", "26-01-01", "2026-01-01T00:00:00Z", " today", "Today"} {
		_, err := ResolveDateDefault(bad, day(2026, 10, 16))
		assert.Error(t, err, "%q", bad)
	}
}

func bindManifest(t *testing.T) *Manifest { return mustDecode(t, goodManifest) }

func TestBind_DefaultsAndTypes(t *testing.T) {
	m := bindManifest(t)
	now := day(2026, 10, 16)
	sql, conn, params, err := Bind(m, "by_region", nil, now)
	require.NoError(t, err)
	assert.Equal(t, m.Queries[1].SQL, sql, "the SQL is returned byte for byte")
	assert.Equal(t, "prod_pg", conn)
	assert.Equal(t, map[string]any{"from": "2026-07-18", "to": "2026-10-16", "top": int64(10)}, params)

	_, conn, params, err = Bind(m, "customers", map[string]any{"region": "West", "from": "2026-01-01"}, now)
	require.NoError(t, err)
	assert.Equal(t, "", conn)
	assert.Equal(t, map[string]any{"from": "2026-01-01", "to": "2026-10-16", "region": "West"}, params)
}

func TestBind_OnlyUsedParamsAreReturnedButAllSuppliedAreChecked(t *testing.T) {
	m := bindManifest(t)
	_, _, params, err := Bind(m, "regions", map[string]any{"top": 5}, day(2026, 10, 16))
	require.NoError(t, err)
	assert.Empty(t, params)

	_, _, _, err = Bind(m, "regions", map[string]any{"top": 500}, day(2026, 10, 16))
	require.Error(t, err, "a value for a parameter the query does not use is still validated")
}

func TestBind_Refusals(t *testing.T) {
	m := bindManifest(t)
	now := day(2026, 10, 16)
	cases := map[string]map[string]any{
		"unknown parameter":       {"nope": "x"},
		"date wrong json type":    {"from": 20260101},
		"date not a date":         {"from": "not-a-date"},
		"date impossible":         {"from": "2026-02-30"},
		"date relative form":      {"from": "today"},
		"date with time":          {"from": "2026-01-01T00:00:00Z"},
		"text over length":        {"note": strings.Repeat("x", 21)},
		"text wrong type":         {"note": 5},
		"integer fractional":      {"top": 2.5},
		"integer string":          {"top": "5"},
		"integer below min":       {"top": 0},
		"integer above max":       {"top": 101},
		"integer over 2^53":       {"top": float64(1 << 54)},
		"integer NaN":             {"top": math.NaN()},
		"number string":           {"share": "0.5"},
		"number inf":              {"share": math.Inf(1)},
		"boolean string":          {"live": "true"},
		"boolean number":          {"live": 1},
		"enum value not a string": {"region": 3},
		"nil":                     {"region": nil},
		"slice":                   {"region": []string{"a"}},
		"map":                     {"region": map[string]any{"a": 1}},
	}
	for name, vals := range cases {
		_, _, _, err := Bind(m, "customers", vals, now)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
	_, _, _, err := Bind(m, "nope", nil, now)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestBind_StaticEnumOnly(t *testing.T) {
	m := mustDecode(t, "version: 1\ntitle: T\nparameters:\n  - name: r\n    type: enum\n    options: [N, S]\n    default: N\nqueries:\n  - name: q\n    sql: SELECT $r\n")
	_, _, params, err := Bind(m, "q", map[string]any{"r": "S"}, day(2026, 1, 1))
	require.NoError(t, err)
	assert.Equal(t, "S", params["r"])
	_, _, _, err = Bind(m, "q", map[string]any{"r": "E"}, day(2026, 1, 1))
	require.Error(t, err)
	_, _, _, err = Bind(m, "q", map[string]any{"r": "n"}, day(2026, 1, 1))
	require.Error(t, err, "options are case sensitive")

	// An options_query enum takes any string (its rows are data): still only a
	// bound string, capped.
	m2 := bindManifest(t)
	_, _, params, err = Bind(m2, "customers", map[string]any{"region": "'; DROP TABLE x; --"}, day(2026, 1, 1))
	require.NoError(t, err)
	assert.Equal(t, "'; DROP TABLE x; --", params["region"])
	_, _, _, err = Bind(m2, "customers", map[string]any{"region": strings.Repeat("x", 1001)}, day(2026, 1, 1))
	require.Error(t, err)
}

func TestBind_ValuesNeverReachSQLText(t *testing.T) {
	m := bindManifest(t)
	hostile := "x'; DROP TABLE sales.orders; --"
	sql, _, params, err := Bind(m, "customers", map[string]any{"region": hostile}, day(2026, 10, 16))
	require.NoError(t, err)
	assert.Equal(t, m.Queries[2].SQL, sql)
	assert.NotContains(t, sql, "DROP")
	assert.Equal(t, hostile, params["region"])
	// The manifest itself is untouched.
	assert.NotContains(t, m.Queries[2].SQL, "DROP")
}

func TestBind_ReDerivesWhatAQueryUses(t *testing.T) {
	// A manifest built in Go and never validated: Bind does not trust a stale or
	// empty Uses, and an undeclared $name is an error, not a missing arg.
	d := "x"
	m := &Manifest{Version: 1, Title: "T", Parameters: []Param{{Name: "a", Type: TypeText, Default: &d}},
		Queries: []Query{{Name: "q", SQL: "SELECT $a, $b", Uses: []string{"a"}}}}
	_, _, _, err := Bind(m, "q", nil, day(2026, 1, 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$b")

	m.Queries[0].SQL = "SELECT $a"
	m.Queries[0].Uses = nil
	_, _, params, err := Bind(m, "q", nil, day(2026, 1, 1))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": "x"}, params)
}

func TestBind_JSONShapedValues(t *testing.T) {
	m := bindManifest(t)
	var vals map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"top": 25, "share": 0.25, "live": false, "note": "hi"}`), &vals))
	_, _, params, err := Bind(m, "by_region", vals, day(2026, 10, 16))
	require.NoError(t, err)
	assert.Equal(t, int64(25), params["top"], "a JSON number for an integer becomes int64")

	dec := json.NewDecoder(strings.NewReader(`{"top": 9007199254740993}`))
	dec.UseNumber()
	var big map[string]any
	require.NoError(t, dec.Decode(&big))
	_, _, _, err = Bind(m, "by_region", big, day(2026, 10, 16))
	require.Error(t, err, "json.Number beyond 2^53 is refused exactly, not rounded")

	_, _, params, err = Bind(m, "by_region", map[string]any{"top": json.Number("7")}, day(2026, 10, 16))
	require.NoError(t, err)
	assert.Equal(t, int64(7), params["top"])
	_, _, params, err = Bind(m, "by_region", map[string]any{"top": json.Number("7.0")}, day(2026, 10, 16))
	require.NoError(t, err)
	assert.Equal(t, int64(7), params["top"])
}

func TestCoerceTypes(t *testing.T) {
	d := ""
	num := &Param{Name: "n", Type: TypeNumber, Default: &d}
	v, err := coerceValue(num, 3)
	require.NoError(t, err)
	assert.Equal(t, float64(3), v)
	v, err = coerceValue(num, int64(4))
	require.NoError(t, err)
	assert.Equal(t, float64(4), v)

	b := &Param{Name: "b", Type: TypeBoolean, Default: &d}
	v, err = coerceValue(b, true)
	require.NoError(t, err)
	assert.Equal(t, true, v)

	txt := &Param{Name: "t", Type: TypeText, Default: &d}
	_, err = coerceValue(txt, "a\x00b")
	assert.Error(t, err, "NUL")
	_, err = coerceValue(txt, string([]byte{0xff, 0xfe}))
	assert.Error(t, err, "invalid UTF-8")
	v, err = coerceValue(txt, strings.Repeat("é", 1000))
	require.NoError(t, err, "length counts characters")
	assert.Len(t, v, 2000)
}
