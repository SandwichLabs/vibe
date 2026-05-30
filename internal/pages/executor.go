package pages

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
)

// inlineValueSeq provides process-wide unique placeholder tokens for inline
// {value:} directives. Using a monotonic counter (rather than a per-TextNode
// idx) guarantees placeholders never collide even if a future change batches
// substitution across multiple nodes.
var inlineValueSeq atomic.Uint64

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

	// InlineValues holds resolved inline {value: ...} directives for a TextNode.
	// Each entry maps a placeholder token (embedded into the node's text content)
	// to a raw scalar value and the format code to render it with. The renderer
	// substitutes formatted <span> elements for the placeholders after markdown
	// conversion so they can embed within prose / report bands.
	InlineValues []InlineValue
}

// InlineValue is a resolved inline {value: ...} directive: a placeholder token
// embedded in a TextNode's content, the raw scalar resolved for it, and the
// Evidence-compatible format code to render it with.
type InlineValue struct {
	Placeholder string // unique sentinel embedded in the TextNode content
	Value       any    // resolved scalar (nil when missing/empty → safe placeholder)
	Fmt         string // format code passed to renderer.Format
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
			en, err := resolveInlineValues(ctx, n, engine, params, results, variables)
			if err != nil {
				return nil, err
			}
			executed = append(executed, en)

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

			// A standalone-line {value: ...} directive (the natural form for
			// embedding a single scalar in a report detail band) resolves like an
			// inline value: it supports inline `sql:` or `source`+`column`, and a
			// missing column / empty result degrades to a nil value (safe
			// placeholder) rather than aborting the page. We must handle it here
			// because `fmt`/`column` are NOT source references and would otherwise
			// fall into the generic source-resolution branch below and error.
			if n.Type == "value" {
				val, fmtCode, err := resolveInlineValueConfig(ctx, n.Config, engine, params, results, variables)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", n.Line, err)
				}
				en.InlineValues = []InlineValue{{Value: val, Fmt: fmtCode}}
				executed = append(executed, en)
				continue
			}

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

		case *BlockNode:
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

// inlineValuePattern matches inline {value: ...} directives embedded within
// prose text, e.g. "Revenue is {value: source=totals, column: total, fmt: usd2}".
// It is non-greedy so multiple directives on one line resolve independently.
var inlineValuePattern = regexp.MustCompile(`\{value:[^{}]*\}`)

// resolveInlineValues scans a TextNode for inline {value: ...} directives,
// resolves each (either from a named SQL result via source+column, or by
// executing inline SQL), and rewrites the node's content to embed an opaque
// placeholder per directive. The resolved scalar + format code are attached to
// the returned ExecutedNode for the renderer to format. Resolution failures
// (missing column, empty result) degrade to a nil value (safe placeholder)
// rather than aborting the page.
func resolveInlineValues(ctx context.Context, n *TextNode, engine QueryEngine, params map[string]any, results map[string]*ResultSet, variables map[string]any) (ExecutedNode, error) {
	en := ExecutedNode{Node: n}
	if !strings.Contains(n.Content, "{value:") {
		return en, nil
	}

	var inlineValues []InlineValue
	var resolveErr error

	newContent := inlineValuePattern.ReplaceAllStringFunc(n.Content, func(match string) string {
		if resolveErr != nil {
			return match
		}
		// Strip the leading "{value:" and trailing "}" to get the inner config.
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "{value:"), "}")
		config, err := parseDirectiveYAML(inner)
		if err != nil {
			resolveErr = fmt.Errorf("line: invalid inline {value} directive %q: %w", match, err)
			return match
		}
		// Normalize bare "key=value" tokens (e.g. "source=totals"), which YAML
		// parses as a key with a nil value, into proper map entries — matching
		// the directive-line parsing convention.
		for k, v := range config {
			if v == nil {
				if key, val, isKV := strings.Cut(k, "="); isKV {
					delete(config, k)
					config[key] = val
				}
			}
		}

		val, fmtCode, err := resolveInlineValueConfig(ctx, config, engine, params, results, variables)
		if err != nil {
			resolveErr = err
			return match
		}

		placeholder := fmt.Sprintf("xVIBEVALUEx%dx", inlineValueSeq.Add(1))
		inlineValues = append(inlineValues, InlineValue{
			Placeholder: placeholder,
			Value:       val,
			Fmt:         fmtCode,
		})
		return placeholder
	})
	if resolveErr != nil {
		return en, resolveErr
	}

	// Replace the node's content in place so downstream rendering uses the
	// placeholder-bearing text.
	n.Content = newContent
	en.InlineValues = inlineValues
	return en, nil
}

// resolveInlineValueConfig resolves a single inline {value} directive's config
// into a scalar value and a format code. Two forms are supported:
//
//  1. source=<block>, column=<col> — reads the named, already-executed SQL
//     result and pulls the first row's <col> value.
//  2. sql="SELECT ..." — executes inline SQL (like the metric directive) and
//     pulls the first cell of the first row.
//
// A missing column or empty result resolves to a nil value (safe placeholder),
// not an error.
func resolveInlineValueConfig(ctx context.Context, config map[string]any, engine QueryEngine, params map[string]any, results map[string]*ResultSet, variables map[string]any) (any, string, error) {
	fmtCode, _ := config["fmt"].(string)

	// Form 2: inline SQL.
	if sqlVal, ok := config["sql"]; ok {
		sqlStr, ok := sqlVal.(string)
		if !ok {
			return nil, fmtCode, fmt.Errorf("inline {value} sql must be a string")
		}
		sqlStr = substituteSQL(sqlStr, variables)
		rs, err := engine.Execute(ctx, sqlStr, params)
		if err != nil {
			return nil, fmtCode, fmt.Errorf("failed to execute inline {value} SQL: %w", err)
		}
		if rs != nil && len(rs.Rows) > 0 && len(rs.Rows[0]) > 0 {
			return rs.Rows[0][0], fmtCode, nil
		}
		return nil, fmtCode, nil
	}

	// Form 1: source + column.
	sourceName, _ := config["source"].(string)
	if sourceName == "" {
		return nil, fmtCode, fmt.Errorf("inline {value} requires either sql or source")
	}
	rs, exists := results[sourceName]
	if !exists {
		return nil, fmtCode, fmt.Errorf("inline {value} references undefined result %q", sourceName)
	}
	column, _ := config["column"].(string)
	if column == "" {
		// No column → first cell of first row.
		if rs != nil && len(rs.Rows) > 0 && len(rs.Rows[0]) > 0 {
			return rs.Rows[0][0], fmtCode, nil
		}
		return nil, fmtCode, nil
	}
	// Locate the column by name; missing column degrades to nil (safe placeholder).
	colIdx := -1
	for i, c := range rs.Columns {
		if c.Name == column {
			colIdx = i
			break
		}
	}
	if colIdx == -1 || len(rs.Rows) == 0 || colIdx >= len(rs.Rows[0]) {
		return nil, fmtCode, nil
	}
	return rs.Rows[0][colIdx], fmtCode, nil
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
