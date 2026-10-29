package repl

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
)

var update = flag.Bool("update", false, "rewrite golden files")

func col(name, typ string) engine.Column { return engine.Column{Name: name, Type: typ} }

// rangeRS is a one-column BIGINT result of n rows: 0..n-1.
func rangeRS(n int) *engine.ResultSet {
	rs := &engine.ResultSet{Columns: []engine.Column{col("range", "BIGINT")}}
	for i := 0; i < n; i++ {
		rs.Rows = append(rs.Rows, []any{int64(i)})
	}
	return rs
}

type renderCase struct {
	name  string
	rs    *engine.ResultSet
	total int // 0: len(rs.Rows)
	st    func(*settings)
	width int // 0: 80
}

func simpleRS() *engine.ResultSet {
	return &engine.ResultSet{
		Columns: []engine.Column{col("a", "INTEGER"), col("b", "VARCHAR"), col("c", "NULL"), col("d", "DECIMAL(2,1)"), col("e", "DATE")},
		Rows:    [][]any{{int64(1), "x", nil, "1.5", "2024-01-02"}},
	}
}

func abcRS() *engine.ResultSet {
	return &engine.ResultSet{
		Columns: []engine.Column{col("a", "INTEGER"), col("b", "VARCHAR"), col("c", "VARCHAR")},
		Rows:    [][]any{{int64(1), "x,y", nil}, {int64(2), "z", "w"}},
	}
}

