package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/hub"
	"github.com/SandwichLabs/slaw/internal/queries"
	semassert "github.com/SandwichLabs/slaw/internal/semantics/assert"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

const (
	runCustomer = `version: 1
name: customer
grain: one row per customer
table: customers
id_column: customer_id
assertions:
  - {kind: unique, column: customer_id}
  - {kind: not_null, column: created_at}
  - {kind: accepted_values, column: status, values: [active, churned]}
`
	runOrder = `version: 1
name: order_line
grain: one row per order
table: orders
id_column: order_id
assertions:
  - {kind: unique, column: order_id}
`
	runMetric = `version: 1
name: revenue
query: revenue_base
measure: sum(amount)
dimensions: [region]
assertions:
  - {kind: non_negative}
  - {kind: not_null}
`
	runJoin = `# orders belong to a customer
version: 1
name: orders_customers
left: order_line
right: customer
on: [{left: customer_id, right: customer_id}]
cardinality: many_to_one
`
)

// checkWorkspace is a workspace with tables, two entities, a metric over a
// saved query with a parameter, and a join: all hand-written, no agent.
func checkWorkspace(t *testing.T) string {
	t.Helper()
	root := newWS(t)
	for _, q := range []string{
		"CREATE TABLE customers AS SELECT * FROM (VALUES (1, DATE '2026-01-01', 'active'), (2, DATE '2026-01-02', 'churned'), (3, DATE '2026-01-03', 'active')) t(customer_id, created_at, status)",
		"CREATE TABLE orders AS SELECT * FROM (VALUES (10, 1), (11, 1), (12, 2), (13, 3)) t(order_id, customer_id)",
		"CREATE TABLE sales AS SELECT * FROM (VALUES ('east', 10), ('east', 20), ('west', 5)) t(region, amount)",
	} {
		_, stderr, err := runSlaw("query", "--workspace", root, q)
		require.NoError(t, err, stderr)
	}
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{
		ID: "revenue_base", Title: "Revenue base", SQL: "SELECT region, amount FROM sales WHERE amount > $floor;",
		Parameters: []queries.Param{{Name: "floor", Type: "number", Default: "-1000"}},
	}))
	require.NoError(t, ws.Close())
	for rel, body := range map[string]string{
		"semantics/entities/customer.yaml":      runCustomer,
		"semantics/entities/order_line.yaml":    runOrder,
		"semantics/metrics/revenue.yaml":        runMetric,
		"semantics/joins/orders_customers.yaml": runJoin,
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return root
}

func TestSemanticsCheck_CleanDataPasses(t *testing.T) {
	root := checkWorkspace(t)
	out, stderr, err := runSlaw("semantics", "check", "--workspace", root)
	require.NoError(t, err, out+stderr)
	assert.Contains(t, out, "9 passed, 0 failed, 0 could not run")
	assert.Regexp(t, `pass\s+entity customer\s+compile\s+compiles`, out)
	assert.Regexp(t, `pass\s+metric revenue\s+compile\s+compiles`, out)
	assert.Regexp(t, `pass\s+entity customer\s+unique customer_id`, out)
	assert.Regexp(t, `pass\s+metric revenue\s+non_negative`, out)
}

func TestSemanticsCheck_BadDataFailsWithAReadableReport(t *testing.T) {
	root := checkWorkspace(t)
	for _, q := range []string{
		"INSERT INTO customers VALUES (1, NULL, 'paused')",
		"INSERT INTO sales VALUES ('north', -50)",
	} {
		_, stderr, err := runSlaw("query", "--workspace", root, q)
		require.NoError(t, err, stderr)
	}
	out, _, err := runSlaw("semantics", "check", "--workspace", root)
	require.Error(t, err)
	assert.Equal(t, 1, exitStatus(err))
	assert.Regexp(t, `fail\s+entity customer\s+unique customer_id\s+1 violating row\n`, out)
	assert.Regexp(t, `fail\s+entity customer\s+not_null created_at\s+1 violating row\n`, out)
	assert.Regexp(t, `fail\s+entity customer\s+accepted_values status\s+1 violating row\n`, out)
	assert.Regexp(t, `fail\s+metric revenue\s+non_negative\s+1 violating row\n`, out)
	assert.Regexp(t, `pass\s+metric revenue\s+not_null`, out)
	assert.Contains(t, out, "5 passed, 4 failed, 0 could not run")

	out, _, err = runSlaw("semantics", "check", "entity", "order_line", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "2 passed")
	assert.NotContains(t, out, "customer")

	out, _, err = runSlaw("semantics", "check", "--json", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 9)
	assert.Equal(t, "compile", rows[0]["assertion"], "each subject starts with its compile row")
	assert.Equal(t, "pass", rows[0]["status"])
	assert.Equal(t, "fail", rows[1]["status"])
	assert.EqualValues(t, 1, rows[1]["violations"])
}

