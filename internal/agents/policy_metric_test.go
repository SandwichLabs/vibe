package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
)

// metric() is expanded before the policy: the policy judges the expanded
// statement, a hostile saved query behind a metric is refused, and `metric` is
// not a function the policy would allow if it ever reached it.

func putMetric(t *testing.T, fx *policyFixture, name, query, measure string) {
	t.Helper()
	m, err := semantics.DecodeMetric(name, []byte("version: 1\nname: "+name+"\nquery: "+query+"\nmeasure: "+measure+"\ndimensions: [region, month]\n"))
	require.NoError(t, err)
	require.NoError(t, fx.h.access.Semantics().PutMetric(context.Background(), m))
}

func putSaved(t *testing.T, fx *policyFixture, id, sqlText string, params ...queries.Param) {
	t.Helper()
	require.NoError(t, fx.h.access.Queries().Create(context.Background(), &queries.Query{ID: id, Title: id, SQL: sqlText, Parameters: params}))
}

func metricFixture(t *testing.T, mode SandboxMode) *policyFixture {
	t.Helper()
	fx := newPolicyFixture(t, mode)
	fx.exec(`CREATE TABLE sales (region VARCHAR, month INTEGER, amount INTEGER);
		INSERT INTO sales VALUES ('east', 1, 10), ('east', 2, 20), ('west', 1, 5), ('west', 2, 1)`)
	putSaved(t, fx, "net_revenue_base", "SELECT region, month, amount FROM sales WHERE amount > $min;\n",
		queries.Param{Name: "min", Type: "number", Default: "2"})
	putMetric(t, fx, "net_revenue", "net_revenue_base", "sum(amount)")
	return fx
}

func TestPolicyMetric_RunQueryGroupsAndReportsUse(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		fx := metricFixture(t, mode)
		resp := fx.run("SELECT * FROM metric('net_revenue', by => 'region') ORDER BY region")
		require.Nil(t, resp.Error, "%s: %+v", mode, resp.Error)
		res := mapOf(t, resp.Result)
		assert.Equal(t, []string{"region", "net_revenue"}, res["columns"])
		rows, ok := res["rows"].([][]any)
		require.True(t, ok)
		require.Len(t, rows, 2)
		assert.EqualValues(t, 30, rows[0][1])
		assert.EqualValues(t, 5, rows[1][1], "the declared default $min = 2 filtered the 1")
		assert.Equal(t, []map[string]any{{"name": "net_revenue", "by": []string{"region"}, "query": "net_revenue_base"}}, res["metrics_used"])
	}
}

func TestPolicyMetric_CallerParamWins(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	resp := fx.call("run_query", map[string]any{"sql": "SELECT * FROM metric('net_revenue')", "params": map[string]any{"min": float64(10)}})
	require.Nil(t, resp.Error, "%+v", resp.Error)
	rows, ok := mapOf(t, resp.Result)["rows"].([][]any)
	require.True(t, ok)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 20, rows[0][0])
}

func TestPolicyMetric_PolicySeesTheExpandedText(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	ctx := context.Background()
	mx, params, err := fx.h.expandMetrics(ctx, "", "SELECT * FROM metric('net_revenue', by => 'region')", nil)
	require.NoError(t, err)
	assert.Contains(t, mx.SQL, "GROUP BY region")
	assert.NotContains(t, mx.SQL, "metric(")
	assert.Contains(t, params, "min")
	v := fx.h.classify(ctx, fx.db, mx.SQL, params)
	assert.Equal(t, sqlClassRead, v.Class, v.Reason)

	// The policy itself never sees metric(): handed the unexpanded text it
	// refuses (no such function), it is not on any allow-list.
	v = fx.h.classify(ctx, fx.db, "SELECT * FROM metric('net_revenue', by => 'region')", nil)
	assert.NotEqual(t, sqlClassRead, v.Class)
}

func TestPolicyMetric_HostileSavedQueryIsRefused(t *testing.T) {
	fx := metricFixture(t, ModeFull)
	// Passes the lexical shape (one SELECT), refused by the policy: a file
	// outside data/.
	putSaved(t, fx, "evil_base", "SELECT 1 AS region, 1 AS month, length(content) AS amount FROM read_text('/etc/passwd')")
	putMetric(t, fx, "evil", "evil_base", "sum(amount)")
	resp := fx.run("SELECT * FROM metric('evil')")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "metric 'evil': ")

	// A write behind a name never gets as far as the policy.
	for id, body := range map[string]string{
		"copy_base":   "COPY sales TO '/tmp/x.csv'",
		"attach_base": "ATTACH 'x.db'",
		"drop_base":   "DROP TABLE sales",
		"multi_base":  "SELECT 1; DROP TABLE sales",
	} {
		putSaved(t, fx, id, body)
		putMetric(t, fx, "m_"+id[:4], id, "count(*)")
		resp = fx.run("SELECT * FROM metric('m_" + id[:4] + "')")
		require.NotNil(t, resp.Error, id)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, id)
		assert.Contains(t, resp.Error.Message, "not one read statement", id)
	}
	assert.Equal(t, 4, fx.count("SELECT count(*) FROM sales"))
}

