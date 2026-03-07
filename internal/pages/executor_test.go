package pages

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEngine is a test implementation of QueryEngine.
type mockEngine struct {
	results map[string]*ResultSet
	calls   []mockCall
}

type mockCall struct {
	SQL    string
	Params map[string]any
}

func newMockEngine() *mockEngine {
	return &mockEngine{
		results: make(map[string]*ResultSet),
	}
}

func (m *mockEngine) AddResult(sql string, rs *ResultSet) {
	m.results[sql] = rs
}

func (m *mockEngine) Execute(_ context.Context, sql string, params map[string]any) (*ResultSet, error) {
	m.calls = append(m.calls, mockCall{SQL: sql, Params: params})
	if rs, ok := m.results[sql]; ok {
		return rs, nil
	}
	return &ResultSet{}, nil
}

func TestExecuteSQLBlocks(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&SQLBlockNode{
				Name: "revenue",
				SQL:  "SELECT month, total FROM sales",
				Line: 1,
			},
		},
	}

	engine := newMockEngine()
	expected := &ResultSet{
		Columns: []Column{{Name: "month", Type: "text"}, {Name: "total", Type: "integer"}},
		Rows:    [][]any{{"Jan", 100}, {"Feb", 200}},
	}
	engine.AddResult("SELECT month, total FROM sales", expected)

	result, err := Execute(context.Background(), page, engine, nil)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 1)

	assert.Equal(t, expected, result.Nodes[0].Result)
	require.Len(t, engine.calls, 1)
	assert.Equal(t, "SELECT month, total FROM sales", engine.calls[0].SQL)
}

func TestExecuteDirectiveReferencesValidResult(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&SQLBlockNode{
				Name: "data",
				SQL:  "SELECT 1 AS val",
				Line: 1,
			},
			&DirectiveNode{
				Type:   "table",
				Config: map[string]any{"source": "data"},
				Line:   5,
			},
		},
	}

	engine := newMockEngine()
	expected := &ResultSet{
		Columns: []Column{{Name: "val", Type: "integer"}},
		Rows:    [][]any{{1}},
	}
	engine.AddResult("SELECT 1 AS val", expected)

	result, err := Execute(context.Background(), page, engine, nil)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 2)

	// The table directive should reference the SQL block's result.
	assert.Equal(t, expected, result.Nodes[1].Result)
}

func TestExecuteDirectiveReferencesMissingResult(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&DirectiveNode{
				Type:   "table",
				Config: map[string]any{"source": "nonexistent"},
				Line:   3,
			},
		},
	}

	engine := newMockEngine()

	_, err := Execute(context.Background(), page, engine, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined result")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestExecuteInlineMetricSQL(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&DirectiveNode{
				Type: "metric",
				Config: map[string]any{
					"source": "total_metric",
					"sql":    "SELECT SUM(amount) AS total FROM sales",
					"label":  "Total Sales",
				},
				Line: 1,
			},
		},
	}

	engine := newMockEngine()
	expected := &ResultSet{
		Columns: []Column{{Name: "total", Type: "integer"}},
		Rows:    [][]any{{5000}},
	}
	engine.AddResult("SELECT SUM(amount) AS total FROM sales", expected)

	result, err := Execute(context.Background(), page, engine, nil)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 1)

	// Inline SQL takes precedence over source reference.
	assert.Equal(t, expected, result.Nodes[0].Result)
}

func TestExecuteInlineMetricWithEngine(t *testing.T) {
	// engine config is accepted but ignored; all queries run on DuckDB
	page := &Page{
		Nodes: []Node{
			&DirectiveNode{
				Type: "metric",
				Config: map[string]any{
					"source": "count_metric",
					"sql":    "SELECT COUNT(*) FROM users",
					"engine": "sqlite",
				},
				Line: 1,
			},
		},
	}

	engine := newMockEngine()
	engine.AddResult("SELECT COUNT(*) FROM users", &ResultSet{
		Columns: []Column{{Name: "count", Type: "integer"}},
		Rows:    [][]any{{42}},
	})

	_, err := Execute(context.Background(), page, engine, nil)
	require.NoError(t, err)

	require.Len(t, engine.calls, 1)
	assert.Equal(t, "SELECT COUNT(*) FROM users", engine.calls[0].SQL)
}

func TestExecuteTextNodesPassThrough(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&TextNode{Content: "# Hello World"},
		},
	}

	engine := newMockEngine()

	result, err := Execute(context.Background(), page, engine, nil)
	require.NoError(t, err)
	require.Len(t, result.Nodes, 1)

	assert.Nil(t, result.Nodes[0].Result)
	textNode, ok := result.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "# Hello World", textNode.Content)
}

func TestExecuteSQLBlockError(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&SQLBlockNode{
				Name: "bad_query",
				SQL:  "INVALID SQL",
				Line: 3,
			},
		},
	}

	engine := &failingEngine{err: fmt.Errorf("syntax error")}

	_, err := Execute(context.Background(), page, engine, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 3")
	assert.Contains(t, err.Error(), "bad_query")
	assert.Contains(t, err.Error(), "syntax error")
}

type failingEngine struct {
	err error
}

func (e *failingEngine) Execute(_ context.Context, _ string, _ map[string]any) (*ResultSet, error) {
	return nil, e.err
}