func renderCases() map[string][]renderCase {
	mode := func(m mode) func(*settings) { return func(s *settings) { s.mode = m } }
	return map[string][]renderCase{
		"duckbox": {
			{name: "simple", rs: simpleRS()},
			{name: "range100", rs: rangeRS(100)},
			{name: "maxrows9", rs: rangeRS(100), st: func(s *settings) { s.maxrows = 9 }},
			{name: "empty", rs: &engine.ResultSet{Columns: []engine.Column{col("x", "INTEGER")}}},
			{name: "beyond_fetch_cap", rs: rangeRS(50), total: 20000},
			{name: "wide_elided", rs: &engine.ResultSet{
				Columns: []engine.Column{col("alpha", "VARCHAR"), col("beta", "VARCHAR"), col("gamma", "VARCHAR"), col("delta", "VARCHAR"), col("epsilon", "VARCHAR"), col("zeta", "VARCHAR")},
				Rows:    [][]any{{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc", "dddddddddddd", "eeeeeeeeeeee", "ffffffffffff"}},
			}, st: func(s *settings) { s.maxwidth = 60 }},
			{name: "wrap", rs: &engine.ResultSet{
				Columns: []engine.Column{col("n", "INTEGER"), col("text", "VARCHAR")},
				Rows:    [][]any{{int64(1), strings.Repeat("x", 30)}, {int64(2), "short"}},
			}, st: func(s *settings) { s.maxwidth = 30 }},
			{name: "cjk_emoji", rs: &engine.ResultSet{
				Columns: []engine.Column{col("s", "VARCHAR")},
				Rows:    [][]any{{"日本語"}, {"é"}, {"🙂"}, {"ab"}},
			}},
			{name: "types", rs: &engine.ResultSet{
				Columns: []engine.Column{col("l", "INTEGER[]"), col("s", `STRUCT("a" INTEGER)`), col("dec", "DECIMAL(10,2)"), col("ts", "TIMESTAMP"), col("big", "BIGINT"), col("flag", "BOOLEAN")},
				Rows:    [][]any{{[]any{int64(1), nil, int64(3)}, map[string]any{"a": int64(1)}, "10.50", "2024-01-02 03:04:05", int64(9007199254740993), true}},
			}},
			{name: "null_numeric", rs: &engine.ResultSet{
				Columns: []engine.Column{col("n", "INTEGER")},
				Rows:    [][]any{{int64(1)}, {nil}},
			}},
			{name: "nullvalue_dash", rs: abcRS(), st: func(s *settings) { s.nullvalue = "-" }},
			{name: "newline_in_value", rs: &engine.ResultSet{
				Columns: []engine.Column{col("s", "VARCHAR")},
				Rows:    [][]any{{"a\nb"}, {"c\r\nd"}},
			}},
			{name: "columns", rs: abcRS(), st: func(s *settings) { s.columns = true }},
		},
		"box": {
			{name: "simple", rs: abcRS(), st: mode(modeBox)},
		},
		"line": {
			{name: "simple", rs: abcRS(), st: mode(modeLine)},
			{name: "nullvalue", rs: abcRS(), st: func(s *settings) { s.mode = modeLine; s.nullvalue = "(none)" }},
		},
		"csv": {
			{name: "simple", rs: abcRS(), st: mode(modeCSV)},
			{name: "quotes_newline", rs: &engine.ResultSet{
				Columns: []engine.Column{col("s", "VARCHAR"), col("n", "INTEGER")},
				Rows:    [][]any{{"say \"hi\"\nthere", int64(1)}},
			}, st: mode(modeCSV)},
			{name: "headers_off", rs: abcRS(), st: func(s *settings) { s.mode = modeCSV; s.headers = false }},
		},
		"json": {
			{name: "simple", rs: abcRS(), st: mode(modeJSON)},
			{name: "empty", rs: &engine.ResultSet{Columns: []engine.Column{col("a", "INTEGER")}}, st: mode(modeJSON)},
			{name: "nested", rs: &engine.ResultSet{
				Columns: []engine.Column{col("l", "INTEGER[]"), col("f", "DOUBLE")},
				Rows:    [][]any{{[]any{int64(1), "x<y"}, 1.5}},
			}, st: mode(modeJSON)},
		},
		"markdown": {
			{name: "simple", rs: abcRS(), st: mode(modeMarkdown)},
		},
	}
}

func renderToString(t *testing.T, rc renderCase, color bool) string {
	t.Helper()
	st := defaultSettings()
	if rc.st != nil {
		rc.st(&st)
	}
	total := rc.total
	if total == 0 {
		total = len(rc.rs.Rows)
	}
	width := rc.width
	if width == 0 {
		width = 80
	}
	var b bytes.Buffer
	require.NoError(t, render(context.Background(), &b, rc.rs, total, renderOpts{st: st, width: width, pal: palette{on: color}}))
	return b.String()
}

func TestRenderGolden(t *testing.T) {
	for modeName, cases := range renderCases() {
		for _, rc := range cases {
			t.Run(modeName+"_"+rc.name, func(t *testing.T) {
				got := renderToString(t, rc, false)
				path := filepath.Join("testdata", "render", modeName+"_"+rc.name+".golden")
				if *update {
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
					require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
				}
				want, err := os.ReadFile(path)
				require.NoError(t, err, "run with -update to create the golden")
				assert.Equal(t, string(want), got)
			})
		}
	}
}

// With Color on, the output differs from the plain rendering by exactly the
// four SGR constants.
func TestRenderColorStripsToPlain(t *testing.T) {
	rc := renderCase{rs: simpleRS()}
	plain := renderToString(t, rc, false)
	colored := renderToString(t, rc, true)
	assert.NotEqual(t, plain, colored)
	stripped := strings.NewReplacer(sgrDim, "", sgrBold, "", sgrRed, "", sgrReset, "").Replace(colored)
	assert.Equal(t, plain, stripped)
}

// The reference box below was produced by the DuckDB CLI (duckdb -no-agent -c
// "...") and is pinned byte for byte. The footer lines under an elided result
// are laid out the same but lack the CLI's trailing space.
func TestDuckboxMatchesDuckDBCLI(t *testing.T) {
	want := strings.Join([]string{
		"┌───────┬─────────┬────────┬──────────────┬────────────┐",
		"│   a   │    b    │   c    │      d       │     e      │",
		"│ int32 │ varchar │ \"null\" │ decimal(2,1) │    date    │",
		"├───────┼─────────┼────────┼──────────────┼────────────┤",
		"│     1 │ x       │ NULL   │          1.5 │ 2024-01-02 │",
		"└───────┴─────────┴────────┴──────────────┴────────────┘",
		"",
	}, "\n")
	assert.Equal(t, want, renderToString(t, renderCase{rs: simpleRS()}, false))

	got := renderToString(t, renderCase{rs: rangeRS(100)}, false)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	assert.Equal(t, "   100 rows", lines[len(lines)-2])
	assert.Equal(t, "  (40 shown)", lines[len(lines)-1])
	assert.Equal(t, "│   range    │", lines[1], fmt.Sprint(lines[:3]))
}
