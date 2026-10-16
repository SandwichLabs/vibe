package storageglob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

// fakeLister serves keys by exact pattern and counts calls.
type fakeLister struct {
	keys  map[string][]string
	calls []string
	err   error
}

func (f *fakeLister) ListMatching(_ context.Context, pattern string, limit int) ([]string, error) {
	f.calls = append(f.calls, pattern)
	if f.err != nil {
		return nil, f.err
	}
	ks := f.keys[pattern]
	if len(ks) > limit+1 {
		ks = ks[:limit+1]
	}
	return append([]string(nil), ks...), nil
}

func newFake() *fakeLister {
	return &fakeLister{keys: map[string][]string{
		"s3://b/dir/*.parquet": {"s3://b/dir/b.parquet", "s3://b/dir/a.parquet"},
		"s3://b/**":            {"s3://b/x.csv", "s3://b/sub/y.csv"},
		"s3://b/*.csv":         {"s3://b/one.csv"},
		"s3://b/lake/*.json":   {"s3://b/lake/a.json", "s3://b/lake/b.json"},
		"s3://b/none/*":        nil,
		"s3://b/other/*.p":     {"s3://b/other/z.p"},
	}}
}

func expand(t *testing.T, text string) (string, *fakeLister, error) {
	t.Helper()
	f := newFake()
	out, _, err := Expand(context.Background(), text, Options{Lister: f})
	return out, f, err
}

