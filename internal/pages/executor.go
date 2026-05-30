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
			survives, err := evalCondition(n, results, variables)
			if err != nil {
				return nil, err
			}
			if !survives {
				// Dropped branch contributes nothing.
				continue
			}
			// Inline the surviving branch's children directly into the output so
			// downstream rendering never has to handle a ConditionalNode.
			children, err := executeNodes(ctx, n.Children, engine, params, results, variables)
			if err != nil {
				return nil, err
			}
			executed = append(executed, children...)

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

// equalityPattern matches variable-equality conditions of the form
// `{{var}} == 'literal'` or `{{var}} != "literal"`. Whitespace is flexible and
// either single or double quotes may delimit the RHS string literal.
var equalityPattern = regexp.MustCompile(`^\{\{\s*(\w+)\s*\}\}\s*(==|!=)\s*(['"])(.*)(['"])$`)

// evalCondition decides whether a ConditionalNode's branch survives.
//
// Two condition forms are supported:
//
//  1. Variable-equality expression — `{{var}} == 'x'` or `{{var}} != "x"`.
//     The {{var}} placeholder is resolved from the variables map (stringified),
//     compared against the RHS string literal, and the result drives survival.
//     The Mode/no_rows inversion does NOT apply to this form (it only makes
//     sense for the row-count form); use `!=` to express the inverse instead.
//
//  2. Bare identifier — the name of an already-executed SQL result set. The
//     branch survives when that result set has rows (or has zero rows when
//     Mode == "no_rows").
func evalCondition(n *ConditionalNode, results map[string]*ResultSet, variables map[string]any) (bool, error) {
	cond := strings.TrimSpace(n.Condition)

	// Form 1: variable-equality expression.
	if m := equalityPattern.FindStringSubmatch(cond); m != nil {
		if m[3] != m[5] {
			return false, fmt.Errorf("line %d: mismatched quotes in conditional expression %q", n.Line, cond)
		}
		varName := m[1]
		op := m[2]
		literal := m[4]

		var actual string
		if v, ok := variables[varName]; ok && v != nil {
			actual = fmt.Sprintf("%v", v)
		}
		equal := actual == literal
		if op == "!=" {
			return !equal, nil
		}
		return equal, nil
	}

	// Form 2: bare identifier referencing a named result set (row-count form).
	rs, exists := results[cond]
	if !exists {
		return false, fmt.Errorf("line %d: conditional references undefined result %q", n.Line, cond)
	}
	hasRows := len(rs.Rows) > 0
	survives := hasRows
	if n.Mode == "no_rows" {
		survives = !hasRows
	}
	return survives, nil
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
