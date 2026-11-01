package semantics

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2" // registers "duckdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/queries"
)

type fakeSource struct {
	metrics map[string]*Metric
	queries map[string]*queries.Query
}

func (f *fakeSource) Metric(_ context.Context, name string) (*Metric, error) {
	if m, ok := f.metrics[name]; ok {
		return m, nil
	}
	return nil, &notFoundError{kind: KindMetric, name: name}
}

func (f *fakeSource) Query(_ context.Context, id string) (*queries.Query, error) {
	if q, ok := f.queries[id]; ok {
		return q, nil
	}
	return nil, queries.ErrNotFound
}

func metricNamed(t *testing.T, name, query, measure string, dims ...string) *Metric {
	t.Helper()
	m, err := DecodeMetric("net_revenue", []byte(goodMetric))
	require.NoError(t, err)
	m.Name, m.Query, m.Measure, m.Dimensions = name, query, measure, dims
	return m
}

func newFake(t *testing.T) *fakeSource {
	return &fakeSource{
		metrics: map[string]*Metric{
			"net_revenue": metricNamed(t, "net_revenue", "net_revenue_base", "sum(amount)", "region", "month"),
			"orders":      metricNamed(t, "orders", "orders_base", "count(*)"),
		},
		queries: map[string]*queries.Query{
			"net_revenue_base": {ID: "net_revenue_base", SQL: "SELECT region, month, amount FROM sales;\n"},
			"orders_base":      {ID: "orders_base", SQL: "SELECT * FROM orders -- all of them\n"},
		},
	}
}

func expand(t *testing.T, f Reader, text string, o ...Options) (*Result, error) {
	t.Helper()
	var opt Options
	if len(o) > 0 {
		opt = o[0]
	}
	return Expand(context.Background(), text, f, opt)
}

func TestExpand_Shapes(t *testing.T) {
	f := newFake(t)
	cases := []struct{ in, want string }{
		{"SELECT * FROM metric('net_revenue', by => 'region')",
			"SELECT * FROM (SELECT \"region\", sum(amount) AS \"net_revenue\" FROM (\nSELECT region, month, amount FROM sales\n) AS slaw_metric_net_revenue GROUP BY \"region\")"},
		{"select * from METRIC ( 'net_revenue' , BY=>['Month', 'region'] )",
			"select * from (SELECT \"month\", \"region\", sum(amount) AS \"net_revenue\" FROM (\nSELECT region, month, amount FROM sales\n) AS slaw_metric_net_revenue GROUP BY \"month\", \"region\")"},
		{"SELECT metric('orders') AS n",
			"SELECT (SELECT count(*) AS \"orders\" FROM (\nSELECT * FROM orders\n) AS slaw_metric_orders) AS n"},
		{"SELECT 'metric(''x'')' AS s -- metric('y')\n, \"metric\" FROM t", "SELECT 'metric(''x'')' AS s -- metric('y')\n, \"metric\" FROM t"},
		{"SELECT * FROM s.metric('x')", "SELECT * FROM s.metric('x')"},
		{"SELECT metric FROM metrics", "SELECT metric FROM metrics"},
	}
	for _, c := range cases {
		r, err := expand(t, f, c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, r.SQL, c.in)
		assert.Equal(t, c.want != c.in, r.Changed, c.in)
	}
}

func TestExpand_Idempotent(t *testing.T) {
	f := newFake(t)
	for _, in := range []string{
		"SELECT * FROM metric('net_revenue', by => ['region','month']) WHERE net_revenue > 0",
		"SELECT metric('orders'), metric('orders')",
		"SELECT 1",
		"SELECT * FROM metric_table",
	} {
		a, err := expand(t, f, in)
		require.NoError(t, err)
		b, err := expand(t, f, a.SQL)
		require.NoError(t, err)
		assert.Equal(t, a.SQL, b.SQL, in)
		assert.False(t, b.Changed, in)
	}
}

func TestExpand_Uses(t *testing.T) {
	f := newFake(t)
	r, err := expand(t, f, "SELECT * FROM metric('net_revenue', by => 'REGION'), metric('orders')")
	require.NoError(t, err)
	require.Len(t, r.Uses, 2)
	assert.Equal(t, "net_revenue", r.Uses[0].Name)
	assert.Equal(t, []string{"region"}, r.Uses[0].Dims)
	assert.Equal(t, "net_revenue_base", r.Uses[0].QueryID)
	assert.Equal(t, "orders", r.Uses[1].Name)
	assert.Len(t, r.Bodies, 2)
	assert.Equal(t, []map[string]any{
		{"name": "net_revenue", "by": []string{"region"}, "query": "net_revenue_base"},
		{"name": "orders", "by": []string{}, "query": "orders_base"},
	}, r.UsesJSON())
}