func TestExpandRewritesEveryShape(t *testing.T) {
	list := "['s3://b/dir/a.parquet', 's3://b/dir/b.parquet']"
	cases := []struct{ name, in, want string }{
		{"reader", `SELECT * FROM read_parquet('s3://b/dir/*.parquet')`, `SELECT * FROM read_parquet(` + list + `)`},
		{"parquet_scan alias", `SELECT * FROM parquet_scan('s3://b/dir/*.parquet')`, `SELECT * FROM parquet_scan(` + list + `)`},
		{"named options keep their place", `SELECT * FROM read_parquet('s3://b/dir/*.parquet', hive_partitioning = true, union_by_name = true)`,
			`SELECT * FROM read_parquet(` + list + `, hive_partitioning = true, union_by_name = true)`},
		{"csv header option", `SELECT count(*) FROM read_csv('s3://b/*.csv', header=true)`, `SELECT count(*) FROM read_csv(['s3://b/one.csv'], header=true)`},
		{"read_csv_auto", `FROM read_csv_auto('s3://b/*.csv')`, `FROM read_csv_auto(['s3://b/one.csv'])`},
		{"json reader", `SELECT * FROM read_json('s3://b/lake/*.json')`, `SELECT * FROM read_json(['s3://b/lake/a.json', 's3://b/lake/b.json'])`},
		{"text and blob", `SELECT * FROM read_text('s3://b/*.csv'), read_blob('s3://b/*.csv')`, `SELECT * FROM read_text(['s3://b/one.csv']), read_blob(['s3://b/one.csv'])`},
		{"FROM pattern", `SELECT * FROM 's3://b/dir/*.parquet'`, `SELECT * FROM read_parquet(` + list + `)`},
		{"FROM pattern with alias and join", `SELECT * FROM 's3://b/dir/*.parquet' AS p JOIN 's3://b/*.csv' c ON c.id = p.id`,
			`SELECT * FROM read_parquet(` + list + `) AS p JOIN read_csv(['s3://b/one.csv']) c ON c.id = p.id`},
		{"FROM quoted identifier", `SELECT * FROM "s3://b/*.csv"`, `SELECT * FROM read_csv(['s3://b/one.csv'])`},
		{"FROM json", `SELECT * FROM 's3://b/lake/*.json'`, `SELECT * FROM read_json_auto(['s3://b/lake/a.json', 's3://b/lake/b.json'])`},
		{"glob()", `SELECT * FROM glob('s3://b/**')`, `SELECT * FROM glob(['s3://b/sub/y.csv', 's3://b/x.csv'])`},
		{"glob() nothing matched", `SELECT count(*) FROM glob('s3://b/none/*')`, `SELECT count(*) FROM glob([]::VARCHAR[])`},
		{"list argument, one pattern one plain", `SELECT * FROM read_parquet(['s3://b/plain.parquet', 's3://b/dir/*.parquet'])`,
			`SELECT * FROM read_parquet(['s3://b/plain.parquet', 's3://b/dir/a.parquet', 's3://b/dir/b.parquet'])`},
		{"list argument, two patterns", `SELECT * FROM read_csv(['s3://b/*.csv', 's3://b/lake/*.json'])`,
			`SELECT * FROM read_csv(['s3://b/one.csv', 's3://b/lake/a.json', 's3://b/lake/b.json'])`},
		{"CTE", `WITH x AS (SELECT * FROM read_csv('s3://b/*.csv')) SELECT * FROM x`, `WITH x AS (SELECT * FROM read_csv(['s3://b/one.csv'])) SELECT * FROM x`},
		{"subquery", `SELECT * FROM t WHERE id IN (SELECT id FROM read_csv('s3://b/*.csv'))`, `SELECT * FROM t WHERE id IN (SELECT id FROM read_csv(['s3://b/one.csv']))`},
		{"join of two readers", `SELECT * FROM read_csv('s3://b/*.csv') a JOIN read_parquet('s3://b/dir/*.parquet') b USING (id)`,
			`SELECT * FROM read_csv(['s3://b/one.csv']) a JOIN read_parquet(` + list + `) b USING (id)`},
		{"union of the same pattern twice", `SELECT * FROM read_csv('s3://b/*.csv') UNION ALL SELECT * FROM read_csv('s3://b/*.csv')`,
			`SELECT * FROM read_csv(['s3://b/one.csv']) UNION ALL SELECT * FROM read_csv(['s3://b/one.csv'])`},
		{"CTAS", `CREATE TABLE t AS SELECT * FROM read_csv('s3://b/*.csv')`, `CREATE TABLE t AS SELECT * FROM read_csv(['s3://b/one.csv'])`},
		{"INSERT SELECT", `INSERT INTO t SELECT * FROM read_csv('s3://b/*.csv')`, `INSERT INTO t SELECT * FROM read_csv(['s3://b/one.csv'])`},
		{"INSERT SELECT with column list", `INSERT INTO t (a, b) SELECT a, b FROM 's3://b/*.csv'`, `INSERT INTO t (a, b) SELECT a, b FROM read_csv(['s3://b/one.csv'])`},
		{"COPY query TO a file", `COPY (SELECT * FROM read_csv('s3://b/*.csv')) TO '/tmp/out.csv'`, `COPY (SELECT * FROM read_csv(['s3://b/one.csv'])) TO '/tmp/out.csv'`},
		{"EXPLAIN", `EXPLAIN SELECT * FROM read_csv('s3://b/*.csv')`, `EXPLAIN SELECT * FROM read_csv(['s3://b/one.csv'])`},
		{"LIMIT wrapper", `SELECT * FROM (SELECT * FROM read_csv('s3://b/*.csv')) AS _a2ui_limit LIMIT 51`, `SELECT * FROM (SELECT * FROM read_csv(['s3://b/one.csv'])) AS _a2ui_limit LIMIT 51`},
		{"two statements, both rewritten", `SELECT * FROM read_csv('s3://b/*.csv'); SELECT * FROM 's3://b/dir/*.parquet'`,
			`SELECT * FROM read_csv(['s3://b/one.csv']); SELECT * FROM read_parquet(` + list + `)`},
		{"comments and newlines survive", "-- a comment with 's3://b/*.csv'\nSELECT *\n/* block s3://b/x.csv */ FROM read_csv(\n  's3://b/*.csv'\n) -- tail", "-- a comment with 's3://b/*.csv'\nSELECT *\n/* block s3://b/x.csv */ FROM read_csv(\n  ['s3://b/one.csv']\n) -- tail"},
		{"multibyte text before the pattern", `SELECT 'é☃', x FROM read_csv('s3://b/*.csv')`, `SELECT 'é☃', x FROM read_csv(['s3://b/one.csv'])`},
		{"parameter placeholder beside it", `SELECT * FROM read_csv('s3://b/*.csv') WHERE a = ? AND b = ?`, `SELECT * FROM read_csv(['s3://b/one.csv']) WHERE a = ? AND b = ?`},
		{"uppercase function name", `SELECT * FROM READ_CSV('s3://b/*.csv')`, `SELECT * FROM READ_CSV(['s3://b/one.csv'])`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := expand(t, tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
		})
	}
}