// A check that cannot run is an error, not a failure, and the rest still run.
// A cloned metric that reads outside the workspace is refused by the policy.
func TestSemanticsCheck_ErrorsAreReportedAndOthersStillRun(t *testing.T) {
	root := checkWorkspace(t)
	outside := filepath.Join(t.TempDir(), "secret.csv")
	require.NoError(t, os.WriteFile(outside, []byte("region,amount\nx,-99\n"), 0o644))
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{ID: "pv_base", Title: "pv", SQL: "SELECT region, amount FROM read_csv('" + outside + "')"}))
	require.NoError(t, ws.Close())
	body := "version: 1\nname: pv\nquery: pv_base\nmeasure: sum(amount)\nassertions:\n  - {kind: non_negative}\n"
	seedMetricFile(t, root, "pv", body)
	seedMetricFile(t, root, "gone", "version: 1\nname: gone\nquery: no_such_query\nmeasure: sum(amount)\nassertions:\n  - {kind: not_null}\n")

	out, _, err := runSlaw("semantics", "check", "metric", "--workspace", root)
	require.Error(t, err)
	assert.Equal(t, 1, exitStatus(err), "a definition the policy refuses fails to compile; the missing saved query could not run")
	assert.Regexp(t, `fail\s+metric pv\s+compile\s+does not compile: .*outside the workspace`, out, "the agent policy refused it, not some other failure")
	assert.Regexp(t, `error\s+metric pv\s+non_negative\s+could not run: not run: the definition does not compile`, out)
	assert.Regexp(t, `error\s+metric gone\s+plan\s+could not run: .*no_such_query`, out)
	assert.Regexp(t, `pass\s+metric revenue\s+non_negative`, out)
	assert.NotContains(t, out, "-99")

	// A failure outranks an error in the exit status.
	_, _, err = runSlaw("query", "--workspace", root, "INSERT INTO sales VALUES ('north', -50)")
	require.NoError(t, err)
	_, _, err = runSlaw("semantics", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))

}

// Typos and setup problems exit 2, a data failure 1, so a script can tell them apart.
func TestSemanticsExitStatusSeparatesUsageFromDataFailures(t *testing.T) {
	root := checkWorkspace(t)
	for _, args := range [][]string{
		{"check", "metric", "nope"},
		{"check", "widget"},
		{"check", "metric", "revenue", "extra"},
		{"check", "--no-such-flag"},
		{"profile", "nope"},
		{"profile"},
		{"profile", "a", "b"},
	} {
		_, _, err := runSlaw(append(append([]string{"semantics"}, args...), "--workspace", root)...)
		require.Error(t, err, args)
		assert.Equal(t, 2, exitStatus(err), args)
	}
	_, _, err := runSlaw("semantics", "check", "metric", "nope", "--workspace", root)
	assert.Contains(t, err.Error(), `no metric named "nope"`)
	// No workspace at all.
	_, _, err = runSlaw("semantics", "check", "--workspace", t.TempDir())
	require.Error(t, err)
	assert.Equal(t, 2, exitStatus(err))

	_, _, err = runSlaw("query", "--workspace", root, "INSERT INTO customers VALUES (1, NULL, 'x')")
	require.NoError(t, err)
	_, _, err = runSlaw("query", "--workspace", root, "INSERT INTO sales VALUES ('n', -5)")
	require.NoError(t, err)
	_, _, err = runSlaw("semantics", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))
	_, _, err = runSlaw("semantics", "check", "entity", "order_line", "--workspace", root)
	assert.Equal(t, 0, exitStatus(err))
}

func TestSemanticsCheck_NoAssertions(t *testing.T) {
	root := newWS(t)
	out, _, err := runSlaw("semantics", "check", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "No entities or metrics defined")
}

