package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cliMetric = "# hand-written\nversion: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(amount)\ndimensions: [region, month]\n"

func seedSemantics(t *testing.T, root string) {
	t.Helper()
	for rel, body := range map[string]string{
		"semantics/metrics/net_revenue.yaml":    cliMetric,
		"semantics/metrics/broken.yaml":         "version: 1\nname: broken\nquery: q\n",
		"semantics/entities/customer.yaml":      "version: 1\nname: customer\ngrain: one row per customer\ntable: customers\nid_column: customer_id\n",
		"semantics/joins/orders_customers.yaml": "version: 1\nname: orders_customers\nleft: orders\nright: customer\non: [{left: customer_id, right: customer_id}]\ncardinality: many_to_one\n",
		"semantics/glossary.md":                 "## Active customer\n\nBought lately.\n\n## Refund\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
}

func TestSemanticsList_EmptyHandWrittenAndBroken(t *testing.T) {
	ws := newWS(t)
	out, stderr, err := runAssay("semantics", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "No semantic definitions")

	seedSemantics(t, ws)
	out, stderr, err = runAssay("semantics", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "KIND")
	assert.Regexp(t, `metric\s+net_revenue\s+sum\(amount\) by region, month\s+-`, out)
	assert.Regexp(t, `metric\s+broken\s+-\s+\S+.*measure is required`, out, "a corrupt file lists with its error")
	assert.Regexp(t, `entity\s+customer\s+one row per customer`, out)
	assert.Regexp(t, `join\s+orders_customers\s+orders to customer \(many_to_one\)`, out)
	assert.Regexp(t, `glossary\s+glossary.md\s+2 terms`, out)

	out, stderr, err = runAssay("semantics", "list", "--json", "--workspace", ws)
	require.NoError(t, err, stderr)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 5)
	byName := map[string]map[string]any{}
	for _, r := range rows {
		name, _ := r["name"].(string)
		byName[name] = r
	}
	assert.NotEmpty(t, byName["broken"]["error"])
	assert.NotEmpty(t, byName["net_revenue"]["rev"])
	assert.Nil(t, byName["net_revenue"]["error"])
}

func TestSemanticsShow_PrintsTheFileAsStored(t *testing.T) {
	ws := newWS(t)
	seedSemantics(t, ws)

	out, stderr, err := runAssay("semantics", "show", "metric", "net_revenue", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Equal(t, cliMetric, out, "comments and layout are kept")

	out, _, err = runAssay("semantics", "show", "glossary", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "## Refund")

	out, stderr, err = runAssay("semantics", "show", "metric", "broken", "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, out, "name: broken", "a broken file is still printed")
	assert.Contains(t, err.Error()+stderr, "does not pass validation")

	_, _, err = runAssay("semantics", "show", "metric", "absent", "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	_, _, err = runAssay("semantics", "show", "colour", "x", "--workspace", ws)
	require.Error(t, err)
	_, _, err = runAssay("semantics", "show", "metric", "--workspace", ws)
	require.Error(t, err)
}

func TestBundle_SemanticsRoundTripThroughTheCLI(t *testing.T) {
	src := newWS(t)
	seedSemantics(t, src)
	out := filepath.Join(t.TempDir(), "share.assay")
	stdout, stderr, err := runAssay("export", "bundle", "-o", out, "--semantics", "metrics/net_revenue", "--semantics", "glossary", "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "2 semantic definitions")

	dst := newWS(t)
	stdout, stderr, err = runAssay("import", out, "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "semantics/metrics/net_revenue")
	got, err := os.ReadFile(filepath.Join(dst, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, err)
	assert.Equal(t, cliMetric, string(got))
}

func TestSemantics_ControlCharactersNeverReachTheTerminal(t *testing.T) {
	ws := newWS(t)
	dir := filepath.Join(ws, "semantics", "metrics")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "evil.yaml"), []byte("version: 1\nname: evil\n# \x1b]0;pwned\x07\nquery: q\nmeasure: sum(a)\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x\x1b[31m.yaml"), []byte("x"), 0o644))

	out, stderr, err := runAssay("semantics", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "\x07")
	assert.Contains(t, out, "<U+001B>")

	out, stderr, err = runAssay("semantics", "show", "metric", "evil", "--workspace", ws)
	require.Error(t, err)
	assert.NotContains(t, out+stderr+err.Error(), "\x1b")
	assert.Contains(t, out, "<U+001B>]0;pwned<U+0007>")
}

func TestSemanticsList_PrintsALongErrorWhole(t *testing.T) {
	ws := newWS(t)
	// An unquoted (key, unit) inside {...}: a long decode error, with the quoting hint.
	body := "version: 1\nname: sleep_avg\nquery: sleep_base\nmeasure: avg(score)\n" +
		"assertions: [{kind: accepted_values, column: some_long_column_name_here, values: (key, unit)}]\n"
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "semantics", "metrics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "semantics", "metrics", "sleep_avg.yaml"), []byte(body), 0o644))

	out, stderr, err := runAssay("semantics", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "field unit) not found")
	assert.Contains(t, out, "inside {...} quote a value")
	assert.NotContains(t, out, "...\n")
	assert.Contains(t, stderr, "1 file(s) could not be read; see the ERROR column")
}