func TestExpand_Refusals(t *testing.T) {
	f := newFake(t)
	f.queries["nested"] = &queries.Query{ID: "nested", SQL: "SELECT * FROM metric('orders')"}
	f.metrics["nest"] = metricNamed(t, "nest", "nested", "count(*)")
	f.queries["script"] = &queries.Query{ID: "script", SQL: "SELECT 1; DROP TABLE t"}
	f.metrics["script"] = metricNamed(t, "script", "script", "count(*)")
	f.queries["copy"] = &queries.Query{ID: "copy", SQL: "COPY t TO 'x.csv'"}
	f.metrics["copy"] = metricNamed(t, "copy", "copy", "count(*)")
	f.queries["open"] = &queries.Query{ID: "open", SQL: "SELECT 1 /* oops"}
	f.metrics["open"] = metricNamed(t, "open", "open", "count(*)")
	f.queries["paren"] = &queries.Query{ID: "paren", SQL: "SELECT (1"}
	f.metrics["paren"] = metricNamed(t, "paren", "paren", "count(*)")
	f.queries["close"] = &queries.Query{ID: "close", SQL: "SELECT 1) UNION (SELECT 2"}
	f.metrics["close"] = metricNamed(t, "close", "close", "count(*)")
	f.queries["str"] = &queries.Query{ID: "str", SQL: "SELECT 'oops"}
	f.metrics["str"] = metricNamed(t, "str", "str", "count(*)")
	f.metrics["gone"] = metricNamed(t, "gone", "no_such_query", "count(*)")
	f.queries["undeclared"] = &queries.Query{ID: "undeclared", SQL: "SELECT * FROM t WHERE a > $x"}
	f.metrics["undeclared"] = metricNamed(t, "undeclared", "undeclared", "count(*)")
	f.queries["shape"] = &queries.Query{ID: "shape", SQL: "SELECT * FROM t WHERE a > ?"}
	f.metrics["shape"] = metricNamed(t, "shape", "shape", "count(*)")
	f.queries["errq"] = &queries.Query{ID: "errq", SQL: "SELECT 1", Error: "bad header"}
	f.metrics["errq"] = metricNamed(t, "errq", "errq", "count(*)")

	many := strings.Repeat("metric('orders'),", MaxMetricCalls+1)
	cases := []struct{ in, msg string }{
		{"SELECT * FROM metric('nope')", `metric('nope'): there is no metric named 'nope'`},
		{"SELECT * FROM metric('Bad Name')", `there is no metric named 'Bad Name'`},
		{"SELECT * FROM metric('net_revenue', by => 'colour')", `metric('net_revenue'): 'colour' is not a dimension of net_revenue (dimensions: region, month)`},
		{"SELECT * FROM metric('orders', by => 'x')", `(dimensions: none)`},
		{"SELECT * FROM metric('net_revenue', by => ['region', 'REGION'])", `dimension "region" is listed twice`},
		{"SELECT * FROM metric()", `metric(): the metric name must be a plain 'quoted' literal`},
		{"SELECT * FROM metric(net_revenue)", `the metric name must be a plain 'quoted' literal`},
		{"SELECT * FROM metric(E'net_revenue')", `the metric name must be a plain 'quoted' literal`},
		{"SELECT * FROM metric($$net_revenue$$)", `the metric name must be a plain 'quoted' literal`},
		{"SELECT * FROM metric('a' 'b')", `only by => is supported`},
		{"SELECT * FROM metric('net_revenue', where => 'x')", `only by => is supported`},
		{"SELECT * FROM metric('net_revenue', 'region')", `only by => is supported`},
		{"SELECT * FROM metric('net_revenue', by => region)", `each dimension must be a plain 'quoted' literal`},
		{"SELECT * FROM metric('net_revenue', by => E'region')", `each dimension must be a plain 'quoted' literal`},
		{"SELECT * FROM metric('net_revenue', by => ['region',])", `needs one or more`},
		{"SELECT * FROM metric('net_revenue', by => [])", `each dimension must be a plain`},
		{"SELECT * FROM metric('net_revenue', by => ['region' || 'x'])", `each dimension must be a plain`},
		{"SELECT * FROM metric('net_revenue'", `missing its closing parenthesis`},
		{"SELECT * FROM metric('nest')", `its saved query "nested" uses metric(), which a metric's own query may not do`},
		{"SELECT * FROM metric('script')", `saved query "script" is not one read statement`},
		{"SELECT * FROM metric('copy')", `saved query "copy" is not one read statement`},
		{"SELECT * FROM metric('paren')", `not one read statement`},
		{"SELECT * FROM metric('close')", `not one read statement`},
		{"SELECT * FROM metric('str')", `not one read statement`},
		{"SELECT * FROM metric('gone')", `its saved query "no_such_query" does not exist`},
		{"SELECT * FROM metric('undeclared')", `uses $x, which it does not declare`},
		{"SELECT * FROM metric('shape')", `saved query "shape": a bare ?`},
		{"SELECT * FROM metric('errq')", `cannot be read: bad header`},
		{"SELECT " + many + " 1", `21 metric() calls; at most 20`},
	}
	for _, c := range cases {
		_, err := expand(t, f, c.in)
		require.Error(t, err, c.in)
		assert.ErrorIs(t, err, ErrInvalid, c.in)
		assert.Contains(t, err.Error(), c.msg, c.in)
		assert.NotContains(t, err.Error(), "invalid semantics definition", "a refusal is a sentence for the writer of the statement: "+c.in)
	}
	// an unclosed trailing comment is cut off with the semicolon, not left to swallow the closing paren.
	r, err := expand(t, f, "SELECT * FROM metric('open')")
	require.NoError(t, err)
	assert.Contains(t, r.SQL, "SELECT 1\n) AS slaw_metric_open")
	// 20 calls is fine.
	_, err = expand(t, f, "SELECT "+strings.Repeat("metric('orders'),", MaxMetricCalls)+" 1")
	assert.NoError(t, err)
	// a nil source cannot expand a call, but plain text passes.
	_, err = Expand(context.Background(), "SELECT metric('x')", nil, Options{})
	assert.ErrorIs(t, err, ErrInvalid)
	r, err = Expand(context.Background(), "SELECT 1", nil, Options{})
	require.NoError(t, err)
	assert.False(t, r.Changed)
}

