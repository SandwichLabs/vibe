package pages

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Parse parses a markdown page with frontmatter, SQL blocks, and directives
// into a Page AST.
func Parse(input string) (*Page, error) {
	page := &Page{}
	lines := strings.Split(input, "\n")

	idx := 0

	// Parse optional frontmatter.
	if idx < len(lines) && strings.TrimSpace(lines[idx]) == "---" {
		fmEnd := -1
		for i := idx + 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				fmEnd = i
				break
			}
		}
		if fmEnd == -1 {
			return nil, fmt.Errorf("line %d: unclosed frontmatter (missing closing ---)", idx+1)
		}
		fmContent := strings.Join(lines[idx+1:fmEnd], "\n")
		if err := yaml.Unmarshal([]byte(fmContent), &page.Frontmatter); err != nil {
			return nil, fmt.Errorf("line %d: invalid frontmatter YAML: %w", idx+1, err)
		}
		idx = fmEnd + 1
	}

	// Parse body nodes.
	nodes, endIdx, err := parseNodes(lines, idx, false)
	if err != nil {
		return nil, err
	}
	if endIdx < len(lines) {
		// Should not happen at top level unless there's a stray {/if}.
		return nil, fmt.Errorf("line %d: unexpected {/if} without matching {if:}", endIdx+1)
	}
	page.Nodes = nodes
	return page, nil
}

// parseNodes parses lines into nodes. When inConditional is true, it stops at {/if}.
// Returns the parsed nodes and the line index after the last consumed line.
func parseNodes(lines []string, start int, inConditional bool) ([]Node, int, error) {
	var nodes []Node
	var textBuf strings.Builder
	textStart := start
	idx := start

	flushText := func() {
		content := textBuf.String()
		if content != "" {
			nodes = append(nodes, &TextNode{Content: content})
		}
		textBuf.Reset()
		textStart = idx
	}

	for idx < len(lines) {
		line := lines[idx]
		trimmed := strings.TrimSpace(line)

		// Check for {/if} closing tag.
		if trimmed == "{/if}" {
			if !inConditional {
				return nil, idx, fmt.Errorf("line %d: unexpected {/if} without matching {if:}", idx+1)
			}
			flushText()
			return nodes, idx + 1, nil
		}

		// Check for SQL fenced code block.
		if strings.HasPrefix(trimmed, "```sql") {
			flushText()
			sqlNode, endIdx, err := parseSQLBlock(lines, idx)
			if err != nil {
				return nil, idx, err
			}
			nodes = append(nodes, sqlNode)
			idx = endIdx
			textStart = idx
			continue
		}

		// Check for directive line: must be a standalone line matching {type: ...}.
		if isDirectiveLine(trimmed) {
			flushText()

			// Check if this is a conditional block.
			if isConditionalStart(trimmed) {
				condNode, endIdx, err := parseConditional(lines, idx)
				if err != nil {
					return nil, idx, err
				}
				nodes = append(nodes, condNode)
				idx = endIdx
				textStart = idx
				continue
			}

			dirNode, err := parseDirective(trimmed, idx)
			if err != nil {
				return nil, idx, err
			}
			nodes = append(nodes, dirNode)
			idx++
			textStart = idx
			continue
		}

		// Regular text line.
		if textBuf.Len() > 0 || idx > textStart {
			textBuf.WriteString("\n")
		}
		textBuf.WriteString(line)
		idx++
	}

	if inConditional {
		return nil, idx, fmt.Errorf("line %d: unclosed {if:} block (missing {/if})", start+1)
	}

	flushText()
	return nodes, idx, nil
}