func TestValidateParamsRequired(t *testing.T) {
	page := &Page{
		Frontmatter: Frontmatter{
			Params: []ParamDef{
				{Name: "start_date", Type: "date", Required: true},
				{Name: "limit", Type: "integer", Required: false, Default: "100"},
			},
		},
	}

	// Missing required param.
	err := ValidateParams(page, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_date")

	// Providing required param.
	err = ValidateParams(page, map[string]any{"start_date": "2024-01-01"})
	require.NoError(t, err)

	// Nil provided params.
	err = ValidateParams(page, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_date")
}

func TestValidateParamsWithDefault(t *testing.T) {
	page := &Page{
		Frontmatter: Frontmatter{
			Params: []ParamDef{
				{Name: "limit", Type: "integer", Required: true, Default: "50"},
			},
		},
	}

	// Required but has default - should pass.
	err := ValidateParams(page, map[string]any{})
	require.NoError(t, err)
}

func TestResolveLinks(t *testing.T) {
	content := "See [[sales_report]] for details and [[inventory]] for stock."
	pages := map[string]string{
		"sales_report": "/pages/sales_report",
		"inventory":    "/pages/inventory",
	}

	result := ResolveLinks(content, pages)
	assert.Contains(t, result, "[sales_report](/pages/sales_report)")
	assert.Contains(t, result, "[inventory](/pages/inventory)")
	assert.NotContains(t, result, "[[")
}

func TestFrontmatterVarsInSQL(t *testing.T) {
	page := &Page{
		Frontmatter: Frontmatter{
			Extra: map[string]string{
				"category": "electronics",
			},
		},
		Nodes: []Node{
			&SQLBlockNode{
				Name: "filtered",
				SQL:  "SELECT * FROM products WHERE category = ${category}",
				Line: 1,
			},
		},
	}

	eng := newMockEngine()
	eng.AddResult("SELECT * FROM products WHERE category = 'electronics'", &ResultSet{
		Columns: []Column{{Name: "name", Type: "text"}},
		Rows:    [][]any{{"Laptop"}},
	})

	result, err := Execute(context.Background(), page, eng, nil)
	require.NoError(t, err)
	require.Len(t, eng.calls, 1)
	assert.Equal(t, "SELECT * FROM products WHERE category = 'electronics'", eng.calls[0].SQL)
	assert.NotNil(t, result.Variables)
	assert.Equal(t, "electronics", result.Variables["category"])
}

func TestExternalParamOverridesFrontmatter(t *testing.T) {
	page := &Page{
		Frontmatter: Frontmatter{
			Extra: map[string]string{
				"category": "electronics",
			},
		},
		Nodes: []Node{
			&SQLBlockNode{
				Name: "filtered",
				SQL:  "SELECT * FROM products WHERE category = ${category}",
				Line: 1,
			},
		},
	}

	eng := newMockEngine()
	eng.AddResult("SELECT * FROM products WHERE category = 'clothing'", &ResultSet{
		Columns: []Column{{Name: "name", Type: "text"}},
		Rows:    [][]any{{"Shirt"}},
	})

	params := map[string]any{"category": "clothing"}
	result, err := Execute(context.Background(), page, eng, params)
	require.NoError(t, err)
	require.Len(t, eng.calls, 1)
	// External param "clothing" should override frontmatter "electronics".
	assert.Equal(t, "SELECT * FROM products WHERE category = 'clothing'", eng.calls[0].SQL)
	assert.Equal(t, "clothing", result.Variables["category"])
}

func TestConditionalSQLRemoval(t *testing.T) {
	// When variable is unset, conditional block is removed.
	sql := "SELECT * FROM products {#if category}WHERE category = ${category}{/if}"
	result := substituteSQL(sql, map[string]any{})
	assert.Equal(t, "SELECT * FROM products ", result)
}

func TestConditionalSQLInclusion(t *testing.T) {
	// When variable is set, conditional block content is included.
	sql := "SELECT * FROM products {#if category}WHERE category = ${category}{/if}"
	result := substituteSQL(sql, map[string]any{"category": "books"})
	assert.Equal(t, "SELECT * FROM products WHERE category = 'books'", result)
}

func TestConditionalSQLEmptyStringRemoval(t *testing.T) {
	sql := "SELECT * FROM products {#if category}WHERE category = ${category}{/if}"
	result := substituteSQL(sql, map[string]any{"category": ""})
	assert.Equal(t, "SELECT * FROM products ", result)
}

func TestConditionalSQLFalseRemoval(t *testing.T) {
	sql := "SELECT * FROM products {#if active}WHERE active = true{/if}"
	result := substituteSQL(sql, map[string]any{"active": false})
	assert.Equal(t, "SELECT * FROM products ", result)
}

func TestSubstituteSQLNumericValue(t *testing.T) {
	sql := "SELECT * FROM products LIMIT ${limit}"
	result := substituteSQL(sql, map[string]any{"limit": 10})
	assert.Equal(t, "SELECT * FROM products LIMIT 10", result)
}

func TestSubstituteSQLNilValue(t *testing.T) {
	sql := "SELECT * FROM products WHERE category = ${category}"
	result := substituteSQL(sql, map[string]any{})
	assert.Equal(t, "SELECT * FROM products WHERE category = NULL", result)
}

func TestSubstituteSQLEscapesSingleQuotes(t *testing.T) {
	sql := "SELECT * FROM products WHERE name = ${name}"
	result := substituteSQL(sql, map[string]any{"name": "it's a test"})
	assert.Equal(t, "SELECT * FROM products WHERE name = 'it''s a test'", result)
}

func TestVariablesPopulatedInExecutedPage(t *testing.T) {
	page := &Page{
		Frontmatter: Frontmatter{
			Extra: map[string]string{
				"region": "us-east",
				"limit":  "100",
			},
		},
		Nodes: []Node{
			&TextNode{Content: "# Report"},
		},
	}

	eng := newMockEngine()
	params := map[string]any{"limit": "50"}
	result, err := Execute(context.Background(), page, eng, params)
	require.NoError(t, err)

	// External param overrides frontmatter.
	assert.Equal(t, "50", result.Variables["limit"])
	// Frontmatter var preserved when no override.
	assert.Equal(t, "us-east", result.Variables["region"])
}
