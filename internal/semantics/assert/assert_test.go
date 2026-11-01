package assert

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
)

func entity(table string, as ...semantics.Assertion) *semantics.Entity {
	return &semantics.Entity{Version: 1, Name: "customer", Grain: "one row per customer", Table: table, IDColumn: "customer_id", Assertions: as}
}

func TestPlanEntity_Shapes(t *testing.T) {
	e := entity("prod_pg.public.customers",
		semantics.Assertion{Kind: semantics.AssertUnique, Column: "customer_id"},
		semantics.Assertion{Kind: semantics.AssertNotNull, Column: "created_at"},
		semantics.Assertion{Kind: semantics.AssertAcceptedValues, Column: "status", Values: []string{"active", "x'); DROP TABLE t; --"}},
	)
	e.Connection = "prod_pg"
	checks, err := PlanEntity(e)
	require.NoError(t, err)
	require.Len(t, checks, 3)
	assert.Equal(t, `SELECT count("customer_id") - count(DISTINCT "customer_id") FROM "prod_pg"."public"."customers"`, checks[0].SQL)
	assert.Equal(t, `SELECT count(*) FROM "prod_pg"."public"."customers" WHERE "created_at" IS NULL`, checks[1].SQL)
	assert.Equal(t, `SELECT count(*) FROM "prod_pg"."public"."customers" WHERE CAST("status" AS VARCHAR) NOT IN ($v0, $v1)`, checks[2].SQL)
	assert.Equal(t, map[string]any{"v0": "active", "v1": "x'); DROP TABLE t; --"}, checks[2].Params, "values travel only as bound params")
	for _, c := range checks {
		assert.Equal(t, "prod_pg", c.Connection)
		assert.Equal(t, "customer", c.Subject)
		assert.NotContains(t, c.SQL, "DROP")
	}
}

func TestPlanEntity_HostileNamesAreRefused(t *testing.T) {
	for _, table := range []string{
		"customers; DROP TABLE customers", "customers--", `"customers"`, "a.b.c.d", "customers UNION SELECT 1",
		"customers WHERE 1=1", "c'; ATTACH 'x' AS y; --", "", "select", "read_csv('/etc/passwd')", "t\nx", "ünï",
	} {
		_, err := PlanEntity(entity(table, semantics.Assertion{Kind: semantics.AssertNotNull, Column: "id"}))
		require.Error(t, err, table)
		assert.ErrorIs(t, err, semantics.ErrInvalid, table)
	}
	for _, col := range []string{"id; DROP TABLE x", "id) OR (1=1", `"id"`, "ID", "id--", "from"} {
		_, err := PlanEntity(entity("customers", semantics.Assertion{Kind: semantics.AssertNotNull, Column: col}))
		require.Error(t, err, col)
	}
}

// safeSQL is every statement shape the planner may emit: only validated
// identifiers, $vN holes and fixed keywords.
var safeSQL = regexp.MustCompile(`^SELECT (count\(\*\)|count\("\w+"\) - count\(DISTINCT "\w+"\)) FROM "[A-Za-z_]\w*"(\."[A-Za-z_]\w*"){0,2}( WHERE ("\w+" IS NULL|CAST\("\w+" AS VARCHAR\) NOT IN \(\$v0(, \$v\d+)*\)))?$`)

// FuzzPlanEntity checks that whatever names arrive (the seed corpus runs in a
// normal test), a plan that succeeds emits only the known statement shapes.
func FuzzPlanEntity(f *testing.F) {
	for _, s := range []string{"customers", "a.b.c", "x; DROP", "t'--", "x y", "\x00", "a..b", "A_1"} {
		f.Add(s, "id", s)
	}
	f.Fuzz(func(t *testing.T, table, column, value string) {
		for _, a := range []semantics.Assertion{
			{Kind: semantics.AssertUnique, Column: column},
			{Kind: semantics.AssertNotNull, Column: column},
			{Kind: semantics.AssertAcceptedValues, Column: column, Values: []string{value}},
		} {
			checks, err := PlanEntity(entity(table, a))
			if err != nil {
				continue
			}
			require.Len(t, checks, 1)
			require.Regexp(t, safeSQL, checks[0].SQL, "table %q column %q", table, column)
			require.NotContains(t, checks[0].SQL, value+"'", "a value is never part of the text")
		}
	})
}