func TestExpand_LiteralsNeverReachSQL(t *testing.T) {
	f := newFake(t)
	r, err := expand(t, f, "SELECT * FROM metric('net_revenue', by => 'month')")
	require.NoError(t, err)
	// the dimension is the declared spelling, the name the declared one.
	assert.NotContains(t, r.SQL, "'month'")
	// an echo of a hostile literal in an error is bounded and escaped.
	_, err = expand(t, f, "SELECT * FROM metric('"+strings.Repeat("x", 500)+"')")
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 400)
	_, err = expand(t, f, "SELECT * FROM metric('a''b); DROP TABLE t; --')")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'a''b); DROP TABLE t; --'")
}

func TestExpand_ConnectionRule(t *testing.T) {
	f := newFake(t)
	f.queries["net_revenue_base"].Connection = "prod_pg"
	_, err := expand(t, f, "SELECT * FROM metric('net_revenue')")
	assert.ErrorContains(t, err, "metric 'net_revenue' is defined on connection 'prod_pg'; run this statement on prod_pg")
	_, err = expand(t, f, "SELECT * FROM metric('net_revenue')", Options{Connection: "other", DefaultConnection: "main"})
	assert.ErrorContains(t, err, "defined on connection 'prod_pg'")
	_, err = expand(t, f, "SELECT * FROM metric('net_revenue')", Options{Connection: "prod_pg"})
	assert.NoError(t, err)
	// the workspace default and "" are the same place.
	_, err = expand(t, f, "SELECT * FROM metric('net_revenue')", Options{Connection: "", DefaultConnection: "prod_pg"})
	assert.NoError(t, err)
	// a saved query on the default connection run elsewhere is refused too.
	_, err = expand(t, f, "SELECT * FROM metric('orders')", Options{Connection: "prod_pg", DefaultConnection: "main"})
	assert.ErrorContains(t, err, "defined on connection 'main'")
	_, err = expand(t, f, "SELECT * FROM metric('orders')", Options{Connection: "main", DefaultConnection: "main"})
	assert.NoError(t, err)
	_, err = expand(t, f, "SELECT * FROM metric('orders')", Options{Connection: "x"})
	assert.ErrorContains(t, err, "default connection")
}

