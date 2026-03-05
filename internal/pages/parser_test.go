package pages

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter(t *testing.T) {
	input := `---
title: Sales Dashboard
type: dashboard
refresh: "30s"
params:
  - name: start_date
    type: date
    required: true
  - name: limit
    type: integer
    required: false
    default: "100"
---

# Hello`

	page, err := Parse(input)
	require.NoError(t, err)

	assert.Equal(t, "Sales Dashboard", page.Frontmatter.Title)
	assert.Equal(t, "dashboard", page.Frontmatter.Type)
	assert.Equal(t, "30s", page.Frontmatter.Refresh)
	require.Len(t, page.Frontmatter.Params, 2)

	assert.Equal(t, "start_date", page.Frontmatter.Params[0].Name)
	assert.Equal(t, "date", page.Frontmatter.Params[0].Type)
	assert.True(t, page.Frontmatter.Params[0].Required)

	assert.Equal(t, "limit", page.Frontmatter.Params[1].Name)
	assert.Equal(t, "100", page.Frontmatter.Params[1].Default)
}

func TestParseSQLBlock(t *testing.T) {
	input := "```sql revenue_by_month engine=duckdb cache=5m\nSELECT month, SUM(amount) AS total\nFROM sales\nGROUP BY month\n```"

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	sqlNode, ok := page.Nodes[0].(*SQLBlockNode)
	require.True(t, ok)
	assert.Equal(t, "revenue_by_month", sqlNode.Name)
	assert.Equal(t, "duckdb", sqlNode.Engine)
	assert.Equal(t, 5*time.Minute, sqlNode.Cache)
	assert.Contains(t, sqlNode.SQL, "SELECT month")
	assert.Equal(t, 1, sqlNode.Line)
}

func TestParseSQLBlockDefaultEngine(t *testing.T) {
	input := "```sql my_query\nSELECT 1\n```"

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	sqlNode := page.Nodes[0].(*SQLBlockNode)
	assert.Equal(t, "duckdb", sqlNode.Engine)
}

func TestParseSQLBlockSQLiteEngine(t *testing.T) {
	input := "```sql my_query engine=sqlite\nSELECT 1\n```"

	page, err := Parse(input)
	require.NoError(t, err)

	sqlNode := page.Nodes[0].(*SQLBlockNode)
	assert.Equal(t, "sqlite", sqlNode.Engine)
}

func TestParseTableDirective(t *testing.T) {
	input := "{table: revenue_by_month, page_size: 20, sortable: true}"

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir, ok := page.Nodes[0].(*DirectiveNode)
	require.True(t, ok)
	assert.Equal(t, "table", dir.Type)
	assert.Equal(t, "revenue_by_month", dir.Config["source"])
	assert.Equal(t, 20, dir.Config["page_size"])
	assert.Equal(t, true, dir.Config["sortable"])
}

func TestParseChartDirective(t *testing.T) {
	input := "{chart: revenue_by_month, type: bar, x: month, y: total}"

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir := page.Nodes[0].(*DirectiveNode)
	assert.Equal(t, "chart", dir.Type)
	assert.Equal(t, "revenue_by_month", dir.Config["source"])
	assert.Equal(t, "bar", dir.Config["type"])
	assert.Equal(t, "month", dir.Config["x"])
	assert.Equal(t, "total", dir.Config["y"])
}

func TestParseMetricDirective(t *testing.T) {
	input := `{metric: total_sales, label: "Total Sales"}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir := page.Nodes[0].(*DirectiveNode)
	assert.Equal(t, "metric", dir.Type)
	assert.Equal(t, "total_sales", dir.Config["source"])
	assert.Equal(t, "Total Sales", dir.Config["label"])
}

func TestParseFormDirective(t *testing.T) {
	input := `{form: add_record, target: records, fields: [{name: title, type: text}, {name: amount, type: number}]}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir := page.Nodes[0].(*DirectiveNode)
	assert.Equal(t, "form", dir.Type)
	assert.Equal(t, "add_record", dir.Config["source"])
	assert.Equal(t, "records", dir.Config["target"])

	fields, ok := dir.Config["fields"].([]any)
	require.True(t, ok)
	require.Len(t, fields, 2)

	field0, ok := fields[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "title", field0["name"])
	assert.Equal(t, "text", field0["type"])
}