func TestPlanEntity_RandomHostileNamesNeverReachSQL(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const alphabet = "abcXYZ_09.;'\"()- \n\t*,=/\\$%"
	for i := 0; i < 5000; i++ {
		b := make([]byte, 1+rng.Intn(24))
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		s := string(b)
		checks, err := PlanEntity(entity(s, semantics.Assertion{Kind: semantics.AssertNotNull, Column: s}))
		if err == nil {
			require.Regexp(t, safeSQL, checks[0].SQL, s)
		}
	}
}

func TestPlanEntity_RefusesAnUnreadableOrOtherKind(t *testing.T) {
	_, err := PlanEntity(&semantics.Entity{Name: "bad", Meta: semantics.Meta{Error: "line 3: nope"}})
	assert.ErrorContains(t, err, "cannot be read")
	_, err = PlanEntity(entity("t", semantics.Assertion{Kind: semantics.AssertNonNegative}))
	assert.Error(t, err)
	_, err = PlanEntity(nil)
	assert.Error(t, err)
}

type fakeSource struct {
	m *semantics.Metric
	q *queries.Query
}

func (f fakeSource) Metric(_ context.Context, name string) (*semantics.Metric, error) {
	if f.m != nil && f.m.Name == name {
		return f.m, nil
	}
	return nil, semantics.ErrNotFound
}

func (f fakeSource) Query(_ context.Context, id string) (*queries.Query, error) {
	if f.q != nil && f.q.ID == id {
		return f.q, nil
	}
	return nil, queries.ErrNotFound
}

func metricFixture() (*semantics.Metric, fakeSource) {
	m := &semantics.Metric{Version: 1, Name: "net_revenue", Query: "base", Measure: "sum(amount)", Dimensions: []string{"region", "month"},
		Assertions: []semantics.Assertion{{Kind: semantics.AssertNonNegative}, {Kind: semantics.AssertNotNull}}}
	q := &queries.Query{ID: "base", SQL: "SELECT region, month, amount FROM sales WHERE amount > $floor",
		Connection: "prod_pg", Parameters: []queries.Param{{Name: "floor", Type: "number", Default: "2"}}}
	return m, fakeSource{m: m, q: q}
}

func TestPlanMetric_ExpandedAtFullDimensionSetWithDefaults(t *testing.T) {
	m, src := metricFixture()
	checks, err := PlanMetric(context.Background(), m, src, time.Now())
	require.NoError(t, err)
	require.Len(t, checks, 2)
	for _, c := range checks {
		assert.Equal(t, "prod_pg", c.Connection)
		assert.Contains(t, c.SQL, `GROUP BY "region", "month"`)
		assert.Contains(t, c.SQL, `sum(amount) AS "net_revenue"`)
		assert.NotContains(t, strings.ToLower(c.SQL), "metric(")
		assert.EqualValues(t, 2, c.Params["floor"], "the saved query's default is bound")
	}
	assert.True(t, strings.HasSuffix(checks[0].SQL, `WHERE "net_revenue" < 0`), checks[0].SQL)
	assert.True(t, strings.HasSuffix(checks[1].SQL, `WHERE "net_revenue" IS NULL`), checks[1].SQL)
}

func TestPlanMetric_Refusals(t *testing.T) {
	m, src := metricFixture()
	src.q = nil
	_, err := PlanMetric(context.Background(), m, src, time.Now())
	assert.ErrorContains(t, err, `saved query "base"`)

	m2, src2 := metricFixture()
	src2.q.SQL = "SELECT 1; DROP TABLE sales"
	_, err = PlanMetric(context.Background(), m2, src2, time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, semantics.ErrInvalid)

	m3, src3 := metricFixture()
	m3.Assertions = []semantics.Assertion{{Kind: semantics.AssertUnique, Column: "x"}}
	_, err = PlanMetric(context.Background(), m3, src3, time.Now())
	assert.Error(t, err)
}

func rs(v any) *engine.ResultSet {
	return &engine.ResultSet{Columns: []engine.Column{{Name: "n"}}, Rows: [][]any{{v}}}
}