func TestExpand_ParamsAndClash(t *testing.T) {
	f := newFake(t)
	f.queries["net_revenue_base"] = &queries.Query{ID: "net_revenue_base",
		SQL:        "SELECT region, month, amount FROM sales WHERE day >= $from AND amount > $min",
		Parameters: []queries.Param{{Name: "from", Type: "date", Default: "2026-01-31"}, {Name: "min", Type: "number", Default: "5"}}}
	r, err := expand(t, f, "SELECT * FROM metric('net_revenue', by => 'region') WHERE net_revenue > $floor")
	require.NoError(t, err)
	assert.Contains(t, r.SQL, "$from")
	assert.Contains(t, r.SQL, "$min")
	require.Len(t, r.Params, 2)
	// the expanded text passes the shape check next to the caller's own $floor.
	require.NoError(t, queries.CheckParamShape(r.SQL))

	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	got, err := r.BindParams(map[string]any{"floor": 3, "min": 7}, now)
	require.NoError(t, err)
	assert.Equal(t, 3, got["floor"], "a caller's own parameter passes through")
	assert.EqualValues(t, 7, got["min"], "a supplied value wins")
	assert.NotNil(t, got["from"], "an unsupplied one takes its default")
	assert.Len(t, got, 3)

	_, err = r.BindParams(map[string]any{"min": "not a number"}, now)
	assert.ErrorIs(t, err, ErrInvalid)

	// no metric parameters: values pass through unchanged.
	plain, err := expand(t, f, "SELECT 1")
	require.NoError(t, err)
	v := map[string]any{"a": 1}
	got, err = plain.BindParams(v, now)
	require.NoError(t, err)
	assert.Equal(t, v, got)

	// a second metric declaring $min differently is a clash; identically it is fine.
	f.queries["orders_base"] = &queries.Query{ID: "orders_base", SQL: "SELECT * FROM o WHERE a > $min",
		Parameters: []queries.Param{{Name: "min", Type: "number", Default: "6"}}}
	_, err = expand(t, f, "SELECT metric('net_revenue'), metric('orders')")
	require.ErrorIs(t, err, ErrParamClash)
	assert.ErrorContains(t, err, "net_revenue and orders")
	f.queries["orders_base"].Parameters[0].Default = "5"
	r, err = expand(t, f, "SELECT metric('net_revenue'), metric('orders')")
	require.NoError(t, err)
	assert.Len(t, r.Params, 2)
}

func TestExpand_Fuzzish(t *testing.T) {
	f := newFake(t)
	for _, in := range []string{"metric(", "metric('", "metric('a", "metric('a',", "metric('a', by =>", "metric(')", "((metric(", "metric('net_revenue', by => [", "'metric('", "/* metric('x') */", "E'metric('x')'"} {
		assert.NotPanics(t, func() { _, _ = expand(t, f, in) }, in)
	}
}

func TestExpand_CheckOuterParams(t *testing.T) {
	f := newFake(t)
	f.queries["net_revenue_base"] = &queries.Query{ID: "net_revenue_base", SQL: "SELECT region, month, amount FROM sales WHERE amount > $min",
		Parameters: []queries.Param{{Name: "min", Type: "number", Default: "2"}}}
	in := "SELECT * FROM metric('net_revenue') WHERE net_revenue > $min AND net_revenue < $max"
	r, err := expand(t, f, in)
	require.NoError(t, err)
	err = r.CheckOuterParams(in, map[string]any{"max": 5}, nil)
	require.ErrorIs(t, err, ErrInvalid)
	assert.EqualError(t, err, "$min belongs to metric net_revenue's saved query; it can't be used outside metric()")
	assert.NoError(t, r.CheckOuterParams(in, map[string]any{"min": 3}, nil), "a supplied value is fine")
	assert.NoError(t, r.CheckOuterParams("SELECT * FROM metric('net_revenue')", nil, nil), "only the outer text counts")
	assert.NoError(t, r.CheckOuterParams(in, map[string]any{"min": 3}, []queries.Param{{Name: "min", Type: "number"}}))
	err = r.CheckOuterParams(in, map[string]any{"min": 3}, []queries.Param{{Name: "min", Type: "text"}})
	require.ErrorIs(t, err, ErrInvalid)
	assert.Contains(t, err.Error(), "declared as text by the open query and as number by metric net_revenue's saved query")
}