func TestSemanticsProfile_WritesMeasuredBlock(t *testing.T) {
	root := checkWorkspace(t)
	path := filepath.Join(root, "semantics", "joins", "orders_customers.yaml")

	out, stderr, err := runSlaw("semantics", "profile", "orders_customers", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Regexp(t, `left rows\s+4`, out)
	assert.Regexp(t, `right rows\s+3`, out)
	assert.Regexp(t, `max right per left\s+1`, out)
	assert.Regexp(t, `verdict\s+consistent`, out)
	assert.NotContains(t, out, "warning")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.True(t, strings.HasPrefix(text, "# orders belong to a customer\n"), "the hand-written comment is kept")
	assert.Contains(t, text, "measured:\n  at: ")
	assert.Contains(t, text, "  left_rows: 4\n  right_rows: 3\n  max_fanout: 1\n  left_orphans: 0\n  right_orphans: 0\n  verdict: consistent\n")

	// A duplicate customer fans the join out: two right rows for customer 1's orders.
	_, _, err = runSlaw("query", "--workspace", root, "INSERT INTO customers VALUES (1, DATE '2026-02-01', 'active'), (4, DATE '2026-02-02', 'active')")
	require.NoError(t, err)
	_, _, err = runSlaw("query", "--workspace", root, "INSERT INTO orders VALUES (14, 99)")
	require.NoError(t, err)
	out, _, err = runSlaw("semantics", "profile", "orders_customers", "--workspace", root)
	require.NoError(t, err)
	assert.Regexp(t, `max right per left\s+2`, out)
	assert.Regexp(t, `left orphans\s+1`, out)
	assert.Regexp(t, `right orphans\s+1`, out)
	assert.Regexp(t, `verdict\s+fan_out`, out)
	assert.Contains(t, out, "warning: declared many_to_one, but a left key matches up to 2 right rows")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), "measured:"), "the block is replaced, not repeated")
	assert.Contains(t, string(data), "  max_fanout: 2\n")
	assert.Contains(t, string(data), "verdict: fan_out\n")

	out, _, err = runSlaw("semantics", "profile", "orders_customers", "--json", "--workspace", root)
	require.NoError(t, err)
	var p map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &p))
	assert.Equal(t, "orders_customers", p["join"])

	_, _, err = runSlaw("semantics", "profile", "nope", "--workspace", root)
	require.Error(t, err)
}

// A hand-written entity whose table tries to smuggle SQL never reaches the
// database: the file is unreadable (the table-name rule), so its checks are
// errors and the data is untouched.
// A metric whose measure names a column its saved query does not return fails
// to compile with the database's own message, and its data checks do not run.
func TestSemanticsCheck_CompileCatchesWhatTheDataChecksWouldHide(t *testing.T) {
	root := checkWorkspace(t)
	seedMetricFile(t, root, "revenue", "version: 1\nname: revenue\nquery: revenue_base\nmeasure: sum(amunt)\ndimensions: [region]\nassertions:\n  - {kind: non_negative}\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "entities", "order_line.yaml"),
		[]byte("version: 1\nname: order_line\ngrain: one row per order\ntable: no_such_table\nid_column: order_id\n"), 0o644))

	out, _, err := runSlaw("semantics", "check", "--workspace", root)
	require.Error(t, err)
	assert.Equal(t, 1, exitStatus(err))
	assert.Regexp(t, `fail\s+metric revenue\s+compile\s+does not compile: .*amunt`, out)
	assert.Regexp(t, `error\s+metric revenue\s+non_negative\s+could not run: not run: the definition does not compile`, out)
	assert.Regexp(t, `fail\s+entity order_line\s+compile\s+does not compile: .*no_such_table`, out)
	assert.Regexp(t, `pass\s+entity customer\s+compile`, out)

	// --compile runs only compile rows and names the same culprits.
	out, _, err = runSlaw("semantics", "check", "--compile", "--json", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 3, "one row per entity and metric")
	for _, r := range rows {
		assert.Equal(t, "compile", r["assertion"])
	}
	statuses := map[string]any{}
	for _, r := range rows {
		statuses[fmt.Sprint(r["subject"])] = r["status"]
	}
	assert.Equal(t, map[string]any{"customer": "pass", "order_line": "fail", "revenue": "fail"}, statuses)

	// Fix both: --compile passes and exits 0.
	seedMetricFile(t, root, "revenue", "version: 1\nname: revenue\nquery: revenue_base\nmeasure: sum(amount)\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "entities", "order_line.yaml"),
		[]byte("version: 1\nname: order_line\ngrain: one row per order\ntable: orders\nid_column: order_id\n"), 0o644))
	out, _, err = runSlaw("semantics", "check", "--compile", "--workspace", root)
	require.NoError(t, err, out)
	assert.Contains(t, out, "3 passed, 0 failed, 0 could not run", "a metric with no dimensions compiles without by")
}

