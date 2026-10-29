package queries

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func paramQuery() *Query {
	return &Query{
		SQL: "SELECT * FROM t WHERE n >= $min_n AND label = $label AND d >= $since AND ok = $flag",
		Parameters: []Param{
			{Name: "min_n", Type: "integer", Default: "5"},
			{Name: "label", Type: "text", Default: "x"},
			{Name: "since", Type: "date", Default: "2026-01-31"},
			{Name: "flag", Type: "boolean", Default: "true"},
		},
	}
}

func TestBind_DefaultsAndTypedValuesNeverBecomeSQLText(t *testing.T) {
	q := paramQuery()
	params, err := Bind(q, nil, BindOptions{Strict: true})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"min_n": int64(5), "label": "x", "since": "2026-01-31", "flag": true}, params)

	evil := "x'; DROP TABLE t; --"
	params, err = Bind(q, map[string]any{"label": evil, "min_n": float64(9)}, BindOptions{})
	require.NoError(t, err)
	assert.Equal(t, evil, params["label"])
	assert.Equal(t, int64(9), params["min_n"])
}

func TestBind_Refusals(t *testing.T) {
	q := paramQuery()
	_, err := Bind(q, map[string]any{"min_n": "9"}, BindOptions{})
	require.Error(t, err, "a string is not an integer")
	_, err = Bind(q, map[string]any{"nope": 1}, BindOptions{})
	require.ErrorContains(t, err, `no parameter named "nope"`)
	_, err = Bind(q, map[string]any{"since": "2026-02-30"}, BindOptions{})
	require.Error(t, err)

	undeclared := &Query{SQL: "SELECT $a"}
	_, err = Bind(undeclared, nil, BindOptions{})
	require.ErrorContains(t, err, "not a declared parameter")
	_, err = Bind(undeclared, nil, BindOptions{Strict: true})
	require.ErrorContains(t, err, "not a declared parameter")

	unused := &Query{SQL: "SELECT 1", Parameters: []Param{{Name: "a", Type: "text", Default: "x"}}}
	_, err = Bind(unused, nil, BindOptions{Strict: true})
	require.ErrorContains(t, err, "never uses $a")
	_, err = Bind(unused, nil, BindOptions{})
	require.NoError(t, err, "a run may leave a declared parameter unused")
}

func TestBind_RequiredWithoutDefaultNeedsAValue(t *testing.T) {
	q := &Query{SQL: "SELECT $a", Parameters: []Param{{Name: "a", Type: "text", Required: true}}}
	_, err := Bind(q, nil, BindOptions{})
	require.Error(t, err)
	params, err := Bind(q, map[string]any{"a": "v"}, BindOptions{})
	require.NoError(t, err)
	assert.Equal(t, "v", params["a"])
}

func TestParseValues_TypesByDeclaration(t *testing.T) {
	q := paramQuery()
	v, err := ParseValues(q, map[string]string{"min_n": "7", "flag": "false", "label": "a=b", "since": "2026-03-01"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"min_n": int64(7), "flag": false, "label": "a=b", "since": "2026-03-01"}, v)

	_, err = ParseValues(q, map[string]string{"min_n": "seven"})
	require.ErrorContains(t, err, "whole number")
	_, err = ParseValues(q, map[string]string{"flag": "yes"})
	require.ErrorContains(t, err, "true or false")
	_, err = ParseValues(q, map[string]string{"zzz": "1"})
	require.ErrorContains(t, err, "declares: min_n, label, since, flag")
	_, err = ParseValues(&Query{}, map[string]string{"zzz": "1"})
	require.ErrorContains(t, err, "declares: none")
}

func TestBind_RefusesTextBindParamsWouldRewriteOrSplit(t *testing.T) {
	decl := []Param{{Name: "q", Type: "text", Default: "x"}}
	for name, sqlText := range map[string]string{
		"parameter as query() argument":  "SELECT * FROM query($q)",
		"parameter in a reader":          "SELECT * FROM read_csv($q)",
		"placeholder in a literal":       "SELECT '$q' AS s, $q",
		"dollar-quoted second statement": "SELECT $pé$; CREATE SECRET pwn (TYPE s3); --$pé$",
		"bare question mark":             "SELECT ?, $q",
		"positional parameter":           "SELECT $1, $q",
		"two statements":                 "SELECT $q; SELECT 2",
	} {
		_, err := Bind(&Query{SQL: sqlText, Parameters: decl}, nil, BindOptions{})
		assert.Error(t, err, name)
	}
	_, err := Bind(&Query{SQL: "SELECT $q", Parameters: decl}, nil, BindOptions{})
	assert.NoError(t, err)
}

func TestBind_ValidatesTheDeclarations(t *testing.T) {
	_, err := Bind(&Query{SQL: "SELECT $a", Parameters: []Param{{Name: "a", Type: "blob", Default: "x"}}}, nil, BindOptions{})
	require.ErrorContains(t, err, "parameters are not valid")
	_, err = Bind(&Query{SQL: "SELECT $a", Parameters: []Param{{Name: "bad-name", Type: "text", Default: "x"}}}, nil, BindOptions{})
	require.ErrorContains(t, err, "parameters are not valid")
}

func TestParseValues_NumbersUseTheDecimalShape(t *testing.T) {
	q := &Query{Parameters: []Param{{Name: "n", Type: "number"}, {Name: "i", Type: "integer"}}}
	for _, bad := range []string{"0x10", "1_000", "inf", "NaN", "1e", ".5"} {
		_, err := ParseValues(q, map[string]string{"n": bad})
		assert.Error(t, err, bad)
	}
	_, err := ParseValues(q, map[string]string{"i": "1_0"})
	assert.Error(t, err)
	v, err := ParseValues(q, map[string]string{"n": "-1.5e3", "i": "-4"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"n": -1500.0, "i": int64(-4)}, v)
}
