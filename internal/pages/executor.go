package pages

import (
	"context"
	"fmt"
)

// QueryEngine is the interface the executor needs to run SQL queries.
// It avoids importing the engine package directly to prevent circular imports.
type QueryEngine interface {
	Execute(ctx context.Context, sql string, engineName string, params map[string]any) (*ResultSet, error)
}

// ResultSet mirrors engine.ResultSet to avoid circular imports.
type ResultSet struct {
	Columns []Column
	Rows    [][]any
}

// Column describes a single column in a result set.
type Column struct {
	Name string
	Type string
}

// ExecutedPage is the result of executing a parsed page.
type ExecutedPage struct {
	Frontmatter Frontmatter
	Nodes       []ExecutedNode
}

// ExecutedNode pairs an AST node with its optional query result.
type ExecutedNode struct {
	Node   Node
	Result *ResultSet // non-nil for SQL blocks and inline metric queries
}

// Execute walks a parsed page top-to-bottom, executing SQL blocks and resolving
// directive references to result sets.
func Execute(ctx context.Context, page *Page, engine QueryEngine, params map[string]any) (*ExecutedPage, error) {
	if params == nil {
		params = make(map[string]any)
	}

	results := make(map[string]*ResultSet)
	executed := &ExecutedPage{
		Frontmatter: page.Frontmatter,
	}

	nodes, err := executeNodes(ctx, page.Nodes, engine, params, results)
	if err != nil {
		return nil, err
	}
	executed.Nodes = nodes
	return executed, nil
}

// executeNodes processes a slice of nodes, executing SQL and resolving references.
func executeNodes(ctx context.Context, nodes []Node, engine QueryEngine, params map[string]any, results map[string]*ResultSet) ([]ExecutedNode, error) {
	var executed []ExecutedNode

	for _, node := range nodes {
		switch n := node.(type) {
		case *TextNode:
			executed = append(executed, ExecutedNode{Node: n})

		case *SQLBlockNode:
			rs, err := engine.Execute(ctx, n.SQL, n.Engine, params)
			if err != nil {
				return nil, fmt.Errorf("line %d: failed to execute SQL block %q: %w", n.Line, n.Name, err)
			}
			results[n.Name] = rs
			executed = append(executed, ExecutedNode{Node: n, Result: rs})

		case *DirectiveNode:
			en := ExecutedNode{Node: n}

			// Check for metric directive with inline SQL.
			if n.Type == "metric" {
				if sqlVal, ok := n.Config["sql"]; ok {
					sqlStr, ok := sqlVal.(string)
					if !ok {
						return nil, fmt.Errorf("line %d: metric sql must be a string", n.Line)
					}
					engineName := "duckdb"
					if engVal, ok := n.Config["engine"]; ok {
						engineName, _ = engVal.(string)
					}
					rs, err := engine.Execute(ctx, sqlStr, engineName, params)
					if err != nil {
						return nil, fmt.Errorf("line %d: failed to execute inline metric SQL: %w", n.Line, err)
					}
					en.Result = rs
				}
			}

			// Resolve source reference if present.
			if sourceVal, ok := n.Config["source"]; ok {
				if sourceName, ok := sourceVal.(string); ok {
					if en.Result == nil {
						rs, exists := results[sourceName]
						if !exists {
							return nil, fmt.Errorf("line %d: directive %q references undefined result %q", n.Line, n.Type, sourceName)
						}
						en.Result = rs
					}
				}
			}

			executed = append(executed, en)

		case *ConditionalNode:
			// Execute children recursively.
			children, err := executeNodes(ctx, n.Children, engine, params, results)
			if err != nil {
				return nil, err
			}
			// Wrap the conditional node with its executed children.
			executed = append(executed, ExecutedNode{
				Node: &ConditionalNode{
					Condition: n.Condition,
					Children:  extractNodes(children),
					Line:      n.Line,
				},
			})

		default:
			executed = append(executed, ExecutedNode{Node: node})
		}
	}

	return executed, nil
}

// extractNodes pulls the raw Node values out of executed nodes.
func extractNodes(executed []ExecutedNode) []Node {
	nodes := make([]Node, len(executed))
	for i, en := range executed {
		nodes[i] = en.Node
	}
	return nodes
}