func TestSemanticsCheck_HostileEntityTableNeverRuns(t *testing.T) {
	root := checkWorkspace(t)
	p := filepath.Join(root, "semantics", "entities", "evil.yaml")
	require.NoError(t, os.WriteFile(p, []byte("version: 1\nname: evil\ngrain: x\ntable: \"customers; DROP TABLE customers\"\nid_column: customer_id\nassertions:\n  - {kind: unique, column: customer_id}\n"), 0o644))
	out, _, err := runSlaw("semantics", "check", "entity", "--workspace", root)
	require.Error(t, err)
	assert.Equal(t, 2, exitStatus(err))
	assert.Regexp(t, `error\s+entity evil\s+plan\s+could not run: .*table`, out)
	out, _, err = runSlaw("query", "--workspace", root, "--format", "csv", "SELECT count(*) AS n FROM customers")
	require.NoError(t, err)
	assert.Equal(t, "n\n3\n", out)
}

// A column named like a function or keyword DuckDB does not reserve (current_date)
// is the column, not today's date: the emitted identifiers are quoted.
func TestSemanticsCheck_ColumnNamedLikeAFunction(t *testing.T) {
	root := checkWorkspace(t)
	_, stderr, err := runSlaw("query", "--workspace", root, `CREATE TABLE events AS SELECT * FROM (VALUES ('a'), ('a'), (NULL)) t("current_date")`)
	require.NoError(t, err, stderr)
	p := filepath.Join(root, "semantics", "entities", "event.yaml")
	require.NoError(t, os.WriteFile(p, []byte("version: 1\nname: event\ngrain: one row per event\ntable: events\nid_column: current_date\nassertions:\n  - {kind: unique, column: current_date}\n  - {kind: not_null, column: current_date}\n  - {kind: accepted_values, column: current_date, values: [b]}\n"), 0o644))
	out, _, err := runSlaw("semantics", "check", "entity", "event", "--json", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 4)
	assert.Equal(t, "pass", rows[0]["status"], "the compile row binds the quoted column")
	assert.EqualValues(t, 1, rows[1]["violations"], "one surplus duplicate, not a count of today's date")
	assert.EqualValues(t, 1, rows[2]["violations"])
	assert.EqualValues(t, 2, rows[3]["violations"])
}

// When no check could run, the report says loudly that nothing was verified
// (#302: "0 passed, 0 failed, 57 could not run" was easy to misread).
func TestSemanticsCheck_NothingVerifiedIsSaidOutLoud(t *testing.T) {
	root := newWS(t)
	seedMetricFile(t, root, "gone", "version: 1\nname: gone\nquery: no_such_query\nmeasure: sum(amount)\nassertions:\n  - {kind: not_null}\n")
	out, _, err := runSlaw("semantics", "check", "--workspace", root)
	require.Error(t, err)
	assert.Equal(t, 2, exitStatus(err))
	assert.Contains(t, out, "0 passed, 0 failed, 1 could not run")
	assert.Contains(t, out, "Nothing was verified")

	// With anything verified, the line is not printed.
	root = checkWorkspace(t)
	out, _, err = runSlaw("semantics", "check", "--workspace", root)
	require.NoError(t, err)
	assert.NotContains(t, out, "Nothing was verified")
}

// check and profile open read-only and policy-gated, or MotherDuck is
// unreachable (#302).
func TestSemanticsRunOpensReadOnlyAndPolicyGated(t *testing.T) {
	o := semanticsRunOpenOptions("slaw semantics check")
	assert.Equal(t, hub.ModeReadOnly, o.Mode)
	assert.True(t, o.PolicyGatedRuns)
}

// Compile rows that passed do not make a run that checked no data look clean.
func TestSemanticsCheck_NoDataCheckCouldRunIsSaidOutLoud(t *testing.T) {
	root := checkWorkspace(t)
	// The saved query now reads a table the data check cannot get at: both the
	// compile and the data checks of "revenue" would fail, so use a closed ctx
	// stand-in instead: an entity with an assertion and a metric, checked
	// compile-only, says nothing extra.
	out, _, err := runSlaw("semantics", "check", "--compile", "--workspace", root)
	require.NoError(t, err, out)
	assert.NotContains(t, out, "Nothing was verified")
	assert.NotContains(t, out, "No data check could run")

	rows := []semassert.Row{
		{SubjectKind: "metric", Subject: "m", Assertion: "compile", Status: "pass"},
		{SubjectKind: "metric", Subject: "m", Assertion: "non_negative", Status: "error", Error: "connection lost"},
	}
	var buf strings.Builder
	printCheckReport(&buf, rows)
	assert.Contains(t, buf.String(), "No data check could run")
	assert.NotContains(t, buf.String(), "Nothing was verified")

	buf.Reset()
	printCheckReport(&buf, []semassert.Row{{SubjectKind: "metric", Subject: "m", Assertion: "compile", Status: "error", Error: "x"}})
	assert.Contains(t, buf.String(), "Nothing was verified")
}