func TestExpandLeavesEverythingElseAlone(t *testing.T) {
	for name, in := range map[string]string{
		"value, not a path":           `SELECT 's3://b/*.csv' AS pattern`,
		"comparison":                  `SELECT * FROM t WHERE url = 's3://b/*.csv'`,
		"second argument":             `SELECT * FROM read_csv('s3://b/one.csv', delim = 's3://b/*.csv')`,
		"a function that never globs": `SELECT * FROM iceberg_scan('s3://b/*.csv')`,
		"scalar function":             `SELECT replace('s3://b/*.csv', 'a', 'b')`,
		"azure is not rewritten":      `SELECT * FROM read_parquet('az://c/dir/*.parquet')`,
		"local glob":                  `SELECT * FROM read_csv('/data/*.csv')`,
		"a plain key":                 `SELECT * FROM read_parquet('s3://b/dir/a.parquet')`,
		"? is part of the name":       `SELECT * FROM read_parquet('s3://b/dir/a?.parquet')`,
		"comment only":                "SELECT 1 -- read_csv('s3://b/*.csv')",
		"block comment only":          "SELECT 1 /* FROM 's3://b/x.csv' and s3://b/ */ -- s3://b/*.csv",
		"INSERT value":                `INSERT INTO t VALUES ('s3://b/*.csv')`,
		"string with escaped quote":   `SELECT * FROM t WHERE a = 'it''s s3://b/*.csv'`,
		"dollar-quoted value":         `SELECT $$s3://b/*.csv$$`,
	} {
		t.Run(name, func(t *testing.T) {
			out, f, err := expand(t, in)
			require.NoError(t, err)
			assert.Equal(t, in, out)
			assert.Empty(t, f.calls, "nothing is listed for a literal that is not a pattern")
		})
	}
}

func TestExpandRefusesWhatItCannotRewriteSafely(t *testing.T) {
	for name, in := range map[string]string{
		"E string":              `SELECT * FROM read_csv(E's3://b/*.csv')`,
		"pattern with a ?":      `SELECT * FROM read_csv('s3://b/a?/*.csv')`,
		"pattern with [":        `SELECT * FROM read_csv('s3://b/[ab]*.csv')`,
		"url parameters":        `SELECT * FROM read_csv('s3://b/*.csv?s3_endpoint=evil.example')`,
		"dotdot":                `SELECT * FROM read_csv('s3://b/../c/*.csv')`,
		"nothing matched":       `SELECT * FROM read_csv('s3://b/none/*')`,
		"FROM without a reader": `SELECT * FROM 's3://b/none/*'`,
		"view":                  `CREATE VIEW v AS SELECT * FROM read_csv('s3://b/*.csv')`,
		"or replace temp view":  `CREATE OR REPLACE TEMP VIEW v AS SELECT * FROM 's3://b/*.csv'`,
		"COPY FROM":             `COPY t FROM 's3://b/dir/*.csv'`,
		"adjacent literals":     "SELECT * FROM read_csv('s3://b/*.csv'\n'')",
		"macro":                 `CREATE MACRO m() AS TABLE SELECT * FROM read_csv('s3://b/*.csv')`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := expand(t, in)
			require.Error(t, err)
		})
	}
	t.Run("FROM with an unknown extension", func(t *testing.T) {
		f := newFake()
		f.keys["s3://b/dir/*"] = []string{"s3://b/dir/a"}
		_, _, err := Expand(context.Background(), `SELECT * FROM 's3://b/dir/*'`, Options{Lister: f})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read_parquet")
	})
	t.Run("zero matches in a list element", func(t *testing.T) {
		_, _, err := expand(t, `SELECT * FROM read_csv(['s3://b/*.csv', 's3://b/none/*'])`)
		require.Error(t, err)
	})
}

