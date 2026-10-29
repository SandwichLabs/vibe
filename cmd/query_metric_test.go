package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// metricWorkspace is a workspace with a table, a saved query and a hand-written
// metric over it: no agent involved, two files and a table.
func metricWorkspace(t *testing.T) string {
	t.Helper()
	root := newWS(t)
	_, stderr, err := runVibe("query", "--workspace", root, "CREATE TABLE sales AS SELECT * FROM (VALUES ('east', 1, 10), ('east', 2, 20), ('west', 1, 5), ('west', 2, 1)) t(region, month, amount)")
	require.NoError(t, err, stderr)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{
		ID: "net_revenue_base", Title: "Net revenue base", SQL: "SELECT region, month, amount FROM sales WHERE amount > $min;",
		Parameters: []queries.Param{{Name: "min", Type: "number", Default: "2"}},
	}))
	require.NoError(t, ws.Close())
	seedMetricFile(t, root, "net_revenue", cliMetric)
	return root
}

func seedMetricFile(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, "semantics", "metrics", name+".yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

func TestQuery_HandWrittenMetricRuns(t *testing.T) {
	root := metricWorkspace(t)
	out, stderr, err := runVibe("query", "--workspace", root, "--format", "csv",
		"SELECT * FROM metric('net_revenue', by => 'region') ORDER BY region")
	require.NoError(t, err, stderr)
	assert.Equal(t, "region,net_revenue\neast,30\nwest,5\n", out, "the saved query's default $min = 2 applied")
	assert.Contains(t, stderr, "expanded 1 metric: net_revenue")

	out, stderr, err = runVibe("query", "--workspace", root, "--format", "csv", "SELECT metric('net_revenue') AS total")
	require.NoError(t, err, stderr)
	assert.Equal(t, "total\n35\n", out)

	out, _, err = runVibe("query", "--workspace", root, "--format", "csv", "SELECT * FROM metric('net_revenue', by => ['month', 'region']) WHERE net_revenue > 10")
	require.NoError(t, err)
	assert.Equal(t, "month,region,net_revenue\n2,east,20\n", out)
}

func TestQuery_MetricRefusalsAreReadable(t *testing.T) {
	root := metricWorkspace(t)
	for q, want := range map[string]string{
		"SELECT * FROM metric('nope')":                                 `there is no metric named 'nope'`,
		"SELECT * FROM metric('net_revenue', by => 'colour')":          `'colour' is not a dimension of net_revenue (dimensions: region, month)`,
		"SELECT * FROM metric(net_revenue)":                            `the metric name must be a plain 'quoted' literal`,
		"SELECT * FROM metric('net_revenue', by => 'region'; DROP x)":  `metric(): the call is missing its closing parenthesis`,
		"SELECT * FROM metric('net_revenue') WHERE net_revenue > $min": `$min belongs to metric net_revenue's saved query; it can't be used outside metric()`,
	} {
		_, _, err := runVibe("query", "--workspace", root, q)
		require.Error(t, err, q)
		assert.Contains(t, err.Error(), want, q)
	}
	// A saved query edited into a write behind a metric never runs: it is not one read statement.
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{ID: "sneaky", Title: "Sneaky", SQL: "WITH x AS (SELECT 1 AS amount) INSERT INTO sales SELECT 'z', 9, amount FROM x"}))
	require.NoError(t, ws.Close())
	seedMetricFile(t, root, "sneaky", "version: 1\nname: sneaky\nquery: sneaky\nmeasure: count(*)\n")
	_, _, err = runVibe("query", "--workspace", root, "SELECT * FROM metric('sneaky')")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a plain read")
	out, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT count(*) AS n FROM sales")
	require.NoError(t, err)
	assert.Equal(t, "n\n4\n", out)
}

// A cloned repository's metric whose saved query reads outside the workspace is
// refused at run, even under --read-only, with the metric's name.
func TestQuery_HostileClonedMetricIsRefused(t *testing.T) {
	root := metricWorkspace(t)
	outside := filepath.Join(t.TempDir(), "secret.csv")
	require.NoError(t, os.WriteFile(outside, []byte("region,month,amount\nx,1,99\n"), 0o644))
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{ID: "pv_base", Title: "pv", SQL: "SELECT region, month, amount FROM read_csv('" + outside + "')"}))
	require.NoError(t, ws.Close())
	seedMetricFile(t, root, "pv", "version: 1\nname: pv\nquery: pv_base\nmeasure: sum(amount)\n")
	for _, args := range [][]string{{"--read-only"}, {}} {
		out, _, err := runVibe(append(append([]string{"query", "--workspace", root}, args...), "SELECT metric('pv')")...)
		require.Error(t, err, args)
		assert.Contains(t, err.Error(), "metric 'pv' is refused")
		assert.NotContains(t, out, "99")
	}
	// The person's own SQL outside a metric is not held to the policy.
	out, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT count(*) AS n FROM read_csv('"+outside+"')")
	require.NoError(t, err)
	assert.Equal(t, "n\n1\n", out)
}
