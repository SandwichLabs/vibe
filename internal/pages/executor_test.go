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
	SQL        string
	EngineName string
	Params     map[string]any
}

func newMockEngine() *mockEngine {
	return &mockEngine{
		results: make(map[string]*ResultSet),
	}
}

func (m *mockEngine) AddResult(sql string, rs *ResultSet) {
	m.results[sql] = rs
}

func (m *mockEngine) Execute(_ context.Context, sql string, engineName string, params map[string]any) (*ResultSet, error) {
	m.calls = append(m.calls, mockCall{SQL: sql, EngineName: engineName, Params: params})
	if rs, ok := m.results[sql]; ok {
		return rs, nil
	}
	return &ResultSet{}, nil
}

func TestExecuteSQLBlocks(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&SQLBlockNode{
				Name:   "revenue",
				Engine: "duckdb",
				SQL:    "SELECT month, total FROM sales",
				Line:   1,
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
	assert.Equal(t, "duckdb", engine.calls[0].EngineName)
}

func TestExecuteDirectiveReferencesValidResult(t *testing.T) {
	page := &Page{
		Nodes: []Node{
			&SQLBlockNode{
				Name:   "data",
				Engine: "duckdb",
				SQL:    "SELECT 1 AS val",
				Line:   1,
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
	assert.Equal(t, "sqlite", engine.calls[0].EngineName)
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
				Name:   "bad_query",
				Engine: "duckdb",
				SQL:    "INVALID SQL",
				Line:   3,
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

func (e *failingEngine) Execute(_ context.Context, _ string, _ string, _ map[string]any) (*ResultSet, error) {
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
