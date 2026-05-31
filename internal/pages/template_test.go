package pages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTemplatesRange(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "sales", SQL: "SELECT region, total FROM sales", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "region"}, {Name: "total"}},
					Rows:    [][]any{{"North", 100}, {"South", 200}},
				},
			},
			{
				Node: &TextNode{Content: `| Region | Total |
|--------|-------|
{{range .Results.sales.Rows}}| {{index . 0}} | {{index . 1}} |
{{end}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Contains(t, text, "| North | 100 |")
	assert.Contains(t, text, "| South | 200 |")
}

func TestExpandTemplatesIf(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "alerts", SQL: "SELECT * FROM alerts", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "msg"}},
					Rows:    [][]any{{"fire"}, {"flood"}},
				},
			},
			{
				Node: &TextNode{Content: `{{if .Results.alerts.Rows}}ALERTS FOUND{{else}}NO ALERTS{{end}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "ALERTS FOUND", text)
}

func TestExpandTemplatesIfEmpty(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node:   &SQLBlockNode{Name: "alerts", SQL: "SELECT * FROM alerts", Line: 1},
				Result: &ResultSet{Columns: []Column{{Name: "msg"}}, Rows: nil},
			},
			{
				Node: &TextNode{Content: `{{if .Results.alerts.Rows}}ALERTS FOUND{{else}}NO ALERTS{{end}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "NO ALERTS", text)
}

func TestExpandTemplatesParams(t *testing.T) {
	executed := &ExecutedPage{
		Frontmatter: Frontmatter{Title: "Report"},
		Nodes: []ExecutedNode{
			{
				Node: &TextNode{Content: `# {{.Meta.title}} for {{.Params.month}}`},
			},
		},
	}

	params := map[string]any{"month": "March"}
	result, err := ExpandTemplates(executed, params)
	require.NoError(t, err)

	tmp, ok := result.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "# Report for March", text)
}

func TestExpandTemplatesVars(t *testing.T) {
	executed := &ExecutedPage{
		Variables: map[string]any{
			"name":   "Acme Corp",
			"region": "US-East",
		},
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: "Report for {{.Vars.name}} in {{.Vars.region}}"}},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "Report for Acme Corp in US-East", text)
}

func TestExpandTemplatesNoSyntax(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: "# Plain markdown\nNo templates here."}},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	// Should pass through unchanged.
	tmp, ok := result.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "# Plain markdown\nNo templates here.", text)
}

func TestExpandTemplatesFuncCell(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "data", SQL: "SELECT x", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "x"}},
					Rows:    [][]any{{42}},
				},
			},
			{
				Node: &TextNode{Content: `{{range .Results.data.Rows}}Value: {{cell . 0}}{{end}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "Value: 42", text)
}

func TestExpandTemplatesFuncCount(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "items", SQL: "SELECT id FROM items", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "id"}},
					Rows:    [][]any{{1}, {2}, {3}},
				},
			},
			{
				Node: &TextNode{Content: `Total: {{count .Results.items}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "Total: 3", text)
}

func TestExpandTemplatesFuncSum(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "sales", SQL: "SELECT amount FROM sales", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "amount"}},
					Rows:    [][]any{{100.0}, {250.5}, {49.5}},
				},
			},
			{
				Node: &TextNode{Content: `Sum: {{sum .Results.sales 0}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "Sum: 400", text)
}

func TestExpandTemplatesFuncCurrency(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &TextNode{Content: `{{fmt_currency 1234.56}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	tmp, ok := result.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	text := tmp.Content
	assert.Equal(t, "$1,234.56", text)
}

func TestExpandTemplatesFuncPercent(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: `{{fmt_percent 0.42}}`}},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)
	node, ok := result.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "42.0%", node.Content)
}

func TestExpandTemplatesFuncCol(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "data", SQL: "SELECT name, age FROM people", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "name"}, {Name: "age"}},
					Rows:    [][]any{{"Alice", 30}},
				},
			},
			{
				Node: &TextNode{Content: `Header: {{col .Results.data 0}} | {{col .Results.data 1}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)
	node, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "Header: name | age", node.Content)
}

func TestExpandTemplatesInvalidSyntax(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: `{{.BadField`}},
		},
	}

	_, err := ExpandTemplates(executed, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template error")
}

func TestExpandTemplatesColNames(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{
				Node: &SQLBlockNode{Name: "data", SQL: "SELECT a, b FROM t", Line: 1},
				Result: &ResultSet{
					Columns: []Column{{Name: "a"}, {Name: "b"}},
					Rows:    [][]any{{1, 2}},
				},
			},
			{
				Node: &TextNode{Content: `{{range .Results.data.ColNames}}{{.}} {{end}}`},
			},
		},
	}

	result, err := ExpandTemplates(executed, nil)
	require.NoError(t, err)
	node, ok := result.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "a b ", node.Content)
}

func TestFormatCommas(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-5000, "-5,000"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, formatCommas(tt.input))
	}
}

func TestToFloat64(t *testing.T) {
	assert.Equal(t, 42.0, toFloat64(42))
	assert.Equal(t, 3.14, toFloat64(3.14))
	assert.Equal(t, 100.0, toFloat64(int64(100)))
	assert.Equal(t, 0.0, toFloat64("not a number"))
	assert.Equal(t, 5.5, toFloat64("5.5"))
	assert.Equal(t, 0.0, toFloat64(nil))
}