func TestExpandErrorsNameThePattern(t *testing.T) {
	_, _, err := expand(t, `SELECT * FROM read_csv('s3://b/none/*')`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no files found that match the pattern "s3://b/none/*"`)

	f := newFake()
	f.err = errors.New("HTTP 403")
	_, _, err = Expand(context.Background(), `SELECT * FROM read_csv('s3://b/*.csv')`, Options{Lister: f})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"s3://b/*.csv"`)

	_, _, err = Expand(context.Background(), `SELECT * FROM read_csv('s3://b/*.csv')`, Options{})
	require.Error(t, err, "no lister")
}

func TestExpandCapsTheNumberOfKeys(t *testing.T) {
	f := &fakeLister{keys: map[string][]string{}}
	var ks []string
	for i := 0; i < 12; i++ {
		ks = append(ks, fmt.Sprintf("s3://b/many/%02d.csv", i))
	}
	f.keys["s3://b/many/*.csv"] = ks
	var few []string
	for i := 0; i < 6; i++ {
		few = append(few, fmt.Sprintf("s3://b/few/%02d.csv", i))
	}
	f.keys["s3://b/few/*.csv"] = few

	_, _, err := Expand(context.Background(), `SELECT * FROM read_csv('s3://b/many/*.csv')`, Options{Lister: f, Max: 10})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than 10")
	assert.Contains(t, err.Error(), "s3://b/many/*.csv")
	assert.NotContains(t, err.Error(), "s3://b/many/00.csv", "an error never lists keys")

	out, rw, err := Expand(context.Background(), `SELECT * FROM read_csv('s3://b/few/*.csv')`, Options{Lister: f, Max: 10})
	require.NoError(t, err)
	assert.Equal(t, 6, rw.Keys)
	assert.Contains(t, out, "s3://b/few/00.csv")

	// The cap is for the whole statement.
	_, _, err = Expand(context.Background(), `SELECT * FROM read_csv('s3://b/few/*.csv') UNION ALL SELECT * FROM read_csv('s3://b/many/*.csv')`, Options{Lister: f, Max: 10})
	require.Error(t, err)
}

func TestExpandVetsPatternsBeforeListing(t *testing.T) {
	f := newFake()
	vetErr := errors.New("outside the scope")
	_, _, err := Expand(context.Background(), `SELECT * FROM read_csv('s3://b/*.csv')`, Options{Lister: f, Vet: func(string) error { return vetErr }})
	require.ErrorIs(t, err, vetErr)
	assert.Empty(t, f.calls, "a pattern the policy refuses is never listed")
}

func TestExpandQuotesEveryKeyAndKeepsItInsideThePrefix(t *testing.T) {
	f := &fakeLister{keys: map[string][]string{"s3://b/dir/*": {
		"s3://b/dir/it's.csv",
		"s3://b/dir/a'); DROP TABLE t; --.csv",
	}}}
	out, _, err := Expand(context.Background(), `SELECT * FROM read_csv('s3://b/dir/*')`, Options{Lister: f})
	require.NoError(t, err)
	assert.Contains(t, out, `'s3://b/dir/it''s.csv'`)
	assert.Contains(t, out, `'s3://b/dir/a''); DROP TABLE t; --.csv'`)
	// And the whole thing is still one SELECT to DuckDB.
	_, err = DefaultParser().Parse(context.Background(), out)
	require.NoError(t, err)

	// A name with ?, # or a control character is refused, with a count.
	for _, k := range []string{"s3://b/dir/x.csv?s3_endpoint=127.0.0.1:1", "s3://b/dir/a#b.csv", "s3://b/dir/a\x00b.csv", "s3://b/dir/a\nb.csv"} {
		f.keys["s3://b/dir/*"] = []string{"s3://b/dir/ok.csv", k}
		_, _, err = Expand(context.Background(), `SELECT * FROM read_csv('s3://b/dir/*')`, Options{Lister: f})
		require.Error(t, err, k)
		assert.Contains(t, err.Error(), "1 of the objects")
	}

	// A listing that strays outside the pattern's folder is refused.
	f.keys["s3://b/dir/*"] = []string{"s3://b/dir/a.csv", "s3://b/elsewhere/b.csv"}
	_, _, err = Expand(context.Background(), `SELECT * FROM read_csv('s3://b/dir/*')`, Options{Lister: f})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "elsewhere")
}