// parseSQLBlock parses a fenced SQL code block starting at lines[start].
// Format: ```sql name [engine=X] [cache=Xm]
func parseSQLBlock(lines []string, start int) (*SQLBlockNode, int, error) {
	header := strings.TrimSpace(lines[start])
	// Remove the ``` prefix.
	header = strings.TrimPrefix(header, "```")
	parts := strings.Fields(header)

	node := &SQLBlockNode{
		Engine: "duckdb", // default engine
		Line:   start + 1,
	}

	// parts[0] is "sql"
	if len(parts) < 2 {
		return nil, start, fmt.Errorf("line %d: SQL block requires a name: ```sql name [engine=X] [cache=Xm]", start+1)
	}

	node.Name = parts[1]

	// Parse optional key=value pairs.
	for _, part := range parts[2:] {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return nil, start, fmt.Errorf("line %d: invalid SQL block option %q (expected key=value)", start+1, part)
		}
		switch key {
		case "engine":
			if val != "duckdb" && val != "sqlite" {
				return nil, start, fmt.Errorf("line %d: invalid engine %q (must be duckdb or sqlite)", start+1, val)
			}
			node.Engine = val
		case "cache":
			d, err := time.ParseDuration(val)
			if err != nil {
				return nil, start, fmt.Errorf("line %d: invalid cache duration %q: %w", start+1, val, err)
			}
			node.Cache = d
		default:
			return nil, start, fmt.Errorf("line %d: unknown SQL block option %q", start+1, key)
		}
	}

	// Collect SQL body until closing ```.
	var sqlBuf strings.Builder
	idx := start + 1
	for idx < len(lines) {
		if strings.TrimSpace(lines[idx]) == "```" {
			node.SQL = strings.TrimSpace(sqlBuf.String())
			return node, idx + 1, nil
		}
		if sqlBuf.Len() > 0 {
			sqlBuf.WriteString("\n")
		}
		sqlBuf.WriteString(lines[idx])
		idx++
	}

	return nil, start, fmt.Errorf("line %d: unclosed SQL block (missing closing ```)", start+1)
}

// isDirectiveLine checks if a line looks like a directive: {type: ...}.
func isDirectiveLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}

// isConditionalStart checks if a directive line is an {if: ...} block start.
func isConditionalStart(trimmed string) bool {
	inner := trimmed[1 : len(trimmed)-1]
	inner = strings.TrimSpace(inner)
	return strings.HasPrefix(inner, "if:")
}

// parseConditional parses an {if: "expr"} ... {/if} block.
func parseConditional(lines []string, start int) (*ConditionalNode, int, error) {
	trimmed := strings.TrimSpace(lines[start])
	inner := trimmed[1 : len(trimmed)-1]

	config, err := parseDirectiveYAML(inner)
	if err != nil {
		return nil, start, fmt.Errorf("line %d: invalid conditional directive: %w", start+1, err)
	}

	condition, ok := config["if"]
	if !ok {
		return nil, start, fmt.Errorf("line %d: conditional directive missing condition", start+1)
	}

	condStr, ok := condition.(string)
	if !ok {
		return nil, start, fmt.Errorf("line %d: conditional expression must be a string", start+1)
	}

	children, endIdx, err := parseNodes(lines, start+1, true)
	if err != nil {
		return nil, start, err
	}

	return &ConditionalNode{
		Condition: condStr,
		Children:  children,
		Line:      start + 1,
	}, endIdx, nil
}

// parseDirective parses a single directive line into a DirectiveNode.
func parseDirective(trimmed string, lineIdx int) (*DirectiveNode, error) {
	inner := trimmed[1 : len(trimmed)-1]

	config, err := parseDirectiveYAML(inner)
	if err != nil {
		return nil, fmt.Errorf("line %d: invalid directive: %w", lineIdx+1, err)
	}

	// The first key in the YAML map is the directive type. Its value is the primary argument.
	dirType, source, remaining := extractDirectiveType(config)
	if dirType == "" {
		return nil, fmt.Errorf("line %d: empty directive", lineIdx+1)
	}

	// Build the config map with "source" as the primary value.
	result := make(map[string]any)
	if source != nil {
		result["source"] = source
	}
	for k, v := range remaining {
		result[k] = v
	}

	return &DirectiveNode{
		Type:   dirType,
		Config: result,
		Line:   lineIdx + 1,
	}, nil
}

// parseDirectiveYAML parses the inner content of a directive as YAML.
func parseDirectiveYAML(inner string) (map[string]any, error) {
	// Wrap in braces to make it valid YAML flow mapping.
	yamlStr := "{" + inner + "}"
	var result map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse directive YAML: %w", err)
	}
	return result, nil
}

// extractDirectiveType finds the first key in the map (the directive type),
// extracts its value, and returns the remaining keys.
// Since Go maps are unordered, we re-parse to find the first key.
func extractDirectiveType(config map[string]any) (string, any, map[string]any) {
	if len(config) == 0 {
		return "", nil, nil
	}

	// To get a stable first key, we look at the keys and use a heuristic:
	// known directive types get priority.
	knownTypes := []string{"table", "chart", "metric", "form", "detail", "if"}
	for _, t := range knownTypes {
		if val, ok := config[t]; ok {
			remaining := make(map[string]any)
			for k, v := range config {
				if k != t {
					remaining[k] = v
				}
			}
			return t, val, remaining
		}
	}

	// Fallback: pick any key.
	for k, v := range config {
		remaining := make(map[string]any)
		for k2, v2 := range config {
			if k2 != k {
				remaining[k2] = v2
			}
		}
		return k, v, remaining
	}

	return "", nil, nil
}
