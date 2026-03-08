package pages

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// QueryEngine is the interface the executor needs to run SQL queries.
// It avoids importing the engine package directly to prevent circular imports.
type QueryEngine interface {
	Execute(ctx context.Context, sql string, params map[string]any) (*ResultSet, error)
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
	Variables   map[string]any
}

// ExecutedNode pairs an AST node with its optional query result.
type ExecutedNode struct {
	Node     Node
	Result   *ResultSet     // non-nil for SQL blocks and inline metric queries
	Children []ExecutedNode // non-nil for container nodes (RowNode)
}

// Execute walks a parsed page top-to-bottom, executing SQL blocks and resolving
// directive references to result sets.
func Execute(ctx context.Context, page *Page, engine QueryEngine, params map[string]any) (*ExecutedPage, error) {
	if params == nil {
		params = make(map[string]any)
	}

	// Build variable registry: frontmatter Extra first, then external params override.
	variables := make(map[string]any)
	for k, v := range page.Frontmatter.Extra {
		variables[k] = v
	}
	for k, v := range params {
		variables[k] = v
	}

	results := make(map[string]*ResultSet)
	executed := &ExecutedPage{
		Frontmatter: page.Frontmatter,
		Variables:   variables,
	}

	nodes, err := executeNodes(ctx, page.Nodes, engine, params, results, variables)
	if err != nil {
		return nil, err
	}
	executed.Nodes = nodes
	return executed, nil
}

// executeNodes processes a slice of nodes, executing SQL and resolving references.
func executeNodes(ctx context.Context, nodes []Node, engine QueryEngine, params map[string]any, results map[string]*ResultSet, variables map[string]any) ([]ExecutedNode, error) {
	var executed []ExecutedNode

	for _, node := range nodes {
		switch n := node.(type) {
		case *TextNode:
			executed = append(executed, ExecutedNode{Node: n})

		case *SQLBlockNode:
			sql := substituteSQL(n.SQL, variables)
			rs, err := engine.Execute(ctx, sql, params)
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
					sqlStr = substituteSQL(sqlStr, variables)
					rs, err := engine.Execute(ctx, sqlStr, params)
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
			children, err := executeNodes(ctx, n.Children, engine, params, results, variables)
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

		case *RowNode:
			children, err := executeNodes(ctx, n.Children, engine, params, results, variables)
			if err != nil {
				return nil, err
			}
			executed = append(executed, ExecutedNode{
				Node:     n,
				Children: children,
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

// varPattern matches ${varname} placeholders in SQL.
var varPattern = regexp.MustCompile(`\$\{(\w+)\}`)

// conditionalPattern matches {#if var}...{/if} blocks in SQL.
var conditionalPattern = regexp.MustCompile(`(?s)\{#if\s+(\w+)\}(.*?)\{/if\}`)

// substituteSQL replaces ${varname} placeholders and {#if var}...{/if} conditional
// blocks in SQL text using the provided variable map.
func substituteSQL(sql string, variables map[string]any) string {
	// Process conditionals first.
	sql = conditionalPattern.ReplaceAllStringFunc(sql, func(match string) string {
		sub := conditionalPattern.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		varName := sub[1]
		body := sub[2]
		if isVarSet(variables, varName) {
			return body
		}
		return ""
	})

	// Then substitute ${var} placeholders.
	sql = varPattern.ReplaceAllStringFunc(sql, func(match string) string {
		sub := varPattern.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		varName := sub[1]
		val, ok := variables[varName]
		if !ok || val == nil {
			return "NULL"
		}
		return formatSQLValue(val)
	})

	return sql
}

// isVarSet returns true if the variable exists and is non-empty/non-nil/non-false.
func isVarSet(variables map[string]any, name string) bool {
	val, ok := variables[name]
	if !ok || val == nil {
		return false
	}
	switch v := val.(type) {
	case string:
		return v != ""
	case bool:
		return v
	default:
		return true
	}
}

// formatSQLValue formats a Go value for safe inclusion in SQL text.
func formatSQLValue(val any) string {
	switch v := val.(type) {
	case string:
		escaped := strings.ReplaceAll(v, "'", "''")
		return "'" + escaped + "'"
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		escaped := strings.ReplaceAll(fmt.Sprintf("%v", v), "'", "''")
		return "'" + escaped + "'"
	}
}