func TestExpandRequirePlainRefusesOddNames(t *testing.T) {
	f := &fakeLister{keys: map[string][]string{"s3://b/dir/*": {"s3://b/dir/a.csv", "s3://b/dir/a b.csv", "s3://b/dir/x%y.csv", "s3://b/dir/z*.csv"}}}
	_, _, err := Expand(context.Background(), `SELECT * FROM read_csv('s3://b/dir/*')`, Options{Lister: f, RequirePlain: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 of the objects")
	_, _, err = Expand(context.Background(), `SELECT * FROM read_csv('s3://b/dir/*')`, Options{Lister: f})
	require.NoError(t, err, "a person's SQL reads such names literally")
}

func TestExpandHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeLister{err: ctx.Err()}
	_, _, err := Expand(ctx, `SELECT * FROM read_csv('s3://b/*.csv')`, Options{Lister: f})
	require.ErrorIs(t, err, context.Canceled)
}

func TestRewriteRedactsErrors(t *testing.T) {
	text := `SELECT * FROM read_csv('s3://b/dir/*.parquet')`
	out, rw, err := expand2(t, text)
	require.NoError(t, err)
	require.True(t, rw.Changed())
	assert.Equal(t, 1, rw.Patterns)
	assert.Equal(t, 2, rw.Keys)
	msg := rw.Redact(`Binder Error: bad column in LINE 1: ` + out)
	assert.Contains(t, msg, `'s3://b/dir/*.parquet'`)
	assert.NotContains(t, msg, "a.parquet")
	long := rw.Redact(strings.Repeat("x", 10000))
	assert.Less(t, len(long), 5000)
	var none *Rewrite
	assert.Equal(t, "same", none.Redact("same"))
	assert.False(t, none.Changed())
}

func expand2(t *testing.T, text string) (string, *Rewrite, error) {
	t.Helper()
	return Expand(context.Background(), text, Options{Lister: newFake()})
}

func TestContextMarksSQLAsExpanded(t *testing.T) {
	_, ok := FromContext(context.Background())
	assert.False(t, ok)
	ctx := WithRewrite(context.Background(), nil)
	rw, ok := FromContext(ctx)
	require.True(t, ok)
	assert.False(t, rw.Changed())
}

func TestFromReaderByExtension(t *testing.T) {
	for in, want := range map[string]string{
		"s3://b/*.parquet": "read_parquet", "s3://b/*.PQ": "read_parquet", "s3://b/*.csv.gz": "read_csv",
		"s3://b/*.tsv": "read_csv", "s3://b/*.jsonl": "read_json_auto", "s3://b/*.ndjson.zst": "read_json_auto", "s3://b/*.json": "read_json_auto",
	} {
		got, ok := fromReader(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	_, ok := fromReader("s3://b/*")
	assert.False(t, ok)
}

// The default parser is the real DuckDB parser; the hub is the real lister.
func TestHubIsALister(t *testing.T) {
	sess, err := session.New()
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	_, ok := sess.(Lister)
	assert.True(t, ok)
}

func TestExpandSkipsACTENamedLikeAPattern(t *testing.T) {
	in := `WITH "s3://b/*.csv" AS (SELECT 1 AS x) SELECT * FROM "s3://b/*.csv"`
	out, f, err := expand(t, in)
	require.NoError(t, err)
	assert.Equal(t, in, out)
	assert.Empty(t, f.calls)
}

func TestExpandAgentGlobWithNoMatchIsAnError(t *testing.T) {
	f := newFake()
	_, _, err := Expand(context.Background(), `SELECT * FROM glob('s3://b/none/*')`, Options{Lister: f, RequirePlain: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files found")
}

func TestExpandCapNamesTheTotal(t *testing.T) {
	f := &fakeLister{keys: map[string][]string{
		"s3://b/a/*.csv": {"s3://b/a/1.csv", "s3://b/a/2.csv", "s3://b/a/3.csv"},
		"s3://b/c/*.csv": {"s3://b/c/1.csv", "s3://b/c/2.csv", "s3://b/c/3.csv"},
	}}
	_, _, err := Expand(context.Background(), `SELECT * FROM read_csv('s3://b/a/*.csv') UNION ALL SELECT * FROM read_csv('s3://b/c/*.csv')`, Options{Lister: f, Max: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in all")
}