func TestParseDetailDirective(t *testing.T) {
	input := "{detail: order_info}"

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir := page.Nodes[0].(*DirectiveNode)
	assert.Equal(t, "detail", dir.Type)
	assert.Equal(t, "order_info", dir.Config["source"])
}

func TestParseConditionalBlock(t *testing.T) {
	input := `{if: "total > 100"}
Some text inside
{table: results}
{/if}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	cond, ok := page.Nodes[0].(*ConditionalNode)
	require.True(t, ok)
	assert.Equal(t, "total > 100", cond.Condition)
	require.Len(t, cond.Children, 2)

	_, ok = cond.Children[0].(*TextNode)
	assert.True(t, ok)

	dir, ok := cond.Children[1].(*DirectiveNode)
	require.True(t, ok)
	assert.Equal(t, "table", dir.Type)
}

func TestParseInvalidSQLBlockMissingName(t *testing.T) {
	input := "```sql\nSELECT 1\n```"

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1")
	assert.Contains(t, err.Error(), "requires a name")
}

func TestParseUnclosedSQLBlock(t *testing.T) {
	input := "```sql my_query\nSELECT 1"

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1")
	assert.Contains(t, err.Error(), "unclosed SQL block")
}

func TestParseUnclosedFrontmatter(t *testing.T) {
	input := "---\ntitle: Test\n"

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed frontmatter")
}

func TestParseUnclosedConditional(t *testing.T) {
	input := `{if: "x > 1"}
some text`

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed {if:}")
}

func TestParseStrayEndIf(t *testing.T) {
	input := `some text
{/if}`

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected {/if}")
}

func TestParseInvalidEngine(t *testing.T) {
	input := "```sql my_query engine=postgres\nSELECT 1\n```"

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid engine")
}

func TestParseFullPage(t *testing.T) {
	input := "---\ntitle: Sales Report\ntype: notebook\n---\n\n# Monthly Sales\n\n" +
		"```sql revenue_by_month engine=duckdb\n" +
		"SELECT month, SUM(amount) AS total\nFROM sales\nGROUP BY month\nORDER BY month\n" +
		"```\n\n{table: revenue_by_month, page_size: 25}\n\n{chart: revenue_by_month, type: bar, x: month, y: total}"

	page, err := Parse(input)
	require.NoError(t, err)

	assert.Equal(t, "Sales Report", page.Frontmatter.Title)
	assert.Equal(t, "notebook", page.Frontmatter.Type)

	// Expect: text, sql block, text (empty between blocks), table directive, text (empty), chart directive
	// The exact count depends on whitespace handling, so check for key node types.
	var sqlCount, dirCount int
	for _, n := range page.Nodes {
		switch n.(type) {
		case *SQLBlockNode:
			sqlCount++
		case *DirectiveNode:
			dirCount++
		}
	}
	assert.Equal(t, 1, sqlCount)
	assert.Equal(t, 2, dirCount)
}

func TestParseNestedFormFields(t *testing.T) {
	input := `{form: edit_user, target: users, fields: [{name: email, type: email, required: true}, {name: role, type: select}]}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir := page.Nodes[0].(*DirectiveNode)
	assert.Equal(t, "form", dir.Type)

	fields, ok := dir.Config["fields"].([]any)
	require.True(t, ok)
	require.Len(t, fields, 2)

	field0 := fields[0].(map[string]any)
	assert.Equal(t, "email", field0["name"])
	assert.Equal(t, true, field0["required"])

	field1 := fields[1].(map[string]any)
	assert.Equal(t, "select", field1["type"])
}