func TestPolicyMetric_AWriteAroundAMetricStaysAWrite(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	resp := fx.run("INSERT INTO t2 SELECT net_revenue FROM metric('net_revenue')")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	assert.Equal(t, 1, fx.count("SELECT count(*) FROM t2"))
}

func TestPolicyMetric_RefusalsAreInvalidParams(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	for _, q := range []string{
		"SELECT * FROM metric('nope')",
		"SELECT * FROM metric('net_revenue', by => 'colour')",
		"SELECT * FROM metric(net_revenue)",
	} {
		resp := fx.run(q)
		require.NotNil(t, resp.Error, q)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, q)
	}
}

func TestPolicyMetric_ConnectionRule(t *testing.T) {
	fx := metricFixture(t, ModeFull)
	require.NoError(t, fx.h.access.Queries().Create(context.Background(), &queries.Query{ID: "ledger_base", Title: "ledger", SQL: "SELECT id, amount FROM ledger", Connection: "ro"}))
	m, err := semantics.DecodeMetric("ledger_total", []byte("version: 1\nname: ledger_total\nquery: ledger_base\nmeasure: sum(amount)\n"))
	require.NoError(t, err)
	require.NoError(t, fx.h.access.Semantics().PutMetric(context.Background(), m))
	resp := fx.run("SELECT * FROM metric('ledger_total')")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "defined on connection 'ro'")
	resp = fx.call("run_saved_query", map[string]any{"id": "ledger_base"})
	require.Nil(t, resp.Error)
}

func TestPolicyMetric_ReadOnlyBindingsAndVets(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	ctx := context.Background()
	rs, _, err := fx.h.RunReadOnlySQLIn(ctx, "", "SELECT region, net_revenue FROM metric('net_revenue', by => 'region') ORDER BY 1", nil, 100)
	require.NoError(t, err)
	require.Len(t, rs.Rows, 2)
	_, vf := fx.h.vetNotebookSQL(ctx, "", "SELECT * FROM metric('net_revenue', by => 'month')", "notebook sql cells")
	assert.Nil(t, vf)
	_, vf = fx.h.vetNotebookSQL(ctx, "", "SELECT * FROM metric('nope')", "notebook sql cells")
	require.NotNil(t, vf)
	assert.Equal(t, ErrInvalidParams, vf.code)
	resp := fx.call("preview_query", map[string]any{"sql": "SELECT * FROM metric('net_revenue', by => 'region')"})
	require.Nil(t, resp.Error, "%+v", resp.Error)
	resp = fx.call("preview_query", map[string]any{"sql": "SELECT * FROM metric('nope')"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
}

// A store error behind a metric (a saved query whose header failed to read)
// can hold an absolute path; the agent never sees it.
func TestPolicyMetric_StoreErrorsAreScrubbed(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	require.NoError(t, fx.h.access.Queries().Create(context.Background(), &queries.Query{ID: "bad_base", Title: "bad", SQL: "SELECT 1",
		Error: "failed to read /home/zac/secret-project/queries/bad_base.sql: permission denied"}))
	putMetric(t, fx, "bad", "bad_base", "count(*)")
	resp := fx.run("SELECT * FROM metric('bad')")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "<path>")
	assert.NotContains(t, resp.Error.Message, "/home/zac")
}

// A $name in the statement the agent wrote that only a metric declares would
// silently take the metric's default: it is refused, unless the agent sent a value.
func TestPolicyMetric_OuterPlaceholderIsRefusedUnlessSupplied(t *testing.T) {
	fx := metricFixture(t, ModeReadOnly)
	q := "SELECT * FROM metric('net_revenue') WHERE net_revenue > $min"
	resp := fx.run(q)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "$min belongs to metric net_revenue's saved query; it can't be used outside metric()")
	resp = fx.call("run_query", map[string]any{"sql": q, "params": map[string]any{"min": float64(10)}})
	require.Nil(t, resp.Error, "%+v", resp.Error)
}