func TestRun_StatusesAndPartialFailure(t *testing.T) {
	checks := []Check{
		{Kind: "a", SQL: "pass"}, {Kind: "b", SQL: "fail"}, {Kind: "c", SQL: "boom"}, {Kind: "d", SQL: "bad"},
		{Kind: "e", PlanError: "no saved query"}, {Kind: "f", SQL: "empty"}, {Kind: "g", SQL: "wide"},
	}
	var ran []string
	res := Run(context.Background(), checks, Runner{Run: func(_ context.Context, _ string, sql string, _ map[string]any) (*engine.ResultSet, error) {
		ran = append(ran, sql)
		switch sql {
		case "pass":
			return rs(int64(0)), nil
		case "fail":
			return rs(int64(3)), nil
		case "boom":
			return nil, errors.New("connection prod_pg is down")
		case "bad":
			return rs("text"), nil
		case "wide":
			return rs(int32(1)), nil
		}
		return nil, nil
	}})
	require.Len(t, res, 7)
	got := []string{}
	for _, r := range res {
		got = append(got, r.Status)
	}
	assert.Equal(t, []string{"pass", "fail", "error", "error", "error", "error", "fail"}, got)
	assert.EqualValues(t, 3, res[1].Violations)
	assert.Contains(t, res[2].Error, "connection prod_pg is down")
	assert.Equal(t, "no saved query", res[4].Error)
	assert.NotContains(t, ran, "", "a plan error is never run")
	assert.Equal(t, []string{"pass", "fail", "boom", "bad", "empty", "wide"}, ran)
	for _, r := range res {
		assert.Empty(t, r.Receipt)
	}
}

func TestRun_CanceledContextMarksTheRestAsErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	res := Run(ctx, []Check{{SQL: "a"}, {SQL: "b"}}, Runner{Run: func(context.Context, string, string, map[string]any) (*engine.ResultSet, error) {
		calls++
		cancel()
		return rs(int64(0)), nil
	}})
	assert.Equal(t, 1, calls)
	assert.Equal(t, []string{StatusPass, StatusError}, []string{res[0].Status, res[1].Status})
}

func TestFailure(t *testing.T) {
	r := Failure(SubjectMetric, "x", errors.New("nope"))
	assert.Equal(t, StatusError, r.Status)
	assert.Equal(t, "x", r.Check.Subject)
}