// A metric file that does not decode is reported as one sentence, not as the
// store's nested "failed to get metric ...: invalid semantics definition: ..." chain.
func TestExpand_UnreadableMetricIsOneSentence(t *testing.T) {
	mock := &Mock{}
	mock.SetFile("metrics/broken.yaml", []byte("version: 1\nname: broken\nquery: q\nmeasure: sum(a) FROM t\n"))
	q := &queries.Mock{}
	_, err := Expand(context.Background(), "SELECT * FROM metric('broken')", NewReader(mock, q), Options{})
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "the metric cannot be read: semantics/metrics/broken.yaml: measure may not use FROM")
	assert.NotContains(t, msg, "failed to get")
	assert.NotContains(t, msg, "invalid semantics definition")
}

// A dimension named like a DuckDB function (current_date is not a reserved
// word) groups by the column, not by today's date.
func TestExpand_DimensionNamedLikeAFunctionGroupsByTheColumn(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE sales AS SELECT * FROM (VALUES ('a', 1), ('a', 2), ('b', 5)) t("current_date", amount)`)
	require.NoError(t, err)
	f := &fakeSource{
		metrics: map[string]*Metric{"total": metricNamed(t, "total", "base", "sum(amount)", "current_date")},
		queries: map[string]*queries.Query{"base": {ID: "base", SQL: `SELECT "current_date", amount FROM sales`}},
	}
	r, err := expand(t, f, "SELECT * FROM metric('total', by => 'current_date') ORDER BY 1")
	require.NoError(t, err)
	rows, err := db.Query(r.SQL)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var k string
		var v int64
		require.NoError(t, rows.Scan(&k, &v))
		got[k] = v
	}
	assert.Equal(t, map[string]int64{"a": 3, "b": 5}, got)
}

func TestExpandGrain_ShapeAndRefusals(t *testing.T) {
	f := newFake(t)
	r, err := ExpandGrain(context.Background(), f.metrics["net_revenue"], f, Options{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT count(*) FROM (SELECT 1 FROM (\nSELECT region, month, amount FROM sales\n) AS slaw_grain_net_revenue GROUP BY \"region\", \"month\" HAVING count(*) > 1) AS slaw_grain_dups", r.SQL)
	require.Len(t, r.Bodies, 1)

	// No dimension, no grain.
	_, err = ExpandGrain(context.Background(), f.metrics["orders"], f, Options{})
	require.ErrorIs(t, err, ErrInvalid)

	// A saved query that is not one read statement, uses metric() or sits on
	// another connection is refused exactly like a metric() expansion.
	f.queries["net_revenue_base"].SQL = "SELECT 1; DROP TABLE sales"
	_, err = ExpandGrain(context.Background(), f.metrics["net_revenue"], f, Options{})
	require.ErrorIs(t, err, ErrInvalid)
	f.queries["net_revenue_base"].SQL = "SELECT * FROM metric('orders')"
	_, err = ExpandGrain(context.Background(), f.metrics["net_revenue"], f, Options{})
	require.ErrorIs(t, err, ErrInvalid)
	f.queries["net_revenue_base"] = &queries.Query{ID: "net_revenue_base", SQL: "SELECT region, month, amount FROM sales", Connection: "pg"}
	_, err = ExpandGrain(context.Background(), f.metrics["net_revenue"], f, Options{})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestExpandGrain_DefaultsBindAndDuplicatesAreCounted(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE sales AS SELECT * FROM (VALUES ('a', 1), ('a', 2), ('b', 5), ('b', 6), ('b', 7), ('c', 0)) t(region, amount)`)
	require.NoError(t, err)
	f := &fakeSource{
		metrics: map[string]*Metric{"total": metricNamed(t, "total", "base", "sum(amount)", "region")},
		queries: map[string]*queries.Query{"base": {ID: "base", SQL: "SELECT region, amount FROM sales WHERE amount >= $floor",
			Parameters: []queries.Param{{Name: "floor", Type: "number", Default: "1"}}}},
	}
	r, err := ExpandGrain(context.Background(), f.metrics["total"], f, Options{})
	require.NoError(t, err)
	params, err := r.BindParams(nil, time.Now())
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"floor": float64(1)}, params)
	var n int
	require.NoError(t, db.QueryRow(r.SQL, sql.Named("floor", 1.0)).Scan(&n))
	assert.Equal(t, 2, n, "regions a and b each hold more than one row")
}