func TestPlanEntityCompile_QuotedDeclaredColumnsOnly(t *testing.T) {
	e := entity("prod_pg.public.customers")
	e.Connection = "prod_pg"
	e.Columns = []semantics.Column{{Name: "email", PII: true}, {Name: "customer_id"}, {Name: "created_at"}}
	c, err := PlanEntityCompile(e)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "customer_id", "email", "created_at" FROM "prod_pg"."public"."customers"`, c.SQL)
	assert.True(t, c.VetOnly)
	assert.Equal(t, KindCompile, c.Kind)
	assert.Equal(t, "prod_pg", c.Connection)
	assert.Empty(t, c.Params)

	_, err = PlanEntityCompile(entity("customers; DROP TABLE x"))
	assert.ErrorIs(t, err, semantics.ErrInvalid)
	_, err = PlanEntityCompile(nil)
	assert.Error(t, err)
}

func TestPlanMetricCompile_EveryDimensionAndTheSavedQueryDefaults(t *testing.T) {
	m, src := metricFixture()
	c, err := PlanMetricCompile(context.Background(), m, src, time.Now())
	require.NoError(t, err)
	assert.True(t, c.VetOnly)
	assert.Equal(t, KindCompile, c.Kind)
	assert.Equal(t, "prod_pg", c.Connection)
	assert.Contains(t, c.SQL, `GROUP BY "region", "month"`)
	assert.Contains(t, c.SQL, `sum(amount) AS "net_revenue"`)
	assert.NotContains(t, strings.ToLower(c.SQL), "metric(")
	assert.EqualValues(t, 2, c.Params["floor"])

	// No dimensions: the scalar, no by.
	m.Dimensions = nil
	c, err = PlanMetricCompile(context.Background(), m, src, time.Now())
	require.NoError(t, err)
	assert.NotContains(t, c.SQL, "GROUP BY")

	src.q = nil
	_, err = PlanMetricCompile(context.Background(), m, src, time.Now())
	assert.ErrorContains(t, err, `saved query "base"`)
}

type connDown struct{}

func (connDown) Error() string        { return "failed to connect \"md\": token refused" }
func (connDown) ConnectFailure() bool { return true }

type defErr struct{ msg string }

func (e defErr) Error() string         { return e.msg }
func (defErr) DefinitionProblem() bool { return true }

func TestRun_CompileChecks(t *testing.T) {
	compile := func(sub string) Check {
		return Check{SubjectKind: SubjectMetric, Subject: sub, Kind: KindCompile, SQL: sub, VetOnly: true}
	}
	data := func(sub string) Check {
		return Check{SubjectKind: SubjectMetric, Subject: sub, Kind: "non_negative", SQL: "count"}
	}
	var vetted, ran []string
	r := Runner{
		Vet: func(_ context.Context, _ string, sql string, _ map[string]any) error {
			vetted = append(vetted, sql)
			switch sql {
			case "bad":
				return defErr{"could not validate query: Binder Error: column \"x\" not found"}
			case "down":
				return connDown{}
			}
			return nil
		},
		Run: func(_ context.Context, _ string, sql string, _ map[string]any) (*engine.ResultSet, error) {
			ran = append(ran, sql)
			return rs(int64(0)), nil
		},
	}
	res := Run(context.Background(), []Check{compile("ok"), data("ok"), compile("bad"), data("bad"), data("bad"), compile("down")}, r)
	got := []string{}
	for _, x := range res {
		got = append(got, x.Status)
	}
	assert.Equal(t, []string{"pass", "pass", "fail", "error", "error", "error"}, got)
	assert.Contains(t, res[2].Error, "Binder Error", "a binder error is a fail")
	assert.Equal(t, "not run: the definition does not compile", res[3].Error)
	assert.Contains(t, res[5].Error, "token refused", "a connection that cannot connect is an error, not a fail")
	assert.Equal(t, []string{"ok", "bad", "down"}, vetted)
	assert.Equal(t, []string{"count"}, ran, "only the compiling subject's data check ran")

	// A wrapped connect failure is still a connect failure.
	r.Vet = func(context.Context, string, string, map[string]any) error { return fmt.Errorf("vet: %w", connDown{}) }
	assert.Equal(t, StatusError, Run(context.Background(), []Check{compile("w")}, r)[0].Status)

	// A canceled context is an error, whatever the vet said.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Vet = func(context.Context, string, string, map[string]any) error { return errors.New("interrupted") }
	assert.Equal(t, StatusError, Run(ctx, []Check{compile("c")}, r)[0].Status)

	assert.Equal(t, StatusError, Run(context.Background(), []Check{compile("n")}, Runner{})[0].Status, "no vet is an error")

	// Anything that is not a definition problem (an I/O failure while binding,
	// a closed workspace) says nothing about the files: an error, not a fail.
	r.Vet = func(context.Context, string, string, map[string]any) error {
		return errors.New("IO Error: connection reset")
	}
	assert.Equal(t, StatusError, Run(context.Background(), []Check{compile("io")}, r)[0].Status)
	r.Vet = func(context.Context, string, string, map[string]any) error {
		return fmt.Errorf("vet: %w", defErr{"nope"})
	}
	assert.Equal(t, StatusFail, Run(context.Background(), []Check{compile("wrapped")}, r)[0].Status)
}

func TestPlanMetricCompile_RequiredParameterWithoutDefaultStillCompiles(t *testing.T) {
	m, src := metricFixture()
	src.q.SQL = "SELECT region, month, amount FROM sales WHERE amount > $floor AND month >= $since AND live = $live AND note <> $note"
	src.q.Parameters = []queries.Param{
		{Name: "floor", Type: "number", Required: true},
		{Name: "since", Type: "date", Required: true},
		{Name: "live", Type: "boolean", Required: true},
		{Name: "note", Type: "text", Required: true},
	}
	m.Assertions = nil
	c, err := PlanMetricCompile(context.Background(), m, src, time.Now())
	require.NoError(t, err)
	assert.Len(t, c.Params, 4, "every required parameter gets a type-valid placeholder")
	assert.EqualValues(t, 0, c.Params["floor"])
	assert.Equal(t, false, c.Params["live"])
}
